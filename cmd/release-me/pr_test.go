// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

// mainBranch is the release branch of the implicit "main" component a
// repository without "components" carries, config.Default().Resolve()'s
// single entry.
const mainBranch = "release-me/main"

// newPRRepo creates a git repository with a local identity and an "origin"
// remote whose push URL, through git's pushInsteadOf configuration, is
// rewritten to a local bare repository, so a real "git push" succeeds
// without touching the network. The fetch URL that history.Remote parses
// stays the configured GitHub one. It returns the repository directory and
// the bare repository directory.
func newPRRepo(t *testing.T) (dir, origin string) {
	t.Helper()
	origin = t.TempDir()
	runGit(t, origin, "init", "-q", "--bare", "-b", "main")

	dir = t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.name", "release-me tests")
	runGit(t, dir, "config", "user.email", "release-me-tests@example.com")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:owner/repo.git")
	runGit(t, dir, "config", "url."+origin+".pushInsteadOf", "git@github.com:owner/repo.git")
	return dir, origin
}

// newPRRepoWithoutIdentity is newPRRepo without a configured git identity
// anywhere, local or global: the commit call under test must supply its
// own, never rely on one already configured.
func newPRRepoWithoutIdentity(t *testing.T) (dir, origin string) {
	t.Helper()
	origin = t.TempDir()
	runGit(t, origin, "init", "-q", "--bare", "-b", "main")

	dir = t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:owner/repo.git")
	runGit(t, dir, "config", "url."+origin+".pushInsteadOf", "git@github.com:owner/repo.git")
	return dir, origin
}

// commitWithIdentity creates an empty commit with subject in dir, with an
// explicit author and committer passed through the subprocess environment
// rather than configuration, so a repository a test deliberately leaves
// without an identity of its own can still grow a commit history.
func commitWithIdentity(t *testing.T, dir, subject string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "commit", "-q", "--allow-empty", "-m", subject)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=release-me tests", "GIT_AUTHOR_EMAIL=release-me-tests@example.com",
		"GIT_COMMITTER_NAME=release-me tests", "GIT_COMMITTER_EMAIL=release-me-tests@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit %q: %v\n%s", subject, err, out)
	}
}

// showBareBranchFile reads path at mainBranch from a bare
// repository, failing the test when the git invocation fails.
func showBareBranchFile(t *testing.T, bareDir, path string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", bareDir, "show", mainBranch+":"+path)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git show %s:%s: %v", mainBranch, path, err)
	}
	return string(out)
}

// bareRefExists reports whether ref exists in a bare repository.
func bareRefExists(t *testing.T, bareDir, ref string) bool {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", bareDir, "rev-parse", "--verify", ref)
	return cmd.Run() == nil
}

// bareBranchSHA resolves mainBranch to its commit SHA in a bare
// repository, the head SHA a real forge would report for an open pull
// request.
func bareBranchSHA(t *testing.T, bareDir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", bareDir, "rev-parse", mainBranch)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", mainBranch, err)
	}
	return strings.TrimSpace(string(out))
}

// bareBranchAuthor returns "name <email>" of mainBranch's tip commit in a
// bare repository, failing the test when the git invocation fails.
func bareBranchAuthor(t *testing.T, bareDir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", bareDir, "log", "-1", "--format=%an <%ae>", mainBranch)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log -1 %s: %v", mainBranch, err)
	}
	return strings.TrimSpace(string(out))
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// newFake returns a Fake forge configured with the two labels pr and check
// expect to find on the repository, and the test repository's default
// branch, so tests only need to override what they actually exercise.
func newFake() *forgetest.Fake {
	return &forgetest.Fake{
		RepoLabels:        []string{"release: pending", "release: tagged", "release: alpha", "release: beta", "release: rc"},
		DefaultBranchName: "main",
	}
}

