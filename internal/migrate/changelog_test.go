// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package migrate_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/migrate"
)

func TestFromReleasePleaseTranslatesTheSentenceCasePlugin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		plugins     string
		wantWarning string
	}{
		{name: "named by its type alone", plugins: `["sentence-case"]`},
		{name: "as an object", plugins: `[{"type": "sentence-case"}]`},
		{
			name:    "with special words release-me cannot carry",
			plugins: `[{"type": "sentence-case", "specialWords": ["gRPC", "npm", "macOS"]}]`,
			wantWarning: "plugins: sentence-case: specialWords: release-me keeps gRPC, npm and any first word " +
				"already holding an upper case letter as written, gRPC, npm, macOS left out",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configJSON := `{"plugins": ` + tc.plugins + `, "packages": {".": {"release-type": "go"}}}`
			got, err := migrate.FromReleasePlease([]byte(configJSON), nil, files(nil))
			if err != nil {
				t.Fatalf("FromReleasePlease() error = %v", err)
			}
			if !load(t, got.YAML).ChangelogStyle.SentenceCase {
				t.Fatalf("ChangelogStyle.SentenceCase = false, want true:\n%s", got.YAML)
			}
			var pluginWarnings []string
			for _, w := range got.Warnings {
				if strings.HasPrefix(w, "plugins:") {
					pluginWarnings = append(pluginWarnings, w)
				}
			}
			var want []string
			if tc.wantWarning != "" {
				want = []string{tc.wantWarning}
			}
			if !reflect.DeepEqual(pluginWarnings, want) {
				t.Fatalf("plugin warnings = %q, want %q", pluginWarnings, want)
			}
		})
	}
}

func TestFromReleasePleaseExplainsWhyDateFormatIsLeftOut(t *testing.T) {
	t.Parallel()
	configJSON := `{"date-format": "%d.%m.%Y", "packages": {".": {"release-type": "go", "date-format": "%Y"}}}`
	got, err := migrate.FromReleasePlease([]byte(configJSON), nil, files(nil))
	if err != nil {
		t.Fatalf("FromReleasePlease() error = %v", err)
	}
	reason := "release-please formats the date its generic updater writes with it, not the changelog's, " +
		"which release-me's changelog-date-format sets, left out"
	want := []string{"date-format: " + reason, "package .: date-format: " + reason}
	var w []string
	for _, warning := range got.Warnings {
		if strings.Contains(warning, "date-format") {
			w = append(w, warning)
		}
	}
	if !reflect.DeepEqual(w, want) {
		t.Fatalf("warnings = %q, want %q", w, want)
	}
	if load(t, got.YAML).ChangelogStyle.DateFormat != "" {
		t.Fatalf("YAML = %s, want no changelog-date-format", got.YAML)
	}
}
