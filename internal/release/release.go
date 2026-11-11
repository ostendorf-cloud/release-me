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
	"slices"
	"strconv"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// VersionFromTitle extracts "X.Y.Z", or "X.Y.Z-<pre>" for a pre-release,
// from a release pull request title in the default per-component shape,
// "chore(<name>): release vX.Y.Z", whose component name matches name. ok is
// false when title does not match that shape, or names a different
// component.
func VersionFromTitle(title, name string) (versionStr string, ok bool) {
	return componentVersion(nil, title, name)
}

// componentVersion reads the version back from title with the first of
// patterns it matches, the default per-component pattern when patterns is
// empty, and only from a title naming name: every per-component pattern
// carries ${component}.
func componentVersion(patterns []releasepr.TitlePattern, title, name string) (versionStr string, ok bool) {
	if len(patterns) == 0 {
		patterns = []releasepr.TitlePattern{releasepr.DefaultTitle(releasepr.TitlePerComponent)}
	}
	for _, p := range patterns {
		v, ok := p.Parse(title)
		if !ok || v.Component != name {
			continue
		}
		return v.Version, true
	}
	return "", false
}

// LockedTitle renders the release pull request and release title for a
// locked family run in the default shape: the version, then every
// component it covers, in release-me.yaml's order, so the title names both
// even though it always covers the whole family.
func LockedTitle(versionStr string, names []string) string {
	return fmt.Sprintf("chore: release v%s (%s)", versionStr, strings.Join(names, ", "))
}

// VersionFromLockedTitle extracts the version and the component names from
// a locked release pull request title in LockedTitle's default shape. ok is
// false when title does not match that shape.
func VersionFromLockedTitle(title string) (versionStr string, names []string, ok bool) {
	v, ok := releasepr.DefaultTitle(releasepr.TitleLocked).Parse(title)
	if !ok {
		return "", nil, false
	}
	return v.Version, v.Components, true
}

