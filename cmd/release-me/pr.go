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
		pending, err := mergedPendingRelease(ctx, f, owner, repoName, comp.Branch, cfg.Labels.Pending, logger)
		if err != nil {
			return fmt.Errorf("release-me: component %s: %w", comp.Name, err)
		}
		if pending {
			printLine(stdout, comp.Name, multi, "nothing to release")
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

	entriesBody := changelog.Sections(entries, rel.hiddenTypes)
	if entriesBody == "" {
		entriesBody = noUserFacingChangesLine
	}

	tag := comp.TagPrefix + versionStr
	link := compareURL(f, owner, repoName, fromTag, tag)
	update := changelogUpdate{path: comp.ChangelogPath, version: versionStr, entriesBody: entriesBody, compareURL: link}
	plan, err := planReleaseBranch(ctx, dir, []changelogUpdate{update}, filesAt(comp.Files, versionStr), comp.Branch)
	if err != nil {
		return fmt.Errorf("release-me: component %s: %w", comp.Name, err)
	}
	defer plan.cleanup()

	notes := ""
	if existing != nil {
		notes, _ = releasepr.ExtractNotes(existing.Body)
	}

	date := time.Now()
	if existing != nil && !plan.changed {
		if d, ok := releasepr.ExtractDate(existing.Body); ok {
			date = d
		}
	}
	section := changelog.RenderHeading(versionStr, date, link) + "\n\n" + entriesBody + "\n"

	title := releasepr.Title(comp.Name, versionStr)
	body := releasepr.Body(versionStr, tag, guideLabels(cfg), notes, versionOverride, section)

	switch {
	case existing != nil && !plan.changed && body == existing.Body:
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

		if err := plan.write(ctx, cfg, existing, comp.Branch, date, title); err != nil {
			return fmt.Errorf("release-me: component %s: %w", comp.Name, err)
		}

		if existing == nil {
			created, err := f.CreatePullRequest(ctx, owner, repoName, base, comp.Branch, title, body, []string{cfg.Labels.Pending})
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
		pending, err := mergedPendingRelease(ctx, f, owner, repoName, branch, cfg.Labels.Pending, logger)
		if err != nil {
			return fmt.Errorf("release-me: locked family: %w", err)
		}
		if pending {
			fmt.Fprintln(stdout, "nothing to release")
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

	changelogs := make([]changelogUpdate, 0, len(members))
	bodyParts := make([]releasepr.ComponentChangelog, 0, len(members))
	var files []fileUpdate
	for _, m := range members {
		var entries []changelog.Entry
		if rel, ok := releases[m.comp.Name]; ok {
			entries = rel.entries
		}
		entriesBody := changelog.Sections(entries, m.comp.HiddenTypes)
		if entriesBody == "" {
			entriesBody = noUserFacingChangesLine
		}
		changelogs = append(changelogs, changelogUpdate{
			path: m.comp.ChangelogPath, version: versionStr, entriesBody: entriesBody,
			compareURL: compareURL(f, owner, repoName, releases[m.comp.Name].fromTag, m.comp.TagPrefix+versionStr),
		})
		bodyParts = append(bodyParts, releasepr.ComponentChangelog{Name: m.comp.Name, Version: versionStr, Section: entriesBody})
		files = append(files, filesAt(m.comp.Files, versionStr)...)
	}

	plan, err := planReleaseBranch(ctx, dir, changelogs, files, branch)
	if err != nil {
		return fmt.Errorf("release-me: locked family: %w", err)
	}
	defer plan.cleanup()

	notes := ""
	if existing != nil {
		notes, _ = releasepr.ExtractNotes(existing.Body)
	}
	date := time.Now()
	if existing != nil && !plan.changed {
		if d, ok := releasepr.ExtractDate(existing.Body); ok {
			date = d
		}
	}
	// The combined heading stays unlinked: every component carries a tag
	// of its own, so there is no one compare view for the family. Each
	// component's own CHANGELOG.md heading links its own.
	section := changelog.RenderHeading(versionStr, date, "") + "\n\n" +
		releasepr.RenderComponents(releasepr.Layout(cfg.ReleasePullRequestLayout), bodyParts) + "\n"

	tags := make([]string, 0, len(members))
	for _, m := range members {
		tags = append(tags, m.comp.TagPrefix+versionStr)
	}

	title := tagrelease.LockedTitle(versionStr, names)
	body := releasepr.Body(versionStr, strings.Join(tags, ", "), guideLabels(cfg), notes, versionOverride, section)

	return publishSharedPR(ctx, cfg, f, owner, repoName, base, branch, "locked family", names, existing, plan, date, title, body, dryRun, stdout)
}

// publishSharedPR creates, updates or leaves alone the one release pull
// request every component shares, on branch, the same three outcomes
// doPRComponent decides for one component's own: up to date, a body only
// update with no push, or a push followed by the create or update. who
// names the locked family or the shared release pull request in an error;
// components names every component the pull request releases.
func publishSharedPR(
	ctx context.Context, cfg config.Config, f forge.Forge, owner, repoName, base, branch, who string, components []string,
	existing *forge.PullRequest, plan *releasePlan, date time.Time, title, body string, dryRun bool, stdout io.Writer,
) error {
	switch {
	case existing != nil && !plan.changed && body == existing.Body:
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

		if err := plan.write(ctx, cfg, existing, branch, date, title); err != nil {
			return fmt.Errorf("release-me: %s: %w", who, err)
		}

		if existing == nil {
			created, err := f.CreatePullRequest(ctx, owner, repoName, base, branch, title, body, []string{cfg.Labels.Pending})
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
			entries: entries, hiddenTypes: m.comp.HiddenTypes, fromTag: fromTag,
		}
	}
	return versionStr, out, nil
}

// mergedPendingRelease reports whether branch already carries a closed,
// merged pull request still labeled pendingLabel: a release already
// decided, not yet tagged. doRelease recognizes the exact same pull
// request by the exact same signal to tag it, so as long as it still
// carries the label, nothing else may release in its place.
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
func mergedPendingRelease(ctx context.Context, f forge.Forge, owner, repoName, branch, pendingLabel string, logger *slog.Logger) (bool, error) {
	closed, err := f.PullRequests(ctx, owner, repoName, branch, "closed")
	if err != nil {
		return false, fmt.Errorf("list closed pull requests on %s: %w", branch, err)
	}
	for _, pr := range closed {
		if pr.Merged && slices.Contains(pr.Labels, pendingLabel) {
			logger.Debug("merged pull request still pending, deferring to release",
				"head", branch, "number", pr.Number)
			return true, nil
		}
	}
	return false, nil
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

// ensurePendingLabel adds the pending label to a pull request when it is
// not already carrying it, never replacing the labels it has.
func ensurePendingLabel(ctx context.Context, f forge.Forge, owner, repoName string, number int, currentLabels []string, pending string) error {
	if slices.Contains(currentLabels, pending) {
		return nil
	}
	if err := f.AddLabels(ctx, owner, repoName, number, []string{pending}); err != nil {
		return fmt.Errorf("release-me: add label %q to release pull request #%d: %w", pending, number, err)
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
}

// fileUpdate is one configured file doPR bumps, with the version it bumps
// it to: the component's own, so components sharing a release branch at
// different versions each bump their own files to their own version.
type fileUpdate struct {
	file    config.File
	version string
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

// headingReadsBack reports whether the heading write renders for version
// and compareURL parses again, which the comparison in sameAsReleaseBranch
// relies on. The date is irrelevant to that, so any date does.
func headingReadsBack(version, compareURL string) bool {
	_, ok := changelog.ParseHeading(changelog.RenderHeading(version, time.Time{}, compareURL))
	return ok
}

// write commits the changelog update and the bumped files onto branch and
// pushes it, through the temporary worktree planned in p. A fresh branch,
// with no existing pull request to lease against, is pushed without
// force; otherwise the lease is the existing pull request's head SHA from
// the forge, not a locally fetched ref, so a concurrent fetch can never
// defeat it. The commit carries cfg's configured author and committer,
// never written into any git config, only passed to that one subprocess.
func (p *releasePlan) write(
	ctx context.Context, cfg config.Config, existing *forge.PullRequest, branch string, date time.Time, commitMessage string,
) error {
	if err := gitRun(ctx, p.tmp, "checkout", "--quiet", "-B", branch); err != nil {
		return fmt.Errorf("release-me: checkout %s: %w", branch, err)
	}

	for _, c := range p.changelogs {
		section := changelog.RenderHeading(c.version, date, c.compareURL) + "\n\n" + c.entriesBody + "\n"
		fullChangelogPath := filepath.Join(p.tmp, c.path)
		// A configured changelog may name a directory that does not exist
		// yet; os.WriteFile creates the file only.
		if err := os.MkdirAll(filepath.Dir(fullChangelogPath), 0o755); err != nil {
			return fmt.Errorf("release-me: write %s: %w", c.path, err)
		}
		if err := os.WriteFile(fullChangelogPath, []byte(changelog.Prepend(c.baseline, section)), 0o644); err != nil {
			return fmt.Errorf("release-me: write %s: %w", c.path, err)
		}
	}
	for path, content := range p.desiredFiles {
		if err := os.WriteFile(filepath.Join(p.tmp, path), []byte(content), 0o644); err != nil {
			return fmt.Errorf("release-me: write %s: %w", path, err)
		}
	}

	if err := gitRun(ctx, p.tmp, "add", "-A"); err != nil {
		return fmt.Errorf("release-me: stage release branch: %w", err)
	}
	if _, err := gitOutputEnv(ctx, p.tmp, commitEnv(cfg), "commit", "--quiet", "-m", commitMessage); err != nil {
		return fmt.Errorf("release-me: commit release branch: %w", err)
	}

	pushArgs := []string{"push", "--quiet", "origin", "HEAD:refs/heads/" + branch}
	if existing != nil && existing.HeadSHA != "" {
		pushArgs = []string{
			"push", "--force-with-lease=refs/heads/" + branch + ":" + existing.HeadSHA,
			"--quiet", "origin", "HEAD:refs/heads/" + branch,
		}
	}
	if err := gitRun(ctx, p.tmp, pushArgs...); err != nil {
		return fmt.Errorf("release-me: push %s: %w", branch, err)
	}
	return nil
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
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// commitEnv returns the environment for the commit subprocess: the
// process environment, extended with GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL,
// GIT_COMMITTER_NAME and GIT_COMMITTER_EMAIL from cfg.Author wherever the
// process environment does not already carry one of them. The identity
// never reaches git config, only this one subprocess's environment.
func commitEnv(cfg config.Config) []string {
	env := os.Environ()
	for key, value := range map[string]string{
		"GIT_AUTHOR_NAME":     cfg.Author.Name,
		"GIT_AUTHOR_EMAIL":    cfg.Author.Email,
		"GIT_COMMITTER_NAME":  cfg.Author.Name,
		"GIT_COMMITTER_EMAIL": cfg.Author.Email,
	} {
		if _, ok := os.LookupEnv(key); !ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

// resolvedAuthorName and resolvedAuthorEmail report the git author name
// and email the commit call would use: GIT_AUTHOR_NAME and
// GIT_AUTHOR_EMAIL from the process environment when set, cfg.Author
// otherwise, the same precedence commitEnv applies.
func resolvedAuthorName(cfg config.Config) string {
	if v, ok := os.LookupEnv("GIT_AUTHOR_NAME"); ok {
		return v
	}
	return cfg.Author.Name
}

func resolvedAuthorEmail(cfg config.Config) string {
	if v, ok := os.LookupEnv("GIT_AUTHOR_EMAIL"); ok {
		return v
	}
	return cfg.Author.Email
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
