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

// mergedFake is a forge holding one merged release pull request, number
// 21, on the release branch "release-me/main", carrying label.
func mergedFake(title, label string) *forgetest.Fake {
	pr := forge.PullRequest{Number: 21, Title: title, Labels: []string{label}, Merged: true}
	return &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}}},
		MergeCommits:  map[int]string{21: "4f2a9c1"},
		PRLabels:      map[int][]string{21: {label}},
	}
}

// releaseDraft is one created release's tag and whether it was a draft.
type releaseDraft struct {
	Tag   string
	Draft bool
}

func TestReleaseDraftsTheReleaseUnderDraftAndLeavesItForAHuman(t *testing.T) {
	t.Parallel()
	draftMain := release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v", Draft: true}
	apiDraft := []release.Component{
		{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true, Draft: true},
		{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true},
	}
	withDraft := func(f *forgetest.Fake, tag string) *forgetest.Fake {
		f.ReleaseDrafts = map[string]bool{tag: true}
		f.ExistingTags = map[string]bool{tag: true}
		return f
	}
	cases := []struct {
		name                   string
		run                    releaseRun
		fake                   *forgetest.Fake
		comps                  []release.Component
		draftUntilAssetsAttach bool
		dryRun                 bool
		wantReleases           []releaseDraft
		wantMessages           []string
		wantLabelSwap          bool
		wantDraftChecks        int
	}{
		{
			name: "both draft options off make no draft lookup", run: runOwn,
			fake:         mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps:        []release.Component{{Name: "main", Branch: "release-me/main", TagPrefix: "v"}},
			wantReleases: []releaseDraft{{Tag: "v0.2.0"}},
			wantMessages: []string{"tagged and released v0.2.0"}, wantLabelSwap: true,
		},
		{
			name: "drafts the release of the implicit main component", run: runOwn,
			fake:         mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps:        []release.Component{draftMain},
			wantReleases: []releaseDraft{{Tag: "v0.2.0", Draft: true}},
			wantMessages: []string{"tagged and drafted v0.2.0"}, wantLabelSwap: true, wantDraftChecks: 1,
		},
		{
			name: "drafts no second release when a rerun finds the first draft", run: runOwn,
			fake:         withDraft(mergedFake("chore(main): release v0.2.0", "release: pending"), "v0.2.0"),
			comps:        []release.Component{draftMain},
			wantMessages: []string{"already tagged v0.2.0"}, wantLabelSwap: true, wantDraftChecks: 1,
		},
		{
			name: "resumes no draft left on purpose", run: runOwn,
			fake:  withDraft(mergedFake("chore(main): release v0.2.0", "release: tagged"), "v0.2.0"),
			comps: []release.Component{draftMain},
		},
		{
			name: "resumes the draft when draft-until-assets-attach is on too", run: runOwn,
			fake:  withDraft(mergedFake("chore(main): release v0.2.0", "release: tagged"), "v0.2.0"),
			comps: []release.Component{draftMain}, draftUntilAssetsAttach: true,
			wantMessages: []string{"resuming unpublished release v0.2.0"}, wantDraftChecks: 1,
		},
		{
			name: "drafts only the locked component that asks for it", run: runLocked,
			fake:         mergedFake("chore: release v0.3.0 (api, web)", "release: pending"),
			comps:        apiDraft,
			wantReleases: []releaseDraft{{Tag: "api/v0.3.0", Draft: true}, {Tag: "web/v0.3.0"}},
			wantMessages: []string{"tagged and drafted api/v0.3.0", "tagged and released web/v0.3.0"}, wantLabelSwap: true, wantDraftChecks: 1,
		},
		{
			name: "drafts only the shared component that asks for it", run: runShared,
			fake:         mergedFake("chore: release api v1.3.0, web v0.4.1", "release: pending"),
			comps:        apiDraft,
			wantReleases: []releaseDraft{{Tag: "api/v1.3.0", Draft: true}, {Tag: "web/v0.4.1"}},
			wantMessages: []string{"tagged and drafted api/v1.3.0", "tagged and released web/v0.4.1"}, wantLabelSwap: true, wantDraftChecks: 1,
		},
		{
			name: "dry run reports the draft and writes nothing", run: runOwn,
			fake:  mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps: []release.Component{draftMain}, dryRun: true,
			wantMessages: []string{"would tag and draft v0.2.0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results, err := tc.run(tc.fake, tc.comps, tc.draftUntilAssetsAttach, tc.dryRun)
			if err != nil {
				t.Fatalf("release error = %v", err)
			}
			if got := len(tc.fake.IsReleaseDraftCalls); got != tc.wantDraftChecks {
				t.Fatalf("draft lookups = %q, want %d calls", tc.fake.IsReleaseDraftCalls, tc.wantDraftChecks)
			}
			var messages []string
			for _, r := range results {
				messages = append(messages, r.Message)
			}
			if !reflect.DeepEqual(messages, tc.wantMessages) {
				t.Fatalf("messages = %q, want %q", messages, tc.wantMessages)
			}
			var releases []releaseDraft
			for _, c := range tc.fake.CreatedReleases {
				releases = append(releases, releaseDraft{Tag: c.Tag, Draft: c.Draft})
			}
			if !reflect.DeepEqual(releases, tc.wantReleases) {
				t.Fatalf("created releases = %+v, want %+v", releases, tc.wantReleases)
			}
			if swapped := len(tc.fake.SetLabelsCalls) == 1; swapped != tc.wantLabelSwap {
				t.Fatalf("SetLabelsCalls = %+v, want a swap: %v", tc.fake.SetLabelsCalls, tc.wantLabelSwap)
			}
		})
	}
}
