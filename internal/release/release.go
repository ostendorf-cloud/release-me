// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package release tags, releases and swaps the label of merged release pull
// requests. It calls the forge directly: tagging and releasing need no
// local git write, only the command layer's git worktree for the pr command
// does.
package release

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// titlePattern matches the release pull request title releasepr.Title
// renders, "chore(<name>): release vX.Y.Z" or "chore(<name>): release
// vX.Y.Z-<pre>" for a pre-release.
var titlePattern = regexp.MustCompile(`^chore\(([^)]+)\): release v(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$`)

// VersionFromTitle extracts "X.Y.Z", or "X.Y.Z-<pre>" for a pre-release,
// from a release pull request title whose component name matches name. ok
// is false when title does not match that shape, or names a different
// component.
func VersionFromTitle(title, name string) (versionStr string, ok bool) {
	m := titlePattern.FindStringSubmatch(title)
	if m == nil || m[1] != name {
		return "", false
	}
	return m[2], true
}

// LockedTitle renders the release pull request and release title for a
// locked family run: the version, then every component it covers, in
// release-me.yaml's order, so the title names both even though it always
// covers the whole family.
func LockedTitle(versionStr string, names []string) string {
	return fmt.Sprintf("chore: release v%s (%s)", versionStr, strings.Join(names, ", "))
}

// lockedTitlePattern matches the title LockedTitle renders.
var lockedTitlePattern = regexp.MustCompile(`^chore: release v(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?) \(([^)]*)\)$`)

// VersionFromLockedTitle extracts the version and the component names from
// a locked release pull request title LockedTitle renders. ok is false
// when title does not match that shape.
func VersionFromLockedTitle(title string) (versionStr string, names []string, ok bool) {
	m := lockedTitlePattern.FindStringSubmatch(title)
	if m == nil {
		return "", nil, false
	}
	return m[1], strings.Split(m[2], ", "), true
}

// lockedReleaseBody renders the GitHub release body for one component of a
// release pull request every component shares, locked or not: the free
// text of the release-me-notes block, shared by every component, followed
// by name's own slice of the combined changelog.
func lockedReleaseBody(prBody, name string) string {
	var parts []string
	if notes, ok := releasepr.ExtractNotes(prBody); ok && notes != "" {
		parts = append(parts, notes)
	}
	if section := releasepr.ComponentSection(releasepr.ExtractChangelog(prBody), name); section != "" {
		parts = append(parts, section)
	}
	return strings.Join(parts, "\n\n")
}

// TitleRelease is one component a shared release pull request title names,
// at the version it releases.
type TitleRelease struct {
	Name    string
	Version string
}

// SharedTitle renders the release pull request title for a shared release
// pull request in VersioningIndependent or VersioningLinked: every
// component it releases, in release-me.yaml's order, each at its own
// version, "chore: release api v1.3.0, web v0.4.1". The title is where
// release reads every component's version back from after the merge, the
// same as it does for one component's own title.
func SharedTitle(releases []TitleRelease) string {
	parts := make([]string, 0, len(releases))
	for _, r := range releases {
		parts = append(parts, fmt.Sprintf("%s v%s", r.Name, r.Version))
	}
	return "chore: release " + strings.Join(parts, ", ")
}

// sharedTitlePattern matches the title SharedTitle renders, and
// sharedTitlePart one of its comma separated components.
var (
	sharedTitlePattern = regexp.MustCompile(`^chore: release (.+)$`)
	sharedTitlePart    = regexp.MustCompile(`^(\S+) v(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$`)
)

// ParseSharedTitle extracts every component and its version from a title
// SharedTitle renders. ok is false when title does not match that shape.
func ParseSharedTitle(title string) (releases []TitleRelease, ok bool) {
	m := sharedTitlePattern.FindStringSubmatch(title)
	if m == nil {
		return nil, false
	}
	for _, part := range strings.Split(m[1], ", ") {
		pm := sharedTitlePart.FindStringSubmatch(part)
		if pm == nil {
			return nil, false
		}
		releases = append(releases, TitleRelease{Name: pm[1], Version: pm[2]})
	}
	return releases, true
}

// planned is one component a merged shared release pull request tags and
// releases, at the version its title names.
type planned struct {
	comp       Component
	versionStr string
	ver        version.Semver
}

