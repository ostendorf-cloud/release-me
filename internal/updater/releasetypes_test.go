// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package updater_test

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/updater"
)

func TestReleaseTypeUpdatersBumpTheVersionOfTheirFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		updater string
		content string
		from    string
		to      string
	}{
		{
			name:    "python bumps an __init__.py's __version__",
			updater: updater.Python,
			content: "\"\"\"Release notes for Python.\"\"\"\n\n__version__ = \"1.4.0\"\n__all__ = [\"render\"]\n",
			from:    `__version__ = "1.4.0"`,
			to:      `__version__ = "1.5.0-rc.1"`,
		},
		{
			name:    "python bumps setup.py's version keyword, not a version read at run time",
			updater: updater.Python,
			content: "from setuptools import setup\n\nsetup(\n    name=\"release-notes\",\n    python_requires=\">=3.9\",\n    version='1.4.0',\n)\n",
			from:    "    version='1.4.0',",
			to:      "    version='1.5.0-rc.1',",
		},
		{
			name:    "python bumps setup.cfg's bare version",
			updater: updater.Python,
			content: "[metadata]\nname = release-notes\nversion = 1.4.0\n\n[options]\npackages = find:\n",
			from:    "version = 1.4.0",
			to:      "version = 1.5.0-rc.1",
		},
		{
			name:    "ruby bumps version.rb's frozen VERSION constant",
			updater: updater.Ruby,
			content: "# frozen_string_literal: true\n\nmodule ReleaseNotes\n  VERSION = \"1.4.0\".freeze\nend\n",
			from:    `  VERSION = "1.4.0".freeze`,
			to:      `  VERSION = "1.5.0-rc.1".freeze`,
		},
		{
			name:    "elixir bumps the @version attribute project/0 refers to",
			updater: updater.Elixir,
			content: "defmodule ReleaseNotes.MixProject do\n  use Mix.Project\n\n  @version \"1.4.0\"\n\n  def project do\n    [app: :release_notes, version: @version, elixir: \"~> 1.16\"]\n  end\nend\n",
			from:    `  @version "1.4.0"`,
			to:      `  @version "1.5.0-rc.1"`,
		},
		{
			name:    "elixir bumps the version keyword without an @version attribute",
			updater: updater.Elixir,
			content: "defmodule ReleaseNotes.MixProject do\n  use Mix.Project\n\n  def project do\n    [\n      app: :release_notes,\n      version: \"1.4.0\",\n      elixir: \"~> 1.16\"\n    ]\n  end\nend\n",
			from:    `      version: "1.4.0",`,
			to:      `      version: "1.5.0-rc.1",`,
		},
		{
			name:    "elixir leaves version examples in documentation and comments alone",
			updater: updater.Elixir,
			content: "defmodule ReleaseNotes.MixProject do\n  use Mix.Project\n  @moduledoc ~s(Use version: \"1.0.0\" in the project list.)\n  # version: \"1.1.0\"\n\n  def project do\n    [\n      app: :release_notes,\n      version: \"1.4.0\",\n      elixir: \"~> 1.16\"\n    ]\n  end\nend\n",
			from:    `      version: "1.4.0",`,
			to:      `      version: "1.5.0-rc.1",`,
		},
		{
			name:    "ocaml bumps an opam file's version field",
			updater: updater.OCaml,
			content: "opam-version: \"2.0\"\nversion: \"1.4.0\"\nsynopsis: \"Release notes\"\ndepends: [\n  \"dune\" {>= \"3.0\"}\n]\n",
			from:    `version: "1.4.0"`,
			to:      `version: "1.5.0-rc.1"`,
		},
		{
			name:    "ocaml keeps the trailing comment on an opam version field",
			updater: updater.OCaml,
			content: "opam-version: \"2.0\"\nversion: \"1.4.0\" # release version\nsynopsis: \"Release notes\"\n",
			from:    `version: "1.4.0" # release version`,
			to:      `version: "1.5.0-rc.1" # release version`,
		},
		{
			name:    "ocaml bumps a dune-project's version stanza",
			updater: updater.OCaml,
			content: "(lang dune 3.0)\n(name release_notes)\n(version 1.4.0)\n",
			from:    "(version 1.4.0)",
			to:      "(version 1.5.0-rc.1)",
		},
		{
			name:    "r bumps DESCRIPTION's Version field, a development suffix included",
			updater: updater.R,
			content: "Package: releasenotes\nTitle: Release Notes\nVersion: 1.4.0.9000\nDepends: R (>= 4.1.0)\n",
			from:    "Version: 1.4.0.9000",
			to:      "Version: 1.5.0-rc.1",
		},
		{
			name:    "bazel bumps the module() call's version, not a bazel_dep's",
			updater: updater.Bazel,
			content: "module(\n    name = \"rules_release\",\n    # (keep the version in sync)\n    version = \"1.4.0\",\n    compatibility_level = 1,\n)\n\nbazel_dep(name = \"platforms\", version = \"0.0.10\")\n",
			from:    `    version = "1.4.0",`,
			to:      `    version = "1.5.0-rc.1",`,
		},
		{
			name:    "dart counts a numeric build number up",
			updater: updater.Dart,
			content: "name: release_notes\ndescription: Release notes for Flutter.\nversion: 1.4.0+12\n\nenvironment:\n  sdk: ^3.4.0\n",
			from:    "version: 1.4.0+12",
			to:      "version: 1.5.0-rc.1+13",
		},
		{
			name:    "dart bumps a version without a build number",
			updater: updater.Dart,
			content: "name: release_notes\nversion: 1.4.0\n",
			from:    "version: 1.4.0",
			to:      "version: 1.5.0-rc.1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := updater.Updaters[tc.updater].Update(tc.content, "", "1.5.0-rc.1")
			if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			want := replaceLine(tc.content, tc.from, tc.to)
			if want == tc.content {
				t.Fatalf("fixture has no line %q", tc.from)
			}
			if got != want {
				t.Fatalf("Update() =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestReleaseTypeUpdatersRejectAFileWithoutTheirVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		updater string
		content string
	}{
		{"python rejects a version read at run time", updater.Python, "setup(\n    version=get_version(),\n)\n"},
		{"python rejects setup.cfg's attr: version", updater.Python, "[metadata]\nversion = attr: release_notes.__version__\n"},
		{"ruby rejects a file without a VERSION constant", updater.Ruby, "module ReleaseNotes\nend\n"},
		{"elixir rejects a mix.exs without a version", updater.Elixir, "defmodule A.MixProject do\nend\n"},
		{"elixir rejects version examples without a version keyword", updater.Elixir, "@moduledoc ~s(Use version: \"1.0.0\".)\n# version: \"1.1.0\"\n"},
		{"ocaml rejects a file without a version", updater.OCaml, "opam-version: \"2.0\"\n"},
		{"r rejects a DESCRIPTION without a Version", updater.R, "Package: releasenotes\n"},
		{"bazel rejects a MODULE.bazel without a module() call", updater.Bazel, "bazel_dep(name = \"platforms\", version = \"0.0.10\")\n"},
		{"bazel rejects a module() call without a version", updater.Bazel, "module(name = \"rules_release\")\n\nbazel_dep(name = \"platforms\", version = \"0.0.10\")\n"},
		{"dart rejects a pubspec.yaml without a version", updater.Dart, "name: release_notes\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := updater.Updaters[tc.updater].Update(tc.content, "", "2.0.0"); err == nil {
				t.Fatal("Update() error = nil, want an error")
			}
		})
	}
}

