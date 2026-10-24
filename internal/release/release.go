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

// lockedSectionHeading matches one component's heading inside a locked
// release pull request's combined changelog, "#### <name>" on its own
// line. The fourth level keeps it apart from the "### Features"-shaped
// section headings changelog.Sections renders inside each component's own
// slice.
var lockedSectionHeading = regexp.MustCompile(`(?m)^#### (.+)$`)

// componentSection returns name's own slice of changelogText, a locked
// release pull request's combined changelog: the text between its
// "#### <name>" heading and the next heading, or the end. "" when name
// carries no heading.
func componentSection(changelogText, name string) string {
	locs := lockedSectionHeading.FindAllStringSubmatchIndex(changelogText, -1)
	for i, loc := range locs {
		if changelogText[loc[2]:loc[3]] != name {
			continue
		}
		end := len(changelogText)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		return strings.TrimSpace(changelogText[loc[1]:end])
	}
	return ""
}

// lockedReleaseBody renders the GitHub release body for one component of a
// locked family run: the free text of the release-me-notes block, shared
// by the whole family, followed by name's own slice of the combined
// changelog.
func lockedReleaseBody(prBody, name string) string {
	var parts []string
	if notes, ok := releasepr.ExtractNotes(prBody); ok && notes != "" {
		parts = append(parts, notes)
	}
	if section := componentSection(releasepr.ExtractChangelog(prBody), name); section != "" {
		parts = append(parts, section)
	}
	return strings.Join(parts, "\n\n")
}

// RunLocked tags, releases and swaps the label of every merged,
// pending-labeled pull request on branch, the shared locked release
// branch, tagging and releasing every component in comps at the same
// version and merge commit: the release per component as Run already
// renders it, drawing each one's own slice of the combined changelog from
// the single pull request body.
func RunLocked(
	ctx context.Context, f forge.Forge, owner, repoName, branch string, comps []Component, labels Labels, dryRun bool, logger *slog.Logger,
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

			res, err := releaseLockedFamily(ctx, f, owner, repoName, pr, versionStr, ver, comps, labels, dryRun)
			if err != nil {
				return nil, err
			}
			results = append(results, res...)
			continue
		}

		if dryRun || !slices.Contains(pr.Labels, labels.Tagged) {
			continue
		}
		versionStr, _, ok := VersionFromLockedTitle(pr.Title)
		if !ok {
			continue
		}
		res, err := resumeLockedFamily(ctx, f, owner, repoName, pr, versionStr, comps)
		if err != nil {
			return nil, err
		}
		results = append(results, res...)
	}
	return results, nil
}

// resumeLockedFamily reprints the released-tag marker for every component
// of pr whose release is still a draft: pr already carries the Tagged
// label, so an earlier run tagged and drafted every component's release
// and swapped the label, but the asset attach or publish step that was
// meant to follow, in that same or a later workflow run, never finished for
// at least one of them. Finding that out needs no local git state and
// writes nothing: CreateTag and CreateRelease already ran, tolerating
// either one already existing, so calling them again would risk creating a
// second, duplicate draft for the same tag instead of finishing the first.
func resumeLockedFamily(
	ctx context.Context, f forge.Forge, owner, repoName string, pr forge.PullRequest, versionStr string, comps []Component,
) ([]Result, error) {
	var results []Result
	for _, comp := range comps {
		tag := comp.TagPrefix + versionStr
		draft, err := f.IsReleaseDraft(ctx, owner, repoName, tag)
		if err != nil {
			if errors.Is(err, forge.ErrReleaseNotFound) {
				continue
			}
			return nil, fmt.Errorf("release: locked family: component %s: pull request #%d: resume %s: %w", comp.Name, pr.Number, tag, err)
		}
		if !draft {
			continue
		}
		results = append(results, Result{
			PullRequestNumber: pr.Number, Name: comp.Name, Version: versionStr, Tag: tag,
			Message: fmt.Sprintf("resuming unpublished release %s", tag),
		})
	}
	return results, nil
}

