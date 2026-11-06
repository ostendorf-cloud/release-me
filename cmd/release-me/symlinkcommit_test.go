// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func TestAPIReleaseCommitPreservesTrackedSymlinks(t *testing.T) {
	for _, tc := range []struct {
		name, target                       string
		executable, symlinkedTemp, wantErr bool
	}{
		{name: "relative tracked version target", target: "internal/VERSION"},
		{name: "executable tracked target", target: "internal/VERSION", executable: true},
		{name: "target outside the repository", target: "../outside-version", wantErr: true},
		{name: "relative tracked target under symlinked temp root", target: "internal/VERSION", symlinkedTemp: true},
		{name: "executable target under symlinked temp root", target: "internal/VERSION", executable: true, symlinkedTemp: true},
		{name: "escaping target under symlinked temp root", target: "../outside-version", symlinkedTemp: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.symlinkedTemp {
				tempRoot := t.TempDir()
				link := filepath.Join(t.TempDir(), "temp-root")
				if err := os.Symlink(tempRoot, link); err != nil {
					t.Fatal(err)
				}
				t.Setenv("TMPDIR", link)
			}
			dir, remote := newPRRepo(t)
			content := "VERSION=0.0.1 # release-me: version\n"
			if tc.wantErr {
				if err := os.WriteFile(filepath.Join(dir, tc.target), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755); err != nil {
					t.Fatal(err)
				}
				writeAndCommit(t, dir, tc.target, content, "chore: add version")
				if tc.executable {
					runGit(t, dir, "update-index", "--chmod=+x", tc.target)
					runGit(t, dir, "commit", "-qm", "chore: make executable")
				}
			}
			if err := os.Symlink(tc.target, filepath.Join(dir, "VERSION")); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "add", "VERSION")
			runGit(t, dir, "commit", "-qm", "feat: add version link")
			f := newPRFake(remote)
			cfg := config.Default()
			cfg.Files = []config.File{{Path: "VERSION", Updater: "marker"}}
			var out bytes.Buffer
			err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out)
			if tc.wantErr {
				if err == nil {
					t.Fatal("escaping symlink accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := gitOut(t, remote, "ls-tree", mainBranch, "--", "VERSION"); !strings.HasPrefix(got, "120000 ") {
				t.Fatalf("link replaced: %s", got)
			}
			if got := showBareBranchFile(t, remote, "VERSION"); got != tc.target {
				t.Fatalf("link = %q", got)
			}
			if got := showBareBranchFile(t, remote, tc.target); got != "VERSION=0.1.0 # release-me: version\n" {
				t.Fatalf("target = %q", got)
			}
			mode := "100644 "
			if tc.executable {
				mode = "100755 "
			}
			if got := gitOut(t, remote, "ls-tree", mainBranch, "--", tc.target); !strings.HasPrefix(got, mode) {
				t.Fatalf("target mode: %s", got)
			}
		})
	}
}
