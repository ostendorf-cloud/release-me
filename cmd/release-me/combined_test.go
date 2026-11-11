// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// Every test in this file drives doPR and then doRelease over one
// repository with many keys configured at once, the combinations a real
// adopter sets rather than the single key a feature's own test exercises.
// They are the end-to-end guard against two features that each work alone
// breaking each other.

// mergeReleasePullRequest configures f so doRelease finds the release pull
// request doPR opened on branch merged, pending-labeled and carrying the
// title and body doPR wrote, the state a run right after the merge sees.
// labels are the labels the merged pull request carries.
func mergeReleasePullRequest(f *forgetest.Fake, branch string, created forgetest.CreatePullRequestCall, labels ...string) {
	f.ListResponses = append(f.ListResponses, forgetest.ListResponse{
		Head: branch, State: "closed", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, Labels: labels, Merged: true},
		},
	})
	f.MergeCommits = map[int]string{1: "c0ffee42"}
	f.PRLabels = map[int][]string{1: labels}
}

// createdTagNames returns the tag of every CreateTag call f recorded, in
// order.
func createdTagNames(f *forgetest.Fake) []string {
	tags := make([]string, 0, len(f.CreatedTags))
	for _, tag := range f.CreatedTags {
		tags = append(tags, tag.Tag)
	}
	return tags
}

