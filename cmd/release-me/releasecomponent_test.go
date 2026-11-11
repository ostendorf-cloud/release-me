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
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/history"
)

// mergeCommitWithFiles commits files with message onto a side branch
// created from main, then merges it back with --no-ff and a "Merge pull
// request #N from ..." subject. An empty files map makes the branch commit
// an empty one, and so the merge commit's diff against its first parent.
func mergeCommitWithFiles(t *testing.T, dir string, number int, branch, message string, files map[string]string) {
	t.Helper()
	runGit(t, dir, "checkout", "-q", "-b", branch)
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", path, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", message)
	runGit(t, dir, "checkout", "-q", "main")
	runGit(t, dir, "merge", "--no-ff", "-q", "-m", fmt.Sprintf("Merge pull request #%d from owner/%s", number, branch), branch)
}

// TestComputeReleaseAttributesAnEmptyOrNamedCommitToComponents covers both
// attribution rules in every changelog source mode: an empty commit counts
// for every component, a "Release-Component" footer adds the named
// component on top of the files a commit changed and narrows an empty
// commit to the named components.
func TestComputeReleaseAttributesAnEmptyOrNamedCommitToComponents(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		setup     func(t *testing.T, dir string)
		responses map[int]forgetest.Response
		want      map[string]string
		wantWarn  string
	}{
		{
			name:   "an empty direct commit counts for every component",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "feat: publish the docs site")
			},
			want: map[string]string{"duxt": "0.4.0", "api": "0.4.0"},
		},
		{
			name:   "an empty direct commit with a footer counts for the named component only",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "feat(duxt): document the layout prop\n\nRelease-Component: duxt")
			},
			want: map[string]string{"duxt": "0.4.0"},
		},
		{
			name:   "an empty direct commit naming two components counts for both",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "fix: ship the shared date helper\n\nRelease-Component: duxt, api")
			},
			want: map[string]string{"duxt": "0.3.1", "api": "0.3.1"},
		},
		{
			name:   "a footer attributes a commit outside every component's path",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				writeFiles(t, dir, "feat(duxt): document the layout prop\n\nRelease-Component: duxt",
					map[string]string{"docs/duxt/layout.md": "# Layout\n"})
			},
			want: map[string]string{"duxt": "0.4.0"},
		},
		{
			name:   "a footer adds a component on top of the files a commit changed",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				writeFiles(t, dir, "fix(api): return the layout the client renders\n\nRelease-Component: duxt",
					map[string]string{"packages/api/layout.go": "package api\n"})
			},
			want: map[string]string{"duxt": "0.3.1", "api": "0.3.1"},
		},
		{
			name:   "a merge commit counts through its diff, not as an empty commit",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				mergeCommitWithFiles(t, dir, 20, "fix-api", "fix(api): validate the input",
					map[string]string{"packages/api/validate.go": "package api\n"})
			},
			responses: map[int]forgetest.Response{
				20: {PullRequest: forge.PullRequest{Title: "fix(api): validate the input", URL: "https://example.invalid/pull/20"}},
			},
			want: map[string]string{"api": "0.3.1"},
		},
		{
			name:   "a merged pull request of only empty commits counts for every component",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				mergeCommitWithFiles(t, dir, 21, "retag", "fix: retag the broken build", nil)
			},
			responses: map[int]forgetest.Response{
				21: {PullRequest: forge.PullRequest{Title: "fix: retag the broken build", URL: "https://example.invalid/pull/21"}},
			},
			want: map[string]string{"duxt": "0.3.1", "api": "0.3.1"},
		},
		{
			name:   "a merged pull request's body footer attributes the merge",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				mergeCommitWithFiles(t, dir, 22, "docs-layout", "docs: describe the layout prop",
					map[string]string{"docs/duxt/layout.md": "# Layout\n"})
			},
			responses: map[int]forgetest.Response{
				22: {PullRequest: forge.PullRequest{
					Title: "feat(duxt): document the layout prop",
					Body:  "Documents the prop the layout already takes.\n\nRelease-Component: duxt",
					URL:   "https://example.invalid/pull/22",
				}},
			},
			want: map[string]string{"duxt": "0.4.0"},
		},
		{
			name:   "first-parent mode reads no sub commit footer",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				mergeCommitWithFiles(t, dir, 23, "docs-layout", "docs: describe the layout prop\n\nRelease-Component: duxt",
					map[string]string{"docs/duxt/layout.md": "# Layout\n"})
			},
			responses: map[int]forgetest.Response{
				23: {PullRequest: forge.PullRequest{Title: "feat(duxt): document the layout prop", URL: "https://example.invalid/pull/23"}},
			},
			want: map[string]string{},
		},
		{
			name:   "a squash commit's body footer attributes it",
			source: config.ChangelogSourceSquash,
			setup: func(t *testing.T, dir string) {
				writeFiles(t, dir, "feat(duxt): document the layout prop (#24)\n\n* docs: describe the prop\n\nRelease-Component: duxt",
					map[string]string{"docs/duxt/layout.md": "# Layout\n"})
			},
			responses: map[int]forgetest.Response{
				24: {PullRequest: forge.PullRequest{Title: "feat(duxt): document the layout prop", URL: "https://example.invalid/pull/24"}},
			},
			want: map[string]string{"duxt": "0.4.0"},
		},
		{
			name:   "a sub commit's footer attributes the merge in nested mode",
			source: config.ChangelogSourceNested,
			setup: func(t *testing.T, dir string) {
				mergeCommitWithFiles(t, dir, 25, "docs-layout", "feat(duxt): document the layout prop\n\nRelease-Component: duxt",
					map[string]string{"docs/duxt/layout.md": "# Layout\n"})
			},
			responses: map[int]forgetest.Response{
				25: {PullRequest: forge.PullRequest{Title: "docs: describe the layout", URL: "https://example.invalid/pull/25"}},
			},
			want: map[string]string{"duxt": "0.4.0"},
		},
		{
			name:   "a branch commit's footer attributes it in all mode",
			source: config.ChangelogSourceAll,
			setup: func(t *testing.T, dir string) {
				mergeCommitWithFiles(t, dir, 26, "docs-layout", "feat(duxt): document the layout prop\n\nRelease-Component: duxt",
					map[string]string{"docs/duxt/layout.md": "# Layout\n"})
			},
			responses: map[int]forgetest.Response{
				26: {PullRequest: forge.PullRequest{Title: "docs: describe the layout", URL: "https://example.invalid/pull/26"}},
			},
			want: map[string]string{"duxt": "0.4.0"},
		},
		{
			name:   "an empty sub commit counts for every component in all mode",
			source: config.ChangelogSourceAll,
			setup: func(t *testing.T, dir string) {
				mergeCommitWithFiles(t, dir, 27, "retag", "fix: retag the broken build", nil)
			},
			responses: map[int]forgetest.Response{
				27: {PullRequest: forge.PullRequest{Title: "fix: retag the broken build", URL: "https://example.invalid/pull/27"}},
			},
			want: map[string]string{"duxt": "0.3.1", "api": "0.3.1"},
		},
		{
			name:   "an empty commit naming only an unknown component counts for none, with a warning",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "fix: retag the broken build\n\nRelease-Component: duxtt")
			},
			want:     map[string]string{},
			wantWarn: `level=WARN msg="Release-Component footer names no configured component`,
		},
		{
			name:   "an empty commit naming a known and an unknown component counts for the known one",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "fix: retag the broken build\n\nRelease-Component: duxt, apii")
			},
			want:     map[string]string{"duxt": "0.3.1"},
			wantWarn: `value=apii components=duxt,api`,
		},
		{
			name:   "a fenced footer quoted in a pull request body attributes nothing",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				mergeCommitWithFiles(t, dir, 28, "readme", "docs: explain the footer", map[string]string{
					"packages/api/README.md": "api\n\nSee the footer.\n",
				})
			},
			responses: map[int]forgetest.Response{
				28: {PullRequest: forge.PullRequest{
					Title: "fix(api): explain the footer",
					Body:  "Attribute a docs only commit:\n\n```text\nfeat(duxt): document the layout prop\n\nRelease-Component: duxt\n```",
					URL:   "https://example.invalid/pull/28",
				}},
			},
			want: map[string]string{"api": "0.3.1"},
		},
		{
			name:   "an empty commit's Release-As footer forces only the named component",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "chore: release duxt 1.0.0\n\nRelease-As: 1.0.0\nRelease-Component: duxt")
			},
			want: map[string]string{"duxt": "1.0.0"},
		},
		{
			name:   "an empty commit's Release-As footer forces every component without a name",
			source: config.ChangelogSourceFirstParent,
			setup: func(t *testing.T, dir string) {
				commitEntry(t, dir, "chore: release 1.0.0\n\nRelease-As: 1.0.0")
			},
			want: map[string]string{"duxt": "1.0.0", "api": "1.0.0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			writeConfig(t, dir, "changelog-source: "+tc.source+"\ncomponents:\n"+
				"  duxt:\n    path: packages/duxt\n  api:\n    path: packages/api\n")
			writeFiles(t, dir, "chore: scaffold the two packages", map[string]string{
				"packages/duxt/README.md": "duxt\n",
				"packages/api/README.md":  "api\n",
			})
			runGit(t, dir, "tag", "duxt/v0.3.0")
			runGit(t, dir, "tag", "api/v0.3.0")
			tc.setup(t, dir)

			cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
			if err != nil {
				t.Fatalf("config.Load() error = %v", err)
			}
			f := newFake()
			f.Responses = tc.responses
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))

			got := map[string]string{}
			for _, comp := range cfg.Resolve() {
				rel, released, err := computeRelease(t.Context(), dir, cfg, comp, f, logger)
				if err != nil {
					t.Fatalf("computeRelease(%s) error = %v", comp.Name, err)
				}
				if released {
					got[comp.Name] = rel.version.Version
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("released %v, want %v", got, tc.want)
			}
			if tc.wantWarn == "" && logs.Len() > 0 {
				t.Fatalf("logs = %q, want no warning", logs.String())
			}
			if tc.wantWarn != "" && !strings.Contains(logs.String(), tc.wantWarn) {
				t.Fatalf("logs = %q, want a warning containing %q", logs.String(), tc.wantWarn)
			}
		})
	}
}