// RunLocked tags, releases and swaps the label of every merged,
// pending-labeled pull request on branch, the shared locked release
// branch, tagging and releasing every component in comps at the same
// version and merge commit: the release per component as Run already
// renders it, drawing each one's own slice of the combined changelog from
// the single pull request body. draftUntilAssetsAttach is release-me.yaml's
// "draft-until-assets-attach" key: false, the default, releases
// immediately, published, exactly as release-me did before that key
// existed; true drafts instead, and also resumes a release a prior,
// interrupted run already tagged and drafted but never got to publish.
func RunLocked(
	ctx context.Context, f forge.Forge, owner, repoName, branch string, comps []Component, labels Labels,
	draftUntilAssetsAttach, dryRun bool, logger *slog.Logger,
) ([]Result, error) {
	prs, err := f.PullRequests(ctx, owner, repoName, branch, "closed")
	if err != nil {
		return nil, fmt.Errorf("release: locked family: list pull requests on %s: %w", branch, err)
	}

	var results []Result
	for _, pr := range prs {
		if !pr.Merged {
			continue
		}

		if slices.Contains(pr.Labels, labels.Pending) {
			logger.Debug("locked release pull request merged", "number", pr.Number, "title", pr.Title)

			versionStr, _, ok := VersionFromLockedTitle(pr.Title)
			if !ok {
				return nil, fmt.Errorf("release: locked family: pull request #%d: title %q does not name a version", pr.Number, pr.Title)
			}
			ver, ok := version.ParseTag("v" + versionStr)
			if !ok {
				return nil, fmt.Errorf("release: locked family: pull request #%d: version %q does not parse", pr.Number, versionStr)
			}
			plan := make([]planned, 0, len(comps))
			for _, comp := range comps {
				plan = append(plan, planned{comp: comp, versionStr: versionStr, ver: ver})
			}

			res, err := releaseShared(ctx, f, owner, repoName, "locked family", pr, plan, labels, draftUntilAssetsAttach, dryRun)
			if err != nil {
				return nil, err
			}
			results = append(results, res...)
			continue
		}

		// Resuming an orphaned draft only ever applies when this
		// repository opted into drafting in the first place: off, a
		// release publishes the moment it is created, so a tagged pull
		// request never has anything left to resume.
		if dryRun || !draftUntilAssetsAttach || !slices.Contains(pr.Labels, labels.Tagged) {
			continue
		}
		versionStr, _, ok := VersionFromLockedTitle(pr.Title)
		if !ok {
			continue
		}
		plan := make([]planned, 0, len(comps))
		for _, comp := range comps {
			plan = append(plan, planned{comp: comp, versionStr: versionStr})
		}
		res, err := resumeShared(ctx, f, owner, repoName, "locked family", pr, plan)
		if err != nil {
			return nil, err
		}
		results = append(results, res...)
	}
	return results, nil
}

// RunShared tags, releases and swaps the label of every merged,
// pending-labeled pull request on branch, the release branch every
// component shares in VersioningIndependent or VersioningLinked with
// "release-pull-request: shared": every component the title SharedTitle
// rendered names, at its own version, all from the same merge commit, each
// with its own slice of the combined changelog, then the label once for
// the whole pull request. A component the title names that comps does not
// carry is an error: it was renamed or removed from release-me.yaml after
// the pull request was opened, and tagging it under a guessed prefix
// would be wrong. draftUntilAssetsAttach is as in RunLocked.
func RunShared(
	ctx context.Context, f forge.Forge, owner, repoName, branch string, comps []Component, labels Labels,
	draftUntilAssetsAttach, dryRun bool, logger *slog.Logger,
) ([]Result, error) {
	prs, err := f.PullRequests(ctx, owner, repoName, branch, "closed")
	if err != nil {
		return nil, fmt.Errorf("release: shared release pull request: list pull requests on %s: %w", branch, err)
	}

	var results []Result
	for _, pr := range prs {
		if !pr.Merged {
			continue
		}
		pending := slices.Contains(pr.Labels, labels.Pending)
		if !pending && (dryRun || !draftUntilAssetsAttach || !slices.Contains(pr.Labels, labels.Tagged)) {
			continue
		}

		titled, ok := ParseSharedTitle(pr.Title)
		if !ok {
			if !pending {
				continue
			}
			return nil, fmt.Errorf("release: shared release pull request #%d: title %q does not name a version per component", pr.Number, pr.Title)
		}
		plan := make([]planned, 0, len(titled))
		for _, t := range titled {
			i := slices.IndexFunc(comps, func(c Component) bool { return c.Name == t.Name })
			if i < 0 {
				return nil, fmt.Errorf("release: shared release pull request #%d: component %s is not configured", pr.Number, t.Name)
			}
			ver, ok := version.ParseTag("v" + t.Version)
			if !ok {
				return nil, fmt.Errorf("release: component %s: pull request #%d: version %q does not parse", t.Name, pr.Number, t.Version)
			}
			plan = append(plan, planned{comp: comps[i], versionStr: t.Version, ver: ver})
		}

		if !pending {
			res, err := resumeShared(ctx, f, owner, repoName, "shared release pull request", pr, plan)
			if err != nil {
				return nil, err
			}
			results = append(results, res...)
			continue
		}

		logger.Debug("shared release pull request merged", "number", pr.Number, "title", pr.Title)
		res, err := releaseShared(ctx, f, owner, repoName, "shared release pull request", pr, plan, labels, draftUntilAssetsAttach, dryRun)
		if err != nil {
			return nil, err
		}
		results = append(results, res...)
	}
	return results, nil
}

