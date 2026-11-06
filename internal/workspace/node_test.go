// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package workspace_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/workspace"
)

// webPackageJSON is the package.json of a web app in a pnpm workspace,
// depending on the workspace's core and ui packages in every way a
// package.json can.
const webPackageJSON = `{
  "name": "@acme/web",
  "version": "0.4.0",
  "private": true,
  "scripts": {
    "build": "nuxt build"
  },
  "dependencies": {
    "@acme/core": "^1.2.0",
    "@acme/ui": "workspace:*",
    "vue": "^3.5.13"
  },
  "devDependencies": {
    "@acme/core": "workspace:~1.2.0",
    "@acme/lint": "file:../lint",
    "vitest": "^3.0.0"
  },
  "peerDependencies": {
    "@acme/core": ">=1.0.0 <2"
  }
}
`

func TestParseReadsAPackageJSONsNameVersionAndDependencies(t *testing.T) {
	t.Parallel()
	got, err := workspace.Parse(workspace.Node, []byte(webPackageJSON))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := workspace.Package{
		Name: "@acme/web", Version: "0.4.0",
		Dependencies: []workspace.Dependency{
			{Name: "@acme/core", Range: "^1.2.0"},
			{Name: "@acme/ui", Range: "workspace:*"},
			{Name: "vue", Range: "^3.5.13"},
			{Name: "@acme/core", Range: "workspace:~1.2.0"},
			{Name: "@acme/lint", Range: "file:../lint"},
			{Name: "vitest", Range: "^3.0.0"},
			{Name: "@acme/core", Range: ">=1.0.0 <2"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %+v, want %+v", got, want)
	}
}

func TestParseRejectsAPackageJSONThatIsNoJSON(t *testing.T) {
	t.Parallel()
	if _, err := workspace.Parse(workspace.Node, []byte("name: web\n")); err == nil {
		t.Fatal("Parse() error = nil, want an error")
	}
}

func TestSetDependencyMovesEveryVersionedDeclarationAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	got, err := workspace.SetDependency(workspace.Node, webPackageJSON, "@acme/core", "1.3.0")
	if err != nil {
		t.Fatalf("SetDependency() error = %v", err)
	}
	want := strings.NewReplacer(
		`"@acme/core": "^1.2.0"`, `"@acme/core": "^1.3.0"`,
		`"@acme/core": "workspace:~1.2.0"`, `"@acme/core": "workspace:~1.3.0"`,
	).Replace(webPackageJSON)
	if got != want {
		t.Fatalf("SetDependency() =\n%s\nwant\n%s", got, want)
	}
}

func TestSetDependencyMovesARangeByItsOperator(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, from, want string
	}{
		{name: "a caret range keeps its caret", from: "^1.2.0", want: "^2.0.0-rc.1"},
		{name: "a tilde range keeps its tilde", from: "~1.2.0", want: "~2.0.0-rc.1"},
		{name: "an exact version stays exact", from: "1.2.0", want: "2.0.0-rc.1"},
		{name: "an explicit equality keeps its operator", from: "=1.2.0", want: "=2.0.0-rc.1"},
		{name: "a lower bound keeps its operator", from: ">=1.2.0", want: ">=2.0.0-rc.1"},
		{name: "an exclusive lower bound stays as written", from: ">1.2.0", want: ">1.2.0"},
		{name: "a v prefix is dropped", from: "v1.2.0", want: "2.0.0-rc.1"},
		{name: "a workspace range keeps its protocol", from: "workspace:^1.2.0", want: "workspace:^2.0.0-rc.1"},
		{name: "a workspace alias stays as written", from: "workspace:^", want: "workspace:^"},
		{name: "a star stays as written", from: "*", want: "*"},
		{name: "a compound range stays as written", from: ">=1.0.0 <2", want: ">=1.0.0 <2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := "{\n  \"name\": \"@acme/web\",\n  \"dependencies\": {\n    \"@acme/core\": \"" + tc.from + "\"\n  }\n}\n"
			got, err := workspace.SetDependency(workspace.Node, content, "@acme/core", "2.0.0-rc.1")
			if err != nil {
				t.Fatalf("SetDependency() error = %v", err)
			}
			if want := strings.Replace(content, tc.from, tc.want, 1); got != want {
				t.Fatalf("SetDependency() = %q, want %q", got, want)
			}
		})
	}
}

func TestSetDependencyLeavesANameOutsideTheDependencyObjectsAlone(t *testing.T) {
	t.Parallel()
	content := "{\n  \"name\": \"@acme/web\",\n  \"pnpm\": {\n    \"overrides\": {\n      \"@acme/core\": \"1.0.0\"\n    }\n  },\n" +
		"  \"dependencies\": {\n    \"@acme/core\": \"^1.2.0\"\n  }\n}\n"
	got, err := workspace.SetDependency(workspace.Node, content, "@acme/core", "1.3.0")
	if err != nil {
		t.Fatalf("SetDependency() error = %v", err)
	}
	if want := strings.Replace(content, `"^1.2.0"`, `"^1.3.0"`, 1); got != want {
		t.Fatalf("SetDependency() = %q, want %q", got, want)
	}
}

func TestSetDependencyRejectsADependencyObjectOnOneLine(t *testing.T) {
	t.Parallel()
	content := `{"name": "@acme/web", "dependencies": {"@acme/core": "^1.2.0"}}`
	if _, err := workspace.SetDependency(workspace.Node, content, "@acme/core", "1.3.0"); err == nil {
		t.Fatal("SetDependency() error = nil, want an error naming the layout it cannot read")
	}
}
