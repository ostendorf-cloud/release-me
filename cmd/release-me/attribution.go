// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/conventional"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/history"
)

// attribution decides which history entries count for a component: by the
// files each changed, history.Touches, and by the components its
// "Release-Component" footers name, through history.Counts.
//
// A footer counts wherever release-me reads the entry's text, the same
// places a "Release-As" footer counts: the commit's own message, the body
// of the pull request it belongs to, and, in config.ChangelogSourceNested,
// the messages of a merge commit's sub commits.
type attribution struct {
	dir, owner, repoName string
	forge                forge.Forge
	known                []string
	logger               *slog.Logger
	// warned holds the commit and name of every unknown footer value
	// already logged, so one commit read twice warns once.
	warned map[string]bool
	// subs holds every merge commit's sub commits already read, by SHA,
	// see subCommits.
	subs map[string][]history.Entry
}

// newAttribution returns the attribution for one run over cfg's
// components. f should be the same forge the run resolves pull request
// text through, a pullRequestCache, so the attribution's reads cost no
// extra request.
func newAttribution(dir string, cfg config.Config, owner, repoName string, f forge.Forge, logger *slog.Logger) *attribution {
	var known []string
	for _, c := range cfg.Resolve() {
		known = append(known, c.Name)
	}
	return &attribution{
		dir: dir, owner: owner, repoName: repoName, forge: f, known: known, logger: logger,
		warned: map[string]bool{}, subs: map[string][]history.Entry{},
	}
}

// filter keeps only the entries that count for comp. A root component
// beside others owns every file outside their paths, so their paths count
// as excluded for it on top of its own "exclude-paths". comp.Path "."
// alone, the one component of its repository, with neither, keeps every
// entry without reading any commit's touched files or footers: no footer
// can change what counts for it, and a footer naming an unknown component
// goes without a warning there.
func (a *attribution) filter(ctx context.Context, entries []history.Entry, comp config.ResolvedComponent) ([]history.Entry, error) {
	if comp.Path == "." && len(comp.OtherPaths) == 0 && len(comp.ExcludePaths) == 0 {
		return entries, nil
	}
	exclude := append(slices.Clone(comp.OtherPaths), comp.ExcludePaths...)
	filtered := make([]history.Entry, 0, len(entries))
	for _, e := range entries {
		named, err := a.named(ctx, e, comp)
		if err != nil {
			return nil, err
		}
		paths, err := history.Touches(ctx, a.dir, e.SHA)
		if err != nil {
			return nil, fmt.Errorf("commit %s: %w", e.SHA, err)
		}
		if history.Counts(paths, named, comp.Name, comp.Path, exclude) {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

// named returns every component name e's "Release-Component" footers name,
// each once, warning about every name no component carries. An unknown
// name stays in the result: history.Counts matches it against no
// component, so an empty commit naming only unknown components counts for
// none instead of for every one.
func (a *attribution) named(ctx context.Context, e history.Entry, comp config.ResolvedComponent) ([]string, error) {
	texts := []string{entryText(e)}
	if e.PullRequestNumber > 0 {
		pr, err := a.forge.PullRequest(ctx, a.owner, a.repoName, e.PullRequestNumber)
		switch {
		case err == nil:
			texts = append(texts, prText(pr.Title, pr.Body))
		case errors.Is(err, forge.ErrNotFound):
		default:
			return nil, fmt.Errorf("release-me: pull request #%d: %w", e.PullRequestNumber, err)
		}
		if comp.ChangelogSource == config.ChangelogSourceNested {
			subs, err := a.subCommits(ctx, e.SHA)
			if err != nil {
				return nil, err
			}
			for _, s := range subs {
				texts = append(texts, entryText(s))
			}
		}
	}

	var named []string
	for _, text := range texts {
		for _, name := range conventional.Parse(text, "").Components {
			if slices.Contains(named, name) {
				continue
			}
			if !slices.Contains(a.known, name) {
				a.warnUnknown(e, name)
			}
			named = append(named, name)
		}
	}
	return named, nil
}

// subCommits returns the sub commits of the merge commit sha through
// history.SubCommits, reading each merge once per attribution: named and
// nestedSubEntries both read them, once per component.
func (a *attribution) subCommits(ctx context.Context, sha string) ([]history.Entry, error) {
	if subs, ok := a.subs[sha]; ok {
		return subs, nil
	}
	subs, err := history.SubCommits(ctx, a.dir, sha)
	if err != nil {
		return nil, fmt.Errorf("release-me: merge commit %s: sub commits: %w", sha, err)
	}
	a.subs[sha] = subs
	return subs, nil
}

// warnUnknown logs a "Release-Component" footer naming no configured
// component, once per commit and name, never failing the run, the same way
// a refused "Release-As" footer is: the name reaches no component, so the
// commit counts only for the components its files or its other names
// reach, an empty commit for none.
func (a *attribution) warnUnknown(e history.Entry, name string) {
	key := e.SHA + "\x00" + name
	if a.warned[key] {
		return
	}
	a.warned[key] = true
	attrs := []any{"commit", e.SHA, "value", name, "components", strings.Join(a.known, ",")}
	if e.PullRequestNumber > 0 {
		attrs = append(attrs, "pull_request", e.PullRequestNumber)
	}
	a.logger.Warn("Release-Component footer names no configured component: the name reaches no component, "+
		"an empty commit naming only unknown components counts for none", attrs...)
}

// pullRequestCache is a forge.Forge that reads every pull request once:
// attribution and assembleEntries both resolve an entry's pull request,
// and the second read is served from memory. Only PullRequest is cached,
// every other method goes to the wrapped forge unchanged.
type pullRequestCache struct {
	forge.Forge
	prs map[pullRequestKey]cachedPullRequest
}

type pullRequestKey struct {
	owner, repo string
	number      int
}

type cachedPullRequest struct {
	pr  forge.PullRequest
	err error
}

// newPullRequestCache wraps f in a pullRequestCache. A run should keep the
// cache for the reads of one computation only, never across a write to the
// pull requests it caches.
func newPullRequestCache(f forge.Forge) *pullRequestCache {
	return &pullRequestCache{Forge: f, prs: map[pullRequestKey]cachedPullRequest{}}
}

// PullRequest implements forge.Forge, reading each pull request from the
// wrapped forge once, its error included.
func (c *pullRequestCache) PullRequest(ctx context.Context, owner, repo string, number int) (forge.PullRequest, error) {
	key := pullRequestKey{owner: owner, repo: repo, number: number}
	if hit, ok := c.prs[key]; ok {
		return hit.pr, hit.err
	}
	pr, err := c.Forge.PullRequest(ctx, owner, repo, number)
	c.prs[key] = cachedPullRequest{pr: pr, err: err}
	return pr, err
}
