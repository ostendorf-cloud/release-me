// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/history"
	tagrelease "github.com/ostendorf-cloud/release-me/internal/release"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
	"github.com/ostendorf-cloud/release-me/internal/updater"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// noUserFacingChangesLine is rendered under the changelog heading when a
// release has no visible entry, every one of them a hidden type, so the
// version bump stays visible instead of leaving a bare heading.
const noUserFacingChangesLine = "- Version bump without user facing changes."

// compareRef is the local ref the release branch's current remote tip is
// fetched into, so its content can be read with "git show" without
// disturbing any real remote-tracking ref.
const compareRef = "refs/release-me/compare"

// runPR implements the pr command: it creates or updates the release pull
// request for every configured component or the one named with
// --component.
func runPR(args []string, stdout, stderr io.Writer) int {
	verbose, dryRun, component, ok := parseWriteFlags("pr", args, stderr)
	if !ok {
		return 2
	}

	out := &runOutputs{}
	ctx := withOutputs(context.Background(), out)
	dir, cfg, f, logger, err := setup(ctx, stderr, verbose)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if err := rejectComponentForLocked(cfg, component); err != nil {
		fmt.Fprintf(stderr, "release-me: %v\n", err)
		return 2
	}
	if err := rejectComponentForShared(cfg, component); err != nil {
		fmt.Fprintf(stderr, "release-me: %v\n", err)
		return 2
	}

	components, err := resolveComponents(cfg, component)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	return finishWrite(stderr, components, out, doPR(ctx, dir, cfg, component, f, logger, dryRun, stdout))
}

// doPR creates or updates the release pull request for every component
// component resolves to. The pending and tagged labels and the base
// branch are checked once, shared by every component; a failure there
// stops before any component writes anything.
func doPR(ctx context.Context, dir string, cfg config.Config, component string, f forge.Forge, logger *slog.Logger, dryRun bool, stdout io.Writer) error {
	warnIgnoredAuthor(cfg, logger)
	owner, repoName, err := history.Remote(ctx, dir)
	if err != nil {
		return fmt.Errorf("release-me: origin remote: %w", err)
	}

	components, err := resolveComponents(cfg, component)
	if err != nil {
		return err
	}
	multi := len(components) > 1

	// The pending and tagged labels must exist before anything is written,
	// the same check doCheck runs.
	missing, unauthorized, err := missingLabels(ctx, f, owner, repoName, cfg)
	if err != nil {
		if unauthorized {
			return fmt.Errorf("release-me: token rejected by the forge: %w", err)
		}
		return fmt.Errorf("release-me: check labels: %w", err)
	}
	if len(missing) > 0 {
		return fmt.Errorf("release-me: label(s) do not exist on the repository: %s", strings.Join(missing, ", "))
	}

	base, err := resolveBaseBranch(ctx, f, dir, owner, repoName)
	if err != nil {
		return err
	}

	switch {
	case cfg.Versioning == config.VersioningLocked:
		return doPRLocked(ctx, dir, cfg, owner, repoName, base, f, logger, dryRun, stdout)
	case sharedOutsideLocked(cfg):
		return doPRShared(ctx, dir, cfg, owner, repoName, base, f, logger, dryRun, stdout)
	case cfg.Versioning == config.VersioningLinked:
		return doPRLinked(ctx, dir, cfg, components, owner, repoName, base, f, logger, dryRun, multi, stdout)
	default:
		// Every release is computed before the first pull request is
		// written, so the reads computeReleases caches never span a write.
		results, err := computeReleases(ctx, dir, cfg, components, owner, repoName, f, logger)
		if err != nil {
			return err
		}
		for _, r := range results {
			if !r.released {
				printLine(stdout, r.comp.Name, multi, "nothing to release")
				continue
			}
			if err := doPRComponent(ctx, dir, cfg, r.comp, []string{r.comp.TagPrefix}, r.rel, owner, repoName, base, f, logger, dryRun, multi, stdout); err != nil {
				return err
			}
		}
		return nil
	}
}

// doPRLinked creates or updates the release pull request for every
// component that has its own entries since its own last tag, at the
// family's shared version: the highest version any component in the
// family carries, bumped by the highest bump type among the changed
// components. An unchanged component is left untouched, same as
// VersioningIndependent reports "nothing to release" for one.
func doPRLinked(
	ctx context.Context, dir string, cfg config.Config, components []config.ResolvedComponent, owner, repoName, base string,
	f forge.Forge, logger *slog.Logger, dryRun, multi bool, stdout io.Writer,
) error {
	members, err := gatherFamily(ctx, dir, cfg, owner, repoName, f, logger)
	if err != nil {
		return err
	}
	releases, released := familyReleases(cfg, members, logger)
	prefixes := familyTagPrefixes(members)

	for _, comp := range components {
		rel, ok := releases[comp.Name]
		if !released || !ok {
			printLine(stdout, comp.Name, multi, "nothing to release")
			continue
		}
		if err := doPRComponent(ctx, dir, cfg, comp, prefixes, rel, owner, repoName, base, f, logger, dryRun, multi, stdout); err != nil {
			return err
		}
	}
	return nil
}

