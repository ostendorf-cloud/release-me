// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

const (
	testHeader = "Reviewed by the release team.\n\n```release-me-notes\nNot the notes.\n```\n"
	testFooter = "<details>\n<summary>Checklist</summary>\n\n- [ ] smoke test\n\n</details>\n"
)

// TestHeaderAndFooterFrameEveryBodyAndChangeNoRelease opens every kind of
// release pull request with a header and a footer, then releases it: the
// body opens with the header and closes with the footer, and every release
// body is the one the same pull request without them releases.
func TestHeaderAndFooterFrameEveryBodyAndChangeNoRelease(t *testing.T) {
	for _, guide := range []bool{false, true} {
		for _, family := range prFamilies {
			t.Run(fmt.Sprintf("%s/guide=%v", family.name, guide), func(t *testing.T) {
				releases := map[bool][]forgetest.CreateReleaseCall{}
				for _, framed := range []bool{false, true} {
					dir, _, cfg := family.repo(t)
					cfg.ReleasePullRequestGuide = guide
					if framed {
						cfg.ReleasePullRequestHeader, cfg.ReleasePullRequestFooter = testHeader, testFooter
					}
					f := newFake()
					var stdout bytes.Buffer
					if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
						t.Fatalf("doPR() error = %v", err)
					}
					body := f.CreatedPullRequests[0].Body
					if got := strings.Contains(body, "<summary>How this release works</summary>"); got != guide {
						t.Fatalf("guide present = %v, want %v", got, guide)
					}
					if framed {
						if !strings.HasPrefix(body, "Reviewed by the release team.\n\n ```release-me-notes\n") ||
							!strings.HasSuffix(body, testFooter) {
							t.Fatalf("body = %q, want the escaped header first and the footer last", body)
						}
						if _, ok := releasepr.ExtractNotes(body); ok {
							t.Fatalf("body = %q, want the header's block left inert", body)
						}
					}
					releaseMerged(t, dir, cfg, f, f.CreatedPullRequests[0].Title, body, family.wantTags)
					releases[framed] = f.CreatedReleases
				}
				if !reflect.DeepEqual(releases[true], releases[false]) {
					t.Fatalf("releases = %+v, want the ones without a header or footer, %+v", releases[true], releases[false])
				}
			})
		}
	}
}

// TestRerunAddsTheHeaderWithoutAPushAndThenStaysUpToDate sets the header and
// footer on an open release pull request carrying a notes block: the next
// run updates the body alone, notes kept, and the run after it finds the
// body up to date.
func TestRerunAddsTheHeaderWithoutAPushAndThenStaysUpToDate(t *testing.T) {
	dir, origin, cfg := newSingleComponentRepo(t)
	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	sha := bareBranchSHA(t, origin)
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{
			Number: 1, Title: created.Title, Body: releasepr.SetNotes(created.Body, "Run the migration first."),
			Labels: []string{"release: pending"}, HeadSHA: sha,
		}}},
	}

	cfg.ReleasePullRequestHeader, cfg.ReleasePullRequestFooter = testHeader, testFooter
	var rerun bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &rerun); err != nil {
		t.Fatalf("doPR() rerun error = %v", err)
	}
	if got := bareBranchSHA(t, origin); got != sha {
		t.Fatalf("release branch = %s, want it unchanged at %s", got, sha)
	}
	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one body update", f.UpdatedPullRequests)
	}
	updated := f.UpdatedPullRequests[0].Body
	if notes, _ := releasepr.ExtractNotes(updated); notes != "Run the migration first." {
		t.Fatalf("notes = %q, want them kept", notes)
	}
	if !strings.HasPrefix(updated, "Reviewed by the release team.") || !strings.HasSuffix(updated, testFooter) {
		t.Fatalf("updated body = %q, want the header and the footer", updated)
	}

	f.ListResponses[0].PullRequests[0].Body = updated
	f.UpdatedPullRequests = nil
	var third bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &third); err != nil {
		t.Fatalf("doPR() third run error = %v", err)
	}
	if len(f.UpdatedPullRequests) != 0 || !strings.Contains(third.String(), "release pull request up to date") {
		t.Fatalf("third run updated %+v, stdout %q, want up to date", f.UpdatedPullRequests, third.String())
	}
}