// TestComputeReleaseInASingleComponentRepositoryIgnoresTheFooter pins that
// the footer changes nothing without components: the implicit "main"
// component at "." counts every commit without reading a footer, so even a
// name it does not carry goes without a warning, since it cannot change
// the outcome.
func TestComputeReleaseInASingleComponentRepositoryIgnoresTheFooter(t *testing.T) {
	cases := []struct {
		name     string
		message  string
		wantWarn bool
	}{
		{name: "a footer naming main is silent", message: "fix: retag the broken build\n\nRelease-Component: main"},
		{name: "a footer naming another name is silent", message: "fix: retag the broken build\n\nRelease-Component: duxt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			commitEntry(t, dir, "chore: initial commit")
			tagRepo(t, dir)
			commitEntry(t, dir, tc.message)

			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			cfg := config.Default()
			rel, released, err := computeRelease(t.Context(), dir, cfg, cfg.Resolve()[0], newFake(), logger)
			if err != nil {
				t.Fatalf("computeRelease() error = %v", err)
			}
			if !released || rel.version.Version != "0.1.1" {
				t.Fatalf("released = %v, version = %q, want 0.1.1", released, rel.version.Version)
			}
			warned := strings.Contains(logs.String(), "Release-Component footer names no configured component")
			if warned != tc.wantWarn {
				t.Fatalf("logs = %q, want a warning %v", logs.String(), tc.wantWarn)
			}
		})
	}
}

