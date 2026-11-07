// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package changelog_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
)

func TestSectionsCreditsTheAuthorWhenAskedTo(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{
		{
			Type: "feat", Scope: "cli", Description: "add the next command", Author: "@octocat",
			PullRequestNumber: 5, URL: "https://github.com/owner/repo/pull/5",
			SubEntries: []changelog.Entry{{Type: "feat", Description: "wire up the flag", Author: "Ada Lovelace"}},
		},
		{Type: "fix", Description: "handle the empty case", Author: "Ada Lovelace"},
		{Type: "fix", Description: "close the file", Author: ""},
	}
	cases := []struct {
		name    string
		authors bool
		want    string
	}{
		{
			name:    "on, the author follows the description and precedes the link, sub entries carry none",
			authors: true,
			want: "### Features\n\n" +
				"- **cli:** add the next command (@octocat) ([#5](https://github.com/owner/repo/pull/5))\n" +
				"  - wire up the flag\n\n" +
				"### Bug Fixes\n\n- handle the empty case (Ada Lovelace)\n- close the file",
		},
		{
			name: "off by default",
			want: "### Features\n\n" +
				"- **cli:** add the next command ([#5](https://github.com/owner/repo/pull/5))\n" +
				"  - wire up the flag\n\n" +
				"### Bug Fixes\n\n- handle the empty case\n- close the file",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := changelog.Sections(entries, changelog.Options{Authors: tc.authors})
			if got != tc.want {
				t.Fatalf("Sections() = %q, want %q", got, tc.want)
			}
		})
	}
}