func TestDoPRCreatesTheReleasePullRequest(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want one", f.CreatedPullRequests)
	}
	created := f.CreatedPullRequests[0]
	if created.Base != "main" || created.Head != mainBranch || created.Title != "chore(main): release v0.1.0" {
		t.Fatalf("created = %+v", created)
	}
	if want := []string{"release: pending"}; !equalStrings(created.Labels, want) {
		t.Fatalf("labels = %v, want %v", created.Labels, want)
	}
	if !strings.Contains(stdout.String(), "created chore(main): release v0.1.0") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	changelog := showBareBranchFile(t, origin, "CHANGELOG.md")
	if !strings.Contains(changelog, "add the next command") {
		t.Fatalf("CHANGELOG.md on the release branch = %q", changelog)
	}
}

// TestDoPRCommitsTheReleaseBranchWithTheDefaultAuthorWhenNoIdentityExists
// reproduces a runner with no git identity at all: HOME points at an empty
// directory and GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM are both /dev/null,
// so neither a global nor a system configuration can supply one, and the
// repository itself carries none either. The commit on the release branch
// must still succeed, carrying the default author.
func TestDoPRCommitsTheReleaseBranchWithTheDefaultAuthorWhenNoIdentityExists(t *testing.T) {
	dir, origin := newPRRepoWithoutIdentity(t)
	commitWithIdentity(t, dir, "feat: add the next command")

	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	f := newFake()
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if !bareRefExists(t, origin, "refs/heads/"+mainBranch) {
		t.Fatal("no commit landed on the release branch")
	}
	want := "release-me <release-me[bot]@users.noreply.github.com>"
	if got := bareBranchAuthor(t, origin); got != want {
		t.Fatalf("author = %q, want %q", got, want)
	}
}

// TestDoPRLetsAnEnvironmentIdentityWinOverTheConfiguredAuthor exercises the
// precedence rule: GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME
// and GIT_COMMITTER_EMAIL already set in the process environment win over
// the configured author, even though no other identity exists.
func TestDoPRLetsAnEnvironmentIdentityWinOverTheConfiguredAuthor(t *testing.T) {
	dir, origin := newPRRepoWithoutIdentity(t)
	commitWithIdentity(t, dir, "feat: add the next command")

	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "CI Runner")
	t.Setenv("GIT_AUTHOR_EMAIL", "ci@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "CI Runner")
	t.Setenv("GIT_COMMITTER_EMAIL", "ci@example.com")

	f := newFake()
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	want := "CI Runner <ci@example.com>"
	if got := bareBranchAuthor(t, origin); got != want {
		t.Fatalf("author = %q, want %q", got, want)
	}
}

func TestDoPRUpdatesWhenNewEntriesArriveSinceTheLastRun(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]

	commitEntry(t, dir, "fix: stop a panic")
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	if !strings.Contains(second.String(), "updated chore(main): release v0.1.0") {
		t.Fatalf("stdout = %q", second.String())
	}
	if !strings.Contains(f.UpdatedPullRequests[0].Body, "stop a panic") {
		t.Fatalf("updated body = %q, want the new entry", f.UpdatedPullRequests[0].Body)
	}

	changelog := showBareBranchFile(t, origin, "CHANGELOG.md")
	if !strings.Contains(changelog, "stop a panic") {
		t.Fatalf("CHANGELOG.md on the release branch = %q", changelog)
	}
}

func TestDoPRKeepsTheNotesBlockAcrossAnUpdate(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]

	withNotes, ok := addNotesBlock(created.Body, "Upgrade note: run the migration first.")
	if !ok {
		t.Fatalf("could not add a notes block to %q", created.Body)
	}
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: withNotes, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	commitEntry(t, dir, "fix: stop a panic")

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	updatedBody := f.UpdatedPullRequests[0].Body
	if !strings.Contains(updatedBody, "Upgrade note: run the migration first.") {
		t.Fatalf("updated body = %q, want the notes block kept", updatedBody)
	}
	if !strings.Contains(updatedBody, "stop a panic") {
		t.Fatalf("updated body = %q, want the new entry", updatedBody)
	}
}