// TestFamilyReleasesCountAnEmptyCommitForEveryMember covers linked
// versioning: an empty commit changes every member, a footer narrows it.
func TestFamilyReleasesCountAnEmptyCommitForEveryMember(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    []string
	}{
		{name: "an empty commit changes every member", message: "fix: retag the broken build", want: []string{"a", "b"}},
		{name: "a footer narrows an empty commit to one member", message: "fix: retag a\n\nRelease-Component: a", want: []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			writeConfig(t, dir, "versioning: linked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
			writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
			runGit(t, dir, "tag", "a/v0.8.0")
			runGit(t, dir, "tag", "b/v0.8.0")
			commitEntry(t, dir, tc.message)

			cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
			if err != nil {
				t.Fatalf("config.Load() error = %v", err)
			}
			logger := discardLogger()
			members, err := gatherFamily(t.Context(), dir, cfg, "owner", "repo", newFake(), logger)
			if err != nil {
				t.Fatalf("gatherFamily() error = %v", err)
			}
			releases, _ := familyReleases(cfg, members, logger)
			var got []string
			for _, name := range []string{"a", "b"} {
				if _, ok := releases[name]; ok {
					got = append(got, name)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("released %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAComponentsFirstReleaseLeavesOutEmptyCommitsOlderThanItsPath covers
// an untagged component, whose range reaches back past the commit that
// created it: an empty commit from before then is not its own, in any
// changelog source, unless a footer names it.
func TestAComponentsFirstReleaseLeavesOutEmptyCommitsOlderThanItsPath(t *testing.T) {
	cases := []struct {
		name    string
		before  string
		after   string
		want    []string
		notWant []string
	}{
		{
			name:    "an empty commit older than the path is left out",
			before:  "feat(header): reveal the new theme from the toggle",
			want:    []string{"add the wiki package"},
			notWant: []string{"reveal the new theme"},
		},
		{
			name:   "an older empty commit naming the component still counts",
			before: "feat(header): reveal the new theme from the toggle\n\nRelease-Component: wiki",
			want:   []string{"add the wiki package", "reveal the new theme"},
		},
		{
			name:  "an empty commit once the path exists counts on a first release",
			after: "fix(search): retag the broken index",
			want:  []string{"add the wiki package", "retag the broken index"},
		},
	}
	sources := []string{
		config.ChangelogSourceFirstParent, config.ChangelogSourceSquash, config.ChangelogSourceAll, config.ChangelogSourceNested,
	}
	for _, tc := range cases {
		for _, source := range sources {
			t.Run(tc.name+"/"+source, func(t *testing.T) {
				dir := newRepo(t)
				writeConfig(t, dir, "changelog-source: "+source+
					"\ncomponents:\n  duxt:\n    path: packages/duxt\n  wiki:\n    path: packages/wiki\n    initial-version: 0.1.0\n")
				writeFiles(t, dir, "chore: scaffold duxt", map[string]string{"packages/duxt/README.md": "duxt\n"})
				if tc.before != "" {
					commitEntry(t, dir, tc.before)
				}
				writeFiles(t, dir, "feat(wiki): add the wiki package", map[string]string{"packages/wiki/index.ts": "export {}\n"})
				if tc.after != "" {
					commitEntry(t, dir, tc.after)
				}

				cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
				if err != nil {
					t.Fatalf("config.Load() error = %v", err)
				}
				rel, released, err := computeRelease(t.Context(), dir, cfg, cfg.Resolve()[1], newFake(), discardLogger())
				if err != nil {
					t.Fatalf("computeRelease() error = %v", err)
				}
				if !released {
					t.Fatal("released = false, want the wiki's first release")
				}
				var got []string
				for _, e := range rel.entries {
					got = append(got, e.Description)
				}
				text := strings.Join(got, "\n")
				for _, want := range tc.want {
					if !strings.Contains(text, want) {
						t.Errorf("entries = %q, want one containing %q", got, want)
					}
				}
				for _, notWant := range tc.notWant {
					if strings.Contains(text, notWant) {
						t.Errorf("entries = %q, want none containing %q", got, notWant)
					}
				}
			})
		}
	}
}

// TestLinkedSharedFirstChangelogStaysWithinTheComponent is the shape of
// the report in #115: a new package joins a linked family on a shared
// release pull request with changelog-source all. An empty commit older
// than the package stays in the tagged component's changelog and out of
// the new one's.
func TestLinkedSharedFirstChangelogStaysWithinTheComponent(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "versioning: linked\nrelease-pull-request: shared\nchangelog-source: all\ncomponents:\n"+
		"  duxt:\n    path: packages/duxt\n    tag-prefix: duxt@v\n"+
		"  wiki:\n    path: packages/wiki\n    tag-prefix: wiki@v\n    initial-version: 0.1.0\n")
	writeFiles(t, dir, "chore: scaffold duxt", map[string]string{"packages/duxt/README.md": "duxt\n"})
	runGit(t, dir, "tag", "duxt@v0.12.0")
	commitEntry(t, dir, "feat(header): reveal the new theme from the toggle")
	writeFiles(t, dir, "feat(wiki): add the wiki package", map[string]string{"packages/wiki/index.ts": "export {}\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if err := doPR(t.Context(), dir, cfg, "", newPRFake(origin), discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	wiki := showBareFile(t, origin, "release-me/main", "packages/wiki/CHANGELOG.md")
	if !strings.Contains(wiki, "add the wiki package") || strings.Contains(wiki, "reveal the new theme") {
		t.Fatalf("wiki changelog = %q, want only its own commit", wiki)
	}
	duxt := showBareFile(t, origin, "release-me/main", "packages/duxt/CHANGELOG.md")
	if !strings.Contains(duxt, "reveal the new theme") {
		t.Fatalf("duxt changelog = %q, want the empty commit made while it existed", duxt)
	}
}

func TestPullRequestCacheReadsEachPullRequestOnce(t *testing.T) {
	cases := []struct {
		name    string
		number  int
		wantErr error
	}{
		{name: "a pull request the forge has", number: 30},
		{name: "a pull request the forge does not have", number: 31, wantErr: forge.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counting := newCountingForge(&forgetest.Fake{Responses: map[int]forgetest.Response{
				30: {PullRequest: forge.PullRequest{Number: 30, Title: "feat(duxt): document the layout prop"}},
			}})
			cache := newPullRequestCache(counting)
			for range 2 {
				_, err := cache.PullRequest(t.Context(), "owner", "repo", tc.number)
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("PullRequest() error = %v, want %v", err, tc.wantErr)
				}
			}
			if counting.calls[tc.number] != 1 {
				t.Fatalf("forge reads = %d, want 1", counting.calls[tc.number])
			}
		})
	}
}

func TestAttributionReadsEachMergesSubCommitsOnce(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	mergeCommitWithFiles(t, dir, 32, "layout", "feat(duxt): add the layout prop", map[string]string{
		"packages/duxt/layout.ts": "export const layout = 1\n",
	})
	walked, err := history.Walk(t.Context(), dir, "")
	if err != nil {
		t.Fatalf("history.Walk() error = %v", err)
	}
	merge := walked[0].SHA

	attr := newAttribution(dir, config.Default(), "owner", "repo", newFake(), discardLogger())
	first, err := attr.subCommits(t.Context(), merge)
	if err != nil {
		t.Fatalf("subCommits() error = %v", err)
	}
	if len(first) != 1 || first[0].Subject != "feat(duxt): add the layout prop" {
		t.Fatalf("subCommits() = %+v, want the one branch commit", first)
	}
	// A second read never reaches git: with the repository gone, only the
	// memoised sub commits can answer.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove the repository: %v", err)
	}
	second, err := attr.subCommits(t.Context(), merge)
	if err != nil {
		t.Fatalf("second subCommits() error = %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("second subCommits() = %+v, want %+v", second, first)
	}
}

func TestComputeReleasesSharesOneAttributionAcrossIndependentComponents(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "components:\n  duxt:\n    path: packages/duxt\n  api:\n    path: packages/api\n")
	writeFiles(t, dir, "chore: scaffold the two packages", map[string]string{
		"packages/duxt/README.md": "duxt\n",
		"packages/api/README.md":  "api\n",
	})
	runGit(t, dir, "tag", "duxt/v0.3.0")
	runGit(t, dir, "tag", "api/v0.3.0")
	mergeCommitWithFiles(t, dir, 33, "dates", "fix: share the date helper", map[string]string{
		"packages/duxt/date.ts": "export const date = 1\n",
		"packages/api/date.go":  "package api\n",
	})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	counting := newCountingForge(&forgetest.Fake{Responses: map[int]forgetest.Response{
		33: {PullRequest: forge.PullRequest{
			Title: "fix: share the date helper",
			Body:  "Moves the helper.\n\nRelease-Component: datez",
			URL:   "https://example.invalid/pull/33",
		}},
	}})
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	results, err := computeReleases(t.Context(), dir, cfg, cfg.Resolve(), "owner", "repo", counting, logger)
	if err != nil {
		t.Fatalf("computeReleases() error = %v", err)
	}
	got := map[string]string{}
	for _, r := range results {
		if r.released {
			got[r.comp.Name] = r.rel.version.Version
		}
	}
	if want := map[string]string{"duxt": "0.3.1", "api": "0.3.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("released %v, want %v", got, want)
	}
	if counting.calls[33] != 1 {
		t.Fatalf("forge reads of #33 = %d, want 1 across both components", counting.calls[33])
	}
	if n := strings.Count(logs.String(), "value=datez"); n != 1 {
		t.Fatalf("logs = %q, want the unknown name warned once, got %d", logs.String(), n)
	}
}
