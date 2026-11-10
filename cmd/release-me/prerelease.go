// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/history"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// selectPreReleaseType reads which of the enabled pre-release type labels,
// alpha, beta, rc and every further type "labels.prerelease" configures,
// prLabels carries. ok is false when none of them is present: the release
// stays stable, the next run's version returns to it the moment the label
// it carried is removed. err names every label present when more than one
// is.
func selectPreReleaseType(prLabels []string, lbl config.Labels) (preType string, ok bool, err error) {
	var found []config.PreReleaseType
	for _, k := range lbl.PreRelease {
		if k.Label != "" && slices.Contains(prLabels, k.Label) {
			found = append(found, k)
		}
	}
	switch len(found) {
	case 0:
		return "", false, nil
	case 1:
		return found[0].Type, true, nil
	default:
		names := make([]string, len(found))
		for i, k := range found {
			names[i] = k.Label
		}
		return "", false, fmt.Errorf("more than one pre-release label: %s", strings.Join(names, ", "))
	}
}

// preReleaseVersion computes the pre-release version for preType on top of
// base, the version a stable release would get, together with the history
// tag to collect changelog entries since. since is "" when ownTagPrefix
// carries no earlier pre-release tag yet for base: the caller then keeps
// the entries it already collected since the last stable tag. N starts at
// 1 for preType and counts up from the highest N an earlier pre-release
// tag of that same type and base already carries, scanning every prefix
// in familyTagPrefixes: the counter is shared across the whole family in a
// linked or locked run, ownTagPrefix on its own, as the only entry of
// familyTagPrefixes, for an independent component. A type change does not
// inherit the counter of the type it replaces.
func preReleaseVersion(
	ctx context.Context, dir, ownTagPrefix string, familyTagPrefixes []string, base version.Semver, preType string,
) (v version.Semver, since string, err error) {
	n := 1
	for _, prefix := range familyTagPrefixes {
		tags, err := history.PreReleaseTags(ctx, dir, prefix, base)
		if err != nil {
			return version.Semver{}, "", fmt.Errorf("release-me: pre-release tags: %w", err)
		}
		for _, t := range tags {
			typ, num, ok := t.Version.PreReleaseType()
			if ok && typ == preType && num+1 > n {
				n = num + 1
			}
		}
		if prefix == ownTagPrefix && len(tags) > 0 {
			since = tags[len(tags)-1].Tag
		}
	}
	return base.WithPreRelease(preType, n), since, nil
}

// preReleaseEntries computes the pre-release version string for preType on
// top of rel's stable version, together with the changelog entries to
// render for it and the tag they are collected since: the entries since
// the previous pre-release tag for that base version, and that tag, when
// one exists, rel's own entries, already collected since the last stable
// tag, and rel's own fromTag otherwise.
//
// A component on versioning-strategy service-pack is refused: its version
// already carries a pre-release identifier of its own, "X.Y.Z-sp.N", and a
// second one cannot be placed on top of it. The counter scans the tags of
// the base version, so a base that is itself a pre-release matches none and
// never counts up, and the type replaces "sp.N" wholesale, so the service
// pack the release is would be released as a plain pre-release of the last
// stable version instead, at the same tag on every run.
func preReleaseEntries(
	ctx context.Context, f forge.Forge, dir string, cfg config.Config, comp config.ResolvedComponent, familyTagPrefixes []string,
	owner, repoName string, rel release, preType string, logger *slog.Logger,
) (versionStr string, entries []changelog.Entry, fromTag string, err error) {
	if comp.VersioningStrategy == version.StrategyServicePack {
		return "", nil, "", fmt.Errorf(
			"versioning-strategy service-pack already releases the pre-release version %s, "+
				"a %s pre-release cannot be released on top of it: remove the pre-release label from the release pull request",
			rel.version.Version, preType,
		)
	}
	base, ok := version.ParseTag("v" + rel.version.Version)
	if !ok {
		return "", nil, "", fmt.Errorf("next version %q does not parse", rel.version.Version)
	}

	preVersion, since, err := preReleaseVersion(ctx, dir, comp.TagPrefix, familyTagPrefixes, base, preType)
	if err != nil {
		return "", nil, "", err
	}
	if since == "" {
		return preVersion.String(), rel.entries, rel.fromTag, nil
	}

	raw, err := walkEntries(ctx, dir, since, comp.ChangelogSource)
	if err != nil {
		return "", nil, "", fmt.Errorf("history since %s: %w", since, err)
	}
	f = newPullRequestCache(f)
	attr := newAttribution(dir, cfg, owner, repoName, f, logger)
	raw, err = attr.filter(ctx, raw, comp)
	if err != nil {
		return "", nil, "", err
	}
	_, restricted, err := assembleEntries(ctx, f, owner, repoName, raw, cfg.FallbackType, comp.ChangelogSource, attr.subCommits)
	if err != nil {
		return "", nil, "", err
	}
	return preVersion.String(), restricted, since, nil
}

// preReleaseLabelsByType maps every configured pre-release type to its
// label, for release to swap it alongside pending. A type turned off, with
// an empty label, is left out.
func preReleaseLabelsByType(lbl config.Labels) map[string]string {
	out := make(map[string]string, len(lbl.PreRelease))
	for _, t := range lbl.PreRelease {
		if t.Label != "" {
			out[t.Type] = t.Label
		}
	}
	return out
}