func TestDoPRAppliesAVersionOverrideAndKeepsItAcrossAnUpdate(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	if !strings.Contains(created.Title, "v0.1.0") {
		t.Fatalf("created title = %q, want v0.1.0", created.Title)
	}

	withOverride := releasepr.SetVersionOverride(created.Body, "5.0.0")
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: withOverride, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	commitEntry(t, dir, "fix: stop a panic")

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	updated := f.UpdatedPullRequests[0]
	if !strings.Contains(updated.Title, "v5.0.0") {
		t.Fatalf("updated title = %q, want v5.0.0", updated.Title)
	}
	if !strings.Contains(updated.Body, "```release-me-version\n5.0.0\n```") {
		t.Fatalf("updated body = %q, want the version override block kept", updated.Body)
	}

	changelog := showBareBranchFile(t, origin, "CHANGELOG.md")
	if !strings.Contains(changelog, "## v5.0.0") {
		t.Fatalf("CHANGELOG.md on the release branch = %q, want the overridden version", changelog)
	}
}

// TestDoPRIgnoresAVersionOverrideNotGreaterThanTheLastTag guards against a
// release-as override producing a backwards release: a repository already
// at v5.0.0 commenting "release-as 1.2.3" must not ship 1.2.3 over 5.0.0.
func TestDoPRIgnoresAVersionOverrideNotGreaterThanTheLastTag(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the original feature")
	runGit(t, dir, "tag", "v5.0.0")
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	if !strings.Contains(created.Title, "v5.1.0") {
		t.Fatalf("created title = %q, want v5.1.0", created.Title)
	}

	withOverride := releasepr.SetVersionOverride(created.Body, "1.2.3")
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: withOverride, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	commitEntry(t, dir, "fix: stop a panic")

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	updated := f.UpdatedPullRequests[0]
	if !strings.Contains(updated.Title, "v5.1.0") {
		t.Fatalf("updated title = %q, want the computed version kept, not the backwards override", updated.Title)
	}
	if !strings.Contains(updated.Body, "```release-me-version\n1.2.3\n```") {
		t.Fatalf("updated body = %q, want the rejected override text kept verbatim for visibility", updated.Body)
	}
}

func TestDoPRIgnoresAnUnparsableVersionOverride(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]

	withOverride := releasepr.SetVersionOverride(created.Body, "not-a-version")
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: withOverride, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	commitEntry(t, dir, "fix: stop a panic")

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	updated := f.UpdatedPullRequests[0]
	if !strings.Contains(updated.Title, "v0.1.0") {
		t.Fatalf("updated title = %q, want the computed version kept, not the unparsable override", updated.Title)
	}
	if !strings.Contains(updated.Body, "```release-me-version\nnot-a-version\n```") {
		t.Fatalf("updated body = %q, want the unparsable override text kept verbatim", updated.Body)
	}
}

// addNotesBlock inserts a release-me-notes fenced block, holding notes,
// right after the first line of body, the same shape releasepr.Body
// renders it in.
func addNotesBlock(body, notes string) (string, bool) {
	firstLine, rest, found := strings.Cut(body, "\n")
	if !found {
		return "", false
	}
	return firstLine + "\n\n```release-me-notes\n" + notes + "\n```\n" + rest, true
}

// stripGuideBlock removes the "How this release works" guide from a
// rendered body, leaving the version line directly followed by the
// changelog block: the shape a release pull request opened before this
// guide existed still carries.
func stripGuideBlock(t *testing.T, body string) string {
	t.Helper()
	firstLine, _, found := strings.Cut(body, "\n")
	if !found {
		t.Fatalf("body has no first line: %q", body)
	}
	idx := strings.Index(body, "<details>\n<summary>Changelog</summary>")
	if idx < 0 {
		t.Fatalf("body has no changelog details block: %q", body)
	}
	return firstLine + "\n\n" + body[idx:]
}

func TestDoPRChangesNothingWhenAlreadyUpToDate(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	firstSHA := bareBranchSHA(t, origin)

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: firstSHA},
		}},
	}

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 || len(f.UpdatedPullRequests) != 0 {
		t.Fatalf("expected no further create or update, created=%d updated=%d", len(f.CreatedPullRequests), len(f.UpdatedPullRequests))
	}
	if !strings.Contains(second.String(), "release pull request up to date") {
		t.Fatalf("stdout = %q", second.String())
	}
	if got := bareBranchSHA(t, origin); got != firstSHA {
		t.Fatalf("release branch SHA = %s, want it unchanged at %s", got, firstSHA)
	}
}

