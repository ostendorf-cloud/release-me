// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

// TestRunNextForcesTheReleaseAsKeyUntilItShipped walks a release-as key
// through its lifetime: it wins over a footer while it is ahead of the
// last tag, and once that tag carries it, the computed bump applies again
// with a warning to remove the key.
func TestRunNextForcesTheReleaseAsKeyUntilItShipped(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "release-as: 2.0.0\n")
	commitEntry(t, dir, "chore: initial commit")
	runGit(t, dir, "tag", "v1.4.0")
	commitEntry(t, dir, "fix: close the pool\n\nRelease-As: 3.0.0")

	next := func() (stdout, stderr string) {
		t.Helper()
		var out, errOut bytes.Buffer
		if code := run([]string{"next"}, &out, &errOut); code != 0 {
			t.Fatalf("exit code = %d, want 0, stderr: %s", code, errOut.String())
		}
		return out.String(), errOut.String()
	}

	if got, _ := next(); got != "2.0.0 (release-as key)\n" {
		t.Fatalf("before the release: stdout = %q, want the key's 2.0.0 over the footer's 3.0.0", got)
	}

	runGit(t, dir, "tag", "v2.0.0")
	commitEntry(t, dir, "fix: retry the health check")
	got, stderr := next()
	if got != "2.0.1 (fix)\n" {
		t.Fatalf("after the release: stdout = %q, want the computed 2.0.1", got)
	}
	if !strings.Contains(stderr, "ignoring a release-as key") || !strings.Contains(stderr, "remove it from release-me.yaml") {
		t.Fatalf("stderr = %q, want a warning to remove the shipped key", stderr)
	}
}

func TestRunNextForcesOnlyTheComponentCarryingItsOwnReleaseAsKey(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "components:\n  api:\n    path: api\n    release-as: 2.0.0\n  web:\n    path: web\n")
	writeFiles(t, dir, "chore: scaffold", map[string]string{"api/main.go": "package main\n", "web/index.html": "<p>web</p>\n"})
	runGit(t, dir, "tag", "api/v1.4.0")
	runGit(t, dir, "tag", "web/v0.3.0")
	writeFiles(t, dir, "fix: escape the title", map[string]string{"web/index.html": "<p>web &amp; more</p>\n"})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"next"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "api: 2.0.0 (release-as key)\nweb: 0.3.1 (fix)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestFamilyReleasesForceEveryLinkedMemberWithTheReleaseAsKey(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "versioning: linked\nrelease-as: 1.0.0\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	runGit(t, dir, "tag", "a/v0.8.0")
	runGit(t, dir, "tag", "b/v0.8.0")
	writeFiles(t, dir, "fix: patch a", map[string]string{"a/fix.txt": "a\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	logger := discardLogger()
	members, err := gatherFamily(t.Context(), dir, cfg, "owner", "repo", newFake(), logger)
	if err != nil {
		t.Fatalf("gatherFamily() error = %v", err)
	}
	releases, released := familyReleases(cfg, members, logger)
	if !released || len(releases) != 2 || releases["a"].version.Version != "1.0.0" || releases["b"].version.Version != "1.0.0" {
		t.Fatalf("familyReleases() = %+v, %v, want a and b at 1.0.0", releases, released)
	}
}

// TestDoPRLetsTheReleaseAsCommentOverrideWinOverTheReleaseAsKey pins the
// precedence: the release-me-version block on the release pull request
// beats the release-as key.
func TestDoPRLetsTheReleaseAsCommentOverrideWinOverTheReleaseAsKey(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")
	cfg := config.Default()
	cfg.ReleaseAs = "2.0.0"

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	if created.Title != "chore(main): release v2.0.0" {
		t.Fatalf("created title = %q, want the key's v2.0.0", created.Title)
	}

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: releasepr.SetVersionOverride(created.Body, "3.0.0"), HeadSHA: bareBranchSHA(t, origin)},
		}},
	}
	var second bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}
	if len(f.UpdatedPullRequests) != 1 || f.UpdatedPullRequests[0].Title != "chore(main): release v3.0.0" {
		t.Fatalf("UpdatedPullRequests = %+v, want one titled v3.0.0", f.UpdatedPullRequests)
	}
}