// doPRComponent creates or updates the release pull request for one
// component. Idempotency is judged on content, not on the rendered title
// and body: the changelog heading's version, its entries (without the
// heading's date) and the bumped files are compared against the release
// branch's current tip. Equal content changes nothing, not even the pull
// request body, unless the notes block itself changed, which updates the
// body alone, no push.
func doPRComponent(
	ctx context.Context, dir string, cfg config.Config, comp config.ResolvedComponent, familyTagPrefixes []string, rel release,
	owner, repoName, base string, f forge.Forge, logger *slog.Logger, dryRun, multi bool, stdout io.Writer,
) error {
	existingList, err := f.PullRequests(ctx, owner, repoName, comp.Branch, "open")
	if err != nil {
		return fmt.Errorf("release-me: component %s: list release pull requests: %w", comp.Name, err)
	}
	var existing *forge.PullRequest
	if len(existingList) > 0 {
		existing = &existingList[0]
		if len(existingList) > 1 {
			logger.Warn("more than one open release pull request, using the first",
				"component", comp.Name, "head", comp.Branch, "count", len(existingList), "number", existing.Number)
		}
	}

	if existing == nil {
		deferral, err := mergedReleaseDeferral(ctx, f, owner, repoName, comp.Branch, cfg, logger)
		if err != nil {
			return fmt.Errorf("release-me: component %s: %w", comp.Name, err)
		}
		if deferral != "" {
			printLine(stdout, comp.Name, multi, deferral)
			return nil
		}
	}

	versionStr := rel.version.Version
	entries := rel.entries
	fromTag := rel.fromTag
	versionOverride := ""
	overrideValid := false
	if existing != nil {
		_, last, hasLast, err := history.LastTag(ctx, dir, comp.TagPrefix)
		if err != nil {
			return fmt.Errorf("release-me: component %s: last tag: %w", comp.Name, err)
		}
		versionOverride, versionStr, overrideValid = applyVersionOverride(existing.Body, versionStr, last, hasLast, logger, comp.Name, existing.Number)
		if versionOverride != "" {
			recordOverrideOutcome(ctx, existing.Number, "", overrideValid)
		}
	}
	if existing != nil && !overrideValid {
		preType, isPre, err := selectPreReleaseType(existing.Labels, cfg.Labels)
		if err != nil {
			return fmt.Errorf("release-me: component %s: pull request #%d: %w", comp.Name, existing.Number, err)
		}
		if isPre {
			versionStr, entries, fromTag, err = preReleaseEntries(ctx, f, dir, cfg, comp, familyTagPrefixes, owner, repoName, rel, preType, logger)
			if err != nil {
				return fmt.Errorf("release-me: component %s: %w", comp.Name, err)
			}
		}
	}

	tag := comp.TagPrefix + versionStr
	entriesBody, err := sectionBody(ctx, f, dir, owner, repoName, comp, entries, rel.options, tag, fromTag, true)
	if err != nil {
		return err
	}
	link := compareURL(f, owner, repoName, fromTag, tag)
	update := changelogUpdate{
		path: comp.ChangelogPath, version: versionStr, entriesBody: entriesBody, compareURL: link, skip: comp.SkipChangelog,
	}
	plan, err := planReleaseBranch(ctx, dir, []changelogUpdate{update}, filesAt(comp.Files, versionStr), comp.Branch)
	if err != nil {
		return fmt.Errorf("release-me: component %s: %w", comp.Name, err)
	}
	defer plan.cleanup()
	plan.requireSignoff(ctx, cfg.Signoff)

	notes := ""
	if existing != nil {
		notes, _ = releasepr.ExtractNotes(existing.Body)
	}

	date := time.Now()
	if existing != nil && !plan.changed {
		if d, ok := releasepr.ExtractDate(existing.Body, cfg.ChangelogStyle.DateFormat); ok {
			date = d
		}
	}
	section := changelog.RenderHeading(versionStr, date, link, cfg.ChangelogStyle.DateFormat) + "\n\n" + entriesBody + "\n"

	title, err := renderTitle(cfg.TitlePatterns(),
		releasepr.TitleValues{Version: versionStr, Component: comp.Name, Branch: base}, "component "+comp.Name)
	if err != nil {
		return err
	}
	body := cfg.Frame().Wrap(releasepr.Body(versionStr, tag, bodyGuide(cfg), notes, versionOverride, section)+
		releasepr.TitlePatternMarker(cfg.TitlePatterns()[0], releasepr.TitlePerComponent)) +
		releasepr.DateMarker(date, cfg.ChangelogStyle.DateFormat)

	switch {
	case existing != nil && !plan.changed && body == existing.Body && title == existing.Title:
		recordPR(ctx, dryRun, *existing, comp.Branch, []string{comp.Name})
		printLine(stdout, comp.Name, multi, "release pull request up to date")
		return nil

	case existing != nil && !plan.changed:
		// The content that would land on the branch did not change, only
		// the notes block or another free-text part of the body did: there
		// is nothing to push.
		if dryRun {
			printLine(stdout, comp.Name, multi, fmt.Sprintf("would update %s", title))
			return nil
		}
		updated, err := f.UpdatePullRequest(ctx, owner, repoName, existing.Number, title, body)
		if err != nil {
			return fmt.Errorf("release-me: component %s: update release pull request #%d: %w", comp.Name, existing.Number, err)
		}
		if err := ensurePendingLabel(ctx, f, owner, repoName, existing.Number, existing.Labels, cfg.Labels.Pending); err != nil {
			return err
		}
		recordPR(ctx, dryRun, updated, comp.Branch, []string{comp.Name})
		printLine(stdout, comp.Name, multi, fmt.Sprintf("updated %s", title))
		return nil

	default:
		action := "create"
		if existing != nil {
			action = "update"
		}
		if dryRun {
			printLine(stdout, comp.Name, multi, fmt.Sprintf("would %s %s", action, title))
			return nil
		}

		if err := plan.write(ctx, cfg, f, owner, repoName, existing, comp.Branch, date, releasepr.CommitMessage(title, cfg.Signoff), logger); err != nil {
			return fmt.Errorf("release-me: component %s: %w", comp.Name, err)
		}

		if existing == nil {
			created, err := f.CreatePullRequest(ctx, owner, repoName, base, comp.Branch, title, body, cfg.Labels.Opened(), cfg.ReleasePullRequestDraft)
			if err != nil {
				return fmt.Errorf(
					"release-me: component %s: create release pull request: pushed branch %s but the pull request failed: %w",
					comp.Name, comp.Branch, err,
				)
			}
			recordPR(ctx, dryRun, created, comp.Branch, []string{comp.Name})
			printLine(stdout, comp.Name, multi, fmt.Sprintf("created %s", title))
			return nil
		}
		updated, err := f.UpdatePullRequest(ctx, owner, repoName, existing.Number, title, body)
		if err != nil {
			return fmt.Errorf(
				"release-me: component %s: update release pull request #%d: pushed branch %s but the update failed: %w",
				comp.Name, existing.Number, comp.Branch, err,
			)
		}
		if err := ensurePendingLabel(ctx, f, owner, repoName, existing.Number, existing.Labels, cfg.Labels.Pending); err != nil {
			return err
		}
		recordPR(ctx, dryRun, updated, comp.Branch, []string{comp.Name})
		printLine(stdout, comp.Name, multi, fmt.Sprintf("updated %s", title))
		return nil
	}
}