// TestDoPRRendersTheGuideWithTheRealTagAndConfiguredLabelNames confirms the
// "How this release works" guide names the tag this pull request's merge
// would actually create, and the configured label names, never the
// defaults, when release-me.yaml overrides them.
func TestDoPRRendersTheGuideWithTheRealTagAndConfiguredLabelNames(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	cfg := config.Default()
	cfg.Labels.Tagged = "status: released"
	cfg.Labels.Alpha = "pre: alpha"
	cfg.Labels.Beta = "pre: beta"
	cfg.Labels.Rc = "pre: rc"

	f := &forgetest.Fake{
		RepoLabels:        []string{"release: pending", "status: released", "pre: alpha", "pre: beta", "pre: rc"},
		DefaultBranchName: "main",
	}
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	body := f.CreatedPullRequests[0].Body
	for _, want := range []string{"tags `v0.1.0`", "`status: released`", "`pre: alpha`", "`pre: beta`", "`pre: rc`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, want it to contain %q", body, want)
		}
	}
}

// TestDoPRAddsTheGuideToAnExistingPullRequestWithoutAPush covers a release
// pull request opened before the "How this release works" guide existed:
// its body lacks the guide, but the changelog entries and the bumped
// files already match the release branch's tip. The body must gain the
// guide on this run, through an update alone, with nothing pushed.
func TestDoPRAddsTheGuideToAnExistingPullRequestWithoutAPush(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]

	legacyBody := stripGuideBlock(t, created.Body)
	if strings.Contains(legacyBody, "How this release works") {
		t.Fatalf("legacy body still carries the guide: %q", legacyBody)
	}

	headSHA := bareBranchSHA(t, origin)
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: legacyBody, HeadSHA: headSHA},
		}},
	}

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	if !strings.Contains(f.UpdatedPullRequests[0].Body, "How this release works") {
		t.Fatalf("updated body = %q, want the guide added", f.UpdatedPullRequests[0].Body)
	}
	if !strings.Contains(second.String(), "updated chore(main): release v0.1.0") {
		t.Fatalf("stdout = %q", second.String())
	}
	if got := bareBranchSHA(t, origin); got != headSHA {
		t.Fatalf("release branch tip changed from %q to %q, want no push", headSHA, got)
	}
}

func TestDoPRDryRunWritesNothing(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), true, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "would create chore(main): release v0.1.0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(f.CreatedPullRequests) != 0 || len(f.UpdatedPullRequests) != 0 {
		t.Fatalf("dry run called the forge: created=%+v updated=%+v", f.CreatedPullRequests, f.UpdatedPullRequests)
	}
	if bareRefExists(t, origin, "refs/heads/"+mainBranch) {
		t.Fatal("dry run pushed a branch, want none")
	}
}

func TestDoPRPrintsNothingToReleaseWithoutAReleasableChange(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")

	f := newFake()
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if stdout.String() != "nothing to release\n" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "nothing to release\n")
	}
	if len(f.CreatedPullRequests) != 0 {
		t.Fatalf("CreatedPullRequests = %+v, want none", f.CreatedPullRequests)
	}
}

func TestDoPRBumpsAConfiguredMarkerFileOnTheReleaseBranch(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeAndCommit(t, dir, "version.go", "package main\n\nconst Version = \"0.0.0\" // release-me: version\n", "chore: add the version file")
	commitEntry(t, dir, "feat: add the next command")

	cfg := config.Default()
	cfg.Files = []config.File{{Path: "version.go", Updater: "marker"}}
	f := newFake()
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	got := showBareBranchFile(t, origin, "version.go")
	want := "package main\n\nconst Version = \"0.1.0\" // release-me: version\n"
	if got != want {
		t.Fatalf("version.go on the release branch = %q, want %q", got, want)
	}
}

func TestDoPRBumpsATypedUpdaterFileOnTheReleaseBranch(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeAndCommit(t, dir, "Chart.yaml", "apiVersion: v2\nname: chart\nversion: 0.0.0\n", "chore: add the chart")
	commitEntry(t, dir, "feat: add the next command")

	cfg := config.Default()
	cfg.Files = []config.File{{Path: "Chart.yaml", Updater: "helm"}}
	f := newFake()
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	got := showBareBranchFile(t, origin, "Chart.yaml")
	want := "apiVersion: v2\nname: chart\nversion: 0.1.0\n"
	if got != want {
		t.Fatalf("Chart.yaml on the release branch = %q, want %q", got, want)
	}
}

