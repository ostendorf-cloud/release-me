// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/history"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// sharedWho names the shared release pull request in an error, the way
// "locked family" names a locked one.
const sharedWho = "shared release pull request"

// errComponentNotAllowedForShared is returned when --component names a
// component for a command that writes the release pull request or tags it
// while every component shares one: the shared pull request carries every
// component with something to release, so limiting one run to one of them
// would drop the others from it.
var errComponentNotAllowedForShared = errors.New(
	"release-pull-request shared carries every component in one release pull request, --component is not allowed",
)

// rejectComponentForShared returns errComponentNotAllowedForShared when the
// release pull request is shared outside VersioningLocked, which
// rejectComponentForLocked already covers, and component names one. pr,
// release and run check it; next and changelog only read, so they keep
// --component.
func rejectComponentForShared(cfg config.Config, component string) error {
	if sharedOutsideLocked(cfg) && component != "" {
		return errComponentNotAllowedForShared
	}
	return nil
}

// sharedOutsideLocked reports whether cfg shares one release pull request
// across VersioningIndependent or VersioningLinked components, the mode
// doPRShared and doReleaseShared implement. VersioningLocked always shares
// one, through doPRLocked and doReleaseLocked.
func sharedOutsideLocked(cfg config.Config) bool {
	return cfg.Versioning != config.VersioningLocked && cfg.SharedPullRequest()
}

// sharedMember is one component with something to release this run in a
// shared release pull request: its computed release, the tag prefixes its
// pre-release counter is shared across, and, once overrides and the
// pre-release label are applied, the version, entries and starting tag it
// renders.
type sharedMember struct {
	comp       config.ResolvedComponent
	rel        release
	prefixes   []string
	versionStr string
	entries    []changelog.Entry
	fromTag    string
	overridden bool
}

