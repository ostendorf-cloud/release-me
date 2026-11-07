// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// releaseChangelogBody is a merged release pull request body with one
// feature, the shape pr renders.
const releaseChangelogBody = "Release v1.3.0\n\n<details>\n<summary>Changelog</summary>\n\n## v1.3.0 (2026-03-14)\n\n" +
	"### Features\n\n- export orders as CSV\n\n</details>\n"

// mergedReleaseRepo returns a repository whose release-me.yaml is
// configYAML, empty for none, with branch pushed to the remote at the
// current commit, the head a merged release pull request on branch merged
// with, and that head.
func mergedReleaseRepo(t *testing.T, configYAML, branch string) (dir string, cfg config.Config, head string) {
	t.Helper()
	dir, _ = newPRRepo(t)
	if configYAML != "" {
		writeConfig(t, dir, configYAML)
	}
	commitEntry(t, dir, "feat: export orders as CSV")
	runGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return dir, cfg, headSHA(t, dir)
}

// mergedReleaseBranchFake returns a fake forge listing one merged pull request
// titled title on branch, still pending and merged with head.
func mergedReleaseBranchFake(branch, title, head string) *forgetest.Fake {
	f := newFake()
	f.ListResponses = []forgetest.ListResponse{
		{Head: branch, State: "closed", PullRequests: []forge.PullRequest{{
			Number: 31, Title: title, Body: releaseChangelogBody, Labels: []string{"release: pending"}, Merged: true, HeadSHA: head,
		}}},
	}
	f.MergeCommits = map[int]string{31: "9c1d4e7f2a3b5c6d7e8f9a0b1c2d3e4f5a6b7c8d"}
	f.PRLabels = map[int][]string{31: {"release: pending"}}
	return f
}

func TestReleaseDeletesTheReleaseBranchOnceItsPullRequestIsReleased(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		branch   string
		title    string
		wantTags []string
	}{
		{
			name: "independent", branch: "release-me/main", title: "chore(main): release v1.3.0",
			wantTags: []string{"v1.3.0"},
		},
		{
			name:   "linked",
			config: "versioning: linked\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n",
			branch: "release-me/api", title: "chore(api): release v1.3.0",
			wantTags: []string{"api/v1.3.0"},
		},
		{
			name:   "locked, after every component is tagged",
			config: "versioning: locked\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n",
			branch: "release-me/main", title: "chore: release v1.3.0 (api, web)",
			wantTags: []string{"api/v1.3.0", "web/v1.3.0"},
		},
		{
			name:   "shared, after every component is tagged",
			config: "release-pull-request: shared\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n",
			branch: "release-me/main", title: "chore: release api v1.3.0, web v0.4.1",
			wantTags: []string{"api/v1.3.0", "web/v0.4.1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, cfg, head := mergedReleaseRepo(t, tc.config, tc.branch)
			f := mergedReleaseBranchFake(tc.branch, tc.title, head)

			var stdout bytes.Buffer
			if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doRelease() error = %v", err)
			}
			var tags []string
			for _, c := range f.CreatedTags {
				tags = append(tags, c.Tag)
			}
			if !reflect.DeepEqual(tags, tc.wantTags) {
				t.Fatalf("CreatedTags = %q, want %q", tags, tc.wantTags)
			}
			remote := cleanupGitOutput(t, dir, "remote", "get-url", "--push", "origin")
			if bareRefExists(t, remote, "refs/heads/"+tc.branch) {
				t.Fatal("released branch was not deleted")
			}

			out := stdout.String()
			if !strings.Contains(out, "deleted release branch "+tc.branch) {
				t.Fatalf("stdout = %q, want the deleted branch reported", out)
			}
			if strings.Index(out, "deleted release branch") < strings.LastIndex(out, "released-tag:") {
				t.Fatalf("stdout = %q, want the branch deleted after every tag", out)
			}
		})
	}
}

