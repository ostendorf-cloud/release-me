// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

type interleavedCommitForge struct {
	forge.Forge
	afterCommit func()
}

func (f interleavedCommitForge) CommitBranch(ctx context.Context, owner, repo string, c forge.BranchCommit) (string, error) {
	sha, err := f.Forge.CommitBranch(ctx, owner, repo, c)
	if err == nil {
		f.afterCommit()
	}
	return sha, err
}

func TestReleasePublicationKeepsAnInterleavedWriter(t *testing.T) {
	for _, move := range []bool{false, true} {
		name := "publishes while the expected head is unchanged"
		if move {
			name = "keeps a winner published after planning"
		}
		t.Run(name, func(t *testing.T) {
			dir, remote := newPRRepo(t)
			commitEntry(t, dir, "feat: add export")
			f := newPRFake(remote)
			var out bytes.Buffer
			cfg := config.Default()
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
				t.Fatal(err)
			}
			old := bareBranchSHA(t, remote)
			commitEntry(t, dir, "fix: handle empty exports")
			winner := gitOut(t, dir, "rev-parse", "HEAD")
			created := f.CreatedPullRequests[0]
			f.ListResponses = nil
			wrapped := interleavedCommitForge{Forge: f, afterCommit: func() {
				if move {
					runGit(t, remote, "update-ref", "refs/heads/"+mainBranch, winner)
				}
			}}
			f.ListResponses = append(f.ListResponses, forgetest.ListResponse{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{Number: 7, Title: created.Title, Body: created.Body, HeadSHA: old}}})
			err := doPR(t.Context(), dir, cfg, "", wrapped, discardLogger(), false, &out)
			if move {
				if !errors.Is(err, forge.ErrBranchMoved) {
					t.Fatalf("error = %v, want ErrBranchMoved", err)
				}
				if got := bareBranchSHA(t, remote); got != winner {
					t.Fatalf("tip = %s, want winner %s", got, winner)
				}
				if len(f.UpdatedPullRequests) != 0 {
					t.Fatal("losing writer updated PR metadata")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// moveRefHook is a pre-receive hook body that moves the release branch to
// another commit, which is what receive-pack sees when a second writer's
// push lands between the ref advertisement this push leased against and
// its own ref transaction. The quarantine variables are unset because
// receive-pack refuses a ref update from a hook that still carries them.
const moveRefHook = "env -u GIT_QUARANTINE_PATH -u GIT_OBJECT_DIRECTORY " +
	"-u GIT_ALTERNATE_OBJECT_DIRECTORIES git update-ref refs/heads/" + mainBranch + " {{other}}"

// hookOnPush installs body as the pre-receive hook of the bare repository
// at origin, so a test can make a push fail the way a real forge does.
func hookOnPush(t *testing.T, origin, body string) {
	t.Helper()
	path := filepath.Join(origin, "hooks", "pre-receive")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestPreparedCommitPublicationTellsALeaseLossFromARefusedPush(t *testing.T) {
	for _, tc := range []struct {
		name   string
		hook   string
		stale  bool
		moved  bool
		reason string
	}{
		{
			name:  "a branch that moved before the push starts",
			stale: true,
			moved: true,
		},
		{
			name:  "a writer landing while the push runs",
			hook:  moveRefHook,
			moved: true,
		},
		{
			name:   "a ruleset refusing the commit",
			hook:   "echo 'commits must have verified signatures' >&2\nexit 1",
			reason: "commits must have verified signatures",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			commitEntry(t, dir, "feat: add export")
			other := gitOut(t, dir, "rev-parse", "HEAD")
			commitEntry(t, dir, "fix: handle empty exports")
			runGit(t, dir, "push", "-q", origin, "HEAD:refs/heads/"+mainBranch)
			expected := bareBranchSHA(t, origin)
			commitEntry(t, dir, "fix: report an empty export set")
			sha := gitOut(t, dir, "rev-parse", "HEAD")
			runGit(t, dir, "push", "-q", origin, "HEAD:refs/heads/release-me-prepared")
			if tc.stale {
				expected = other
			}
			if tc.hook != "" {
				hookOnPush(t, origin, strings.ReplaceAll(tc.hook, "{{other}}", other))
			}

			err := publishReleaseCommit(t.Context(), dir, mainBranch, sha, expected)
			if got := bareBranchSHA(t, origin); got == sha {
				t.Fatalf("tip = %s, want the rejected commit unpublished", got)
			}
			if tc.moved {
				if !errors.Is(err, forge.ErrBranchMoved) {
					t.Fatalf("error = %v, want ErrBranchMoved", err)
				}
				return
			}
			if err == nil || errors.Is(err, forge.ErrBranchMoved) {
				t.Fatalf("error = %v, want a plain failure", err)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error = %v, want it to carry %q", err, tc.reason)
			}
		})
	}
}

func TestPreparedCommitFetchErrorsNeverExposeTheRemote(t *testing.T) {
	for _, name := range []string{"private-token", "user:private-token@remote"} {
		t.Run(name, func(t *testing.T) {
			dir, _ := newPRRepo(t)
			remote := filepath.Join(t.TempDir(), name)
			runGit(t, dir, "remote", "set-url", "--push", "origin", remote)
			err := publishReleaseCommit(t.Context(), dir, mainBranch, strings.Repeat("a", 40), "")
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("fetch error = %v", err)
			}
		})
	}
}

func TestPreparedCommitPushErrorsNeverExposeTheRemote(t *testing.T) {
	for _, name := range []string{"private-token", "user:private-token@remote"} {
		t.Run(name, func(t *testing.T) {
			dir, _ := newPRRepo(t)
			remote := filepath.Join(t.TempDir(), name)
			if err := os.MkdirAll(remote, 0o755); err != nil {
				t.Fatalf("create %s: %v", remote, err)
			}
			runGit(t, remote, "init", "-q", "--bare", "-b", mainBranch)
			commitEntry(t, dir, "feat: add export")
			runGit(t, dir, "push", "-q", remote, "HEAD:refs/heads/"+mainBranch)
			commitEntry(t, dir, "fix: handle empty exports")
			sha := gitOut(t, dir, "rev-parse", "HEAD")
			runGit(t, dir, "push", "-q", remote, "HEAD:refs/heads/release-me-prepared")
			runGit(t, dir, "remote", "set-url", "--push", "origin", remote)
			hookOnPush(t, remote, "echo 'commits must have verified signatures' >&2\nexit 1")

			err := publishReleaseCommit(t.Context(), dir, mainBranch, sha, "")
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("push error = %v", err)
			}
			if !strings.Contains(err.Error(), "commits must have verified signatures") {
				t.Fatalf("push error = %v, want git's own reason in it", err)
			}
		})
	}
}
