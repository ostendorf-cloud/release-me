// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// leftoverCase is the state a pr run meets: the release branch an earlier
// run pushed, at old, what was done to it since, and what the forge
// reports about pull requests on it.
type leftoverCase struct {
	name string
	// signoff configures a Signed-off-by trailer on the release commit.
	signoff string
	// skipFirst leaves the remote branch absent: a first publication.
	skipFirst bool
	// prs returns the pull requests the forge lists for the branch.
	prs func(old string) []forgetest.ListResponse
	// humanCommit, when set, is the subject of a commit pushed by hand on
	// top of the release commit after the pull request closed.
	humanCommit string
	// moveBranch, when true, moves the branch to an unrelated commit before
	// the run, so an open pull request's head no longer matches.
	moveBranch bool

	wantErrIs     error
	wantErrHas    []string
	wantCreated   bool
	wantUnchanged bool
}

func closedPR(merged bool, labels ...string) func(string) []forgetest.ListResponse {
	return func(old string) []forgetest.ListResponse {
		return []forgetest.ListResponse{{
			Head: mainBranch, State: "closed",
			PullRequests: []forge.PullRequest{{Number: 34, Title: "chore(main): release v0.1.0", Merged: merged, Labels: labels, HeadSHA: old}},
		}}
	}
}

// TestDoPRRecoversAReleaseBranchNoOpenPullRequestUses covers a release
// branch left behind by a release pull request that is gone: closed
// without a merge, merged without the branch being deleted, or never
// opened because the run that pushed the branch failed to create it. The
// next run replaces the branch under a lease on the commit it just read
// and opens a new release pull request from it. A head commit that is not
// a release commit is left alone, and an open pull request whose branch
// moved still fails with forge.ErrBranchMoved.
func TestDoPRRecoversAReleaseBranchNoOpenPullRequestUses(t *testing.T) {
	cases := []leftoverCase{
		{
			name:        "a first publication creates the branch",
			skipFirst:   true,
			wantCreated: true,
		},
		{
			name:        "a pull request closed without a merge left its branch",
			prs:         closedPR(false, "release: pending"),
			wantCreated: true,
		},
		{
			name:        "a merged and tagged pull request left its branch",
			prs:         closedPR(true, "release: tagged"),
			wantCreated: true,
		},
		{
			name:        "a run that pushed the branch but failed to open the pull request",
			wantCreated: true,
		},
		{
			name:        "a closed pull request left a signed off release commit",
			signoff:     "Release Bot <release-bot@example.com>",
			prs:         closedPR(false, "release: pending"),
			wantCreated: true,
		},
		{
			name:        "a commit pushed by hand onto the leftover branch is left alone",
			prs:         closedPR(false, "release: pending"),
			humanCommit: "docs: reword the upgrade note",
			wantErrIs:   errForeignReleaseBranch,
			wantErrHas: []string{
				"component main", "release branch " + mainBranch, "docs: reword the upgrade note",
				"not a release commit under the configured title patterns",
				"its message does not match the configured title patterns", "delete the branch " + mainBranch,
			},
			wantUnchanged: true,
		},
		{
			name: "an open pull request whose branch moved still reports the moved branch",
			prs: func(old string) []forgetest.ListResponse {
				return []forgetest.ListResponse{{
					Head: mainBranch, State: "open",
					PullRequests: []forge.PullRequest{{Number: 7, Labels: []string{"release: pending"}, HeadSHA: old}},
				}}
			},
			moveBranch:    true,
			wantErrIs:     forge.ErrBranchMoved,
			wantErrHas:    []string{"release branch " + mainBranch, "pull request #7"},
			wantUnchanged: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			commitEntry(t, dir, "feat: add the export command")
			cfg := config.Default()
			cfg.Signoff = tc.signoff
			f := newPRFake(origin)

			old := ""
			if !tc.skipFirst {
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &bytes.Buffer{}); err != nil {
					t.Fatalf("first doPR() error = %v", err)
				}
				old = bareBranchSHA(t, origin)
			}
			if tc.humanCommit != "" {
				clone := cloneReleaseBranch(t, origin)
				runGit(t, clone, "commit", "-q", "--allow-empty", "-m", tc.humanCommit)
				runGit(t, clone, "push", "-q", "origin", "HEAD:refs/heads/"+mainBranch)
			}
			if tc.moveBranch {
				runGit(t, origin, "update-ref", "refs/heads/"+mainBranch, gitOut(t, dir, "rev-parse", "HEAD"))
			}
			before := ""
			if !tc.skipFirst {
				before = bareBranchSHA(t, origin)
			}

			commitEntry(t, dir, "fix: keep the export order stable")
			head := gitOut(t, dir, "rev-parse", "HEAD")
			f.CreatedPullRequests = nil
			f.ListResponses = nil
			if tc.prs != nil {
				f.ListResponses = tc.prs(old)
			}

			err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &bytes.Buffer{})
			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("doPR() error = %v, want %v", err, tc.wantErrIs)
				}
				for _, want := range tc.wantErrHas {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("doPR() error = %q, want it to name %q", err, want)
					}
				}
			} else if err != nil {
				t.Fatalf("doPR() error = %v", err)
			}

			if tc.wantUnchanged {
				if got := bareBranchSHA(t, origin); got != before {
					t.Fatalf("branch = %s, want it left at %s", got, before)
				}
				if len(f.CreatedPullRequests)+len(f.UpdatedPullRequests) != 0 {
					t.Fatalf("created %+v, updated %+v, want no pull request written", f.CreatedPullRequests, f.UpdatedPullRequests)
				}
				return
			}
			if tc.wantCreated && len(f.CreatedPullRequests) != 1 {
				t.Fatalf("CreatedPullRequests = %+v, want one", f.CreatedPullRequests)
			}
			if got := gitOut(t, origin, "rev-parse", mainBranch+"^"); got != head {
				t.Fatalf("branch tip's parent = %s, want the default branch's HEAD %s", got, head)
			}
			if got := showBareBranchFile(t, origin, "CHANGELOG.md"); !strings.Contains(got, "keep the export order stable") {
				t.Fatalf("CHANGELOG.md on the branch = %q, want the new entry", got)
			}
		})
	}
}

