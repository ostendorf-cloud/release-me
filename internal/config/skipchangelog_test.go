// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func TestResolveAppliesSkipChangelogFromTheTopLevelAndPerComponentOverride(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "skip-changelog: true\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n    skip-changelog: false\n")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := cfg.Resolve()
	if !got[0].SkipChangelog || got[1].SkipChangelog {
		t.Fatalf("SkipChangelog = api %v, web %v, want api true from the top level and web false from its own key",
			got[0].SkipChangelog, got[1].SkipChangelog)
	}
	if config.Default().SkipChangelog {
		t.Fatal("Default().SkipChangelog = true, want false")
	}
}
