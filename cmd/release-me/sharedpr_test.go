// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

// sharedIndependentConfig is release-me.yaml for three independently
// versioned components sharing one release pull request: api and web each
// bump their own package.json, and both bump their own key of the
// deploy/versions.yaml they share.
const sharedIndependentConfig = `release-pull-request: shared
components:
  api:
    path: api
    files:
      - path: api/package.json
        updater: json
        key: version
      - path: deploy/versions.yaml
        updater: yaml
        key: api
  web:
    path: web
    files:
      - path: web/package.json
        updater: json
        key: version
      - path: deploy/versions.yaml
        updater: yaml
        key: web
  docs:
    path: docs
`

// newSharedRepo returns a repository with sharedIndependentConfig, api on
// 1.2.0, web on 0.3.0 and docs on 2.0.0, then a feature in api and a fix
// in web: api and web have something to release, docs does not.
func newSharedRepo(t *testing.T) (dir, origin string, cfg config.Config) {
	t.Helper()
	dir, origin = newPRRepo(t)
	writeConfig(t, dir, sharedIndependentConfig)
	writeFiles(t, dir, "chore: scaffold the services", map[string]string{
		"api/package.json":     "{\n  \"name\": \"api\",\n  \"version\": \"1.2.0\"\n}\n",
		"web/package.json":     "{\n  \"name\": \"web\",\n  \"version\": \"0.3.0\"\n}\n",
		"docs/README.md":       "# Docs\n",
		"deploy/versions.yaml": "api: 1.2.0\nweb: 0.3.0\n",
	})
	runGit(t, dir, "tag", "api/v1.2.0")
	runGit(t, dir, "tag", "web/v0.3.0")
	runGit(t, dir, "tag", "docs/v2.0.0")
	writeFiles(t, dir, "feat: export orders as CSV", map[string]string{"api/export.go": "package api\n"})
	writeFiles(t, dir, "fix: keep the session on reload", map[string]string{"web/session.ts": "export {}\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return dir, origin, cfg
}

func TestSharedReleasePullRequestCarriesEveryChangedComponentAtItsOwnVersion(t *testing.T) {
	dir, origin, cfg := newSharedRepo(t)

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want exactly one shared pull request", f.CreatedPullRequests)
	}
	created := f.CreatedPullRequests[0]
	if created.Head != "release-me/main" {
		t.Fatalf("head = %q, want the shared branch release-me/main", created.Head)
	}
	if created.Title != "chore: release api v1.3.0, web v0.3.1" {
		t.Fatalf("title = %q, want every changed component at its own version", created.Title)
	}
	if !reflect.DeepEqual(created.Labels, []string{"release: pending"}) {
		t.Fatalf("labels = %v, want the pending label", created.Labels)
	}
	for _, want := range []string{
		"Release api v1.3.0, web v0.3.1\n",
		"tags `api/v1.3.0, web/v0.3.1`",
		"## api\n\n### [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (",
		"- export orders as CSV",
		"## web\n\n### [v0.3.1](https://github.com/owner/repo/compare/web/v0.3.0...web/v0.3.1) (",
		"- keep the session on reload",
		"`release-as <component> X.Y.Z`",
	} {
		if !strings.Contains(created.Body, want) {
			t.Errorf("body = %q, want it to contain %q", created.Body, want)
		}
	}
	if strings.Contains(created.Body, "## docs") {
		t.Errorf("body = %q, want no section for docs, which has nothing to release", created.Body)
	}
	if !strings.Contains(stdout.String(), "docs: nothing to release") {
		t.Errorf("stdout = %q, want docs reported as nothing to release", stdout.String())
	}

	files := map[string]string{
		"api/CHANGELOG.md":     "## [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0)",
		"web/CHANGELOG.md":     "## [v0.3.1](https://github.com/owner/repo/compare/web/v0.3.0...web/v0.3.1)",
		"api/package.json":     `"version": "1.3.0"`,
		"web/package.json":     `"version": "0.3.1"`,
		"deploy/versions.yaml": "api: 1.3.0\nweb: 0.3.1\n",
	}
	for path, want := range files {
		if got := showBareFile(t, origin, "release-me/main", path); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", path, got, want)
		}
	}
	if bareRefExists(t, origin, "refs/heads/release-me/api") || bareRefExists(t, origin, "refs/heads/release-me/web") {
		t.Errorf("a per-component branch was pushed, want only the shared one")
	}

	// The shared pull request merges; the same push's run calls doPR before
	// doRelease, which must not open a second one.
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, Merged: true},
		}},
	}
	f.MergeCommits = map[int]string{1: "c0ffee42"}
	f.PRLabels = map[int][]string{1: {"release: pending"}}
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() after the merge error = %v", err)
	}
	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want still only the one shared pull request", f.CreatedPullRequests)
	}

	stdout.Reset()
	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}
	var tags []string
	for _, tag := range f.CreatedTags {
		tags = append(tags, tag.Tag)
	}
	if !reflect.DeepEqual(tags, []string{"api/v1.3.0", "web/v0.3.1"}) {
		t.Fatalf("CreatedTags = %v, want one tag per released component and none for docs", tags)
	}
	if len(f.CreatedReleases) != 2 || f.CreatedReleases[0].Name != "api v1.3.0" ||
		!strings.Contains(f.CreatedReleases[0].Body, "export orders as CSV") ||
		strings.Contains(f.CreatedReleases[0].Body, "keep the session on reload") {
		t.Fatalf("CreatedReleases = %+v, want one per component, each with its own slice of the changelog", f.CreatedReleases)
	}
	if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, []string{"release: tagged"}) {
		t.Fatalf("SetLabelsCalls = %+v, want one swap to release: tagged for the whole pull request", f.SetLabelsCalls)
	}
	for _, want := range []string{"released-tag: api/v1.3.0", "released-tag: web/v0.3.1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout.String(), want)
		}
	}
}