// doPRLocked creates or updates the one release pull request a locked
// family shares, branch "release-me/main": every configured component
// bumped to the family's shared version and released together, changed or
// not, each with its own CHANGELOG.md section and its own named section
// inside the combined pull request body.
func doPRLocked(
	ctx context.Context, dir string, cfg config.Config, owner, repoName, base string,
	f forge.Forge, logger *slog.Logger, dryRun bool, stdout io.Writer,
) error {
	members, err := gatherFamily(ctx, dir, cfg, owner, repoName, f, logger)
	if err != nil {
		return err
	}
	releases, released := familyReleases(cfg, members, logger)
	if !released {
		fmt.Fprintln(stdout, "nothing to release")
		return nil
	}

	branch := members[0].comp.Branch
	names := make([]string, 0, len(members))
	for _, m := range members {
		names = append(names, m.comp.Name)
	}

	existingList, err := f.PullRequests(ctx, owner, repoName, branch, "open")
	if err != nil {
		return fmt.Errorf("release-me: locked family: list release pull requests: %w", err)
	}
	var existing *forge.PullRequest
	if len(existingList) > 0 {
		existing = &existingList[0]
		if len(existingList) > 1 {
			logger.Warn("more than one open locked release pull request, using the first",
				"head", branch, "count", len(existingList), "number", existing.Number)
		}
	}

	if existing == nil {
		deferral, err := mergedReleaseDeferral(ctx, f, owner, repoName, branch, cfg, logger)
		if err != nil {
			return fmt.Errorf("release-me: locked family: %w", err)
		}
		if deferral != "" {
			fmt.Fprintln(stdout, deferral)
			return nil
		}
	}

	versionStr := ""
	for _, rel := range releases {
		versionStr = rel.version.Version
		break
	}

	versionOverride := ""
	overrideValid := false
	if existing != nil {
		last, hasLast := familyLastVersion(members)
		versionOverride, versionStr, overrideValid = applyVersionOverride(existing.Body, versionStr, last, hasLast, logger, "locked family", existing.Number)
		if versionOverride != "" {
			recordOverrideOutcome(ctx, existing.Number, "", overrideValid)
		}
	}
	if existing != nil && !overrideValid {
		preType, isPre, err := selectPreReleaseType(existing.Labels, cfg.Labels)
		if err != nil {
			return fmt.Errorf("release-me: locked family: pull request #%d: %w", existing.Number, err)
		}
		if isPre {
			versionStr, releases, err = lockedPreReleaseEntries(ctx, f, dir, cfg, members, owner, repoName, releases, preType, logger)
			if err != nil {
				return fmt.Errorf("release-me: locked family: %w", err)
			}
		}
	}

	// Every component releases at versionStr in a locked family, so a
	// workspace dependency adds no member, only the manifest updates and
	// the entries naming them.
	var depFiles []fileUpdate
	var depEntries map[string][]changelog.Entry
	if len(cfg.Workspaces) > 0 {
		all := make([]config.ResolvedComponent, 0, len(members))
		versions := make(map[string]string, len(members))
		for _, m := range members {
			all = append(all, m.comp)
			versions[m.comp.Name] = versionStr
		}
		manifests, err := readWorkspace(dir, cfg, all)
		if err != nil {
			return err
		}
		depFiles, depEntries = workspaceUpdates(cfg, manifests, versions, logger)
	}

	changelogs := make([]changelogUpdate, 0, len(members))
	bodyParts := make([]releasepr.ComponentChangelog, 0, len(members))
	var files []fileUpdate
	for _, m := range members {
		var entries []changelog.Entry
		if rel, ok := releases[m.comp.Name]; ok {
			entries = rel.entries
		}
		entries = append(slices.Clone(entries), depEntries[m.comp.Name]...)
		entriesBody, err := sectionBody(ctx, f, dir, owner, repoName, m.comp, entries, changelogOptions(m.comp),
			m.comp.TagPrefix+versionStr, releases[m.comp.Name].fromTag, true)
		if err != nil {
			return err
		}
		changelogs = append(changelogs, changelogUpdate{
			path: m.comp.ChangelogPath, version: versionStr, entriesBody: entriesBody,
			compareURL: compareURL(f, owner, repoName, releases[m.comp.Name].fromTag, m.comp.TagPrefix+versionStr),
			skip:       m.comp.SkipChangelog,
		})
		bodyParts = append(bodyParts, releasepr.ComponentChangelog{Name: m.comp.Name, Version: versionStr, Section: entriesBody})
		files = append(files, filesAt(m.comp.Files, versionStr)...)
	}
	files = append(files, depFiles...)

	plan, err := planReleaseBranch(ctx, dir, changelogs, files, branch)
	if err != nil {
		return fmt.Errorf("release-me: locked family: %w", err)
	}
	defer plan.cleanup()
	plan.requireSignoff(ctx, cfg.Signoff)

	notes := ""
	if existing != nil {
		notes, _ = releasepr.ExtractNotes(existing.Body)
	}
	date := time.Now()
	if existing != nil && !plan.changed {
		if d, ok := releasepr.ExtractDate(existing.Body, cfg.ChangelogStyle.DateFormat); ok {
			date = d
		}
	}
	// The combined heading stays unlinked: every component carries a tag
	// of its own, so there is no one compare view for the family. Each
	// component's own CHANGELOG.md heading links its own.
	section := changelog.RenderHeading(versionStr, date, "", cfg.ChangelogStyle.DateFormat) + "\n\n" +
		releasepr.RenderComponents(releasepr.Layout(cfg.ReleasePullRequestLayout), bodyParts) + "\n"

	tags := make([]string, 0, len(members))
	for _, m := range members {
		tags = append(tags, m.comp.TagPrefix+versionStr)
	}

	title, err := renderTitle(cfg.SharedTitlePatterns(),
		releasepr.TitleValues{Version: versionStr, Components: names, Branch: base}, "locked family")
	if err != nil {
		return err
	}
	body := cfg.Frame().Wrap(releasepr.Body(versionStr, strings.Join(tags, ", "), bodyGuide(cfg), notes, versionOverride, section)+
		releasepr.TitlePatternMarker(cfg.SharedTitlePatterns()[0], cfg.SharedTitleKind())) +
		releasepr.DateMarker(date, cfg.ChangelogStyle.DateFormat)

	return publishSharedPR(ctx, cfg, f, owner, repoName, base, branch, "locked family", names, existing, plan, date, title, body, logger, dryRun, stdout)
}

