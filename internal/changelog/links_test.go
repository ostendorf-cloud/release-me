// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package changelog_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
)

func TestSectionsLinksCommitsAndIssueReferencesWhenAskedTo(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{
		{
			Type: "fix", Description: "handle the empty case",
			PullRequestNumber: 12, URL: "https://github.com/owner/repo/pull/12",
			CommitSHA: "8fa6c1b0d3e2f4a5b6c7d8e9f0a1b2c3d4e5f6a7",
			CommitURL: "https://github.com/owner/repo/commit/8fa6c1b0d3e2f4a5b6c7d8e9f0a1b2c3d4e5f6a7",
			Issues: []changelog.Issue{
				{Number: 7, URL: "https://github.com/owner/repo/issues/7"},
				{Number: 9, URL: "https://github.com/owner/repo/issues/9"},
			},
		},
		{Type: "fix", Description: "close the file"},
	}
	cases := []struct {
		name string
		opts changelog.Options
		want string
	}{
		{
			name: "both on, after the pull request link",
			opts: changelog.Options{CommitLinks: true, IssueReferences: true},
			want: "### Bug Fixes\n\n" +
				"- handle the empty case ([#12](https://github.com/owner/repo/pull/12)) " +
				"([8fa6c1b](https://github.com/owner/repo/commit/8fa6c1b0d3e2f4a5b6c7d8e9f0a1b2c3d4e5f6a7)), " +
				"refs [#7](https://github.com/owner/repo/issues/7) [#9](https://github.com/owner/repo/issues/9)\n" +
				"- close the file",
		},
		{
			name: "commit links only",
			opts: changelog.Options{CommitLinks: true},
			want: "### Bug Fixes\n\n" +
				"- handle the empty case ([#12](https://github.com/owner/repo/pull/12)) " +
				"([8fa6c1b](https://github.com/owner/repo/commit/8fa6c1b0d3e2f4a5b6c7d8e9f0a1b2c3d4e5f6a7))\n" +
				"- close the file",
		},
		{
			name: "issue references only",
			opts: changelog.Options{IssueReferences: true},
			want: "### Bug Fixes\n\n" +
				"- handle the empty case ([#12](https://github.com/owner/repo/pull/12)), " +
				"refs [#7](https://github.com/owner/repo/issues/7) [#9](https://github.com/owner/repo/issues/9)\n" +
				"- close the file",
		},
		{
			name: "off by default",
			want: "### Bug Fixes\n\n- handle the empty case ([#12](https://github.com/owner/repo/pull/12))\n- close the file",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := changelog.Sections(entries, tc.opts); got != tc.want {
				t.Fatalf("Sections() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSectionsLinksASubEntrysOwnCommit(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{
		Type: "feat", Description: "add the widget",
		SubEntries: []changelog.Entry{{
			Type: "feat", Description: "wire up the flag",
			CommitSHA: "d355940aa", CommitURL: "https://github.com/owner/repo/commit/d355940aa",
		}},
	}}

	got := changelog.Sections(entries, changelog.Options{CommitLinks: true})
	want := "### Features\n\n- add the widget\n  - wire up the flag ([d355940](https://github.com/owner/repo/commit/d355940aa))"
	if got != want {
		t.Fatalf("Sections() = %q, want %q", got, want)
	}
}