// doPRShared creates or updates the one release pull request every
// component shares in VersioningIndependent or VersioningLinked with
// "release-pull-request: shared", branch "release-me/main": every component
// with something to release this run, each at its own version in
// VersioningIndependent and at the family's in VersioningLinked, with its
// own CHANGELOG.md section, its own file updates and its own slice of the
// combined body, laid out by release-pull-request-layout. A component with nothing to release is left
// out, the same as it gets no pull request of its own per component.
func doPRShared(
	ctx context.Context, dir string, cfg config.Config, owner, repoName, base string,
	f forge.Forge, logger *slog.Logger, dryRun bool, stdout io.Writer,
) error {
	all := cfg.Resolve()
	branch := all[0].Branch

	reportSupersededPRs(ctx, f, owner, repoName, all, branch, logger, stdout)

	members, familyLast, familyHasLast, err := sharedMembers(ctx, dir, cfg, all, owner, repoName, f, logger, stdout)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}

	existing, err := openReleasePR(ctx, f, owner, repoName, branch, sharedWho, logger)
	if err != nil {
		return err
	}
	if existing == nil {
		pending, err := mergedPendingRelease(ctx, f, owner, repoName, branch, cfg.Labels.Pending, logger)
		if err != nil {
			return fmt.Errorf("release-me: %s: %w", sharedWho, err)
		}
		if pending {
			fmt.Fprintln(stdout, "nothing to release")
			return nil
		}
	}

	perComponentVersion := cfg.Versioning == config.VersioningIndependent
	versionOverride := ""
	if existing != nil {
		versionOverride, _ = releasepr.ExtractVersionOverride(existing.Body)
		if versionOverride != "" {
			if perComponentVersion {
				if err := applyComponentOverrides(ctx, dir, members, versionOverride, existing.Number, logger); err != nil {
					return err
				}
			} else {
				applyFamilyOverride(ctx, members, versionOverride, familyLast, familyHasLast, existing.Number, logger)
			}
		}

		preType, isPre, err := selectPreReleaseType(existing.Labels, cfg.Labels)
		if err != nil {
			return fmt.Errorf("release-me: %s: pull request #%d: %w", sharedWho, existing.Number, err)
		}
		if isPre {
			for i := range members {
				m := &members[i]
				if m.overridden {
					continue
				}
				m.versionStr, m.entries, m.fromTag, err = preReleaseEntries(ctx, f, dir, cfg, m.comp, m.prefixes, owner, repoName, m.rel, preType, logger)
				if err != nil {
					return fmt.Errorf("release-me: component %s: %w", m.comp.Name, err)
				}
			}
		}
	}

	depFiles, err := applyWorkspaceUpdates(dir, cfg, all, members, logger)
	if err != nil {
		return err
	}

	titled := make([]releasepr.TitleRelease, 0, len(members))
	titledNames := make([]string, 0, len(members))
	for _, m := range members {
		titled = append(titled, releasepr.TitleRelease{Name: m.comp.Name, Version: m.versionStr})
		titledNames = append(titledNames, m.comp.Name)
	}
	patterns := cfg.SharedTitlePatterns()
	title, err := patterns[0].Render(releasepr.TitleValues{Releases: titled, Components: titledNames, Branch: base})
	if err != nil {
		return fmt.Errorf("release-me: %s: %w", sharedWho, err)
	}
	if err := checkSharedTitleLength(title, members); err != nil {
		return err
	}

	changelogs := make([]changelogUpdate, 0, len(members))
	var files []fileUpdate
	for _, m := range members {
		entriesBody := changelog.Sections(m.entries, m.rel.options)
		if entriesBody == "" {
			entriesBody = noUserFacingChangesLine
		}
		changelogs = append(changelogs, changelogUpdate{
			path: m.comp.ChangelogPath, version: m.versionStr, entriesBody: entriesBody,
			compareURL: compareURL(f, owner, repoName, m.fromTag, m.comp.TagPrefix+m.versionStr),
		})
		files = append(files, filesAt(m.comp.Files, m.versionStr)...)
	}
	files = append(files, depFiles...)

	plan, err := planReleaseBranch(ctx, dir, changelogs, files, branch)
	if err != nil {
		return fmt.Errorf("release-me: %s: %w", sharedWho, err)
	}
	defer plan.cleanup()

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

	// Every component's slice carries the heading its own CHANGELOG.md
	// gets, linked to its own compare view: the pull request has no one
	// version for a combined heading, and each component's GitHub release
	// renders its slice exactly the way a release of its own would.
	parts := make([]releasepr.ComponentChangelog, 0, len(members))
	releases := make([]releasepr.Release, 0, len(members))
	for i, m := range members {
		c := changelogs[i]
		parts = append(parts, releasepr.ComponentChangelog{
			Name: m.comp.Name, Version: m.versionStr,
			Section: changelog.RenderHeading(c.version, date, c.compareURL, cfg.ChangelogStyle.DateFormat) + "\n\n" + c.entriesBody,
		})
		releases = append(releases, releasepr.Release{Name: m.comp.Name, Version: m.versionStr, Tag: m.comp.TagPrefix + m.versionStr})
	}
	section := releasepr.RenderComponents(releasepr.Layout(cfg.ReleasePullRequestLayout), parts) + "\n"

	body := releasepr.SharedBody(releases, bodyGuide(cfg), perComponentVersion, notes, versionOverride, section) +
		releasepr.TitlePatternMarker(patterns[0], cfg.SharedTitleKind()) +
		releasepr.DateMarker(date, cfg.ChangelogStyle.DateFormat)
	names := make([]string, 0, len(members))
	for _, m := range members {
		names = append(names, m.comp.Name)
	}
	return publishSharedPR(ctx, cfg, f, owner, repoName, base, branch, sharedWho, names, existing, plan, date, title, body, dryRun, stdout)
}

// checkSharedTitleLength refuses a shared release pull request title past
// forge.MaxTitleLength, before anything is pushed or written: release
// reads every component and version back from the title after the merge,
// so a title the forge rejects or truncates would leave the merged
// components untagged.
func checkSharedTitleLength(title string, members []sharedMember) error {
	n := utf8.RuneCountInString(title)
	if n <= forge.MaxTitleLength {
		return nil
	}
	names := make([]string, 0, len(members))
	for _, m := range members {
		names = append(names, m.comp.Name)
	}
	return fmt.Errorf(
		"release-me: %s: title is %d characters, past the forge's limit of %d, for components %s: "+
			"release reads the versions back from the title, so it cannot be shortened; "+
			"release more often so fewer components share one pull request, or use release-pull-request: per-component",
		sharedWho, n, forge.MaxTitleLength, strings.Join(names, ", "),
	)
}