// resumeShared reprints the released-tag marker for every component in
// plan whose release is still a draft: pr already carries the Tagged
// label, so an earlier run tagged and drafted every component's release
// and swapped the label, but the asset attach or publish step that was
// meant to follow, in that same or a later workflow run, never finished for
// at least one of them. Finding that out needs no local git state and
// writes nothing: CreateTag and CreateRelease already ran, tolerating
// either one already existing, so calling them again would risk creating a
// second, duplicate draft for the same tag instead of finishing the first.
// who names the locked family or the shared release pull request in an
// error.
func resumeShared(
	ctx context.Context, f forge.Forge, owner, repoName, who string, pr forge.PullRequest, plan []planned,
) ([]Result, error) {
	var results []Result
	for _, p := range plan {
		tag := p.comp.TagPrefix + p.versionStr
		draft, err := f.IsReleaseDraft(ctx, owner, repoName, tag)
		if err != nil {
			if errors.Is(err, forge.ErrReleaseNotFound) {
				continue
			}
			return nil, fmt.Errorf("release: %s: component %s: pull request #%d: resume %s: %w", who, p.comp.Name, pr.Number, tag, err)
		}
		if !draft {
			continue
		}
		results = append(results, Result{
			PullRequestNumber: pr.Number, Name: p.comp.Name, Version: p.versionStr, Tag: tag,
			Message: fmt.Sprintf("resuming unpublished release %s", tag),
		})
	}
	return results, nil
}

