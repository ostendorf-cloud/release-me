// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
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
		{name: "changelog-authors", content: "changelog-authors: true\n", want: config.ChangelogStyle{Authors: true}},
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
	path := writeFile(t, "changelog-sentence-case: true\nchangelog-authors: true\n"+
		"components:\n  api:\n    path: api\n  web:\n    path: web\n")

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
	for _, key := range []string{"changelog-sentence-case", "changelog-authors"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(writeFile(t, "components:\n  api:\n    path: api\n    "+key+": true\n"))
			if err == nil {
				t.Fatal("Load() error = nil, want the unknown component key refused")
			}
		})
	}
}
