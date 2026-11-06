// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo creates a git repository in a temporary directory, with an origin
// remote and a local identity so commits never depend on the host's git
// configuration, and switches the test's working directory into it.
//
// It uses t.Chdir, so the calling test must not run in parallel.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.name", "release-me tests")
	runGit(t, dir, "config", "user.email", "release-me-tests@example.com")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:owner/repo.git")
	t.Chdir(dir)
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func commitEntry(t *testing.T, dir, subject string) {
	t.Helper()
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", subject)
}

func tagRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "tag", "v0.1.0")
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "release-me.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write release-me.yaml: %v", err)
	}
}

func TestRunNextPrintsTheInitialVersionWithoutATag(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	var stdout, stderr bytes.Buffer
	code := run([]string{"next"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "0.1.0 (initial version)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunNextPrintsAMinorBumpForAFeature(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	commitEntry(t, dir, "feat: add the changelog command")

	var stdout, stderr bytes.Buffer
	code := run([]string{"next"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "0.2.0 (feat)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunNextAppliesTheConfiguredVersioningStrategy(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   string
	}{
		{name: "always bump patch at the top level", config: "versioning-strategy: always-bump-patch\n", want: "0.1.1 (feat, always-bump-patch)\n"},
		{name: "always bump major at the top level", config: "versioning-strategy: always-bump-major\n", want: "1.0.0 (feat, always-bump-major)\n"},
		{name: "the default strategy", config: "versioning-strategy: default\n", want: "0.2.0 (feat)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			writeConfig(t, dir, tc.config)
			commitEntry(t, dir, "chore: initial commit")
			tagRepo(t, dir)
			commitEntry(t, dir, "feat: add the changelog command")

			var stdout, stderr bytes.Buffer
			code := run([]string{"next"}, &stdout, &stderr)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
			}
			if stdout.String() != tc.want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tc.want)
			}
		})
	}
}

func TestRunNextCountsServicePacksOfTheLastVersion(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "versioning-strategy: service-pack\n")
	commitEntry(t, dir, "chore: initial commit")
	runGit(t, dir, "tag", "v1.2.3")

	next := func() string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run([]string{"next"}, &stdout, &stderr); code != 0 {
			t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
		}
		return stdout.String()
	}

	commitEntry(t, dir, "fix: close the connection pool on shutdown")
	if got, want := next(), "1.2.3-sp.1 (fix, service-pack)\n"; got != want {
		t.Fatalf("first service pack: stdout = %q, want %q", got, want)
	}
	runGit(t, dir, "tag", "v1.2.3-sp.1")
	if got := next(); got != "" {
		t.Fatalf("after the first service pack: stdout = %q, want empty, its changes already shipped", got)
	}
	commitEntry(t, dir, "feat: retry a failed health check")
	if got, want := next(), "1.2.3-sp.2 (feat, service-pack)\n"; got != want {
		t.Fatalf("second service pack: stdout = %q, want %q", got, want)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"changelog"}, &stdout, &stderr); code != 0 {
		t.Fatalf("changelog exit code = %d, stderr: %s", code, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "retry a failed health check") || strings.Contains(out, "connection pool") {
		t.Fatalf("changelog = %q, want only the entries since the first service pack", out)
	}
}

func TestRunNextBumpsByADependencyUpdateWithDependencyManifest(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		want    string
	}{
		{name: "dependabot major update", subject: "chore(deps): bump react from 18.3.1 to 19.0.0", want: "1.0.0 (dependency react major update)\n"},
		{name: "renovate minor update", subject: "chore(deps): update dependency vite to v5.4.0", want: "0.2.0 (dependency vite minor update)\n"},
		{name: "an ordinary chore", subject: "chore: tidy up", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			writeConfig(t, dir, "versioning-strategy: dependency-manifest\n")
			commitEntry(t, dir, "chore: initial commit")
			tagRepo(t, dir)
			commitEntry(t, dir, tc.subject)

			var stdout, stderr bytes.Buffer
			if code := run([]string{"next"}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
			}
			if stdout.String() != tc.want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tc.want)
			}
		})
	}
}

