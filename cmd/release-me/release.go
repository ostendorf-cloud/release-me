// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/conventional"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/history"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// release is the computed next version and changelog entries for one
// component, ready for the next and changelog commands to print.
type release struct {
	version version.Result
	entries []changelog.Entry
	// options is how the component's entries render, changelogOptions of
	// it.
	options changelog.Options
	// fromTag is the tag entries are collected since, the one the
	// changelog heading's compare link starts at: the component's last
	// stable tag, or, for a pre-release with an earlier one of the same
	// base, that earlier pre-release tag. "" for a component's first
	// release, which links nowhere.
	fromTag string
}

// compareURL returns the forge's compare view from fromTag, a release's
// own fromTag, to toTag, the tag that release will get, for the changelog
// heading to link to. "" when fromTag is: a first release has nothing to
// compare against.
func compareURL(f forge.Forge, owner, repoName, fromTag, toTag string) string {
	if fromTag == "" {
		return ""
	}
	return f.CompareURL(owner, repoName, fromTag, toTag)
}

// componentRelease is one component's outcome of computeReleases.
type componentRelease struct {
	comp     config.ResolvedComponent
	rel      release
	released bool
}

// computeReleases computes every one of components' releases on its own,
// for config.VersioningIndependent, in order. The components share one
// attribution and with it one pullRequestCache, the way gatherFamily's
// members do: the loop is one computation with no write in between, so a
// pull request belonging to several components is read once and an
// unknown footer name warns once.
func computeReleases(
	ctx context.Context, dir string, cfg config.Config, components []config.ResolvedComponent, owner, repoName string, f forge.Forge, logger *slog.Logger,
) ([]componentRelease, error) {
	attr := newAttribution(dir, cfg, owner, repoName, newPullRequestCache(f), logger)
	out := make([]componentRelease, 0, len(components))
	for _, comp := range components {
		rel, released, err := computeReleaseWith(ctx, dir, cfg, comp, attr, logger)
		if err != nil {
			return nil, err
		}
		out = append(out, componentRelease{comp: comp, rel: rel, released: released})
	}
	return out, nil
}

// computeReleaseWith wires history, the forge and the conventional and
// version packages into one release for comp, reading its entries and
// their pull requests through attr, which may be shared with other
// components of the same computation. released is false when there is
// nothing to release.
func computeReleaseWith(
	ctx context.Context, dir string, cfg config.Config, comp config.ResolvedComponent, attr *attribution, logger *slog.Logger,
) (release, bool, error) {
	tag, last, hasTag, err := history.LastTag(ctx, dir, comp.TagPrefix)
	if err != nil {
		return release{}, false, fmt.Errorf("release-me: component %s: last tag: %w", comp.Name, err)
	}
	logger.Debug("last tag", "component", comp.Name, "tag", tag, "found", hasTag)

	since := ""
	if hasTag {
		since = tag
	}
	raw, err := walkEntries(ctx, dir, since, comp.ChangelogSource)
	if err != nil {
		return release{}, false, fmt.Errorf("release-me: component %s: history: %w", comp.Name, err)
	}
	raw, err = attr.filter(ctx, raw, comp)
	if err != nil {
		return release{}, false, fmt.Errorf("release-me: component %s: %w", comp.Name, err)
	}
	logger.Debug("history entries", "component", comp.Name, "count", len(raw), "source", comp.ChangelogSource)

	changes, entries, err := assembleEntries(ctx, attr.forge, attr.owner, attr.repoName, raw, cfg.FallbackType, comp.ChangelogSource, attr.subCommits)
	if err != nil {
		return release{}, false, fmt.Errorf("release-me: component %s: %w", comp.Name, err)
	}

	result, released, refused := version.Next(last, hasTag, changes, comp.Rules())
	warnRefusedReleaseAs(logger, comp.Name, refused)
	if !released {
		return release{}, false, nil
	}
	return release{version: result, entries: entries, options: changelogOptions(comp), fromTag: since}, true, nil
}

// walkEntries reads the raw history entries for since, exclusive, to HEAD,
// through the history function source selects: history.Walk for
// config.ChangelogSourceFirstParent and config.ChangelogSourceNested, both
// first parent walks; history.WalkSquash for config.ChangelogSourceSquash;
// history.AllEntries for config.ChangelogSourceAll.
func walkEntries(ctx context.Context, dir, since, source string) ([]history.Entry, error) {
	switch source {
	case config.ChangelogSourceSquash:
		return history.WalkSquash(ctx, dir, since)
	case config.ChangelogSourceAll:
		return history.AllEntries(ctx, dir, since)
	default:
		return history.Walk(ctx, dir, since)
	}
}

