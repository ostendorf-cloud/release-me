// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// titleFamilies is every kind of release pull request pr opens, a custom
// title pattern for each, the title it renders and the title the default
// pattern rendered before the pattern was set.
var titleFamilies = []struct {
	prFamily
	pattern      string
	nextPattern  string
	sharedTitle  bool
	wantTitle    string
	defaultTitle string
}{
	{
		prFamily:     prFamilies[0],
		pattern:      "release(${component}): ${version} onto ${branch}",
		nextPattern:  "ship ${component} ${version}",
		wantTitle:    "release(main): 0.1.0 onto main",
		defaultTitle: "chore(main): release v0.1.0",
	},
	{
		prFamily:     prFamilies[1],
		sharedTitle:  true,
		pattern:      "build: ship ${releases}",
		nextPattern:  "build(${branch}): release ${releases}",
		wantTitle:    "build: ship api v1.3.0, web v0.3.1",
		defaultTitle: "chore: release api v1.3.0, web v0.3.1",
	},
	{
		prFamily:     prFamilies[2],
		sharedTitle:  true,
		pattern:      "release ${version} of ${components}",
		nextPattern:  "ship ${version} for ${components}",
		wantTitle:    "release 0.1.0 of a, b",
		defaultTitle: "chore: release v0.1.0 (a, b)",
	},
}

// TestTitlePatternRoundTripsThroughPRAndRelease opens the release pull
// request with a custom title pattern and merges it: release reads every
// version back from the title the pattern rendered and tags it.
func TestTitlePatternRoundTripsThroughPRAndRelease(t *testing.T) {
	for guideName, guide := range map[string]bool{"shown": true, "hidden": false} {
		for _, family := range titleFamilies {
			t.Run(family.name+"/guide-"+guideName, func(t *testing.T) {
				dir, _, cfg := family.repo(t)
				cfg.ReleasePullRequestGuide = guide
				setTitlePattern(&cfg, family.pattern, family.sharedTitle)
				f := newPRFake(gitOut(t, dir, "remote", "get-url", "--push", "origin"))
				var stdout bytes.Buffer
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
					t.Fatalf("doPR() error = %v", err)
				}
				created := f.CreatedPullRequests[0]
				if got := strings.Contains(created.Body, "<summary>How this release works</summary>"); got != guide {
					t.Fatalf("guide present = %v, want %v", got, guide)
				}
				if created.Title != family.wantTitle {
					t.Fatalf("title = %q, want %q", created.Title, family.wantTitle)
				}

				releaseMerged(t, dir, cfg, f, created.Title, created.Body, family.wantTags)
			})
		}
	}
}

// TestTitlePatternStillReleasesAPullRequestOpenedBeforeIt merges a release
// pull request carrying the default title after the pattern was set: release
// falls back to the default pattern and still tags every version.
func TestTitlePatternStillReleasesAPullRequestOpenedBeforeIt(t *testing.T) {
	for _, family := range titleFamilies {
		t.Run(family.name, func(t *testing.T) {
			dir, _, cfg := family.repo(t)
			f := newPRFake(gitOut(t, dir, "remote", "get-url", "--push", "origin"))
			var stdout bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doPR() error = %v", err)
			}
			created := f.CreatedPullRequests[0]
			if created.Title != family.defaultTitle {
				t.Fatalf("title = %q, want the default %q", created.Title, family.defaultTitle)
			}

			setTitlePattern(&cfg, family.pattern, family.sharedTitle)
			releaseMerged(t, dir, cfg, f, created.Title, created.Body, family.wantTags)
		})
	}
}

// TestRerunRetitlesAnOpenPullRequestWhenThePatternChanges reruns pr on an
// open release pull request after the pattern was set: nothing about the
// release changed, so nothing is pushed, and the title alone follows the
// pattern.
func TestRerunRetitlesAnOpenPullRequestWhenThePatternChanges(t *testing.T) {
	for _, family := range titleFamilies {
		t.Run(family.name, func(t *testing.T) {
			dir, origin, cfg := family.repo(t)
			f := newPRFake(origin)
			var first bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &first); err != nil {
				t.Fatalf("doPR() first run error = %v", err)
			}
			created := f.CreatedPullRequests[0]
			sha := bareBranchSHA(t, origin)
			f.ListResponses = []forgetest.ListResponse{
				{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{
					Number: 1, Title: created.Title, Body: created.Body, Labels: []string{"release: pending"}, HeadSHA: sha,
				}}},
			}

			setTitlePattern(&cfg, family.pattern, family.sharedTitle)
			var rerun bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &rerun); err != nil {
				t.Fatalf("doPR() rerun error = %v", err)
			}
			if got := bareBranchSHA(t, origin); got != sha {
				t.Fatalf("release branch = %s, want it unchanged at %s: only the title changed", got, sha)
			}
			if len(f.UpdatedPullRequests) != 1 || f.UpdatedPullRequests[0].Title != family.wantTitle {
				t.Fatalf("UpdatedPullRequests = %+v, want one update titled %q", f.UpdatedPullRequests, family.wantTitle)
			}
		})
	}
}

