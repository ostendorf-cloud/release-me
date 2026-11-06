// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package release_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/release"
)

// releaseRun is one way release reads a merged release pull request: a
// component's own branch, a locked family or a shared release pull request.
type releaseRun func(f *forgetest.Fake, comps []release.Component, draftUntilAssetsAttach, dryRun bool) ([]release.Result, error)

func runOwn(f *forgetest.Fake, comps []release.Component, draftUntilAssetsAttach, dryRun bool) ([]release.Result, error) {
	return release.Run(context.Background(), f, "owner", "repo", comps[0], testLabels, draftUntilAssetsAttach, dryRun, discardLogger())
}

func runLocked(f *forgetest.Fake, comps []release.Component, draftUntilAssetsAttach, dryRun bool) ([]release.Result, error) {
	return release.RunLocked(
		context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, draftUntilAssetsAttach, dryRun, discardLogger(),
	)
}

func runShared(f *forgetest.Fake, comps []release.Component, draftUntilAssetsAttach, dryRun bool) ([]release.Result, error) {
	return release.RunShared(
		context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, draftUntilAssetsAttach, dryRun, discardLogger(),
	)
}

// mergedFake is a forge holding one merged, pending release pull request,
// number 21, on the release branch "release-me/main".
func mergedFake(title string) *forgetest.Fake {
	const label = "release: pending"
	pr := forge.PullRequest{Number: 21, Title: title, Labels: []string{label}, Merged: true}
	return &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}}},
		MergeCommits:  map[int]string{21: "4f2a9c1"},
		PRLabels:      map[int][]string{21: {label}},
	}
}

// releaseTarget is one created release's tag and the commit it hands the
// forge to create a missing tag on.
type releaseTarget struct {
	Tag    string
	Target string
}

func TestReleaseLeavesTheTagToThePublishWithoutForceTagCreation(t *testing.T) {
	t.Parallel()
	onPublish := release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v", TagOnPublish: true}
	apiOnPublish := []release.Component{
		{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true, TagOnPublish: true},
		{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true},
	}
	cases := []struct {
		name                   string
		run                    releaseRun
		fake                   *forgetest.Fake
		comps                  []release.Component
		draftUntilAssetsAttach bool
		dryRun                 bool
		wantTags               []string
		wantReleases           []releaseTarget
		wantMessages           []string
	}{
		{
			name: "hands a published release the merge commit to tag", run: runOwn,
			fake:         mergedFake("chore(main): release v0.2.0"),
			comps:        []release.Component{onPublish},
			wantReleases: []releaseTarget{{Tag: "v0.2.0", Target: "4f2a9c1"}},
			wantMessages: []string{"tagged and released v0.2.0"},
		},
		{
			name: "leaves the tag of a draft to its publish", run: runOwn,
			fake:  mergedFake("chore(main): release v0.2.0"),
			comps: []release.Component{onPublish}, draftUntilAssetsAttach: true,
			wantReleases: []releaseTarget{{Tag: "v0.2.0", Target: "4f2a9c1"}},
			wantMessages: []string{"drafted v0.2.0, tagged on publish"},
		},
		{
			name: "still moves the major tag", run: runOwn,
			fake: mergedFake("chore(main): release v1.4.0"),
			comps: []release.Component{{
				Name: "main", Branch: "release-me/main", TagPrefix: "v", TagOnPublish: true, MajorTag: true,
			}},
			wantTags:     []string{"v1"},
			wantReleases: []releaseTarget{{Tag: "v1.4.0", Target: "4f2a9c1"}},
			wantMessages: []string{"tagged and released v1.4.0"},
		},
		{
			name: "tags only the shared component that forces its tag", run: runShared,
			fake:  mergedFake("chore: release api v1.3.0, web v0.4.1"),
			comps: apiOnPublish, draftUntilAssetsAttach: true,
			wantTags:     []string{"web/v0.4.1"},
			wantReleases: []releaseTarget{{Tag: "api/v1.3.0", Target: "4f2a9c1"}, {Tag: "web/v0.4.1"}},
			wantMessages: []string{"drafted api/v1.3.0, tagged on publish", "tagged and released web/v0.4.1"},
		},
		{
			name: "tags only the locked component that forces its tag", run: runLocked,
			fake:  mergedFake("chore: release v0.3.0 (api, web)"),
			comps: apiOnPublish, draftUntilAssetsAttach: true,
			wantTags:     []string{"web/v0.3.0"},
			wantReleases: []releaseTarget{{Tag: "api/v0.3.0", Target: "4f2a9c1"}, {Tag: "web/v0.3.0"}},
			wantMessages: []string{"drafted api/v0.3.0, tagged on publish", "tagged and released web/v0.3.0"},
		},
		{
			name: "dry run reports the tag left to the publish", run: runOwn,
			fake:  mergedFake("chore(main): release v0.2.0"),
			comps: []release.Component{onPublish}, draftUntilAssetsAttach: true, dryRun: true,
			wantMessages: []string{"would draft v0.2.0, tagged on publish"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results, err := tc.run(tc.fake, tc.comps, tc.draftUntilAssetsAttach, tc.dryRun)
			if err != nil {
				t.Fatalf("release error = %v", err)
			}
			var messages []string
			for _, r := range results {
				messages = append(messages, r.Message)
			}
			if !reflect.DeepEqual(messages, tc.wantMessages) {
				t.Fatalf("messages = %q, want %q", messages, tc.wantMessages)
			}
			var tags []string
			for _, c := range tc.fake.CreatedTags {
				tags = append(tags, c.Tag)
			}
			if !reflect.DeepEqual(tags, tc.wantTags) {
				t.Fatalf("created tags = %q, want %q", tags, tc.wantTags)
			}
			var releases []releaseTarget
			for _, c := range tc.fake.CreatedReleases {
				releases = append(releases, releaseTarget{Tag: c.Tag, Target: c.Target})
			}
			if !reflect.DeepEqual(releases, tc.wantReleases) {
				t.Fatalf("created releases = %+v, want %+v", releases, tc.wantReleases)
			}
		})
	}
}
