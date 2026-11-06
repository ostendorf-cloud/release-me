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
		{name: "changelog-commit-links", content: "changelog-commit-links: true\nchangelog-issue-references: true\n", want: config.ChangelogStyle{CommitLinks: true, IssueReferences: true}},
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
	path := writeFile(t, "changelog-commit-links: true\nchangelog-issue-references: true\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n")

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
	_, err := config.Load(writeFile(t, "components:\n  api:\n    path: api\n    changelog-commit-links: true\n"))
	if err == nil {
		t.Fatal("Load() error = nil, want the unknown component key refused")
	}
}
