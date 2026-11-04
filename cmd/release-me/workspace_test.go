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

func TestCargoWorkspaceReleasesEveryDependentAndMovesTheWorkspaceRoot(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, `release-pull-request: shared
workspaces: [cargo]
components:
  core:
    path: crates/core
    files:
      - {path: crates/core/Cargo.toml, updater: toml, key: package.version}
  cli:
    path: crates/cli
    files:
      - {path: crates/cli/Cargo.toml, updater: toml, key: package.version}
  server:
    path: crates/server
    files:
      - {path: crates/server/Cargo.toml, updater: toml, key: package.version}
`)
	writeFiles(t, dir, "chore: scaffold the cargo workspace", map[string]string{
		"Cargo.toml": "[workspace]\nmembers = [\"crates/*\"]\nresolver = \"2\"\n\n[workspace.dependencies]\n" +
			"acme-core = { path = \"crates/core\", version = \"1.2.0\" }\nserde = \"1.0\"\n",
		"crates/core/Cargo.toml": "[package]\nname = \"acme-core\"\nversion = \"1.2.0\"\nedition = \"2021\"\n",
		"crates/cli/Cargo.toml": "[package]\nname = \"acme-cli\"\nversion = \"0.4.0\"\nedition = \"2021\"\n\n" +
			"[dependencies]\nacme-core = { workspace = true }\n",
		"crates/server/Cargo.toml": "[package]\nname = \"acme-server\"\nversion = \"2.1.0\"\nedition = \"2021\"\n\n" +
			"[dependencies]\nacme-core = { path = \"../core\", version = \"1.2.0\" }\nserde = { workspace = true }\n",
	})
	for _, tag := range []string{"core/v1.2.0", "cli/v0.4.0", "server/v2.1.0"} {
		runGit(t, dir, "tag", tag)
	}
	writeFiles(t, dir, "fix(core): stop the parser dropping the last token", map[string]string{
		"crates/core/src/lib.rs": "pub fn parse() {}\n",
	})
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Title != "chore: release core v1.2.1, cli v0.4.1, server v2.1.1" {
		t.Fatalf("CreatedPullRequests = %+v, want core with both dependents", f.CreatedPullRequests)
	}
	for path, want := range map[string]string{
		"Cargo.toml":               `acme-core = { path = "crates/core", version = "1.2.1" }`,
		"crates/server/Cargo.toml": `acme-core = { path = "../core", version = "1.2.1" }`,
		"crates/cli/Cargo.toml":    "version = \"0.4.1\"\n",
		"crates/cli/CHANGELOG.md":  "- **deps:** update acme-core to 1.2.1",
	} {
		if got := showBareFile(t, origin, "release-me/main", path); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", path, got, want)
		}
	}
	if got := showBareFile(t, origin, "release-me/main", "crates/cli/Cargo.toml"); !strings.Contains(got, "acme-core = { workspace = true }") {
		t.Errorf("crates/cli/Cargo.toml = %q, want the workspace dependency left as written", got)
	}
}

func TestNodeWorkspaceWithoutAlwaysLinkLocalLeavesARangeTheLocalVersionMissesAlone(t *testing.T) {
	dir, origin, cfg := newNodeWorkspaceRepo(t, "release-pull-request: shared\nworkspaces: [node]\nalways-link-local: false\n")
	// web pins core from the registry at a range the local 1.2.0 misses,
	// and reaches core only through ui, which it links by workspace:*.
	writeFiles(t, dir, "chore(web): pin core from the registry", map[string]string{
		"packages/web/package.json": "{\n  \"name\": \"@acme/web\",\n  \"version\": \"0.4.0\",\n  \"dependencies\": {\n" +
			"    \"@acme/core\": \"~1.1.0\",\n    \"@acme/ui\": \"workspace:*\"\n  }\n}\n",
	})

	f := newFake()
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Title != "chore: release core v1.3.0, ui v0.8.1, web v0.4.1" {
		t.Fatalf("CreatedPullRequests = %+v, want web released through ui", f.CreatedPullRequests)
	}
	web := showBareFile(t, origin, "release-me/main", "packages/web/package.json")
	if !strings.Contains(web, `"@acme/core": "~1.1.0"`) {
		t.Errorf("packages/web/package.json = %q, want the registry range left alone", web)
	}
	changelog := showBareFile(t, origin, "release-me/main", "packages/web/CHANGELOG.md")
	if !strings.Contains(changelog, "update @acme/ui to 0.8.1") || strings.Contains(changelog, "update @acme/core") {
		t.Errorf("packages/web/CHANGELOG.md = %q, want the ui update only", changelog)
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

func TestCargoWorkspaceReleasesDependentsThroughInheritedAliases(t *testing.T) {
	for _, decl := range []string{"[dependencies]\nengine = { workspace = true }\n", "[dependencies.engine]\nworkspace = true\n", "[dependencies]\nengine.workspace = true\n"} {
		t.Run(decl, func(t *testing.T) {
			dir, remote := newPRRepo(t)
			writeConfig(t, dir, "release-pull-request: shared\nworkspaces: [cargo]\ncomponents:\n  core:\n    path: crates/core\n    files:\n      - {path: crates/core/Cargo.toml, updater: toml, key: package.version}\n  cli:\n    path: crates/cli\n    files:\n      - {path: crates/cli/Cargo.toml, updater: toml, key: package.version}\n")
			child := "[package]\nname = \"cli\"\nversion = \"1.0.0\"\nedition = \"2021\"\n" + decl
			writeFiles(t, dir, "chore: scaffold Cargo workspace", map[string]string{
				"Cargo.toml":             "[workspace]\nmembers = [\"crates/*\"]\nresolver = \"2\"\n[workspace.dependencies]\nengine = { package = \"core\", path = \"crates/core\", version = \"1.0.0\" }\n",
				"crates/core/Cargo.toml": "[package]\nname = \"core\"\nversion = \"1.0.0\"\nedition = \"2021\"\n",
				"crates/cli/Cargo.toml":  child,
			})
			runGit(t, dir, "tag", "core/v1.0.0")
			runGit(t, dir, "tag", "cli/v1.0.0")
			writeFiles(t, dir, "feat: add the core API", map[string]string{"crates/core/src/lib.rs": "pub fn export() {}\n"})
			f := newFake()
			var out bytes.Buffer
			if err := doPR(t.Context(), dir, loadConfig(t, dir), "", f, discardLogger(), false, &out); err != nil {
				t.Fatal(err)
			}
			if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Title != "chore: release core v1.1.0, cli v1.0.1" {
				t.Fatalf("PRs=%+v", f.CreatedPullRequests)
			}
			root := showBareFile(t, remote, mainBranch, "Cargo.toml")
			if !strings.Contains(root, `engine = { package = "core", path = "crates/core", version = "1.1.0" }`) {
				t.Fatalf("workspace root = %s", root)
			}
			want := strings.Replace(child, `version = "1.0.0"`, `version = "1.0.1"`, 1)
			if got := showBareFile(t, remote, mainBranch, "crates/cli/Cargo.toml"); got != want {
				t.Fatalf("child = %s; want %s", got, want)
			}
		})
	}
}
