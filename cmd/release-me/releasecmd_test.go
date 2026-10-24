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

func TestDoReleaseTagsAndReleasesAMergedPendingPullRequest(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")

	pr := forge.PullRequest{
		Number: 9, Title: "chore(main): release v0.1.0",
		Body: "Release v0.1.0\n\n<details>\n<summary>Changelog</summary>\n\n## v0.1.0 (2026-01-02)\n\n### Features\n\n" +
			"- add the thing\n\n</details>\n",
		Labels: []string{"release: pending"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	var stdout bytes.Buffer

	if err := doRelease(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	if len(f.CreatedTags) != 1 || f.CreatedTags[0].Tag != "v0.1.0" {
		t.Fatalf("CreatedTags = %+v", f.CreatedTags)
	}
	if len(f.CreatedReleases) != 1 {
		t.Fatalf("CreatedReleases = %+v", f.CreatedReleases)
	}
	if len(f.SetLabelsCalls) != 1 {
		t.Fatalf("SetLabelsCalls = %+v", f.SetLabelsCalls)
	}
	if !strings.Contains(stdout.String(), "tagged and released v0.1.0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "released-tag: v0.1.0") {
		t.Fatalf("stdout = %q, want the released-tag marker run-release-me.sh greps for", stdout.String())
	}
}

func TestDoReleaseStillFinishesAnAlreadyTaggedPullRequest(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")

	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.1.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		ExistingTags: map[string]bool{"v0.1.0": true},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	var stdout bytes.Buffer

	if err := doRelease(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	// The tag existing already means an earlier run got partway through;
	// this run still creates the release and swaps the label, instead of
	// leaving the pull request pending forever.
	if !strings.Contains(stdout.String(), "already tagged v0.1.0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	// The release and its draft still need assets attached, even though
	// the tag already existed, so the marker is still printed.
	if !strings.Contains(stdout.String(), "released-tag: v0.1.0") {
		t.Fatalf("stdout = %q, want the released-tag marker for the already-tagged release too", stdout.String())
	}
	if len(f.CreatedReleases) != 1 {
		t.Fatalf("CreatedReleases = %+v, want the release created", f.CreatedReleases)
	}
	if len(f.SetLabelsCalls) != 1 {
		t.Fatalf("SetLabelsCalls = %+v, want the label swapped", f.SetLabelsCalls)
	}
}

// TestDoReleaseResumesAnOrphanedDraftOnARerunOfTheWholePipeline reproduces
// issue #47's second blocker end to end, at the level release.yaml itself
// drives: a first run tags, drafts and swaps the label exactly as today,
// then the asset attach or publish step that was meant to run right after
// it, in the same workflow job, fails: GitHub still shows the release as a
// draft, release-me's own run already exited 0, so the pull request is
// left carrying Tagged, not Pending. A rerun of the whole pipeline, the
// obvious operator reaction, calls doRelease again exactly as release.yaml
// would; before this fix it found no pending pull request, emitted no
// released-tag marker and skipped the asset step a second time, orphaning
// the draft forever. It must instead find the still-draft release by its
// Tagged pull request and reprint the marker, without creating a second
// tag, a second release or swapping the label again.
func TestDoReleaseResumesAnOrphanedDraftOnARerunOfTheWholePipeline(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")

	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.1.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	var stdout bytes.Buffer

	// The first run: release-me tags, drafts and swaps the label, exactly
	// as release.yaml's own step does today. The asset attach and publish
	// step that is meant to run right after it is not part of this test:
	// it is a shell step, not release-me's own code, and it is the one
	// that fails, leaving the release a draft.
	if err := doRelease(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() first run error = %v", err)
	}
	if len(f.CreatedTags) != 1 || len(f.CreatedReleases) != 1 || len(f.SetLabelsCalls) != 1 {
		t.Fatalf("first run: tags=%+v releases=%+v labels=%+v", f.CreatedTags, f.CreatedReleases, f.SetLabelsCalls)
	}

	// The pull request now carries the swapped label, read back from the
	// forge exactly as release.yaml's next run would see it; the release
	// stays a draft, since the asset step never published it.
	f.ListResponses[0].PullRequests[0].Labels = []string{"release: tagged"}
	stdout.Reset()

	if err := doRelease(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() rerun error = %v", err)
	}
	if len(f.CreatedTags) != 1 || len(f.CreatedReleases) != 1 {
		t.Fatalf("rerun recreated the tag or the release: tags=%+v releases=%+v", f.CreatedTags, f.CreatedReleases)
	}
	if len(f.SetLabelsCalls) != 1 {
		t.Fatalf("rerun swapped the label again: %+v", f.SetLabelsCalls)
	}
	if !strings.Contains(stdout.String(), "released-tag: v0.1.0") {
		t.Fatalf("stdout = %q, want the rerun to find and finish the still-draft release", stdout.String())
	}
}

func TestDoReleasePrintsNothingToReleaseWithoutAMergedPendingPullRequest(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")

	f := &forgetest.Fake{}
	var stdout bytes.Buffer

	if err := doRelease(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}
	if stdout.String() != "nothing to release\n" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "nothing to release\n")
	}
}

func TestDoReleaseMarksAPreReleaseVersionAsAPreReleaseAndSwapsItsTypeLabel(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")

	pr := forge.PullRequest{
		Number: 9, Title: "chore(main): release v0.1.0-rc.1",
		Body:   "Release v0.1.0-rc.1\n\n<details>\n<summary>Changelog</summary>\n\n## v0.1.0-rc.1 (2026-01-02)\n\n### Features\n\n- add the thing\n\n</details>\n",
		Labels: []string{"release: pending", "release: rc"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending", "release: rc"}},
	}
	var stdout bytes.Buffer

	if err := doRelease(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	if len(f.CreatedTags) != 1 || f.CreatedTags[0].Tag != "v0.1.0-rc.1" {
		t.Fatalf("CreatedTags = %+v", f.CreatedTags)
	}
	if len(f.CreatedReleases) != 1 || !f.CreatedReleases[0].Prerelease {
		t.Fatalf("CreatedReleases = %+v, want a pre-release", f.CreatedReleases)
	}
	want := []string{"release: tagged"}
	if len(f.SetLabelsCalls) != 1 || !equalStrings(f.SetLabelsCalls[0].Labels, want) {
		t.Fatalf("SetLabelsCalls = %+v, want the pending and rc labels both swapped for %v", f.SetLabelsCalls, want)
	}
}

func TestDoReleaseCreatesTheMajorTagWhenConfigured(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")

	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v1.0.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	cfg := config.Default()
	cfg.MajorTag = true
	var stdout bytes.Buffer

	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	if len(f.CreatedTags) != 2 || f.CreatedTags[1].Tag != "v1" {
		t.Fatalf("CreatedTags = %+v, want the major tag v1 created too", f.CreatedTags)
	}
	if !strings.Contains(stdout.String(), "created major tag v1") {
		t.Fatalf("stdout = %q, want it to report the major tag", stdout.String())
	}
}

func TestDoReleaseDryRunWritesNothing(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")

	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.1.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
	}
	var stdout bytes.Buffer

	if err := doRelease(t.Context(), dir, config.Default(), "", f, discardLogger(), true, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "would tag and release v0.1.0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "released-tag:") {
		t.Fatalf("stdout = %q, dry run must not print the released-tag marker: no release was created", stdout.String())
	}
	if len(f.CreatedTags) != 0 || len(f.CreatedReleases) != 0 || len(f.SetLabelsCalls) != 0 {
		t.Fatalf("dry run wrote something: tags=%+v releases=%+v labels=%+v", f.CreatedTags, f.CreatedReleases, f.SetLabelsCalls)
	}
}