// publishSharedPR creates, updates or leaves alone the one release pull
// request every component shares, on branch, the same three outcomes
// doPRComponent decides for one component's own: up to date, a body only
// update with no push, or a push followed by the create or update. who
// names the locked family or the shared release pull request in an error;
// components names every component the pull request releases.
func publishSharedPR(
	ctx context.Context, cfg config.Config, f forge.Forge, owner, repoName, base, branch, who string, components []string,
	existing *forge.PullRequest, plan *releasePlan, date time.Time, title, body string, logger *slog.Logger, dryRun bool, stdout io.Writer,
) error {
	switch {
	case existing != nil && !plan.changed && body == existing.Body && title == existing.Title:
		recordPR(ctx, dryRun, *existing, branch, components)
		fmt.Fprintln(stdout, "release pull request up to date")
		return nil

	case existing != nil && !plan.changed:
		if dryRun {
			fmt.Fprintf(stdout, "would update %s\n", title)
			return nil
		}
		updated, err := f.UpdatePullRequest(ctx, owner, repoName, existing.Number, title, body)
		if err != nil {
			return fmt.Errorf("release-me: %s: update release pull request #%d: %w", who, existing.Number, err)
		}
		if err := ensurePendingLabel(ctx, f, owner, repoName, existing.Number, existing.Labels, cfg.Labels.Pending); err != nil {
			return err
		}
		recordPR(ctx, dryRun, updated, branch, components)
		fmt.Fprintf(stdout, "updated %s\n", title)
		return nil

	default:
		action := "create"
		if existing != nil {
			action = "update"
		}
		if dryRun {
			fmt.Fprintf(stdout, "would %s %s\n", action, title)
			return nil
		}

		if err := plan.write(ctx, cfg, f, owner, repoName, existing, branch, date, releasepr.CommitMessage(title, cfg.Signoff), logger); err != nil {
			return fmt.Errorf("release-me: %s: %w", who, err)
		}

		if existing == nil {
			created, err := f.CreatePullRequest(ctx, owner, repoName, base, branch, title, body, cfg.Labels.Opened(), cfg.ReleasePullRequestDraft)
			if err != nil {
				return fmt.Errorf(
					"release-me: %s: create release pull request: pushed branch %s but the pull request failed: %w", who, branch, err,
				)
			}
			recordPR(ctx, dryRun, created, branch, components)
			fmt.Fprintf(stdout, "created %s\n", title)
			return nil
		}
		updated, err := f.UpdatePullRequest(ctx, owner, repoName, existing.Number, title, body)
		if err != nil {
			return fmt.Errorf(
				"release-me: %s: update release pull request #%d: pushed branch %s but the update failed: %w",
				who, existing.Number, branch, err,
			)
		}
		if err := ensurePendingLabel(ctx, f, owner, repoName, existing.Number, existing.Labels, cfg.Labels.Pending); err != nil {
			return err
		}
		recordPR(ctx, dryRun, updated, branch, components)
		fmt.Fprintf(stdout, "updated %s\n", title)
		return nil
	}
}

// lockedPreReleaseEntries recomputes, for a locked family's open pull
// request carrying a pre-release type label, the pre-release version
// shared by every component and the entries each one renders: the shared
// counter scans every component's own tag prefix, and each component's own
// entries are restricted to those since its own previous pre-release tag,
// the same rule preReleaseEntries applies to one component.
func lockedPreReleaseEntries(
	ctx context.Context, f forge.Forge, dir string, cfg config.Config, members []familyMember, owner, repoName string,
	releases map[string]release, preType string, logger *slog.Logger,
) (versionStr string, out map[string]release, err error) {
	prefixes := familyTagPrefixes(members)
	out = make(map[string]release, len(releases))
	for _, m := range members {
		rel, ok := releases[m.comp.Name]
		if !ok {
			continue
		}
		vStr, entries, fromTag, err := preReleaseEntries(ctx, f, dir, cfg, m.comp, prefixes, owner, repoName, rel, preType, logger)
		if err != nil {
			return "", nil, fmt.Errorf("component %s: %w", m.comp.Name, err)
		}
		versionStr = vStr
		out[m.comp.Name] = release{
			version: version.Result{Version: vStr, Bump: rel.version.Bump, Reason: rel.version.Reason},
			entries: entries, options: changelogOptions(m.comp), fromTag: fromTag,
		}
	}
	return versionStr, out, nil
}

// mergedReleaseDeferral returns a message when branch carries a closed,
// merged pull request still carrying every one of the pending labels, or a
// tagged release whose deferred tag is absent. An empty message means a new
// release pull request may be opened. Only the newest merged, tagged pull
// request is probed for a missing deferred tag, keeping remote lookups
// bounded as the release history grows.
//
// That probe runs whatever "github-release-draft-until-assets-attach" is set to, because a
// deferred tag can go missing without a draft ever existing: with drafts
// off the release is published right away, and if GitHub then creates no
// tag, nothing local would notice. It costs nothing where no component
// defers its tag, which is the default: unpublishedDeferredRelease skips
// every component with ForceTagCreation on before it makes a single forge
// call.
//
// The pending labels signal a release already decided, not yet tagged.
// doRelease recognizes the exact same pull request by the exact same
// signal to tag it, so as long as it still carries the label, nothing else
// may release in its place.
//
// This is the case right after a release pull request's merge lands on
// the default branch: that same push's run invocation calls doPR before
// doRelease tags the release, so the pull request is no longer open, and
// history.LastTag, reading the local repository doPR runs against, does
// not see the tag either: CreateTag writes it through the forge API
// alone, never to a local ref, and release-me never fetches mid-run.
// Reordering run to tag first would not help: the same local repository
// stays blind to that tag until the next checkout. Recognizing the
// merged, still pending pull request directly, the same way doRelease
// already does, needs no local git state at all.
func mergedReleaseDeferral(ctx context.Context, f forge.Forge, owner, repoName, branch string, cfg config.Config, logger *slog.Logger) (string, error) {
	closed, err := f.PullRequests(ctx, owner, repoName, branch, "closed")
	if err != nil {
		return "", fmt.Errorf("list closed pull requests on %s: %w", branch, err)
	}
	var newestTagged *forge.PullRequest
	for _, pr := range closed {
		if !pr.Merged {
			continue
		}
		if tagrelease.HasAll(pr.Labels, cfg.Labels.Pending) {
			logger.Debug("merged pull request still pending, deferring to release",
				"head", branch, "number", pr.Number)
			return "nothing to release", nil
		}
		if tagrelease.HasAll(pr.Labels, cfg.Labels.Tagged) && (newestTagged == nil || pr.Number > newestTagged.Number) {
			newestTagged = &pr
		}
	}
	if newestTagged != nil {
		deferral, err := unpublishedDeferredRelease(ctx, f, owner, repoName, cfg, *newestTagged)
		if err != nil {
			return "", err
		}
		if deferral != "" {
			logger.Debug("merged release has an unpublished deferred tag, deferring to release",
				"head", branch, "number", newestTagged.Number, "deferral", deferral)
			return deferral, nil
		}
	}
	return "", nil
}