// TestDoPRRefusesAForeignCommitBeforeCreatingAnything pins where the
// refusal happens: before the forge creates a release commit, so a
// refused run leaves no orphan object behind and changes nothing.
func TestDoPRRefusesAForeignCommitBeforeCreatingAnything(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the export command")
	f := newPRFake(origin)
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &bytes.Buffer{}); err != nil {
		t.Fatalf("first doPR() error = %v", err)
	}
	clone := cloneReleaseBranch(t, origin)
	runGit(t, clone, "commit", "-q", "--allow-empty", "-m", "chore(main): release v0.1.0", "-m", "Hand edited after the pull request closed.")
	runGit(t, clone, "push", "-q", "origin", "HEAD:refs/heads/"+mainBranch)
	commitEntry(t, dir, "fix: keep the export order stable")
	calls := len(f.CommitBranchCalls)

	err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &bytes.Buffer{})
	if !errors.Is(err, errForeignReleaseBranch) {
		t.Fatalf("doPR() error = %v, want errForeignReleaseBranch for a release subject with a body of its own", err)
	}
	if len(f.CommitBranchCalls) != calls {
		t.Fatalf("CommitBranchCalls = %d, want %d: no commit before the refusal", len(f.CommitBranchCalls), calls)
	}
}

// TestDoPRTakesOverALockedFamilysLeftoverBranch covers the shared branch a
// locked family writes: its release commit carries the shared title, which
// the takeover must read back with the shared patterns.
func TestDoPRTakesOverALockedFamilysLeftoverBranch(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	writeFiles(t, dir, "feat: add a's feature", map[string]string{"a/feature.go": "package a\n"})
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := newPRFake(origin)
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &bytes.Buffer{}); err != nil {
		t.Fatalf("first doPR() error = %v", err)
	}
	old := bareBranchSHA(t, origin)
	writeFiles(t, dir, "fix: handle b's empty input", map[string]string{"b/fix.go": "package b\n"})
	head := gitOut(t, dir, "rev-parse", "HEAD")
	f.CreatedPullRequests = nil
	f.ListResponses = []forgetest.ListResponse{{
		Head: mainBranch, State: "closed",
		PullRequests: []forge.PullRequest{{Number: 12, Labels: []string{"release: pending"}, HeadSHA: old}},
	}}

	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &bytes.Buffer{}); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want one", f.CreatedPullRequests)
	}
	if got := gitOut(t, origin, "rev-parse", mainBranch+"^"); got != head {
		t.Fatalf("branch tip's parent = %s, want the default branch's HEAD %s", got, head)
	}
}