// TestTitlePatternStillReleasesAPullRequestTitledWithAPreviousPattern opens
// the release pull request with one custom pattern and merges it after the
// pattern changed to another, before pr retitled it: release reads the
// pattern the body records and still tags every version.
func TestTitlePatternStillReleasesAPullRequestTitledWithAPreviousPattern(t *testing.T) {
	for _, family := range titleFamilies {
		t.Run(family.name, func(t *testing.T) {
			dir, _, cfg := family.repo(t)
			setTitlePattern(&cfg, family.pattern, family.sharedTitle)
			f := newPRFake(gitOut(t, dir, "remote", "get-url", "--push", "origin"))
			var stdout bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doPR() error = %v", err)
			}
			created := f.CreatedPullRequests[0]
			if !strings.Contains(created.Body, "<!-- release-me-title-pattern: "+family.pattern+" -->") {
				t.Fatalf("body = %q, want it to record the pattern", created.Body)
			}

			setTitlePattern(&cfg, family.nextPattern, family.sharedTitle)
			releaseMerged(t, dir, cfg, f, created.Title, created.Body, family.wantTags)
		})
	}
}

func TestDoPRRefusesATitleThatDoesNotReadBack(t *testing.T) {
	dir, _, cfg := newSingleComponentRepo(t)
	// The branch "release-2026" right after the version reads back as
	// version 0.1.0-release, branch 2026, so release would tag a version
	// pr never computed.
	cfg.ReleasePullRequestTitle = "chore(${component}): release v${version}-${branch}"
	f := newFake()
	f.DefaultBranchName = "release-2026"
	var stdout bytes.Buffer
	err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout)
	if err == nil || !strings.Contains(err.Error(), "does not read back") {
		t.Fatalf("doPR() error = %v, want the title refused", err)
	}
	if len(f.CreatedPullRequests) != 0 {
		t.Fatalf("CreatedPullRequests = %+v, want none", f.CreatedPullRequests)
	}
}

func TestDoPRRefusesATitlePastTheForgeLimit(t *testing.T) {
	dir, _, cfg := newSingleComponentRepo(t)
	cfg.ReleasePullRequestTitle = "chore(${component}): release v${version} " + strings.Repeat("x", 250)
	f := newFake()
	var stdout bytes.Buffer
	err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout)
	if err == nil || !strings.Contains(err.Error(), "past the forge's limit") {
		t.Fatalf("doPR() error = %v, want the title refused", err)
	}
	if len(f.CreatedPullRequests) != 0 {
		t.Fatalf("CreatedPullRequests = %+v, want none", f.CreatedPullRequests)
	}
}

// setTitlePattern sets pattern as the per-component title pattern, or as
// the shared one when shared is true.
func setTitlePattern(cfg *config.Config, pattern string, shared bool) {
	if shared {
		cfg.ReleasePullRequestSharedTitle = pattern
		return
	}
	cfg.ReleasePullRequestTitle = pattern
}

func TestRecordedOverlappingTitlesReleaseTheirOriginalVersions(t *testing.T) {
	cases := []struct {
		family       prFamily
		old, current string
		shared       bool
	}{
		{family: prFamilies[0], old: "chore(${component}): release v${version}-2.0.0", current: "chore(${component}): release v0.1.0-${version}"},
		{family: prFamilies[1], old: "build: ${releases}, api v2.0.0", current: "build: api v1.3.0, web v0.3.1, ${releases}", shared: true},
		{family: prFamilies[2], old: "build: ${version}-2.0.0 (${components})", current: "build: 0.1.0-${version} (${components})", shared: true},
	}
	for _, tc := range cases {
		for _, layout := range []config.ReleasePullRequestLayout{config.LayoutHeadings, config.LayoutDetails, config.LayoutCompact} {
			t.Run(tc.family.name+"/"+string(layout), func(t *testing.T) {
				dir, _, cfg := tc.family.repo(t)
				cfg.ReleasePullRequestLayout = layout
				setTitlePattern(&cfg, tc.old, tc.shared)
				f := newPRFake(gitOut(t, dir, "remote", "get-url", "--push", "origin"))
				var out bytes.Buffer
				if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
					t.Fatal(err)
				}
				created := f.CreatedPullRequests[0]
				setTitlePattern(&cfg, tc.current, tc.shared)
				releaseMerged(t, dir, cfg, f, created.Title, created.Body, tc.family.wantTags)
			})
		}
	}
}
