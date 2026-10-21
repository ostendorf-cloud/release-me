// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	forgegithub "github.com/ostendorf-cloud/release-me/internal/forge/github"
	forgegitlab "github.com/ostendorf-cloud/release-me/internal/forge/gitlab"
)

func TestResolveForgeNamePrefersTheConfiguredForge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "remote", "add", "origin", "git@gitlab.example.com:group/project.git")

	name, reason, err := resolveForgeName(t.Context(), dir, "github")
	if err != nil {
		t.Fatalf("resolveForgeName() error = %v", err)
	}
	if name != "github" {
		t.Fatalf("resolveForgeName() name = %q, want %q", name, "github")
	}
	if reason == "" {
		t.Fatal("resolveForgeName() reason is empty")
	}
}

func TestResolveForgeNameFallsBackToTheRemoteHost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		remoteURL string
		want      string
	}{
		{name: "an SSH gitlab host selects gitlab", remoteURL: "git@gitlab.example.com:group/project.git", want: "gitlab"},
		{name: "an HTTPS gitlab host selects gitlab", remoteURL: "https://gitlab.com/group/project.git", want: "gitlab"},
		{name: "a GitHub host selects github", remoteURL: "git@github.com:owner/repo.git", want: "github"},
		{name: "a host without gitlab in its name selects github", remoteURL: "git@git.example.com:owner/repo.git", want: "github"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "-q", "-b", "main")
			runGit(t, dir, "remote", "add", "origin", tc.remoteURL)

			name, _, err := resolveForgeName(t.Context(), dir, "")
			if err != nil {
				t.Fatalf("resolveForgeName() error = %v", err)
			}
			if name != tc.want {
				t.Fatalf("resolveForgeName() name = %q, want %q", name, tc.want)
			}
		})
	}
}

func TestResolveForgeNameFailsWithoutAnOriginRemote(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")

	if _, _, err := resolveForgeName(t.Context(), dir, ""); err == nil {
		t.Fatal("resolveForgeName() error = nil, want an error")
	}
}

func TestForgeTokenEnv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		forgeName  string
		configured string
		want       string
	}{
		{
			name:       "a gitlab forge at the default token-env reads GITLAB_TOKEN",
			forgeName:  "gitlab",
			configured: config.DefaultTokenEnv,
			want:       forgegitlab.DefaultTokenEnv,
		},
		{
			name:       "a gitlab forge with an explicit token-env keeps it",
			forgeName:  "gitlab",
			configured: "CUSTOM_TOKEN",
			want:       "CUSTOM_TOKEN",
		},
		{
			name:       "a github forge keeps the configured token-env",
			forgeName:  "github",
			configured: config.DefaultTokenEnv,
			want:       config.DefaultTokenEnv,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := forgeTokenEnv(tc.forgeName, tc.configured)
			if got != tc.want {
				t.Fatalf("forgeTokenEnv(%q, %q) = %q, want %q", tc.forgeName, tc.configured, got, tc.want)
			}
		})
	}
}

func TestBuildForge(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		forgeName string
	}{
		{name: "builds a github forge", forgeName: "github"},
		{name: "builds a gitlab forge", forgeName: "gitlab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := buildForge(tc.forgeName, "", "a-token")
			if err != nil {
				t.Fatalf("buildForge() error = %v", err)
			}
			switch tc.forgeName {
			case "github":
				if _, ok := f.(*forgegithub.Forge); !ok {
					t.Fatalf("buildForge(%q) = %T, want *github.Forge", tc.forgeName, f)
				}
			case "gitlab":
				if _, ok := f.(*forgegitlab.Forge); !ok {
					t.Fatalf("buildForge(%q) = %T, want *gitlab.Forge", tc.forgeName, f)
				}
			}
		})
	}
}

func TestBuildForgeRejectsAnUnknownName(t *testing.T) {
	t.Parallel()
	if _, err := buildForge("bitbucket", "", "a-token"); err == nil {
		t.Fatal("buildForge() error = nil, want an error")
	}
}