// TestDoPRLeftoverTakeoverKeepsAConcurrentWriter covers the lease the
// takeover holds: a writer that moves the branch after the run read it,
// before or while the push runs, keeps its commit, and the run fails with
// forge.ErrBranchMoved without opening a pull request.
func TestDoPRLeftoverTakeoverKeepsAConcurrentWriter(t *testing.T) {
	for _, during := range []bool{false, true} {
		name := "a writer between the read and the push"
		if during {
			name = "a writer landing while the push runs"
		}
		t.Run(name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			commitEntry(t, dir, "feat: add the export command")
			f := newPRFake(origin)
			if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &bytes.Buffer{}); err != nil {
				t.Fatalf("first doPR() error = %v", err)
			}
			old := bareBranchSHA(t, origin)
			commitEntry(t, dir, "fix: keep the export order stable")
			winner := gitOut(t, dir, "rev-parse", "HEAD")
			f.CreatedPullRequests = nil
			f.ListResponses = closedPR(false, "release: pending")(old)

			var forgeUnderTest forge.Forge = f
			if during {
				hookOnPush(t, origin, strings.ReplaceAll(moveRefHook, "{{other}}", winner))
			} else {
				forgeUnderTest = interleavedCommitForge{Forge: f, afterCommit: func() {
					runGit(t, origin, "update-ref", "refs/heads/"+mainBranch, winner)
				}}
			}

			err := doPR(t.Context(), dir, config.Default(), "", forgeUnderTest, discardLogger(), false, &bytes.Buffer{})
			if !errors.Is(err, forge.ErrBranchMoved) {
				t.Fatalf("doPR() error = %v, want ErrBranchMoved", err)
			}
			if got := bareBranchSHA(t, origin); got != winner {
				t.Fatalf("branch = %s, want the concurrent writer's %s", got, winner)
			}
			if len(f.CreatedPullRequests) != 0 {
				t.Fatalf("CreatedPullRequests = %+v, want none after a lost lease", f.CreatedPullRequests)
			}
		})
	}
}

// TestPreparedCommitPublicationWithoutALeaseKeepsGitsReason covers a push
// without a lease that git refuses as a non fast forward: no lease was
// held, so none was lost, and the error carries git's own reason instead
// of forge.ErrBranchMoved.
func TestPreparedCommitPublicationWithoutALeaseKeepsGitsReason(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add export")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	commitEntry(t, dir, "fix: handle empty exports")
	runGit(t, dir, "push", "-q", origin, "HEAD:refs/heads/"+mainBranch)
	leftover := bareBranchSHA(t, origin)
	runGit(t, dir, "reset", "-q", "--hard", base)
	commitEntry(t, dir, "fix: report an empty export set")
	sha := gitOut(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "push", "-q", origin, "HEAD:refs/heads/release-me-prepared")

	err := publishReleaseCommit(t.Context(), dir, mainBranch, sha, "")
	if err == nil || errors.Is(err, forge.ErrBranchMoved) {
		t.Fatalf("error = %v, want a plain failure", err)
	}
	if !strings.Contains(err.Error(), "non-fast-forward") {
		t.Fatalf("error = %v, want git's reason non-fast-forward in it", err)
	}
	if strings.Contains(err.Error(), origin) {
		t.Fatalf("error = %v, want the remote named origin, not its URL", err)
	}
	if got := bareBranchSHA(t, origin); got != leftover {
		t.Fatalf("branch = %s, want it left at %s", got, leftover)
	}
}