func TestDoPRFailsNamingThePathWhenAConfiguredFileIsMissing(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	cfg := config.Default()
	cfg.Files = []config.File{{Path: "does-not-exist.go", Updater: "marker"}}
	f := newFake()
	var stdout bytes.Buffer

	err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout)
	if err == nil {
		t.Fatal("doPR() error = nil, want an error naming the missing file")
	}
	if !strings.Contains(err.Error(), "does-not-exist.go") {
		t.Fatalf("doPR() error = %q, want it to name the path", err)
	}
}

func TestDoPRFailsNamingThePathAndKeyWhenTheKeyIsNotFound(t *testing.T) {
	dir, _ := newPRRepo(t)
	writeAndCommit(t, dir, "values.yaml", "replicaCount: 1\n", "chore: add the values file")
	commitEntry(t, dir, "feat: add the next command")

	cfg := config.Default()
	cfg.Files = []config.File{{Path: "values.yaml", Updater: "yaml", Key: "image.tag"}}
	f := newFake()
	var stdout bytes.Buffer

	err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout)
	if err == nil {
		t.Fatal("doPR() error = nil, want an error naming the path and key")
	}
	if !strings.Contains(err.Error(), "values.yaml") || !strings.Contains(err.Error(), "image.tag") {
		t.Fatalf("doPR() error = %q, want it to name the path and key", err)
	}
}

func TestDoPRDetectsAFilesConfigChangeEvenWithTheSameTitleAndBody(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeAndCommit(t, dir, "version.go", "package main\n\nconst Version = \"0.0.0\" // release-me: version\n", "chore: add the version file")
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	// release-me.yaml now lists version.go, with nothing else changed:
	// no new commit, so the title and the changelog entries are identical
	// to the first run.
	cfg := config.Default()
	cfg.Files = []config.File{{Path: "version.go", Updater: "marker"}}

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	if strings.Contains(second.String(), "up to date") {
		t.Fatalf("stdout = %q, want the file bump to be pushed, not a no-op", second.String())
	}
	got := showBareBranchFile(t, origin, "version.go")
	want := "package main\n\nconst Version = \"0.1.0\" // release-me: version\n"
	if got != want {
		t.Fatalf("version.go on the release branch = %q, want %q", got, want)
	}
}

func TestDoPRRendersAnExplicitLineWhenEveryEntryIsHidden(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: internal rework nobody sees")

	cfg := config.Default()
	cfg.HiddenTypes = []string{"feat"}
	f := newFake()
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	changelogFile := showBareBranchFile(t, origin, "CHANGELOG.md")
	if !strings.Contains(changelogFile, noUserFacingChangesLine) {
		t.Fatalf("CHANGELOG.md on the release branch = %q, want the explicit line", changelogFile)
	}
	if strings.Contains(changelogFile, "### ") {
		t.Fatalf("CHANGELOG.md on the release branch = %q, want no empty section heading", changelogFile)
	}
}

func TestDoPRUsesTheForgesDefaultBranchAsBase(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	f.DefaultBranchName = "trunk"
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if got := f.CreatedPullRequests[0].Base; got != "trunk" {
		t.Fatalf("base = %q, want %q", got, "trunk")
	}
}

func TestDoPRFallsBackToOriginHEADWhenTheForgeDefaultBranchFails(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")
	runGit(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")

	f := newFake()
	f.DefaultBranchErr = errors.New("boom")
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if got := f.CreatedPullRequests[0].Base; got != "trunk" {
		t.Fatalf("base = %q, want %q", got, "trunk")
	}
}

func TestDoPRFailsCleanlyWhenTheBaseBranchCannotBeResolved(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	f.DefaultBranchErr = errors.New("boom")
	var stdout bytes.Buffer

	err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout)
	if err == nil {
		t.Fatal("doPR() error = nil, want an error")
	}
	if len(f.CreatedPullRequests) != 0 {
		t.Fatalf("CreatedPullRequests = %+v, want none", f.CreatedPullRequests)
	}
}

