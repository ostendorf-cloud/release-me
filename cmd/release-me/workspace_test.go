// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// nodeWorkspaceComponents is the "components" block of a pnpm workspace:
// core, ui depending on core, web depending on ui and core, and docs
// depending on nothing local. Each bumps its own package.json.
const nodeWorkspaceComponents = `components:
  core:
    path: packages/core
    files:
      - {path: packages/core/package.json, updater: json, key: version}
  ui:
    path: packages/ui
    files:
      - {path: packages/ui/package.json, updater: json, key: version}
  web:
    path: packages/web
    files:
      - {path: packages/web/package.json, updater: json, key: version}
  docs:
    path: docs
    files:
      - {path: docs/package.json, updater: json, key: version}
`

// newNodeWorkspaceRepo returns a repository with the pnpm workspace of
// nodeWorkspaceComponents under head, core on 1.2.0, ui on 0.8.0, web on
// 0.4.0 and docs on 2.0.0, then a feature in core only.
func newNodeWorkspaceRepo(t *testing.T, head string) (dir, origin string, cfg config.Config) {
	t.Helper()
	dir, origin = newPRRepo(t)
	writeConfig(t, dir, head+nodeWorkspaceComponents)
	writeFiles(t, dir, "chore: scaffold the workspace", map[string]string{
		"pnpm-workspace.yaml":        "packages:\n  - packages/*\n  - docs\n",
		"packages/core/package.json": "{\n  \"name\": \"@acme/core\",\n  \"version\": \"1.2.0\"\n}\n",
		"packages/ui/package.json": "{\n  \"name\": \"@acme/ui\",\n  \"version\": \"0.8.0\",\n  \"dependencies\": {\n" +
			"    \"@acme/core\": \"^1.2.0\",\n    \"vue\": \"^3.5.13\"\n  }\n}\n",
		"packages/web/package.json": "{\n  \"name\": \"@acme/web\",\n  \"version\": \"0.4.0\",\n  \"dependencies\": {\n" +
			"    \"@acme/core\": \"^1.2.0\",\n    \"@acme/ui\": \"workspace:*\"\n  }\n}\n",
		"docs/package.json": "{\n  \"name\": \"@acme/docs\",\n  \"version\": \"2.0.0\",\n  \"devDependencies\": {\n" +
			"    \"vitepress\": \"^1.6.0\"\n  }\n}\n",
	})
	for _, tag := range []string{"core/v1.2.0", "ui/v0.8.0", "web/v0.4.0", "docs/v2.0.0"} {
		runGit(t, dir, "tag", tag)
	}
	writeFiles(t, dir, "feat(core): add the currency formatter", map[string]string{
		"packages/core/currency.ts": "export const currency = 1\n",
	})
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return dir, origin, cfg
}

func TestNodeWorkspaceReleasesEveryDependentWithTheNewDependencyVersion(t *testing.T) {
	dir, origin, cfg := newNodeWorkspaceRepo(t, "release-pull-request: shared\nworkspaces: [node]\n")

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want one shared pull request", f.CreatedPullRequests)
	}
	created := f.CreatedPullRequests[0]
	if created.Title != "chore: release core v1.3.0, ui v0.8.1, web v0.4.1" {
		t.Fatalf("title = %q, want core with its dependents ui and web, and not docs", created.Title)
	}
	files := map[string][]string{
		"packages/core/package.json": {`"version": "1.3.0"`},
		"packages/ui/package.json":   {`"version": "0.8.1"`, `"@acme/core": "^1.3.0"`, `"vue": "^3.5.13"`},
		"packages/web/package.json":  {`"version": "0.4.1"`, `"@acme/core": "^1.3.0"`, `"@acme/ui": "workspace:*"`},
		"packages/ui/CHANGELOG.md":   {"### Bug Fixes\n\n- **deps:** update @acme/core to 1.3.0"},
		"packages/web/CHANGELOG.md":  {"- **deps:** update @acme/ui to 0.8.1", "- **deps:** update @acme/core to 1.3.0"},
	}
	for path, wants := range files {
		got := showBareFile(t, origin, "release-me/main", path)
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s = %q, want it to contain %q", path, got, want)
			}
		}
	}
	if got := showBareFile(t, origin, "release-me/main", "docs/package.json"); !strings.Contains(got, `"version": "2.0.0"`) {
		t.Errorf("docs/package.json = %q, want docs left at 2.0.0", got)
	}

	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{
			{Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, Merged: true},
		}},
	}
	f.MergeCommits = map[int]string{1: "c0ffee42"}
	f.PRLabels = map[int][]string{1: {"release: pending"}}
	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}
	var tags []string
	for _, tag := range f.CreatedTags {
		tags = append(tags, tag.Tag)
	}
	if !reflect.DeepEqual(tags, []string{"core/v1.3.0", "ui/v0.8.1", "web/v0.4.1"}) {
		t.Fatalf("CreatedTags = %v, want core and both dependents tagged", tags)
	}
}

func TestNodeWorkspaceMovesDependenciesInALockedFamily(t *testing.T) {
	dir, origin, cfg := newNodeWorkspaceRepo(t, "versioning: locked\nworkspaces: [node]\n")

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 {
		t.Fatalf("CreatedPullRequests = %+v, want one locked pull request", f.CreatedPullRequests)
	}
	for path, want := range map[string]string{
		"packages/ui/package.json":  `"@acme/core": "^2.1.0"`,
		"packages/web/package.json": `"@acme/core": "^2.1.0"`,
		"packages/web/CHANGELOG.md": "- **deps:** update @acme/ui to 2.1.0",
	} {
		if got := showBareFile(t, origin, "release-me/main", path); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", path, got, want)
		}
	}
}

func TestNodeWorkspaceReleasesADependentAtTheLinkedFamilysVersion(t *testing.T) {
	dir, origin, cfg := newNodeWorkspaceRepo(t, "versioning: linked\nrelease-pull-request: shared\nworkspaces: [node]\n")

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Title != "chore: release core v2.1.0, ui v2.1.0, web v2.1.0" {
		t.Fatalf("CreatedPullRequests = %+v, want core and its dependents at the family's version, docs left out", f.CreatedPullRequests)
	}
	if got := showBareFile(t, origin, "release-me/main", "packages/web/package.json"); !strings.Contains(got, `"@acme/core": "^2.1.0"`) {
		t.Errorf("packages/web/package.json = %q, want the dependency on core moved", got)
	}
}

func TestNodeWorkspaceLeftOutReleasesNoDependent(t *testing.T) {
	dir, origin, cfg := newNodeWorkspaceRepo(t, "release-pull-request: shared\n")

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Title != "chore: release core v1.3.0" {
		t.Fatalf("CreatedPullRequests = %+v, want core alone", f.CreatedPullRequests)
	}
	if got := showBareFile(t, origin, "release-me/main", "packages/ui/package.json"); !strings.Contains(got, `"@acme/core": "^1.2.0"`) {
		t.Errorf("packages/ui/package.json = %q, want its dependency left alone", got)
	}
}