// TestIsReleaseCommitAcceptsOnlyTheMessageThisToolWrites pins the message
// shape releasepr.CommitMessage writes: the release title alone, or
// followed by exactly one Signed-off-by trailer. Anything a human adds, a
// second trailer, another trailer, a body or text after the title, is not
// a release commit.
func TestIsReleaseCommitAcceptsOnlyTheMessageThisToolWrites(t *testing.T) {
	patterns := config.Default().TitlePatterns()
	for _, tc := range []struct {
		name    string
		message string
		want    bool
	}{
		{name: "the release title alone", message: "chore(main): release v0.1.0\n", want: true},
		{name: "one sign-off", message: "chore(main): release v0.1.0\n\nSigned-off-by: Release Bot <release-bot@example.com>\n", want: true},
		{
			name:    "two sign-offs",
			message: "chore(main): release v0.1.0\n\nSigned-off-by: Release Bot <release-bot@example.com>\nSigned-off-by: Jane Doe <jane@example.com>\n",
		},
		{name: "a co-author trailer", message: "chore(main): release v0.1.0\n\nCo-authored-by: Jane Doe <jane@example.com>\n"},
		{name: "a release title with a body", message: "chore(main): release v0.1.0\n\nReworded the upgrade note by hand.\n"},
		{name: "a release title with trailing text", message: "chore(main): release v0.1.0 and a hand fix\n"},
		{name: "another subject", message: "docs: reword the upgrade note\n"},
		{name: "an empty message", message: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isReleaseCommit(tc.message, patterns); got != tc.want {
				t.Fatalf("isReleaseCommit(%q) = %v, want %v", tc.message, got, tc.want)
			}
		})
	}
}

// TestDoPRRefusesAReleaseTitledHeadOverHandWork covers a head whose
// message looks like a release commit but which does not sit on the
// default branch: a commit copying the release title on top of hand work,
// or one with no parent at all. A release commit this tool made has the
// default branch's tip, or an older commit of it, as its only parent, so
// either head is refused and the hand work stays on the branch. The commits
// are made in a separate clone, so the run has none of their objects
// locally, the shape of a fresh CI checkout.
func TestDoPRRefusesAReleaseTitledHeadOverHandWork(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(t *testing.T, clone string)
	}{
		{
			name: "a release title on top of a hand commit",
			build: func(t *testing.T, clone string) {
				runGit(t, clone, "commit", "-q", "--allow-empty", "-m", "fix: correct the changelog wording")
				runGit(t, clone, "commit", "-q", "--allow-empty", "-m", "chore(main): release v0.1.0")
			},
		},
		{
			name: "a release title on a merge of hand work into the default branch",
			build: func(t *testing.T, clone string) {
				runGit(t, clone, "checkout", "-q", "-b", "hand-work")
				runGit(t, clone, "commit", "-q", "--allow-empty", "-m", "fix: correct the changelog wording")
				runGit(t, clone, "checkout", "-q", "--detach", "HEAD~2")
				runGit(t, clone, "merge", "-q", "--no-ff", "-m", "chore(main): release v0.1.0", "hand-work")
			},
		},
		{
			name: "a release title on a commit without a parent",
			build: func(t *testing.T, clone string) {
				runGit(t, clone, "checkout", "-q", "--orphan", "rewritten")
				runGit(t, clone, "commit", "-q", "--allow-empty", "-m", "chore(main): release v0.1.0")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			commitEntry(t, dir, "feat: add the export command")
			f := newPRFake(origin)
			if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &bytes.Buffer{}); err != nil {
				t.Fatalf("first doPR() error = %v", err)
			}
			clone := cloneReleaseBranch(t, origin)
			tc.build(t, clone)
			runGit(t, clone, "push", "-q", "-f", "origin", "HEAD:refs/heads/"+mainBranch)
			before := bareBranchSHA(t, origin)
			commitEntry(t, dir, "fix: keep the export order stable")
			f.CreatedPullRequests = nil
			f.ListResponses = closedPR(false, "release: pending")("")

			err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &bytes.Buffer{})
			if !errors.Is(err, errForeignReleaseBranch) {
				t.Fatalf("doPR() error = %v, want errForeignReleaseBranch", err)
			}
			if !strings.Contains(err.Error(), "its parent is not a commit of the default branch") {
				t.Fatalf("doPR() error = %q, want it to name the parent", err)
			}
			if got := bareBranchSHA(t, origin); got != before {
				t.Fatalf("branch = %s, want the hand work left at %s", got, before)
			}
			if len(f.CreatedPullRequests) != 0 {
				t.Fatalf("CreatedPullRequests = %+v, want none", f.CreatedPullRequests)
			}
		})
	}
}

