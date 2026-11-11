// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
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
				f := newPRFake(origin)
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
				if got := bareBranchAuthor(t, origin); got != forgetest.FakeAuthor {
					t.Fatalf("author = %q, want the forge's own %q unchanged by the signoff", got, forgetest.FakeAuthor)
				}
			})
		}
	}
}

func TestSignoffChangesRefreshExistingReleaseCommitsAndThenSettle(t *testing.T) {
	for _, family := range prFamilies {
		for _, previous := range []string{"", "Old Bot <old@example.com>"} {
			t.Run(family.name+"/"+previous, func(t *testing.T) {
				dir, remote, cfg := family.repo(t)
				cfg.Signoff = previous
				f := newPRFake(remote)
				var out bytes.Buffer
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
					t.Fatal(err)
				}
				created := f.CreatedPullRequests[0]
				before := bareBranchSHA(t, remote)
				f.ListResponses = []forgetest.ListResponse{{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: before, Labels: []string{"release: pending"}}}}}
				cfg.Signoff = "Release Bot <release-bot@example.com>"
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
					t.Fatal(err)
				}
				after := bareBranchSHA(t, remote)
				if after == before {
					t.Fatal("signoff change left the existing release commit unchanged")
				}
				trailers, err := gitOutput(t.Context(), remote, "log", "-1", "--format=%(trailers:key=Signed-off-by,valueonly)", mainBranch)
				if err != nil || trailers != cfg.Signoff {
					t.Fatalf("trailers=%q,%v;want %q", trailers, err, cfg.Signoff)
				}
				updated := f.UpdatedPullRequests[len(f.UpdatedPullRequests)-1]
				f.ListResponses[0].PullRequests[0] = forge.PullRequest{Number: 1, Title: updated.Title, Body: updated.Body, HeadSHA: after, Labels: []string{"release: pending"}}
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
					t.Fatal(err)
				}
				if got := bareBranchSHA(t, remote); got != after {
					t.Fatalf("unchanged rerun moved %s to %s", after, got)
				}
			})
		}
	}
}
