// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

// unlinkedForge is a forgetest.Fake whose compare view is "", standing in
// for release-me before the changelog heading carried a compare link: the
// heading it renders is the plain "## vX.Y.Z (YYYY-MM-DD)" one.
type unlinkedForge struct {
	*forgetest.Fake
}

func (unlinkedForge) CompareURL(_, _, _, _ string) string { return "" }

// headingCompareURL reads the compare link of content's first version
// heading, failing the test when content carries no version heading.
func headingCompareURL(t *testing.T, what, content string) string {
	t.Helper()
	h, ok := changelog.ParseHeading(content)
	if !ok {
		t.Fatalf("%s carries no version heading: %q", what, content)
	}
	return h.CompareURL
}

func TestDoPRLinksTheChangelogHeadingToTheCompareViewFromTheLastStableTag(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want string
	}{
		{name: "a first release links nowhere", want: ""},
		{
			name: "from the last stable tag",
			tags: []string{"v0.1.0"},
			want: "https://github.com/owner/repo/compare/v0.1.0...v0.2.0",
		},
		{
			name: "a stable release skips an earlier pre-release of its own version",
			tags: []string{"v0.1.0", "v0.2.0-rc.1"},
			want: "https://github.com/owner/repo/compare/v0.1.0...v0.2.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			commitEntry(t, dir, "chore: initial commit")
			for _, tag := range tc.tags {
				runGit(t, dir, "tag", tag)
			}
			commitEntry(t, dir, "feat: add the compare link")

			f := newPRFake(origin)
			if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, io.Discard); err != nil {
				t.Fatalf("doPR() error = %v", err)
			}
			if len(f.CreatedPullRequests) != 1 {
				t.Fatalf("CreatedPullRequests = %+v, want one", f.CreatedPullRequests)
			}
			body := f.CreatedPullRequests[0].Body

			if got := headingCompareURL(t, "CHANGELOG.md", showBareBranchFile(t, origin, "CHANGELOG.md")); got != tc.want {
				t.Fatalf("CHANGELOG.md compare link = %q, want %q", got, tc.want)
			}
			if got := headingCompareURL(t, "the pull request body", releasepr.ExtractChangelog(body)); got != tc.want {
				t.Fatalf("pull request body compare link = %q, want %q", got, tc.want)
			}
			if got := headingCompareURL(t, "the release body", releasepr.ReleaseBody(body)); got != tc.want {
				t.Fatalf("release body compare link = %q, want %q", got, tc.want)
			}
			if _, ok := releasepr.ExtractDate(body); !ok {
				t.Fatalf("ExtractDate() ok = false, want the date read back from %q", body)
			}
		})
	}
}

