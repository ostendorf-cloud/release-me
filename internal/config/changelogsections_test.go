// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func TestLoadReadsChangelogSectionsInConfigOrder(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `changelog-sections:
  - type: feat
    section: Features
  - type: deps
    section: Dependencies
  - type: chore
    hidden: true
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []config.ChangelogSection{
		{Type: "feat", Section: "Features"},
		{Type: "deps", Section: "Dependencies"},
		{Type: "chore", Hidden: true},
	}
	if !reflect.DeepEqual(cfg.ChangelogSections, want) {
		t.Fatalf("ChangelogSections = %+v, want %+v", cfg.ChangelogSections, want)
	}
	if got := cfg.Resolve()[0].ChangelogSections; !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve()[0].ChangelogSections = %+v, want %+v", got, want)
	}
}

func TestLoadLeavesChangelogSectionsNilWhenLeftOut(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(writeFile(t, "initial-version: 1.0.0\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ChangelogSections != nil {
		t.Fatalf("ChangelogSections = %+v, want nil for the default sections", cfg.ChangelogSections)
	}
}

func TestLoadKeepsAnEmptyChangelogSectionsListApartFromALeftOutOne(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(writeFile(t, "changelog-sections: []\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ChangelogSections == nil || len(cfg.ChangelogSections) != 0 {
		t.Fatalf("ChangelogSections = %#v, want an empty, non-nil list", cfg.ChangelogSections)
	}
}

func TestResolveAppliesChangelogSectionsFromTheTopLevelAndPerComponentOverride(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `changelog-sections:
  - type: feat
    section: Features
components:
  api:
    path: api
  web:
    path: web
    changelog-sections:
      - type: fix
        section: Fixes
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := cfg.Resolve()
	if want := []config.ChangelogSection{{Type: "feat", Section: "Features"}}; !reflect.DeepEqual(got[0].ChangelogSections, want) {
		t.Fatalf("api ChangelogSections = %+v, want the top level %+v", got[0].ChangelogSections, want)
	}
	if want := []config.ChangelogSection{{Type: "fix", Section: "Fixes"}}; !reflect.DeepEqual(got[1].ChangelogSections, want) {
		t.Fatalf("web ChangelogSections = %+v, want its own %+v", got[1].ChangelogSections, want)
	}
}

func TestLoadRejectsAnInvalidChangelogSectionsEntry(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "an entry without a type",
			content: "changelog-sections:\n  - section: Features\n",
			want:    "changelog-sections: entry 1: type is required",
		},
		{
			name:    "a type listed twice",
			content: "changelog-sections:\n  - type: feat\n    section: Features\n  - type: feat\n    section: New\n",
			want:    `type "feat" is listed twice`,
		},
		{
			name:    "a visible type without a section",
			content: "changelog-sections:\n  - type: deps\n",
			want:    `type "deps": section is required unless hidden is true`,
		},
		{
			name:    "a section spanning two lines",
			content: "changelog-sections:\n  - type: deps\n    section: \"Dependency\\nUpdates\"\n",
			want:    "spans more than one line",
		},
		{
			name:    "an unknown key in an entry",
			content: "changelog-sections:\n  - type: deps\n    section: Dependencies\n    heading: Deps\n",
			want:    "field heading not found",
		},
		{
			name:    "a component's invalid entry",
			content: "components:\n  api:\n    path: api\n    changelog-sections:\n      - type: deps\n",
			want:    `component api: changelog-sections: type "deps": section is required`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(writeFile(t, tc.content))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}