// TestSharedNodeWorkspaceReleaseCarriesEveryConfiguredFeature drives a
// three-component pnpm workspace through pr and release with the keys an
// enterprise monorepo sets together: a shared release pull request under
// always-bump-minor, the details layout, a header and a footer around the
// body, commit links and issue references pointed at a changelog-host,
// an extra label, drafted GitHub releases, moving major tags and release
// names without the "v".
func TestSharedNodeWorkspaceReleaseCarriesEveryConfiguredFeature(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, `versioning-strategy: always-bump-minor
changelog-commit-links: true
changelog-issue-references: true
changelog-host: https://github.example.com
release-pull-request: shared
release-pull-request-layout: details
release-pull-request-header: |
  Prepared by the release pipeline. Hold the merge until staging is green.
release-pull-request-footer: |
  The merge checklist lives in docs/releasing.md.
major-tag: true
include-v-in-release-name: false
github-release-draft: true
labels:
  extra: [automerge]
workspaces: [node]
components:
  core:
    path: packages/core
    files:
      - {path: packages/core/package.json, updater: json, key: version}
  ui:
    path: packages/ui
    files:
      - {path: packages/ui/package.json, updater: json, key: version}
  docs:
    path: docs
    files:
      - {path: docs/package.json, updater: json, key: version}
`)
	writeFiles(t, dir, "chore: scaffold the workspace", map[string]string{
		"pnpm-workspace.yaml":        "packages:\n  - packages/*\n  - docs\n",
		"packages/core/package.json": "{\n  \"name\": \"@acme/core\",\n  \"version\": \"1.2.0\"\n}\n",
		"packages/ui/package.json": "{\n  \"name\": \"@acme/ui\",\n  \"version\": \"0.8.0\",\n  \"dependencies\": {\n" +
			"    \"@acme/core\": \"^1.2.0\"\n  }\n}\n",
		"docs/package.json": "{\n  \"name\": \"@acme/docs\",\n  \"version\": \"2.0.0\"\n}\n",
	})
	for _, tag := range []string{"core/v1.2.0", "ui/v0.8.0", "docs/v2.0.0"} {
		runGit(t, dir, "tag", tag)
	}
	writeFiles(t, dir, "feat(core): format a price in the account's currency\n\nCloses #412", map[string]string{
		"packages/core/currency.ts": "export const currency = 1\n",
	})

	cfg := loadConfig(t, dir)
	f := newPRFake(origin)
	f.RepoLabels = append(f.RepoLabels, "automerge")
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", withChangelogHost(cfg, f), discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want one shared pull request", f.CreatedPullRequests)
	}
	created := f.CreatedPullRequests[0]
	if want := "chore: release core v1.3.0, ui v0.8.1"; created.Title != want {
		t.Errorf("title = %q, want %q: core at its minor bump, ui as its workspace dependent, docs left out", created.Title, want)
	}
	if created.Draft {
		t.Error("the pull request is a draft, want it open: release-pull-request-draft is left out")
	}
	if want := []string{"release: pending", "automerge"}; !reflect.DeepEqual(created.Labels, want) {
		t.Errorf("labels = %v, want %v: the pending label and the extra one", created.Labels, want)
	}

	body := created.Body
	if header := "Prepared by the release pipeline. Hold the merge until staging is green."; !strings.HasPrefix(body, header+"\n") {
		t.Errorf("body = %q, want it to open with the configured header", body)
	}
	if footer := "The merge checklist lives in docs/releasing.md."; !strings.HasSuffix(strings.TrimRight(body, "\n"), footer) {
		t.Errorf("body = %q, want it to close with the configured footer", body)
	}
	for _, want := range []string{
		"<summary>How this release works</summary>",
		"<details><summary>core v1.3.0</summary>",
		"<details><summary>ui v0.8.1</summary>",
		"- **deps:** update @acme/core to 1.3.0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %q, want it to contain %q", body, want)
		}
	}
	if strings.Contains(body, "<details><summary>docs v") {
		t.Errorf("body = %q, want no docs block: nothing changed in it and nothing depends on core through it", body)
	}
	// changelog-host must move every link the changelog renders, the
	// compare link and the pull request's as well as the commit and issue
	// links changelog-commit-links and changelog-issue-references add.
	if !strings.Contains(body, "https://github.example.com/owner/repo/commit/") {
		t.Errorf("body = %q, want the commit link on the configured changelog host", body)
	}
	if !strings.Contains(body, "https://github.example.com/owner/repo/issues/412") {
		t.Errorf("body = %q, want the issue reference on the configured changelog host", body)
	}
	if strings.Contains(body, "https://github.com/") {
		t.Errorf("body = %q, want no changelog link left on the forge's own host", body)
	}

	for path, wants := range map[string][]string{
		"packages/core/package.json": {`"version": "1.3.0"`},
		"packages/ui/package.json":   {`"version": "0.8.1"`, `"@acme/core": "^1.3.0"`},
		"packages/core/CHANGELOG.md": {
			"https://github.example.com/owner/repo/compare/core/v1.2.0...core/v1.3.0",
			"format a price in the account's currency",
			"https://github.example.com/owner/repo/issues/412",
		},
		"packages/ui/CHANGELOG.md": {"- **deps:** update @acme/core to 1.3.0"},
	} {
		got := showBareFile(t, origin, mainBranch, path)
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s = %q, want it to contain %q", path, got, want)
			}
		}
	}
	if got := showBareFile(t, origin, mainBranch, "docs/package.json"); !strings.Contains(got, `"version": "2.0.0"`) {
		t.Errorf("docs/package.json = %q, want docs left at its version", got)
	}

	mergeReleasePullRequest(f, mainBranch, created, "release: pending", "automerge")
	if err := doRelease(t.Context(), dir, cfg, "", withChangelogHost(cfg, f), discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	wantTags := []string{"core/v1.3.0", "core/v1", "ui/v0.8.1", "ui/v0"}
	if got := createdTagNames(f); !reflect.DeepEqual(got, wantTags) {
		t.Errorf("CreatedTags = %v, want %v: a version tag and a major tag per released component", got, wantTags)
	}
	if len(f.CreatedReleases) != 2 {
		t.Fatalf("CreatedReleases = %+v, want one per released component", f.CreatedReleases)
	}
	for i, want := range []struct{ tag, name string }{
		{tag: "core/v1.3.0", name: "core 1.3.0"},
		{tag: "ui/v0.8.1", name: "ui 0.8.1"},
	} {
		got := f.CreatedReleases[i]
		if got.Tag != want.tag || got.Name != want.name {
			t.Errorf("release %d = tag %q name %q, want tag %q name %q: include-v-in-release-name drops the v from the name, never the tag",
				i, got.Tag, got.Name, want.tag, want.name)
		}
		if !got.Draft {
			t.Errorf("release %s is published, want a draft: github-release-draft is on", got.Tag)
		}
		if got.Prerelease {
			t.Errorf("release %s is a pre-release, want a stable one", got.Tag)
		}
	}
	if coreBody := f.CreatedReleases[0].Body; !strings.Contains(coreBody, "format a price in the account's currency") ||
		strings.Contains(coreBody, "update @acme/core") {
		t.Errorf("core release body = %q, want core's own changelog section read back out of the details layout", coreBody)
	}
	if uiBody := f.CreatedReleases[1].Body; !strings.Contains(uiBody, "- **deps:** update @acme/core to 1.3.0") {
		t.Errorf("ui release body = %q, want ui's own changelog section", uiBody)
	}
}

