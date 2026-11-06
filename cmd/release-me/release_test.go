// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/history"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// computeRelease computes comp's release on its own, for the repository
// owner/repo, through a fresh attribution and pullRequestCache over f, the
// one component shape most tests drive.
func computeRelease(
	ctx context.Context, dir string, cfg config.Config, comp config.ResolvedComponent, f forge.Forge, logger *slog.Logger,
) (release, bool, error) {
	return computeReleaseWith(ctx, dir, cfg, comp, newAttribution(dir, cfg, "owner", "repo", newPullRequestCache(f), logger), logger)
}

func TestAssembleEntries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		raw         []history.Entry
		responses   map[int]forgetest.Response
		wantChanges []version.Change
		wantEntries []changelog.Entry
	}{
		{
			name: "uses the pull request title when the forge finds it",
			raw: []history.Entry{
				{PullRequestNumber: 5, Subject: "Merge pull request #5 from owner/feat-branch", Body: "feat(cli): add the next command"},
			},
			responses: map[int]forgetest.Response{
				5: {PullRequest: forge.PullRequest{
					Title: "feat(cli): add the real next command",
					URL:   "https://github.com/owner/repo/pull/5",
				}},
			},
			wantChanges: []version.Change{{Type: "feat"}},
			wantEntries: []changelog.Entry{{
				Type:              "feat",
				Scope:             "cli",
				Description:       "add the real next command",
				PullRequestNumber: 5,
				URL:               "https://github.com/owner/repo/pull/5",
			}},
		},
		{
			name: "falls back to the commit body when the forge has no pull request",
			raw: []history.Entry{
				{PullRequestNumber: 7, Subject: "Merge pull request #7 from owner/fix-branch", Body: "fix: stop the panic on empty history"},
			},
			wantChanges: []version.Change{{Type: "fix"}},
			wantEntries: []changelog.Entry{{
				Type:              "fix",
				Description:       "stop the panic on empty history",
				PullRequestNumber: 7,
			}},
		},
		{
			name: "parses a direct commit subject without a link",
			raw: []history.Entry{
				{Subject: "fix: stop the panic on empty history"},
			},
			wantChanges: []version.Change{{Type: "fix"}},
			wantEntries: []changelog.Entry{{Type: "fix", Description: "stop the panic on empty history"}},
		},
		{
			name: "uses the override block instead of the pull request title",
			raw: []history.Entry{
				{PullRequestNumber: 5, Subject: "Merge pull request #5 from owner/feat-branch"},
			},
			responses: map[int]forgetest.Response{
				5: {PullRequest: forge.PullRequest{
					Title: "misc: internal rework",
					Body:  "```release-me\nfeat(cli): add the next command\nfix: stop the panic on empty history\n```",
					URL:   "https://github.com/owner/repo/pull/5",
				}},
			},
			wantChanges: []version.Change{{Type: "feat"}, {Type: "fix"}},
			wantEntries: []changelog.Entry{
				{Type: "feat", Scope: "cli", Description: "add the next command", PullRequestNumber: 5, URL: "https://github.com/owner/repo/pull/5"},
				{Type: "fix", Description: "stop the panic on empty history", PullRequestNumber: 5, URL: "https://github.com/owner/repo/pull/5"},
			},
		},
		{
			name: "hides a pull request with an empty override block",
			raw: []history.Entry{
				{PullRequestNumber: 6, Subject: "Merge pull request #6 from owner/chore-branch"},
			},
			responses: map[int]forgetest.Response{
				6: {PullRequest: forge.PullRequest{
					Title: "feat: this would otherwise bump the version",
					Body:  "```release-me\n```",
					URL:   "https://github.com/owner/repo/pull/6",
				}},
			},
			wantChanges: []version.Change{},
			wantEntries: []changelog.Entry{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &forgetest.Fake{Responses: tc.responses}

			changes, entries, err := assembleEntries(context.Background(), f, "owner", "repo", tc.raw, "other", config.ChangelogSourceFirstParent, nil)
			if err != nil {
				t.Fatalf("assembleEntries() error = %v", err)
			}
			if !reflect.DeepEqual(changes, tc.wantChanges) {
				t.Fatalf("changes = %+v, want %+v", changes, tc.wantChanges)
			}
			if !reflect.DeepEqual(entries, tc.wantEntries) {
				t.Fatalf("entries = %+v, want %+v", entries, tc.wantEntries)
			}
		})
	}
}