func TestSharedReleasePullRequestInLinkedVersioningSharesTheFamilyVersion(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "versioning: linked\nrelease-pull-request: shared\n"+strings.TrimPrefix(linkedFamilyConfig, "versioning: linked\n"))
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n", "c/README.md": "c\n"})
	runGit(t, dir, "tag", "a/v0.8.0")
	runGit(t, dir, "tag", "b/v0.8.2")
	runGit(t, dir, "tag", "c/v0.8.0")
	writeFiles(t, dir, "fix: patch a", map[string]string{"a/CHANGES.txt": "a\n"})
	writeFiles(t, dir, "fix: patch c", map[string]string{"c/CHANGES.txt": "c\n"})

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
		t.Fatalf("CreatedPullRequests = %+v, want exactly one shared pull request", f.CreatedPullRequests)
	}
	created := f.CreatedPullRequests[0]
	if created.Title != "chore: release a v0.8.3, c v0.8.3" {
		t.Fatalf("title = %q, want the changed components at the family's next version", created.Title)
	}
	if !strings.Contains(created.Body, "followed by `release-as X.Y.Z`") {
		t.Fatalf("body = %q, want the guide to ask for one version for the whole family", created.Body)
	}
	if !strings.Contains(stdout.String(), "b: nothing to release") {
		t.Fatalf("stdout = %q, want b reported as nothing to release", stdout.String())
	}
	if got := showBareFile(t, origin, "release-me/main", "c/CHANGELOG.md"); !strings.Contains(got, "c/v0.8.0...c/v0.8.3") {
		t.Fatalf("c/CHANGELOG.md = %q, want a heading linked from c's own last tag", got)
	}
}

