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

func TestFromReleasePleaseTranslatesSkipChangelog(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		configJSON string
		// wantTop is the top-level key, want each component's resolved one.
		wantTop bool
		want    []bool
	}{
		{
			name:       "a top-level default every package inherits is written once",
			configJSON: `{"skip-changelog": true, "packages": {"api": {"component": "api"}, "web": {"component": "web"}}}`,
			wantTop:    true,
			want:       []bool{true, true},
		},
		{
			name:       "a package's own stays on its component",
			configJSON: `{"packages": {"api": {"component": "api", "skip-changelog": true}, "web": {"component": "web"}}}`,
			want:       []bool{true, false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := migrate.FromReleasePlease([]byte(tc.configJSON), nil, files(nil))
			if err != nil {
				t.Fatalf("FromReleasePlease() error = %v", err)
			}
			if w := keyWarnings(got.Warnings, "skip-changelog"); len(w) > 0 {
				t.Fatalf("warnings = %q, want none", w)
			}
			cfg := load(t, got.YAML)
			if cfg.SkipChangelog != tc.wantTop {
				t.Fatalf("top-level SkipChangelog = %v, want %v:\n%s", cfg.SkipChangelog, tc.wantTop, got.YAML)
			}
			for i, comp := range cfg.Resolve() {
				if comp.SkipChangelog != tc.want[i] {
					t.Fatalf("component %s SkipChangelog = %v, want %v:\n%s", comp.Name, comp.SkipChangelog, tc.want[i], got.YAML)
				}
			}
		})
	}
}

func keyWarnings(warnings []string, key string) []string {
	var out []string
	for _, w := range warnings {
		if strings.HasPrefix(w, key) {
			out = append(out, w)
		}
	}
	return out
}
