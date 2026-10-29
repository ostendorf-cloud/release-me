// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

// linkedFamilyConfig is release-me.yaml for a linked family of three
// components a, b and c, the shape the issue's example table walks: a
// family on 0.8.0 with a fix landing in one or two components per step.
const linkedFamilyConfig = `versioning: linked
components:
  a:
    path: a
  b:
    path: b
  c:
    path: c
`

// TestLinkedFamilyFollowsTheIssueTable walks release-me.yaml's linked
// family through the exact steps the issue's example table lists: the
// next version is bump(highest version in the family, highest bump type
// among the changed components), and every changed component in a run
// gets that version.
func TestLinkedFamilyFollowsTheIssueTable(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, linkedFamilyConfig)
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{
		"a/README.md": "a\n", "b/README.md": "b\n", "c/README.md": "c\n",
	})
	runGit(t, dir, "tag", "a/v0.8.0")
	runGit(t, dir, "tag", "b/v0.8.0")
	runGit(t, dir, "tag", "c/v0.8.0")

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := newFake()
	logger := discardLogger()

	// step touches every named component with its own fix commit, computes
	// the family's release for this run, tags every released component at
	// the version it reports, the same effect a merged and tagged release
	// pull request would have, and returns the version each released
	// component landed on.
	stepNum := 0
	step := func(touched ...string) map[string]string {
		stepNum++
		for _, name := range touched {
			writeFiles(t, dir, "fix: patch "+name, map[string]string{
				name + "/CHANGES.txt": fmt.Sprintf("%s-step-%d\n", name, stepNum),
			})
		}
		members, err := gatherFamily(t.Context(), dir, cfg, "owner", "repo", f, logger)
		if err != nil {
			t.Fatalf("gatherFamily() error = %v", err)
		}
		releases, released := familyReleases(cfg, members, logger)
		if !released {
			t.Fatal("familyReleases() released = false, want true")
		}
		got := make(map[string]string, len(releases))
		for name, rel := range releases {
			got[name] = rel.version.Version
			runGit(t, dir, "tag", name+"/v"+rel.version.Version)
		}
		return got
	}

	cases := []struct {
		name    string
		touched []string
		want    map[string]string
	}{
		{name: "fix in a", touched: []string{"a"}, want: map[string]string{"a": "0.8.1"}},
		{name: "fix in c", touched: []string{"c"}, want: map[string]string{"c": "0.8.2"}},
		{name: "fix in a", touched: []string{"a"}, want: map[string]string{"a": "0.8.3"}},
		{name: "fix in b and c", touched: []string{"b", "c"}, want: map[string]string{"b": "0.8.4", "c": "0.8.4"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := step(tc.touched...)
			if len(got) != len(tc.want) {
				t.Fatalf("released %+v, want %+v", got, tc.want)
			}
			for name, want := range tc.want {
				if got[name] != want {
					t.Fatalf("released %+v, want %+v", got, tc.want)
				}
			}
		})
	}
}

// TestLockedFamilyReleasesAnUnchangedComponentAnyway exercises locked's
// defining difference from linked: every component is bumped and released
// together, changed or not, through the one shared release pull request.
func TestLockedFamilyReleasesAnUnchangedComponentAnyway(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	// Only "a" ever gets an entry; "b" never changes.
	writeFiles(t, dir, "feat: add a's feature", map[string]string{"a/feature.go": "package a\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	f := newPRFake(origin)
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want exactly one shared locked pull request", f.CreatedPullRequests)
	}
	created := f.CreatedPullRequests[0]
	if created.Head != "release-me/main" {
		t.Fatalf("head = %q, want the shared locked branch", created.Head)
	}
	if created.Title != "chore: release v0.1.0 (a, b)" {
		t.Fatalf("title = %q, want it to name the version and both components", created.Title)
	}

	bChangelog := showBareFile(t, origin, "release-me/main", "b/CHANGELOG.md")
	if !strings.Contains(bChangelog, "Version bump without user facing changes") {
		t.Fatalf("b's changelog = %q, want the no-user-facing-changes line even though b itself did not change", bChangelog)
	}

	// The shared release pull request merges.
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, Merged: true},
		}},
	}
	f.MergeCommits = map[int]string{1: "deadbeef"}
	f.PRLabels = map[int][]string{1: {"release: pending"}}

	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	wantTags := map[string]bool{"a/v0.1.0": false, "b/v0.1.0": false}
	for _, tag := range f.CreatedTags {
		if _, ok := wantTags[tag.Tag]; ok {
			wantTags[tag.Tag] = true
		}
	}
	for tag, found := range wantTags {
		if !found {
			t.Fatalf("CreatedTags = %+v, want %q among them: locked releases every component, changed or not", f.CreatedTags, tag)
		}
	}
}

