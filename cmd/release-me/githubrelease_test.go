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

func TestDoReleaseWithoutForceTagCreationLeavesTheTagToThePublish(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	f := mergedReleaseFake()
	cfg := config.Default()
	cfg.ForceTagCreation = false
	cfg.DraftUntilAssetsAttach = true
	var stdout bytes.Buffer

	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}
	if len(f.CreatedTags) != 0 {
		t.Fatalf("CreatedTags = %+v, want none, the publish creates the tag", f.CreatedTags)
	}
	if len(f.CreatedReleases) != 1 || f.CreatedReleases[0].Target != "deadbeef" || !f.CreatedReleases[0].Draft {
		t.Fatalf("CreatedReleases = %+v, want one draft targeting the merge commit", f.CreatedReleases)
	}
	for _, want := range []string{"drafted v0.1.0, tagged on publish", "released-tag: v0.1.0"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), want)
		}
	}
}
