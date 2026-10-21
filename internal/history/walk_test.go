// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package history_test

import (
	"context"
	"reflect"
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
			name: "parses the merge request number from a GitLab merge subject and body",
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "chore: initial commit", "")
				commitEntry(t, dir, "Merge branch 'feat-branch' into 'main'", "See merge request group/project!42")
			},
			want: []history.Entry{
				{
					PullRequestNumber: 42,
					Subject:           "Merge branch 'feat-branch' into 'main'",
					Body:              "See merge request group/project!42",
				},
				{Subject: "chore: initial commit"},
			},
		},
		{
			name: "treats a GitLab merge subject without the merge request reference as a direct commit",
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "Merge branch 'feat-branch' into 'main'", "")
			},
			want: []history.Entry{
				{Subject: "Merge branch 'feat-branch' into 'main'"},
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
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Walk() = %+v, want %+v", got, tc.want)
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Walk() = %+v, want %+v", got, want)
	}

	for _, e := range got {
		if e.Subject == "feat: work on the side branch" || e.Subject == "fix: a side branch typo" {
			t.Fatalf("Walk() includes a side branch commit: %+v", got)
		}
	}
}
