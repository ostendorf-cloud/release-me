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

// commits is a prChangelog build adding one empty commit per subject.
func commits(subjects ...string) func(t *testing.T, dir string) {
	return func(t *testing.T, dir string) {
		t.Helper()
		for _, subject := range subjects {
			commitEntry(t, dir, subject)
		}
	}
}

// withAuthorPullRequests returns newFake with responses configured for every
// pull request in prs, keyed by number.
func withAuthorPullRequests(prs ...forge.PullRequest) *forgetest.Fake {
	f := newFake()
	f.Responses = map[int]forgetest.Response{}
	for _, pr := range prs {
		f.Responses[pr.Number] = forgetest.Response{PullRequest: pr}
	}
	return f
}

func TestDoPRRendersTheConfiguredChangelogStyle(t *testing.T) {
	cases := []struct {
		name       string
		configYAML string
		// forge is newFake when nil.
		forge *forgetest.Fake
		build func(t *testing.T, dir string)
		want  string
	}{
		{
			name:       "changelog-sentence-case upper cases each description",
			configYAML: "changelog-sentence-case: true\n",
			build:      commits("feat(cli): add the widget", "fix: npm publish runs once"),
			want: "### Features\n\n- **cli:** Add the widget\n\n" +
				"### Bug Fixes\n\n- npm publish runs once",
		},
		{
			name:       "changelog-authors credits the pull request's login and a direct commit's author",
			configYAML: "changelog-authors: true\n",
			forge: withAuthorPullRequests(forge.PullRequest{
				Number: 12, Title: "feat: add the widget", URL: "https://github.com/owner/repo/pull/12", Author: "octocat",
			}),
			build: func(t *testing.T, dir string) {
				t.Helper()
				mergePullRequest(t, dir, 12, "feat/widget", map[string]string{"widget.go": "package widget\n"})
				runGit(t, dir, "-c", "user.name=Ada Lovelace", "commit", "--allow-empty", "-q", "-m", "fix: handle the empty case")
			},
			want: "### Features\n\n- add the widget (@octocat) ([#12](https://github.com/owner/repo/pull/12))\n\n" +
				"### Bug Fixes\n\n- handle the empty case (Ada Lovelace)",
		},
		{
			name: "without changelog-authors no one is credited",
			forge: withAuthorPullRequests(forge.PullRequest{
				Number: 12, Title: "feat: add the widget", URL: "https://github.com/owner/repo/pull/12", Author: "octocat",
			}),
			build: func(t *testing.T, dir string) {
				t.Helper()
				mergePullRequest(t, dir, 12, "feat/widget", map[string]string{"widget.go": "package widget\n"})
			},
			want: "### Features\n\n- add the widget ([#12](https://github.com/owner/repo/pull/12))",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.forge
			if f == nil {
				f = newFake()
			}
			got, body := prChangelog(t, f, tc.configYAML, tc.build)
			if got != tc.want {
				t.Fatalf("CHANGELOG.md section = %q, want %q", got, tc.want)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("pull request body = %q, want it to carry %q", body, tc.want)
			}
		})
	}
}