// releaseShared tags and releases every component in plan, each at its own
// version, for one merged release pull request every component shares,
// locked or not, then swaps its label once for the whole pull request. who
// names the locked family or the shared release pull request in an error.
// draftUntilAssetsAttach decides whether CreateRelease drafts the release
// or publishes it immediately; see RunLocked.
func releaseShared(
	ctx context.Context, f forge.Forge, owner, repoName, who string, pr forge.PullRequest, plan []planned,
	labels Labels, draftUntilAssetsAttach, dryRun bool,
) ([]Result, error) {
	if dryRun {
		results := make([]Result, 0, len(plan))
		for _, p := range plan {
			comp, versionStr := p.comp, p.versionStr
			tag := comp.TagPrefix + versionStr
			result := Result{
				PullRequestNumber: pr.Number, Name: comp.Name, Version: versionStr, Tag: tag,
				Message: fmt.Sprintf("would tag and release %s", tag), HeadSHA: pr.HeadSHA,
			}
			mtag, mmsg, err := applyMajorTag(ctx, f, owner, repoName, comp.Name, comp.TagPrefix, versionStr, "", comp.MajorTag, dryRun)
			if err != nil {
				return nil, err
			}
			result.MajorTag, result.MajorTagMessage = mtag, mmsg
			results = append(results, result)
		}
		return results, nil
	}

	sha, err := f.MergeCommit(ctx, owner, repoName, pr.Number)
	if err != nil {
		return nil, fmt.Errorf("release: %s: pull request #%d: merge commit: %w", who, pr.Number, err)
	}

	results := make([]Result, 0, len(plan))
	remove := []string{labels.Pending}
	for _, p := range plan {
		comp, versionStr, ver := p.comp, p.versionStr, p.ver
		if preType, _, ok := ver.PreReleaseType(); ok {
			if typeLabel := labels.typeLabel(preType); typeLabel != "" && !slices.Contains(remove, typeLabel) {
				remove = append(remove, typeLabel)
			}
		}
		tag := comp.TagPrefix + versionStr
		name := tag
		if comp.Multi {
			name = fmt.Sprintf("%s v%s", comp.Name, versionStr)
		}

		alreadyTagged := false
		if err := f.CreateTag(ctx, owner, repoName, tag, sha); err != nil {
			if !errors.Is(err, forge.ErrTagExists) {
				return nil, fmt.Errorf("release: %s: component %s: pull request #%d: create tag %s: %w", who, comp.Name, pr.Number, tag, err)
			}
			alreadyTagged = true
		}

		// See releaseOne's matching comment: a draft release is invisible
		// to GitHub's own duplicate-tag_name check, so checking
		// IsReleaseDraft first closes the same race here, one step
		// before the label swap, that resumeShared closes one step
		// after it.
		alreadyDrafted := false
		if draftUntilAssetsAttach {
			draft, err := f.IsReleaseDraft(ctx, owner, repoName, tag)
			switch {
			case err == nil && draft:
				alreadyDrafted = true
			case errors.Is(err, forge.ErrReleaseNotFound):
				// The normal, first-time case: nothing has been
				// drafted yet, so CreateRelease below runs as usual.
			case err != nil:
				return nil, fmt.Errorf("release: %s: component %s: pull request #%d: check draft %s: %w", who, comp.Name, pr.Number, tag, err)
			}
		}

		if !alreadyDrafted {
			body := lockedReleaseBody(pr.Body, comp.Name)
			if err := f.CreateRelease(ctx, owner, repoName, tag, name, body, ver.IsPreRelease(), draftUntilAssetsAttach); err != nil &&
				!errors.Is(err, forge.ErrReleaseExists) {
				return nil, fmt.Errorf("release: %s: component %s: pull request #%d: create release %s: %w", who, comp.Name, pr.Number, tag, err)
			}
		}

		message := fmt.Sprintf("tagged and released %s", tag)
		if alreadyTagged {
			message = fmt.Sprintf("already tagged %s", tag)
		}

		mtag, mmsg, err := applyMajorTag(ctx, f, owner, repoName, comp.Name, comp.TagPrefix, versionStr, sha, comp.MajorTag, dryRun)
		if err != nil {
			return nil, err
		}
		results = append(results, Result{
			PullRequestNumber: pr.Number, Name: comp.Name, Version: versionStr, Tag: tag, Skipped: alreadyTagged,
			Message: message, MajorTag: mtag, MajorTagMessage: mmsg, HeadSHA: pr.HeadSHA,
		})
	}

	prLabels, err := f.Labels(ctx, owner, repoName, pr.Number)
	if err != nil {
		return nil, fmt.Errorf("release: %s: pull request #%d: labels: %w", who, pr.Number, err)
	}
	if err := f.SetLabels(ctx, owner, repoName, pr.Number, swapLabels(prLabels, remove, labels.Tagged)); err != nil {
		return nil, fmt.Errorf("release: %s: pull request #%d: set labels: %w", who, pr.Number, err)
	}
	return results, nil
}

// Component names the release branch, tag prefix and display name release
// Run tags, releases and swaps the label for, and whether release-me.yaml
// declares more than one component. Multi false, the implicit "main"
// component a repository without "components" carries, keeps today's bare
// tag prefix and release name instead of the generic "<name>/v" prefix and
// "<name> vX.Y.Z" release name a configured component carries.
type Component struct {
	Name      string
	Branch    string
	TagPrefix string
	Multi     bool
	// MajorTag is comp's resolved "major-tag" key: when true, a stable
	// release moves the major tag, TagPrefix plus the released major
	// number, to the release commit. A pre-release never moves it.
	MajorTag bool
}

// Labels names the labels Run reads and writes: Pending and Tagged on
// every release, plus the three pre-release type labels, so a pre-release
// pull request's type label swaps to Tagged alongside Pending, the same as
// Pending does.
type Labels struct {
	Pending string
	Tagged  string
	Alpha   string
	Beta    string
	Rc      string
}

