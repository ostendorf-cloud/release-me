// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

func TestCommitMessageCarriesTheSignoffTrailerOnlyWhenOneIsSet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, signoff, want string
	}{
		{name: "without a signoff", want: "chore(main): release v1.2.3"},
		{
			name: "with a signoff", signoff: "Release Bot <release-bot@example.com>",
			want: "chore(main): release v1.2.3\n\nSigned-off-by: Release Bot <release-bot@example.com>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := releasepr.CommitMessage("chore(main): release v1.2.3", tc.signoff); got != tc.want {
				t.Fatalf("CommitMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckSignoffAcceptsOnlyANameAndAnEmail(t *testing.T) {
	t.Parallel()
	cases := []struct {
		identity string
		ok       bool
	}{
		{identity: "Release Bot <release-bot@example.com>", ok: true},
		{identity: "release-me[bot] <release-me[bot]@users.noreply.github.com>", ok: true},
		{identity: "release-bot@example.com"},
		{identity: "<release-bot@example.com>"},
		{identity: "Release Bot <release bot@example.com>"},
		{identity: "Release Bot <release-bot@example.com>\nSigned-off-by: Someone Else <x@example.com>"},
	}
	for _, tc := range cases {
		if err := releasepr.CheckSignoff(tc.identity); (err == nil) != tc.ok {
			t.Errorf("CheckSignoff(%q) error = %v, want ok %v", tc.identity, err, tc.ok)
		}
	}
}
