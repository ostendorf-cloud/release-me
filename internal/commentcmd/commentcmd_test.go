// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package commentcmd_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/commentcmd"
)

const mention = "@release-me"

func TestParse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		body        string
		association string
		want        commentcmd.Outcome
	}{
		{
			name: "an unrelated comment never matches",
			body: "Looks good to me, thanks for the quick turnaround!",
			want: commentcmd.Outcome{Matched: false},
		},
		{
			name:        "the mention alone names no command",
			body:        "@release-me",
			association: "OWNER",
			want:        commentcmd.Outcome{Matched: true, Allowed: true},
		},
		{
			name:        "the mention alone followed by trailing punctuation names no command",
			body:        "cc @release-me, any thoughts?",
			association: "OWNER",
			want:        commentcmd.Outcome{Matched: true, Allowed: true},
		},
		{
			name:        "a valid release-as command from an owner",
			body:        "@release-me release-as 1.2.3",
			association: "OWNER",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseAs, Version: "1.2.3", Valid: true,
			},
		},
		{
			name:        "a valid release-as command from a member",
			body:        "please @release-me release-as 2.0.0 to recover from the burned tags",
			association: "MEMBER",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseAs, Version: "2.0.0", Valid: true,
			},
		},
		{
			name:        "a valid release-as command from a collaborator",
			body:        "@release-me release-as 1.0.0",
			association: "COLLABORATOR",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseAs, Version: "1.0.0", Valid: true,
			},
		},
		{
			name:        "a valid release-notes command",
			body:        "@release-me release-notes Run the migration before upgrading.",
			association: "OWNER",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseNotes,
				Notes: "Run the migration before upgrading.", Valid: true,
			},
		},
		{
			name:        "a release-notes command spanning several lines",
			body:        "@release-me release-notes Line one.\nLine two.\n",
			association: "OWNER",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseNotes,
				Notes: "Line one.\nLine two.", Valid: true,
			},
		},
		{
			name:        "release-notes with no text clears the notes",
			body:        "@release-me release-notes",
			association: "OWNER",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseNotes, Notes: "", Valid: true,
			},
		},
		{
			name:        "a command from an unauthorized association is still parsed, but not allowed",
			body:        "@release-me release-as 1.2.3",
			association: "NONE",
			want: commentcmd.Outcome{
				Matched: true, Allowed: false, Command: commentcmd.KindReleaseAs, Version: "1.2.3", Valid: true,
			},
		},
		{
			name:        "a command with no association at all is not allowed",
			body:        "@release-me release-as 1.2.3",
			association: "",
			want: commentcmd.Outcome{
				Matched: true, Allowed: false, Command: commentcmd.KindReleaseAs, Version: "1.2.3", Valid: true,
			},
		},
		{
			name:        "a malformed version is rejected, never silently accepted",
			body:        "@release-me release-as latest",
			association: "OWNER",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseAs, Valid: false,
				Reason: `"latest" after release-as is not a parseable X.Y.Z version`,
			},
		},
		{
			name:        "a release-as missing a major component is rejected",
			body:        "@release-me release-as 1.2",
			association: "OWNER",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseAs, Valid: false,
				Reason: `"1.2" after release-as is not a parseable X.Y.Z version`,
			},
		},
		{
			name:        "release-as accepts a leading v",
			body:        "@release-me release-as v3.4.5",
			association: "OWNER",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseAs, Version: "3.4.5", Valid: true,
			},
		},
		{
			name:        "a second mention embedded in unrelated text is the one that matches",
			body:        "cc @release-me-fan, unrelated. Also @release-me release-as 2.0.0 please.",
			association: "OWNER",
			want: commentcmd.Outcome{
				Matched: true, Allowed: true, Command: commentcmd.KindReleaseAs, Version: "2.0.0", Valid: true,
			},
		},
		{
			name:        "a decoy mention with no real command after it names no command",
			body:        "cc @release-me-fan, nothing else here.",
			association: "OWNER",
			want:        commentcmd.Outcome{Matched: false},
		},
		{
			name:        "a mention glued to an email-like token never matches",
			body:        "contact foo@release-me for details",
			association: "OWNER",
			want:        commentcmd.Outcome{Matched: false},
		},
		{
			name:        "an unrecognized word after the mention names no command",
			body:        "@release-me what do you think?",
			association: "OWNER",
			want:        commentcmd.Outcome{Matched: true, Allowed: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := commentcmd.Parse(tc.body, mention, tc.association)
			if got != tc.want {
				t.Fatalf("Parse() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseWithACustomMention(t *testing.T) {
	t.Parallel()
	got := commentcmd.Parse("@my-release-bot release-as 1.0.0", "@my-release-bot", "OWNER")
	want := commentcmd.Outcome{Matched: true, Allowed: true, Command: commentcmd.KindReleaseAs, Version: "1.0.0", Valid: true}
	if got != want {
		t.Fatalf("Parse() = %+v, want %+v", got, want)
	}
}

func TestAuthorized(t *testing.T) {
	t.Parallel()
	cases := []struct {
		association string
		want        bool
	}{
		{association: "OWNER", want: true},
		{association: "MEMBER", want: true},
		{association: "COLLABORATOR", want: true},
		{association: "CONTRIBUTOR", want: false},
		{association: "FIRST_TIME_CONTRIBUTOR", want: false},
		{association: "NONE", want: false},
		{association: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.association, func(t *testing.T) {
			t.Parallel()
			if got := commentcmd.Authorized(tc.association); got != tc.want {
				t.Fatalf("Authorized(%q) = %v, want %v", tc.association, got, tc.want)
			}
		})
	}
}