func TestSharedReleasePullRequestAppliesAVersionOverridePerComponent(t *testing.T) {
	tests := []struct {
		name      string
		override  func(body string) string
		wantTitle string
	}{
		{
			name:      "a component line forces only that component",
			override:  func(body string) string { return releasepr.SetComponentVersionOverride(body, "web", "1.0.0") },
			wantTitle: "chore: release api v1.3.0, web v1.0.0",
		},
		{
			name:      "a line not greater than the component's last tag is ignored",
			override:  func(body string) string { return releasepr.SetComponentVersionOverride(body, "api", "1.2.0") },
			wantTitle: "chore: release api v1.3.0, web v0.3.1",
		},
		{
			name:      "an unscoped version is ambiguous with several components and ignored",
			override:  func(body string) string { return releasepr.SetVersionOverride(body, "5.0.0") },
			wantTitle: "chore: release api v1.3.0, web v0.3.1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin, cfg := newSharedRepo(t)
			f := newFake()
			var stdout bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doPR() first run error = %v", err)
			}
			created := f.CreatedPullRequests[0]
			f.ListResponses = []forgetest.ListResponse{
				{Head: "release-me/main", State: "open", PullRequests: []forge.PullRequest{{
					Number: 1, Title: created.Title, Body: tc.override(created.Body),
					Labels: []string{"release: pending"}, HeadSHA: bareBranchSHA(t, origin),
				}}},
			}

			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doPR() second run error = %v", err)
			}
			if len(f.UpdatedPullRequests) != 1 {
				t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
			}
			if got := f.UpdatedPullRequests[0].Title; got != tc.wantTitle {
				t.Fatalf("title = %q, want %q", got, tc.wantTitle)
			}
			if _, ok := releasepr.ExtractVersionOverride(f.UpdatedPullRequests[0].Body); !ok {
				t.Fatalf("body = %q, want the override block kept", f.UpdatedPullRequests[0].Body)
			}
		})
	}
}

func TestSharedReleasePullRequestTurnsEveryComponentIntoAPreRelease(t *testing.T) {
	dir, origin, cfg := newSharedRepo(t)
	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() first run error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	runGit(t, dir, "tag", "web/v0.3.1-rc.1")
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/main", State: "open", PullRequests: []forge.PullRequest{{
			Number: 1, Title: created.Title, Body: created.Body,
			Labels: []string{"release: pending", "release: rc"}, HeadSHA: bareBranchSHA(t, origin),
		}}},
	}

	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}
	if len(f.UpdatedPullRequests) != 1 {
		t.Fatalf("UpdatedPullRequests = %+v, want one", f.UpdatedPullRequests)
	}
	// Each component counts its own pre-releases: web already has rc.1.
	if got, want := f.UpdatedPullRequests[0].Title, "chore: release api v1.3.0-rc.1, web v0.3.1-rc.2"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
}

func TestSharedReleasePullRequestReportsAPerComponentOneItSupersedes(t *testing.T) {
	dir, _, cfg := newSharedRepo(t)
	f := newFake()
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/api", State: "open", PullRequests: []forge.PullRequest{
			{Number: 4, Title: "chore(api): release v1.3.0", Labels: []string{"release: pending"}},
		}},
	}
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	want := "api: release pull request #4 on release-me/api is superseded by the shared one on release-me/main, close it"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), want)
	}
	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Head != "release-me/main" {
		t.Fatalf("CreatedPullRequests = %+v, want the shared one created anyway", f.CreatedPullRequests)
	}
}

func TestSharedReleasePullRequestIsWrittenWhenLookingForASupersededOneFails(t *testing.T) {
	dir, _, cfg := newSharedRepo(t)
	f := newFake()
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/api", State: "open", Err: errors.New("GET /repos/owner/repo/pulls: 502 Bad Gateway")},
	}
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v, want the informational lookup to never stop pr", err)
	}
	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Head != "release-me/main" {
		t.Fatalf("CreatedPullRequests = %+v, want the shared one created anyway", f.CreatedPullRequests)
	}
}

func TestSharedReleaseSweepsAPerComponentBranchOnlyWhileItExistsOnTheRemote(t *testing.T) {
	tests := []struct {
		name       string
		pushBranch bool
		wantTags   []string
	}{
		{name: "tags a pull request merged before the switch while its branch exists", pushBranch: true, wantTags: []string{"api/v1.3.0"}},
		{name: "skips the sweep once the branch is deleted", pushBranch: false, wantTags: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, _, cfg := newSharedRepo(t)
			if tc.pushBranch {
				runGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/release-me/api")
			}
			f := newFake()
			f.ListResponses = []forgetest.ListResponse{
				{Head: "release-me/api", State: "closed", PullRequests: []forge.PullRequest{{
					Number: 4, Title: "chore(api): release v1.3.0", Labels: []string{"release: pending"}, Merged: true,
					Body: "Release v1.3.0\n\n<details>\n<summary>Changelog</summary>\n\n## v1.3.0 (2026-03-14)\n\n" +
						"### Features\n\n- export orders as CSV\n\n</details>\n",
				}}},
			}
			f.MergeCommits = map[int]string{4: "facade01"}
			f.PRLabels = map[int][]string{4: {"release: pending"}}

			var stdout bytes.Buffer
			if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doRelease() error = %v", err)
			}
			var got []string
			for _, c := range f.CreatedTags {
				got = append(got, c.Tag)
			}
			if !reflect.DeepEqual(got, tc.wantTags) {
				t.Fatalf("CreatedTags = %q, want %q", got, tc.wantTags)
			}
		})
	}
}

