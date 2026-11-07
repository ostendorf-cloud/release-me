// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
)

type interleavedCleanupForge struct {
	forge.Forge
	beforeDelete func()
}

func (f interleavedCleanupForge) PullRequests(ctx context.Context, owner, repo, head, state string) ([]forge.PullRequest, error) {
	prs, err := f.Forge.PullRequests(ctx, owner, repo, head, state)
	if state == "open" && err == nil {
		f.beforeDelete()
	}
	return prs, err
}

func TestCleanupLeasesTheReleasedHeadAtDeletion(t *testing.T) {
	for _, move := range []bool{false, true} {
		name := "deletes an unchanged released tip"
		if move {
			name = "keeps a writer arriving after lookup"
		}
		t.Run(name, func(t *testing.T) {
			dir, cfg, head := mergedReleaseRepo(t, "", mainBranch)
			remote := cleanupGitOutput(t, dir, "remote", "get-url", "--push", "origin")
			f := mergedReleaseBranchFake(mainBranch, "chore(main): release v1.3.0", head)
			winner := ""
			wrapped := interleavedCleanupForge{Forge: f, beforeDelete: func() {
				if move {
					commitEntry(t, dir, "feat: start the next release")
					winner = headSHA(t, dir)
					runGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/"+mainBranch)
				}
			}}
			var out, logs bytes.Buffer
			if err := doRelease(t.Context(), dir, cfg, "", wrapped, newLogger(&logs, false), false, &out); err != nil {
				t.Fatal(err)
			}
			if move {
				if got := cleanupGitOutput(t, remote, "rev-parse", mainBranch); got != winner {
					t.Fatalf("tip=%s, want winner %s", got, winner)
				}
				if strings.Contains(out.String(), "deleted release branch") {
					t.Fatal("reported the concurrent branch deleted")
				}
				if !strings.Contains(logs.String(), "level=WARN") {
					t.Fatalf("missing keep warning: %s", logs.String())
				}
			} else if bareRefExists(t, remote, "refs/heads/"+mainBranch) {
				t.Fatal("released branch still exists")
			}
		})
	}
}

func cleanupGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitOutput(t.Context(), dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