// TestLockedFamilyReleaseCarriesEveryConfiguredFeature drives a locked
// two-component family through pr and release with the keys a coordinated
// monorepo release sets together: a drafted release pull request without
// the guide, a custom changelog date format, sentence-cased entries with
// their authors, a signed-off release commit, the details layout, moving
// major tags and a release branch that survives the release.
func TestLockedFamilyReleaseCarriesEveryConfiguredFeature(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, `versioning: locked
changelog-sentence-case: true
changelog-authors: true
changelog-date-format: "%d %B %Y"
release-pull-request-layout: details
release-pull-request-guide: false
release-pull-request-draft: true
signoff: Release Bot <bot@example.com>
major-tag: true
delete-release-branch: false
components:
  core:
    path: packages/core
  ui:
    path: packages/ui
`)
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{
		"packages/core/core.ts": "export const core = 1\n",
		"packages/ui/ui.ts":     "export const ui = 1\n",
	})
	for _, tag := range []string{"core/v1.2.0", "ui/v1.2.0"} {
		runGit(t, dir, "tag", tag)
	}
	writeFiles(t, dir, "feat(core): format a price in the account's currency", map[string]string{
		"packages/core/currency.ts": "export const currency = 1\n",
	})
	writeFiles(t, dir, "fix(ui): clip a label that overflows its row", map[string]string{
		"packages/ui/label.ts": "export const label = 1\n",
	})

	cfg := loadConfig(t, dir)
	f := newPRFake(origin)
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want one pull request for the locked family", f.CreatedPullRequests)
	}
	created := f.CreatedPullRequests[0]
	if want := "chore: release v1.3.0 (core, ui)"; created.Title != want {
		t.Errorf("title = %q, want %q: the family's highest bump applied to every component", created.Title, want)
	}
	if !created.Draft {
		t.Error("the pull request is open, want a draft: release-pull-request-draft is on")
	}

	body := created.Body
	if strings.Contains(body, "How this release works") {
		t.Errorf("body = %q, want no guide: release-pull-request-guide is off", body)
	}
	for _, want := range []string{
		"## v1.3.0 (11 ",
		"<details><summary>core v1.3.0</summary>",
		"<details><summary>ui v1.3.0</summary>",
		"- **core:** Format a price in the account's currency (release-me tests)",
		"- **ui:** Clip a label that overflows its row (release-me tests)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %q, want it to contain %q", body, want)
		}
	}
	if !strings.Contains(body, "<!-- release-me-date: ") {
		t.Errorf("body = %q, want the canonical date recorded: the heading renders a custom format a rerun cannot read back", body)
	}

	if got, want := gitOut(t, origin, "log", "-1", "--format=%B", mainBranch), "Signed-off-by: Release Bot <bot@example.com>"; !strings.Contains(got, want) {
		t.Errorf("release commit message = %q, want the %q trailer", got, want)
	}
	coreChangelog := showBareFile(t, origin, mainBranch, "packages/core/CHANGELOG.md")
	if !strings.Contains(coreChangelog, "### Features") || !strings.Contains(coreChangelog, "Format a price") {
		t.Errorf("packages/core/CHANGELOG.md = %q, want its own sentence-cased section", coreChangelog)
	}
	if strings.Contains(coreChangelog, "(2026-") {
		t.Errorf("packages/core/CHANGELOG.md = %q, want the heading date in the configured format, not the default one", coreChangelog)
	}

	mergeReleasePullRequest(f, mainBranch, created, "release: pending")
	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	wantTags := []string{"core/v1.3.0", "core/v1", "ui/v1.3.0", "ui/v1"}
	if got := createdTagNames(f); !reflect.DeepEqual(got, wantTags) {
		t.Errorf("CreatedTags = %v, want %v: every component of the locked family at the family version, each with its major tag", got, wantTags)
	}
	if len(f.CreatedReleases) != 2 {
		t.Fatalf("CreatedReleases = %+v, want one per component of the locked family", f.CreatedReleases)
	}
	for i, want := range []struct{ tag, name, entry string }{
		{tag: "core/v1.3.0", name: "core v1.3.0", entry: "Format a price in the account's currency"},
		{tag: "ui/v1.3.0", name: "ui v1.3.0", entry: "Clip a label that overflows its row"},
	} {
		got := f.CreatedReleases[i]
		if got.Tag != want.tag || got.Name != want.name {
			t.Errorf("release %d = tag %q name %q, want tag %q name %q", i, got.Tag, got.Name, want.tag, want.name)
		}
		if got.Draft {
			t.Errorf("release %s is a draft, want it published: github-release-draft is left out", got.Tag)
		}
		if !strings.Contains(got.Body, want.entry) {
			t.Errorf("release %s body = %q, want it to carry %q and nothing of the other component", got.Tag, got.Body, want.entry)
		}
	}
	if !bareRefExists(t, origin, "refs/heads/"+mainBranch) {
		t.Error("the release branch is gone, want it kept: delete-release-branch is off")
	}
}