func TestDoCommentReleaseAsTargetsAComponentOfASharedReleasePullRequest(t *testing.T) {
	tests := []struct {
		name        string
		comment     string
		wantUpdated bool
		wantTitle   string
		wantReply   string
	}{
		{
			name: "a named component is forced and recomputed immediately", comment: "@release-me release-as web 1.0.0",
			wantUpdated: true, wantTitle: "chore: release api v1.3.0, web v1.0.0",
			wantReply: "forced the next version of web to 1.0.0 and updated",
		},
		{
			name: "no component while several are released is refused", comment: "@release-me release-as 1.0.0",
			wantReply: "this pull request releases api, web, each at its own version: name the component",
		},
		{
			name: "a component the pull request does not release is refused", comment: "@release-me release-as docs 3.0.0",
			wantReply: `component "docs" is not part of this pull request, which releases api, web`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin, cfg := newSharedRepo(t)
			f := newFake()
			var first bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
				t.Fatalf("doPR() first run error = %v", err)
			}
			created := f.CreatedPullRequests[0]
			f.Responses = map[int]forgetest.Response{1: {PullRequest: forge.PullRequest{
				Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"},
			}}}
			f.ListResponses = []forgetest.ListResponse{
				{Head: "release-me/main", State: "open", PullRequests: []forge.PullRequest{{
					Number: 1, Title: created.Title, Body: releasepr.SetComponentVersionOverride(created.Body, "web", "1.0.0"),
					Labels: []string{"release: pending"}, HeadSHA: bareBranchSHA(t, origin),
				}}},
			}

			var stdout bytes.Buffer
			flags := commentFlags{prNumber: 1, association: "OWNER", body: tc.comment}
			if err := doComment(t.Context(), dir, cfg, f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
				t.Fatalf("doComment() error = %v", err)
			}

			if len(f.CreatedComments) != 1 || !strings.Contains(f.CreatedComments[0].Body, tc.wantReply) {
				t.Fatalf("CreatedComments = %+v, want one reply containing %q", f.CreatedComments, tc.wantReply)
			}
			if !tc.wantUpdated {
				if len(f.UpdatedPullRequests) != 0 {
					t.Fatalf("UpdatedPullRequests = %+v, want none for a refused command", f.UpdatedPullRequests)
				}
				return
			}
			if len(f.UpdatedPullRequests) != 2 {
				t.Fatalf("UpdatedPullRequests = %+v, want the override write and the recompute", f.UpdatedPullRequests)
			}
			if raw, _ := releasepr.ExtractVersionOverride(f.UpdatedPullRequests[0].Body); raw != "web 1.0.0" {
				t.Fatalf("override block = %q, want %q", raw, "web 1.0.0")
			}
			if got := f.UpdatedPullRequests[1].Title; got != tc.wantTitle {
				t.Fatalf("recomputed title = %q, want %q", got, tc.wantTitle)
			}
		})
	}
}

