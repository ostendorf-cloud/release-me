// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// withLabelLists sets two pending and two tagged labels and two extra ones,
// release-please's comma separated label, release-label and extra-label.
func withLabelLists(cfg *config.Config, f *forgetest.Fake) {
	cfg.Labels.Pending = []string{"autorelease: pending", "release"}
	cfg.Labels.Tagged = []string{"autorelease: tagged", "shipped"}
	cfg.Labels.Extra = []string{"team: platform", "automerge"}
	f.RepoLabels = append(f.RepoLabels, "autorelease: pending", "release", "autorelease: tagged", "shipped", "team: platform", "automerge")
}

func TestLabelListsOpenTagAndReleaseEveryReleasePullRequest(t *testing.T) {
	for _, family := range prFamilies {
		t.Run(family.name, func(t *testing.T) {
			dir, _, cfg := family.repo(t)
			f := newPRFake(gitOut(t, dir, "remote", "get-url", "--push", "origin"))
			withLabelLists(&cfg, f)
			var stdout bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doPR() error = %v", err)
			}
			created := f.CreatedPullRequests[0]
			wantOpened := []string{"autorelease: pending", "release", "team: platform", "automerge"}
			if !reflect.DeepEqual(created.Labels, wantOpened) {
				t.Fatalf("created labels = %q, want %q", created.Labels, wantOpened)
			}
			if !strings.Contains(created.Body, "moves the labels to `autorelease: tagged` and `shipped`.") {
				t.Fatalf("body = %q, want the guide to name both tagged labels", created.Body)
			}

			merged := forge.PullRequest{Number: 1, Title: created.Title, Body: created.Body, Labels: wantOpened, Merged: true}
			f.ListResponses = []forgetest.ListResponse{{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{merged}}}
			f.MergeCommits = map[int]string{1: "deadbeef"}
			f.PRLabels = map[int][]string{1: wantOpened}
			var released bytes.Buffer
			if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &released); err != nil {
				t.Fatalf("doRelease() error = %v", err)
			}
			if len(f.CreatedTags) != len(family.wantTags) {
				t.Fatalf("CreatedTags = %+v, want %v", f.CreatedTags, family.wantTags)
			}
			wantSwapped := []string{"team: platform", "automerge", "autorelease: tagged", "shipped"}
			if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, wantSwapped) {
				t.Fatalf("SetLabelsCalls = %+v, want %q", f.SetLabelsCalls, wantSwapped)
			}
		})
	}
}

func TestDoPRAddsBackOnlyThePendingLabelAReleasePullRequestLost(t *testing.T) {
	dir, origin, cfg := newSingleComponentRepo(t)
	f := newPRFake(origin)
	withLabelLists(&cfg, f)
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{
			Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release"}, HeadSHA: bareBranchSHA(t, origin),
		}}},
	}
	commitEntry(t, dir, "fix: keep the session on reload")
	var rerun bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &rerun); err != nil {
		t.Fatalf("doPR() rerun error = %v", err)
	}
	if len(f.AddLabelsCalls) != 1 || !reflect.DeepEqual(f.AddLabelsCalls[0].Labels, []string{"autorelease: pending"}) {
		t.Fatalf("AddLabelsCalls = %+v, want only the lost pending label added back", f.AddLabelsCalls)
	}
}

func TestDoPRRefusesToStartWhileAnExtraLabelIsMissingAndCreateMissingIsFalse(t *testing.T) {
	dir, _, cfg := newSingleComponentRepo(t)
	f := newFake()
	withLabelLists(&cfg, f)
	// "labels.create-missing" defaults to true on config.Default(), which
	// withLabelLists does not touch: forced false here so this test keeps
	// exercising the report-and-fail behavior the key restores, not a
	// label release-me would otherwise create.
	cfg.Labels.CreateMissing = false
	f.RepoLabels = f.RepoLabels[:len(f.RepoLabels)-1]
	var stdout bytes.Buffer
	err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout)
	if err == nil || !strings.Contains(err.Error(), "automerge") {
		t.Fatalf("doPR() error = %v, want the missing extra label named", err)
	}
}

// TestDoPRCreatesEveryMissingLabelByDefault proves the default
// "labels.create-missing: true" creates a missing pending, tagged and
// pre-release label instead of failing, each with the color and
// description labelDefaults assigns its role, and that doPR then proceeds
// as if every label had already existed.
func TestDoPRCreatesEveryMissingLabelByDefault(t *testing.T) {
	dir, origin, cfg := newSingleComponentRepo(t)
	f := newPRFake(origin)
	// newFake() seeds every configured label; drop the pending, tagged
	// and alpha ones so RepositoryLabels reports all three absent, one
	// from each role this test wants to prove labelDefaults tells apart.
	var kept []string
	for _, l := range f.RepoLabels {
		if l != "release: pending" && l != "release: tagged" && l != "release: alpha" {
			kept = append(kept, l)
		}
	}
	f.RepoLabels = kept

	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v, want the missing labels created instead of failing", err)
	}

	want := []forgetest.CreateLabelCall{
		{Owner: "owner", Repo: "repo", Name: "release: pending", Color: "ededed", Description: "Release pull request open."},
		{Owner: "owner", Repo: "repo", Name: "release: tagged", Color: "ededed", Description: "Release tagged."},
		{Owner: "owner", Repo: "repo", Name: "release: alpha", Color: "c2e0c6", Description: "Next release as alpha pre-release."},
	}
	if !reflect.DeepEqual(f.CreateLabelCalls, want) {
		t.Fatalf("CreateLabelCalls = %+v, want %+v", f.CreateLabelCalls, want)
	}
}

// TestDoPRWrapsACreateLabelFailureNamingTheLabel proves a CreateLabel
// failure surfaces as a clear error naming the label that failed, rather
// than being reported as if it were simply missing the old way.
func TestDoPRWrapsACreateLabelFailureNamingTheLabel(t *testing.T) {
	dir, _, cfg := newSingleComponentRepo(t)
	f := newFake()
	f.RepoLabels = f.RepoLabels[:len(f.RepoLabels)-1] // drops "release: rc"
	f.CreateLabelErr = errors.New("403 Forbidden")

	var stdout bytes.Buffer
	err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout)
	if err == nil || !strings.Contains(err.Error(), `create label "release: rc"`) || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("doPR() error = %v, want it to name the label that failed to create", err)
	}
}
