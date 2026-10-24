// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	tagrelease "github.com/ostendorf-cloud/release-me/internal/release"
)

// headSHA resolves HEAD to its commit SHA in dir's repository: the merge
// or landed commit a test just created, the one a real forge would report
// through MergeCommit.
func headSHA(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// These tests tie together the two invariants that, on their own, are only
// tested in isolation: release.Run tags the commit the forge reports as a
// pull request's merge commit, internal/release/release.go, and
// history.Walk reads the exclusive range "since..HEAD --first-parent",
// internal/history/walk.go. Neither alone proves a just released pull
// request cannot reappear: a tag one commit off the true merge commit
// still leaves that merge reachable inside the next walk's range, bringing
// the release pull request's own number and its chore type right back
// into the next version's changelog and bump decision.

// TestATaggedMergeCommitKeepsTheReleasedPullRequestOutOfTheNextRelease
// releases a merge-commit-shaped pull request through tagrelease.Run with
// the fake forge, tags the repository the same way a real forge would,
// then computes the next release for the same component and asserts the
// entries carry neither the released pull request's number nor a chore
// type entry.
func TestATaggedMergeCommitKeepsTheReleasedPullRequestOutOfTheNextRelease(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)

	mergePullRequest(t, dir, 9, "release-me/main", map[string]string{
		"CHANGELOG.md": "## v0.2.0 (2026-01-02)\n\n### Features\n\n- add the widget\n",
	})
	mergeSHA := headSHA(t, dir)

	comp := tagrelease.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v"}
	labels := tagrelease.Labels{Pending: "release: pending", Tagged: "release: tagged"}
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}}},
		MergeCommits:  map[int]string{9: mergeSHA},
		PRLabels:      map[int][]string{9: {"release: pending"}},
	}

	results, err := tagrelease.Run(t.Context(), f, "owner", "repo", comp, labels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || len(f.CreatedTags) != 1 {
		t.Fatalf("Run() = %+v, CreatedTags = %+v", results, f.CreatedTags)
	}
	// A real forge creates the tag release.Run asked for; the fake only
	// records the call, so the test creates it for real, at the exact SHA
	// release.Run resolved for the pull request.
	runGit(t, dir, "tag", f.CreatedTags[0].Tag, f.CreatedTags[0].SHA)

	mergePullRequest(t, dir, 12, "feat-after", map[string]string{"after.go": "package after\n"})

	comp2 := resolvedComponent(config.ChangelogSourceFirstParent)
	nextForge := &forgetest.Fake{Responses: map[int]forgetest.Response{
		12: {PullRequest: forge.PullRequest{Title: "feat: add the follow up feature", URL: "https://example.invalid/pull/12"}},
	}}
	rel, released, err := computeRelease(t.Context(), dir, config.Default(), comp2, "owner", "repo", nextForge, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease() error = %v", err)
	}
	if !released {
		t.Fatal("released = false, want true: the follow up feature should bump the version")
	}
	for _, e := range rel.entries {
		if e.PullRequestNumber == 9 {
			t.Fatalf("entries = %+v, want none carrying the released pull request's own number", rel.entries)
		}
		if e.Type == "chore" {
			t.Fatalf("entries = %+v, want no chore type entry", rel.entries)
		}
	}
	if _, ok := findEntry(rel.entries, "add the follow up feature"); !ok {
		t.Fatalf("entries = %+v, want the follow up feature entry", rel.entries)
	}
}

// TestATaggedSquashCommitKeepsTheReleasedPullRequestOutOfTheNextRelease
// covers the squash shape: the released commit is the landed commit
// itself, the SHA the forge reports as the merge commit and the commit
// that lands on main being one and the same, since a squash merge creates
// no separate merge commit of its own.
func TestATaggedSquashCommitKeepsTheReleasedPullRequestOutOfTheNextRelease(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)

	writeFiles(t, dir, "chore(main): release v0.2.0 (#9)", map[string]string{"CHANGELOG.md": "## v0.2.0\n"})
	landedSHA := headSHA(t, dir)

	comp := tagrelease.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v"}
	labels := tagrelease.Labels{Pending: "release: pending", Tagged: "release: tagged"}
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}}},
		MergeCommits:  map[int]string{9: landedSHA},
		PRLabels:      map[int][]string{9: {"release: pending"}},
	}

	results, err := tagrelease.Run(t.Context(), f, "owner", "repo", comp, labels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || len(f.CreatedTags) != 1 {
		t.Fatalf("Run() = %+v, CreatedTags = %+v", results, f.CreatedTags)
	}
	runGit(t, dir, "tag", f.CreatedTags[0].Tag, f.CreatedTags[0].SHA)

	writeFiles(t, dir, "feat: add the follow up feature (#12)", map[string]string{"after.go": "package after\n"})

	comp2 := resolvedComponent(config.ChangelogSourceSquash)
	rel, released, err := computeRelease(t.Context(), dir, config.Default(), comp2, "owner", "repo", &forgetest.Fake{}, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease() error = %v", err)
	}
	if !released {
		t.Fatal("released = false, want true: the follow up feature should bump the version")
	}
	for _, e := range rel.entries {
		if e.PullRequestNumber == 9 {
			t.Fatalf("entries = %+v, want none carrying the released pull request's own number", rel.entries)
		}
		if e.Type == "chore" {
			t.Fatalf("entries = %+v, want no chore type entry", rel.entries)
		}
	}
	if e, ok := findEntry(rel.entries, "add the follow up feature"); !ok || e.PullRequestNumber != 12 {
		t.Fatalf("entries = %+v, want the follow up feature entry resolved as pull request #12", rel.entries)
	}
}