// assembleEntries resolves each history entry's text through the forge when
// it names a pull request, falling back to the commit's own text when the
// forge has no pull request for it, then parses the result as a
// conventional commit, unparseable text falling back to fallbackType. A
// pull request carrying a "release-me" override block contributes its
// entries instead, one Conventional Commits line per entry; an empty block
// hides it. Both apply in every changelog source mode.
//
// config.ChangelogSourceAll takes a different shape, assembleAllEntries's:
// raw already carries one entry per commit, with the pull request it
// belongs to, if any, resolved once per group instead of once per entry.
// subs reads a merge commit's sub commits in config.ChangelogSourceNested;
// every other mode leaves it unused, so it may be nil there.
func assembleEntries(
	ctx context.Context, f forge.Forge, owner, repoName string, raw []history.Entry, fallbackType, source string, subs subCommitReader,
) ([]version.Change, []changelog.Entry, error) {
	if source == config.ChangelogSourceAll {
		return assembleAllEntries(ctx, f, owner, repoName, raw, fallbackType)
	}

	changes := make([]version.Change, 0, len(raw))
	entries := make([]changelog.Entry, 0, len(raw))

	for _, e := range raw {
		c, en, err := processEntry(ctx, f, owner, repoName, e, fallbackType, source, subs)
		if err != nil {
			return nil, nil, err
		}
		changes = append(changes, c...)
		entries = append(entries, en...)
	}
	return changes, entries, nil
}

// processEntry resolves one history entry into zero or more changes and
// changelog entries. In config.ChangelogSourceNested, a merge entry with no
// override additionally collects its merge's sub commits.
func processEntry(
	ctx context.Context, f forge.Forge, owner, repoName string, e history.Entry, fallbackType, source string, subs subCommitReader,
) ([]version.Change, []changelog.Entry, error) {
	text, url := entryText(e), ""

	if e.PullRequestNumber > 0 {
		pr, err := f.PullRequest(ctx, owner, repoName, e.PullRequestNumber)
		switch {
		case err == nil:
			if override := conventional.ParseOverride(pr.Body); override.Present {
				c, en := overrideEntries(override, e.PullRequestNumber, pr.URL, fallbackType)
				return c, en, nil
			}
			text, url = pullRequestText(e, pr.Title, pr.Body), pr.URL
		case errors.Is(err, forge.ErrNotFound):
			text, url = unresolvedText(e), pr.URL
		default:
			return nil, nil, fmt.Errorf("release-me: pull request #%d: %w", e.PullRequestNumber, err)
		}
	}

	msg := conventional.Parse(text, fallbackType)
	if msg.ReleaseAs == "" && e.PullRequestNumber > 0 {
		// The text resolved through the forge is the pull request's title
		// and body: a footer in the commit's own message, a squash merge's
		// commit body for one, still counts.
		msg.ReleaseAs = conventional.Parse(entryText(e), fallbackType).ReleaseAs
	}
	changes := []version.Change{{Type: msg.Type, Breaking: msg.Breaking, ReleaseAs: msg.ReleaseAs}}
	entry := changelog.Entry{
		Type:              msg.Type,
		Scope:             msg.Scope,
		Description:       msg.Description,
		Breaking:          msg.Breaking,
		PullRequestNumber: e.PullRequestNumber,
		URL:               url,
	}

	if source == config.ChangelogSourceNested && e.PullRequestNumber > 0 {
		subChanges, subEntries, err := nestedSubEntries(ctx, subs, e.SHA, fallbackType)
		if err != nil {
			return nil, nil, err
		}
		changes = append(changes, subChanges...)
		entry.SubEntries = subEntries
	}

	return changes, []changelog.Entry{entry}, nil
}

// subCommitReader returns the sub commits of the merge commit sha, the
// shape history.SubCommits returns. attribution.subCommits is one, reading
// each merge once.
type subCommitReader func(ctx context.Context, sha string) ([]history.Entry, error)

// nestedSubEntries parses the sub commits of the merge commit sha, read
// through subs, each on its own, oldest first. It returns no error for a
// commit with no sub commits, the nil slice a single parent commit yields.
func nestedSubEntries(ctx context.Context, subs subCommitReader, sha, fallbackType string) ([]version.Change, []changelog.Entry, error) {
	commits, err := subs(ctx, sha)
	if err != nil {
		return nil, nil, err
	}

	changes := make([]version.Change, 0, len(commits))
	entries := make([]changelog.Entry, 0, len(commits))
	for _, s := range commits {
		msg := conventional.Parse(entryText(s), fallbackType)
		changes = append(changes, version.Change{Type: msg.Type, Breaking: msg.Breaking, ReleaseAs: msg.ReleaseAs})
		entries = append(entries, changelog.Entry{
			Type: msg.Type, Scope: msg.Scope, Description: msg.Description, Breaking: msg.Breaking,
		})
	}
	return changes, entries, nil
}