func TestDoCommentReleaseAsRefusesAComponentWhereThereIsOnlyOneVersion(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name: "one release pull request per component", config: "components:\n  api:\n    path: api\n",
			want: "this pull request releases one component, so release-as takes no component",
		},
		{
			name:   "a shared release pull request in linked versioning",
			config: "versioning: linked\nrelease-pull-request: shared\ncomponents:\n  api:\n    path: api\n",
			want:   "every component in this pull request shares one version, so release-as takes no component",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeConfig(t, dir, tc.config)
			cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
			if err != nil {
				t.Fatalf("config.Load() error = %v", err)
			}
			f := newFake()
			f.Responses = map[int]forgetest.Response{7: {PullRequest: releasePR()}}

			var stdout bytes.Buffer
			flags := commentFlags{prNumber: 7, association: "OWNER", body: "@release-me release-as api 2.0.0"}
			if err := doComment(t.Context(), dir, cfg, f, discardLogger(), "owner", "repo", flags, &stdout); err != nil {
				t.Fatalf("doComment() error = %v", err)
			}
			if len(f.UpdatedPullRequests) != 0 {
				t.Fatalf("UpdatedPullRequests = %+v, want none", f.UpdatedPullRequests)
			}
			if len(f.CreatedComments) != 1 || !strings.Contains(f.CreatedComments[0].Body, tc.want) {
				t.Fatalf("CreatedComments = %+v, want one reply containing %q", f.CreatedComments, tc.want)
			}
		})
	}
}

func TestComponentFlagIsRejectedForTheWritingCommandsWithASharedReleasePullRequest(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "release-pull-request: shared\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")

	for _, cmd := range []string{"pr", "release", "run"} {
		t.Run(cmd, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{cmd, "--component", "a"}, &stdout, &stderr)
			if code != 2 {
				t.Fatalf("%s --component a: exit code = %d, want 2, stderr: %s", cmd, code, stderr.String())
			}
			want := "release-pull-request shared carries every component in one release pull request, --component is not allowed"
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("%s --component a: stderr = %q, want it to contain %q", cmd, stderr.String(), want)
			}
		})
	}
}

func TestSharedReleasePullRequestHonoursTheReleaseAsAndReleaseComponentFooters(t *testing.T) {
	dir, _, cfg := newSharedRepo(t)
	commitEntry(t, dir, "fix: correct the session cookie\n\nRelease-Component: web\nRelease-As: 1.0.0")
	commitEntry(t, dir, "docs: describe the CSV export\n\nRelease-Component: docs\nRelease-As: 2.1.0")

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want exactly one shared pull request", f.CreatedPullRequests)
	}
	if got, want := f.CreatedPullRequests[0].Title, "chore: release api v1.3.0, web v1.0.0, docs v2.1.0"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
}

func TestSharedReleasePullRequestRefusesATitlePastTheForgeLimitBeforeWritingAnything(t *testing.T) {
	dir, origin := newPRRepo(t)
	var yaml strings.Builder
	yaml.WriteString("release-pull-request: shared\ncomponents:\n")
	files := map[string]string{}
	for i := 1; i <= 14; i++ {
		name := fmt.Sprintf("service-component-%02d", i)
		fmt.Fprintf(&yaml, "  %s:\n    path: services/%s\n", name, name)
		files["services/"+name+"/main.go"] = "package main\n"
	}
	writeConfig(t, dir, yaml.String())
	writeFiles(t, dir, "feat: scaffold every service", files)
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := newFake()

	var stdout bytes.Buffer
	err = doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout)
	if err == nil {
		t.Fatal("doPR() error = nil, want the title refused")
	}
	for _, want := range []string{"title is 419 characters", "limit of 256", "service-component-01, ", "service-component-14", "per-component"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("doPR() error = %q, want it to contain %q", err, want)
		}
	}
	if len(f.CreatedPullRequests) != 0 || bareRefExists(t, origin, "refs/heads/release-me/main") {
		t.Fatalf("CreatedPullRequests = %+v, want nothing written before the refusal", f.CreatedPullRequests)
	}
}

func TestSharedReleaseFailsWithoutNamingThePushURLWhenTheBranchLookupFails(t *testing.T) {
	dir, _, cfg := newSharedRepo(t)
	missing := filepath.Join(t.TempDir(), "x-access-token-s3cr3t")
	runGit(t, dir, "config", "remote.origin.pushurl", missing)

	var stdout bytes.Buffer
	err := doRelease(t.Context(), dir, cfg, "", newFake(), discardLogger(), false, &stdout)
	if err == nil {
		t.Fatal("doRelease() error = nil, want the failed branch lookup reported")
	}
	if !strings.Contains(err.Error(), "list the per-component release branches") || strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("doRelease() error = %q, want the lookup named and the push URL left out", err)
	}
}
