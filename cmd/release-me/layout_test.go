// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

var everyLayout = []config.ReleasePullRequestLayout{config.LayoutHeadings, config.LayoutDetails, config.LayoutCompact}

// newLockedRepo returns a repository with a locked family of a and b, a
// feature in a and nothing in b.
func newLockedRepo(t *testing.T) (dir, origin string, cfg config.Config) {
	t.Helper()
	dir, origin = newPRRepo(t)
	writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	writeFiles(t, dir, "feat: add the CSV export", map[string]string{"a/export.go": "package a\n"})
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return dir, origin, cfg
}

func TestSharedReleasePullRequestRendersTheConfiguredLayout(t *testing.T) {
	compare := "https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0"
	tests := []struct {
		layout config.ReleasePullRequestLayout
		want   []string
	}{
		{layout: config.LayoutHeadings, want: []string{
			"\n## api\n\n### [v1.3.0](" + compare + ") (", "\n#### Features\n\n- export orders as CSV\n",
			"\n## web\n\n### [v0.3.1](",
		}},
		{layout: config.LayoutDetails, want: []string{
			"\n<details><summary>api v1.3.0</summary>\n\n## [v1.3.0](" + compare + ") (",
			"\n### Features\n\n- export orders as CSV\n\n</details>\n", "\n<details><summary>web v0.3.1</summary>\n",
		}},
		{layout: config.LayoutCompact, want: []string{
			"\n#### api\n\n## [v1.3.0](" + compare + ") (", "\n### Features\n\n- export orders as CSV\n",
			"\n#### web\n\n## [v0.3.1](",
		}},
	}
	for _, tc := range tests {
		t.Run(string(tc.layout), func(t *testing.T) {
			dir, origin, cfg := newSharedRepo(t)
			cfg.ReleasePullRequestLayout = tc.layout
			f := newFake()
			var stdout bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doPR() error = %v", err)
			}
			body := f.CreatedPullRequests[0].Body
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("body = %q, want it to contain %q", body, want)
				}
			}
			// The layout is the pull request body's alone: the changelog
			// file keeps its own levels.
			if got := showBareFile(t, origin, mainBranch, "api/CHANGELOG.md"); !strings.Contains(got, "\n## [v1.3.0]("+compare+") (") ||
				!strings.Contains(got, "\n### Features\n") {
				t.Errorf("api/CHANGELOG.md = %q, want its own heading levels", got)
			}
		})
	}
}

func TestLockedFamilyRendersTheConfiguredLayout(t *testing.T) {
	tests := []struct {
		layout config.ReleasePullRequestLayout
		want   []string
	}{
		{layout: config.LayoutHeadings, want: []string{
			"\n## a\n\n#### Features\n\n- add the CSV export\n", "\n## b\n\n- Version bump without user facing changes.\n",
		}},
		{layout: config.LayoutDetails, want: []string{
			"\n<details><summary>a v0.1.0</summary>\n\n### Features\n\n- add the CSV export\n\n</details>\n",
			"\n<details><summary>b v0.1.0</summary>\n\n- Version bump without user facing changes.\n\n</details>\n",
		}},
		{layout: config.LayoutCompact, want: []string{
			"\n#### a\n\n### Features\n\n- add the CSV export\n", "\n#### b\n\n- Version bump without user facing changes.\n",
		}},
	}
	for _, tc := range tests {
		t.Run(string(tc.layout), func(t *testing.T) {
			dir, _, cfg := newLockedRepo(t)
			cfg.ReleasePullRequestLayout = tc.layout
			f := newFake()
			var stdout bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doPR() error = %v", err)
			}
			body := f.CreatedPullRequests[0].Body
			if !strings.Contains(body, "<summary>Changelog</summary>\n\n## v0.1.0 (") {
				t.Errorf("body = %q, want the family's combined heading above every component", body)
			}
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("body = %q, want it to contain %q", body, want)
				}
			}
		})
	}
}