func TestDoPRRejectsAMissingLabelBeforeWritingAnything(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	f.RepoLabels = []string{"release: pending"} // "release: tagged" is missing

	err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, io.Discard)
	if err == nil {
		t.Fatal("doPR() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "release: tagged") {
		t.Fatalf("error = %v, want it to name the missing label", err)
	}
	if bareRefExists(t, origin, "refs/heads/"+mainBranch) {
		t.Fatal("a branch was pushed despite the missing label")
	}
}

func TestDoPRKeepsAHumanAddedLabelOnUpdate(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var first bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &first); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	f.PRLabels = map[int][]string{1: {"release: pending", "needs human"}}
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{
				Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending", "needs human"},
				HeadSHA: bareBranchSHA(t, origin),
			},
		}},
	}

	commitEntry(t, dir, "fix: stop a panic")

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	got := f.PRLabels[1]
	want := []string{"release: pending", "needs human"}
	if !equalStrings(got, want) {
		t.Fatalf("Labels() = %v, want %v, the human-added label kept", got, want)
	}
}

// TestDoPRFollowsThePreReleaseLabelThroughATypeChangeBackToStableAndToRC
// walks a single release pull request through a label alpha, then beta,
// then no pre-release label at all, then rc, exercising "pr reads the
// label and renders the pre-release version", "removing the label returns
// it to the stable version" and "the counter counts up from the last
// pre-release tag of that type".
func TestDoPRFollowsThePreReleaseLabelThroughATypeChangeBackToStableAndToRC(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var out bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &out); err != nil {
		t.Fatalf("doPR() create error = %v", err)
	}
	pr := f.CreatedPullRequests[0]
	if pr.Title != "chore(main): release v0.1.0" {
		t.Fatalf("created title = %q, want the stable version", pr.Title)
	}

	setLabel := func(label string) {
		labels := []string{"release: pending"}
		if label != "" {
			labels = append(labels, label)
		}
		f.ListResponses = []forgetest.ListResponse{
			{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
				{Number: 1, Title: pr.Title, Body: pr.Body, Labels: labels, HeadSHA: bareBranchSHA(t, origin)},
			}},
		}
	}
	runAndCheck := func(step, wantTitle string) {
		t.Helper()
		var buf bytes.Buffer
		if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &buf); err != nil {
			t.Fatalf("doPR() %s error = %v", step, err)
		}
		if len(f.UpdatedPullRequests) == 0 {
			t.Fatalf("%s: no update recorded", step)
		}
		updated := f.UpdatedPullRequests[len(f.UpdatedPullRequests)-1]
		if updated.Title != wantTitle {
			t.Fatalf("%s: title = %q, want %q", step, updated.Title, wantTitle)
		}
		pr.Body = updated.Body
	}

	setLabel("release: alpha")
	runAndCheck("alpha", "chore(main): release v0.1.0-alpha.1")

	setLabel("release: beta")
	runAndCheck("beta", "chore(main): release v0.1.0-beta.1")

	setLabel("")
	runAndCheck("label removed", "chore(main): release v0.1.0")

	setLabel("release: rc")
	runAndCheck("first rc", "chore(main): release v0.1.0-rc.1")

	// Simulate release having tagged the first rc: the next one for the
	// same base version and type counts up from it. The label itself did
	// not change, but the branch moved under the first rc, so the head
	// SHA the forge reports has to be refreshed the same way a real list
	// call would before the next run leases against it.
	runGit(t, dir, "tag", "v0.1.0-rc.1")
	setLabel("release: rc")
	runAndCheck("second rc", "chore(main): release v0.1.0-rc.2")
}

func TestDoPRRejectsTwoPreReleaseLabelsAtOnce(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newFake()
	var out bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &out); err != nil {
		t.Fatalf("doPR() create error = %v", err)
	}
	pr := f.CreatedPullRequests[0]
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{
				Number: 1, Title: pr.Title, Body: pr.Body,
				Labels: []string{"release: pending", "release: beta", "release: rc"}, HeadSHA: bareBranchSHA(t, origin),
			},
		}},
	}

	err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, io.Discard)
	if err == nil {
		t.Fatal("doPR() error = nil, want an error naming both pre-release labels")
	}
	if !strings.Contains(err.Error(), "release: beta") || !strings.Contains(err.Error(), "release: rc") {
		t.Fatalf("error = %v, want it to name both labels", err)
	}
}