// lockedVersion reads a locked family's version back from title with the
// first of patterns it matches, the default locked pattern when patterns
// is empty.
func lockedVersion(patterns []releasepr.TitlePattern, title string) (versionStr string, ok bool) {
	if len(patterns) == 0 {
		patterns = []releasepr.TitlePattern{releasepr.DefaultTitle(releasepr.TitleLocked)}
	}
	v, _, ok := releasepr.ParseAny(patterns, title)
	return v.Version, ok
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
type TitleRelease = releasepr.TitleRelease

// SharedTitle renders the release pull request title for a shared release
// pull request in VersioningIndependent or VersioningLinked in the default
// shape: every component it releases, in release-me.yaml's order, each at
// its own version, "chore: release api v1.3.0, web v0.4.1". The title is
// where release reads every component's version back from after the
// merge, the same as it does for one component's own title.
func SharedTitle(releases []TitleRelease) string {
	parts := make([]string, 0, len(releases))
	for _, r := range releases {
		parts = append(parts, fmt.Sprintf("%s v%s", r.Name, r.Version))
	}
	return "chore: release " + strings.Join(parts, ", ")
}

// ParseSharedTitle extracts every component and its version from a title
// in SharedTitle's default shape. ok is false when title does not match
// that shape.
func ParseSharedTitle(title string) (releases []TitleRelease, ok bool) {
	return sharedReleases(nil, title)
}

// sharedReleases reads every component and its version back from title
// with the first of patterns it matches, the default shared pattern when
// patterns is empty.
func sharedReleases(patterns []releasepr.TitlePattern, title string) (releases []TitleRelease, ok bool) {
	if len(patterns) == 0 {
		patterns = []releasepr.TitlePattern{releasepr.DefaultTitle(releasepr.TitleShared)}
	}
	v, _, ok := releasepr.ParseAny(patterns, title)
	return v.Releases, ok
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
// "github-release-draft-until-assets-attach" key: false, the default, releases
// immediately, published, exactly as release-me did before that key
// existed; true drafts instead, and also resumes a release a prior,
// interrupted run already tagged and drafted but never got to publish.
func RunLocked(
	ctx context.Context, f forge.Forge, owner, repoName, branch string, comps []Component, titles []releasepr.TitlePattern, labels Labels,
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

		if HasAll(pr.Labels, labels.Pending) {
			logger.Debug("locked release pull request merged", "number", pr.Number, "title", pr.Title)

			versionStr, ok := lockedVersion(releasepr.ReadPatterns(titles, pr.Body, releasepr.TitleLocked), pr.Title)
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
		if dryRun || !draftUntilAssetsAttach || !HasAll(pr.Labels, labels.Tagged) {
			continue
		}
		versionStr, ok := lockedVersion(releasepr.ReadPatterns(titles, pr.Body, releasepr.TitleLocked), pr.Title)
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
	ctx context.Context, f forge.Forge, owner, repoName, branch string, comps []Component, titles []releasepr.TitlePattern, labels Labels,
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
		pending := HasAll(pr.Labels, labels.Pending)
		if !pending && (dryRun || !draftUntilAssetsAttach || !HasAll(pr.Labels, labels.Tagged)) {
			continue
		}

		titled, ok := sharedReleases(releasepr.ReadPatterns(titles, pr.Body, releasepr.TitleShared), pr.Title)
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
	sha := ""
	for _, p := range plan {
		// A component with its own "github-release-draft" key drafts on
		// purpose: that draft waits for a human, so it is no interrupted
		// run and never resumes, whatever
		// github-release-draft-until-assets-attach is set to.
		if p.comp.SkipGitHubRelease || p.comp.Draft {
			continue
		}
		tag := p.comp.TagPrefix + p.versionStr
		rel, err := f.Release(ctx, owner, repoName, tag)
		if err != nil {
			if errors.Is(err, forge.ErrReleaseNotFound) {
				continue
			}
			return nil, fmt.Errorf("release: %s: component %s: pull request #%d: resume %s: %w", who, p.comp.Name, pr.Number, tag, err)
		}
		if !rel.Draft {
			continue
		}
		if sha == "" {
			if sha, err = f.MergeCommit(ctx, owner, repoName, pr.Number); err != nil {
				return nil, fmt.Errorf("release: %s: pull request #%d: merge commit: %w", who, pr.Number, err)
			}
		}
		results = append(results, Result{
			PullRequestNumber: pr.Number, Name: p.comp.Name, Version: p.versionStr, Tag: tag,
			Message: fmt.Sprintf("resuming unpublished release %s", tag),
			SHA:     sha, Body: lockedReleaseBody(pr.Body, p.comp.Name), HTMLURL: rel.HTMLURL, UploadURL: rel.UploadURL, Draft: rel.Draft,
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
				Message: dryRunMessage(comp, tag, comp.draft(draftUntilAssetsAttach)), HeadSHA: pr.HeadSHA,
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
	remove := slices.Clone(labels.Pending)
	for _, p := range plan {
		comp, versionStr, ver := p.comp, p.versionStr, p.ver
		if preType, _, ok := ver.PreReleaseType(); ok {
			if typeLabel := labels.PreRelease[preType]; typeLabel != "" && !slices.Contains(remove, typeLabel) {
				remove = append(remove, typeLabel)
			}
		}
		tag := comp.TagPrefix + versionStr
		name := comp.releaseName(versionStr)

		target, alreadyTagged, err := createTag(ctx, f, owner, repoName, comp, tag, sha)
		if err != nil {
			return nil, fmt.Errorf("release: %s: component %s: pull request #%d: %w", who, comp.Name, pr.Number, err)
		}

		body := lockedReleaseBody(pr.Body, comp.Name)
		var rel forge.Release
		if !comp.SkipGitHubRelease {
			rel, err = createRelease(ctx, f, owner, repoName, tag, target, name, body, ver.IsPreRelease(), comp.draft(draftUntilAssetsAttach))
			if err != nil {
				return nil, fmt.Errorf("release: %s: component %s: pull request #%d: %w", who, comp.Name, pr.Number, err)
			}
		}

		message := releasedMessage(comp, tag, alreadyTagged, comp.draft(draftUntilAssetsAttach))

		mtag, mmsg, err := applyMajorTag(ctx, f, owner, repoName, comp.Name, comp.TagPrefix, versionStr, sha, comp.MajorTag, dryRun)
		if err != nil {
			return nil, err
		}
		results = append(results, Result{
			PullRequestNumber: pr.Number, Name: comp.Name, Version: versionStr, Tag: tag, Skipped: alreadyTagged,
			Message: message, MajorTag: mtag, MajorTagMessage: mmsg, HeadSHA: pr.HeadSHA,
			SHA: sha, Body: body, HTMLURL: rel.HTMLURL, UploadURL: rel.UploadURL, Draft: rel.Draft,
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
// "<name> vX.Y.Z" release name a configured component carries. Both
// names drop the "v" under OmitVInReleaseName.
type Component struct {
	Name      string
	Branch    string
	TagPrefix string
	Multi     bool
	// MajorTag is comp's resolved "major-tag" key: when true, a stable
	// release moves the major tag, TagPrefix plus the released major
	// number, to the release commit. A pre-release never moves it.
	MajorTag bool
	// Titles are the configured patterns Run reads the version back from the
	// title of the component's own release pull request with, in order, see
	// releasepr.TitlePatterns, before the pattern the pull request body
	// records and the default, see releasepr.ReadPatterns.
	Titles []releasepr.TitlePattern
	// TagOnPublish is comp's resolved "force-tag-creation" key turned
	// off: release creates no tag itself but hands the release the merge
	// commit, and the forge creates the tag on it once the release is
	// published, right away for a published release, on publish for a
	// draft. False, the zero value, keeps release-me's own tag before the
	// release.
	TagOnPublish bool
	// SkipGitHubRelease is comp's resolved "skip-github-release" key: when
	// true, a merged release pull request still gets its tag, major tag
	// and label swap, but no GitHub release.
	SkipGitHubRelease bool
	// OmitVInReleaseName is comp's resolved "include-v-in-release-name"
	// key turned off: the release is named "X.Y.Z", or "<name> X.Y.Z",
	// without the "v". False, the zero value, keeps the "v".
	OmitVInReleaseName bool
	// Draft is comp's resolved "github-release-draft" key: when true, the
	// GitHub release is created as a draft, for a human to publish. Unlike
	// github-release-draft-until-assets-attach it never resumes one: a
	// draft left on purpose is no interrupted run.
	Draft bool
}

// draft reports whether comp's release is created as a draft: comp's own
// "github-release-draft" key, or draftUntilAssetsAttach for the whole repository.
func (comp Component) draft(draftUntilAssetsAttach bool) bool {
	return comp.Draft || draftUntilAssetsAttach
}

// releaseName returns the name of comp's release at versionStr:
// the tag itself for the implicit "main" component,
// "<name> vX.Y.Z" for a configured one, each without the "v" under
// OmitVInReleaseName.
func (comp Component) releaseName(versionStr string) string {
	switch {
	case comp.Multi && comp.OmitVInReleaseName:
		return fmt.Sprintf("%s %s", comp.Name, versionStr)
	case comp.Multi:
		return fmt.Sprintf("%s v%s", comp.Name, versionStr)
	case comp.OmitVInReleaseName:
		// The implicit main component always has tag prefix "v".
		return versionStr
	default:
		return comp.TagPrefix + versionStr
	}
}

// Labels names the labels Run reads and writes: Pending and Tagged on
// every release, plus every pre-release type's label, so a pre-release
// pull request's type label swaps to Tagged alongside Pending, the same as
// Pending does.
type Labels struct {
	// Pending is every label a merged release pull request carries while
	// it waits for its tag: it is pending only while it carries all of
	// them.
	Pending []string
	// Tagged is every label releasing a pull request adds in place of the
	// pending ones; it is tagged only while it carries all of them.
	Tagged []string
	// PreRelease maps every configured pre-release type, alpha, beta, rc
	// and any further one, to its label; a type turned off carries none.
	PreRelease map[string]string
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
	// SHA is the merge commit the tag is on.
	SHA string
	// Body is the release notes rendered from the pull request for the
	// component, including when an existing release is read back. Edits
	// made directly to the GitHub release are not included.
	Body string
	// HTMLURL and UploadURL are the GitHub release's web page and asset
	// upload address, empty when an earlier run's release exists but its
	// lookup found none, or when the component skips its GitHub release.
	HTMLURL   string
	UploadURL string
	// Draft is true while the release is still an unpublished draft.
	Draft bool
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
//
// It runs for a drafted release too, which is deliberate and a known
// limitation in one combination. With force-tag-creation on, the default,
// the version tag is already on the merge commit by the time the major tag
// moves there, draft or not, so the two agree. With force-tag-creation off
// and the release drafted, the version tag does not exist yet, so the
// major tag points at a version that is not published. Deferring the move
// instead would be worse: nothing in release-me runs after a human
// publishes a draft, the resume paths only ever see a release that is
// still a draft, so a deferred major tag would never be created at all and
// every consumer pinned to it would silently stay on the previous release
// forever. Moving it early is visible, but it is not corrected on its own
// under a standing draft: true, since nothing ever resumes a deliberate
// draft: every release drafts again, so the major tag stays ahead of the
// last existing version tag for as long as draft stays on. See
// docs/configuration.md's major-tag key.
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
// "github-release-draft-until-assets-attach" key: false, the default, releases
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

		if HasAll(pr.Labels, labels.Pending) {
			logger.Debug("release pull request merged", "component", comp.Name, "number", pr.Number, "title", pr.Title)

			versionStr, ok := componentVersion(releasepr.ReadPatterns(comp.Titles, pr.Body, releasepr.TitlePerComponent), pr.Title, comp.Name)
			if !ok {
				return nil, fmt.Errorf("release: component %s: pull request #%d: title %q does not name a version", comp.Name, pr.Number, pr.Title)
			}
			ver, ok := version.ParseTag("v" + versionStr)
			if !ok {
				return nil, fmt.Errorf("release: component %s: pull request #%d: version %q does not parse", comp.Name, pr.Number, versionStr)
			}
			result, err := releaseOne(ctx, f, owner, repoName, comp, pr, ver, labels, draftUntilAssetsAttach, dryRun)
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
		if dryRun || !draftUntilAssetsAttach || !HasAll(pr.Labels, labels.Tagged) {
			continue
		}
		// A component with its own "github-release-draft" key drafts on
		// purpose: that draft waits for a human, so it is no interrupted
		// run and never resumes, whatever
		// github-release-draft-until-assets-attach is set to.
		if comp.SkipGitHubRelease || comp.Draft {
			continue
		}
		result, resumed, err := resumeOne(ctx, f, owner, repoName, comp, pr)
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
	ctx context.Context, f forge.Forge, owner, repoName string, comp Component, pr forge.PullRequest,
) (result Result, ok bool, err error) {
	componentName, tagPrefix := comp.Name, comp.TagPrefix
	versionStr, titleOK := componentVersion(releasepr.ReadPatterns(comp.Titles, pr.Body, releasepr.TitlePerComponent), pr.Title, componentName)
	if !titleOK {
		return Result{}, false, nil
	}
	tag := tagPrefix + versionStr

	rel, err := f.Release(ctx, owner, repoName, tag)
	if err != nil {
		if errors.Is(err, forge.ErrReleaseNotFound) {
			return Result{}, false, nil
		}
		return Result{}, false, fmt.Errorf("release: component %s: pull request #%d: resume %s: %w", componentName, pr.Number, tag, err)
	}
	if !rel.Draft {
		return Result{}, false, nil
	}
	sha, err := f.MergeCommit(ctx, owner, repoName, pr.Number)
	if err != nil {
		return Result{}, false, fmt.Errorf("release: component %s: pull request #%d: merge commit: %w", componentName, pr.Number, err)
	}

	return Result{
		PullRequestNumber: pr.Number, Name: componentName, Version: versionStr, Tag: tag,
		Message: fmt.Sprintf("resuming unpublished release %s", tag),
		SHA:     sha, Body: releasepr.ReleaseBody(pr.Body), HTMLURL: rel.HTMLURL, UploadURL: rel.UploadURL, Draft: rel.Draft,
	}, true, nil
}

// releaseOne tags and releases pr, swapping its label, for comp at ver.
// draftUntilAssetsAttach decides whether CreateRelease drafts the release
// or publishes it immediately; see Run.
func releaseOne(
	ctx context.Context, f forge.Forge, owner, repoName string, comp Component,
	pr forge.PullRequest, ver version.Semver, labels Labels, draftUntilAssetsAttach, dryRun bool,
) (Result, error) {
	componentName, tagPrefix, majorTagEnabled := comp.Name, comp.TagPrefix, comp.MajorTag
	// Single-component titles are normalized through Semver before the
	// tag is built; release-me itself always renders canonical versions.
	versionStr := ver.String()
	tag := tagPrefix + versionStr
	releaseName := comp.releaseName(versionStr)
	if dryRun {
		result := Result{
			PullRequestNumber: pr.Number, Name: componentName, Version: versionStr, Tag: tag,
			Message: dryRunMessage(comp, tag, comp.draft(draftUntilAssetsAttach)), HeadSHA: pr.HeadSHA,
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

	target, alreadyTagged, err := createTag(ctx, f, owner, repoName, comp, tag, sha)
	if err != nil {
		return Result{}, fmt.Errorf("release: component %s: pull request #%d: %w", componentName, pr.Number, err)
	}

	body := releasepr.ReleaseBody(pr.Body)
	var rel forge.Release
	if !comp.SkipGitHubRelease {
		rel, err = createRelease(ctx, f, owner, repoName, tag, target, releaseName, body, ver.IsPreRelease(), comp.draft(draftUntilAssetsAttach))
		if err != nil {
			return Result{}, fmt.Errorf("release: component %s: pull request #%d: %w", componentName, pr.Number, err)
		}
	}

	prLabels, err := f.Labels(ctx, owner, repoName, pr.Number)
	if err != nil {
		return Result{}, fmt.Errorf("release: component %s: pull request #%d: labels: %w", componentName, pr.Number, err)
	}
	remove := slices.Clone(labels.Pending)
	if preType, _, ok := ver.PreReleaseType(); ok {
		if typeLabel := labels.PreRelease[preType]; typeLabel != "" {
			remove = append(remove, typeLabel)
		}
	}
	if err := f.SetLabels(ctx, owner, repoName, pr.Number, swapLabels(prLabels, remove, labels.Tagged)); err != nil {
		return Result{}, fmt.Errorf("release: component %s: pull request #%d: set labels: %w", componentName, pr.Number, err)
	}

	message := releasedMessage(comp, tag, alreadyTagged, comp.draft(draftUntilAssetsAttach))

	mtag, mmsg, err := applyMajorTag(ctx, f, owner, repoName, componentName, tagPrefix, versionStr, sha, majorTagEnabled, dryRun)
	if err != nil {
		return Result{}, err
	}

	return Result{
		PullRequestNumber: pr.Number, Name: componentName, Version: versionStr, Tag: tag, Skipped: alreadyTagged,
		Message: message, MajorTag: mtag, MajorTagMessage: mmsg, HeadSHA: pr.HeadSHA,
		SHA: sha, Body: body, HTMLURL: rel.HTMLURL, UploadURL: rel.UploadURL, Draft: rel.Draft,
	}, nil
}

// releasedMessage reports what release did for comp at tag, its release a
// draft when draft is true: already tagged by an earlier run, tagged
// without a GitHub release, drafted with the tag left to its publish,
// tagged with a release left a draft on purpose for a human, or tagged
// and released. Only comp's own TagOnPublish and Draft change the
// message; a temporary draft from github-release-draft-until-assets-attach
// alone keeps the plain tagged and released wording.
func releasedMessage(comp Component, tag string, alreadyTagged, draft bool) string {
	switch {
	case alreadyTagged:
		return fmt.Sprintf("already tagged %s", tag)
	case comp.SkipGitHubRelease:
		return fmt.Sprintf("tagged %s, GitHub release skipped", tag)
	case comp.TagOnPublish && draft:
		return fmt.Sprintf("drafted %s, tagged on publish", tag)
	case comp.Draft:
		return fmt.Sprintf("tagged and drafted %s", tag)
	default:
		return fmt.Sprintf("tagged and released %s", tag)
	}
}

// dryRunMessage reports what release would do for comp at tag, its release
// a draft when draft is true. Like releasedMessage, it calls out comp's
// own TagOnPublish and Draft only.
func dryRunMessage(comp Component, tag string, draft bool) string {
	switch {
	case comp.SkipGitHubRelease:
		return fmt.Sprintf("would tag %s, GitHub release skipped", tag)
	case comp.TagOnPublish && draft:
		return fmt.Sprintf("would draft %s, tagged on publish", tag)
	case comp.Draft:
		return fmt.Sprintf("would tag and draft %s", tag)
	default:
		return fmt.Sprintf("would tag and release %s", tag)
	}
}

// createTag creates tag on sha for comp, unless comp leaves its tag to the
// release: then target is sha, for createRelease to hand the forge, which
// creates the tag once the release is published. alreadyTagged is true
// when the tag exists already: an earlier run got this far before failing
// or being interrupted, and the caller carries on, so the release exists
// and the label still swaps instead of the pull request staying pending
// forever. The returned error names the step, for the caller to wrap with
// the component and the pull request.
func createTag(
	ctx context.Context, f forge.Forge, owner, repoName string, comp Component, tag, sha string,
) (target string, alreadyTagged bool, err error) {
	if comp.TagOnPublish {
		return sha, false, nil
	}
	if err := f.CreateTag(ctx, owner, repoName, tag, sha); err != nil {
		if !errors.Is(err, forge.ErrTagExists) {
			return "", false, fmt.Errorf("create tag %s: %w", tag, err)
		}
		return "", true, nil
	}
	return "", false, nil
}

// createRelease creates the release for tag, named name with body, a
// pre-release when prerelease is true, a draft when draft is true, the
// tag created on target once published when createTag left it. A
// release that already exists is not an error: an earlier, interrupted run
// created it, and it is read back for its addresses instead. It returns the
// release, created or found. The returned error names the step, for the
// caller to wrap with the component and the pull request.
//
// A draft release is invisible to GitHub's own duplicate-tag_name check
// (see forge.Forge's CreateRelease and Release doc comments), so the
// ErrReleaseExists tolerance only closes this hole once the release is
// published. The gap it misses sits one step earlier: a previous run can
// have drafted the release successfully and then failed before the label
// swap, leaving the pull request still pending. Re-entering would call
// CreateRelease again and get a second, orphaned draft at the same tag,
// since GitHub never objects. Reading the release first closes that gap
// the same way resumeOne and resumeShared close it for the step after the
// label swap: find the draft that already exists and skip straight past
// it, instead of creating another one.
func createRelease(
	ctx context.Context, f forge.Forge, owner, repoName, tag, target, name, body string, prerelease, draft bool,
) (forge.Release, error) {
	if draft {
		existing, err := f.Release(ctx, owner, repoName, tag)
		switch {
		case err == nil && existing.Draft:
			return existing, nil
		case errors.Is(err, forge.ErrReleaseNotFound):
			// The normal, first-time case: nothing has been drafted yet,
			// so CreateRelease below runs as usual.
		case err != nil:
			return forge.Release{}, fmt.Errorf("check draft %s: %w", tag, err)
		}
	}
	created, err := f.CreateRelease(ctx, owner, repoName, tag, target, name, body, prerelease, draft)
	switch {
	case err == nil:
		return created, nil
	case !errors.Is(err, forge.ErrReleaseExists):
		return forge.Release{}, fmt.Errorf("create release %s: %w", tag, err)
	}
	existing, err := f.Release(ctx, owner, repoName, tag)
	if err != nil && !errors.Is(err, forge.ErrReleaseNotFound) {
		return forge.Release{}, fmt.Errorf("read release %s: %w", tag, err)
	}
	return existing, nil
}

// swapLabels removes every label in remove from labels, when present, and
// appends every label in add it does not carry yet.
func swapLabels(labels, remove, add []string) []string {
	out := make([]string, 0, len(labels)+len(add))
	for _, l := range labels {
		if slices.Contains(remove, l) || slices.Contains(add, l) {
			continue
		}
		out = append(out, l)
	}
	return append(out, add...)
}

// HasAll reports whether labels carries every label in want: how a release
// pull request is recognized as pending or tagged when either role names
// several labels.
func HasAll(labels, want []string) bool {
	for _, w := range want {
		if !slices.Contains(labels, w) {
			return false
		}
	}
	return len(want) > 0
}