func TestAssembleEntriesUsesTheGivenFallbackType(t *testing.T) {
	t.Parallel()
	raw := []history.Entry{{Subject: "quick hack before the demo"}}
	f := &forgetest.Fake{}

	_, entries, err := assembleEntries(context.Background(), f, "owner", "repo", raw, "misc", config.ChangelogSourceFirstParent, nil)
	if err != nil {
		t.Fatalf("assembleEntries() error = %v", err)
	}

	want := []changelog.Entry{{Type: "misc", Description: "quick hack before the demo"}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
}

func TestAssembleEntriesInSquashModeKeepsTheSubjectAsTitleWhenTheForgeCannotResolveIt(t *testing.T) {
	t.Parallel()
	// WalkSquash already stripped the trailing "(#12)" and put the number
	// in PullRequestNumber: the subject left behind is the real title and
	// must survive a 404, unlike a merge commit's boilerplate subject.
	raw := []history.Entry{{PullRequestNumber: 12, Subject: "feat: add thing", Squashed: true}}
	f := &forgetest.Fake{}

	changes, entries, err := assembleEntries(context.Background(), f, "owner", "repo", raw, "other", config.ChangelogSourceSquash, nil)
	if err != nil {
		t.Fatalf("assembleEntries() error = %v", err)
	}

	wantChanges := []version.Change{{Type: "feat"}}
	wantEntries := []changelog.Entry{{Type: "feat", Description: "add thing", PullRequestNumber: 12}}
	if !reflect.DeepEqual(changes, wantChanges) {
		t.Fatalf("changes = %+v, want %+v", changes, wantChanges)
	}
	if !reflect.DeepEqual(entries, wantEntries) {
		t.Fatalf("entries = %+v, want %+v", entries, wantEntries)
	}
}

func TestAssembleEntriesInSquashModeAddsOnlyTheLinkAndBodyWhenTheForgeResolves(t *testing.T) {
	t.Parallel()
	raw := []history.Entry{{PullRequestNumber: 12, Subject: "feat: add thing", Squashed: true}}
	f := &forgetest.Fake{Responses: map[int]forgetest.Response{
		12: {PullRequest: forge.PullRequest{
			// a different title on the forge is still never used: the
			// commit's own cleaned subject is the title in squash mode.
			Title: "feat: a retitled pull request",
			Body:  "BREAKING CHANGE: also breaking",
			URL:   "https://github.com/owner/repo/pull/12",
		}},
	}}

	_, entries, err := assembleEntries(context.Background(), f, "owner", "repo", raw, "other", config.ChangelogSourceSquash, nil)
	if err != nil {
		t.Fatalf("assembleEntries() error = %v", err)
	}

	want := []changelog.Entry{{
		Type: "feat", Description: "add thing", Breaking: true,
		PullRequestNumber: 12, URL: "https://github.com/owner/repo/pull/12",
	}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
}

func TestAssembleEntriesAppliesTheOverrideBlockInSquashMode(t *testing.T) {
	t.Parallel()
	raw := []history.Entry{{PullRequestNumber: 12, Subject: "feat: add thing", Squashed: true}}
	f := &forgetest.Fake{Responses: map[int]forgetest.Response{
		12: {PullRequest: forge.PullRequest{
			Body: "```release-me\nfix: the override wins over the squash title\n```",
			URL:  "https://github.com/owner/repo/pull/12",
		}},
	}}

	_, entries, err := assembleEntries(context.Background(), f, "owner", "repo", raw, "other", config.ChangelogSourceSquash, nil)
	if err != nil {
		t.Fatalf("assembleEntries() error = %v", err)
	}

	want := []changelog.Entry{{
		Type: "fix", Description: "the override wins over the squash title",
		PullRequestNumber: 12, URL: "https://github.com/owner/repo/pull/12",
	}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
}

func TestAssembleAllEntriesAppliesTheOverrideBlockToTheWholeGroup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  []history.Entry
	}{
		{
			name: "one commit links itself and uses the pull request issue references",
			raw: []history.Entry{
				{SHA: "a", PullRequestNumber: 5, Subject: "feat(widget): add the widget", Body: "Closes #13"},
			},
		},
		{
			name: "newest first commits link the oldest commit and use the pull request issue references",
			raw: []history.Entry{
				{SHA: "b", PullRequestNumber: 5, Subject: "fix(widget): correct a bug", Body: "Fixes #14"},
				{SHA: "a", PullRequestNumber: 5, Subject: "feat(widget): add the widget", Body: "Closes #13"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := tc.raw
			raw = append(raw, history.Entry{SHA: "c", Subject: "fix: direct hotfix"})
			f := &forgetest.Fake{Responses: map[int]forgetest.Response{
				5: {PullRequest: forge.PullRequest{
					Title: "ignored: all mode never uses the pull request title anyway",
					Body:  "Closes #7\n\n```release-me\nfeat: the override replaces the whole group\n```",
					URL:   "https://github.com/owner/repo/pull/5",
				}},
			}}

			changes, entries, err := assembleAllEntries(context.Background(), f, "owner", "repo", raw, "other")
			if err != nil {
				t.Fatalf("assembleAllEntries() error = %v", err)
			}

			wantChanges := []version.Change{{Type: "feat"}, {Type: "fix"}}
			wantEntries := []changelog.Entry{
				{
					Type: "feat", Description: "the override replaces the whole group", PullRequestNumber: 5, URL: "https://github.com/owner/repo/pull/5",
					CommitSHA: "a", Issues: []changelog.Issue{{Number: 7}},
				},
				{Type: "fix", Description: "direct hotfix", CommitSHA: "c"},
			}
			if !reflect.DeepEqual(changes, wantChanges) {
				t.Fatalf("changes = %+v, want %+v", changes, wantChanges)
			}
			if !reflect.DeepEqual(entries, wantEntries) {
				t.Fatalf("entries = %+v, want %+v, want the override replacing every commit #5 introduced, the direct commit untouched", entries, wantEntries)
			}
		})
	}
}

func TestAssembleEntriesPropagatesAForgeError(t *testing.T) {
	t.Parallel()
	raw := []history.Entry{
		{PullRequestNumber: 1, Subject: "Merge pull request #1 from owner/branch", Body: "feat: x"},
	}
	f := &forgetest.Fake{Responses: map[int]forgetest.Response{
		1: {Err: context.DeadlineExceeded},
	}}

	_, _, err := assembleEntries(context.Background(), f, "owner", "repo", raw, "other", config.ChangelogSourceFirstParent, nil)
	if err == nil {
		t.Fatal("assembleEntries() error = nil, want an error")
	}
}