func TestRunNextAppliesAComponentsOwnVersioningStrategy(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, `versioning-strategy: always-bump-minor
components:
  api:
    path: api
    versioning-strategy: always-bump-patch
  web:
    path: web
`)
	writeFiles(t, dir, "chore: scaffold", map[string]string{"api/main.go": "package main\n", "web/index.html": "<p>web</p>\n"})
	runGit(t, dir, "tag", "api/v1.4.0")
	runGit(t, dir, "tag", "web/v2.0.3")
	writeFiles(t, dir, "fix: handle an empty request", map[string]string{"api/main.go": "package main // fixed\n"})
	writeFiles(t, dir, "fix: escape the title", map[string]string{"web/index.html": "<p>web &amp; more</p>\n"})

	var stdout, stderr bytes.Buffer
	code := run([]string{"next"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "api: 1.4.1 (fix, always-bump-patch)\nweb: 2.1.0 (fix, always-bump-minor)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunNextPrintsNothingWhenThereIsNothingToRelease(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	commitEntry(t, dir, "chore: tidy up")

	var stdout, stderr bytes.Buffer
	code := run([]string{"next"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunNextLogsDebugOnStderrWhenVerbose(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	var stdout, stderr bytes.Buffer
	code := run([]string{"next", "--verbose"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "resolved repository") {
		t.Fatalf("stderr = %q, want debug logging", stderr.String())
	}
}

func TestRunNextLogsTheChangelogSourceWhenVerbose(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	var stdout, stderr bytes.Buffer
	code := run([]string{"next", "--verbose"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "source=first-parent") {
		t.Fatalf("stderr = %q, want the resolved changelog source logged", stderr.String())
	}
}

func TestRunChangelogLogsAConfiguredChangelogSourceWhenVerbose(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "changelog-source: all\n")
	commitEntry(t, dir, "feat: add the changelog command")

	var stdout, stderr bytes.Buffer
	code := run([]string{"changelog", "--verbose"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "source=all") {
		t.Fatalf("stderr = %q, want the configured changelog source logged", stderr.String())
	}
}

func TestRunNextFailsWithoutAnOriginRemote(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.name", "release-me tests")
	runGit(t, dir, "config", "user.email", "release-me-tests@example.com")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	commitEntry(t, dir, "feat: add the next command")
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"next"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want an error")
	}
}

func TestRunChangelogPrintsTheRenderedEntries(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	commitEntry(t, dir, "feat(cli): add the changelog command")
	commitEntry(t, dir, "fix: stop the panic on empty history")

	var stdout, stderr bytes.Buffer
	code := run([]string{"changelog"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "### Features") || !strings.Contains(stdout.String(), "### Bug Fixes") {
		t.Fatalf("stdout = %q, want Features and Bug Fixes sections", stdout.String())
	}
	if !strings.Contains(stdout.String(), "add the changelog command") {
		t.Fatalf("stdout = %q, want the feature description", stdout.String())
	}
}

func TestRunChangelogPrintsNothingWhenThereIsNothingToRelease(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	commitEntry(t, dir, "chore: tidy up")

	var stdout, stderr bytes.Buffer
	code := run([]string{"changelog"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunChangelogHidesAConfiguredHiddenType(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	writeConfig(t, dir, "hidden-types: [fix]\n")
	commitEntry(t, dir, "feat: add the changelog command")
	commitEntry(t, dir, "fix: stop the panic on empty history")

	var stdout, stderr bytes.Buffer
	code := run([]string{"changelog"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Bug Fixes") {
		t.Fatalf("stdout = %q, want no Bug Fixes section", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Features") {
		t.Fatalf("stdout = %q, want a Features section", stdout.String())
	}
}

func TestRunChangelogReadsConfigFromTheRepositoryRootNotTheWorkingDirectory(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	writeConfig(t, dir, "hidden-types: [feat]\n")
	commitEntry(t, dir, "feat: add the changelog command")

	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(sub)

	var stdout, stderr bytes.Buffer
	code := run([]string{"changelog"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty, the root config hides feat", stdout.String())
	}
}
