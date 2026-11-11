// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

// mergeBranch commits each of commits onto a side branch created from
// main, then merges it back with --no-ff and a "Merge pull request #N
// from ..." subject, the shape history.Walk recognizes as a pull request
// merge.
func mergeBranch(t *testing.T, dir string, number int, branch string, commits ...string) {
	t.Helper()
	runGit(t, dir, "checkout", "-q", "-b", branch)
	for _, c := range commits {
		commitEntry(t, dir, c)
	}
	runGit(t, dir, "checkout", "-q", "main")
	runGit(t, dir, "merge", "--no-ff", "-q", "-m", "Merge pull request #"+strconv.Itoa(number)+" from owner/"+branch, branch)
}

func TestComputeReleaseForcesTheVersionOfAReleaseAsFooter(t *testing.T) {
	cases := []struct {
		name        string
		source      string
		setup       func(t *testing.T, dir string)
		responses   map[int]forgetest.Response
		wantVersion string
		wantWarn    string
	}{
		{
			name:   "a direct commit's footer releases without a bumping change",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "chore: release 2.0.0\n\nRelease-As: 2.0.0")
			},
			wantVersion: "2.0.0",
		},
		{
			name:   "a merged pull request's body footer forces the version",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				mergeBranch(t, dir, 10, "feat-widget", "feat(widget): add the widget")
			},
			responses: map[int]forgetest.Response{
				10: {PullRequest: forge.PullRequest{
					Title: "feat(widget): add the widget",
					Body:  "Adds the widget the dashboard needs.\n\nRelease-As: 1.0.0",
					URL:   "https://example.invalid/pull/10",
				}},
			},
			wantVersion: "1.0.0",
		},
		{
			name:   "a squash merge's commit body footer counts when the pull request body has none",
			source: config.ChangelogSourceSquash,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "feat(gadget): add the gadget (#11)\n\n* feat: add the gadget\n\nRelease-As: 1.0.0")
			},
			responses: map[int]forgetest.Response{
				11: {PullRequest: forge.PullRequest{
					Title: "feat(gadget): add the gadget",
					Body:  "Adds the gadget.",
					URL:   "https://example.invalid/pull/11",
				}},
			},
			wantVersion: "1.0.0",
		},
		{
			name:   "a branch commit's footer counts in all mode",
			source: config.ChangelogSourceAll,
			setup: func(t *testing.T, dir string) {
				mergeBranch(t, dir, 12, "feat-api", "feat(api): add the endpoint\n\nRelease-As: 1.0.0", "fix(api): validate the input")
			},
			responses: map[int]forgetest.Response{
				12: {PullRequest: forge.PullRequest{Title: "feat(api): add the endpoint", URL: "https://example.invalid/pull/12"}},
			},
			wantVersion: "1.0.0",
		},
		{
			name:   "a sub commit's footer counts in nested mode",
			source: config.ChangelogSourceNested,
			setup: func(t *testing.T, dir string) {
				mergeBranch(t, dir, 13, "feat-cli", "feat(cli): add the flag\n\nRelease-As: 1.0.0")
			},
			responses: map[int]forgetest.Response{
				13: {PullRequest: forge.PullRequest{Title: "chore: merge cli work", URL: "https://example.invalid/pull/13"}},
			},
			wantVersion: "1.0.0",
		},
		{
			name:   "an override block replaces the pull request's footer",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				mergeBranch(t, dir, 14, "fix-panic", "fix: stop the panic")
			},
			responses: map[int]forgetest.Response{
				14: {PullRequest: forge.PullRequest{
					Title: "fix: stop the panic",
					Body:  "```release-me\nfix: stop the panic on empty history\n```\n\nRelease-As: 1.0.0",
					URL:   "https://example.invalid/pull/14",
				}},
			},
			wantVersion: "0.1.1",
		},
		{
			name:   "the highest of several footers wins",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "feat: add the export\n\nRelease-As: 1.5.0")
				commitEntry(t, dir, "fix: correct the export\n\nRelease-As: 1.2.0")
			},
			wantVersion: "1.5.0",
		},
		{
			name:   "a footer not greater than the last tag is ignored with a warning",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "fix: stop the panic\n\nRelease-As: 0.1.0")
			},
			wantVersion: "0.1.1",
			wantWarn:    "is not greater than the last released version 0.1.0",
		},
		{
			name:   "a pre-release footer is ignored with a warning",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "feat: add the export\n\nRelease-As: 1.0.0-rc.1")
			},
			wantVersion: "0.2.0",
			wantWarn:    "is a pre-release version",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			commitEntry(t, dir, "chore: initial commit")
			tagRepo(t, dir)
			tc.setup(t, dir)

			f := newFake()
			f.Responses = tc.responses
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))

			rel, released, err := computeRelease(t.Context(), dir, config.Default(), resolvedComponent(tc.source), f, logger)
			if err != nil {
				t.Fatalf("computeRelease() error = %v", err)
			}
			if !released || rel.version.Version != tc.wantVersion {
				t.Fatalf("released = %v, version = %q, want %q", released, rel.version.Version, tc.wantVersion)
			}
			if tc.wantWarn == "" && logs.Len() > 0 {
				t.Fatalf("logs = %q, want no warning", logs.String())
			}
			if tc.wantWarn != "" && !strings.Contains(logs.String(), tc.wantWarn) {
				t.Fatalf("logs = %q, want a warning containing %q", logs.String(), tc.wantWarn)
			}
		})
	}
}