// sharedMembers computes every component's release for a shared release
// pull request, in release-me.yaml's order, printing "nothing to release"
// for each one left out: computeReleases, every component on its own, in
// VersioningIndependent, the family's releases in VersioningLinked. last
// and hasLast are the family's highest released version, the one a
// linked family's override is checked against.
func sharedMembers(
	ctx context.Context, dir string, cfg config.Config, all []config.ResolvedComponent, owner, repoName string,
	f forge.Forge, logger *slog.Logger, stdout io.Writer,
) (members []sharedMember, last version.Semver, hasLast bool, err error) {
	if cfg.Versioning == config.VersioningLinked {
		family, err := gatherFamily(ctx, dir, cfg, owner, repoName, f, logger)
		if err != nil {
			return nil, version.Semver{}, false, err
		}
		releases, released := familyReleases(cfg, family, logger)
		var dependents []string
		if released {
			var names []string
			for name := range releases {
				names = append(names, name)
			}
			if dependents, err = sharedDependents(dir, cfg, all, names); err != nil {
				return nil, version.Semver{}, false, err
			}
		}
		prefixes := familyTagPrefixes(family)
		var familyResult version.Result
		for _, rel := range releases {
			familyResult = rel.version
			break
		}
		for _, fm := range family {
			rel, ok := releases[fm.comp.Name]
			if released && !ok && slices.Contains(dependents, fm.comp.Name) {
				// A dependent joins the family's release at the family's
				// version, the one version line linked shares.
				rel, ok = release{version: familyResult, options: changelogOptions(fm.comp)}, true
				if fm.ownHasTag {
					rel.fromTag = fm.ownTag
				}
			}
			if !released || !ok {
				printLine(stdout, fm.comp.Name, true, "nothing to release")
				continue
			}
			members = append(members, newSharedMember(fm.comp, rel, prefixes))
		}
		last, hasLast = familyLastVersion(family)
		return members, last, hasLast, nil
	}

	results, err := computeReleases(ctx, dir, cfg, all, owner, repoName, f, logger)
	if err != nil {
		return nil, version.Semver{}, false, err
	}
	var released []string
	for _, r := range results {
		if r.released {
			released = append(released, r.comp.Name)
		}
	}
	dependents, err := sharedDependents(dir, cfg, all, released)
	if err != nil {
		return nil, version.Semver{}, false, err
	}
	for _, r := range results {
		if !r.released && slices.Contains(dependents, r.comp.Name) {
			if r.rel, err = dependencyRelease(ctx, dir, r.comp); err != nil {
				return nil, version.Semver{}, false, err
			}
			r.released = true
		}
		if !r.released {
			printLine(stdout, r.comp.Name, true, "nothing to release")
			continue
		}
		members = append(members, newSharedMember(r.comp, r.rel, []string{r.comp.TagPrefix}))
	}
	return members, version.Semver{}, false, nil
}

// applyWorkspaceUpdates adds every member's workspace dependency entries to
// its entries, once every member's version is final, and returns the
// manifest updates moving each dependent to its dependencies' versions,
// none without "workspaces".
func applyWorkspaceUpdates(
	dir string, cfg config.Config, all []config.ResolvedComponent, members []sharedMember, logger *slog.Logger,
) ([]fileUpdate, error) {
	if len(cfg.Workspaces) == 0 {
		return nil, nil
	}
	manifests, err := readWorkspace(dir, cfg, all)
	if err != nil {
		return nil, err
	}
	versions := make(map[string]string, len(members))
	for _, m := range members {
		versions[m.comp.Name] = m.versionStr
	}
	files, entries := workspaceUpdates(manifests, versions, logger)
	for i := range members {
		m := &members[i]
		m.entries = append(slices.Clone(m.entries), entries[m.comp.Name]...)
	}
	return files, nil
}

// sharedDependents returns every component that releases with released
// through "workspaces", none without the key.
func sharedDependents(dir string, cfg config.Config, all []config.ResolvedComponent, released []string) ([]string, error) {
	if len(cfg.Workspaces) == 0 || len(released) == 0 {
		return nil, nil
	}
	manifests, err := readWorkspace(dir, cfg, all)
	if err != nil {
		return nil, err
	}
	return workspaceDependents(manifests, released), nil
}

// newSharedMember starts a sharedMember at its computed release.
func newSharedMember(comp config.ResolvedComponent, rel release, prefixes []string) sharedMember {
	return sharedMember{
		comp: comp, rel: rel, prefixes: prefixes,
		versionStr: rel.version.Version, entries: rel.entries, fromTag: rel.fromTag,
	}
}

// applyFamilyOverride applies raw, a linked family's release-me-version
// block, to every member at once, checked against the family's highest
// released version, the same rule a locked family's override follows.
func applyFamilyOverride(
	ctx context.Context, members []sharedMember, raw string, last version.Semver, hasLast bool, number int, logger *slog.Logger,
) {
	overridden, ok := checkVersionOverride(raw, members[0].versionStr, last, hasLast, logger, "linked family", number)
	recordOverrideOutcome(ctx, number, "", ok)
	if !ok {
		return
	}
	for i := range members {
		members[i].versionStr = overridden
		members[i].overridden = true
	}
}

