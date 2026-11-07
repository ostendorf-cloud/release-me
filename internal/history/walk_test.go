// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package history_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/history"
)

func TestWalk(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		since string
		want  []history.Entry
	}{
		{
			name: "reads the whole history without a since",
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "chore: initial commit", "")
				commitEntry(t, dir, "feat: add a command", "")
			},
			want: []history.Entry{
				{Subject: "feat: add a command"},
				{Subject: "chore: initial commit"},
			},
		},
		{
			name: "reads only commits since the given tag",
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "chore: initial commit", "")
				tag(t, dir, "v0.1.0")
				commitEntry(t, dir, "feat: add a command", "")
				commitEntry(t, dir, "fix: a bug", "")
			},
			since: "v0.1.0",
			want: []history.Entry{
				{Subject: "fix: a bug"},
				{Subject: "feat: add a command"},
			},
		},
		{
			name: "returns no entries when there is nothing since the tag",
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "chore: initial commit", "")
				tag(t, dir, "v0.1.0")
			},
			since: "v0.1.0",
			want:  nil,
		},
		{
			name: "parses the pull request number from a merge subject",
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "chore: initial commit", "")
				commitEntry(t, dir, "Merge pull request #42 from owner/feat-branch", "feat: add a command")
			},
			want: []history.Entry{
				{PullRequestNumber: 42, Subject: "Merge pull request #42 from owner/feat-branch", Body: "feat: add a command"},
				{Subject: "chore: initial commit"},
			},
		},
		{
			name: "treats an unrecognized merge subject as a direct commit",
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "Merge branch 'feature' into main", "")
			},
			want: []history.Entry{
				{Subject: "Merge branch 'feature' into main"},
			},
		},
		{
			name: "keeps a multi line commit body",
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "fix: rename the config key", "BREAKING CHANGE: the old key is gone\n\nsee the migration guide")
			},
			want: []history.Entry{
				{Subject: "fix: rename the config key", Body: "BREAKING CHANGE: the old key is gone\n\nsee the migration guide"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := newRepo(t)
			tc.setup(t, dir)

			got, err := history.Walk(context.Background(), dir, tc.since)
			if err != nil {
				t.Fatalf("Walk() error = %v", err)
			}
			if !reflect.DeepEqual(stripSHA(got), tc.want) {
				t.Fatalf("Walk() = %+v, want %+v", stripSHA(got), tc.want)
			}
		})
	}
}

func TestWalkExcludesCommitsOnAMergedSideBranch(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit", "")

	runGit(t, dir, "checkout", "-q", "-b", "feat-branch")
	commitEntry(t, dir, "feat: work on the side branch", "")
	commitEntry(t, dir, "fix: a side branch typo", "")
	runGit(t, dir, "checkout", "-q", "main")
	runGit(t, dir, "merge", "--no-ff", "-q", "-m", "Merge pull request #7 from owner/feat-branch", "feat-branch")

	got, err := history.Walk(context.Background(), dir, "")
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}

	want := []history.Entry{
		{PullRequestNumber: 7, Subject: "Merge pull request #7 from owner/feat-branch"},
		{Subject: "chore: initial commit"},
	}
	if !reflect.DeepEqual(stripSHA(got), want) {
		t.Fatalf("Walk() = %+v, want %+v", stripSHA(got), want)
	}

	for _, e := range got {
		if e.Subject == "feat: work on the side branch" || e.Subject == "fix: a side branch typo" {
			t.Fatalf("Walk() includes a side branch commit: %+v", got)
		}
	}
}

// stripSHA returns a copy of entries with SHA zeroed, so a test can compare
// the rest of each Entry without hardcoding a commit hash that changes on
// every run. AuthorName is zeroed too, the test identity every fixture
// commits as: TestEntriesCarryTheCommitAuthorName covers it.
func stripSHA(entries []history.Entry) []history.Entry {
	if entries == nil {
		return nil
	}
	out := make([]history.Entry, len(entries))
	for i, e := range entries {
		e.SHA = ""
		e.AuthorName = ""
		out[i] = e
	}
	return out
}

func TestEntriesCarryTheCommitAuthorName(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit", "")
	runGit(t, dir, "tag", "v0.1.0")
	runGit(t, dir, "-c", "user.name=Ada Lovelace", "-c", "user.email=ada@example.com",
		"commit", "--allow-empty", "-m", "feat: add the engine")

	walkers := map[string]func() ([]history.Entry, error){
		"Walk":       func() ([]history.Entry, error) { return history.Walk(t.Context(), dir, "v0.1.0") },
		"WalkSquash": func() ([]history.Entry, error) { return history.WalkSquash(t.Context(), dir, "v0.1.0") },
		"AllEntries": func() ([]history.Entry, error) { return history.AllEntries(t.Context(), dir, "v0.1.0") },
	}
	for name, walk := range walkers {
		got, err := walk()
		if err != nil {
			t.Fatalf("%s() error = %v", name, err)
		}
		if len(got) != 1 || got[0].AuthorName != "Ada Lovelace" {
			t.Fatalf("%s() = %+v, want one entry by Ada Lovelace", name, got)
		}
	}
}

func TestWalkPopulatesTheCommitSHA(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	commitEntry(t, dir, "feat: add a command", "")

	got, err := history.Walk(context.Background(), dir, "")
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if len(got) != 1 || got[0].SHA == "" {
		t.Fatalf("Walk() = %+v, want one entry with a non-empty SHA", got)
	}

	head := strings.TrimSpace(runGitOutput(t, dir, "rev-parse", "HEAD"))
	if got[0].SHA != head {
		t.Fatalf("SHA = %q, want %q", got[0].SHA, head)
	}
}
