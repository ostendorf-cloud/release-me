// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package migrate_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/migrate"
)

func TestFromReleasePleaseCarriesDraftPullRequestOver(t *testing.T) {
	t.Parallel()
	for _, draft := range []bool{false, true} {
		configJSON := fmt.Sprintf(`{"draft-pull-request": %v, "packages": {".": {"release-type": "go"}}}`, draft)
		got, err := migrate.FromReleasePlease([]byte(configJSON), nil, files(nil))
		if err != nil {
			t.Fatalf("FromReleasePlease() error = %v", err)
		}
		if cfg := load(t, got.YAML); cfg.ReleasePullRequestDraft != draft {
			t.Fatalf("ReleasePullRequestDraft = %v, want %v\n%s", cfg.ReleasePullRequestDraft, draft, got.YAML)
		}
	}
}

func TestFromReleasePleaseExplainsTheRepositoryWideScopeOfPackageDraftPullRequests(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		configJSON string
		wantDraft  bool
	}{
		{
			name:       "a package draft does not become a repository default",
			configJSON: `{"packages": {".": {"release-type": "go", "draft-pull-request": true}}}`,
		},
		{
			name:       "a package override does not change the repository default",
			configJSON: `{"draft-pull-request": true, "packages": {".": {"release-type": "go", "draft-pull-request": false}}}`,
			wantDraft:  true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := migrate.FromReleasePlease([]byte(tt.configJSON), nil, files(nil))
			if err != nil {
				t.Fatalf("FromReleasePlease() error = %v", err)
			}
			wantWarnings := []string{"package .: draft-pull-request: release-me's release-pull-request-draft is repository-wide, set it at the top level if every release pull request should be a draft"}
			if !reflect.DeepEqual(got.Warnings, wantWarnings) {
				t.Fatalf("Warnings = %q, want %q", got.Warnings, wantWarnings)
			}
			if cfg := load(t, got.YAML); cfg.ReleasePullRequestDraft != tt.wantDraft {
				t.Fatalf("ReleasePullRequestDraft = %v, want %v\n%s", cfg.ReleasePullRequestDraft, tt.wantDraft, got.YAML)
			}
		})
	}
}
