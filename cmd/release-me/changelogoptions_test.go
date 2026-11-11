// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

func TestChangelogOptionsTranslatesChangelogSections(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		hidden   []string
		sections []config.ChangelogSection
		want     changelog.Options
	}{
		{
			name:   "without changelog-sections the default sections apply",
			hidden: []string{"chore", "docs"},
			want:   changelog.Options{HiddenTypes: []string{"chore", "docs"}},
		},
		{
			name:   "a listed section shows a type hidden-types hides",
			hidden: []string{"chore", "docs"},
			sections: []config.ChangelogSection{
				{Type: "feat", Section: "Features"},
				{Type: "chore", Section: "Miscellaneous"},
			},
			want: changelog.Options{
				HiddenTypes: []string{"docs"},
				Sections: []changelog.Section{
					{Heading: "Features", Types: []string{"feat"}},
					{Heading: "Miscellaneous", Types: []string{"chore"}},
				},
			},
		},
		{
			name:   "a hidden entry hides a type hidden-types shows",
			hidden: []string{"chore"},
			sections: []config.ChangelogSection{
				{Type: "fix", Section: "Bug Fixes"},
				{Type: "perf", Section: "Performance", Hidden: true},
			},
			want: changelog.Options{
				HiddenTypes: []string{"chore", "perf"},
				Sections:    []changelog.Section{{Heading: "Bug Fixes", Types: []string{"fix"}}},
			},
		},
		{
			name: "types sharing a heading render together where it first appears",
			sections: []config.ChangelogSection{
				{Type: "feat", Section: "Changes"},
				{Type: "deps", Section: "Dependencies"},
				{Type: "fix", Section: "Changes"},
			},
			want: changelog.Options{
				Sections: []changelog.Section{
					{Heading: "Changes", Types: []string{"feat", "fix"}},
					{Heading: "Dependencies", Types: []string{"deps"}},
				},
			},
		},
		{
			name:     "an empty list keeps only Breaking Changes",
			hidden:   []string{"chore"},
			sections: []config.ChangelogSection{},
			want:     changelog.Options{HiddenTypes: []string{"chore"}, Sections: []changelog.Section{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			comp := config.ResolvedComponent{HiddenTypes: tc.hidden, ChangelogSections: tc.sections}
			got := changelogOptions(comp)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("changelogOptions() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestChangelogOptionsLeavesTheConfiguredHiddenTypesUntouched(t *testing.T) {
	t.Parallel()
	hidden := []string{"chore", "docs"}
	comp := config.ResolvedComponent{
		HiddenTypes:       hidden,
		ChangelogSections: []config.ChangelogSection{{Type: "chore", Section: "Miscellaneous"}},
	}
	changelogOptions(comp)
	if !reflect.DeepEqual(hidden, []string{"chore", "docs"}) {
		t.Fatalf("hidden-types = %v after changelogOptions, want it unchanged", hidden)
	}
}

// prChangelog runs pr through f against a generated repository carrying
// configYAML as its release-me.yaml and a tagged initial commit, with build
// adding the history to release after it. It returns the top section of
// the CHANGELOG.md pr pushed and the body of the pull request it opened.
func prChangelog(t *testing.T, f *forgetest.Fake, configYAML string, build func(t *testing.T, dir string)) (section, body string) {
	t.Helper()
	dir, origin := newPRRepo(t)
	f.GitDir = origin
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

func TestDoPRRendersTheConfiguredChangelogSections(t *testing.T) {
	configYAML := `changelog-sections:
  - type: fix
    section: Fixes
  - type: deps
    section: Dependencies
  - type: feat
    section: New Features
`
	got, body := prChangelog(t, newFake(), configYAML, commits(
		"feat: add the widget",
		"deps: bump yaml.v3 to 3.0.1",
		"fix: handle the empty case",
	))
	want := "### Fixes\n\n- handle the empty case\n\n" +
		"### Dependencies\n\n- bump yaml.v3 to 3.0.1\n\n" +
		"### New Features\n\n- add the widget"
	if got != want {
		t.Fatalf("CHANGELOG.md section = %q, want %q", got, want)
	}
	if !strings.Contains(body, want) {
		t.Fatalf("pull request body = %q, want it to carry %q", body, want)
	}
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