// TestAComponentWithoutANewReleaseIsUnaffectedByAnotherComponentsRelease
// covers the multi component case: the backend releases twice and the
// frontend lands its own feature commit, but neither the backend's
// release pull requests nor its merge commits leak into the frontend's
// entries, and the backend itself, untouched since its last release, has
// nothing left to release despite the frontend's merge commit landing
// right after it.
func TestAComponentWithoutANewReleaseIsUnaffectedByAnotherComponentsRelease(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	writeConfig(t, dir, "components:\n  frontend:\n    path: frontend\n  backend:\n    path: backend\n")

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	var frontendRC, backendRC config.ResolvedComponent
	for _, c := range cfg.Resolve() {
		switch c.Name {
		case "frontend":
			frontendRC = c
		case "backend":
			backendRC = c
		}
	}
	backendComp := tagrelease.Component{Name: backendRC.Name, Branch: backendRC.Branch, TagPrefix: backendRC.TagPrefix, Multi: backendRC.Multi}
	labels := tagrelease.Labels{Pending: "release: pending", Tagged: "release: tagged"}

	mergePullRequest(t, dir, 8, "backend-release-8", map[string]string{
		"backend/CHANGELOG.md": "## v0.1.0 (2026-01-02)\n\n### Features\n\n- add the endpoint\n",
	})
	sha1 := headSHA(t, dir)
	f1 := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{{
			Head: backendRC.Branch, State: "closed",
			PullRequests: []forge.PullRequest{{Number: 8, Title: "chore(backend): release v0.1.0", Labels: []string{"release: pending"}, Merged: true}},
		}},
		MergeCommits: map[int]string{8: sha1},
		PRLabels:     map[int][]string{8: {"release: pending"}},
	}
	if _, err := tagrelease.Run(t.Context(), f1, "owner", "repo", backendComp, labels, false, false, discardLogger()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runGit(t, dir, "tag", f1.CreatedTags[0].Tag, f1.CreatedTags[0].SHA)

	mergePullRequest(t, dir, 9, "backend-release-9", map[string]string{
		"backend/CHANGELOG.md": "## v0.2.0 (2026-01-03)\n\n### Features\n\n- add the queue\n",
	})
	sha2 := headSHA(t, dir)
	f2 := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{{
			Head: backendRC.Branch, State: "closed",
			PullRequests: []forge.PullRequest{{Number: 9, Title: "chore(backend): release v0.2.0", Labels: []string{"release: pending"}, Merged: true}},
		}},
		MergeCommits: map[int]string{9: sha2},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	if _, err := tagrelease.Run(t.Context(), f2, "owner", "repo", backendComp, labels, false, false, discardLogger()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runGit(t, dir, "tag", f2.CreatedTags[0].Tag, f2.CreatedTags[0].SHA)

	mergePullRequest(t, dir, 10, "feat-frontend", map[string]string{"frontend/app.js": "console.log(1)\n"})

	frontendForge := &forgetest.Fake{Responses: map[int]forgetest.Response{
		10: {PullRequest: forge.PullRequest{Title: "feat: add the frontend widget", URL: "https://example.invalid/pull/10"}},
	}}
	frontendRel, frontendReleased, err := computeRelease(t.Context(), dir, cfg, frontendRC, "owner", "repo", frontendForge, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease(frontend) error = %v", err)
	}
	if !frontendReleased {
		t.Fatal("frontend released = false, want true: its own feature should bump the version")
	}
	for _, e := range frontendRel.entries {
		if e.PullRequestNumber == 8 || e.PullRequestNumber == 9 {
			t.Fatalf("frontend entries = %+v, want none carrying the backend's released pull request numbers", frontendRel.entries)
		}
		if e.Type == "chore" {
			t.Fatalf("frontend entries = %+v, want no chore type entry leaked from the backend's release", frontendRel.entries)
		}
	}
	if _, ok := findEntry(frontendRel.entries, "add the frontend widget"); !ok {
		t.Fatalf("frontend entries = %+v, want its own feature entry", frontendRel.entries)
	}

	_, backendReleased, err := computeRelease(t.Context(), dir, cfg, backendRC, "owner", "repo", &forgetest.Fake{}, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease(backend) error = %v", err)
	}
	if backendReleased {
		t.Fatal("backend released = true, want false: nothing landed under backend/ since its last release")
	}
}
