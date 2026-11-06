// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package conventional_test

import (
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/conventional"
)

func TestIssueReferencesReadsGitHubsClosingKeywords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		want []int
	}{
		{name: "no reference", text: "fix: handle the empty case", want: nil},
		{name: "closes in a pull request body", text: "fix: handle the empty case\n\nCloses #12", want: []int{12}},
		{
			name: "every keyword, any case, each number once, in order",
			text: "feat: add the widget\n\nfixes #7, resolves #3\nFIXED #7\nclose #9 and Resolved: #4",
			want: []int{7, 3, 9, 4},
		},
		{name: "a plain mention is no reference", text: "fix: follow up on #12", want: nil},
		{name: "another repository's issue is left out", text: "fix: the parser\n\ncloses owner/repo#12", want: nil},
		{name: "a keyword inside a word is no keyword", text: "fix: the prefixes #12 handling", want: nil},
		{name: "a trailing (#N) is the pull request, not a reference", text: "fix: the parser (#12)", want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := conventional.IssueReferences(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("IssueReferences(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