// TestDoPRPushesTheReleaseBranchOnEveryPreReleaseLabelTransition
// reproduces issue #28: removing or changing a pre-release label must push
// the release branch, so its changelog heading carries the version the
// label asks for, not only rewrite the pull request title. It uses
// config.Default() with no "files" configured, the shape that let the bug
// through undetected: with no bumped file whose updater embeds the
// version, nothing but the changelog heading differs when only the
// version changes.
func TestDoPRPushesTheReleaseBranchOnEveryPreReleaseLabelTransition(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	cfg := config.Default()
	f := newFake()
	var out bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
		t.Fatalf("doPR() create error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	lastSHA := bareBranchSHA(t, origin)
	if got, ok := changelog.HeadingVersion(showBareBranchFile(t, origin, "CHANGELOG.md")); !ok || got != "0.1.0" {
		t.Fatalf("created branch heading version = (%q, %v), want (%q, true)", got, ok, "0.1.0")
	}

	title, body := created.Title, created.Body
	steps := []struct {
		name  string
		label string
		want  string
	}{
		{name: "add alpha", label: "release: alpha", want: "0.1.0-alpha.1"},
		{name: "switch to beta", label: "release: beta", want: "0.1.0-beta.1"},
		{name: "switch to rc", label: "release: rc", want: "0.1.0-rc.1"},
		{name: "remove the label", label: "", want: "0.1.0"},
		{name: "add rc again", label: "release: rc", want: "0.1.0-rc.1"},
	}

	for _, step := range steps {
		labels := []string{"release: pending"}
		if step.label != "" {
			labels = append(labels, step.label)
		}
		f.ListResponses = []forgetest.ListResponse{
			{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
				{Number: 1, Title: title, Body: body, Labels: labels, HeadSHA: lastSHA},
			}},
		}

		var buf bytes.Buffer
		if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &buf); err != nil {
			t.Fatalf("%s: doPR() error = %v", step.name, err)
		}

		newSHA := bareBranchSHA(t, origin)
		if newSHA == lastSHA {
			t.Fatalf("%s: release branch SHA did not move, want a push for version %s", step.name, step.want)
		}
		lastSHA = newSHA

		changelogFile := showBareBranchFile(t, origin, "CHANGELOG.md")
		if got, ok := changelog.HeadingVersion(changelogFile); !ok || got != step.want {
			t.Fatalf("%s: changelog heading version = (%q, %v), want (%q, true)", step.name, got, ok, step.want)
		}

		if len(f.UpdatedPullRequests) == 0 {
			t.Fatalf("%s: no update recorded", step.name)
		}
		body = f.UpdatedPullRequests[len(f.UpdatedPullRequests)-1].Body
	}
}

