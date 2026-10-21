// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "release-me.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoadReturnsDefaultsWhenTheFileIsMissing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "release-me.yaml")

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, config.Default()) {
		t.Fatalf("Load() = %+v, want %+v", got, config.Default())
	}
}

func TestDefaultSetsTheChangelogPathAndLabels(t *testing.T) {
	t.Parallel()
	got := config.Default()
	if got.ChangelogPath != "CHANGELOG.md" {
		t.Fatalf("ChangelogPath = %q, want %q", got.ChangelogPath, "CHANGELOG.md")
	}
	want := config.Labels{Pending: "release: pending", Tagged: "release: tagged"}
	if got.Labels != want {
		t.Fatalf("Labels = %+v, want %+v", got.Labels, want)
	}
}

func TestLoadReadsConfiguredKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    config.Config
	}{
		{
			name:    "applies defaults for keys left out",
			content: "initial-version: 0.5.0\n",
			want: config.Config{
				InitialVersion: "0.5.0",
				HiddenTypes:    config.DefaultHiddenTypes,
				FallbackType:   config.DefaultFallbackType,
				TokenEnv:       config.DefaultTokenEnv,
				ChangelogPath:  config.DefaultChangelogPath,
				Labels:         config.Labels{Pending: config.DefaultPendingLabel, Tagged: config.DefaultTaggedLabel},
			},
		},
		{
			name: "reads every known key",
			content: `initial-version: 0.2.0
bump-minor-pre-major: true
hidden-types: [chore, docs]
fallback-type: misc
token-env: RELEASE_TOKEN
forge: gitlab
base-url: https://gitlab.example.com
changelog: HISTORY.md
labels:
  pending: needs-release
  tagged: released
files:
  - path: cmd/cli/version.go
  - path: deploy/values.yaml
    updater: marker
`,
			want: config.Config{
				InitialVersion:    "0.2.0",
				BumpMinorPreMajor: true,
				HiddenTypes:       []string{"chore", "docs"},
				FallbackType:      "misc",
				TokenEnv:          "RELEASE_TOKEN",
				Forge:             "gitlab",
				BaseURL:           "https://gitlab.example.com",
				ChangelogPath:     "HISTORY.md",
				Labels:            config.Labels{Pending: "needs-release", Tagged: "released"},
				Files: []config.File{
					{Path: "cmd/cli/version.go", Updater: "marker"},
					{Path: "deploy/values.yaml", Updater: "marker"},
				},
			},
		},
		{
			name:    "reads the fallback type alone",
			content: "fallback-type: misc\n",
			want: config.Config{
				InitialVersion: config.DefaultInitialVersion,
				HiddenTypes:    config.DefaultHiddenTypes,
				FallbackType:   "misc",
				TokenEnv:       config.DefaultTokenEnv,
				ChangelogPath:  config.DefaultChangelogPath,
				Labels:         config.Labels{Pending: config.DefaultPendingLabel, Tagged: config.DefaultTaggedLabel},
			},
		},
		{
			name:    "defaults a label left out of the labels block",
			content: "labels:\n  pending: needs-release\n",
			want: config.Config{
				InitialVersion: config.DefaultInitialVersion,
				HiddenTypes:    config.DefaultHiddenTypes,
				FallbackType:   config.DefaultFallbackType,
				TokenEnv:       config.DefaultTokenEnv,
				ChangelogPath:  config.DefaultChangelogPath,
				Labels:         config.Labels{Pending: "needs-release", Tagged: config.DefaultTaggedLabel},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeFile(t, tc.content)

			got, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Load() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLoadAcceptsAnExplicitEmptyHiddenTypesList(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "hidden-types: []\n")

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.HiddenTypes == nil || len(got.HiddenTypes) != 0 {
		t.Fatalf("HiddenTypes = %v, want an empty slice", got.HiddenTypes)
	}
}

func TestLoadRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
	}{
		{name: "rejects an unknown key", content: "major-tag: true\n"},
		{name: "rejects malformed YAML", content: "initial-version: [0.1.0\n"},
		{name: "rejects a fallback type that bumps the version", content: "fallback-type: feat\n"},
		{name: "rejects an unknown updater", content: "files:\n  - path: cmd/cli/version.go\n    updater: go\n"},
		{name: "rejects a forge that is neither github nor gitlab", content: "forge: bitbucket\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeFile(t, tc.content)

			if _, err := config.Load(path); err == nil {
				t.Fatal("Load() error = nil, want an error")
			}
		})
	}
}
