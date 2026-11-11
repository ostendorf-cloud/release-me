// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// testLabels is a fully populated config.Labels, so a case that does not
// exercise a missing pre-release label does not trip over one left at its
// zero value.
var testLabels = config.Labels{
	Pending: []string{"release: pending"}, Tagged: []string{"release: tagged"},
	PreRelease: []config.PreReleaseType{
		{Type: "alpha", Label: "release: alpha"}, {Type: "beta", Label: "release: beta"}, {Type: "rc", Label: "release: rc"},
	},
}

// allLabels is every label name testLabels carries, the repository labels
// a case with nothing missing configures.
var allLabels = []string{"release: pending", "release: tagged", "release: alpha", "release: beta", "release: rc"}

func TestDoCheckReportsEveryMissingItem(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		cfg        config.Config
		cfgErr     error
		token      string
		repoLabels []string
		want       []string
	}{
		{
			name:       "reports nothing when everything is present",
			cfg:        config.Config{Labels: testLabels},
			token:      "a-token",
			repoLabels: append(append([]string{}, allLabels...), "feature"),
			want:       nil,
		},
		{
			name:   "reports the config error and skips the label check",
			cfg:    config.Config{},
			cfgErr: errors.New("parse release-me.yaml: unknown key"),
			token:  "a-token",
			want:   []string{"config: parse release-me.yaml: unknown key"},
		},
		{
			name:       "reports the missing token",
			cfg:        config.Config{Labels: testLabels},
			token:      "",
			repoLabels: allLabels,
			want:       []string{"token: environment variable GITHUB_TOKEN is not set"},
		},
		{
			name:       "reports a missing label",
			cfg:        config.Config{Labels: testLabels},
			token:      "a-token",
			repoLabels: []string{"release: pending", "release: alpha", "release: beta", "release: rc"},
			want:       []string{`label "release: tagged" does not exist on the repository`},
		},
		{
			name:       "reports the missing pre-release type labels",
			cfg:        config.Config{Labels: testLabels},
			token:      "a-token",
			repoLabels: []string{"release: pending", "release: tagged"},
			want: []string{
				`label "release: alpha" does not exist on the repository`,
				`label "release: beta" does not exist on the repository`,
				`label "release: rc" does not exist on the repository`,
			},
		},
		{
			name: "does not expect a pre-release label configured empty",
			cfg: config.Config{Labels: config.Labels{
				Pending: []string{"autorelease: pending"}, Tagged: []string{"autorelease: tagged"},
				PreRelease: []config.PreReleaseType{
					{Type: "alpha", Label: ""}, {Type: "beta", Label: ""}, {Type: "rc", Label: "release: rc"},
				},
			}},
			token:      "a-token",
			repoLabels: []string{"autorelease: pending", "autorelease: tagged"},
			want:       []string{`label "release: rc" does not exist on the repository`},
		},
		{
			name: "reports a missing label of a configured prerelease type",
			cfg: config.Config{Labels: config.Labels{
				Pending: []string{"release: pending"}, Tagged: []string{"release: tagged"},
				PreRelease: []config.PreReleaseType{{Type: "next", Label: "release: next"}},
			}},
			token:      "a-token",
			repoLabels: []string{"release: pending", "release: tagged"},
			want:       []string{`label "release: next" does not exist on the repository`},
		},
		{
			name:       "reports every missing label",
			cfg:        config.Config{Labels: testLabels},
			token:      "a-token",
			repoLabels: nil,
			want: []string{
				`label "release: pending" does not exist on the repository`,
				`label "release: tagged" does not exist on the repository`,
				`label "release: alpha" does not exist on the repository`,
				`label "release: beta" does not exist on the repository`,
				`label "release: rc" does not exist on the repository`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &forgetest.Fake{RepoLabels: tc.repoLabels}

			got := doCheck(t.Context(), tc.cfg, tc.cfgErr, nil, "GITHUB_TOKEN", tc.token, "owner", "repo", f)
			if !equalStrings(got, tc.want) {
				t.Fatalf("doCheck() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDoCheckReportsARejectedTokenNotAMissingLabel(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Labels: testLabels}
	wrapped := fmt.Errorf("github: list repository labels: %w: 401 Bad credentials", forge.ErrUnauthorized)
	f := &forgetest.Fake{RepoLabelsErr: wrapped}

	got := doCheck(t.Context(), cfg, nil, nil, "GITHUB_TOKEN", "a-rejected-token", "owner", "repo", f)
	if len(got) != 1 || !strings.HasPrefix(got[0], "token:") {
		t.Fatalf("doCheck() = %v, want a single token line", got)
	}
}

func TestDoCheckDoesNotCheckLabelsWithoutAForge(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Labels: testLabels}

	got := doCheck(t.Context(), cfg, errors.New("boom"), nil, "GITHUB_TOKEN", "a-token", "owner", "repo", nil)
	want := []string{"config: boom"}
	if !equalStrings(got, want) {
		t.Fatalf("doCheck() = %v, want %v", got, want)
	}
}

func TestDoCheckReportsAnUnknownComponentName(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Labels: testLabels}
	f := &forgetest.Fake{RepoLabels: allLabels}

	got := doCheck(t.Context(), cfg, nil, fmt.Errorf(`release-me: unknown component "frontend"`), "GITHUB_TOKEN", "a-token", "owner", "repo", f)
	if len(got) != 1 || !strings.Contains(got[0], `"frontend"`) {
		t.Fatalf("doCheck() = %v, want a single line naming the unknown component", got)
	}
}

// TestWarnIgnoredAuthorNamesEverySetAuthorKey covers the author block pr
// no longer applies: it still loads, so an existing release-me.yaml keeps
// working, and check warns for each key it sets, naming it as ignored,
// while the defaults alone stay silent.
func TestWarnIgnoredAuthorNamesEverySetAuthorKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		content  string
		wantKeys []string
	}{
		{
			name:    "a configuration without an author block warns about nothing",
			content: "initial-version: 0.1.0\n",
		},
		{
			name:     "a configured name and email warn for each",
			content:  "author:\n  name: renovate\n  email: renovate@example.com\n",
			wantKeys: []string{"author.name", "author.email"},
		},
		{
			name:     "an email alone warns for the email only",
			content:  "author:\n  email: release-me[bot]@users.noreply.github.com\n",
			wantKeys: []string{"author.email"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "release-me.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v, want the author block to keep loading", err)
			}

			var logs bytes.Buffer
			warnIgnoredAuthor(cfg, slog.New(slog.NewTextHandler(&logs, nil)))

			got := logs.String()
			if n := strings.Count(got, "level=WARN"); n != len(tc.wantKeys) {
				t.Fatalf("logs = %q, want %d warnings", got, len(tc.wantKeys))
			}
			for _, key := range tc.wantKeys {
				if !strings.Contains(got, key+" is ignored") || !strings.Contains(got, "key="+key) {
					t.Fatalf("logs = %q, want a warning naming %s as ignored", got, key)
				}
			}
		})
	}
}

// TestDoPRWarnsAboutAnIgnoredAuthorKey covers a repository whose workflow
// runs pr and never check: pr is the command that stopped applying the
// key, so the warning reaches it there too, and the run still succeeds.
func TestDoPRWarnsAboutAnIgnoredAuthorKey(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")
	cfg := config.Default()
	cfg.Author.Set = []string{"author.name"}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	if err := doPR(t.Context(), dir, cfg, "", newPRFake(origin), logger, false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if !strings.Contains(logs.String(), "author.name is ignored") {
		t.Fatalf("logs = %q, want a warning naming author.name as ignored", logs.String())
	}
}
