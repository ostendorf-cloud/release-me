// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestReleaseCommitCarriesTheSignoffTrailerOnlyWhenConfigured(t *testing.T) {
	for _, family := range prFamilies {
		for _, signoff := range []string{"", "Release Bot <release-bot@example.com>"} {
			name := family.name + "/unsigned"
			if signoff != "" {
				name = family.name + "/signed"
			}
			t.Run(name, func(t *testing.T) {
				dir, origin, cfg := family.repo(t)
				cfg.Signoff = signoff
				f := newFake()
				var stdout bytes.Buffer
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
					t.Fatalf("doPR() error = %v", err)
				}
				out, err := exec.CommandContext(t.Context(), "git", "--git-dir", origin, "log", "-1",
					"--format=%s%n%(trailers:key=Signed-off-by,valueonly)", mainBranch).Output()
				if err != nil {
					t.Fatalf("git log: %v", err)
				}
				lines := strings.Split(strings.TrimSpace(string(out)), "\n")
				if lines[0] != f.CreatedPullRequests[0].Title {
					t.Fatalf("subject = %q, want the title %q", lines[0], f.CreatedPullRequests[0].Title)
				}
				var trailers []string
				for _, l := range lines[1:] {
					if l != "" {
						trailers = append(trailers, l)
					}
				}
				if signoff == "" && len(trailers) != 0 || signoff != "" && (len(trailers) != 1 || trailers[0] != signoff) {
					t.Fatalf("Signed-off-by trailers = %q, want %q", trailers, signoff)
				}
				if got := bareBranchAuthor(t, origin); got != "release-me <release-me[bot]@users.noreply.github.com>" {
					t.Fatalf("author = %q, want the configured author unchanged by the signoff", got)
				}
			})
		}
	}
}
