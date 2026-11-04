// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
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
	Alpha: "release: alpha", Beta: "release: beta", Rc: "release: rc",
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
				Pending: []string{"autorelease: pending"}, Tagged: []string{"autorelease: tagged"}, Alpha: "", Beta: "", Rc: "release: rc",
			}},
			token:      "a-token",
			repoLabels: []string{"autorelease: pending", "autorelease: tagged"},
			want:       []string{`label "release: rc" does not exist on the repository`},
		},
		{
			name: "reports a missing label of a configured prerelease type",
			cfg: config.Config{Labels: config.Labels{
				Pending: []string{"release: pending"}, Tagged: []string{"release: tagged"},
				PreReleaseTypes: []config.PreReleaseType{{Type: "next", Label: "release: next"}},
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