// TestLinkedFamilyPreReleaseCarriesEveryConfiguredFeature drives a linked
// two-component family through pr and release as a release candidate, with
// the keys a repository preparing a pre-release sets together: a custom
// shared title pattern, custom changelog sections, a hidden type, the
// compact layout, a custom pre-release label and major-tag, which a
// pre-release must never move.
func TestLinkedFamilyPreReleaseCarriesEveryConfiguredFeature(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, `versioning: linked
bump-minor-pre-major: true
changelog-sections:
  - {type: feat, section: What's new}
  - {type: fix, section: Fixes}
  - {type: perf, hidden: true}
release-pull-request: shared
release-pull-request-layout: compact
release-pull-request-shared-title: "release: ${releases}"
major-tag: true
labels:
  prerelease:
    alpha: false
    beta: false
    rc: "release: candidate"
components:
  core:
    path: packages/core
  ui:
    path: packages/ui
`)
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{
		"packages/core/core.ts": "export const core = 1\n",
		"packages/ui/ui.ts":     "export const ui = 1\n",
	})
	for _, tag := range []string{"core/v1.2.0", "ui/v1.1.0"} {
		runGit(t, dir, "tag", tag)
	}
	writeFiles(t, dir, "feat(core): format a price in the account's currency", map[string]string{
		"packages/core/currency.ts": "export const currency = 1\n",
	})
	writeFiles(t, dir, "perf(core): cache the formatter per locale", map[string]string{
		"packages/core/cache.ts": "export const cache = 1\n",
	})

	cfg := loadConfig(t, dir)
	f := newPRFake(origin)
	f.RepoLabels = []string{"release: pending", "release: tagged", "release: candidate"}
	// The release candidate label a reviewer put on the open release pull
	// request, the way a pre-release is selected.
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
			{Number: 1, Title: "release: core v1.3.0", Labels: []string{"release: pending", "release: candidate"}},
		}},
	}
	f.PRLabels = map[int][]string{1: {"release: pending", "release: candidate"}}
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want the open release pull request updated", f.UpdatedPullRequests)
	}
	updated := f.UpdatedPullRequests[0]
	if want := "release: core v1.3.0-rc.1"; updated.Title != want {
		t.Errorf("title = %q, want %q: the configured shared pattern with the pre-release label applied", updated.Title, want)
	}
	for _, want := range []string{"#### core", "### What's new", "format a price in the account's currency"} {
		if !strings.Contains(updated.Body, want) {
			t.Errorf("body = %q, want it to contain %q", updated.Body, want)
		}
	}
	for _, unwanted := range []string{"### Features", "cache the formatter per locale", "#### ui"} {
		if strings.Contains(updated.Body, unwanted) {
			t.Errorf("body = %q, want it to leave out %q: the custom sections rename feat and hide perf, and ui has nothing to release", updated.Body, unwanted)
		}
	}

	created := forgetest.CreatePullRequestCall{Title: updated.Title, Body: updated.Body}
	mergeReleasePullRequest(f, mainBranch, created, "release: pending", "release: candidate")
	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	if got, want := createdTagNames(f), []string{"core/v1.3.0-rc.1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("CreatedTags = %v, want %v: major-tag never follows a pre-release", got, want)
	}
	if len(f.MovedTags) != 0 {
		t.Errorf("MovedTags = %+v, want none: major-tag never follows a pre-release", f.MovedTags)
	}
	if len(f.CreatedReleases) != 1 {
		t.Fatalf("CreatedReleases = %+v, want core's alone", f.CreatedReleases)
	}
	release := f.CreatedReleases[0]
	if release.Tag != "core/v1.3.0-rc.1" || release.Name != "core v1.3.0-rc.1" || !release.Prerelease {
		t.Errorf("release = tag %q name %q prerelease %v, want the release candidate marked as one",
			release.Tag, release.Name, release.Prerelease)
	}
	if !strings.Contains(release.Body, "### What's new") {
		t.Errorf("release body = %q, want core's own section read back out of the compact layout", release.Body)
	}
	if len(f.SetLabelsCalls) != 1 {
		t.Fatalf("SetLabelsCalls = %+v, want the labels swapped once", f.SetLabelsCalls)
	}
	if got, want := f.SetLabelsCalls[0].Labels, []string{"release: tagged"}; !reflect.DeepEqual(got, want) {
		t.Errorf("labels after the release = %v, want %v: the pending and the pre-release label both swapped out", got, want)
	}
	if cfg.Versioning != config.VersioningLinked {
		t.Fatalf("Versioning = %q, want the linked family the test configures", cfg.Versioning)
	}
}