// typeLabel returns the label naming preType, "" when preType is "" or
// names none of the three pre-release types.
func (l Labels) typeLabel(preType string) string {
	switch preType {
	case "alpha":
		return l.Alpha
	case "beta":
		return l.Beta
	case "rc":
		return l.Rc
	default:
		return ""
	}
}

// Result is the outcome for one merged release pull request.
type Result struct {
	PullRequestNumber int
	// Name is the component the result belongs to: a locked family
	// releases several components from the one pull request, so a
	// caller rendering one line per result needs this to label each,
	// rather than assuming results and comps stay in step index for
	// index, which a resumed release does not guarantee.
	Name    string
	Version string
	Tag     string
	Message string
	Skipped bool
	// MajorTag is the major tag name major-tag moved or created, empty
	// when major-tag is off for the component or the version released is
	// a pre-release.
	MajorTag string
	// MajorTagMessage reports what happened to the major tag, including
	// why it was skipped; empty when major-tag is off for the component.
	MajorTagMessage string
	// HeadSHA is the head commit the pull request merged with, the one
	// this run tagged and released, or would in a dry run: the caller
	// deleting the release branch afterwards checks the branch still
	// points there. Empty for a resumed release, which an earlier run
	// tagged, and when the forge reported no head commit.
	HeadSHA string
}

// MajorTagName returns the major tag name for tagPrefix and releasedVersion,
// an "X.Y.Z" version string without its "v" prefix, and whether major-tag
// applies to it: false for a pre-release version, such as "1.2.3-rc.1",
// which never moves the major tag.
func MajorTagName(tagPrefix, releasedVersion string) (tag string, applies bool) {
	v, ok := version.ParseTag("v" + releasedVersion)
	if !ok || v.IsPreRelease() {
		return "", false
	}
	return tagPrefix + strconv.Itoa(v.Major), true
}

// applyMajorTag creates or force-moves the major tag for comp, named by
// tagPrefix, onto sha, when majorTagEnabled and releasedVersion is not a
// pre-release. dryRun reads whether the tag exists, to report accurately
// what would happen, but writes nothing.
func applyMajorTag(
	ctx context.Context, f forge.Forge, owner, repoName, componentName, tagPrefix, releasedVersion, sha string,
	majorTagEnabled, dryRun bool,
) (tag, message string, err error) {
	if !majorTagEnabled {
		return "", "", nil
	}
	tag, applies := MajorTagName(tagPrefix, releasedVersion)
	if !applies {
		return "", "pre-release, major tag not moved", nil
	}

	exists, err := f.TagExists(ctx, owner, repoName, tag)
	if err != nil {
		return "", "", fmt.Errorf("release: component %s: major tag %s: %w", componentName, tag, err)
	}

	verb := "create"
	if exists {
		verb = "move"
	}
	if dryRun {
		return tag, fmt.Sprintf("would %s major tag %s", verb, tag), nil
	}

	if exists {
		if err := f.MoveTag(ctx, owner, repoName, tag, sha); err != nil {
			return "", "", fmt.Errorf("release: component %s: move major tag %s: %w", componentName, tag, err)
		}
		return tag, fmt.Sprintf("moved major tag %s", tag), nil
	}
	if err := f.CreateTag(ctx, owner, repoName, tag, sha); err != nil {
		return "", "", fmt.Errorf("release: component %s: create major tag %s: %w", componentName, tag, err)
	}
	return tag, fmt.Sprintf("created major tag %s", tag), nil
}