func TestDoPRLinksAComponentsHeadingBetweenItsOwnPrefixedTags(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "components:\n  duxt:\n    path: duxt\n    tag-prefix: duxt@v\n")
	writeFiles(t, dir, "chore: scaffold duxt", map[string]string{"duxt/README.md": "duxt\n"})
	runGit(t, dir, "tag", "duxt@v0.8.1")
	writeFiles(t, dir, "feat: add the diff link", map[string]string{"duxt/diff.go": "package duxt\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := newPRFake(origin)
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	changelogFile := showBareFile(t, origin, "release-me/duxt", "duxt/CHANGELOG.md")
	want := "## [v0.9.0](https://github.com/owner/repo/compare/duxt@v0.8.1...duxt@v0.9.0) ("
	if !strings.Contains(changelogFile, want) {
		t.Fatalf("duxt/CHANGELOG.md = %q, want a heading starting %q", changelogFile, want)
	}
}

func TestDoPRLinksAPreReleaseHeadingFromWhereItsEntriesStart(t *testing.T) {
	cases := []struct {
		name string
		// earlierRC tags v0.2.0-rc.1 after the first feature, so the next
		// pre-release collects its entries since that tag instead.
		earlierRC   bool
		wantVersion string
		want        string
	}{
		{
			name:        "the first pre-release links from the last stable tag",
			wantVersion: "0.2.0-rc.1",
			want:        "https://github.com/owner/repo/compare/v0.1.0...v0.2.0-rc.1",
		},
		{
			name:        "a later pre-release links from the earlier one",
			earlierRC:   true,
			wantVersion: "0.2.0-rc.2",
			want:        "https://github.com/owner/repo/compare/v0.2.0-rc.1...v0.2.0-rc.2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			commitEntry(t, dir, "chore: initial commit")
			tagRepo(t, dir)
			commitEntry(t, dir, "feat: add the compare link")
			if tc.earlierRC {
				runGit(t, dir, "tag", "v0.2.0-rc.1")
				commitEntry(t, dir, "fix: escape the compare link")
			}

			cfg := config.Default()
			f := newPRFake(origin)
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
				t.Fatalf("doPR() create error = %v", err)
			}
			created := f.CreatedPullRequests[0]
			f.ListResponses = []forgetest.ListResponse{
				{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
					{
						Number: 1, Title: created.Title, Body: created.Body,
						Labels: []string{"release: pending", "release: rc"}, HeadSHA: bareBranchSHA(t, origin),
					},
				}},
			}
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
				t.Fatalf("doPR() rc error = %v", err)
			}

			h, ok := changelog.ParseHeading(showBareBranchFile(t, origin, "CHANGELOG.md"))
			if !ok || h.Version != tc.wantVersion || h.CompareURL != tc.want {
				t.Fatalf("CHANGELOG.md heading = (%+v, %v), want version %q linked to %q", h, ok, tc.wantVersion, tc.want)
			}
			updated := f.UpdatedPullRequests[len(f.UpdatedPullRequests)-1]
			if got := headingCompareURL(t, "the pull request body", releasepr.ExtractChangelog(updated.Body)); got != tc.want {
				t.Fatalf("pull request body compare link = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDoPRLinksEachLockedComponentsOwnChangelog covers a locked family:
// every component's CHANGELOG.md links its own tags, while the combined
// pull request body's one heading stays unlinked, as there is no single
// compare view for tags of several components.
func TestDoPRLinksEachLockedComponentsOwnChangelog(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	runGit(t, dir, "tag", "a/v0.1.0")
	runGit(t, dir, "tag", "b/v0.1.0")
	writeFiles(t, dir, "feat: add a's feature", map[string]string{"a/feature.go": "package a\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := newPRFake(origin)
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	for _, name := range []string{"a", "b"} {
		got := headingCompareURL(t, name+"/CHANGELOG.md", showBareFile(t, origin, "release-me/main", name+"/CHANGELOG.md"))
		want := "https://github.com/owner/repo/compare/" + name + "/v0.1.0..." + name + "/v0.2.0"
		if got != want {
			t.Fatalf("%s/CHANGELOG.md compare link = %q, want %q", name, got, want)
		}
	}
	if got := headingCompareURL(t, "the pull request body", releasepr.ExtractChangelog(f.CreatedPullRequests[0].Body)); got != "" {
		t.Fatalf("pull request body compare link = %q, want none", got)
	}
}

// TestDoPRPushesTheCompareLinkOntoABranchWrittenWithoutIt covers the
// upgrade: a release branch an earlier release-me wrote, heading unlinked,
// entries the same, must still be pushed, so the open release pull
// request picks the link up instead of reporting itself up to date.
func TestDoPRPushesTheCompareLinkOntoABranchWrittenWithoutIt(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	commitEntry(t, dir, "feat: add the compare link")

	cfg := config.Default()
	f := newPRFake(origin)
	if err := doPR(t.Context(), dir, cfg, "", unlinkedForge{f}, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() without the link error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	unlinkedSHA := bareBranchSHA(t, origin)
	if got := headingCompareURL(t, "CHANGELOG.md", showBareBranchFile(t, origin, "CHANGELOG.md")); got != "" {
		t.Fatalf("CHANGELOG.md compare link = %q, want none before the upgrade", got)
	}

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, HeadSHA: unlinkedSHA},
		}},
	}
	var out bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
		t.Fatalf("doPR() with the link error = %v", err)
	}

	if bareBranchSHA(t, origin) == unlinkedSHA {
		t.Fatalf("release branch did not move, want the compare link pushed; stdout = %q", out.String())
	}
	want := "https://github.com/owner/repo/compare/v0.1.0...v0.2.0"
	if got := headingCompareURL(t, "CHANGELOG.md", showBareBranchFile(t, origin, "CHANGELOG.md")); got != want {
		t.Fatalf("CHANGELOG.md compare link = %q, want %q", got, want)
	}
	updated := f.UpdatedPullRequests[len(f.UpdatedPullRequests)-1]
	if got := headingCompareURL(t, "the pull request body", releasepr.ExtractChangelog(updated.Body)); got != want {
		t.Fatalf("pull request body compare link = %q, want %q", got, want)
	}
}

func TestRunChangelogLinksTheHeadingToTheGitHubCompareView(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	commitEntry(t, dir, "feat(cli): add the changelog command")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"changelog"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "## [v0.2.0](https://github.com/owner/repo/compare/v0.1.0...v0.2.0) ("
	if !strings.HasPrefix(stdout.String(), want) {
		t.Fatalf("stdout = %q, want it to start with %q", stdout.String(), want)
	}
}

// parenForge is a forgetest.Fake whose compare view carries parentheses,
// the shape a "(api)v" tag prefix produced before config rejected it: the
// heading it renders does not read back through changelog.ParseHeading.
type parenForge struct {
	*forgetest.Fake
}

func (parenForge) CompareURL(owner, repo, base, head string) string {
	return "https://github.com/" + owner + "/" + repo + "/compare/(api)" + base + "...(api)" + head
}

// TestDoPRSettlesWhenTheHeadingItWritesDoesNotReadBack guards against a
// push loop: a heading that does not parse would otherwise count as
// changed on every run, and every push would write that same unreadable
// heading again. The dates are pinned a second apart and pushes counted
// through the reflog, as in
// TestDoPRPushesOnlyOnceForAnInitialVersionWithBuildMetadata, so a push of
// a byte identical commit is still caught.
func TestDoPRSettlesWhenTheHeadingItWritesDoesNotReadBack(t *testing.T) {
	dir, origin := newPRRepo(t)
	runGit(t, origin, "config", "core.logAllRefUpdates", "true")
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	commitEntry(t, dir, "feat: add the compare link")

	cfg := config.Default()
	f := newPRFake(origin)
	t.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:00+00:00")
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00+00:00")
	if err := doPR(t.Context(), dir, cfg, "", parenForge{f}, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() create error = %v", err)
	}
	if _, ok := changelog.ParseHeading(showBareBranchFile(t, origin, "CHANGELOG.md")); ok {
		t.Fatal("CHANGELOG.md heading reads back, want the unreadable heading this test is about")
	}
	created := f.CreatedPullRequests[0]
	pushesAfterCreate := bareBranchPushCount(t, origin)

	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}
	t.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:01+00:00")
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:01+00:00")
	var rerun bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", parenForge{f}, discardLogger(), false, &rerun); err != nil {
		t.Fatalf("doPR() rerun error = %v", err)
	}

	if got := bareBranchPushCount(t, origin); got != pushesAfterCreate {
		t.Fatalf("release branch pushes = %d, want %d (no push for a heading no push can make readable)", got, pushesAfterCreate)
	}
	if !strings.Contains(rerun.String(), "release pull request up to date") {
		t.Fatalf("stdout = %q, want it to report up to date", rerun.String())
	}
}