// releaseLockedFamily tags and releases every component in comps, for one
// merged locked release pull request, then swaps its label once for the
// whole family.
func releaseLockedFamily(
	ctx context.Context, f forge.Forge, owner, repoName string, pr forge.PullRequest, versionStr string,
	ver version.Semver, comps []Component, labels Labels, dryRun bool,
) ([]Result, error) {
	if dryRun {
		results := make([]Result, 0, len(comps))
		for _, comp := range comps {
			tag := comp.TagPrefix + versionStr
			result := Result{
				PullRequestNumber: pr.Number, Name: comp.Name, Version: versionStr, Tag: tag,
				Message: fmt.Sprintf("would tag and release %s", tag),
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
		return nil, fmt.Errorf("release: locked family: pull request #%d: merge commit: %w", pr.Number, err)
	}

	results := make([]Result, 0, len(comps))
	for _, comp := range comps {
		tag := comp.TagPrefix + versionStr
		name := tag
		if comp.Multi {
			name = fmt.Sprintf("%s v%s", comp.Name, versionStr)
		}

		alreadyTagged := false
		if err := f.CreateTag(ctx, owner, repoName, tag, sha); err != nil {
			if !errors.Is(err, forge.ErrTagExists) {
				return nil, fmt.Errorf("release: locked family: component %s: pull request #%d: create tag %s: %w", comp.Name, pr.Number, tag, err)
			}
			alreadyTagged = true
		}

		body := lockedReleaseBody(pr.Body, comp.Name)
		if err := f.CreateRelease(ctx, owner, repoName, tag, name, body, ver.IsPreRelease()); err != nil && !errors.Is(err, forge.ErrReleaseExists) {
			return nil, fmt.Errorf("release: locked family: component %s: pull request #%d: create release %s: %w", comp.Name, pr.Number, tag, err)
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
			Message: message, MajorTag: mtag, MajorTagMessage: mmsg,
		})
	}

	prLabels, err := f.Labels(ctx, owner, repoName, pr.Number)
	if err != nil {
		return nil, fmt.Errorf("release: locked family: pull request #%d: labels: %w", pr.Number, err)
	}
	remove := []string{labels.Pending}
	if preType, _, ok := ver.PreReleaseType(); ok {
		if typeLabel := labels.typeLabel(preType); typeLabel != "" {
			remove = append(remove, typeLabel)
		}
	}
	if err := f.SetLabels(ctx, owner, repoName, pr.Number, swapLabels(prLabels, remove, labels.Tagged)); err != nil {
		return nil, fmt.Errorf("release: locked family: pull request #%d: set labels: %w", pr.Number, err)
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
// SetLabels.
func Run(
	ctx context.Context, f forge.Forge, owner, repoName string, comp Component, labels Labels, dryRun bool, logger *slog.Logger,
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

			result, err := releaseOne(ctx, f, owner, repoName, comp.Name, comp.TagPrefix, pr, tag, name, ver, labels, dryRun, comp.MajorTag)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
			continue
		}

		if dryRun || !slices.Contains(pr.Labels, labels.Tagged) {
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

func releaseOne(
	ctx context.Context, f forge.Forge, owner, repoName, componentName, tagPrefix string,
	pr forge.PullRequest, tag, releaseName string, ver version.Semver, labels Labels, dryRun, majorTagEnabled bool,
) (Result, error) {
	versionStr := ver.String()
	if dryRun {
		result := Result{
			PullRequestNumber: pr.Number, Name: componentName, Version: versionStr, Tag: tag,
			Message: fmt.Sprintf("would tag and release %s", tag),
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

	body := releasepr.ReleaseBody(pr.Body)
	if err := f.CreateRelease(ctx, owner, repoName, tag, releaseName, body, ver.IsPreRelease()); err != nil && !errors.Is(err, forge.ErrReleaseExists) {
		return Result{}, fmt.Errorf("release: component %s: pull request #%d: create release %s: %w", componentName, pr.Number, tag, err)
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
		Message: message, MajorTag: mtag, MajorTagMessage: mmsg,
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
