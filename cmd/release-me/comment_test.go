// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/commentcmd"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
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
	if err := doComment(t.Context(), t.TempDir(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
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
	if err := doComment(t.Context(), t.TempDir(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
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
	if err := doComment(t.Context(), t.TempDir(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
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
	if err := doComment(t.Context(), t.TempDir(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
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
	if err := doComment(t.Context(), t.TempDir(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
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

// TestDoCommentAppliesAnAuthorizedReleaseAsCommand covers issue #57: a
// "release-as" command must not only write the override block, it must
// also recompute this release pull request's title and changelog in the
// same run, through the same doPR logic the next scheduled run would use,
// so merging right after the command releases the forced version, not a
// stale one.
func TestDoCommentAppliesAnAuthorizedReleaseAsCommand(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newPRFake(origin)
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{pr.Number: {PullRequest: pr}}
	withOverride := releasepr.SetVersionOverride(pr.Body, "2.0.0")
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: pr.Number, Title: pr.Title, Body: withOverride, Labels: pr.Labels},
		}},
	}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: pr.Number, association: "MEMBER", body: "@release-me release-as 2.0.0"}
	if err := doComment(t.Context(), dir, config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	// The override write, then doPR's own update: two calls, not one.
	if len(f.UpdatedPullRequests) != 2 {
		t.Fatalf("UpdatedPullRequests = %+v, want two: the override write and doPR's immediate update", f.UpdatedPullRequests)
	}
	overrideWrite := f.UpdatedPullRequests[0]
	if !strings.Contains(overrideWrite.Body, "```release-me-version\n2.0.0\n```") {
		t.Fatalf("override write body = %q, want the version override block", overrideWrite.Body)
	}

	recomputed := f.UpdatedPullRequests[1]
	if recomputed.Title != "chore(main): release v2.0.0" {
		t.Fatalf("recomputed title = %q, want the forced version applied immediately", recomputed.Title)
	}
	if !strings.Contains(recomputed.Body, "## v2.0.0") {
		t.Fatalf("recomputed body = %q, want the changelog to carry the forced version", recomputed.Body)
	}

	changelog := showBareBranchFile(t, origin, "CHANGELOG.md")
	if !strings.Contains(changelog, "## v2.0.0") {
		t.Fatalf("CHANGELOG.md on the release branch = %q, want the forced version", changelog)
	}

	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	reply := f.CreatedComments[0].Body
	if !strings.Contains(reply, "2.0.0") {
		t.Fatalf("reply = %q, want it to confirm the version", reply)
	}
	if !strings.Contains(reply, "updated this pull request's title and changelog") {
		t.Fatalf("reply = %q, want it to confirm the immediate update", reply)
	}
	if strings.Contains(reply, "next scheduled run") || strings.Contains(reply, "previously computed version") {
		t.Fatalf("reply = %q, want no lag-window warning: the update already happened", reply)
	}
}

// TestDoCommentReleaseAsUpdatesALockedFamilysPullRequestImmediately covers
// the other half of issue #57's acceptance criteria: a "release-as"
// command on a locked family's one shared release pull request must also
// recompute its title and changelog immediately, across every component.
func TestDoCommentReleaseAsUpdatesALockedFamilysPullRequestImmediately(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	writeFiles(t, dir, "feat: add a's feature", map[string]string{"a/feature.go": "package a\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	f := newPRFake(origin)
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	if !strings.Contains(created.Title, "v0.1.0") {
		t.Fatalf("created title = %q, want v0.1.0", created.Title)
	}

	f.Responses = map[int]forgetest.Response{1: {PullRequest: forge.PullRequest{
		Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"},
	}}}
	withOverride := releasepr.SetVersionOverride(created.Body, "9.0.0")
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/main", State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: withOverride, Labels: []string{"release: pending"}, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	var stdout bytes.Buffer
	flags := commentFlags{prNumber: 1, association: "OWNER", body: "@release-me release-as 9.0.0"}
	if err := doComment(t.Context(), dir, cfg, f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 2 {
		t.Fatalf("UpdatedPullRequests = %+v, want two: the override write and doPR's immediate update", f.UpdatedPullRequests)
	}
	recomputed := f.UpdatedPullRequests[1]
	if !strings.Contains(recomputed.Title, "v9.0.0") {
		t.Fatalf("recomputed title = %q, want the forced version applied immediately", recomputed.Title)
	}

	aChangelog := showBareFile(t, origin, "release-me/main", "a/CHANGELOG.md")
	bChangelog := showBareFile(t, origin, "release-me/main", "b/CHANGELOG.md")
	if !strings.Contains(aChangelog, "## v9.0.0") || !strings.Contains(bChangelog, "## v9.0.0") {
		t.Fatalf("changelogs = %q / %q, want both at the forced version", aChangelog, bChangelog)
	}

	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	if strings.Contains(f.CreatedComments[0].Body, "next scheduled run") {
		t.Fatalf("reply = %q, want no lag-window warning", f.CreatedComments[0].Body)
	}
}

// TestDoCommentReleaseAsThatIsNotForwardNeverChangesTheTitleOrChangelog
// covers the remaining acceptance criterion: a "release-as" version that
// parses but is not greater than the last released version is still
// ignored by the immediate recompute, exactly the way doPR already ignores
// it on a later scheduled run. The override is written for visibility, as
// it always was, but the title and the changelog it drives never change,
// and the reply names the refusal instead of falsely claiming an update.
func TestDoCommentReleaseAsThatIsNotForwardNeverChangesTheTitleOrChangelog(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the original feature")
	runGit(t, dir, "tag", "v5.0.0")
	commitEntry(t, dir, "feat: add the next command")

	f := newPRFake(origin)
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	if !strings.Contains(created.Title, "v5.1.0") {
		t.Fatalf("created title = %q, want v5.1.0", created.Title)
	}

	f.Responses = map[int]forgetest.Response{1: {PullRequest: forge.PullRequest{
		Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"},
	}}}
	withOverride := releasepr.SetVersionOverride(created.Body, "1.2.3")
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: withOverride, Labels: []string{"release: pending"}, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	var stdout bytes.Buffer
	flags := commentFlags{prNumber: 1, association: "OWNER", body: "@release-me release-as 1.2.3"}
	if err := doComment(t.Context(), dir, config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v", err)
	}

	for _, u := range f.UpdatedPullRequests {
		if !strings.Contains(u.Title, "v5.1.0") {
			t.Fatalf("updated title = %q, want the computed version kept, not the backwards override", u.Title)
		}
	}
	changelog := showBareBranchFile(t, origin, "CHANGELOG.md")
	if strings.Contains(changelog, "## v1.2.3") {
		t.Fatalf("CHANGELOG.md on the release branch = %q, want the backwards override never applied", changelog)
	}
	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	reply := f.CreatedComments[0].Body
	if !strings.Contains(reply, "refused") {
		t.Fatalf("reply = %q, want it to name the refusal instead of claiming the title and changelog updated", reply)
	}
	if strings.Contains(reply, "updated this pull request's title and changelog") {
		t.Fatalf("reply = %q, want no false claim that the title and changelog were updated", reply)
	}
}

// TestDoCommentFallsBackToTheNextScheduledRunWhenTheImmediateRecomputeFails
// covers the failure-handling choice issue #57 leaves to judgment: the
// override write already durably succeeded through the forge by the time
// doPR's immediate recompute runs, so a failure there must not be reported
// as a hard error, and must not pretend the title and changelog actually
// updated. The reply names the fallback instead: the next scheduled run
// still applies the override, exactly as it always has.
func TestDoCommentFallsBackToTheNextScheduledRunWhenTheImmediateRecomputeFails(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	// Missing the "release: tagged" label makes doPR's own label check
	// fail deterministically, without touching git at all.
	f.RepoLabels = []string{"release: pending"}
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{pr.Number: {PullRequest: pr}}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: pr.Number, association: "OWNER", body: "@release-me release-as 2.0.0"}
	if err := doComment(t.Context(), dir, config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
		t.Fatalf("doComment() error = %v, want it to recover from the recompute failure, not fail the command", err)
	}

	// The override write still goes through; only the immediate recompute
	// fails, so there is exactly one update, not two.
	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want exactly one: the override write, the failed recompute wrote nothing", f.UpdatedPullRequests)
	}
	if !strings.Contains(f.UpdatedPullRequests[0].Body, "```release-me-version\n2.0.0\n```") {
		t.Fatalf("updated body = %q, want the version override block written despite the recompute failure", f.UpdatedPullRequests[0].Body)
	}

	if len(f.CreatedComments) != 1 {
		t.Fatalf("CreatedComments = %+v, want one reply", f.CreatedComments)
	}
	reply := f.CreatedComments[0].Body
	if !strings.Contains(reply, "2.0.0") {
		t.Fatalf("reply = %q, want it to confirm the override version", reply)
	}
	if !strings.Contains(reply, "next scheduled run") {
		t.Fatalf("reply = %q, want it to name the fallback instead of claiming the title and changelog already updated", reply)
	}
}

func TestDoCommentAppliesAnAuthorizedReleaseNotesCommand(t *testing.T) {
	f := newFake()
	pr := releasePR()
	f.Responses = map[int]forgetest.Response{pr.Number: {PullRequest: pr}}
	var stdout bytes.Buffer

	flags := commentFlags{prNumber: pr.Number, association: "COLLABORATOR", body: "@release-me release-notes Run the migration first."}
	if err := doComment(t.Context(), t.TempDir(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
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
	if err := doComment(t.Context(), t.TempDir(), cfg, f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
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
	if err := doComment(t.Context(), t.TempDir(), config.Default(), f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
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
	// The "release-as applied" scenario triggers an immediate doPR
	// recompute; a non-repository directory makes that recompute fail
	// cleanly instead of succeeding, so this test, which only cares about
	// the reply text never re-triggering the mention, does not also have
	// to stand up a release pull request doPR can find and update.
	dir := t.TempDir()
	var stdout bytes.Buffer
	for _, flags := range scenarios {
		if err := doComment(t.Context(), dir, cfg, f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
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
