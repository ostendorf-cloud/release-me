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

func TestDoReleaseWithSkipGitHubReleaseTagsAndReportsTheTagWithoutARelease(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	f := mergedReleaseFake()
	cfg := config.Default()
	cfg.SkipGitHubRelease = true
	var stdout bytes.Buffer

	out := &runOutputs{}
	if err := doRelease(withOutputs(t.Context(), out), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	if len(f.CreatedTags) != 1 || f.CreatedTags[0].Tag != "v0.1.0" {
		t.Fatalf("CreatedTags = %+v, want v0.1.0", f.CreatedTags)
	}
	if len(f.CreatedReleases) != 0 {
		t.Fatalf("CreatedReleases = %+v, want none", f.CreatedReleases)
	}
	if len(f.SetLabelsCalls) != 1 {
		t.Fatalf("SetLabelsCalls = %+v, want the label swapped", f.SetLabelsCalls)
	}
	for _, want := range []string{"tagged v0.1.0, GitHub release skipped", "released-tag: v0.1.0"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), want)
		}
	}
	got, err := out.render([]string{"main"})
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}
	if !strings.Contains(got, "released=true\n") || !strings.Contains(got, `"tag":"v0.1.0"`) {
		t.Fatalf("outputs = %q, want the tag reported as released", got)
	}
}
