// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package workspace_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/workspace"
)

// cliCargoToml is the Cargo.toml of a CLI crate in a Cargo workspace,
// depending on the workspace's core and macros crates in every way a
// Cargo.toml can.
const cliCargoToml = `[package]
name = "acme-cli"
version = "0.4.0"
edition = "2021"

[dependencies]
acme-core = { path = "../core", version = "1.2.0" } # the engine
acme-macros = { workspace = true }
clap = { version = "4.5", features = ["derive"] }
serde = "1.0"

[dev-dependencies]
acme-core = { path = "../core", version = "=1.2.0", features = ["testing"] }

[target.'cfg(unix)'.dependencies.engine]
package = "acme-core"
path = "../core"
version = "^1.2"

[build-dependencies]
acme-build = { path = "../build" }
`

func TestParseReadsACargoTomlsPackageAndDependencies(t *testing.T) {
	t.Parallel()
	got, err := workspace.Parse(workspace.Cargo, []byte(cliCargoToml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := workspace.Package{
		Name: "acme-cli", Version: "0.4.0",
		Dependencies: []workspace.Dependency{
			{Name: "acme-core", Range: "1.2.0"},
			{Name: "acme-macros", Workspace: true},
			{Name: "clap", Range: "4.5"},
			{Name: "serde", Range: "1.0"},
			{Name: "acme-core", Range: "=1.2.0"},
			{Name: "acme-core", Range: "^1.2"},
			{Name: "acme-build"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %+v, want %+v", got, want)
	}
}

func TestParseReadsAVirtualCargoWorkspace(t *testing.T) {
	t.Parallel()
	content := "[workspace]\nmembers = [\"crates/*\"]\n\n[workspace.dependencies]\nacme-core = { path = \"crates/core\", version = \"1.2.0\" }\n"
	got, err := workspace.Parse(workspace.Cargo, []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := workspace.Package{Dependencies: []workspace.Dependency{{Name: "acme-core", Range: "1.2.0"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %+v, want %+v", got, want)
	}
}

func TestParseRejectsACargoTomlWithoutPackageOrWorkspace(t *testing.T) {
	t.Parallel()
	if _, err := workspace.Parse(workspace.Cargo, []byte("[dependencies]\nserde = \"1.0\"\n")); err == nil {
		t.Fatal("Parse() error = nil, want an error")
	}
}

func TestSetDependencyMovesEveryCargoDeclarationWithAVersion(t *testing.T) {
	t.Parallel()
	got, err := workspace.SetDependency(workspace.Cargo, cliCargoToml, "acme-core", "1.3.0")
	if err != nil {
		t.Fatalf("SetDependency() error = %v", err)
	}
	want := strings.NewReplacer(
		`acme-core = { path = "../core", version = "1.2.0" } # the engine`,
		`acme-core = { path = "../core", version = "1.3.0" } # the engine`,
		`version = "=1.2.0", features`, `version = "=1.3.0", features`,
		`version = "^1.2"`, `version = "^1.3.0"`,
	).Replace(cliCargoToml)
	if got != want {
		t.Fatalf("SetDependency() =\n%s\nwant\n%s", got, want)
	}
}

func TestSetDependencyMovesAPlainCargoRequirementAndKeepsOtherRequirements(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, line, want string
	}{
		{name: "a plain string", line: `acme-core = "1.2.0"`, want: `acme-core = "1.3.0"`},
		{name: "a literal string with a comment", line: `acme-core = '~1.2' # pinned`, want: `acme-core = '~1.3.0' # pinned`},
		{name: "a quoted key", line: `"acme-core" = ">=1.2.0"`, want: `"acme-core" = ">=1.3.0"`},
		{name: "a compound requirement stays", line: `acme-core = ">=1.0, <2"`, want: `acme-core = ">=1.0, <2"`},
		{name: "a star stays", line: `acme-core = "*"`, want: `acme-core = "*"`},
		{name: "another crate stays", line: `acme-core-sys = "1.2.0"`, want: `acme-core-sys = "1.2.0"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := "[package]\nname = \"acme-cli\"\n\n[dependencies]\n" + tc.line + "\n"
			got, err := workspace.SetDependency(workspace.Cargo, content, "acme-core", "1.3.0")
			if err != nil {
				t.Fatalf("SetDependency() error = %v", err)
			}
			if want := strings.Replace(content, tc.line, tc.want, 1); got != want {
				t.Fatalf("SetDependency() = %q, want %q", got, want)
			}
		})
	}
}

func TestSetDependencyLeavesTheCratesOwnVersionAlone(t *testing.T) {
	t.Parallel()
	content := "[package]\nname = \"acme-core\"\nversion = \"1.2.0\"\n\n[dependencies]\nserde = \"1.0\"\n"
	got, err := workspace.SetDependency(workspace.Cargo, content, "acme-core", "1.3.0")
	if err != nil || got != content {
		t.Fatalf("SetDependency() = %q, %v, want the content unchanged", got, err)
	}
}

func TestCargoArrayTablesEndPackageAndDependencyContexts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, content string
		wantDeps      []workspace.Dependency
	}{
		{name: "binary after package", content: "[package]\nname = \"core\"\nversion = \"1.0.0\"\n[[bin]]\nname = \"core-tool\"\npath = \"src/bin/tool.rs\"\n"},
		{name: "example after dependencies", content: "[package]\nname = \"core\"\nversion = \"1.0.0\"\n[dependencies]\nhelper = \"1.0.0\"\n[[example]]\nname = \"core-example\"\npath = \"examples/main.rs\"\n", wantDeps: []workspace.Dependency{{Name: "helper", Range: "1.0.0"}}},
		{name: "binary after a dependency table", content: "[package]\nname = \"core\"\nversion = \"1.0.0\"\n[dependencies.helper]\nversion = \"1.0.0\"\n[[bin]]\nname = \"core-tool\"\npath = \"src/bin/tool.rs\"\n", wantDeps: []workspace.Dependency{{Name: "helper", Range: "1.0.0"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pkg, err := workspace.Parse(workspace.Cargo, []byte(tc.content))
			if err != nil {
				t.Fatal(err)
			}
			if pkg.Name != "core" || pkg.Version != "1.0.0" || !reflect.DeepEqual(pkg.Dependencies, tc.wantDeps) {
				t.Fatalf("package = %+v;want core 1.0.0 deps %+v", pkg, tc.wantDeps)
			}
			members := []workspace.Member{{Component: "core", Manager: workspace.Cargo, Package: pkg}, {Component: "cli", Manager: workspace.Cargo, Package: workspace.Package{Name: "cli", Dependencies: []workspace.Dependency{{Name: "core", Range: "1.0.0"}}}}}
			links := workspace.Links(members, workspace.Options{})
			if len(links) != 1 || links[0].Dependent != "cli" || links[0].Dependency != "core" {
				t.Fatalf("links = %+v", links)
			}
		})
	}
}

func TestCargoInheritedAliasesLinkToTheWorkspacePackage(t *testing.T) {
	t.Parallel()
	for _, decl := range []string{"[dependencies]\nengine = { workspace = true }\n", "[dependencies.engine]\nworkspace = true\n", "[dependencies]\nengine.workspace = true\n"} {
		t.Run(decl, func(t *testing.T) {
			t.Parallel()
			root, err := workspace.Parse(workspace.Cargo, []byte("[workspace]\nmembers = [\"crates/*\"]\n[workspace.dependencies]\nengine = { package = \"core\", path = \"crates/core\", version = \"1.0.0\" }\n"))
			if err != nil {
				t.Fatal(err)
			}
			cli, err := workspace.Parse(workspace.Cargo, []byte("[package]\nname = \"cli\"\nversion = \"1.0.0\"\n"+decl))
			if err != nil {
				t.Fatal(err)
			}
			members := []workspace.Member{{Manager: workspace.Cargo, Package: root}, {Component: "core", Manager: workspace.Cargo, Package: workspace.Package{Name: "core", Version: "1.0.0"}}, {Component: "cli", Manager: workspace.Cargo, Package: cli}}
			dependents := workspace.Dependents(members, []string{"core"}, workspace.Options{})
			if !reflect.DeepEqual(dependents, []string{"cli"}) {
				t.Fatalf("dependents = %v, want cli", dependents)
			}
		})
	}
}
