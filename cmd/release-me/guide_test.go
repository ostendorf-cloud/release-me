// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

const guideSummary = "<summary>How this release works</summary>"

// newSingleRepo returns a repository without components, one feature to
// release, in the shape newSharedRepo and newLockedRepo return.
func newSingleRepo(t *testing.T) (dir, origin string, cfg config.Config) {
	t.Helper()
	dir, origin = newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")
	return dir, origin, config.Default()
}

// guideFamilies is every kind of release pull request body pr renders:
// one component, a shared one and a locked family.
var guideFamilies = []struct {
	name  string
	repo  func(t *testing.T) (string, string, config.Config)
	names []string
}{
	{name: "single", repo: newSingleRepo},
	{name: "shared", repo: newSharedRepo, names: []string{"api", "web"}},
	{name: "locked", repo: newLockedRepo, names: []string{"a", "b"}},
}

func TestEveryReleasePullRequestBodyCarriesTheGuideOnlyWhileTheKeyIsOn(t *testing.T) {
	for _, family := range guideFamilies {
		for _, guide := range []bool{true, false} {
			name := family.name + "/guide-on"
			if !guide {
				name = family.name + "/guide-off"
			}
			t.Run(name, func(t *testing.T) {
				dir, _, cfg := family.repo(t)
				cfg.ReleasePullRequestGuide = guide
				f := newFake()
				var stdout bytes.Buffer
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
					t.Fatalf("doPR() error = %v", err)
				}
				body := f.CreatedPullRequests[0].Body
				if got := strings.Contains(body, guideSummary); got != guide {
					t.Fatalf("body carries the guide = %v, want %v:\n%s", got, guide, body)
				}
				if !strings.HasPrefix(body, "Release ") {
					t.Fatalf("body = %q, want it to open with the version line", body)
				}
				changelogText := releasepr.ExtractChangelog(body)
				if changelogText == "" {
					t.Fatalf("body = %q, want a changelog block", body)
				}
				if got := releasepr.ComponentNames(changelogText); !slices.Equal(got, family.names) {
					t.Fatalf("components = %v, want %v", got, family.names)
				}
				if _, ok := releasepr.ExtractDate(body); !ok {
					t.Fatalf("body = %q, want a changelog date read back", body)
				}
			})
		}
	}
}

// TestRerunAddsOrDropsTheGuideWhenTheKeyChanges opens the release pull
// request with the guide on or off, adds a notes block to it by hand and
// reruns pr with release-pull-request-guide switched: nothing about the
// release changed, so nothing is pushed, and the body alone gains or loses
// the guide, the notes and the changelog kept.
func TestRerunAddsOrDropsTheGuideWhenTheKeyChanges(t *testing.T) {
	for _, family := range guideFamilies {
		for _, opened := range []bool{true, false} {
			name := family.name + "/off-to-on"
			if opened {
				name = family.name + "/on-to-off"
			}
			t.Run(name, func(t *testing.T) {
				dir, origin, cfg := family.repo(t)
				cfg.ReleasePullRequestGuide = opened
				f := newFake()
				var first bytes.Buffer
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
					t.Fatalf("doPR() first run error = %v", err)
				}
				created := f.CreatedPullRequests[0]
				withNotes := releasepr.SetNotes(created.Body, "Run the migration before upgrading.")
				sha := bareBranchSHA(t, origin)
				f.ListResponses = []forgetest.ListResponse{
					{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{
						Number: 1, Title: created.Title, Body: withNotes, Labels: []string{"release: pending"}, HeadSHA: sha,
					}}},
				}

				cfg.ReleasePullRequestGuide = !opened
				var rerun bytes.Buffer
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &rerun); err != nil {
					t.Fatalf("doPR() rerun error = %v", err)
				}
				if got := bareBranchSHA(t, origin); got != sha {
					t.Fatalf("release branch = %s, want it unchanged at %s: only the guide changed", got, sha)
				}
				if len(f.UpdatedPullRequests) != 1 {
					t.Fatalf("UpdatedPullRequests = %+v, want one body update", f.UpdatedPullRequests)
				}
				updated := f.UpdatedPullRequests[0].Body
				if got := strings.Contains(updated, guideSummary); got != !opened {
					t.Fatalf("updated body carries the guide = %v, want %v:\n%s", got, !opened, updated)
				}
				if notes, _ := releasepr.ExtractNotes(updated); notes != "Run the migration before upgrading." {
					t.Fatalf("notes = %q, want them kept", notes)
				}
				if got, want := releasepr.ExtractChangelog(updated), releasepr.ExtractChangelog(created.Body); got != want {
					t.Fatalf("changelog = %q, want it unchanged at %q", got, want)
				}

				// A third run with the key unchanged finds the body up to
				// date: the guide never makes the comparison flap.
				f.ListResponses[0].PullRequests[0].Body = updated
				f.UpdatedPullRequests = nil
				var third bytes.Buffer
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &third); err != nil {
					t.Fatalf("doPR() third run error = %v", err)
				}
				if len(f.UpdatedPullRequests) != 0 || !strings.Contains(third.String(), "release pull request up to date") {
					t.Fatalf("third run updated %+v, stdout %q, want up to date", f.UpdatedPullRequests, third.String())
				}
			})
		}
	}
}