// Run tags, releases and swaps the label of every merged, pending-labeled
// pull request on comp's branch, for the component comp names. dryRun
// reports what would happen and calls neither CreateTag, CreateRelease nor
// SetLabels. draftUntilAssetsAttach is release-me.yaml's
// "draft-until-assets-attach" key: false, the default, releases
// immediately, published, exactly as release-me did before that key
// existed; true drafts instead, and also resumes a release a prior,
// interrupted run already tagged and drafted but never got to publish.
func Run(
	ctx context.Context, f forge.Forge, owner, repoName string, comp Component, labels Labels,
	draftUntilAssetsAttach, dryRun bool, logger *slog.Logger,
) ([]Result, error) {
	prs, err := f.PullRequests(ctx, owner, repoName, comp.Branch, "closed")
	if err != nil {
		return nil, fmt.Errorf("release: component %s: list pull requests on %s: %w", comp.Name, comp.Branch, err)
	}

	var results []Result
	for _, pr := range prs {
		if !pr.Merged {
			continue
		}

		if slices.Contains(pr.Labels, labels.Pending) {
			logger.Debug("release pull request merged", "component", comp.Name, "number", pr.Number, "title", pr.Title)

			versionStr, ok := VersionFromTitle(pr.Title, comp.Name)
			if !ok {
				return nil, fmt.Errorf("release: component %s: pull request #%d: title %q does not name a version", comp.Name, pr.Number, pr.Title)
			}
			ver, ok := version.ParseTag("v" + versionStr)
			if !ok {
				return nil, fmt.Errorf("release: component %s: pull request #%d: version %q does not parse", comp.Name, pr.Number, versionStr)
			}
			tag := comp.TagPrefix + versionStr
			name := tag
			if comp.Multi {
				name = fmt.Sprintf("%s v%s", comp.Name, versionStr)
			}

			result, err := releaseOne(ctx, f, owner, repoName, comp.Name, comp.TagPrefix, pr, tag, name, ver, labels, draftUntilAssetsAttach, dryRun, comp.MajorTag)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
			continue
		}

		// Resuming an orphaned draft only ever applies when this
		// repository opted into drafting in the first place: off, a
		// release publishes the moment it is created, so a tagged pull
		// request never has anything left to resume.
		if dryRun || !draftUntilAssetsAttach || !slices.Contains(pr.Labels, labels.Tagged) {
			continue
		}
		result, resumed, err := resumeOne(ctx, f, owner, repoName, comp.Name, comp.TagPrefix, pr)
		if err != nil {
			return nil, err
		}
		if resumed {
			results = append(results, result)
		}
	}
	return results, nil
}

// resumeOne reprints the released-tag marker for pr's release when it is
// still a draft: pr already carries the Tagged label, so an earlier run
// tagged and drafted the release and swapped the label, but the asset
// attach or publish step that was meant to follow, in that same or a later
// workflow run, never finished. ok is false when there is nothing to
// resume: the release is already published, or (defensively, since the
// Tagged label implies CreateRelease already succeeded once) does not
// exist at all. This writes nothing: CreateTag and CreateRelease already
// ran, tolerating either one already existing, so calling them again would
// risk creating a second, duplicate draft for the same tag instead of
// finishing the first.
func resumeOne(
	ctx context.Context, f forge.Forge, owner, repoName, componentName, tagPrefix string, pr forge.PullRequest,
) (result Result, ok bool, err error) {
	versionStr, titleOK := VersionFromTitle(pr.Title, componentName)
	if !titleOK {
		return Result{}, false, nil
	}
	tag := tagPrefix + versionStr

	draft, err := f.IsReleaseDraft(ctx, owner, repoName, tag)
	if err != nil {
		if errors.Is(err, forge.ErrReleaseNotFound) {
			return Result{}, false, nil
		}
		return Result{}, false, fmt.Errorf("release: component %s: pull request #%d: resume %s: %w", componentName, pr.Number, tag, err)
	}
	if !draft {
		return Result{}, false, nil
	}

	return Result{
		PullRequestNumber: pr.Number, Name: componentName, Version: versionStr, Tag: tag,
		Message: fmt.Sprintf("resuming unpublished release %s", tag),
	}, true, nil
}

