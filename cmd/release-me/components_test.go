// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// writeFiles writes every path-to-content entry in files inside dir,
// creating parent directories as needed, then stages and commits all of
// them in one direct, first parent commit.
func writeFiles(t *testing.T, dir, message string, files map[string]string) {
	t.Helper()
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
	runGit(t, dir, "commit", "-q", "-m", message)
}

// mergePullRequest commits files onto a side branch created from main, then
// merges it back with --no-ff and a "Merge pull request #N from ..."
// subject, the shape history.Walk recognizes as a pull request merge.
func mergePullRequest(t *testing.T, dir string, number int, branch string, files map[string]string) {
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
	runGit(t, dir, "commit", "-q", "-m", "work on "+branch)
	runGit(t, dir, "checkout", "-q", "main")
	subject := fmt.Sprintf("Merge pull request #%d from owner/%s", number, branch)
	runGit(t, dir, "merge", "--no-ff", "-q", "-m", subject, branch)
}

// showBareFile reads path at branch from a bare repository, failing the
// test when the git invocation fails.
func showBareFile(t *testing.T, bareDir, branch, path string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", bareDir, "show", branch+":"+path)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git show %s:%s: %v", branch, path, err)
	}
	return string(out)
}

// TestDoPRAttributesPullRequestsToTheComponentsTheyTouch exercises a
// two-component repository with a pull request touching both components,
// one touching neither, and one touching only the frontend component. It
// asserts that pr opens one release pull request per component, each
// carrying only the entries that belong to it.
func TestDoPRAttributesPullRequestsToTheComponentsTheyTouch(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeFiles(t, dir, "chore: scaffold the two components", map[string]string{
		"frontend/README.md": "frontend\n",
		"backend/README.md":  "backend\n",
		"README.md":          "root\n",
	})

	// #2 touches both components.
	mergePullRequest(t, dir, 2, "both", map[string]string{
		"frontend/app.js": "console.log(1)\n",
		"backend/app.go":  "package backend\n",
	})
	// #3 touches neither component.
	mergePullRequest(t, dir, 3, "none", map[string]string{
		"README.md": "root, updated\n",
	})
	// #4 touches only the frontend component.
	mergePullRequest(t, dir, 4, "frontend-only", map[string]string{
		"frontend/app.js": "console.log(2)\n",
	})

	writeConfig(t, dir, "components:\n  frontend:\n    path: frontend\n  backend:\n    path: backend\n")

	f := newPRFake(origin)
	f.Responses = map[int]forgetest.Response{
		2: {PullRequest: forge.PullRequest{Title: "feat: wire the frontend to the backend", URL: "https://example.invalid/pull/2"}},
		3: {PullRequest: forge.PullRequest{Title: "feat: update the release notes", URL: "https://example.invalid/pull/3"}},
		4: {PullRequest: forge.PullRequest{Title: "fix: stop a frontend crash", URL: "https://example.invalid/pull/4"}},
	}
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 2 {
		t.Fatalf("CreatedPullRequests = %+v, want two, one per component", f.CreatedPullRequests)
	}

	var frontend, backend *forgetest.CreatePullRequestCall
	for i, c := range f.CreatedPullRequests {
		switch c.Head {
		case "release-me/frontend":
			frontend = &f.CreatedPullRequests[i]
		case "release-me/backend":
			backend = &f.CreatedPullRequests[i]
		}
	}
	if frontend == nil || backend == nil {
		t.Fatalf("CreatedPullRequests = %+v, want one on release-me/frontend and one on release-me/backend", f.CreatedPullRequests)
	}
	if frontend.Title != "chore(frontend): release v0.1.0" || backend.Title != "chore(backend): release v0.1.0" {
		t.Fatalf("titles = %q, %q", frontend.Title, backend.Title)
	}

	frontendChangelog := showBareFile(t, origin, "release-me/frontend", "frontend/CHANGELOG.md")
	if !strings.Contains(frontendChangelog, "wire the frontend to the backend") {
		t.Fatalf("frontend changelog = %q, want the pull request touching both components", frontendChangelog)
	}
	if !strings.Contains(frontendChangelog, "stop a frontend crash") {
		t.Fatalf("frontend changelog = %q, want the pull request touching only the frontend", frontendChangelog)
	}
	if strings.Contains(frontendChangelog, "update the release notes") {
		t.Fatalf("frontend changelog = %q, want the pull request touching neither component excluded", frontendChangelog)
	}

	backendChangelog := showBareFile(t, origin, "release-me/backend", "backend/CHANGELOG.md")
	if !strings.Contains(backendChangelog, "wire the frontend to the backend") {
		t.Fatalf("backend changelog = %q, want the pull request touching both components", backendChangelog)
	}
	if strings.Contains(backendChangelog, "stop a frontend crash") {
		t.Fatalf("backend changelog = %q, want the frontend-only pull request excluded", backendChangelog)
	}
	if strings.Contains(backendChangelog, "update the release notes") {
		t.Fatalf("backend changelog = %q, want the pull request touching neither component excluded", backendChangelog)
	}
}

