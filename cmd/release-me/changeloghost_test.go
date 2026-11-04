// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// withHostedPullRequests returns newFake with responses configured for every
// pull request in prs, keyed by number.
func withHostedPullRequests(prs ...forge.PullRequest) *forgetest.Fake {
	f := newFake()
	f.Responses = map[int]forgetest.Response{}
	for _, pr := range prs {
		f.Responses[pr.Number] = forgetest.Response{PullRequest: pr}
	}
	return f
}

func TestDoPRPointsEveryChangelogLinkAtTheChangelogHost(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "changelog-host: https://github.example.com/\n")
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	mergePullRequest(t, dir, 12, "fix/empty", map[string]string{"parse.go": "package parse\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := withHostedPullRequests(forge.PullRequest{
		Number: 12, Title: "fix: handle the empty case", URL: "https://github.com/owner/repo/pull/12",
	})
	if err := doPR(t.Context(), dir, cfg, "", withChangelogHost(cfg, f), discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	content := showBareBranchFile(t, origin, "CHANGELOG.md")
	h, ok := changelog.ParseHeading(content)
	if want := "https://github.example.com/owner/repo/compare/v0.1.0...v0.1.1"; !ok || h.CompareURL != want {
		t.Fatalf("heading = %+v, %v, want it read back with compare link %q", h, ok, want)
	}
	section, _ := changelog.TopSection(content)
	if want := "### Bug Fixes\n\n- handle the empty case ([#12](https://github.example.com/owner/repo/pull/12))"; section != want {
		t.Fatalf("CHANGELOG.md section = %q, want %q", section, want)
	}
	if body := f.CreatedPullRequests[0].Body; strings.Contains(body, "https://github.com/") {
		t.Fatalf("pull request body = %q, want no link left on github.com", body)
	}
}

func TestChangelogHostKeepsAPathPrefixAndLeavesOtherCallsAlone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.ChangelogStyle.Host = "https://code.example.com/github"
	f := withChangelogHost(cfg, newFake())

	if got, want := f.CompareURL("owner", "repo", "v1.0.0", "v1.1.0"), "https://code.example.com/github/owner/repo/compare/v1.0.0...v1.1.0"; got != want {
		t.Fatalf("CompareURL() = %q, want %q", got, want)
	}
	if got := withChangelogHost(config.Default(), newFake()).CompareURL("owner", "repo", "v1.0.0", "v1.1.0"); got != "https://github.com/owner/repo/compare/v1.0.0...v1.1.0" {
		t.Fatalf("CompareURL() without changelog-host = %q, want the forge's own", got)
	}
	created, err := f.CreatePullRequest(t.Context(), "owner", "repo", "main", "release-me/main", "chore: release v1.0.0", "", nil)
	if err != nil || !strings.HasPrefix(created.URL, "https://github.com/") {
		t.Fatalf("CreatePullRequest() = %+v, %v, want the release pull request's own URL untouched", created, err)
	}
}