// releaseOne tags and releases pr, swapping its label, for the component
// componentName names. draftUntilAssetsAttach decides whether
// CreateRelease drafts the release or publishes it immediately; see Run.
func releaseOne(
	ctx context.Context, f forge.Forge, owner, repoName, componentName, tagPrefix string,
	pr forge.PullRequest, tag, releaseName string, ver version.Semver, labels Labels, draftUntilAssetsAttach, dryRun, majorTagEnabled bool,
) (Result, error) {
	versionStr := ver.String()
	if dryRun {
		result := Result{
			PullRequestNumber: pr.Number, Name: componentName, Version: versionStr, Tag: tag,
			Message: fmt.Sprintf("would tag and release %s", tag), HeadSHA: pr.HeadSHA,
		}
		mtag, mmsg, err := applyMajorTag(ctx, f, owner, repoName, componentName, tagPrefix, versionStr, "", majorTagEnabled, dryRun)
		if err != nil {
			return Result{}, err
		}
		result.MajorTag, result.MajorTagMessage = mtag, mmsg
		return result, nil
	}

	sha, err := f.MergeCommit(ctx, owner, repoName, pr.Number)
	if err != nil {
		return Result{}, fmt.Errorf("release: component %s: pull request #%d: merge commit: %w", componentName, pr.Number, err)
	}

	alreadyTagged := false
	if err := f.CreateTag(ctx, owner, repoName, tag, sha); err != nil {
		if !errors.Is(err, forge.ErrTagExists) {
			return Result{}, fmt.Errorf("release: component %s: pull request #%d: create tag %s: %w", componentName, pr.Number, tag, err)
		}
		// The tag exists, so an earlier run got this far before failing or
		// being interrupted. Carry on: make sure the release exists and
		// the label still swaps, so the pull request does not stay
		// pending forever.
		alreadyTagged = true
	}

	// A draft release is invisible to GitHub's own duplicate-tag_name check
	// (see forge.Forge's CreateRelease and IsReleaseDraft doc comments), so
	// the ErrReleaseExists tolerance below only closes this hole once the
	// release is published. The gap it misses sits one step earlier: a
	// previous run can have drafted the release successfully and then
	// failed before the label swap below, leaving the pull request still
	// Pending. Re-entering here would call CreateRelease again and get a
	// second, orphaned draft at the same tag, since GitHub never objects.
	// Checking IsReleaseDraft first closes that gap the same way
	// resumeOne closes it for the step after the label swap: find the
	// draft that already exists and skip straight past it, instead of
	// creating another one.
	alreadyDrafted := false
	if draftUntilAssetsAttach {
		draft, err := f.IsReleaseDraft(ctx, owner, repoName, tag)
		switch {
		case err == nil && draft:
			alreadyDrafted = true
		case errors.Is(err, forge.ErrReleaseNotFound):
			// The normal, first-time case: nothing has been drafted yet,
			// so CreateRelease below runs as usual.
		case err != nil:
			return Result{}, fmt.Errorf("release: component %s: pull request #%d: check draft %s: %w", componentName, pr.Number, tag, err)
		}
	}

	if !alreadyDrafted {
		body := releasepr.ReleaseBody(pr.Body)
		if err := f.CreateRelease(ctx, owner, repoName, tag, releaseName, body, ver.IsPreRelease(), draftUntilAssetsAttach); err != nil &&
			!errors.Is(err, forge.ErrReleaseExists) {
			return Result{}, fmt.Errorf("release: component %s: pull request #%d: create release %s: %w", componentName, pr.Number, tag, err)
		}
	}

	prLabels, err := f.Labels(ctx, owner, repoName, pr.Number)
	if err != nil {
		return Result{}, fmt.Errorf("release: component %s: pull request #%d: labels: %w", componentName, pr.Number, err)
	}
	remove := []string{labels.Pending}
	if preType, _, ok := ver.PreReleaseType(); ok {
		if typeLabel := labels.typeLabel(preType); typeLabel != "" {
			remove = append(remove, typeLabel)
		}
	}
	if err := f.SetLabels(ctx, owner, repoName, pr.Number, swapLabels(prLabels, remove, labels.Tagged)); err != nil {
		return Result{}, fmt.Errorf("release: component %s: pull request #%d: set labels: %w", componentName, pr.Number, err)
	}

	message := fmt.Sprintf("tagged and released %s", tag)
	if alreadyTagged {
		message = fmt.Sprintf("already tagged %s", tag)
	}

	mtag, mmsg, err := applyMajorTag(ctx, f, owner, repoName, componentName, tagPrefix, versionStr, sha, majorTagEnabled, dryRun)
	if err != nil {
		return Result{}, err
	}

	return Result{
		PullRequestNumber: pr.Number, Name: componentName, Version: versionStr, Tag: tag, Skipped: alreadyTagged,
		Message: message, MajorTag: mtag, MajorTagMessage: mmsg, HeadSHA: pr.HeadSHA,
	}, nil
}

// swapLabels removes every label in remove from labels, when present, and
// appends add.
func swapLabels(labels, remove []string, add string) []string {
	out := make([]string, 0, len(labels)+1)
	for _, l := range labels {
		if slices.Contains(remove, l) {
			continue
		}
		out = append(out, l)
	}
	return append(out, add)
}