// TestRerunReadsAPullRequestOpenedUnderAnotherLayout opens the release pull
// request under one layout and reruns pr with release-pull-request-layout
// switched: nothing about the release changed, so nothing is pushed, and
// the body alone is rewritten in the new layout, every component kept.
func TestRerunReadsAPullRequestOpenedUnderAnotherLayout(t *testing.T) {
	families := []struct {
		name  string
		repo  func(t *testing.T) (string, string, config.Config)
		names []string
	}{
		{name: "shared", repo: newSharedRepo, names: []string{"api", "web"}},
		{name: "locked", repo: newLockedRepo, names: []string{"a", "b"}},
	}
	for _, family := range families {
		for _, opened := range everyLayout {
			for _, current := range everyLayout {
				t.Run(family.name+"/"+string(opened)+"-to-"+string(current), func(t *testing.T) {
					dir, origin, cfg := family.repo(t)
					cfg.ReleasePullRequestLayout = opened
					f := newFake()
					var first bytes.Buffer
					if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
						t.Fatalf("doPR() first run error = %v", err)
					}
					created := f.CreatedPullRequests[0]
					sha := bareBranchSHA(t, origin)
					f.ListResponses = []forgetest.ListResponse{
						{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{
							Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, HeadSHA: sha,
						}}},
					}

					cfg.ReleasePullRequestLayout = current
					var rerun bytes.Buffer
					if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &rerun); err != nil {
						t.Fatalf("doPR() rerun error = %v", err)
					}
					if got := bareBranchSHA(t, origin); got != sha {
						t.Fatalf("release branch = %s, want it unchanged at %s: only the layout changed", got, sha)
					}
					if opened == current {
						if !strings.Contains(rerun.String(), "release pull request up to date") {
							t.Fatalf("stdout = %q, want up to date", rerun.String())
						}
						return
					}
					if len(f.UpdatedPullRequests) != 1 {
						t.Fatalf("UpdatedPullRequests = %+v, want one body update", f.UpdatedPullRequests)
					}
					changelogText := releasepr.ExtractChangelog(f.UpdatedPullRequests[0].Body)
					if got := releasepr.DetectLayout(changelogText); got != releasepr.Layout(current) {
						t.Fatalf("updated body layout = %q, want %q", got, current)
					}
					if got := releasepr.ComponentNames(changelogText); !slices.Equal(got, family.names) {
						t.Fatalf("updated body components = %v, want %v", got, family.names)
					}
					for _, name := range family.names {
						before := releasepr.ComponentSection(releasepr.ExtractChangelog(created.Body), name)
						if got := releasepr.ComponentSection(changelogText, name); got != before {
							t.Fatalf("component %s section = %q, want it unchanged at %q", name, got, before)
						}
					}
				})
			}
		}
	}
}

// TestDoCommentReleaseAsReadsTheComponentsOfAnyLayout opens a shared
// release pull request under one layout and comments release-as with
// release-pull-request-layout switched: the components the command accepts
// are the ones the body carries, whichever layout set it.
func TestDoCommentReleaseAsReadsTheComponentsOfAnyLayout(t *testing.T) {
	tests := []struct {
		comment     string
		wantUpdated bool
		wantReply   string
	}{
		{comment: "@release-me release-as 1.0.0", wantReply: "this pull request releases api, web, each at its own version"},
		{comment: "@release-me release-as docs 3.0.0", wantReply: `component "docs" is not part of this pull request, which releases api, web`},
		{comment: "@release-me release-as web 1.0.0", wantUpdated: true, wantReply: "forced the next version of web to 1.0.0"},
	}
	for _, opened := range everyLayout {
		for _, current := range everyLayout {
			for _, tc := range tests {
				t.Run(string(opened)+"-to-"+string(current)+"/"+tc.comment, func(t *testing.T) {
					dir, origin, cfg := newSharedRepo(t)
					cfg.ReleasePullRequestLayout = opened
					f := newFake()
					var first bytes.Buffer
					if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
						t.Fatalf("doPR() first run error = %v", err)
					}
					created := f.CreatedPullRequests[0]
					f.Responses = map[int]forgetest.Response{1: {PullRequest: forge.PullRequest{
						Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"},
					}}}
					f.ListResponses = []forgetest.ListResponse{
						{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{
							Number: 1, Title: created.Title, Body: releasepr.SetComponentVersionOverride(created.Body, "web", "1.0.0"),
							Labels: []string{"release: pending"}, HeadSHA: bareBranchSHA(t, origin),
						}}},
					}

					cfg.ReleasePullRequestLayout = current
					var stdout bytes.Buffer
					flags := commentFlags{prNumber: 1, association: "OWNER", body: tc.comment}
					if err := doComment(t.Context(), dir, cfg, f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
						t.Fatalf("doComment() error = %v", err)
					}
					if len(f.CreatedComments) != 1 || !strings.Contains(f.CreatedComments[0].Body, tc.wantReply) {
						t.Fatalf("CreatedComments = %+v, want one reply containing %q", f.CreatedComments, tc.wantReply)
					}
					if !tc.wantUpdated {
						if len(f.UpdatedPullRequests) != 0 {
							t.Fatalf("UpdatedPullRequests = %+v, want none for a refused command", f.UpdatedPullRequests)
						}
						return
					}
					if len(f.UpdatedPullRequests) == 0 {
						t.Fatal("UpdatedPullRequests is empty, want the override written")
					}
					if raw, _ := releasepr.ExtractVersionOverride(f.UpdatedPullRequests[0].Body); raw != "web 1.0.0" {
						t.Fatalf("override block = %q, want %q", raw, "web 1.0.0")
					}
				})
			}
		}
	}
}
