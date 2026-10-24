// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/commentcmd"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// releasePR is a release pull request fixture doComment's tests read back
// through the fake forge: an open release pull request still carrying the
// pending label, the shape doPR leaves one in.
func releasePR() forge.PullRequest {
	return forge.PullRequest{
		Number: 7,
		Title:  "chore(main): release v0.1.0",
		Body:   "Release v0.1.0\n\n<details><summary>How this release works</summary>...</details>\n",
		Labels: []string{"release: pending"},
	}
}

func TestDoCommentIgnoresACommentThatNeverNamesTheMention(t *testing.T) {
	f := newFake()
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: 7, association: "OWNER", body: "Looks good, thanks!"}
	if err := doComment(t.Context(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.CreatedComments) != 0 {
		t.Fatalf("CreatedComments = %+v, want none: an unrelated comment is left alone", f.CreatedComments)
	}
	if len(f.UpdatedPullRequests) != 0 {
		t.Fatalf("UpdatedPullRequests = %+v, want none", f.UpdatedPullRequests)
	}
}

func TestDoCommentRefusesAPullRequestThatIsNotTheReleasePullRequest(t *testing.T) {
	f := newFake()
	f.Responses = map[int]forgetest.Response{
		9: {PullRequest: forge.PullRequest{Number: 9, Title: "feat: add the thing", Body: "ordinary body", Labels: nil}},
	}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: 9, association: "OWNER", body: "@release-me release-as 1.2.3"}
	if err := doComment(t.Context(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 0 {
		t.Fatalf("UpdatedPullRequests = %+v, want none: not the release pull request", f.UpdatedPullRequests)
	}
	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	if !strings.Contains(f.CreatedComments[0].Body, "not a release pull request") {
		t.Fatalf("reply = %q, want it to explain why", f.CreatedComments[0].Body)
	}
}

func TestDoCommentRepliesWhenTheMentionNamesNoRecognizedCommand(t *testing.T) {
	f := newFake()
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{pr.Number: {PullRequest: pr}}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: pr.Number, association: "OWNER", body: "@release-me what do you think?"}
	if err := doComment(t.Context(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 0 {
		t.Fatalf("UpdatedPullRequests = %+v, want none", f.UpdatedPullRequests)
	}
	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	if !strings.Contains(f.CreatedComments[0].Body, "no recognized command") {
		t.Fatalf("reply = %q, want it to explain why", f.CreatedComments[0].Body)
	}
}

func TestDoCommentRefusesAnUnauthorizedCommenter(t *testing.T) {
	f := newFake()
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{pr.Number: {PullRequest: pr}}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: pr.Number, association: "NONE", body: "@release-me release-as 1.2.3"}
	if err := doComment(t.Context(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 0 {
		t.Fatalf("UpdatedPullRequests = %+v, want none: not authorized", f.UpdatedPullRequests)
	}
	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	if !strings.Contains(f.CreatedComments[0].Body, "OWNER, MEMBER or COLLABORATOR") {
		t.Fatalf("reply = %q, want it to explain the permission requirement", f.CreatedComments[0].Body)
	}
}

func TestDoCommentRejectsAnUnparsableReleaseAsVersion(t *testing.T) {
	f := newFake()
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{pr.Number: {PullRequest: pr}}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: pr.Number, association: "OWNER", body: "@release-me release-as latest"}
	if err := doComment(t.Context(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 0 {
		t.Fatalf("UpdatedPullRequests = %+v, want none: the version never parsed", f.UpdatedPullRequests)
	}
	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	if !strings.Contains(f.CreatedComments[0].Body, "not a parseable") {
		t.Fatalf("reply = %q, want it to name the problem", f.CreatedComments[0].Body)
	}
}

func TestDoCommentAppliesAnAuthorizedReleaseAsCommand(t *testing.T) {
	f := newFake()
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{pr.Number: {PullRequest: pr}}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: pr.Number, association: "MEMBER", body: "@release-me release-as 2.0.0"}
	if err := doComment(t.Context(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	updated := f.UpdatedPullRequests[0]
	if !strings.Contains(updated.Body, "```release-me-version\n2.0.0\n```") {
		t.Fatalf("updated body = %q, want the version override block", updated.Body)
	}
	if updated.Title != pr.Title {
		t.Fatalf("updated title = %q, want it unchanged: %q", updated.Title, pr.Title)
	}
	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	if !strings.Contains(f.CreatedComments[0].Body, "2.0.0") {
		t.Fatalf("reply = %q, want it to confirm the version", f.CreatedComments[0].Body)
	}
}

func TestDoCommentAppliesAnAuthorizedReleaseNotesCommand(t *testing.T) {
	f := newFake()
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{pr.Number: {PullRequest: pr}}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: pr.Number, association: "COLLABORATOR", body: "@release-me release-notes Run the migration first."}
	if err := doComment(t.Context(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	updated := f.UpdatedPullRequests[0]
	if !strings.Contains(updated.Body, "```release-me-notes\nRun the migration first.\n```") {
		t.Fatalf("updated body = %q, want the notes block", updated.Body)
	}
	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
}

func TestDoCommentUsesTheConfiguredMention(t *testing.T) {
	f := newFake()
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{pr.Number: {PullRequest: pr}}
	var stdout bytes.Buffer

	cfg := config.Default()
	cfg.CommentMention = "@my-release-bot"

	flags := commentFlags{prNumber: pr.Number, association: "OWNER", body: "@release-me release-as 1.2.3"}
	if err := doComment(t.Context(), cfg, f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 0 || len(f.CreatedComments) != 0 {
		t.Fatalf("UpdatedPullRequests = %+v, CreatedComments = %+v, want neither: the default mention does not match the configured one",
			f.UpdatedPullRequests, f.CreatedComments)
	}
}

// TestDoCommentChecksAuthorizationBeforeTheReleasePullRequestCheck guards
// against an unauthorized commenter using the bot as a free megaphone on
// any pull request in the repository: the authorization check must run,
// and reply, before doComment ever asks whether the pull request is the
// release one.
func TestDoCommentChecksAuthorizationBeforeTheReleasePullRequestCheck(t *testing.T) {
	f := newFake()
	// An ordinary pull request, not the release one; its labels never
	// reach isReleasePullRequest if the ordering is right, since the
	// unauthorized reply returns first.
	f.Responses = map[int]forgetest.Response{
		9: {PullRequest: forge.PullRequest{Number: 9, Title: "feat: add the thing", Body: "ordinary body"}},
	}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: 9, association: "NONE", body: "@release-me release-as 1.2.3"}
	if err := doComment(t.Context(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	got := f.CreatedComments[0].Body
	if !strings.Contains(got, "OWNER, MEMBER or COLLABORATOR") {
		t.Fatalf("reply = %q, want the authorization reason, not the release pull request one", got)
	}
	if strings.Contains(got, "release pull request") {
		t.Fatalf("reply = %q, want it to never confirm or deny pull request #9 is the release one", got)
	}
}

// TestReplyTemplatesNeverReTriggerTheMention is the regression test the
// self-triggering reply loop needs: every reply doComment can produce,
// across every branch, must never itself match commentcmd.Parse with the
// repository's own configured mention, or the bot's own comment would
// start a new issue_comment event and loop forever. The workflow job's
// own "github.event.comment.user.type != 'Bot'" guard is the other half
// of this fix; this test is the part a unit test can cover.
func TestReplyTemplatesNeverReTriggerTheMention(t *testing.T) {
	f := newFake()
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{
		pr.Number: {PullRequest: pr},
		9:         {PullRequest: forge.PullRequest{Number: 9, Title: "feat: add the thing", Body: "ordinary body"}},
	}
	cfg := config.Default()

	scenarios := []commentFlags{
		{prNumber: pr.Number, association: "OWNER", body: "@release-me what do you think?"},  // no recognized command
		{prNumber: 9, association: "OWNER", body: "@release-me release-as 1.2.3"},            // not the release pull request
		{prNumber: pr.Number, association: "NONE", body: "@release-me release-as 1.2.3"},     // unauthorized
		{prNumber: pr.Number, association: "OWNER", body: "@release-me release-as latest"},   // malformed version
		{prNumber: pr.Number, association: "OWNER", body: "@release-me release-as 2.0.0"},    // release-as applied
		{prNumber: pr.Number, association: "OWNER", body: "@release-me release-notes hello"}, // release-notes applied
	}
	var stdout bytes.Buffer
	for _, flags := range scenarios {
		if err := doComment(t.Context(), cfg, f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
			t.Fatalf("doComment() error = %v", err)
		}
	}

	if len(f.CreatedComments) != len(scenarios) {
		t.Fatalf("CreatedComments = %+v, want one reply per scenario", f.CreatedComments)
	}
	for _, c := range f.CreatedComments {
		if got := commentcmd.Parse(c.Body, cfg.CommentMention, "OWNER"); got.Matched {
			t.Fatalf("reply %q re-matches the configured mention, want Matched = false", c.Body)
		}
	}
}

func TestIsReleasePullRequest(t *testing.T) {
	t.Parallel()
	labels := config.Default().Labels
	cases := []struct {
		name     string
		prLabels []string
		want     bool
	}{
		{name: "pending", prLabels: []string{"release: pending"}, want: true},
		{name: "tagged", prLabels: []string{"release: tagged"}, want: true},
		{name: "alpha", prLabels: []string{"release: alpha"}, want: true},
		{name: "none of the release labels", prLabels: []string{"bug"}, want: false},
		{name: "no labels at all", prLabels: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isReleasePullRequest(tc.prLabels, labels); got != tc.want {
				t.Fatalf("isReleasePullRequest(%v) = %v, want %v", tc.prLabels, got, tc.want)
			}
		})
	}
}