// unpublishedDeferredRelease checks the version the merged PR actually
// released, rather than the next version recomputed from a still-missing tag.
// It returns the message the caller prints, or an empty string when a new
// release pull request may be opened.
//
// Two states hold a new pull request back, both of them reachable only
// while the tag a component deferred to the release publish is still
// absent. The first is a release still waiting as a draft: publishing it
// creates the tag, so the next run moves on. A draft is reachable even
// with "github-release-draft-until-assets-attach" off now, because it can have been
// created while that key was still on. The second is a release that
// is already published while its tag is still missing, which is the state
// GitHub is not supposed to leave behind at all: creating a release with a
// target commitish is assumed to create the tag on publish, and that
// assumption is not documented anywhere authoritative, see
// forge/github.CreateRelease. This branch is where a wrong assumption
// surfaces. Nothing reaches it while GitHub does create the tag, because
// TagExists is true then and the loop continues before any release lookup.
//
// Both states defer rather than return an error, because deferring is the
// conservative answer to each of them. It never opens a pull request for a
// version that is already released, a transient race between the publish
// and the tag creation resolves itself on the next run, and an assumption
// that is permanently wrong shows a named, repeated message on every run
// instead of a silent loop that reopens the same release pull request and
// re-releases the same version forever.
func unpublishedDeferredRelease(ctx context.Context, f forge.Forge, owner, repoName string, cfg config.Config, pr forge.PullRequest) (string, error) {
	versions := map[string]string{}
	components := cfg.Resolve()
	for _, comp := range components {
		if v, ok := tagrelease.VersionFromTitle(pr.Title, comp.Name); ok {
			versions[comp.Name] = v
		}
	}
	if titled, ok := tagrelease.ParseSharedTitle(pr.Title); ok {
		for _, r := range titled {
			versions[r.Name] = r.Version
		}
	}
	if v, names, ok := tagrelease.VersionFromLockedTitle(pr.Title); ok {
		for _, name := range names {
			versions[name] = v
		}
	}
	for _, comp := range components {
		v := versions[comp.Name]
		if comp.ForceTagCreation || v == "" {
			continue
		}
		tag := comp.TagPrefix + v
		exists, err := f.TagExists(ctx, owner, repoName, tag)
		if err != nil {
			return "", fmt.Errorf("component %s pull request #%d: check deferred tag %s: %w", comp.Name, pr.Number, tag, err)
		}
		if exists {
			continue
		}
		rel, err := f.Release(ctx, owner, repoName, tag)
		if errors.Is(err, forge.ErrReleaseNotFound) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("component %s pull request #%d: check deferred draft %s: %w", comp.Name, pr.Number, tag, err)
		}
		if rel.Draft {
			return fmt.Sprintf("waiting for unpublished release %q from pull request #%d; publish or delete its draft to continue", pr.Title, pr.Number), nil
		}
		return fmt.Sprintf(
			"the release from pull request #%d is published but its tag %s does not exist, so GitHub did not create it; "+
				"create %s on the release's target commit, or delete the release so the next run recreates it",
			pr.Number, tag, tag,
		), nil
	}
	return "", nil
}

// resolveBaseBranch reads the repository's default branch from the forge,
// falling back to the "origin" remote's reported HEAD when the forge call
// fails. It never uses the branch currently checked out: on a detached
// HEAD, that would be the literal string "HEAD", not a branch name.
func resolveBaseBranch(ctx context.Context, f forge.Forge, dir, owner, repoName string) (string, error) {
	base, err := f.DefaultBranch(ctx, owner, repoName)
	if err == nil && base != "" {
		return base, nil
	}

	fallback, fallbackErr := history.OriginHEAD(ctx, dir)
	if fallbackErr != nil {
		if err == nil {
			err = fmt.Errorf("the forge returned an empty name")
		}
		return "", fmt.Errorf("release-me: default branch: %w, and the origin HEAD fallback failed: %w", err, fallbackErr)
	}
	return fallback, nil
}