// TestLockedFamilyAppliesAVersionOverride confirms a release-me-version
// override on the one shared locked release pull request forces the
// family's version for every component, skipping the computed bump, and
// survives the next update the same way the notes block does.
func TestLockedFamilyAppliesAVersionOverride(t *testing.T) {
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

	withOverride := releasepr.SetVersionOverride(created.Body, "9.0.0")
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/main", State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: withOverride, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	writeFiles(t, dir, "fix: patch a", map[string]string{"a/CHANGES.txt": "a-fix\n"})

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	updated := f.UpdatedPullRequests[0]
	if !strings.Contains(updated.Title, "v9.0.0") {
		t.Fatalf("updated title = %q, want v9.0.0", updated.Title)
	}
	if !strings.Contains(updated.Body, "```release-me-version\n9.0.0\n```") {
		t.Fatalf("updated body = %q, want the version override block kept", updated.Body)
	}

	aChangelog := showBareFile(t, origin, "release-me/main", "a/CHANGELOG.md")
	bChangelog := showBareFile(t, origin, "release-me/main", "b/CHANGELOG.md")
	if !strings.Contains(aChangelog, "## v9.0.0") || !strings.Contains(bChangelog, "## v9.0.0") {
		t.Fatalf("changelogs = %q / %q, want both at the overridden version", aChangelog, bChangelog)
	}
}

// TestLockedFamilyIgnoresAVersionOverrideNotGreaterThanTheLastTag guards
// against a locked family's release-as override producing a backwards
// release: a family already at 5.0.0 commenting "release-as 1.2.3" must
// not ship 1.2.3 over 5.0.0, the same regression
// TestDoPRIgnoresAVersionOverrideNotGreaterThanTheLastTag guards for a
// single, independent component.
func TestLockedFamilyIgnoresAVersionOverrideNotGreaterThanTheLastTag(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	runGit(t, dir, "tag", "a/v5.0.0")
	runGit(t, dir, "tag", "b/v5.0.0")
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
	if !strings.Contains(created.Title, "v5.1.0") {
		t.Fatalf("created title = %q, want v5.1.0", created.Title)
	}

	withOverride := releasepr.SetVersionOverride(created.Body, "1.2.3")
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/main", State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: withOverride, HeadSHA: bareBranchSHA(t, origin)},
		}},
	}

	writeFiles(t, dir, "fix: patch a", map[string]string{"a/CHANGES.txt": "a-fix\n"})

	var second bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &second); err != nil {
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

// TestLockedFamilyDoesNotDuplicateTheReleasePullRequestOnTheMergeThatReleasesIt
// reproduces #45 for a locked family: the shared release pull request's
// merge pushes to the default branch, and that push's run invocation
// calls doPR before doRelease tags the release. doPR must recognize the
// closed, merged, still pending-labeled pull request and create nothing,
// leaving the one shared pull request for doRelease to tag.
func TestLockedFamilyDoesNotDuplicateTheReleasePullRequestOnTheMergeThatReleasesIt(t *testing.T) {
	dir, _ := newPRRepo(t)
	writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	writeFiles(t, dir, "feat: add a's feature", map[string]string{"a/feature.go": "package a\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want one", f.CreatedPullRequests)
	}
	created := f.CreatedPullRequests[0]

	// The shared release pull request merges. It still carries the pending
	// label: release has not tagged it yet.
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, Merged: true},
		}},
	}
	f.MergeCommits = map[int]string{1: "deadbeef"}
	f.PRLabels = map[int][]string{1: {"release: pending"}}

	// The same push's run invocation calls doPR again, before doRelease.
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want still only the one shared pull request, no duplicate", f.CreatedPullRequests)
	}

	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}
	wantTags := map[string]bool{"a/v0.1.0": false, "b/v0.1.0": false}
	for _, tag := range f.CreatedTags {
		if _, ok := wantTags[tag.Tag]; ok {
			wantTags[tag.Tag] = true
		}
	}
	for tag, found := range wantTags {
		if !found {
			t.Fatalf("CreatedTags = %+v, want %q among them", f.CreatedTags, tag)
		}
	}
}

// TestDoPRIndependentLeavesAnUnchangedComponentUntouched confirms
// VersioningIndependent, the default, still releases only the component
// that actually changed, each on its own: adding the "versioning" key does
// not change today's per-component behavior.
func TestDoPRIndependentLeavesAnUnchangedComponentUntouched(t *testing.T) {
	dir, _ := newPRRepo(t)
	writeConfig(t, dir, "components:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	writeFiles(t, dir, "feat: add a's feature", map[string]string{"a/feature.go": "package a\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Versioning != config.VersioningIndependent {
		t.Fatalf("Versioning = %q, want the independent default", cfg.Versioning)
	}

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Head != "release-me/a" {
		t.Fatalf("CreatedPullRequests = %+v, want exactly one, for component a", f.CreatedPullRequests)
	}
	if !strings.Contains(stdout.String(), "b: nothing to release") {
		t.Fatalf("stdout = %q, want b reported as nothing to release", stdout.String())
	}
}

// TestComponentFlagIsRejectedInLockedMode confirms --component is a usage
// error under locked versioning, for every command that loops over
// components: locked always releases the whole family, so naming one
// component is not a meaningful restriction.
func TestComponentFlagIsRejectedInLockedMode(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")

	for _, cmd := range []string{"next", "changelog", "pr", "release", "run"} {
		t.Run(cmd, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{cmd, "--component", "a"}, &stdout, &stderr)
			if code != 2 {
				t.Fatalf("%s --component a: exit code = %d, want 2, stderr: %s", cmd, code, stderr.String())
			}
			want := "locked versioning releases the family, --component is not allowed"
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("%s --component a: stderr = %q, want it to contain %q", cmd, stderr.String(), want)
			}
		})
	}
}