// assembleAllEntries assembles history.AllEntries's shape for
// config.ChangelogSourceAll: every commit is already its own Entry, with
// the pull request it belongs to, if any, already resolved. Commits
// sharing a pull request number are processed as one group: the forge is
// read once per distinct number, and an override block there replaces the
// whole group's entries, not just the first commit's.
func assembleAllEntries(ctx context.Context, f forge.Forge, owner, repoName string, raw []history.Entry, fallbackType string) ([]version.Change, []changelog.Entry, error) {
	groups := make(map[int][]history.Entry, len(raw))
	for _, e := range raw {
		if e.PullRequestNumber > 0 {
			groups[e.PullRequestNumber] = append(groups[e.PullRequestNumber], e)
		}
	}

	changes := make([]version.Change, 0, len(raw))
	entries := make([]changelog.Entry, 0, len(raw))
	resolved := map[int]bool{}

	for _, e := range raw {
		if e.PullRequestNumber == 0 {
			c, en := parseCommit(e, fallbackType, 0, "")
			changes = append(changes, c)
			entries = append(entries, en)
			continue
		}
		if resolved[e.PullRequestNumber] {
			continue
		}
		resolved[e.PullRequestNumber] = true

		group := groups[e.PullRequestNumber]
		pr, err := f.PullRequest(ctx, owner, repoName, e.PullRequestNumber)
		switch {
		case err == nil:
			if override := conventional.ParseOverride(pr.Body); override.Present {
				c, en := overrideEntries(override, e.PullRequestNumber, pr.URL, fallbackType)
				changes = append(changes, c...)
				entries = append(entries, en...)
				continue
			}
			for _, g := range group {
				c, en := parseCommit(g, fallbackType, e.PullRequestNumber, pr.URL)
				changes = append(changes, c)
				entries = append(entries, en)
			}
			// A footer in the pull request's own body forces a version
			// the same way one on any of its commits does, without an
			// entry of its own.
			if v := conventional.Parse(prText(pr.Title, pr.Body), fallbackType).ReleaseAs; v != "" {
				changes = append(changes, version.Change{ReleaseAs: v})
			}
		case errors.Is(err, forge.ErrNotFound):
			for _, g := range group {
				c, en := parseCommit(g, fallbackType, e.PullRequestNumber, "")
				changes = append(changes, c)
				entries = append(entries, en)
			}
		default:
			return nil, nil, fmt.Errorf("release-me: pull request #%d: %w", e.PullRequestNumber, err)
		}
	}
	return changes, entries, nil
}

// parseCommit parses one commit's own subject and body as a conventional
// commit, attributing it to the given pull request number and URL, 0 and
// "" for a commit belonging to none.
func parseCommit(e history.Entry, fallbackType string, number int, url string) (version.Change, changelog.Entry) {
	msg := conventional.Parse(entryText(e), fallbackType)
	return version.Change{Type: msg.Type, Breaking: msg.Breaking, ReleaseAs: msg.ReleaseAs}, changelog.Entry{
		Type:              msg.Type,
		Scope:             msg.Scope,
		Description:       msg.Description,
		Breaking:          msg.Breaking,
		PullRequestNumber: number,
		URL:               url,
	}
}

// warnRefusedReleaseAs logs every "Release-As" footer and "release-as"
// key value version.Next refused, never failing the run: the value is
// ignored and the computed bump applies, the same way pr treats a release-me-version override that
// is not greater than the last released version. who names the component
// or family.
func warnRefusedReleaseAs(logger *slog.Logger, who string, refused []version.Refusal) {
	for _, r := range refused {
		logger.Warn("ignoring a "+r.Source+": the value "+r.Reason, "component", who, "value", r.Value)
	}
}

// overrideEntries parses one change and one changelog entry per line of an
// override block, all naming the same pull request.
func overrideEntries(override conventional.Override, number int, url, fallbackType string) ([]version.Change, []changelog.Entry) {
	changes := make([]version.Change, 0, len(override.Entries))
	entries := make([]changelog.Entry, 0, len(override.Entries))
	for _, line := range override.Entries {
		msg := conventional.Parse(line, fallbackType)
		changes = append(changes, version.Change{Type: msg.Type, Breaking: msg.Breaking})
		entries = append(entries, changelog.Entry{
			Type:              msg.Type,
			Scope:             msg.Scope,
			Description:       msg.Description,
			Breaking:          msg.Breaking,
			PullRequestNumber: number,
			URL:               url,
		})
	}
	return changes, entries
}

func entryText(e history.Entry) string {
	if e.Body == "" {
		return e.Subject
	}
	return e.Subject + "\n" + e.Body
}

func prText(title, body string) string {
	if body == "" {
		return title
	}
	return title + "\n" + body
}

// pullRequestText is the text processEntry parses once the forge resolves
// e's pull request, title and body. For e.Squashed, set by WalkSquash for
// a commit resolved from its own trailing "(#N)", e.Subject is already the
// real commit title: the forge only adds body, never replaces the title,
// so a breaking change footer there still counts. Every other entry, a
// genuine merge commit included, uses the pull request's own title,
// unchanged.
func pullRequestText(e history.Entry, title, body string) string {
	if e.Squashed {
		return prText(e.Subject, body)
	}
	return prText(title, body)
}

// unresolvedText is the text processEntry parses when the forge has no
// pull request for e's number. For e.Squashed, e.Subject is the real
// commit title and is never dropped, forge reachable or not; e's own
// body, if any, still follows it. Every other entry, a genuine merge
// commit included, falls back to e's own body alone, its subject being
// boilerplate on its own.
func unresolvedText(e history.Entry) string {
	if e.Squashed {
		return entryText(e)
	}
	return e.Body
}
