// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// TestDoPRTurnsAConfiguredPreReleaseTypesLabelIntoItsPreRelease pins a
// free pre-release type end to end: its label on the release pull request
// gives X.Y.Z-<type>.N, the guide names the label, and removing it returns
// to the stable version, the same as alpha, beta and rc.
func TestDoPRTurnsAConfiguredPreReleaseTypesLabelIntoItsPreRelease(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	runGit(t, dir, "tag", "v0.1.0")
	commitEntry(t, dir, "feat: add the next command")
	runGit(t, dir, "tag", "v0.2.0-next.1")
	commitEntry(t, dir, "fix: stop the panic on empty history")

	cfg := config.Default()
	cfg.Labels.PreReleaseTypes = []config.PreReleaseType{{Type: "next", Label: "release: next"}}

	f := newFake()
	f.RepoLabels = append(f.RepoLabels, "release: next")
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	if created.Title != "chore(main): release v0.2.0" {
		t.Fatalf("created title = %q, want the stable v0.2.0 without the label", created.Title)
	}
	if !strings.Contains(created.Body, "`release: next`") {
		t.Fatalf("created body = %q, want the guide to name the release: next label", created.Body)
	}

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: bareBranchSHA(t, origin), Labels: []string{"release: pending", "release: next"}},
		}},
	}
	var second bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}
	if len(f.UpdatedPullRequests) != 1 || f.UpdatedPullRequests[0].Title != "chore(main): release v0.2.0-next.2" {
		t.Fatalf("UpdatedPullRequests = %+v, want one titled v0.2.0-next.2, counting on from next.1", f.UpdatedPullRequests)
	}
}
