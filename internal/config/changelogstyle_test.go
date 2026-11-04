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
		{
			name:    "changelog-host",
			content: "changelog-host: https://github.example.com\n",
			want:    config.ChangelogStyle{Host: "https://github.example.com"},
		},
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
	path := writeFile(t, "changelog-sentence-case: true\nchangelog-host: https://github.example.com\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n")

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
		name string
		key  string
	}{
		{name: "changelog-sentence-case", key: "changelog-sentence-case: true\n"},
		{name: "changelog-host", key: "changelog-host: https://github.example.com\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(writeFile(t, "components:\n  api:\n    path: api\n    "+tc.key))
			if err == nil {
				t.Fatal("Load() error = nil, want the unknown component key refused")
			}
		})
	}
}

func TestLoadReadsChangelogHostWithoutATrailingSlash(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(writeFile(t, "changelog-host: https://github.example.com/\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ChangelogStyle.Host != "https://github.example.com" {
		t.Fatalf("ChangelogStyle.Host = %q, want it without the trailing slash", cfg.ChangelogStyle.Host)
	}
}

func TestLoadRejectsAChangelogHostThatBreaksALink(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		host string
		want string
	}{
		{name: "no scheme", host: "github.example.com", want: "is not an absolute http or https address"},
		{name: "another scheme", host: "ssh://github.example.com", want: "is not an absolute http or https address"},
		{name: "a query", host: "https://github.example.com/?tab=1", want: "carries a query, a fragment or credentials"},
		{name: "credentials", host: "https://user:secret@github.example.com", want: "carries a query, a fragment or credentials"},
		{name: "a parenthesis", host: "https://github.example.com/(git)", want: "parenthesis"},
		{name: "empty", host: "", want: "is not an absolute http or https address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(writeFile(t, "changelog-host: \""+tc.host+"\"\n"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want one containing %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("Load() error = %v, want the credentials left out", err)
			}
		})
	}
}
