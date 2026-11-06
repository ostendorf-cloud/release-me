// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/history"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// familyMember is one component's own changes and changelog entries since
// its own last tag, the per-component state a linked or locked family run
// folds into one shared version. It never computes its own next version:
// that is for familyVersion to do across the whole family.
type familyMember struct {
	comp       config.ResolvedComponent
	changes    []version.Change
	entries    []changelog.Entry
	ownTag     string
	ownVersion version.Semver
	ownHasTag  bool
}

// gatherFamily computes every component's own changes and changelog
// entries, always over release-me.yaml's full "components" list,
// regardless of a --component filter: the family's next version depends
// on every component's own state, even one a filtered run will not act on.
func gatherFamily(
	ctx context.Context, dir string, cfg config.Config, owner, repoName string, f forge.Forge, logger *slog.Logger,
) ([]familyMember, error) {
	components := cfg.Resolve()
	f = newPullRequestCache(f)
	attr := newAttribution(dir, cfg, owner, repoName, f, logger)
	members := make([]familyMember, 0, len(components))
	for _, comp := range components {
		tag, last, hasTag, err := history.LastTag(ctx, dir, comp.TagPrefix)
		if err != nil {
			return nil, fmt.Errorf("release-me: component %s: last tag: %w", comp.Name, err)
		}
		logger.Debug("last tag", "component", comp.Name, "tag", tag, "found", hasTag)

		since := ""
		if hasTag {
			since = tag
		}
		raw, err := walkEntries(ctx, dir, since, comp.ChangelogSource)
		if err != nil {
			return nil, fmt.Errorf("release-me: component %s: history: %w", comp.Name, err)
		}
		raw, err = attr.filter(ctx, raw, comp)
		if err != nil {
			return nil, fmt.Errorf("release-me: component %s: %w", comp.Name, err)
		}
		logger.Debug("history entries", "component", comp.Name, "count", len(raw), "source", comp.ChangelogSource)

		changes, entries, err := assembleEntries(ctx, f, owner, repoName, raw, cfg.FallbackType, comp.ChangelogSource, attr.subCommits)
		if err != nil {
			return nil, fmt.Errorf("release-me: component %s: %w", comp.Name, err)
		}
		members = append(members, familyMember{
			comp: comp, changes: changes, entries: entries,
			ownTag: tag, ownVersion: last, ownHasTag: hasTag,
		})
	}
	return members, nil
}

// familyVersion computes the next version for a linked or locked family
// run: bump(highest version in the family, highest bump type among the
// changed components). released is false when no member has anything to
// release.
//
// A "Release-As" footer on any member's own changes forces the family's
// version, the same way a release-as override on the locked family's
// shared release pull request does, checked against the family's highest
// version.
func familyVersion(cfg config.Config, members []familyMember, logger *slog.Logger) (result version.Result, released bool) {
	highest, highestOK := familyLastVersion(members)

	var changes []version.Change
	for _, m := range members {
		changes = append(changes, m.changes...)
	}
	result, released, refused := version.Next(highest, highestOK, changes, cfg.Rules())
	warnRefusedReleaseAs(logger, string(cfg.Versioning)+" family", refused)
	return result, released
}

// familyLastVersion returns the highest version any member of the family
// already carries its own tag at, ok false when no member has one yet.
// It is the same "last released version" a locked family's release-as
// override is checked against, one tag prefix at a time, that
// applyVersionOverride already checks for a single, independent
// component.
func familyLastVersion(members []familyMember) (highest version.Semver, ok bool) {
	var tags []version.Semver
	for _, m := range members {
		if m.ownHasTag {
			tags = append(tags, m.ownVersion)
		}
	}
	return version.Highest(tags)
}

// familyReleases computes, for a linked or locked family run, the release
// every member that moves gets, keyed by component name: every changed
// member in VersioningLinked, every member once anything in the family
// changed in VersioningLocked, since locked releases every component
// together, changed or not. released is false when nothing in the family
// has anything to release. In VersioningLinked, a member whose own changes
// carry a usable "Release-As" footer counts as changed, even when none of
// them bumps the version.
func familyReleases(cfg config.Config, members []familyMember, logger *slog.Logger) (releases map[string]release, released bool) {
	result, released := familyVersion(cfg, members, logger)
	if !released {
		return nil, false
	}
	highest, highestOK := familyLastVersion(members)

	releases = make(map[string]release, len(members))
	for _, m := range members {
		if cfg.Versioning == config.VersioningLinked && !version.Changed(m.changes, cfg.VersioningStrategy) {
			if _, forces, _ := version.ReleaseAs(highest, highestOK, m.changes); !forces {
				continue
			}
		}
		fromTag := ""
		if m.ownHasTag {
			fromTag = m.ownTag
		}
		releases[m.comp.Name] = release{version: result, entries: m.entries, hiddenTypes: m.comp.HiddenTypes, fromTag: fromTag}
	}
	return releases, true
}

// familyTagPrefixes returns every member's tag prefix, the set a linked or
// locked family run shares one pre-release counter across.
func familyTagPrefixes(members []familyMember) []string {
	prefixes := make([]string, 0, len(members))
	for _, m := range members {
		prefixes = append(prefixes, m.comp.TagPrefix)
	}
	return prefixes
}
