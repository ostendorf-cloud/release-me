// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package migrate_test

import (
	"slices"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/migrate"
)

func TestFromReleasePleaseCarriesTheHeaderAndFooterOver(t *testing.T) {
	t.Parallel()
	configJSON := `{"pull-request-header": ":robot: I have created a release *beep* *boop*",
	  "pull-request-footer": "<details><summary>Changelog</summary></details>",
	  "packages": {".": {"release-type": "go"}}}`
	got, err := migrate.FromReleasePlease([]byte(configJSON), nil, files(nil))
	if err != nil {
		t.Fatalf("FromReleasePlease() error = %v", err)
	}
	cfg := load(t, got.YAML)
	if cfg.ReleasePullRequestHeader != ":robot: I have created a release *beep* *boop*" || cfg.ReleasePullRequestFooter != "" {
		t.Fatalf("header, footer = %q, %q, want the header alone\n%s", cfg.ReleasePullRequestHeader, cfg.ReleasePullRequestFooter, got.YAML)
	}
	want := `pull-request-footer: carries "<details><summary>Changelog</summary>", ` +
		"the opening release-me reads the changelog from, left out"
	if !slices.Contains(got.Warnings, want) {
		t.Fatalf("Warnings = %q, want it to contain %q", got.Warnings, want)
	}
}
