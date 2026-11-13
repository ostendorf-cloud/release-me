// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"reflect"
	"testing"
	"time"

	gogithub "github.com/google/go-github/v93/github"
	"github.com/ostendorf-cloud/release-me/internal/forge"
)

// TestToPullRequest pins the merged mapping without the network: the list
// endpoint never sets go-github's merged field, only merged_at, while the
// single pull request endpoint sets merged directly.
func TestToPullRequest(t *testing.T) {
	t.Parallel()
	mergedAt := gogithub.Timestamp{Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	cases := []struct {
		name string
		pr   *gogithub.PullRequest
		want forge.PullRequest
	}{
		{
			name: "a pull request carrying only the merge timestamp maps to merged true",
			pr: &gogithub.PullRequest{
				Number:   new(9),
				Title:    new("chore(main): release v0.2.0"),
				MergedAt: &mergedAt,
			},
			want: forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{}, Merged: true},
		},
		{
			name: "a pull request carrying neither field maps to merged false",
			pr: &gogithub.PullRequest{
				Number: new(10),
				Title:  new("fix(cli): unrelated change"),
			},
			want: forge.PullRequest{Number: 10, Title: "fix(cli): unrelated change", Labels: []string{}},
		},
		{
			name: "a pull request carrying only the merged field maps to merged true",
			pr: &gogithub.PullRequest{
				Number: new(11),
				Title:  new("chore(main): release v0.3.0"),
				Merged: new(true),
			},
			want: forge.PullRequest{Number: 11, Title: "chore(main): release v0.3.0", Labels: []string{}, Merged: true},
		},
		{
			name: "a pull request explicitly not merged maps to merged false",
			pr: &gogithub.PullRequest{
				Number: new(12),
				Title:  new("chore(main): release v0.4.0"),
				Merged: new(false),
			},
			want: forge.PullRequest{Number: 12, Title: "chore(main): release v0.4.0", Labels: []string{}},
		},
		{
			name: "a body saved with CRLF line endings maps to LF only",
			pr: &gogithub.PullRequest{
				Number: new(13),
				Title:  new("chore(main): release v0.5.0"),
				Body:   new("Release v0.5.0\r\n\r\n```release-me-notes\r\nUpgrade note.\r\n```\r\n"),
			},
			want: forge.PullRequest{
				Number: 13, Title: "chore(main): release v0.5.0",
				Body:   "Release v0.5.0\n\n```release-me-notes\nUpgrade note.\n```\n",
				Labels: []string{},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := toPullRequest(tc.pr)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("toPullRequest() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