// TestDoPRLeftoverTakeoverKeepsAHumanWritersCommit covers a person pushing
// to the leftover branch while the takeover's push runs: their commit
// stays, the run reports forge.ErrBranchMoved without the push URL in it,
// and no pull request opens.
func TestDoPRLeftoverTakeoverKeepsAHumanWritersCommit(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the export command")
	f := newPRFake(origin)
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &bytes.Buffer{}); err != nil {
		t.Fatalf("first doPR() error = %v", err)
	}
	old := bareBranchSHA(t, origin)
	clone := cloneReleaseBranch(t, origin)
	runGit(t, clone, "commit", "-q", "--allow-empty", "-m", "fix: urgent hand fix")
	human := gitOut(t, clone, "rev-parse", "HEAD")
	runGit(t, clone, "push", "-q", "origin", "HEAD:refs/heads/human-staging")
	hookOnPush(t, origin, strings.ReplaceAll(moveRefHook, "{{other}}", human))
	commitEntry(t, dir, "fix: keep the export order stable")
	f.CreatedPullRequests = nil
	f.ListResponses = closedPR(false, "release: pending")(old)

	err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &bytes.Buffer{})
	if !errors.Is(err, forge.ErrBranchMoved) {
		t.Fatalf("doPR() error = %v, want ErrBranchMoved", err)
	}
	if strings.Contains(err.Error(), origin) {
		t.Fatalf("error %q carries the push URL", err)
	}
	if got := bareBranchSHA(t, origin); got != human {
		t.Fatalf("branch = %s, want the human commit %s", got, human)
	}
	if len(f.CreatedPullRequests) != 0 {
		t.Fatalf("CreatedPullRequests = %+v, want none", f.CreatedPullRequests)
	}
}

// TestLeftoverBranchLeaseErrorsNeverExposeTheRemote covers the read of the
// leftover branch: a failing "git ls-remote" names the remote "origin",
// never a push URL that can carry a credential.
func TestLeftoverBranchLeaseErrorsNeverExposeTheRemote(t *testing.T) {
	for _, name := range []string{"private-token", "user:private-token@remote"} {
		t.Run(name, func(t *testing.T) {
			dir, _ := newPRRepo(t)
			runGit(t, dir, "remote", "set-url", "--push", "origin", filepath.Join(t.TempDir(), name))
			_, err := leftoverBranchLease(t.Context(), dir, mainBranch, config.Default().TitlePatterns(), discardLogger())
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("error = %v, want a failure naming origin only", err)
			}
		})
	}
}

// TestReleaseBranchNeverLeavesTheReleaseMeNamespace pins what makes the
// forced push safe: a release branch is always "release-me/<name>", never
// the default branch or a pattern, whatever the configuration tries.
func TestReleaseBranchNeverLeavesTheReleaseMeNamespace(t *testing.T) {
	for _, body := range []string{
		"branch: main\n",
		"release-branch: main\n",
		"components:\n  \"../main\":\n    path: a\n",
		"components:\n  \"main*\":\n    path: a\n",
	} {
		t.Run(body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "release-me.yaml")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				return
			}
			for _, c := range cfg.Resolve() {
				if !strings.HasPrefix(c.Branch, "release-me/") || strings.ContainsAny(c.Branch, "*?[") {
					t.Fatalf("branch %q leaves the release-me/ namespace", c.Branch)
				}
			}
		})
	}
}
