// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func TestLoadReadsTheChangelogStyleKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    config.ChangelogStyle
	}{
		{name: "every key left out", content: "initial-version: 1.0.0\n", want: config.ChangelogStyle{}},
		{
			name:    "changelog-sentence-case",
			content: "changelog-sentence-case: true\n",
			want:    config.ChangelogStyle{SentenceCase: true},
		},
		{name: "changelog-date-format", content: "changelog-date-format: \"%d.%m.%Y\"\n", want: config.ChangelogStyle{DateFormat: "%d.%m.%Y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := config.Load(writeFile(t, tc.content))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.ChangelogStyle != tc.want {
				t.Fatalf("ChangelogStyle = %+v, want %+v", cfg.ChangelogStyle, tc.want)
			}
		})
	}
}

func TestResolveGivesEveryComponentTheTopLevelChangelogStyle(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "changelog-sentence-case: true\nchangelog-date-format: \"%d.%m.%Y\"\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, comp := range cfg.Resolve() {
		if comp.ChangelogStyle != cfg.ChangelogStyle {
			t.Fatalf("component %s ChangelogStyle = %+v, want the top level's %+v", comp.Name, comp.ChangelogStyle, cfg.ChangelogStyle)
		}
	}
}

func TestLoadRejectsAChangelogStyleKeyInsideAComponent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
	}{
		{name: "changelog-sentence-case", content: "components:\n  api:\n    path: api\n    changelog-sentence-case: true\n"},
		{name: "changelog-date-format", content: "components:\n  api:\n    path: api\n    changelog-date-format: \"%Y\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(writeFile(t, tc.content))
			if err == nil {
				t.Fatal("Load() error = nil, want the unknown component key refused")
			}
		})
	}
}

func TestLoadRejectsAnUnknownChangelogDateFormatDirective(t *testing.T) {
	t.Parallel()
	_, err := config.Load(writeFile(t, "changelog-date-format: \"%Y-%m-%d %H:%M\"\n"))
	if err == nil || !strings.Contains(err.Error(), `changelog-date-format "%Y-%m-%d %H:%M" uses an unknown directive`) {
		t.Fatalf("Load() error = %v, want the unknown directive named", err)
	}
}
