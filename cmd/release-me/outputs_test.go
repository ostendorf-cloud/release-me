// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	tagrelease "github.com/ostendorf-cloud/release-me/internal/release"
)

func TestRenderOutputs(t *testing.T) {
	sharedPR := prOutput{
		Number: 12, URL: "https://github.com/kirchDev/duxt/pull/12", Branch: "release-me/main",
		Components: []string{"duxt", "duxt-ui"},
	}
	cases := []struct {
		name       string
		out        runOutputs
		components []string
		want       string
	}{
		{
			name:       "nothing recorded keeps every handled component unreleased with valid JSON",
			components: []string{"duxt", "duxt-ui"},
			want: "released=false\n" +
				`components={"duxt":{"released":false,"version":"","tag":"","pr":null},"duxt-ui":{"released":false,"version":"","tag":"","pr":null}}` + "\n" +
				"pr=null\n" +
				"prs=[]\n",
		},
		{
			name:       "one shared pull request fills pr and every component it releases",
			out:        runOutputs{prs: []prOutput{sharedPR}},
			components: []string{"duxt", "duxt-ui", "docs"},
			want: "released=false\n" +
				`components={"docs":{"released":false,"version":"","tag":"","pr":null},` +
				`"duxt":{"released":false,"version":"","tag":"","pr":{"number":12,"url":"https://github.com/kirchDev/duxt/pull/12","branch":"release-me/main","components":["duxt","duxt-ui"]}},` +
				`"duxt-ui":{"released":false,"version":"","tag":"","pr":{"number":12,"url":"https://github.com/kirchDev/duxt/pull/12","branch":"release-me/main","components":["duxt","duxt-ui"]}}}` + "\n" +
				`pr={"number":12,"url":"https://github.com/kirchDev/duxt/pull/12","branch":"release-me/main","components":["duxt","duxt-ui"]}` + "\n" +
				`prs=[{"number":12,"url":"https://github.com/kirchDev/duxt/pull/12","branch":"release-me/main","components":["duxt","duxt-ui"]}]` + "\n",
		},
		{
			name: "several pull requests leave pr null and list them all in prs",
			out: runOutputs{prs: []prOutput{
				{Number: 3, URL: "https://github.com/kirchDev/duxt/pull/3", Branch: "release-me/duxt", Components: []string{"duxt"}},
				{Number: 4, URL: "https://github.com/kirchDev/duxt/pull/4", Branch: "release-me/duxt-ui", Components: []string{"duxt-ui"}},
			}},
			components: []string{"duxt", "duxt-ui"},
			want: "released=false\n" +
				`components={"duxt":{"released":false,"version":"","tag":"","pr":{"number":3,"url":"https://github.com/kirchDev/duxt/pull/3","branch":"release-me/duxt","components":["duxt"]}},` +
				`"duxt-ui":{"released":false,"version":"","tag":"","pr":{"number":4,"url":"https://github.com/kirchDev/duxt/pull/4","branch":"release-me/duxt-ui","components":["duxt-ui"]}}}` + "\n" +
				"pr=null\n" +
				`prs=[{"number":3,"url":"https://github.com/kirchDev/duxt/pull/3","branch":"release-me/duxt","components":["duxt"]},` +
				`{"number":4,"url":"https://github.com/kirchDev/duxt/pull/4","branch":"release-me/duxt-ui","components":["duxt-ui"]}]` + "\n",
		},
		{
			name: "a tagged release marks its component released with its version and tag",
			out: runOutputs{releases: []tagrelease.Result{
				{PullRequestNumber: 12, Name: "duxt", Version: "0.9.0", Tag: "duxt@v0.9.0"},
			}},
			components: []string{"duxt", "duxt-ui"},
			want: "released=true\n" +
				`components={"duxt":{"released":true,"version":"0.9.0","tag":"duxt@v0.9.0","pr":null},"duxt-ui":{"released":false,"version":"","tag":"","pr":null}}` + "\n" +
				"pr=null\n" +
				"prs=[]\n",
		},
		{
			name: "a pull request and a release for an unhandled component add no key to components",
			out: runOutputs{
				prs: []prOutput{{Number: 7, URL: "https://github.com/kirchDev/duxt/pull/7", Branch: "release-me/docs", Components: []string{"docs"}}},
				releases: []tagrelease.Result{
					{PullRequestNumber: 7, Name: "docs", Version: "1.2.0", Tag: "docs@v1.2.0"},
				},
			},
			components: []string{"duxt"},
			want: "released=true\n" +
				`components={"duxt":{"released":false,"version":"","tag":"","pr":null}}` + "\n" +
				`pr={"number":7,"url":"https://github.com/kirchDev/duxt/pull/7","branch":"release-me/docs","components":["docs"]}` + "\n" +
				`prs=[{"number":7,"url":"https://github.com/kirchDev/duxt/pull/7","branch":"release-me/docs","components":["docs"]}]` + "\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.out.render(tc.components)
			if err != nil {
				t.Fatalf("render() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("render() =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// parsedOutputs is render's output read back the way a workflow reads it
// through fromJSON.
type parsedOutputs struct {
	released   string
	components map[string]componentOutput
	pr         string
	prs        []prOutput
}

func parseOutputs(t *testing.T, rendered string) parsedOutputs {
	t.Helper()
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("output line %q has no name=value shape", line)
		}
		values[name] = value
	}
	var p parsedOutputs
	p.released, p.pr = values["released"], values["pr"]
	if err := json.Unmarshal([]byte(values["components"]), &p.components); err != nil {
		t.Fatalf("components = %q: %v", values["components"], err)
	}
	if err := json.Unmarshal([]byte(values["prs"]), &p.prs); err != nil {
		t.Fatalf("prs = %q: %v", values["prs"], err)
	}
	return p
}

// TestOutputsFollowTheReleasePullRequestAndTheReleaseInEveryMode opens the
// release pull request with doPR, merges it, and tags it with doRelease,
// the way runRun wires them, then checks what the action's outputs report
// after each step: the pull request and the components it releases, then
// every component released at its own version and tag.
func TestOutputsFollowTheReleasePullRequestAndTheReleaseInEveryMode(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T) (dir string, cfg config.Config)
		wantBranch string
		wantPRFor  []string
		wantTags   map[string]string
		handled    []string
	}{
		{
			name: "independent opens a pull request for the changed component only",
			setup: func(t *testing.T) (string, config.Config) {
				dir, _ := newPRRepo(t)
				writeConfig(t, dir, "components:\n  api:\n    path: api\n  web:\n    path: web\n")
				writeFiles(t, dir, "chore: scaffold the services", map[string]string{"api/README.md": "api\n", "web/README.md": "web\n"})
				runGit(t, dir, "tag", "api/v1.2.0")
				runGit(t, dir, "tag", "web/v0.3.0")
				writeFiles(t, dir, "feat: export orders as CSV", map[string]string{"api/export.go": "package api\n"})
				return dir, loadConfig(t, dir)
			},
			wantBranch: "release-me/api",
			wantPRFor:  []string{"api"},
			wantTags:   map[string]string{"api": "api/v1.3.0"},
			handled:    []string{"api", "web"},
		},
		{
			name: "linked opens a pull request for the changed component at the family's version",
			setup: func(t *testing.T) (string, config.Config) {
				dir, _ := newPRRepo(t)
				writeConfig(t, dir, linkedFamilyConfig)
				writeFiles(t, dir, "chore: scaffold the family", map[string]string{
					"a/README.md": "a\n", "b/README.md": "b\n", "c/README.md": "c\n",
				})
				runGit(t, dir, "tag", "a/v0.8.0")
				runGit(t, dir, "tag", "b/v0.9.0")
				runGit(t, dir, "tag", "c/v0.8.0")
				writeFiles(t, dir, "fix: keep the session on reload", map[string]string{"a/session.go": "package a\n"})
				return dir, loadConfig(t, dir)
			},
			wantBranch: "release-me/a",
			wantPRFor:  []string{"a"},
			wantTags:   map[string]string{"a": "a/v0.9.1"},
			handled:    []string{"a", "b", "c"},
		},
		{
			name: "locked opens one pull request releasing every component",
			setup: func(t *testing.T) (string, config.Config) {
				dir, _ := newPRRepo(t)
				writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
				writeFiles(t, dir, "chore: scaffold the family", map[string]string{"a/README.md": "a\n", "b/README.md": "b\n"})
				writeFiles(t, dir, "feat: add a's feature", map[string]string{"a/feature.go": "package a\n"})
				return dir, loadConfig(t, dir)
			},
			wantBranch: "release-me/main",
			wantPRFor:  []string{"a", "b"},
			wantTags:   map[string]string{"a": "a/v0.1.0", "b": "b/v0.1.0"},
			handled:    []string{"a", "b"},
		},
		{
			name: "shared opens one pull request for every changed component at its own version",
			setup: func(t *testing.T) (string, config.Config) {
				dir, _, cfg := newSharedRepo(t)
				return dir, cfg
			},
			wantBranch: "release-me/main",
			wantPRFor:  []string{"api", "web"},
			wantTags:   map[string]string{"api": "api/v1.3.0", "web": "web/v0.3.1"},
			handled:    []string{"api", "web", "docs"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, cfg := tc.setup(t)
			f := newFake()
			var stdout bytes.Buffer

			prOut := &runOutputs{}
			if err := doPR(withOutputs(t.Context(), prOut), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doPR() error = %v", err)
			}
			if len(f.CreatedPullRequests) != 1 {
				t.Fatalf("CreatedPullRequests = %+v, want one", f.CreatedPullRequests)
			}
			created := f.CreatedPullRequests[0]

			rendered, err := prOut.render(tc.handled)
			if err != nil {
				t.Fatalf("render() error = %v", err)
			}
			got := parseOutputs(t, rendered)
			wantPR := prOutput{Number: 1, URL: "https://github.com/owner/repo/pull/1", Branch: tc.wantBranch, Components: tc.wantPRFor}
			if !reflect.DeepEqual(got.prs, []prOutput{wantPR}) {
				t.Fatalf("prs = %+v, want %+v", got.prs, []prOutput{wantPR})
			}
			if got.released != "false" {
				t.Fatalf("released = %q, want false before the merge", got.released)
			}
			var pr prOutput
			if err := json.Unmarshal([]byte(got.pr), &pr); err != nil || !reflect.DeepEqual(pr, wantPR) {
				t.Fatalf("pr = %q (%v), want %+v", got.pr, err, wantPR)
			}
			for _, name := range tc.handled {
				c, ok := got.components[name]
				if !ok {
					t.Fatalf("components = %+v, want %q among them", got.components, name)
				}
				wantsPR := slices.Contains(tc.wantPRFor, name)
				if (c.PR != nil) != wantsPR || (wantsPR && c.PR.Number != 1) {
					t.Errorf("components[%q].pr = %+v, want it set: %t", name, c.PR, wantsPR)
				}
			}

			// The release pull request merges and the same push's run tags it.
			f.ListResponses = []forgetest.ListResponse{
				{Head: tc.wantBranch, State: "closed", PullRequests: []forge.PullRequest{
					{Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, Merged: true},
				}},
			}
			f.MergeCommits = map[int]string{1: "c0ffee42"}
			f.PRLabels = map[int][]string{1: {"release: pending"}}

			runOut := &runOutputs{}
			ctx := withOutputs(t.Context(), runOut)
			if err := doPR(ctx, dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doPR() after the merge error = %v", err)
			}
			if err := doRelease(ctx, dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doRelease() error = %v", err)
			}
			rendered, err = runOut.render(tc.handled)
			if err != nil {
				t.Fatalf("render() error = %v", err)
			}
			got = parseOutputs(t, rendered)
			if got.released != "true" {
				t.Fatalf("released = %q, want true", got.released)
			}
			if got.pr != "null" || len(got.prs) != 0 {
				t.Fatalf("pr = %q, prs = %+v, want no pull request: the merge leaves none open", got.pr, got.prs)
			}
			for _, name := range tc.handled {
				c := got.components[name]
				tag, wantReleased := tc.wantTags[name]
				if c.Released != wantReleased || c.Tag != tag {
					t.Errorf("components[%q] = %+v, want released %t with tag %q", name, c, wantReleased, tag)
				}
				if wantReleased && !strings.HasSuffix(tag, "v"+c.Version) {
					t.Errorf("components[%q].version = %q, want the version tag %q carries", name, c.Version, tag)
				}
			}
		})
	}
}

func TestOutputsRecordNothingInDryRun(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	out := &runOutputs{}
	var stdout bytes.Buffer
	if err := doPR(withOutputs(t.Context(), out), dir, config.Default(), "", newFake(), discardLogger(), true, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if len(out.prs) != 0 || len(out.releases) != 0 {
		t.Fatalf("outputs = %+v, want nothing recorded in dry-run", out)
	}
}

func TestOutputsReportAnUpToDateReleasePullRequest(t *testing.T) {
	dir, origin := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")

	f := newPRFake(origin)
	var stdout bytes.Buffer
	if err := doPR(t.Context(), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	created := f.CreatedPullRequests[0]
	f.ListResponses = []forgetest.ListResponse{
		{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{
			Number: 1, Title: created.Title, Body: created.Body, Labels: created.Labels,
		}}},
	}

	out := &runOutputs{}
	if err := doPR(withOutputs(t.Context(), out), dir, config.Default(), "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "release pull request up to date") {
		t.Fatalf("stdout = %q, want the second run to find the pull request up to date", stdout.String())
	}
	want := []prOutput{{Number: 1, URL: "https://github.com/owner/repo/pull/1", Branch: mainBranch, Components: []string{"main"}}}
	if !reflect.DeepEqual(out.prs, want) {
		t.Fatalf("prs = %+v, want %+v", out.prs, want)
	}
}

func TestEmitOutputsAppendsToTheFileTheEnvironmentNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github_output")
	if err := os.WriteFile(path, []byte("version=0.2.1\n"), 0o600); err != nil {
		t.Fatalf("write output file: %v", err)
	}
	t.Setenv(outputsEnv, path)

	out := &runOutputs{releases: []tagrelease.Result{{Name: "main", Version: "0.2.2", Tag: "v0.2.2"}}}
	if err := emitOutputs(config.Default().Resolve(), out); err != nil {
		t.Fatalf("emitOutputs() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	want := "version=0.2.1\n" +
		"released=true\n" +
		`components={"main":{"released":true,"version":"0.2.2","tag":"v0.2.2","pr":null}}` + "\n" +
		"pr=null\n" +
		"prs=[]\n"
	if string(got) != want {
		t.Fatalf("output file =\n%s\nwant\n%s", got, want)
	}
}

func TestEmitOutputsWritesNothingWithoutTheEnvironment(t *testing.T) {
	t.Setenv(outputsEnv, "")
	if err := emitOutputs(config.Default().Resolve(), &runOutputs{}); err != nil {
		t.Fatalf("emitOutputs() error = %v", err)
	}
}

// releaseListingFailsForge fails every closed pull request listing once a
// release pull request was created, so a run's pr phase succeeds and its
// release phase then stops on the listing.
type releaseListingFailsForge struct {
	*forgetest.Fake
}

func (f releaseListingFailsForge) PullRequests(ctx context.Context, owner, repo, head, state string) ([]forge.PullRequest, error) {
	if state == "closed" && len(f.CreatedPullRequests) > 0 {
		return nil, errors.New("GET https://api.github.com/repos/owner/repo/pulls: 502 Bad Gateway")
	}
	return f.Fake.PullRequests(ctx, owner, repo, head, state)
}

func TestFinishWriteEmitsWhatARunDidBeforeItsReleasePhaseFailed(t *testing.T) {
	dir, _ := newPRRepo(t)
	commitEntry(t, dir, "feat: add the next command")
	path := filepath.Join(t.TempDir(), "github_output")
	t.Setenv(outputsEnv, path)

	f := releaseListingFailsForge{newFake()}
	out := &runOutputs{}
	runErr := doRun(withOutputs(t.Context(), out), dir, config.Default(), "", f, discardLogger(), false, io.Discard)
	if runErr == nil {
		t.Fatal("doRun() error = nil, want the release phase's listing to fail")
	}

	var stderr bytes.Buffer
	if code := finishWrite(&stderr, config.Default().Resolve(), out, runErr); code != 1 {
		t.Fatalf("finishWrite() = %d, want 1 for the failed release phase", code)
	}
	if !strings.Contains(stderr.String(), "502 Bad Gateway") {
		t.Fatalf("stderr = %q, want the release phase's error", stderr.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	parsed := parseOutputs(t, string(got))
	want := []prOutput{{Number: 1, URL: "https://github.com/owner/repo/pull/1", Branch: mainBranch, Components: []string{"main"}}}
	if !reflect.DeepEqual(parsed.prs, want) {
		t.Fatalf("prs = %+v, want the pull request the pr phase opened: %+v", parsed.prs, want)
	}
	if parsed.released != "false" {
		t.Fatalf("released = %q, want false: the release phase tagged nothing", parsed.released)
	}
}

func loadConfig(t *testing.T, dir string) config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return cfg
}