const sfdxProject = `{
  "packageDirectories": [
    {
      "path": "force-app",
      "default": true,
      "package": "Release Notes",
      "versionName": "Spring",
      "versionNumber": "1.4.0.NEXT",
      "dependencies": [
        {
          "package": "Notes Core",
          "versionNumber": "2.1.0.LATEST"
        }
      ]
    },
    {
      "path": "unpackaged",
      "default": false,
      "versionNumber": "0.1.0.NEXT"
    }
  ],
  "namespace": "",
  "sourceApiVersion": "61.0"
}
`

func TestUpdateSfdxBumpsTheDefaultPackageDirectoryOnly(t *testing.T) {
	t.Parallel()
	got, err := updater.UpdateSfdx(sfdxProject, "1.5.0")
	if err != nil {
		t.Fatalf("UpdateSfdx() error = %v", err)
	}
	want := replaceLine(sfdxProject, `      "versionNumber": "1.4.0.NEXT",`, `      "versionNumber": "1.5.0.NEXT",`)
	if got != want {
		t.Fatalf("UpdateSfdx() =\n%s\nwant\n%s", got, want)
	}
}

func TestUpdateSfdxRejectsAProjectWithoutADefaultPackageDirectory(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
	}{
		{name: "rejects a project whose directories are none default", content: `{"packageDirectories": [{"path": "force-app", "versionNumber": "1.0.0.NEXT"}]}`},
		{name: "rejects a default directory without a versionNumber", content: `{"packageDirectories": [{"path": "force-app", "default": true}]}`},
		{name: "rejects content that is not JSON", content: `{"packageDirectories": [`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := updater.UpdateSfdx(tc.content, "2.0.0"); err == nil {
				t.Fatal("UpdateSfdx() error = nil, want an error")
			}
		})
	}
}

func TestBazelVersionArgumentIgnoresCommentsAndStringContents(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, content string }{
		{name: "preceding version example in a comment", content: "module(\n    name = \"release_core\",\n    # version = \"0.9.0\"\n    version = \"1.0.0\",\n)\n"},
		{name: "version-looking text in another string", content: "module(\n    name = 'release_core',\n    repo_name = \"example version = '0.9.0'\",\n    version = \"1.0.0\",\n)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := updater.UpdateBazel(tc.content, "1.1.0")
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(tc.content, `version = "1.0.0"`, `version = "1.1.0"`, 1)
			if got != want {
				t.Fatalf("MODULE.bazel = %s; want %s", got, want)
			}
		})
	}
}