// ensurePendingLabel adds every pending label a pull request does not carry
// yet, never replacing the labels it has.
func ensurePendingLabel(ctx context.Context, f forge.Forge, owner, repoName string, number int, currentLabels, pending []string) error {
	var missing []string
	for _, l := range pending {
		if !slices.Contains(currentLabels, l) {
			missing = append(missing, l)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if err := f.AddLabels(ctx, owner, repoName, number, missing); err != nil {
		return fmt.Errorf("release-me: add label(s) %q to release pull request #%d: %w", missing, number, err)
	}
	return nil
}

// applyVersionOverride reads the release-me-version fenced block from an
// existing release pull request's body, a write-access commenter's
// "release-as" command or a hand edit, and reports what pr should do with
// it: raw is the block's exact text, read back unchanged so Body can carry
// it through to the re-rendered pull request even when it is unused this
// run; overridden is computed, with raw substituted for it when raw
// parses as a real semantic version strictly greater than last, the
// highest version the component, or the whole locked family, already
// carries a tag at; ok is true exactly then, so the caller skips the
// normal pre-release label handling: the override wins outright over the
// computed bump instead of combining with it. hasLast is false before any
// tag exists at all, in which case any parseable version is accepted.
// The rule is version.ParseForced's, a pre-release value accepted: the
// block is transient, gone once the pull request merges, so it never
// forces the same pre-release twice. who and number name the component
// or locked family and the pull request in the warning logged when raw is
// present but unused this run, never failing it.
func applyVersionOverride(
	body, computed string, last version.Semver, hasLast bool, logger *slog.Logger, who string, number int,
) (raw, overridden string, ok bool) {
	raw, found := releasepr.ExtractVersionOverride(body)
	if !found {
		return "", computed, false
	}
	overridden, ok = checkVersionOverride(raw, computed, last, hasLast, logger, who, number)
	return raw, overridden, ok
}

// checkVersionOverride is applyVersionOverride's decision for one value
// already read out of the block: value, as a version, when it parses and
// is strictly greater than last, ok true; computed otherwise, with the
// reason logged.
func checkVersionOverride(
	value, computed string, last version.Semver, hasLast bool, logger *slog.Logger, who string, number int,
) (overridden string, ok bool) {
	sv, err := version.ParseForced(value, last, hasLast, true)
	switch {
	case errors.Is(err, version.ErrNotAVersion):
		logger.Warn("ignoring a release-me-version override that does not parse as a version",
			"component", who, "number", number, "value", value)
		return computed, false
	case err != nil:
		logger.Warn("ignoring a release-me-version override that is not greater than the last released version",
			"component", who, "number", number, "value", value, "last", last.String())
		return computed, false
	default:
		return sv.String(), true
	}
}

// overrideRecorder collects, for every open release pull request doPR
// touches this run, whether a release-me-version override present on its
// body actually won against the last released version: the one decision
// applyVersionOverride already makes, silently, every run. A "release-as"
// comment command wires one into ctx, through withOverrideRecorder, right
// before calling doPR, so it can report accurately on the one pull
// request it just wrote the override onto, instead of assuming the
// override won whenever doPR itself returns no error. The ordinary "pr"
// command's own run never wires one in, so recordOverrideOutcome is a
// no-op and costs nothing there.
type overrideRecorder struct {
	results map[overrideKey]bool
}

// overrideKey names the override an overrideRecorder entry is about: the
// pull request number, and the component a "<component> X.Y.Z" line in a
// shared release pull request with independent versions names, "" for the
// one version every other release pull request carries.
type overrideKey struct {
	number    int
	component string
}

// overrideRecorderKey is the unexported context key an *overrideRecorder
// is stored under, so only withOverrideRecorder and recordOverrideOutcome
// ever reach it.
type overrideRecorderKey struct{}

// withOverrideRecorder returns ctx carrying rec, read back by every
// recordOverrideOutcome call doPR's run makes.
func withOverrideRecorder(ctx context.Context, rec *overrideRecorder) context.Context {
	return context.WithValue(ctx, overrideRecorderKey{}, rec)
}

// recordOverrideOutcome records, for pull request number, whether a
// release-me-version override found on its body actually won this run,
// when ctx carries an *overrideRecorder; it does nothing otherwise.
// component is "" except for one component's line in a shared release
// pull request with independent versions.
func recordOverrideOutcome(ctx context.Context, number int, component string, applied bool) {
	rec, ok := ctx.Value(overrideRecorderKey{}).(*overrideRecorder)
	if !ok {
		return
	}
	if rec.results == nil {
		rec.results = make(map[overrideKey]bool)
	}
	rec.results[overrideKey{number: number, component: component}] = applied
}

// changelogUpdate is one component's changelog file path, the version its
// heading names, the entries body (without the heading's date) doPR would
// prepend to it and the compare view its heading links to, "" for none,
// one per component of a shared release branch, exactly one for a
// component's own.
type changelogUpdate struct {
	path        string
	version     string
	entriesBody string
	compareURL  string
	baseline    string
	// skip is true for a component with "skip-changelog": the file at
	// path is neither compared nor written, the section still renders
	// into the release pull request body.
	skip bool
}

// fileUpdate is one configured file doPR bumps, with the version it bumps
// it to: the component's own, so components sharing a release branch at
// different versions each bump their own files to their own version.
type fileUpdate struct {
	file    config.File
	version string
	// apply, when set, replaces file.Updater: the update a workspace
	// dependency bump makes to a dependent's manifest, which no configured
	// updater describes. file.Path still names the file; what names the
	// update in an error.
	apply func(content string) (string, error)
	what  string
}

// filesAt pairs every file in files with version.
func filesAt(files []config.File, version string) []fileUpdate {
	out := make([]fileUpdate, 0, len(files))
	for _, f := range files {
		out = append(out, fileUpdate{file: f, version: version})
	}
	return out
}

// releasePlan is the release branch content doPR would write, computed
// against a temporary worktree checked out at dir's HEAD, together with
// whether it differs from what the release branch currently carries.
type releasePlan struct {
	dir          string
	tmp          string
	changelogs   []changelogUpdate
	desiredFiles map[string]string
	changed      bool
	cleanedUp    bool
}

// planReleaseBranch computes the release branch content doPR would write
// for every entry of changelogs and files, and compares it against
// branch's current tip, fetched into compareRef. A branch that does not
// exist on the remote yet is always "changed": there is nothing to compare
// it against. Several entries naming the same file apply one after another
// to the same content, so each one's own key or marker keeps its update:
// config refuses two independently versioned components bumping the same
// key, or both a marker, in one file, and in locked and linked versioning
// every entry carries the same version, so no entry overwrites another's.
func planReleaseBranch(ctx context.Context, dir string, changelogs []changelogUpdate, files []fileUpdate, branch string) (*releasePlan, error) {
	tmp, err := os.MkdirTemp("", "release-me-pr-")
	if err != nil {
		return nil, fmt.Errorf("release-me: temporary worktree: %w", err)
	}
	plan := &releasePlan{dir: dir, tmp: tmp}

	if err := gitRun(ctx, dir, "worktree", "add", "--detach", "--quiet", tmp, "HEAD"); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("release-me: add worktree: %w", err)
	}

	plan.changelogs = make([]changelogUpdate, len(changelogs))
	for i, c := range changelogs {
		if c.skip {
			plan.changelogs[i] = c
			continue
		}
		baseline, err := os.ReadFile(filepath.Join(tmp, c.path))
		if err != nil && !os.IsNotExist(err) {
			plan.cleanup()
			return nil, fmt.Errorf("release-me: read %s: %w", c.path, err)
		}
		c.baseline = string(baseline)
		plan.changelogs[i] = c
	}

	plan.desiredFiles = make(map[string]string, len(files))
	for _, fu := range files {
		file, version := fu.file, fu.version
		content, seen := plan.desiredFiles[file.Path]
		if !seen {
			raw, err := os.ReadFile(filepath.Join(tmp, file.Path))
			if err != nil {
				plan.cleanup()
				return nil, fmt.Errorf("release-me: read %s: %w", file.Path, err)
			}
			content = string(raw)
		}
		if fu.apply != nil {
			newContent, err := fu.apply(content)
			if err != nil {
				plan.cleanup()
				return nil, fmt.Errorf("release-me: file %s: %s: %w", file.Path, fu.what, err)
			}
			plan.desiredFiles[file.Path] = newContent
			continue
		}
		entry, ok := updater.Updaters[file.Updater]
		if !ok {
			plan.cleanup()
			return nil, fmt.Errorf("release-me: file %s: unknown updater %q", file.Path, file.Updater)
		}
		newContent, err := entry.Update(content, file.Key, version)
		if err != nil {
			plan.cleanup()
			if file.Key != "" {
				return nil, fmt.Errorf("release-me: file %s: key %s: %w", file.Path, file.Key, err)
			}
			return nil, fmt.Errorf("release-me: file %s: %w", file.Path, err)
		}
		plan.desiredFiles[file.Path] = newContent
	}

	plan.changed = !sameAsReleaseBranch(ctx, tmp, plan.changelogs, plan.desiredFiles, branch)
	return plan, nil
}

// sameAsReleaseBranch reports whether every changelogs entry, at its own
// version, and every desiredFiles entry already match branch's current
// tip. It fetches the branch into compareRef to read it; a branch that
// does not exist remotely yet, or any read failure, is reported as not
// matching. A changelog heading whose version differs from c's counts as not
// matching too, even when the entries underneath read the same: a
// version-only difference, a pre-release label added, changed or removed
// with no new entry, must still push. So does a heading whose compare link
// differs from the one c carries, a heading written before the link
// existed included, so the branch picks the link up. A heading that does
// not read back counts as not matching only when the heading the push
// would write does read back: when neither does, a push could never make
// the comparison settle, so the heading is left out of it and only the
// entries underneath and desiredFiles decide.
func sameAsReleaseBranch(ctx context.Context, tmp string, changelogs []changelogUpdate, desiredFiles map[string]string, branch string) bool {
	pushURL, err := gitOutput(ctx, tmp, "remote", "get-url", "--push", "origin")
	if err != nil {
		return false
	}
	if err := gitRun(ctx, tmp, "fetch", "--quiet", pushURL, "+refs/heads/"+branch+":"+compareRef); err != nil {
		return false
	}

	for _, c := range changelogs {
		if c.skip {
			branchContent, branchExists := gitShowFile(ctx, tmp, compareRef, c.path)
			headContent, headExists := gitShowFile(ctx, tmp, "HEAD", c.path)
			if branchExists != headExists || branchContent != headContent {
				return false
			}
			continue
		}
		changelogContent, ok := gitShowFile(ctx, tmp, compareRef, c.path)
		if !ok {
			return false
		}
		existing, ok := changelog.ParseHeading(changelogContent)
		switch {
		case !ok && headingReadsBack(c.version, c.compareURL):
			return false
		case ok && (existing.Version != c.version || existing.CompareURL != c.compareURL):
			return false
		}
		existingBody, ok := changelog.TopSection(changelogContent)
		if !ok || existingBody != c.entriesBody {
			return false
		}
	}

	for path, want := range desiredFiles {
		got, ok := gitShowFile(ctx, tmp, compareRef, path)
		if !ok || got != want {
			return false
		}
	}
	return true
}

// requireSignoff refreshes an existing release commit whose desired trailer
// changed even when every generated file and the PR body already match.
func (p *releasePlan) requireSignoff(ctx context.Context, signoff string) {
	if p.changed || signoff == "" {
		return
	}
	trailers, err := gitOutput(ctx, p.tmp, "show", "-s", "--format=%(trailers:key=Signed-off-by,valueonly)", compareRef)
	if err != nil || !slices.Contains(strings.Split(trailers, "\n"), signoff) {
		p.changed = true
	}
}

// headingReadsBack reports whether the heading write renders for version
// and compareURL parses again, which the comparison in sameAsReleaseBranch
// relies on. The date is irrelevant to that, so any date does.
func headingReadsBack(version, compareURL string) bool {
	_, ok := changelog.ParseHeading(changelog.RenderHeading(version, time.Time{}, compareURL, ""))
	return ok
}

// write commits the changelog update and the bumped files onto branch
// through the forge's API, f.CommitBranch, never through a local commit
// and push: the forge signs a commit it creates itself, GitHub for a
// GitHub App installation token only, so a ruleset requiring signed
// commits accepts the release pull request, which a local commit made
// with a token that has no signing key never satisfies. A commit the
// forge left unsigned, the outcome of a personal access token, is
// reported through logger by warnUnsigned, never an error: the branch
// is already written and only a ruleset decides whether it matters.
// The commit's parent is the commit the temporary worktree planned in p is
// checked out at, dir's HEAD, the same one a local commit there would get,
// and it carries exactly the content a local commit would: every
// changelog with its section prepended and every bumped file. A fresh
// branch, with no existing pull request to lease against, is created, or
// moved only as a fast-forward; otherwise the lease is the existing pull
// request's head SHA from the forge. The forge attributes the commit to
// the token's own identity: cfg's author is not passed, since GitHub
// leaves a commit with a custom author or committer unsigned.
func (p *releasePlan) write(
	ctx context.Context, cfg config.Config, f forge.Forge, owner, repoName string, existing *forge.PullRequest, branch string, date time.Time, commitMessage string,
	logger *slog.Logger,
) error {
	parent, err := gitOutput(ctx, p.tmp, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("release-me: release branch %s: resolve parent commit: %w", branch, err)
	}

	contents := make(map[string]string, len(p.changelogs)+len(p.desiredFiles))
	var order []string
	set := func(path, content string) {
		if _, seen := contents[path]; !seen {
			order = append(order, path)
		}
		contents[path] = content
	}
	for _, c := range p.changelogs {
		if c.skip {
			continue
		}
		section := changelog.RenderHeading(c.version, date, c.compareURL, cfg.ChangelogStyle.DateFormat) + "\n\n" + c.entriesBody + "\n"
		set(c.path, changelog.Prepend(c.baseline, section))
	}
	paths := make([]string, 0, len(p.desiredFiles))
	for path := range p.desiredFiles {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		set(path, p.desiredFiles[path])
	}

	// A symlink and its target both listed resolve to one path, which gets
	// one tree entry: the later one wins, as it did writing to disk.
	files := make([]forge.CommitFile, 0, len(order))
	entry := make(map[string]int, len(order))
	for _, path := range order {
		target, err := releaseFileTarget(ctx, p.tmp, path)
		if err != nil {
			return fmt.Errorf("release-me: release branch %s: %w", branch, err)
		}
		executable, err := executableAt(ctx, p.tmp, target)
		if err != nil {
			return fmt.Errorf("release-me: release branch %s: %w", branch, err)
		}
		file := forge.CommitFile{Path: target, Content: []byte(contents[path]), Executable: executable}
		if i, seen := entry[target]; seen {
			files[i] = file
			continue
		}
		entry[target] = len(files)
		files = append(files, file)
	}

	sha, err := f.CommitBranch(ctx, owner, repoName, forge.BranchCommit{Branch: branch, Parent: parent, Message: commitMessage, Files: files})
	if err != nil {
		if existing != nil {
			return fmt.Errorf("release-me: commit release branch %s for pull request #%d: %w", branch, existing.Number, err)
		}
		return fmt.Errorf("release-me: commit release branch %s: %w", branch, err)
	}
	lease := ""
	if existing != nil {
		lease = existing.HeadSHA
	}
	if err := publishReleaseCommit(ctx, p.tmp, branch, sha, lease); err != nil {
		if existing != nil {
			return fmt.Errorf("release-me: publish release branch %s for pull request #%d: %w", branch, existing.Number, err)
		}
		return fmt.Errorf("release-me: publish release branch %s: %w", branch, err)
	}
	warnUnsigned(ctx, f, owner, repoName, branch, sha, logger)
	return nil
}

// publishReleaseCommit fetches the API-created object before publishing it.
// The receive-pack comparison is atomic, including a writer arriving after
// the fetch. A new branch retains the ordinary fast-forward-only push rule.
func publishReleaseCommit(ctx context.Context, dir, branch, sha, expected string) error {
	pushURL, err := gitOutput(ctx, dir, "remote", "get-url", "--push", "origin")
	if err != nil {
		return fmt.Errorf("resolve publication remote: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "fetch", "--quiet", "--no-tags", pushURL, sha)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fetch prepared release commit from origin: %w: %s", err,
			strings.ReplaceAll(strings.TrimSpace(stderr.String()), pushURL, "origin"))
	}
	args := []string{"push", "--porcelain"}
	if expected != "" {
		args = append(args, "--force-with-lease=refs/heads/"+branch+":"+expected)
	}
	args = append(args, "origin", sha+":refs/heads/"+branch)
	cmd = exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	stderr.Reset()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if leaseLost(stdout.String()) {
			return fmt.Errorf("publish branch %s: %w", branch, forge.ErrBranchMoved)
		}
		return fmt.Errorf("publish branch %s: %w: %s", branch, err,
			strings.ReplaceAll(strings.TrimSpace(stderr.String()), pushURL, "origin"))
	}
	return nil
}

