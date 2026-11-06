// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package migrate_test

import (
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/migrate"
)

const packageJSON = `{
  "name": "@acme/web",
  "version": "2.0.0",
  "private": true
}
`

func TestFromReleasePleaseCarriesOverTheFilesAReleaseTypeBumps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		settings     string
		repo         map[string]string
		wantFiles    []config.File
		wantWarnings []string
	}{
		{
			name:     "ruby bumps the gem's version.rb",
			settings: `"release-type": "ruby", "component": "release-notes"`,
			repo: map[string]string{
				"web/lib/release/notes/version.rb": "module ReleaseNotes\n  VERSION = \"1.4.0\"\nend\n",
			},
			wantFiles: []config.File{
				{Path: "web/lib/release/notes/version.rb", Updater: "ruby"},
			},
		},
		{
			name:     "ruby bumps the version-file it names",
			settings: `"release-type": "ruby", "component": "release-notes", "version-file": "lib/notes/version.rb"`,
			repo: map[string]string{
				"web/lib/notes/version.rb": "module Notes\n  VERSION = \"1.4.0\"\nend\n",
			},
			wantFiles: []config.File{{Path: "web/lib/notes/version.rb", Updater: "ruby"}},
		},
		{
			name:     "python bumps setup.py, pyproject.toml and the package's __init__.py",
			settings: `"release-type": "python"`,
			repo: map[string]string{
				"web/setup.py":                      "from setuptools import setup\n\nsetup(\n    name=\"release-notes\",\n    version=\"1.4.0\",\n)\n",
				"web/pyproject.toml":                "[project]\nname = \"release-notes\"\nversion = \"1.4.0\"\n",
				"web/src/release_notes/__init__.py": "__version__ = \"1.4.0\"\n",
				"web/release_notes/version.py":      "VERSION_INFO = (1, 4, 0)\n",
			},
			wantFiles: []config.File{
				{Path: "web/setup.py", Updater: "python"},
				{Path: "web/pyproject.toml", Updater: "toml", Key: "project.version"},
				{Path: "web/src/release_notes/__init__.py", Updater: "python"},
			},
		},
		{
			name:     "python bumps a poetry project's version and setup.cfg",
			settings: `"release-type": "python"`,
			repo: map[string]string{
				"web/setup.cfg":         "[metadata]\nname = release-notes\nversion = 1.4.0\n",
				"web/pyproject.toml":    "[tool.poetry]\nname = \"notes\"\nversion = \"1.4.0\"\n",
				"web/notes/__init__.py": "__version__ = '1.4.0'\n",
			},
			wantFiles: []config.File{
				{Path: "web/setup.cfg", Updater: "python"},
				{Path: "web/pyproject.toml", Updater: "toml", Key: "tool.poetry.version"},
				{Path: "web/notes/__init__.py", Updater: "python"},
			},
		},
		{
			name:     "python carries over a pyproject version without a project name",
			settings: `"release-type": "python"`,
			repo: map[string]string{
				"web/pyproject.toml": "[project]\nversion = \"1.4.0\"\n",
			},
			wantFiles: []config.File{
				{Path: "web/pyproject.toml", Updater: "toml", Key: "project.version"},
			},
		},
		{
			name:     "python carries over a poetry version without a project name",
			settings: `"release-type": "python"`,
			repo: map[string]string{
				"web/pyproject.toml": "[tool.poetry]\nversion = \"1.4.0\"\n",
			},
			wantFiles: []config.File{
				{Path: "web/pyproject.toml", Updater: "toml", Key: "tool.poetry.version"},
			},
		},
		{
			name:     "python warns when it finds no version file",
			settings: `"release-type": "python"`,
			wantWarnings: []string{
				"package web: release-type python: no setup.cfg, setup.py, pyproject.toml, __init__.py or version.py " +
					"with a version found, add the version file under files",
			},
		},
		{
			name:      "elixir bumps mix.exs",
			settings:  `"release-type": "elixir"`,
			repo:      map[string]string{"web/mix.exs": "  @version \"1.4.0\"\n"},
			wantFiles: []config.File{{Path: "web/mix.exs", Updater: "elixir"}},
		},
		{
			name:     "ocaml bumps the opam file named after the package and dune-project",
			settings: `"release-type": "ocaml", "component": "notes"`,
			repo: map[string]string{
				"web/notes.opam":   "opam-version: \"2.0\"\nversion: \"1.4.0\"\n",
				"web/dune-project": "(lang dune 3.0)\n(version 1.4.0)\n",
			},
			wantFiles: []config.File{
				{Path: "web/notes.opam", Updater: "ocaml"},
				{Path: "web/dune-project", Updater: "ocaml"},
			},
			wantWarnings: []string{
				"package web: release-type ocaml: release-please bumped every .opam and esy JSON file of the package, " +
					"only those named after the package are carried over, add any other under files",
			},
		},
		{
			name:      "r bumps DESCRIPTION",
			settings:  `"release-type": "r"`,
			repo:      map[string]string{"web/DESCRIPTION": "Package: notes\nVersion: 1.4.0\n"},
			wantFiles: []config.File{{Path: "web/DESCRIPTION", Updater: "r"}},
		},
		{
			name:      "bazel bumps MODULE.bazel",
			settings:  `"release-type": "bazel"`,
			repo:      map[string]string{"web/MODULE.bazel": "module(name = \"rules_notes\", version = \"1.4.0\")\n"},
			wantFiles: []config.File{{Path: "web/MODULE.bazel", Updater: "bazel"}},
		},
		{
			name:     "php bumps composer.json's version and warns about a VERSION file",
			settings: `"release-type": "php"`,
			repo: map[string]string{
				"web/composer.json": "{\n  \"name\": \"acme/notes\",\n  \"version\": \"1.4.0\"\n}\n",
				"web/VERSION":       "1.4.0\n",
			},
			wantFiles: []config.File{
				{Path: "web/composer.json", Updater: "json", Key: "version"},
			},
			wantWarnings: []string{
				"package web: VERSION: release-please bumped it, release-me does not",
			},
		},
		{
			name:     "php leaves out a composer.json without a version",
			settings: `"release-type": "php"`,
			repo:     map[string]string{"web/composer.json": "{\n  \"name\": \"acme/notes\"\n}\n"},
		},
		{
			name:      "dart bumps pubspec.yaml",
			settings:  `"release-type": "dart"`,
			repo:      map[string]string{"web/pubspec.yaml": "name: notes\nversion: 1.4.0+3\n"},
			wantFiles: []config.File{{Path: "web/pubspec.yaml", Updater: "dart"}},
		},
		{
			name:     "expo bumps package.json and app.json, and warns about versionCode",
			settings: `"release-type": "expo"`,
			repo: map[string]string{
				"web/package.json": packageJSON,
				"web/app.json": "{\n  \"expo\": {\n    \"version\": \"2.0.0\",\n    \"ios\": {\n      \"buildNumber\": \"2.0.0\"\n" +
					"    },\n    \"android\": {\n      \"versionCode\": 510020000\n    }\n  }\n}\n",
			},
			wantFiles: []config.File{
				{Path: "web/package.json", Updater: "json", Key: "version"},
				{Path: "web/app.json", Updater: "json", Key: "expo.version"},
				{Path: "web/app.json", Updater: "json", Key: "expo.ios.buildNumber"},
			},
			wantWarnings: []string{
				"package web: release-type expo: app.json's expo.android.versionCode is computed from the Expo SDK " +
					"and the version, release-me does not bump it",
			},
		},
		{
			name:     "expo ignores a versionCode outside its Android settings",
			settings: `"release-type": "expo"`,
			repo: map[string]string{
				"web/package.json": packageJSON,
				"web/app.json":     "{\n  \"versionCode\": 7,\n  \"expo\": {\n    \"version\": \"2.0.0\",\n    \"extra\": {\n      \"versionCode\": 8\n    }\n  }\n}\n",
			},
			wantFiles: []config.File{
				{Path: "web/package.json", Updater: "json", Key: "version"},
				{Path: "web/app.json", Updater: "json", Key: "expo.version"},
			},
		},
		{
			name:      "sfdx bumps sfdx-project.json",
			settings:  `"release-type": "sfdx"`,
			repo:      map[string]string{"web/sfdx-project.json": "{}\n"},
			wantFiles: []config.File{{Path: "web/sfdx-project.json", Updater: "sfdx"}},
		},
		{
			name:     "terraform-module warns that its files are searched for",
			settings: `"release-type": "terraform-module"`,
			wantWarnings: []string{
				"package web: release-type terraform-module: release-please searched the package for the files it bumps, " +
					"release-me lists them: mark each version line with \"release-me: version\" and add it under files",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configJSON := `{"packages": {"web": {` + tc.settings + `}}}`
			got, err := migrate.FromReleasePlease([]byte(configJSON), nil, files(tc.repo))
			if err != nil {
				t.Fatalf("FromReleasePlease() error = %v", err)
			}
			resolved := load(t, got.YAML).Resolve()
			if !reflect.DeepEqual(resolved[0].Files, tc.wantFiles) {
				t.Fatalf("Files = %+v, want %+v", resolved[0].Files, tc.wantFiles)
			}
			if !reflect.DeepEqual(got.Warnings, tc.wantWarnings) {
				t.Fatalf("Warnings = %q, want %q", got.Warnings, tc.wantWarnings)
			}
		})
	}
}

func TestFromReleasePleaseKeepsAnRPackagesChangelogInNEWS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		settings string
		want     string
	}{
		{name: "defaults to NEWS.md, as release-please's r type does", settings: `"release-type": "r"`, want: "web/NEWS.md"},
		{name: "keeps a changelog-path it is given", settings: `"release-type": "r", "changelog-path": "CHANGES.md"`, want: "web/CHANGES.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configJSON := `{"packages": {"web": {` + tc.settings + `}}}`
			read := files(map[string]string{"web/DESCRIPTION": "Package: notes\nVersion: 1.0.0\n"})
			got, err := migrate.FromReleasePlease([]byte(configJSON), nil, read)
			if err != nil {
				t.Fatalf("FromReleasePlease() error = %v", err)
			}
			if path := load(t, got.YAML).Resolve()[0].ChangelogPath; path != tc.want {
				t.Fatalf("ChangelogPath = %q, want %q", path, tc.want)
			}
		})
	}
}
