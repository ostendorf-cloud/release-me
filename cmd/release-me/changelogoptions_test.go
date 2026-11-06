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

// prChangelog runs pr through f against a generated repository carrying
// configYAML as its release-me.yaml and a tagged initial commit, with build
// adding the history to release after it. It returns the top section of
// the CHANGELOG.md pr pushed and the body of the pull request it opened.
func prChangelog(t *testing.T, f *forgetest.Fake, configYAML string, build func(t *testing.T, dir string)) (section, body string) {
	t.Helper()
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, configYAML)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	build(t, dir)

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want one", f.CreatedPullRequests)
	}

	section, ok := changelog.TopSection(showBareBranchFile(t, origin, "CHANGELOG.md"))
	if !ok {
		t.Fatal("CHANGELOG.md carries no version section")
	}
	return section, f.CreatedPullRequests[0].Body
}

// withLinkedPullRequests returns newFake with responses configured for every
// pull request in prs, keyed by number.
func withLinkedPullRequests(prs ...forge.PullRequest) *forgetest.Fake {
	f := newFake()
	f.Responses = map[int]forgetest.Response{}
	for _, pr := range prs {
		f.Responses[pr.Number] = forgetest.Response{PullRequest: pr}
	}
	return f
}

func TestDoPRLinksCommitsAndTheIssuesAnEntryCloses(t *testing.T) {
	f := withLinkedPullRequests(forge.PullRequest{
		Number: 12, Title: "fix: handle the empty case", Body: "The parser no longer panics.\n\nCloses #7",
		URL: "https://github.com/owner/repo/pull/12",
	})
	var mergeSHA, directSHA string
	got, body := prChangelog(t, f, "changelog-commit-links: true\nchangelog-issue-references: true\n",
		func(t *testing.T, dir string) {
			t.Helper()
			mergePullRequest(t, dir, 12, "fix/empty", map[string]string{"parse.go": "package parse\n"})
			mergeSHA = headSHA(t, dir)
			runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "feat: add the widget\n\nFixes #9")
			directSHA = headSHA(t, dir)
		})

	want := "### Features\n\n" +
		"- add the widget ([" + directSHA[:7] + "](https://github.com/owner/repo/commit/" + directSHA + ")), " +
		"refs [#9](https://github.com/owner/repo/issues/9)\n\n" +
		"### Bug Fixes\n\n" +
		"- handle the empty case ([#12](https://github.com/owner/repo/pull/12)) " +
		"([" + mergeSHA[:7] + "](https://github.com/owner/repo/commit/" + mergeSHA + ")), " +
		"refs [#7](https://github.com/owner/repo/issues/7)"
	if got != want {
		t.Fatalf("CHANGELOG.md section = %q, want %q", got, want)
	}
	if !strings.Contains(body, want) {
		t.Fatalf("pull request body = %q, want it to carry %q", body, want)
	}
	if strings.Contains(strings.ToLower(body), "closes #") || strings.Contains(strings.ToLower(body), "fixes #") {
		t.Fatalf("pull request body = %q, want no closing keyword that would close an issue on merge", body)
	}
}
