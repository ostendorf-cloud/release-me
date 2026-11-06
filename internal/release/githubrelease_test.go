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

func TestReleaseNamesTheReleaseWithOrWithoutTheV(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		run       releaseRun
		fake      *forgetest.Fake
		comps     []release.Component
		wantNames []string
	}{
		{
			name: "keeps the v for the implicit main component by default", run: runOwn,
			fake:      mergedFake("chore(main): release v0.2.0"),
			comps:     []release.Component{mainComponent},
			wantNames: []string{"v0.2.0"},
		},
		{
			name: "drops the v for the implicit main component", run: runOwn,
			fake:      mergedFake("chore(main): release v0.2.0"),
			comps:     []release.Component{{Name: "main", Branch: "release-me/main", TagPrefix: "v", OmitVInReleaseName: true}},
			wantNames: []string{"0.2.0"},
		},
		{
			name: "drops the v after a configured component's name", run: runOwn,
			fake: mergedFake("chore(api): release v1.3.0-rc.1"),
			comps: []release.Component{{
				Name: "api", Branch: "release-me/main", TagPrefix: "api@v", Multi: true, OmitVInReleaseName: true,
			}},
			wantNames: []string{"api 1.3.0-rc.1"},
		},
		{
			name: "drops the v only for the shared component that asks", run: runShared,
			fake: mergedFake("chore: release api v1.3.0, web v0.4.1"),
			comps: []release.Component{
				{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true, OmitVInReleaseName: true},
				{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true},
			},
			wantNames: []string{"api 1.3.0", "web v0.4.1"},
		},
		{
			name: "drops the v for every locked component that asks", run: runLocked,
			fake: mergedFake("chore: release v0.3.0 (api, web)"),
			comps: []release.Component{
				{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true, OmitVInReleaseName: true},
				{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true, OmitVInReleaseName: true},
			},
			wantNames: []string{"api 0.3.0", "web 0.3.0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tc.run(tc.fake, tc.comps, false, false); err != nil {
				t.Fatalf("release error = %v", err)
			}
			var names []string
			for _, c := range tc.fake.CreatedReleases {
				names = append(names, c.Name)
			}
			if !reflect.DeepEqual(names, tc.wantNames) {
				t.Fatalf("release names = %q, want %q", names, tc.wantNames)
			}
		})
	}
}