// TestDoPRLimitsToOneComponentWithTheComponentFlag exercises --component:
// doPR should write only the named component's release pull request, even
// though the repository has two.
func TestDoPRLimitsToOneComponentWithTheComponentFlag(t *testing.T) {
	dir, _ := newPRRepo(t)
	writeFiles(t, dir, "chore: scaffold the two components", map[string]string{
		"frontend/README.md": "frontend\n",
		"backend/README.md":  "backend\n",
	})
	mergePullRequest(t, dir, 2, "frontend-only", map[string]string{"frontend/app.js": "console.log(1)\n"})
	mergePullRequest(t, dir, 3, "backend-only", map[string]string{"backend/app.go": "package backend\n"})
	writeConfig(t, dir, "components:\n  frontend:\n    path: frontend\n  backend:\n    path: backend\n")

	f := newFake()
	f.Responses = map[int]forgetest.Response{
		2: {PullRequest: forge.PullRequest{Title: "feat: add the frontend widget"}},
		3: {PullRequest: forge.PullRequest{Title: "feat: add the backend endpoint"}},
	}
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	var stdout bytes.Buffer

	if err := doPR(t.Context(), dir, cfg, "frontend", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Head != "release-me/frontend" {
		t.Fatalf("CreatedPullRequests = %+v, want one on release-me/frontend only", f.CreatedPullRequests)
	}
	// --component's output is for a single component: no "name: " prefix.
	if !strings.Contains(stdout.String(), "created chore(frontend): release v0.1.0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// TestDoPRRejectsAnUnknownComponentName exercises the config validation
// criterion: an unknown --component name is an error, no pull request
// written.
func TestDoPRRejectsAnUnknownComponentName(t *testing.T) {
	dir, _ := newPRRepo(t)
	writeFiles(t, dir, "chore: scaffold the two components", map[string]string{
		"frontend/README.md": "frontend\n",
		"backend/README.md":  "backend\n",
	})
	writeConfig(t, dir, "components:\n  frontend:\n    path: frontend\n  backend:\n    path: backend\n")

	f := newFake()
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	err = doPR(t.Context(), dir, cfg, "frontendd", f, discardLogger(), false, &bytes.Buffer{})
	if err == nil {
		t.Fatal("doPR() error = nil, want an error naming the unknown component")
	}
	if !strings.Contains(err.Error(), `"frontendd"`) {
		t.Fatalf("error = %v, want it to name the unknown component", err)
	}
	if len(f.CreatedPullRequests) != 0 {
		t.Fatalf("CreatedPullRequests = %+v, want none", f.CreatedPullRequests)
	}
}

// TestRunNextPrefixesEachComponentWhenMoreThanOneIsConfigured exercises the
// next command end to end through run(): with two components configured,
// each version line is prefixed with its component name.
func TestRunNextPrefixesEachComponentWhenMoreThanOneIsConfigured(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "components:\n  frontend:\n    path: frontend\n  backend:\n    path: backend\n")
	writeFiles(t, dir, "chore: scaffold the two components", map[string]string{
		"frontend/README.md": "frontend\n",
		"backend/README.md":  "backend\n",
	})
	// A direct commit, not a merge, so next never needs a forge call for a
	// pull request title: a CLI-level test has no fake forge to answer it.
	writeFiles(t, dir, "feat: add the frontend widget", map[string]string{"frontend/app.js": "console.log(1)\n"})

	var stdout, stderr bytes.Buffer
	code := run([]string{"next"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "frontend: 0.1.0 (initial version)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// TestRunNextComponentFlagLimitsToOneComponent exercises --component
// through run(): with two components configured, only the named one
// prints, and without the "<name>: " prefix since the run targets one
// component.
func TestRunNextComponentFlagLimitsToOneComponent(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "components:\n  frontend:\n    path: frontend\n  backend:\n    path: backend\n")
	writeFiles(t, dir, "chore: scaffold the two components", map[string]string{
		"frontend/README.md": "frontend\n",
		"backend/README.md":  "backend\n",
	})
	writeFiles(t, dir, "feat: add the frontend widget", map[string]string{"frontend/app.js": "console.log(1)\n"})
	writeFiles(t, dir, "feat: add the backend endpoint", map[string]string{"backend/app.go": "package backend\n"})

	var stdout, stderr bytes.Buffer
	code := run([]string{"next", "--component", "frontend"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "0.1.0 (initial version)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// TestRunNextRejectsAnUnknownComponentFlag exercises run() reporting the
// unknown --component name as a configuration error.
func TestRunNextRejectsAnUnknownComponentFlag(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "components:\n  frontend:\n    path: frontend\n  backend:\n    path: backend\n")
	writeFiles(t, dir, "chore: scaffold the two components", map[string]string{
		"frontend/README.md": "frontend\n",
		"backend/README.md":  "backend\n",
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"next", "--component", "frontendd"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), `"frontendd"`) {
		t.Fatalf("stderr = %q, want it to name the unknown component", stderr.String())
	}
}

// TestDoReleaseTagsEachComponentWithItsOwnPrefixAndReleaseName exercises
// release across two components: each gets its own tag prefix and a
// release name carrying the component name.
func TestDoReleaseTagsEachComponentWithItsOwnPrefixAndReleaseName(t *testing.T) {
	dir, _ := newPRRepo(t)
	writeConfig(t, dir, "components:\n  frontend:\n    path: frontend\n  backend:\n    path: backend\n")

	frontendPR := forge.PullRequest{
		Number: 10, Title: "chore(frontend): release v0.1.0",
		Body:   "Release v0.1.0\n\n<details>\n<summary>Changelog</summary>\n\n## v0.1.0 (2026-01-02)\n\n### Features\n\n- add the widget\n\n</details>\n",
		Labels: []string{"release: pending"}, Merged: true,
	}
	backendPR := forge.PullRequest{
		Number: 11, Title: "chore(backend): release v0.1.0",
		Body:   "Release v0.1.0\n\n<details>\n<summary>Changelog</summary>\n\n## v0.1.0 (2026-01-02)\n\n### Features\n\n- add the endpoint\n\n</details>\n",
		Labels: []string{"release: pending"}, Merged: true,
	}
	f := newFake()
	f.ListResponses = []forgetest.ListResponse{
		{Head: "release-me/frontend", State: "closed", PullRequests: []forge.PullRequest{frontendPR}},
		{Head: "release-me/backend", State: "closed", PullRequests: []forge.PullRequest{backendPR}},
	}
	f.MergeCommits = map[int]string{10: "deadbeef", 11: "cafef00d"}
	f.PRLabels = map[int][]string{10: {"release: pending"}, 11: {"release: pending"}}

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	var stdout bytes.Buffer

	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}

	wantTags := map[string]bool{"frontend/v0.1.0": false, "backend/v0.1.0": false}
	for _, tag := range f.CreatedTags {
		if _, ok := wantTags[tag.Tag]; ok {
			wantTags[tag.Tag] = true
		}
	}
	for tag, found := range wantTags {
		if !found {
			t.Fatalf("CreatedTags = %+v, want %q among them", f.CreatedTags, tag)
		}
	}

	wantNames := map[string]bool{"frontend v0.1.0": false, "backend v0.1.0": false}
	for _, r := range f.CreatedReleases {
		if _, ok := wantNames[r.Name]; ok {
			wantNames[r.Name] = true
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Fatalf("CreatedReleases = %+v, want release name %q among them", f.CreatedReleases, name)
		}
	}

	if !strings.Contains(stdout.String(), "frontend: ") || !strings.Contains(stdout.String(), "backend: ") {
		t.Fatalf("stdout = %q, want both component names as prefixes", stdout.String())
	}
}

// TestRunNextReadsTheLastTagThroughAConfiguredTagPrefix exercises
// tag-prefix through run(): a component whose releases were tagged
// "duxt@vX.Y.Z" by another tool continues from its last such tag instead of
// starting over at the initial version.
func TestRunNextReadsTheLastTagThroughAConfiguredTagPrefix(t *testing.T) {
	dir := newRepo(t)
	writeConfig(t, dir, "components:\n  duxt:\n    path: packages/duxt\n    tag-prefix: duxt@v\n")
	writeFiles(t, dir, "chore: scaffold the layer", map[string]string{"packages/duxt/README.md": "duxt\n"})
	runGit(t, dir, "tag", "duxt@v0.8.1")
	writeFiles(t, dir, "fix: hide sections an edition does not have", map[string]string{
		"packages/duxt/sections.ts": "export {}\n",
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"next"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr: %s", code, stderr.String())
	}
	if want := "0.8.2 (fix)\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// TestDoPRWritesAComponentsConfiguredChangelog exercises the changelog key
// through doPR: a component whose changelog lives outside its own path, at
// the repository root, gets its section prepended there and nowhere else.
func TestDoPRWritesAComponentsConfiguredChangelog(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeFiles(t, dir, "chore: scaffold the layer", map[string]string{
		"packages/duxt/README.md": "duxt\n",
		"CHANGELOG.md":            "# Changelog\n",
	})
	mergePullRequest(t, dir, 2, "layer-fix", map[string]string{"packages/duxt/sections.ts": "export {}\n"})
	writeConfig(t, dir, "components:\n  duxt:\n    path: packages/duxt\n    changelog: CHANGELOG.md\n")

	f := newPRFake(origin)
	f.Responses = map[int]forgetest.Response{
		2: {PullRequest: forge.PullRequest{Title: "fix: hide sections an edition does not have"}},
	}
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &bytes.Buffer{}); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	changelog := showBareFile(t, origin, "release-me/duxt", "CHANGELOG.md")
	if !strings.Contains(changelog, "hide sections an edition does not have") {
		t.Fatalf("root CHANGELOG.md = %q, want the component's section", changelog)
	}
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", origin, "cat-file", "-e", "release-me/duxt:packages/duxt/CHANGELOG.md")
	if cmd.Run() == nil {
		t.Fatal("packages/duxt/CHANGELOG.md exists on the release branch, want only the configured changelog written")
	}
}

// TestDoPRCreatesTheDirectoryOfAConfiguredChangelog exercises a changelog
// configured two directories deep where neither directory exists yet: the
// release branch gets the directories and the file, not an error.
func TestDoPRCreatesTheDirectoryOfAConfiguredChangelog(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeFiles(t, dir, "chore: scaffold the layer", map[string]string{"packages/duxt/README.md": "duxt\n"})
	mergePullRequest(t, dir, 2, "layer-fix", map[string]string{"packages/duxt/sections.ts": "export {}\n"})
	writeConfig(t, dir, "components:\n  duxt:\n    path: packages/duxt\n    changelog: docs/release/CHANGELOG.md\n")

	f := newPRFake(origin)
	f.Responses = map[int]forgetest.Response{
		2: {PullRequest: forge.PullRequest{Title: "fix: hide sections an edition does not have"}},
	}
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &bytes.Buffer{}); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	changelog := showBareFile(t, origin, "release-me/duxt", "docs/release/CHANGELOG.md")
	if !strings.Contains(changelog, "hide sections an edition does not have") {
		t.Fatalf("docs/release/CHANGELOG.md = %q, want the component's section", changelog)
	}
}
