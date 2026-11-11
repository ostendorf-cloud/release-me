// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

// generatedNotes is the shape GitHub's generated release notes have.
const generatedNotes = "## What's Changed\r\n" +
	"* feat: add the widget by @octocat in https://github.com/owner/repo/pull/12\r\n\r\n" +
	"## New Contributors\r\n" +
	"* @octocat made their first contribution in https://github.com/owner/repo/pull/12\r\n\r\n" +
	"**Full Changelog**: https://github.com/owner/repo/compare/v0.1.0...v0.2.0"

// withGeneratedPullRequests returns newFake with responses configured for every
// pull request in prs, keyed by number.
func withGeneratedPullRequests(prs ...forge.PullRequest) *forgetest.Fake {
	f := newFake()
	f.Responses = map[int]forgetest.Response{}
	for _, pr := range prs {
		f.Responses[pr.Number] = forgetest.Response{PullRequest: pr}
	}
	return f
}

func TestDoPRRendersGitHubsGeneratedNotesWithChangelogTypeGitHub(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "changelog-type: github\n")
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)
	mergePullRequest(t, dir, 12, "feat/widget", map[string]string{"widget.go": "package widget\n"})
	head := headSHA(t, dir)

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := withGeneratedPullRequests(forge.PullRequest{Number: 12, Title: "feat: add the widget", URL: "https://github.com/owner/repo/pull/12"})
	f.ReleaseNotes = map[string]string{"v0.2.0": generatedNotes}
	f.GitDir = origin
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	// The conventional commit still decides the version; GitHub writes the notes.
	wantCall := []forgetest.GenerateReleaseNotesCall{{Owner: "owner", Repo: "repo", Tag: "v0.2.0", PreviousTag: "v0.1.0", Target: head}}
	if !reflect.DeepEqual(f.GeneratedReleaseNotes, wantCall) {
		t.Fatalf("GeneratedReleaseNotes = %+v, want %+v", f.GeneratedReleaseNotes, wantCall)
	}
	content := showBareBranchFile(t, origin, "CHANGELOG.md")
	if h, ok := changelog.ParseHeading(content); !ok || h.Version != "0.2.0" {
		t.Fatalf("ParseHeading() = %+v, %v, want the v0.2.0 heading", h, ok)
	}
	want := changelog.ForgeNotes(generatedNotes)
	if got, ok := changelog.TopSection(content); !ok || got != want {
		t.Fatalf("CHANGELOG.md section = %q, want GitHub's notes %q", got, want)
	}
	created := f.CreatedPullRequests[0]
	if got := releasepr.ReleaseBody(created.Body); !strings.Contains(got, want) {
		t.Fatalf("release body = %q, want GitHub's notes", got)
	}

	// The notes read the same on a rerun, so nothing new pushes nothing.
	f.ListResponses = []forgetest.ListResponse{{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
		{Number: 1, Title: created.Title, Body: created.Body, HeadSHA: bareBranchSHA(t, origin)},
	}}}
	var second bytes.Buffer
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &second); err != nil {
		t.Fatalf("doPR() second run error = %v", err)
	}
	if !strings.Contains(second.String(), "up to date") {
		t.Fatalf("stdout = %q, want the pull request up to date", second.String())
	}
}

func TestDoPRUsesGitHubsNotesForOnlyTheComponentThatAsksForThem(t *testing.T) {
	dir, _ := newPRRepo(t)
	writeConfig(t, dir, "release-pull-request: shared\ncomponents:\n"+
		"  api:\n    path: api\n    changelog-type: github\n  web:\n    path: web\n")
	writeFiles(t, dir, "chore: scaffold", map[string]string{"api/main.go": "package api\n", "web/index.html": "<html></html>\n"})
	writeFiles(t, dir, "feat: add the endpoint", map[string]string{"api/endpoint.go": "package api\n"})
	writeFiles(t, dir, "feat: add the page", map[string]string{"web/page.html": "<p></p>\n"})

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := newPRFake(gitOut(t, dir, "remote", "get-url", "--push", "origin"))
	f.ReleaseNotes = map[string]string{"api/v0.1.0": "## What's Changed\n* feat: add the endpoint by @octocat"}
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	if len(f.GeneratedReleaseNotes) != 1 || f.GeneratedReleaseNotes[0].Tag != "api/v0.1.0" || f.GeneratedReleaseNotes[0].PreviousTag != "" {
		t.Fatalf("GeneratedReleaseNotes = %+v, want one call for api's first release", f.GeneratedReleaseNotes)
	}
	text := releasepr.ExtractChangelog(f.CreatedPullRequests[0].Body)
	if got := releasepr.ComponentSection(text, "api"); !strings.Contains(got, "### What's Changed\n* feat: add the endpoint by @octocat") {
		t.Fatalf("api section = %q, want GitHub's notes", got)
	}
	if got := releasepr.ComponentSection(text, "web"); !strings.Contains(got, "### Features\n\n- add the page") {
		t.Fatalf("web section = %q, want its own entries", got)
	}
	if names := releasepr.ComponentNames(text); !reflect.DeepEqual(names, []string{"api", "web"}) {
		t.Fatalf("ComponentNames() = %v, want api and web only", names)
	}
}

func TestDoPRNamesTheComponentWhenGitHubsNotesFail(t *testing.T) {
	dir, _ := newPRRepo(t)
	writeConfig(t, dir, "changelog-type: github\n")
	commitEntry(t, dir, "feat: add the widget")
	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := newFake()
	f.ReleaseNotesErr = errors.New("github: generate release notes for v0.1.0: 403 Resource not accessible by integration")

	err = doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "component main: github: generate release notes for v0.1.0") {
		t.Fatalf("doPR() error = %v, want the component and the tag named", err)
	}
	if len(f.CreatedPullRequests) != 0 {
		t.Fatalf("CreatedPullRequests = %+v, want none after the failure", f.CreatedPullRequests)
	}
}

func TestRenderSectionPrintsGitHubsNotesUnderTheHeading(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "feat: add the widget")
	f := newFake()
	f.ReleaseNotes = map[string]string{"v0.1.0": "## What's Changed\n* feat: add the widget by @octocat"}
	comp := config.Default().Resolve()[0]
	comp.ChangelogType = config.ChangelogTypeGitHub

	rel := release{}
	rel.version.Version = "0.1.0"
	got, err := renderSection(t.Context(), f, dir, "owner", "repo", comp, rel)
	if err != nil {
		t.Fatalf("renderSection() error = %v", err)
	}
	if !strings.HasPrefix(got, "## v0.1.0 (") || !strings.HasSuffix(got, ")\n\n### What's Changed\n* feat: add the widget by @octocat\n") {
		t.Fatalf("renderSection() = %q, want the heading over GitHub's notes", got)
	}
}
