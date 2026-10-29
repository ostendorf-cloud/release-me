// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package forgetest_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// gitIn runs git in dir and returns its trimmed stdout.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=release-me tests", "GIT_AUTHOR_EMAIL=release-me-tests@example.com",
		"GIT_COMMITTER_NAME=release-me tests", "GIT_COMMITTER_EMAIL=release-me-tests@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRemote returns a bare repository holding a two commit history,
// "base" with a README.md and "next" on top of it, and the two SHAs.
func newRemote(t *testing.T) (remote, base, next string) {
	t.Helper()
	work := t.TempDir()
	gitIn(t, work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# release-me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "README.md")
	gitIn(t, work, "commit", "-q", "-m", "chore: add the readme")
	base = gitIn(t, work, "rev-parse", "HEAD")
	gitIn(t, work, "commit", "-q", "--allow-empty", "-m", "feat: add the next command")
	next = gitIn(t, work, "rev-parse", "HEAD")

	remote = t.TempDir()
	gitIn(t, remote, "clone", "-q", "--bare", work, ".")
	return remote, base, next
}

func TestFakeCommitBranchWritesIntoGitDir(t *testing.T) {
	cases := []struct {
		name string
		// setup prepares the release branch in remote and returns the
		// commit to build on and the expected head to pass.
		setup     func(t *testing.T, remote, base, next string) (parent, expected string)
		wantErrIs error
	}{
		{
			name: "creates a fresh branch",
			setup: func(*testing.T, string, string, string) (string, string) {
				return "", ""
			},
		},
		{
			name: "fast-forwards an existing branch with no expected head",
			setup: func(t *testing.T, remote, base, next string) (string, string) {
				gitIn(t, remote, "update-ref", "refs/heads/release-me/main", base)
				return next, ""
			},
		},
		{
			name: "refuses a non fast-forward with no expected head",
			setup: func(t *testing.T, remote, base, next string) (string, string) {
				gitIn(t, remote, "update-ref", "refs/heads/release-me/main", next)
				return base, ""
			},
			wantErrIs: forge.ErrBranchMoved,
		},
		{
			name: "replaces the branch at the expected head",
			setup: func(t *testing.T, remote, base, next string) (string, string) {
				gitIn(t, remote, "update-ref", "refs/heads/release-me/main", next)
				return base, next
			},
		},
		{
			name: "refuses a branch away from the expected head",
			setup: func(t *testing.T, remote, base, next string) (string, string) {
				gitIn(t, remote, "update-ref", "refs/heads/release-me/main", next)
				return base, base
			},
			wantErrIs: forge.ErrBranchMoved,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remote, base, next := newRemote(t)
			parent, expected := tc.setup(t, remote, base, next)
			if parent == "" {
				parent = next
			}
			f := &forgetest.Fake{GitDir: remote}

			sha, err := f.CommitBranch(t.Context(), "owner", "repo", forge.BranchCommit{
				Branch:  "release-me/main",
				Parent:  parent,
				Message: "chore(main): release v0.2.0",
				Files: []forge.CommitFile{
					{Path: "CHANGELOG.md", Content: []byte("# Changelog\n\n## v0.2.0 (2026-10-29)\n")},
					{Path: "scripts/install.sh", Content: []byte("#!/bin/sh\n"), Executable: true},
				},
				ExpectedHead: expected,
			})
			if len(f.CommitBranchCalls) != 1 {
				t.Fatalf("CommitBranchCalls = %+v, want one", f.CommitBranchCalls)
			}
			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("CommitBranch() error = %v, want %v", err, tc.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("CommitBranch() error = %v", err)
			}

			if got := gitIn(t, remote, "rev-parse", "release-me/main"); got != sha {
				t.Fatalf("branch = %s, want %s", got, sha)
			}
			if got := gitIn(t, remote, "rev-parse", sha+"^"); got != parent {
				t.Fatalf("parent = %s, want %s", got, parent)
			}
			if got := gitIn(t, remote, "show", sha+":README.md"); got != "# release-me" {
				t.Fatalf("README.md = %q, want the parent's untouched", got)
			}
			if got := gitIn(t, remote, "show", sha+":CHANGELOG.md"); got != "# Changelog\n\n## v0.2.0 (2026-10-29)" {
				t.Fatalf("CHANGELOG.md = %q", got)
			}
			if got := strings.Fields(gitIn(t, remote, "ls-tree", sha, "--", "scripts/install.sh"))[0]; got != "100755" {
				t.Fatalf("scripts/install.sh mode = %s, want 100755", got)
			}
			if got := gitIn(t, remote, "log", "-1", "--format=%an <%ae>|%cn <%ce>|%s", sha); got != forgetest.FakeAuthor+"|"+forgetest.FakeCommitter+"|chore(main): release v0.2.0" {
				t.Fatalf("commit = %q", got)
			}
		})
	}
}

func TestFakeCommitBranchWithoutGitDirOnlyRecords(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}
	commit := forge.BranchCommit{Branch: "release-me/main", Parent: "4f1d2c3b5a69788796a5b4c3d2e1f0a9b8c7d6e5", Message: "chore(main): release v0.2.0"}

	sha, err := f.CommitBranch(t.Context(), "owner", "repo", commit)
	if err != nil || len(sha) != 40 {
		t.Fatalf("CommitBranch() = %q, %v, want a 40 character SHA", sha, err)
	}
	if len(f.CommitBranchCalls) != 1 || f.CommitBranchCalls[0].Commit.Branch != "release-me/main" {
		t.Fatalf("CommitBranchCalls = %+v", f.CommitBranchCalls)
	}

	f.CommitBranchErr = forge.ErrBranchMoved
	if _, err := f.CommitBranch(t.Context(), "owner", "repo", commit); !errors.Is(err, forge.ErrBranchMoved) {
		t.Fatalf("CommitBranch() error = %v, want the configured error", err)
	}
}