// TestDoPRRewritesAReleasePleaseHeadingOnceAndThenSettles covers a
// repository migrated from release-please whose open release branch still
// carries release-please's top heading, "## [0.2.0](url) (date)", with no
// "v" before the version. That heading does not read back, while the one
// release-me writes does, so the first run pushes once, rewriting it into
// release-me's own shape, and the run after that pushes nothing.
func TestDoPRRewritesAReleasePleaseHeadingOnceAndThenSettles(t *testing.T) {
	dir, origin := newPRRepo(t)
	runGit(t, origin, "config", "core.logAllRefUpdates", "true")
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	commitEntry(t, dir, "feat: add the compare link")

	cfg := config.Default()
	f := newPRFake(origin)
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() create error = %v", err)
	}
	title, body := f.CreatedPullRequests[0].Title, f.CreatedPullRequests[0].Body

	clone := cloneBranch(t, origin, mainBranch)
	content := showBareBranchFile(t, origin, "CHANGELOG.md")
	_, fromHeading, found := strings.Cut(content, "\n## ")
	if !found {
		t.Fatalf("CHANGELOG.md carries no version heading: %q", content)
	}
	heading, _, _ := strings.Cut("## "+fromHeading, "\n")
	releasePlease := "## [0.2.0](https://github.com/owner/repo/compare/v0.1.0...v0.2.0) (2026-10-01)"
	writeAndCommit(t, clone, "CHANGELOG.md", strings.Replace(content, heading, releasePlease, 1), "chore: release 0.2.0")
	runGit(t, clone, "push", "-q", "--force", "origin", "HEAD:"+mainBranch)
	if _, ok := changelog.ParseHeading(showBareBranchFile(t, origin, "CHANGELOG.md")); ok {
		t.Fatal("release-please heading reads back, want the shape release-me cannot read")
	}

	listOpen := func() {
		f.ListResponses = []forgetest.ListResponse{
			{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
				{Number: 1, Title: title, Body: body, Labels: []string{"release: pending"}, HeadSHA: bareBranchSHA(t, origin)},
			}},
		}
	}
	listOpen()
	pushesBefore := bareBranchPushCount(t, origin)
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() rewrite error = %v", err)
	}
	if got := bareBranchPushCount(t, origin); got != pushesBefore+1 {
		t.Fatalf("release branch pushes = %d, want %d (one push rewriting the heading)", got, pushesBefore+1)
	}
	want := "https://github.com/owner/repo/compare/v0.1.0...v0.2.0"
	if got := headingCompareURL(t, "CHANGELOG.md", showBareBranchFile(t, origin, "CHANGELOG.md")); got != want {
		t.Fatalf("CHANGELOG.md compare link = %q, want %q", got, want)
	}
	updated := f.UpdatedPullRequests[len(f.UpdatedPullRequests)-1]
	title, body = updated.Title, updated.Body

	listOpen()
	pushesAfterRewrite := bareBranchPushCount(t, origin)
	var rerun bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &rerun); err != nil {
		t.Fatalf("doPR() rerun error = %v", err)
	}
	if got := bareBranchPushCount(t, origin); got != pushesAfterRewrite {
		t.Fatalf("release branch pushes = %d, want %d (settled after the rewrite)", got, pushesAfterRewrite)
	}
	if !strings.Contains(rerun.String(), "release pull request up to date") {
		t.Fatalf("stdout = %q, want it to report up to date", rerun.String())
	}
}
