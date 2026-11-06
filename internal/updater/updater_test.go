// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package updater_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/updater"
)

func TestUpdateMarkerReplacesTheVersionOnMarkedLines(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		version string
		want    string
	}{
		{
			name:    "replaces a quoted version before a slash comment",
			content: `const Version = "1.2.3" // release-me: version`,
			version: "2.0.0",
			want:    `const Version = "2.0.0" // release-me: version`,
		},
		{
			name:    "replaces a quoted version before a hash comment",
			content: `version: "1.2.3" # release-me: version`,
			version: "2.0.0",
			want:    `version: "2.0.0" # release-me: version`,
		},
		{
			name:    "replaces a bare version literal",
			content: `version = 1.2.3 # release-me: version`,
			version: "1.3.0",
			want:    `version = 1.3.0 # release-me: version`,
		},
		{
			name:    "keeps the original comment spacing",
			content: `VERSION="1.2.3"  #release-me: version`,
			version: "1.3.0",
			want:    `VERSION="1.3.0"  #release-me: version`,
		},
		{
			name: "updates every marked line and leaves the rest untouched",
			content: `package main

const Version = "1.2.3" // release-me: version

func other() string {
	return "1.2.3"
}

const Fallback = "1.2.3" // release-me: version
`,
			version: "2.0.0",
			want: `package main

const Version = "2.0.0" // release-me: version

func other() string {
	return "1.2.3"
}

const Fallback = "2.0.0" // release-me: version
`,
		},
		{
			name:    "replaces only the first version literal before the marker",
			content: `const Image = "go1.21-1.2.3" // release-me: version`,
			version: "1.3.0",
			want:    `const Image = "go1.21-1.3.0" // release-me: version`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := updater.UpdateMarker(tc.content, tc.version)
			if err != nil {
				t.Fatalf("UpdateMarker() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("UpdateMarker() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdateMarkerReplacesAnExistingPreReleaseOrBuildSuffix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		version string
		want    string
	}{
		{
			name:    "bumps a stable version to a pre-release version",
			content: `0.1.1 # release-me: version`,
			version: "0.3.0-rc.1",
			want:    `0.3.0-rc.1 # release-me: version`,
		},
		{
			name:    "bumps a pre-release version to another build of the same type",
			content: `0.3.0-rc.1 # release-me: version`,
			version: "0.3.0-rc.2",
			want:    `0.3.0-rc.2 # release-me: version`,
		},
		{
			name:    "bumps a pre-release version to a different pre-release type",
			content: `0.3.0-rc.1 # release-me: version`,
			version: "0.3.0-beta.1",
			want:    `0.3.0-beta.1 # release-me: version`,
		},
		{
			name:    "bumps a pre-release version to the stable version it graduates to",
			content: `0.3.0-rc.1 # release-me: version`,
			version: "0.3.0",
			want:    `0.3.0 # release-me: version`,
		},
		{
			name:    "bumps a version carrying build metadata to another one",
			content: `1.2.3+build.5 # release-me: version`,
			version: "1.2.4+build.6",
			want:    `1.2.4+build.6 # release-me: version`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := updater.UpdateMarker(tc.content, tc.version)
			if err != nil {
				t.Fatalf("UpdateMarker() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("UpdateMarker() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdateMarkerRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
	}{
		{name: "rejects content with no marker line", content: "const Version = \"1.2.3\"\n"},
		{name: "rejects a marker line with no version literal", content: "const Version = current // release-me: version\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := updater.UpdateMarker(tc.content, "2.0.0"); err == nil {
				t.Fatal("UpdateMarker() error = nil, want an error")
			}
		})
	}
}

func TestUpdatersRegistersTheMarkerUpdater(t *testing.T) {
	t.Parallel()
	if _, ok := updater.Updaters[updater.Marker]; !ok {
		t.Fatalf("Updaters[%q] missing", updater.Marker)
	}
}

func TestUpdatersRegistersEveryUpdaterWithTheRightKeyRequirement(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		needsKey bool
	}{
		{updater.Marker, false},
		{updater.Helm, false},
		{updater.Go, true},
		{updater.JSON, true},
		{updater.YAML, true},
		{updater.TOML, true},
		{updater.Python, false},
		{updater.Ruby, false},
		{updater.Elixir, false},
		{updater.OCaml, false},
		{updater.R, false},
		{updater.Bazel, false},
		{updater.Dart, false},
		{updater.Sfdx, false},
	}
	for _, tc := range cases {
		entry, ok := updater.Updaters[tc.name]
		if !ok {
			t.Fatalf("Updaters[%q] missing", tc.name)
		}
		if entry.NeedsKey != tc.needsKey {
			t.Fatalf("Updaters[%q].NeedsKey = %v, want %v", tc.name, entry.NeedsKey, tc.needsKey)
		}
	}
}

func TestUpdateHelmReplacesTheTopLevelVersionField(t *testing.T) {
	t.Parallel()
	before := `apiVersion: v2
name: mychart
description: A Helm chart for the service
version: 0.1.0
appVersion: "0.1.0"
dependencies:
  - name: common
    version: 1.2.3
    repository: https://example.com/charts
`
	want := `apiVersion: v2
name: mychart
description: A Helm chart for the service
version: 0.2.0
appVersion: "0.1.0"
dependencies:
  - name: common
    version: 1.2.3
    repository: https://example.com/charts
`

	got, err := updater.UpdateHelm(before, "0.2.0")
	if err != nil {
		t.Fatalf("UpdateHelm() error = %v", err)
	}
	if got != want {
		t.Fatalf("UpdateHelm() = %q, want %q", got, want)
	}
}

func TestUpdateHelmRejectsContentWithNoTopLevelVersionField(t *testing.T) {
	t.Parallel()
	content := "apiVersion: v2\nname: mychart\n"

	if _, err := updater.UpdateHelm(content, "0.2.0"); err == nil {
		t.Fatal("UpdateHelm() error = nil, want an error")
	}
}

func TestUpdateGo(t *testing.T) {
	t.Parallel()
	before := `package version

// Version is the release-me version baked into the binary at release
// time.
const Version = "0.1.0" // do not edit by hand

var Fallback = "0.1.0"
`
	cases := []struct {
		name    string
		key     string
		version string
		want    string
	}{
		{
			name: "replaces a const string literal, keeping the trailing comment",
			key:  "Version", version: "0.2.0",
			want: `package version

// Version is the release-me version baked into the binary at release
// time.
const Version = "0.2.0" // do not edit by hand

var Fallback = "0.1.0"
`,
		},
		{
			name: "replaces a var string literal, leaving the const untouched",
			key:  "Fallback", version: "0.3.0",
			want: `package version

// Version is the release-me version baked into the binary at release
// time.
const Version = "0.1.0" // do not edit by hand

var Fallback = "0.3.0"
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := updater.UpdateGo(before, tc.key, tc.version)
			if err != nil {
				t.Fatalf("UpdateGo() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("UpdateGo() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdateGoRejectsAnIdentifierNotFound(t *testing.T) {
	t.Parallel()
	content := "package version\n\nconst Version = \"0.1.0\"\n"

	if _, err := updater.UpdateGo(content, "BuildNumber", "0.2.0"); err == nil {
		t.Fatal("UpdateGo() error = nil, want an error")
	}
}

func TestUpdateJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		key     string
		version string
		want    string
	}{
		{
			name: "replaces the top-level version field",
			content: `{
  "name": "service",
  "version": "0.1.0",
  "dependencies": {
    "lodash": "4.17.21"
  }
}
`,
			key: "version", version: "0.2.0",
			want: `{
  "name": "service",
  "version": "0.2.0",
  "dependencies": {
    "lodash": "4.17.21"
  }
}
`,
		},
		{
			name: "replaces a nested value at a dotted key path",
			content: `{
  "metadata": {
    "chart": {
      "version": "0.1.0",
      "name": "service"
    }
  }
}
`,
			key: "metadata.chart.version", version: "0.2.0",
			want: `{
  "metadata": {
    "chart": {
      "version": "0.2.0",
      "name": "service"
    }
  }
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := updater.UpdateJSON(tc.content, tc.key, tc.version)
			if err != nil {
				t.Fatalf("UpdateJSON() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("UpdateJSON() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdateJSONRejectsAKeyNotFound(t *testing.T) {
	t.Parallel()
	content := "{\n  \"name\": \"service\"\n}\n"

	if _, err := updater.UpdateJSON(content, "version", "0.2.0"); err == nil {
		t.Fatal("UpdateJSON() error = nil, want an error")
	}
}

func TestUpdateJSONRejectsALeafMissingFromTheTargetedObjectDespiteAMatchElsewhere(t *testing.T) {
	t.Parallel()
	// "chart" carries no "version" field; "other", an unrelated sibling,
	// happens to carry one. That must never be mistaken for a match.
	content := `{
  "metadata": {
    "chart": {
      "name": "service"
    },
    "other": {
      "version": "9.9.9"
    }
  }
}
`

	got, err := updater.UpdateJSON(content, "metadata.chart.version", "0.2.0")
	if err == nil {
		t.Fatalf("UpdateJSON() = %q, error = nil, want a key not found error", got)
	}
}

func TestUpdateJSONScopesABareKeyToTheTopLevel(t *testing.T) {
	t.Parallel()
	// A nested leaf also named "version" appears before the real
	// top-level one; the bare key must not match it.
	content := `{
  "dependencies": {
    "version": "9.9.9"
  },
  "version": "0.1.0"
}
`
	want := `{
  "dependencies": {
    "version": "9.9.9"
  },
  "version": "0.2.0"
}
`

	got, err := updater.UpdateJSON(content, "version", "0.2.0")
	if err != nil {
		t.Fatalf("UpdateJSON() error = %v", err)
	}
	if got != want {
		t.Fatalf("UpdateJSON() = %q, want %q", got, want)
	}
}

func TestUpdateYAML(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		key     string
		version string
		want    string
	}{
		{
			name: "replaces a bare top-level scalar",
			content: `version: 0.1.0
settings:
  debug: false
`,
			key: "version", version: "0.2.0",
			want: `version: 0.2.0
settings:
  debug: false
`,
		},
		{
			name: "replaces a quoted scalar at a nested key path, keeping the quotes",
			content: `replicaCount: 1
image:
  repository: ghcr.io/example/service
  tag: "0.1.0"
service:
  port: 8080
`,
			key: "image.tag", version: "0.2.0",
			want: `replicaCount: 1
image:
  repository: ghcr.io/example/service
  tag: "0.2.0"
service:
  port: 8080
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := updater.UpdateYAML(tc.content, tc.key, tc.version)
			if err != nil {
				t.Fatalf("UpdateYAML() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("UpdateYAML() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdateYAMLRejectsAKeyNotFound(t *testing.T) {
	t.Parallel()
	content := "service:\n  port: 8080\n"

	if _, err := updater.UpdateYAML(content, "image.tag", "0.2.0"); err == nil {
		t.Fatal("UpdateYAML() error = nil, want an error")
	}
}

func TestUpdateYAMLRejectsALeafMissingFromTheTargetedBlockDespiteAMatchElsewhere(t *testing.T) {
	t.Parallel()
	// "chart" carries no "tag"; "other", an unrelated sibling, happens to
	// carry one at the same depth. That must never be mistaken for a
	// match.
	content := `metadata:
  chart:
    name: service
  other:
    tag: "9.9.9"
`

	if _, err := updater.UpdateYAML(content, "metadata.chart.tag", "0.2.0"); err == nil {
		t.Fatal("UpdateYAML() error = nil, want a key not found error")
	}
}

func TestUpdateYAMLScopesABareKeyToTheTopLevel(t *testing.T) {
	t.Parallel()
	// A nested leaf also named "version" appears before the real
	// top-level one; the bare key must not match it.
	content := `dependencies:
  version: 9.9.9
version: 0.1.0
`
	want := `dependencies:
  version: 9.9.9
version: 0.2.0
`

	got, err := updater.UpdateYAML(content, "version", "0.2.0")
	if err != nil {
		t.Fatalf("UpdateYAML() error = %v", err)
	}
	if got != want {
		t.Fatalf("UpdateYAML() = %q, want %q", got, want)
	}
}

func TestUpdateTOML(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		key     string
		version string
		want    string
	}{
		{
			name: "replaces a quoted scalar in a nested table",
			content: `[tool.poetry]
name = "service"
version = "0.1.0"

[tool.poetry.dependencies]
python = "^3.11"
`,
			key: "tool.poetry.version", version: "0.2.0",
			want: `[tool.poetry]
name = "service"
version = "0.2.0"

[tool.poetry.dependencies]
python = "^3.11"
`,
		},
		{
			name: "replaces a top-level scalar before any table header",
			content: `version = "0.1.0"

[package]
name = "service"
`,
			key: "version", version: "0.2.0",
			want: `version = "0.2.0"

[package]
name = "service"
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := updater.UpdateTOML(tc.content, tc.key, tc.version)
			if err != nil {
				t.Fatalf("UpdateTOML() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("UpdateTOML() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdateTOMLRejectsAKeyNotFound(t *testing.T) {
	t.Parallel()
	content := "[package]\nname = \"service\"\n"

	if _, err := updater.UpdateTOML(content, "package.version", "0.2.0"); err == nil {
		t.Fatal("UpdateTOML() error = nil, want an error")
	}
}
