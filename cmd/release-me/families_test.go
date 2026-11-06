// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

// prFamily is one kind of release pull request pr opens: the repository it
// opens it in and the tags merging it creates.
type prFamily struct {
	name     string
	repo     func(t *testing.T) (string, string, config.Config)
	wantTags []string
}

// prFamilies is every kind of release pull request pr opens: one
// component, a shared one and a locked family.
var prFamilies = []prFamily{
	{name: "single", repo: newSingleComponentRepo, wantTags: []string{"v0.1.0"}},
	{name: "shared", repo: newSharedRepo, wantTags: []string{"api/v1.3.0", "web/v0.3.1"}},
	{name: "locked", repo: newLockedFamilyRepo, wantTags: []string{"a/v0.1.0", "b/v0.1.0"}},
}

// newSingleComponentRepo returns a repository without components, one
// feature to release, in the shape newSharedRepo returns.
func newSingleComponentRepo(t *testing.T) (dir, origin string, cfg config.Config) {
	t.Helper()
	dir, origin = newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")
	return dir, origin, config.Default()
}

// newLockedFamilyRepo returns a repository with a locked family of a and
// b, a feature in a and nothing in b.
func newLockedFamilyRepo(t *testing.T) (dir, origin string, cfg config.Config) {
	t.Helper()
	dir, origin = newPRRepo(t)
	writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
	writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
	writeFiles(t, dir, "feat: add the CSV export", map[string]string{"a/export.go": "package a\n"})
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return dir, origin, cfg
}
