// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

func TestDoPRLeavesTheChangelogAloneWithSkipChangelog(t *testing.T) {
	cases := []struct {
		name string
		// configYAML is release-me.yaml.
		configYAML string
		// version is a version file bumped alongside, "" for none.
		version string
	}{
		{name: "with no file to bump the release commit is empty", configYAML: "skip-changelog: true\n"},
		{
			name:       "a file to bump is still bumped",
			configYAML: "skip-changelog: true\nfiles:\n  - path: version.txt\n",
			version:    "0.1.0 # release-me: version\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			writeConfig(t, dir, tc.configYAML)
			writeAndCommit(t, dir, "CHANGELOG.md", "# Changelog\n\n## v0.1.0 (2026-10-01)\n\n- written by hand\n", "chore: initial commit")
			if tc.version != "" {
				writeAndCommit(t, dir, "version.txt", tc.version, "chore: add the version file")
			}
			tagRepo(t, dir)
			commitEntry(t, dir, "feat: add the widget")

			cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
			if err != nil {
				t.Fatalf("config.Load() error = %v", err)
			}
			f := newPRFake(origin)
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
				t.Fatalf("doPR() error = %v", err)
			}

			if got := showBareBranchFile(t, origin, "CHANGELOG.md"); got != "# Changelog\n\n## v0.1.0 (2026-10-01)\n\n- written by hand\n" {
				t.Fatalf("CHANGELOG.md = %q, want it untouched", got)
			}
			if tc.version != "" {
				if got := showBareBranchFile(t, origin, "version.txt"); got != "0.2.0 # release-me: version\n" {
					t.Fatalf("version.txt = %q, want it bumped", got)
				}
			}
			body := f.CreatedPullRequests[0].Body
			if got := releasepr.ExtractChangelog(body); !strings.Contains(got, "### Features\n\n- add the widget") {
				t.Fatalf("pull request changelog = %q, want the section rendered all the same", got)
			}
			if got := releasepr.ReleaseBody(body); !strings.Contains(got, "- add the widget") {
				t.Fatalf("release body = %q, want the section rendered all the same", got)
			}

			// A rerun with nothing new pushes nothing: the changelog file
			// the branch leaves alone never counts as out of date.
			pushes := bareBranchPushCount(t, origin)
			created := f.CreatedPullRequests[0]
			f.ListResponses = []forgetest.ListResponse{{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
				{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: bareBranchSHA(t, origin)},
			}}}
			var second bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &second); err != nil {
				t.Fatalf("doPR() second run error = %v", err)
			}
			if !strings.Contains(second.String(), "up to date") || bareBranchPushCount(t, origin) != pushes {
				t.Fatalf("stdout = %q, want the pull request up to date with no push", second.String())
			}
		})
	}
}

// bareFileExists reports whether path exists at branch in a bare
// repository.
func bareFileExists(t *testing.T, bareDir, branch, path string) bool {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", bareDir, "cat-file", "-e", branch+":"+path)
	return cmd.Run() == nil
}

func TestDoPRSkipsTheChangelogOfOnlyTheComponentThatAsksTo(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "release-pull-request: shared\ncomponents:\n"+
		"  api:\n    path: api\n    skip-changelog: true\n  web:\n    path: web\n")
	writeFiles(t, dir, "chore: scaffold", map[string]string{"api/main.go": "package api\n", "web/index.html": "<html></html>\n"})
	writeFiles(t, dir, "feat: add the endpoint", map[string]string{"api/endpoint.go": "package api\n"})
	writeFiles(t, dir, "feat: add the page", map[string]string{"web/page.html": "<p></p>\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := newPRFake(origin)
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if bareFileExists(t, origin, mainBranch, "api/CHANGELOG.md") {
		t.Fatal("api/CHANGELOG.md exists, want api's changelog skipped")
	}
	if got := showBareFile(t, origin, mainBranch, "web/CHANGELOG.md"); !strings.Contains(got, "- add the page") {
		t.Fatalf("web/CHANGELOG.md = %q, want web's section written", got)
	}
	changelogText := releasepr.ExtractChangelog(f.CreatedPullRequests[0].Body)
	if !strings.Contains(releasepr.ComponentSection(changelogText, "api"), "- add the endpoint") {
		t.Fatalf("pull request changelog = %q, want api's section in the body all the same", changelogText)
	}
}

func TestEnablingSkipChangelogDiscardsExistingBranchEdits(t *testing.T) {
	for _, tt := range []struct {
		name             string
		shared, existing bool
	}{
		{"single absent changelog", false, false},
		{"single existing changelog", false, true},
		{"mixed shared absent changelog", true, false},
		{"mixed shared existing changelog", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			yaml := ""
			path := "CHANGELOG.md"
			files := map[string]string{"main.go": "package main\n"}
			if tt.shared {
				yaml = "release-pull-request: shared\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n"
				path = "api/CHANGELOG.md"
				files = map[string]string{"api/main.go": "package api\n", "web/index.html": "<p>web</p>\n"}
			}
			const baseline = "# Changelog\n\nWritten by hand.\n"
			if tt.existing {
				files[path] = baseline
			}
			writeConfig(t, dir, yaml)
			writeFiles(t, dir, "chore: scaffold", files)
			if tt.shared {
				writeFiles(t, dir, "feat: add endpoint", map[string]string{"api/new.go": "package api\n"})
				writeFiles(t, dir, "feat: add page", map[string]string{"web/new.html": "<p>new</p>\n"})
			} else {
				commitEntry(t, dir, "feat: add endpoint")
			}
			cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			f := newPRFake(origin)
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
				t.Fatal(err)
			}
			created := f.CreatedPullRequests[0]
			old := bareBranchSHA(t, origin)
			f.ListResponses = []forgetest.ListResponse{{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: old}}}}
			if tt.shared {
				yaml = strings.Replace(yaml, "    path: api\n", "    path: api\n    skip-changelog: true\n", 1)
			} else {
				yaml = "skip-changelog: true\n"
			}
			writeConfig(t, dir, yaml)
			cfg, err = config.Load(filepath.Join(dir, "release-me.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
				t.Fatal(err)
			}
			tip := bareBranchSHA(t, origin)
			if tip == old {
				t.Fatal("release branch still contains the generated changelog edit")
			}
			if got := bareFileExists(t, origin, mainBranch, path); got != tt.existing {
				t.Fatalf("changelog exists = %v, want %v", got, tt.existing)
			}
			if tt.existing {
				if got := showBareFile(t, origin, mainBranch, path); got != baseline {
					t.Fatalf("changelog = %q, want original %q", got, baseline)
				}
			}
			if tt.shared {
				if got := showBareFile(t, origin, mainBranch, "web/CHANGELOG.md"); !strings.Contains(got, "- add page") {
					t.Fatalf("web changelog = %q", got)
				}
			}
			if !strings.Contains(releasepr.ExtractChangelog(f.UpdatedPullRequests[len(f.UpdatedPullRequests)-1].Body), "- add endpoint") {
				t.Fatal("skipped component disappeared from the body")
			}
			f.ListResponses[0].PullRequests[0].HeadSHA = tip
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
				t.Fatal(err)
			}
			if got := bareBranchSHA(t, origin); got != tip {
				t.Fatalf("rerun moved branch from %s to %s", tip, got)
			}
		})
	}
}