// bareBranchPushCount counts the reflog entries mainBranch carries in a
// bare repository, the number of times something actually moved its ref
// there, core.logAllRefUpdates enabled. Counting these, instead of
// comparing the branch's current SHA to an earlier one, still catches a
// push whose commit ends up byte identical to the one already there,
// which would otherwise leave the SHA looking unchanged.
func bareBranchPushCount(t *testing.T, bareDir string) int {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", bareDir, "reflog", "show", "--format=%H", "refs/heads/"+mainBranch)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git reflog show refs/heads/%s: %v", mainBranch, err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

// TestDoPRPushesOnlyOnceForAnInitialVersionWithBuildMetadata reproduces the
// build metadata finding: initial-version is unvalidated and stays in
// effect verbatim until the first tag exists, so a value such as
// "1.2.3+build.5" reaches the changelog heading. Before the fix,
// HeadingVersion could not parse that shape, so sameAsReleaseBranch always
// reported the branch as changed and a rerun with nothing new pushed the
// branch again, producing the exact same tree every time. That rerun's
// commit is then byte identical to the one already on the branch except
// for its timestamp, which a fast rerun can share down to the second,
// leaving the branch SHA, and even the bare origin's reflog, exactly as
// unchanged as a rerun that never pushed at all: a before/after SHA or
// reflog comparison could miss the very push it is meant to catch. The
// test pins GIT_AUTHOR_DATE and GIT_COMMITTER_DATE a second apart across
// the two runs to rule that coincidence out, so counting pushes through
// the reflog reliably tells the fixed rerun, which never calls git push,
// apart from the buggy one, which does.
func TestDoPRPushesOnlyOnceForAnInitialVersionWithBuildMetadata(t *testing.T) {
	dir, origin := newPRRepo(t)
	runGit(t, origin, "config", "core.logAllRefUpdates", "true")
	commitEntry(t, dir, "feat: add the next command")

	cfg := config.Default()
	cfg.InitialVersion = "1.2.3+build.5"
	f := newFake()

	t.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:00+00:00")
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00+00:00")

	var out bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
		t.Fatalf("doPR() create error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	if got, ok := changelog.HeadingVersion(showBareBranchFile(t, origin, "CHANGELOG.md")); !ok || got != "1.2.3+build.5" {
		t.Fatalf("created branch heading version = (%q, %v), want (%q, true)", got, ok, "1.2.3+build.5")
	}
	pushesAfterCreate := bareBranchPushCount(t, origin)

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	t.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:01+00:00")
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:01+00:00")

	var rerun bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &rerun); err != nil {
		t.Fatalf("doPR() rerun error = %v", err)
	}

	if got := bareBranchPushCount(t, origin); got != pushesAfterCreate {
		t.Fatalf("release branch pushes = %d, want %d (no push for a build metadata version that already settled)", got, pushesAfterCreate)
	}
	if !strings.Contains(rerun.String(), "release pull request up to date") {
		t.Fatalf("stdout = %q, want it to report up to date", rerun.String())
	}
}

// TestDoPRPushesNothingWhenRerunWithThePreReleaseLabelStillOn guards the
// fix above against the opposite mistake: comparing the heading's version
// must not turn every rerun into a push merely because the version
// carries a pre-release suffix. With the label unchanged, the entries
// unchanged and the branch already at that pre-release version, a rerun
// must change nothing, the same way a rerun of a stable version does.
func TestDoPRPushesNothingWhenRerunWithThePreReleaseLabelStillOn(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	cfg := config.Default()
	f := newFake()
	var out bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
		t.Fatalf("doPR() create error = %v", err)
	}
	created := f.CreatedPullRequests[0]

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending", "release: rc"}, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	var addRC bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &addRC); err != nil {
		t.Fatalf("doPR() add rc error = %v", err)
	}
	afterRC := f.UpdatedPullRequests[len(f.UpdatedPullRequests)-1]
	rcSHA := bareBranchSHA(t, origin)
	if got, ok := changelog.HeadingVersion(showBareBranchFile(t, origin, "CHANGELOG.md")); !ok || got != "0.1.0-rc.1" {
		t.Fatalf("branch heading version = (%q, %v), want (%q, true)", got, ok, "0.1.0-rc.1")
	}

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: afterRC.Title, Body: afterRC.Body, Labels: []string{"release: pending", "release: rc"}, HeadSHA: rcSHA},
		}},
	}

	updatesBeforeRerun := len(f.UpdatedPullRequests)
	var rerun bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &rerun); err != nil {
		t.Fatalf("doPR() rerun error = %v", err)
	}

	if got := bareBranchSHA(t, origin); got != rcSHA {
		t.Fatalf("release branch SHA = %s, want it unchanged at %s", got, rcSHA)
	}
	if len(f.UpdatedPullRequests) != updatesBeforeRerun {
		t.Fatalf("UpdatedPullRequests = %+v, want no further update", f.UpdatedPullRequests)
	}
	if !strings.Contains(rerun.String(), "release pull request up to date") {
		t.Fatalf("stdout = %q, want it to report up to date", rerun.String())
	}
}

// writeAndCommit writes content to path inside dir and commits it.
func writeAndCommit(t *testing.T, dir, path, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	runGit(t, dir, "add", path)
	runGit(t, dir, "commit", "-q", "-m", message)
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
