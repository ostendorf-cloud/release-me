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

// modulePom is the pom.xml of a module of the com.acme build at version,
// under the aggregator pom acme-parent 3.0.0, with dependencies, the
// <dependency> elements of its dependencies list.
func modulePom(artifact, version, dependencies string) string {
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<project xmlns=\"http://maven.apache.org/POM/4.0.0\">\n" +
		"  <modelVersion>4.0.0</modelVersion>\n  <parent>\n    <groupId>com.acme</groupId>\n" +
		"    <artifactId>acme-parent</artifactId>\n    <version>3.0.0</version>\n  </parent>\n" +
		"  <artifactId>" + artifact + "</artifactId>\n  <version>" + version + "</version>\n" +
		"  <dependencies>\n" + dependencies + "  </dependencies>\n</project>\n"
}

func TestMavenWorkspaceReleasesEveryDependentModule(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, `release-pull-request: shared
workspaces: [maven]
components:
  core:
    path: modules/core
  api:
    path: modules/api
  cli:
    path: modules/cli
`)
	coreDependency := "    <dependency>\n      <groupId>com.acme</groupId>\n      <artifactId>acme-core</artifactId>\n" +
		"      <version>1.2.0</version>\n    </dependency>\n"
	writeFiles(t, dir, "chore: scaffold the maven build", map[string]string{
		"pom.xml": "<project>\n  <groupId>com.acme</groupId>\n  <artifactId>acme-parent</artifactId>\n" +
			"  <version>3.0.0</version>\n  <packaging>pom</packaging>\n  <dependencyManagement>\n    <dependencies>\n" +
			"  " + strings.ReplaceAll(coreDependency, "\n    ", "\n      ") + "    </dependencies>\n  </dependencyManagement>\n</project>\n",
		"modules/core/pom.xml": modulePom("acme-core", "1.2.0", ""),
		"modules/api/pom.xml":  modulePom("acme-api", "0.4.0", coreDependency),
		"modules/cli/pom.xml": modulePom("acme-cli", "0.9.0", "    <dependency>\n      <groupId>info.picocli</groupId>\n"+
			"      <artifactId>picocli</artifactId>\n      <version>4.7.6</version>\n    </dependency>\n"),
	})
	for _, tag := range []string{"core/v1.2.0", "api/v0.4.0", "cli/v0.9.0"} {
		runGit(t, dir, "tag", tag)
	}
	writeFiles(t, dir, "feat(core): add the pricing rules", map[string]string{
		"modules/core/src/main/java/com/acme/core/Pricing.java": "package com.acme.core;\n",
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

	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Title != "chore: release core v1.3.0, api v0.4.1" {
		t.Fatalf("CreatedPullRequests = %+v, want core with api, cli depending on neither", f.CreatedPullRequests)
	}
	for path, want := range map[string]string{
		"pom.xml":                  "<artifactId>acme-core</artifactId>\n        <version>1.3.0</version>",
		"modules/api/pom.xml":      "<artifactId>acme-core</artifactId>\n      <version>1.3.0</version>",
		"modules/api/CHANGELOG.md": "- **deps:** update com.acme:acme-core to 1.3.0",
	} {
		if got := showBareFile(t, origin, "release-me/main", path); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", path, got, want)
		}
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

func TestMavenWorkspaceReleasesDependenciesDeclaredOnlyInProfiles(t *testing.T) {
	for _, tt := range []struct{ name, before, after string }{
		{"dependency", "<dependencies>", "</dependencies>"},
		{"managed dependency", "<dependencyManagement><dependencies>", "</dependencies></dependencyManagement>"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			writeConfig(t, dir, `release-pull-request: shared
workspaces: [maven]
components:
  core:
    path: modules/core
  api:
    path: modules/api
`)
			dependency := `<dependency><groupId>com.acme</groupId><artifactId>acme-core</artifactId><version>1.2.0</version></dependency>`
			profile := `<profiles><profile><id>production</id><activation><activeByDefault>true</activeByDefault></activation>` + tt.before + dependency + tt.after + `</profile></profiles>`
			api := strings.Replace(modulePom("acme-api", "0.4.0", ""), "</project>", profile+"</project>", 1)
			writeFiles(t, dir, "chore: scaffold modules", map[string]string{
				"modules/core/pom.xml": modulePom("acme-core", "1.2.0", ""), "modules/api/pom.xml": api,
			})
			for _, tag := range []string{"core/v1.2.0", "api/v0.4.0"} {
				runGit(t, dir, "tag", tag)
			}
			writeFiles(t, dir, "feat(core): add pricing", map[string]string{"modules/core/Pricing.java": "class Pricing {}\n"})
			cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			f := newFake()
			var out bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
				t.Fatal(err)
			}
			if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Title != "chore: release core v1.3.0, api v0.4.1" {
				t.Fatalf("pull requests = %+v", f.CreatedPullRequests)
			}
			want := strings.Replace(api, "<version>1.2.0</version>", "<version>1.3.0</version>", 1)
			if got := showBareFile(t, origin, "release-me/main", "modules/api/pom.xml"); got != want {
				t.Fatalf("profile dependency = %s, want %s", got, want)
			}
		})
	}
}
