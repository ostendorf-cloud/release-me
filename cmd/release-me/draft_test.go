// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

func TestDoPROpensEveryReleasePullRequestAsADraftOnlyWhileTheKeyIsOn(t *testing.T) {
	for guideName, guide := range map[string]bool{"shown": true, "hidden": false} {
		for _, family := range prFamilies {
			for _, draft := range []bool{false, true} {
				name := family.name + "/ready"
				if draft {
					name = family.name + "/draft"
				}
				t.Run(name+"/guide-"+guideName, func(t *testing.T) {
					dir, _, cfg := family.repo(t)
					cfg.ReleasePullRequestGuide = guide
					cfg.ReleasePullRequestDraft = draft
					f := newFake()
					var stdout bytes.Buffer
					if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
						t.Fatalf("doPR() error = %v", err)
					}
					if len(f.CreatedPullRequests) != 1 {
						t.Fatalf("CreatedPullRequests = %+v, want one", f.CreatedPullRequests)
					}
					if got := strings.Contains(f.CreatedPullRequests[0].Body, "<summary>How this release works</summary>"); got != guide {
						t.Fatalf("guide present = %v, want %v", got, guide)
					}
					if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Draft != draft {
						t.Fatalf("CreatedPullRequests = %+v, want one with draft %v", f.CreatedPullRequests, draft)
					}
				})
			}
		}
	}
}

// TestDraftReleasePullRequestUpdatesAndReleasesLikeAnyOther reruns pr on an
// open draft with a new change, then releases it once merged: the update
// goes through the same call as for any pull request, which never touches
// its draft state, and release reads it like any other.
func TestDraftReleasePullRequestUpdatesAndReleasesLikeAnyOther(t *testing.T) {
	for _, family := range prFamilies {
		t.Run(family.name, func(t *testing.T) {
			dir, origin, cfg := family.repo(t)
			cfg.ReleasePullRequestDraft = true
			f := newFake()
			var first bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
				t.Fatalf("doPR() first run error = %v", err)
			}
			created := f.CreatedPullRequests[0]
			f.ListResponses = []forgetest.ListResponse{
				{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{
					Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, HeadSHA: bareBranchSHA(t, origin),
				}}},
			}
			writeFiles(t, dir, "fix: retain refreshed sessions", map[string]string{filepath.Join(cfg.Resolve()[0].Path, "session.go"): "package session\n"})
			var rerun bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &rerun); err != nil {
				t.Fatalf("doPR() rerun error = %v", err)
			}
			if len(f.CreatedPullRequests) != 1 || len(f.UpdatedPullRequests) != 1 {
				t.Fatalf("created %+v, updated %+v, want the draft updated in place", f.CreatedPullRequests, f.UpdatedPullRequests)
			}
			releaseMerged(t, dir, cfg, f, f.UpdatedPullRequests[0].Title, f.UpdatedPullRequests[0].Body, family.wantTags)
		})
	}
}