// TestRunNextForcesOnlyTheComponentAFooterCommitTouches guards the footer's
// attribution: a commit forcing a version counts for the components it
// touches, never for one it does not.
func TestRunNextForcesOnlyTheComponentAFooterCommitTouches(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "components:\n  frontend:\n    path: frontend\n  backend:\n    path: backend\n")
	writeFiles(t, dir, "chore: scaffold the two components", map[string]string{
		"frontend/README.md": "frontend\n",
		"backend/README.md":  "backend\n",
	})
	runGit(t, dir, "tag", "frontend/v0.3.0")
	runGit(t, dir, "tag", "backend/v0.3.0")
	writeFiles(t, dir, "feat: redesign the frontend\n\nRelease-As: 1.0.0", map[string]string{"frontend/app.js": "console.log(2)\n"})
	writeFiles(t, dir, "fix: stop the backend panic", map[string]string{"backend/app.go": "package backend\n"})

	var stdout, stderr bytes.Buffer
	code := run([]string{"next"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "frontend: 1.0.0 (Release-As footer)\nbackend: 0.3.1 (fix)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// TestFamilyReleasesApplyAReleaseAsFooterToTheSharedVersion covers both
// family modes: the footer forces the family's shared version, and in
// linked a component whose only change is the footer commit still
// releases.
func TestFamilyReleasesApplyAReleaseAsFooterToTheSharedVersion(t *testing.T) {
	cases := []struct {
		name       string
		versioning string
		commits    func(t *testing.T, dir string)
		want       map[string]string
	}{
		{
			name:       "linked gives every changed component the forced version",
			versioning: "linked",
			commits: func(t *testing.T, dir string) {
				writeFiles(t, dir, "fix: patch a\n\nRelease-As: 1.0.0", map[string]string{"a/fix.txt": "a\n"})
				writeFiles(t, dir, "fix: patch b", map[string]string{"b/fix.txt": "b\n"})
			},
			want: map[string]string{"a": "1.0.0", "b": "1.0.0"},
		},
		{
			name:       "linked releases a component whose only change is the footer commit",
			versioning: "linked",
			commits: func(t *testing.T, dir string) {
				writeFiles(t, dir, "chore: release a 1.0.0\n\nRelease-As: 1.0.0", map[string]string{"a/VERSION": "1.0.0\n"})
			},
			want: map[string]string{"a": "1.0.0"},
		},
		{
			name:       "locked releases every component at the forced version",
			versioning: "locked",
			commits: func(t *testing.T, dir string) {
				writeFiles(t, dir, "chore: release 1.0.0\n\nRelease-As: 1.0.0", map[string]string{"a/VERSION": "1.0.0\n"})
			},
			want: map[string]string{"a": "1.0.0", "b": "1.0.0", "c": "1.0.0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			writeConfig(t, dir, "versioning: "+tc.versioning+"\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n  c:\n    path: c\n")
			writeFiles(t, dir, "chore: scaffold the family", map[string]string{
				"a/README.md": "a\n", "b/README.md": "b\n", "c/README.md": "c\n",
			})
			for _, name := range []string{"a", "b", "c"} {
				runGit(t, dir, "tag", name+"/v0.8.0")
			}
			tc.commits(t, dir)

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
			if !released {
				t.Fatal("familyReleases() released = false, want true")
			}
			got := make(map[string]string, len(releases))
			for name, rel := range releases {
				got[name] = rel.version.Version
			}
			if len(got) != len(tc.want) {
				t.Fatalf("released %+v, want %+v", got, tc.want)
			}
			for name, want := range tc.want {
				if got[name] != want {
					t.Fatalf("released %+v, want %+v", got, tc.want)
				}
			}
		})
	}
}

// TestFamilyReleasesWarnsOnceForAFooterTwoComponentsShare guards the
// refusal log: a commit touching two members of a family is in both
// members' changes, and its refused footer is still logged once.
func TestFamilyReleasesWarnsOnceForAFooterTwoComponentsShare(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "versioning: linked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	runGit(t, dir, "tag", "a/v0.8.0")
	runGit(t, dir, "tag", "b/v0.8.0")
	writeFiles(t, dir, "fix: patch a and b\n\nRelease-As: 0.7.0", map[string]string{
		"a/fix.txt": "a\n", "b/fix.txt": "b\n",
	})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	members, err := gatherFamily(t.Context(), dir, cfg, "owner", "repo", newFake(), logger)
	if err != nil {
		t.Fatalf("gatherFamily() error = %v", err)
	}
	releases, released := familyReleases(cfg, members, logger)
	if !released || releases["a"].version.Version != "0.8.1" {
		t.Fatalf("familyReleases() = %+v, %v, want a at the computed 0.8.1", releases, released)
	}
	if got := strings.Count(logs.String(), "ignoring a Release-As footer"); got != 1 {
		t.Fatalf("logs = %q, want the refusal logged once, got %d", logs.String(), got)
	}
}

// TestDoPRLetsTheReleaseAsCommentOverrideWinOverAFooter pins the
// precedence: the release-me-version block a release-as comment writes on
// the release pull request beats a Release-As footer in the history.
func TestDoPRLetsTheReleaseAsCommentOverrideWinOverAFooter(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command\n\nRelease-As: 2.0.0")

	f := newPRFake(origin)
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	if created.Title != "chore(main): release v2.0.0" {
		t.Fatalf("created title = %q, want the footer's v2.0.0", created.Title)
	}

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: releasepr.SetVersionOverride(created.Body, "3.0.0"), HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}
	if len(f.UpdatedPullRequests) != 1 || f.UpdatedPullRequests[0].Title != "chore(main): release v3.0.0" {
		t.Fatalf("UpdatedPullRequests = %+v, want one titled v3.0.0", f.UpdatedPullRequests)
	}
	if changelog := showBareBranchFile(t, origin, "CHANGELOG.md"); !strings.Contains(changelog, "## v3.0.0") {
		t.Fatalf("CHANGELOG.md on the release branch = %q, want the comment's v3.0.0", changelog)
	}
}

// TestDoPRBuildsAPreReleaseOnTheFootersVersion pins how a pre-release
// label combines with a footer: the label's pre-release counts on top of
// the forced version, not on top of the computed bump.
func TestDoPRBuildsAPreReleaseOnTheFootersVersion(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command\n\nRelease-As: 2.0.0")

	f := newPRFake(origin)
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: bareBranchSHA(t, origin), Labels: []string{"release: pending", "release: rc"}},
		}},
	}

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}
	if len(f.UpdatedPullRequests) != 1 || f.UpdatedPullRequests[0].Title != "chore(main): release v2.0.0-rc.1" {
		t.Fatalf("UpdatedPullRequests = %+v, want one titled v2.0.0-rc.1", f.UpdatedPullRequests)
	}
}