func TestReleaseKeepsTheReleaseBranchWhenDeletingIsNotSafe(t *testing.T) {
	const branch = "release-me/main"
	const title = "chore(main): release v1.3.0"
	tests := []struct {
		name string
		// arrange changes the repository, the configuration or the fake
		// after the pull request merged with head.
		arrange    func(t *testing.T, dir string, cfg *config.Config, f *forgetest.Fake)
		dryRun     bool
		wantTags   int
		wantStdout string
	}{
		{
			name: "dry run reports it and deletes nothing", dryRun: true,
			wantStdout: "would delete release branch release-me/main",
		},
		{
			name:     "delete-release-branch false keeps it",
			arrange:  func(_ *testing.T, _ string, cfg *config.Config, _ *forgetest.Fake) { cfg.DeleteReleaseBranch = false },
			wantTags: 1,
		},
		{
			name: "a branch already deleted on merge is no error",
			arrange: func(t *testing.T, dir string, _ *config.Config, _ *forgetest.Fake) {
				runGit(t, dir, "push", "-q", "origin", "--delete", branch)
			},
			wantTags: 1,
		},
		{
			name: "a branch pr already pushed the next release onto is kept",
			arrange: func(t *testing.T, dir string, _ *config.Config, _ *forgetest.Fake) {
				commitEntry(t, dir, "chore(main): release v1.4.0")
				runGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
			},
			wantTags: 1,
		},
		{
			name: "a branch an open pull request uses is kept",
			arrange: func(_ *testing.T, _ string, _ *config.Config, f *forgetest.Fake) {
				f.ListResponses = append(f.ListResponses, forgetest.ListResponse{
					Head: branch, State: "open", PullRequests: []forge.PullRequest{{Number: 32, Title: "chore(main): release v1.4.0"}},
				})
			},
			wantTags: 1,
		},
		{
			name: "a pull request without the pending label released nothing, so its branch stays",
			arrange: func(_ *testing.T, _ string, _ *config.Config, f *forgetest.Fake) {
				f.ListResponses[0].PullRequests[0].Labels = []string{"release: tagged"}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, cfg, head := mergedReleaseRepo(t, "", branch)
			f := mergedReleaseBranchFake(branch, title, head)
			if tc.arrange != nil {
				tc.arrange(t, dir, &cfg, f)
			}

			var stdout bytes.Buffer
			if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), tc.dryRun, &stdout); err != nil {
				t.Fatalf("doRelease() error = %v", err)
			}
			if len(f.CreatedTags) != tc.wantTags {
				t.Fatalf("CreatedTags = %+v, want %d", f.CreatedTags, tc.wantTags)
			}

			if tc.wantStdout != "" && !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), tc.wantStdout)
			}
			if tc.wantStdout == "" && strings.Contains(stdout.String(), "release branch") {
				t.Fatalf("stdout = %q, want no branch deletion reported", stdout.String())
			}
		})
	}
}

func TestReleaseWarnsAndSucceedsWhenDeletingTheReleaseBranchFails(t *testing.T) {
	const branch = "release-me/main"
	dir, cfg, head := mergedReleaseRepo(t, "", branch)
	f := mergedReleaseBranchFake(branch, "chore(main): release v1.3.0", head)
	remote := cleanupGitOutput(t, dir, "remote", "get-url", "--push", "origin")
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\necho protected branch >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, logs bytes.Buffer
	if err := doRelease(t.Context(), dir, cfg, "", f, newLogger(&logs, false), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v, want the release to succeed", err)
	}
	if len(f.CreatedTags) != 1 || !strings.Contains(stdout.String(), "released-tag: v1.3.0") {
		t.Fatalf("CreatedTags = %+v, stdout = %q, want the release reported", f.CreatedTags, stdout.String())
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "protected branch") ||
		!strings.Contains(logs.String(), "number=31") {
		t.Fatalf("logs = %q, want a warning naming the failure and the pull request", logs.String())
	}
	if strings.Contains(stdout.String(), "deleted release branch") {
		t.Fatalf("stdout = %q, want no deletion reported", stdout.String())
	}
}

func TestReleaseKeepsTheBranchOfAPullRequestWhoseReleaseFailed(t *testing.T) {
	const branch = "release-me/main"
	dir, cfg, head := mergedReleaseRepo(t, "", branch)
	f := mergedReleaseBranchFake(branch, "chore(main): release v1.3.0", head)
	f.CreateReleaseErrs = map[string]error{"v1.3.0": errors.New("github: create release v1.3.0: 422 tag_name was used by an immutable release")}

	var stdout bytes.Buffer
	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err == nil {
		t.Fatal("doRelease() error = nil, want the failed release reported")
	}
	remote := cleanupGitOutput(t, dir, "remote", "get-url", "--push", "origin")
	if !bareRefExists(t, remote, "refs/heads/"+branch) {
		t.Fatal("failed release lost its branch")
	}
}

func TestReleaseDeletesTheBranchOfAComponentReleasedBeforeALaterOneFailed(t *testing.T) {
	dir, _ := newPRRepo(t)
	writeConfig(t, dir, "components:\n  api:\n    path: api\n  web:\n    path: web\n")
	commitEntry(t, dir, "feat: export orders as CSV")
	runGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/release-me/api")
	runGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/release-me/web")
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	head := headSHA(t, dir)
	f := mergedReleaseBranchFake("release-me/api", "chore(api): release v1.3.0", head)
	f.ListResponses = append(f.ListResponses, forgetest.ListResponse{
		Head: "release-me/web", State: "closed", PullRequests: []forge.PullRequest{{
			Number: 33, Title: "chore(web): release next", Labels: []string{"release: pending"}, Merged: true, HeadSHA: head,
		}},
	})

	var stdout bytes.Buffer
	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err == nil {
		t.Fatal("doRelease() error = nil, want web's unreadable title reported")
	}
	remote := cleanupGitOutput(t, dir, "remote", "get-url", "--push", "origin")
	if bareRefExists(t, remote, "refs/heads/release-me/api") || !bareRefExists(t, remote, "refs/heads/release-me/web") {
		t.Fatal("want only api's successfully released branch deleted")
	}
}