// TestDoCommentWorksOnABodyWithoutTheGuide comments release-as and
// release-notes on release pull requests opened with
// "release-pull-request-guide: false": both commands read and write the
// body the same way, and the immediate recompute keeps the guide out.
func TestDoCommentWorksOnABodyWithoutTheGuide(t *testing.T) {
	tests := []struct {
		name         string
		repo         func(t *testing.T) (string, string, config.Config)
		comment      string
		wantOverride string
		wantNotes    string
		wantReply    string
	}{
		{
			name: "release-as on a single component", repo: newSingleRepo,
			comment: "@release-me release-as 2.0.0", wantOverride: "2.0.0", wantReply: "2.0.0",
		},
		{
			name: "release-as on a shared component", repo: newSharedRepo,
			comment: "@release-me release-as web 1.0.0", wantOverride: "web 1.0.0", wantReply: "forced the next version of web to 1.0.0",
		},
		{
			name: "release-as on a locked family", repo: newLockedRepo,
			comment: "@release-me release-as 1.0.0", wantOverride: "1.0.0", wantReply: "1.0.0",
		},
		{
			name: "release-notes on a shared pull request", repo: newSharedRepo,
			comment: "@release-me release-notes Run the migration first.", wantNotes: "Run the migration first.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin, cfg := tc.repo(t)
			cfg.ReleasePullRequestGuide = false
			f := newFake()
			var first bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
				t.Fatalf("doPR() first run error = %v", err)
			}
			created := f.CreatedPullRequests[0]
			f.Responses = map[int]forgetest.Response{1: {PullRequest: forge.PullRequest{
				Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"},
			}}}
			listed := created.Body
			if tc.wantOverride != "" {
				listed = releasepr.SetVersionOverride(created.Body, tc.wantOverride)
			}
			f.ListResponses = []forgetest.ListResponse{
				{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{
					Number: 1, Title: created.Title, Body: listed, Labels: []string{"release: pending"}, HeadSHA: bareBranchSHA(t, origin),
				}}},
			}

			var stdout bytes.Buffer
			flags := commentFlags{prNumber: 1, association: "OWNER", body: tc.comment}
			if err := doComment(t.Context(), dir, cfg, f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
				t.Fatalf("doComment() error = %v", err)
			}
			if len(f.CreatedComments) != 1 || !strings.Contains(f.CreatedComments[0].Body, tc.wantReply) {
				t.Fatalf("CreatedComments = %+v, want one reply containing %q", f.CreatedComments, tc.wantReply)
			}
			if len(f.UpdatedPullRequests) == 0 {
				t.Fatal("UpdatedPullRequests is empty, want the command written")
			}
			written := f.UpdatedPullRequests[0].Body
			if tc.wantOverride != "" {
				if raw, _ := releasepr.ExtractVersionOverride(written); raw != tc.wantOverride {
					t.Fatalf("override block = %q, want %q", raw, tc.wantOverride)
				}
			}
			if tc.wantNotes != "" {
				if notes, _ := releasepr.ExtractNotes(written); notes != tc.wantNotes {
					t.Fatalf("notes = %q, want %q", notes, tc.wantNotes)
				}
			}
			for _, u := range f.UpdatedPullRequests {
				if strings.Contains(u.Body, guideSummary) {
					t.Fatalf("updated body = %q, want the guide left out", u.Body)
				}
			}
		})
	}
}
