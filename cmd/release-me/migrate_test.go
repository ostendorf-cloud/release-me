// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunMigrateTranslatesTheRepositorysReleasePleaseConfig exercises
// migrate end to end through run(): it reads both release-please files from
// the repository root, prints a release-me.yaml that loads, and warns about
// the one package whose last release tag is missing.
func TestRunMigrateTranslatesTheRepositorysReleasePleaseConfig(t *testing.T) {
	dir := newRepo(t)
	writeFiles(t, dir, "chore: configure release-please", map[string]string{
		"release-please-config.json": `{"tag-separator": "@", "separate-pull-requests": true, "packages": {
		  "packages/layer": {"package-name": "@acme/layer", "changelog-path": "/CHANGELOG.md"},
		  "packages/search": {"package-name": "@acme/search"}}}`,
		".release-please-manifest.json": `{"packages/layer": "0.8.1", "packages/search": "0.1.0"}`,
		"packages/layer/package.json":   `{"name": "@acme/layer", "version": "0.8.1"}`,
		"packages/search/package.json":  `{"name": "@acme/search", "version": "0.1.0"}`,
	})
	runGit(t, dir, "tag", "layer@v0.8.1")

	var stdout, stderr bytes.Buffer
	code := run([]string{"migrate", "release-please"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	for _, want := range []string{"  layer:\n", "    tag-prefix: layer@v\n", "    changelog-path: CHANGELOG.md\n", "  search:\n"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %s, want it to contain %q", stdout.String(), want)
		}
	}
	wantWarning := "warning: search: tag search@v0.1.0 does not exist, release-me would start over at the initial version\n"
	if stderr.String() != wantWarning {
		t.Fatalf("stderr = %q, want only %q", stderr.String(), wantWarning)
	}
}

// TestRunMigrateShowsARefusedDeferredTagWithoutAGitHubRelease covers a
// release-please configuration carrying both keys release-me refuses
// together: migrate translates them, the load check catches the
// combination, and the translation is printed for a human to correct
// instead of landing as a release-me.yaml no command can load.
func TestRunMigrateShowsARefusedDeferredTagWithoutAGitHubRelease(t *testing.T) {
	dir := newRepo(t)
	writeFiles(t, dir, "chore: configure release-please", map[string]string{
		"release-please-config.json": `{"packages": {
		  ".": {"force-tag-creation": false, "skip-github-release": true}}}`,
		".release-please-manifest.json": `{".": "1.4.0"}`,
		"package.json":                  `{"name": "@acme/api", "version": "1.4.0"}`,
	})
	runGit(t, dir, "tag", "v1.4.0")

	var stdout, stderr bytes.Buffer
	code := run([]string{"migrate", "release-please"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1, stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing printed", stdout.String())
	}
	for _, want := range []string{
		"the translated release-me.yaml does not load",
		"force-tag-creation",
		"skip-github-release",
		"the refused translation, to correct by hand:",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %s, want it to contain %q", stderr.String(), want)
		}
	}
}

// TestRunMigrateRejectsAnExtraFilesPathThatEscapesTheRepositoryRoot covers a
// release-please-config.json whose extra-files entry climbs out of the
// repository root with "..": migrate leaves it out of files and warns about
// the escape, instead of reading whatever that path resolves to outside the
// repository.
func TestRunMigrateRejectsAnExtraFilesPathThatEscapesTheRepositoryRoot(t *testing.T) {
	dir := newRepo(t)
	writeFiles(t, dir, "chore: configure release-please", map[string]string{
		"release-please-config.json": `{"packages": {
		  ".": {"extra-files": ["../outside.txt"]}}}`,
		".release-please-manifest.json": `{".": "1.0.0"}`,
		"package.json":                  `{"name": "@acme/api", "version": "1.0.0"}`,
	})
	runGit(t, dir, "tag", "v1.0.0")

	var stdout, stderr bytes.Buffer
	code := run([]string{"migrate", "release-please"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "extra-files path \"../outside.txt\" escapes the repository root") {
		t.Fatalf("stderr = %q, want it to warn that the extra-files path escapes the repository root", stderr.String())
	}
	if strings.Contains(stdout.String(), "outside.txt") {
		t.Fatalf("stdout = %s, want the escaping path left out of files", stdout.String())
	}
}

func TestRunMigrateRejectsAMissingOrUnknownSource(t *testing.T) {
	for _, args := range [][]string{{"migrate"}, {"migrate", "semantic-release"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(%q) exit code = %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "release-me migrate release-please") {
			t.Fatalf("run(%q) stderr = %q, want the migrate usage", args, stderr.String())
		}
	}
}

func TestRunMigrateReportsAMissingReleasePleaseConfig(t *testing.T) {
	newRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"migrate", "release-please"}, &stdout, &stderr)

	if code != 1 || !strings.Contains(stderr.String(), "release-please-config.json") {
		t.Fatalf("exit code = %d, stderr = %q, want 1 naming the missing file", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing printed", stdout.String())
	}
}

// TestRunMigrateShowsARefusedTranslationWithItsWarnings covers a package
// nested inside another below the root, a shape release-please supports
// and release-me does not: the warnings and the refused translation reach
// stderr, and the error names release-me.yaml rather than a temporary file.
func TestRunMigrateShowsARefusedTranslationWithItsWarnings(t *testing.T) {
	dir := newRepo(t)
	writeFiles(t, dir, "chore: configure release-please", map[string]string{
		"release-please-config.json": `{"separate-pull-requests": true, "packages": {
		  "packages/web": {"component": "web"},
		  "packages/web/ui": {"component": "ui", "pull-request-header": "UI"}}}`,
		"packages/web/package.json":    `{"name": "@acme/web", "version": "0.4.0"}`,
		"packages/web/ui/package.json": `{"name": "@acme/ui", "version": "0.2.0"}`,
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"migrate", "release-please"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1, stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing printed", stdout.String())
	}
	for _, want := range []string{
		"warning: package packages/web/ui: pull-request-header: no release-me equivalent, left out",
		`config: parse release-me.yaml: component "web" and "ui" have overlapping paths "packages/web" and "packages/web/ui"`,
		"the refused translation, to correct by hand:\n# Generated by release-me migrate release-please.\n",
		"  web:\n    path: packages/web\n",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %s, want it to contain %q", stderr.String(), want)
		}
	}
	if strings.Contains(stderr.String(), "release-me-migrate-") {
		t.Fatalf("stderr = %s, want no temporary path", stderr.String())
	}
}