// leaseLost reports whether the porcelain status lines of a failed push
// say the lease was lost, which git words in two ways. Its own check
// against the ref advertisement prints "[rejected] (stale info)", for a
// branch that already moved when the push started; receive-pack's
// compare-and-swap prints "[remote rejected] (incorrect old value
// provided)", for one that moves while the push runs, which is the
// concurrent case the lease exists for. Every other "[remote rejected]"
// stays a plain error carrying git's own reason, a ruleset declining an
// unsigned commit through the pre-receive hook above all.
func leaseLost(porcelain string) bool {
	return strings.Contains(porcelain, "[rejected]") ||
		strings.Contains(porcelain, "(incorrect old value provided)")
}

// warnUnsigned reads the signature the forge verified on sha, the commit
// just written to branch, and logs a warning when it has none: GitHub
// signs a commit created through its API only for a GitHub App
// installation token, so a personal access token or an OAuth token
// yields an unsigned one, and a ruleset requiring signed commits then
// blocks the release pull request from merging. A signature that cannot
// be read is a warning too, never an error, since the commit itself
// landed.
func warnUnsigned(ctx context.Context, f forge.Forge, owner, repoName, branch, sha string, logger *slog.Logger) {
	sig, err := f.CommitSignature(ctx, owner, repoName, sha)
	if err != nil {
		logger.Warn("could not check whether the release commit is signed",
			"branch", branch, "commit", sha, "error", err)
		return
	}
	if sig.Verified {
		logger.Debug("release commit is signed", "branch", branch, "commit", sha)
		return
	}
	logger.Warn("the release commit is unsigned: the forge signs a commit it creates only for a GitHub App "+
		"installation token, a user token such as a personal access token yields an unsigned one, and a "+
		"ruleset requiring signed commits blocks the release pull request from merging",
		"branch", branch, "commit", sha, "reason", sig.Reason)
}

