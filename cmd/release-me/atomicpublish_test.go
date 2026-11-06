// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
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
