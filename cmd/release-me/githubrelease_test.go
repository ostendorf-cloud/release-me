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

// mergedReleaseFake is a forge holding one merged, pending release pull
// request for the implicit main component, number 9, at v0.1.0.
func mergedReleaseFake() *forgetest.Fake {
	pr := forge.PullRequest{
		Number: 9, Title: "chore(main): release v0.1.0",
		Body: "Release v0.1.0\n\n<details>\n<summary>Changelog</summary>\n\n## v0.1.0 (2026-01-02)\n\n### Features\n\n" +
			"- add the thing\n\n</details>\n",
		Labels: []string{"release: pending"}, Merged: true,
	}
	return &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{pr}}},
		MergeCommits:  map[int]string{9: "deadbeef"},
		PRLabels:      map[int][]string{9: {"release: pending"}},
	}
}

func TestDoReleaseWithDraftLeavesTheReleaseADraftAndNeverResumesIt(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	f := mergedReleaseFake()
	cfg := config.Default()
	cfg.Draft = true
	var stdout bytes.Buffer

	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}
	if len(f.CreatedReleases) != 1 || !f.CreatedReleases[0].Draft {
		t.Fatalf("CreatedReleases = %+v, want one draft", f.CreatedReleases)
	}
	for _, want := range []string{"tagged and drafted v0.1.0", "released-tag: v0.1.0"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), want)
		}
	}

	// The next run finds the pull request tagged and its release still a
	// draft: left for a human on purpose, so nothing is resumed.
	f.ListResponses[0].PullRequests[0].Labels = f.PRLabels[9]
	stdout.Reset()
	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("second doRelease() error = %v", err)
	}
	if got := stdout.String(); got != "nothing to release\n" {
		t.Fatalf("second stdout = %q, want nothing to release", got)
	}
	if len(f.CreatedReleases) != 1 {
		t.Fatalf("CreatedReleases = %+v, want still one", f.CreatedReleases)
	}
}