// TestSharedReleaseSweepDeletesALeftoverBranchOnlyOnceItsPullRequestIsTagged
// covers the leftover sweep's interplay with deleting: the sweep only looks
// at a per-component branch while it exists, so deleting one must never
// hide a merged pull request that is not tagged yet.
func TestSharedReleaseSweepDeletesALeftoverBranchOnlyOnceItsPullRequestIsTagged(t *testing.T) {
	tests := []struct {
		name         string
		drafts       bool
		releaseErr   bool
		wantDeleted  bool
		wantErr      bool
		wantTagCount int
	}{
		{
			name:         "deletes the leftover branch once its pull request is tagged",
			wantDeleted:  true,
			wantTagCount: 1,
		},
		{
			name:       "keeps the leftover branch while its release failed, so the next sweep still finds it",
			releaseErr: true, wantErr: true, wantTagCount: 1,
		},
		{
			name:   "keeps the leftover branch with draft-until-assets-attach, so the sweep can resume its draft",
			drafts: true, wantTagCount: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin, cfg := newSharedRepo(t)
			cfg.DraftUntilAssetsAttach = tc.drafts
			runGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/release-me/api")
			f := mergedReleaseBranchFake("release-me/api", "chore(api): release v1.3.0", headSHA(t, dir))
			if tc.releaseErr {
				f.CreateReleaseErrs = map[string]error{"api/v1.3.0": errors.New("github: create release api/v1.3.0: 502 Bad Gateway")}
			}

			var stdout bytes.Buffer
			err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout)
			if (err != nil) != tc.wantErr {
				t.Fatalf("doRelease() error = %v, want error %v", err, tc.wantErr)
			}
			if len(f.CreatedTags) != tc.wantTagCount {
				t.Fatalf("CreatedTags = %+v, want %d", f.CreatedTags, tc.wantTagCount)
			}
			if exists := bareRefExists(t, origin, "refs/heads/release-me/api"); exists == tc.wantDeleted {
				t.Fatalf("branch exists = %v, want deleted %v", exists, tc.wantDeleted)
			}
		})
	}
}

// TestReleaseResumesADraftAfterItsReleaseBranchWasDeleted covers
// "draft-until-assets-attach" with "delete-release-branch": the first run
// drafts the release and deletes the branch, the publish step after it
// fails, and the rerun must still find the tagged pull request to resume.
// GitHub's head filter still lists a merged pull request whose branch is
// deleted, confirmed live against this repository, so the fake keeps
// listing it while the branch is gone from the remote.
func TestReleaseResumesADraftAfterItsReleaseBranchWasDeleted(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		title   string
		wantTag string
	}{
		{
			name: "independent", title: "chore(main): release v1.3.0",
			wantTag: "released-tag: v1.3.0",
		},
		{
			name:   "locked",
			config: "versioning: locked\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n",
			title:  "chore: release v1.3.0 (api, web)", wantTag: "released-tag: web/v1.3.0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const branch = "release-me/main"
			dir, cfg, head := mergedReleaseRepo(t, tc.config, branch)
			cfg.DraftUntilAssetsAttach = true
			f := mergedReleaseBranchFake(branch, tc.title, head)

			var stdout bytes.Buffer
			if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doRelease() first run error = %v", err)
			}
			remote := cleanupGitOutput(t, dir, "remote", "get-url", "--push", "origin")
			if bareRefExists(t, remote, "refs/heads/"+branch) {
				t.Fatal("released branch was not deleted")
			}
			tags, releases := len(f.CreatedTags), len(f.CreatedReleases)

			f.ListResponses[0].PullRequests[0].Labels = []string{"release: tagged"}
			stdout.Reset()

			if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doRelease() rerun error = %v", err)
			}
			if len(f.CreatedTags) != tags || len(f.CreatedReleases) != releases {
				t.Fatalf("rerun recreated a tag or a release: tags=%+v releases=%+v", f.CreatedTags, f.CreatedReleases)
			}
			if !strings.Contains(stdout.String(), tc.wantTag) {
				t.Fatalf("stdout = %q, want the rerun to resume the still-draft release", stdout.String())
			}
		})
	}
}