// applyComponentOverrides applies raw, the release-me-version block of a
// shared release pull request with independent versions, to every member
// a "<component> X.Y.Z" line names, each checked against that component's
// own last released version. An unscoped "X.Y.Z" line applies when the
// pull request releases exactly one component and is ignored, with a
// warning, when it releases several: there is no telling which one it
// meant. A line naming a component the pull request does not release is
// ignored with a warning too.
func applyComponentOverrides(ctx context.Context, dir string, members []sharedMember, raw string, number int, logger *slog.Logger) error {
	byName, unscoped := releasepr.VersionOverrides(raw)
	for name := range byName {
		if !slices.ContainsFunc(members, func(m sharedMember) bool { return m.comp.Name == name }) {
			logger.Warn("ignoring a release-me-version override for a component this pull request does not release",
				"component", name, "number", number)
		}
	}
	if unscoped != "" && len(members) > 1 {
		logger.Warn("ignoring a release-me-version override without a component: this pull request releases several, "+
			"name one per line as \"<component> X.Y.Z\"", "number", number, "value", unscoped)
	}

	for i := range members {
		m := &members[i]
		value, ok := byName[m.comp.Name]
		if !ok && unscoped != "" && len(members) == 1 {
			value, ok = unscoped, true
		}
		if !ok {
			continue
		}
		_, last, hasLast, err := history.LastTag(ctx, dir, m.comp.TagPrefix)
		if err != nil {
			return fmt.Errorf("release-me: component %s: last tag: %w", m.comp.Name, err)
		}
		overridden, applied := checkVersionOverride(value, m.versionStr, last, hasLast, logger, m.comp.Name, number)
		recordOverrideOutcome(ctx, number, m.comp.Name, applied)
		if applied {
			m.versionStr, m.overridden = overridden, true
		}
	}
	return nil
}

// openReleasePR returns the open release pull request on branch, nil when
// there is none, the first one with a warning when there are several. who
// names the component, locked family or shared release pull request in an
// error.
func openReleasePR(ctx context.Context, f forge.Forge, owner, repoName, branch, who string, logger *slog.Logger) (*forge.PullRequest, error) {
	list, err := f.PullRequests(ctx, owner, repoName, branch, "open")
	if err != nil {
		return nil, fmt.Errorf("release-me: %s: list release pull requests: %w", who, err)
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		logger.Warn("more than one open release pull request, using the first",
			"component", who, "head", branch, "count", len(list), "number", list[0].Number)
	}
	return &list[0], nil
}

// reportSupersededPRs names every release pull request still open on a
// component's own branch, "release-me/<name>", left over from before
// "release-pull-request: shared": the shared one on sharedBranch replaces
// it, and nothing updates or tags it any more once it is closed by hand. A
// component whose own branch is sharedBranch, one named "main", has
// nothing to report. release still tags such a pull request when it is
// merged rather than closed; see doReleaseShared. The report is only a
// migration hint, so a forge failure listing one branch is logged and the
// rest still run: it never stops pr from writing the shared one.
func reportSupersededPRs(
	ctx context.Context, f forge.Forge, owner, repoName string, all []config.ResolvedComponent, sharedBranch string,
	logger *slog.Logger, stdout io.Writer,
) {
	for _, comp := range all {
		own := config.PerComponentBranch(comp.Name)
		if own == sharedBranch {
			continue
		}
		list, err := f.PullRequests(ctx, owner, repoName, own, "open")
		if err != nil {
			logger.Warn("could not look for a superseded per-component release pull request, continuing",
				"component", comp.Name, "head", own, "error", err)
			continue
		}
		for _, pr := range list {
			logger.Warn("a per-component release pull request is superseded by the shared one",
				"component", comp.Name, "number", pr.Number, "head", own, "shared", sharedBranch)
			printLine(stdout, comp.Name, true, fmt.Sprintf(
				"release pull request #%d on %s is superseded by the shared one on %s, close it", pr.Number, own, sharedBranch,
			))
		}
	}
}

// bodyGuide returns the guide every release pull request body renders: shown
// unless "release-pull-request-guide: false", naming the configured labels.
func bodyGuide(cfg config.Config) releasepr.Guide {
	return releasepr.Guide{
		Show:   cfg.ReleasePullRequestGuide,
		Labels: releasepr.GuideLabels{Tagged: cfg.Labels.Tagged, Alpha: cfg.Labels.Alpha, Beta: cfg.Labels.Beta, Rc: cfg.Labels.Rc},
	}
}