// releaseFileTarget follows symlinks as the worktree writer does, while
// refusing to replace a link or write outside the scratch repository.
func releaseFileTarget(ctx context.Context, dir, path string) (string, error) {
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve release file %s: %w", path, err)
	}
	candidate := filepath.Join(dir, filepath.FromSlash(path))
	ancestor := candidate
	var suffix []string
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve release file %s: %w", path, err)
		}
		suffix = append(suffix, filepath.Base(ancestor))
		next := filepath.Dir(ancestor)
		if next == ancestor {
			return "", fmt.Errorf("resolve release file %s: %w", path, err)
		}
		ancestor = next
	}
	real, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", fmt.Errorf("resolve release file %s: %w", path, err)
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		real = filepath.Join(real, suffix[i])
	}
	rel, err := filepath.Rel(dir, real)
	if err != nil {
		return "", fmt.Errorf("resolve release file %s: %w", path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("release file %s: symlink target escapes the repository", path)
	}
	target := filepath.ToSlash(rel)
	if target != path {
		entry, err := gitOutput(ctx, dir, "ls-tree", "HEAD", "--", target)
		if err != nil {
			return "", fmt.Errorf("release file %s: read symlink target: %w", path, err)
		}
		if !strings.HasPrefix(entry, "100644 ") && !strings.HasPrefix(entry, "100755 ") {
			return "", fmt.Errorf("release file %s: symlink target %s is not a tracked regular file", path, target)
		}
	}
	return target, nil
}

// executableAt reports whether path is tracked as an executable file at
// HEAD of the worktree dir, so the commit keeps its mode; a path HEAD does
// not track, a changelog created by this release, is not.
func executableAt(ctx context.Context, dir, path string) (bool, error) {
	out, err := gitOutput(ctx, dir, "ls-tree", "HEAD", "--", path)
	if err != nil {
		return false, fmt.Errorf("read the mode of %s: %w", path, err)
	}
	return strings.HasPrefix(out, "100755 "), nil
}

// cleanup removes the temporary worktree. It is safe to call more than
// once.
func (p *releasePlan) cleanup() {
	if p.cleanedUp {
		return
	}
	p.cleanedUp = true
	_ = gitRun(context.Background(), p.dir, "worktree", "remove", "--force", p.tmp)
	_ = os.RemoveAll(p.tmp)
}

// gitRun runs git in dir, surfacing stderr on failure.
func gitRun(ctx context.Context, dir string, args ...string) error {
	_, err := gitOutput(ctx, dir, args...)
	return err
}

// gitOutput runs git in dir and returns its trimmed stdout, surfacing
// stderr on failure. Use gitShowFile, not this, to read file content: a
// trimmed read would silently drop a real trailing newline.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	return gitOutputEnv(ctx, dir, nil, args...)
}

// gitOutputEnv runs git in dir with env as the subprocess environment,
// nil meaning the process's own, and returns its trimmed stdout, surfacing
// stderr on failure.
func gitOutputEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stdout.String()), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// gitShowFile returns the exact, untrimmed content of path at ref in dir.
// ok is false for any failure, a missing ref or a missing path alike: both
// are the ordinary shape of "nothing to compare against yet".
func gitShowFile(ctx context.Context, dir, ref, path string) (content string, ok bool) {
	cmd := exec.CommandContext(ctx, "git", "show", ref+":"+path)
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return stdout.String(), true
}
