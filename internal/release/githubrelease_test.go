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
		context.Background(), f, "owner", "repo", "release-me/main", comps, nil, testLabels, draftUntilAssetsAttach, dryRun, discardLogger(),
	)
}

func runShared(f *forgetest.Fake, comps []release.Component, draftUntilAssetsAttach, dryRun bool) ([]release.Result, error) {
	return release.RunShared(
		context.Background(), f, "owner", "repo", "release-me/main", comps, nil, testLabels, draftUntilAssetsAttach, dryRun, discardLogger(),
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

// apiAndWeb are two configured components on the shared release branch,
// api skipping its GitHub release and web keeping its own.
var apiAndWeb = []release.Component{
	{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true, SkipGitHubRelease: true},
	{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true},
}

func TestReleaseSkipsTheGitHubReleaseButStillTagsAndSwapsTheLabel(t *testing.T) {
	t.Parallel()
	skipMain := release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v", SkipGitHubRelease: true}
	alreadyTagged := mergedFake("chore(main): release v0.2.0", "release: pending")
	alreadyTagged.ExistingTags = map[string]bool{"v0.2.0": true}
	cases := []struct {
		name                   string
		run                    releaseRun
		fake                   *forgetest.Fake
		comps                  []release.Component
		draftUntilAssetsAttach bool
		dryRun                 bool
		wantTags               []string
		wantReleases           []string
		wantMessages           []string
		wantLabelSwap          bool
	}{
		{
			name: "tags the implicit main component without a release", run: runOwn,
			fake:  mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps: []release.Component{skipMain}, wantTags: []string{"v0.2.0"},
			wantMessages: []string{"tagged v0.2.0, GitHub release skipped"}, wantLabelSwap: true,
		},
		{
			name: "reports an existing tag before the skipped release", run: runOwn,
			fake: alreadyTagged, comps: []release.Component{skipMain},
			wantMessages: []string{"already tagged v0.2.0"}, wantLabelSwap: true,
		},
		{
			name: "drafts nothing with github-release-draft-until-assets-attach on", run: runOwn,
			fake:  mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps: []release.Component{skipMain}, draftUntilAssetsAttach: true, wantTags: []string{"v0.2.0"},
			wantMessages: []string{"tagged v0.2.0, GitHub release skipped"}, wantLabelSwap: true,
		},
		{
			name: "still moves the major tag", run: runOwn,
			fake: mergedFake("chore(main): release v1.4.0", "release: pending"),
			comps: []release.Component{{
				Name: "main", Branch: "release-me/main", TagPrefix: "v", SkipGitHubRelease: true, MajorTag: true,
			}},
			wantTags:      []string{"v1.4.0", "v1"},
			wantMessages:  []string{"tagged v1.4.0, GitHub release skipped"},
			wantLabelSwap: true,
		},
		{
			name: "releases only the locked component that keeps its release", run: runLocked,
			fake:  mergedFake("chore: release v0.3.0 (api, web)", "release: pending"),
			comps: apiAndWeb, wantTags: []string{"api/v0.3.0", "web/v0.3.0"}, wantReleases: []string{"web/v0.3.0"},
			wantMessages:  []string{"tagged api/v0.3.0, GitHub release skipped", "tagged and released web/v0.3.0"},
			wantLabelSwap: true,
		},
		{
			name: "releases only the shared component that keeps its release", run: runShared,
			fake:  mergedFake("chore: release api v1.3.0, web v0.4.1", "release: pending"),
			comps: apiAndWeb, wantTags: []string{"api/v1.3.0", "web/v0.4.1"}, wantReleases: []string{"web/v0.4.1"},
			wantMessages:  []string{"tagged api/v1.3.0, GitHub release skipped", "tagged and released web/v0.4.1"},
			wantLabelSwap: true,
		},
		{
			name: "dry run reports the skipped release and writes nothing", run: runOwn,
			fake:  mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps: []release.Component{skipMain}, dryRun: true,
			wantMessages: []string{"would tag v0.2.0, GitHub release skipped"},
		},
		{
			name: "resumes nothing for a tagged pull request without a release", run: runOwn,
			fake:  mergedFake("chore(main): release v0.2.0", "release: tagged"),
			comps: []release.Component{skipMain}, draftUntilAssetsAttach: true,
		},
		{
			name: "resumes nothing for a tagged shared pull request without a release", run: runShared,
			fake:  mergedFake("chore: release api v1.3.0", "release: tagged"),
			comps: apiAndWeb, draftUntilAssetsAttach: true,
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
			var releases []string
			for _, c := range tc.fake.CreatedReleases {
				releases = append(releases, c.Tag)
			}
			if !reflect.DeepEqual(releases, tc.wantReleases) {
				t.Fatalf("created releases = %q, want %q", releases, tc.wantReleases)
			}
			swapped := len(tc.fake.SetLabelsCalls) == 1 && reflect.DeepEqual(tc.fake.SetLabelsCalls[0].Labels, []string{"release: tagged"})
			if swapped != tc.wantLabelSwap || (!tc.wantLabelSwap && len(tc.fake.SetLabelsCalls) != 0) {
				t.Fatalf("SetLabelsCalls = %+v, want a swap to release: tagged: %v", tc.fake.SetLabelsCalls, tc.wantLabelSwap)
			}
		})
	}
}

// releaseOutputs is what a result reports for the action's outputs.
type releaseOutputs struct {
	Tag, SHA, Body, HTMLURL, UploadURL string
	Draft                              bool
}

func TestReleaseReportsTheCommitNotesAndAddressesOfEveryRelease(t *testing.T) {
	t.Parallel()
	notes := "## v0.2.0 (2026-01-02)\n\n### Features\n\n- add the thing"
	withBody := func(f *forgetest.Fake, body string) *forgetest.Fake {
		f.ListResponses[0].PullRequests[0].Body = body
		return f
	}
	mainBody := "Release v0.2.0\n\n<details>\n<summary>Changelog</summary>\n\n" + notes + "\n\n</details>\n"
	sharedNotes := "<details>\n<summary>Changelog</summary>\n\n" +
		"#### api\n\n## v1.3.0 (2026-03-14)\n\n- add the export endpoint\n\n" +
		"#### web\n\n## v0.4.1 (2026-03-14)\n\n- keep the session on reload\n\n</details>\n"
	created := releaseOutputs{
		Tag: "v0.2.0", SHA: "4f2a9c1", Body: notes,
		HTMLURL:   "https://github.com/owner/repo/releases/tag/v0.2.0",
		UploadURL: "https://uploads.github.com/repos/owner/repo/releases/9001/assets{?name,label}",
	}
	drafted := created
	drafted.Draft = true
	type releaseOutputCase struct {
		name                   string
		run                    releaseRun
		fake                   *forgetest.Fake
		comps                  []release.Component
		draftUntilAssetsAttach bool
		want                   []releaseOutputs
	}
	cases := []releaseOutputCase{
		{
			name: "reports the release it created", run: runOwn,
			fake:  withBody(mergedFake("chore(main): release v0.2.0", "release: pending"), mainBody),
			comps: []release.Component{mainComponent},
			want:  []releaseOutputs{created},
		},
		{
			name: "reads back the release an earlier run created", run: runOwn,
			fake: func() *forgetest.Fake {
				f := withBody(mergedFake("chore(main): release v0.2.0", "release: pending"), mainBody)
				f.ExistingTags = map[string]bool{"v0.2.0": true}
				f.ExistingReleases = map[string]bool{"v0.2.0": true}
				return f
			}(),
			comps: []release.Component{mainComponent},
			want:  []releaseOutputs{created},
		},
		{
			name: "reports the draft it creates", run: runOwn,
			fake:  withBody(mergedFake("chore(main): release v0.2.0", "release: pending"), mainBody),
			comps: []release.Component{mainComponent}, draftUntilAssetsAttach: true,
			want: []releaseOutputs{drafted},
		},
		{
			name: "reports the draft left before an earlier run swapped the label", run: runOwn,
			fake: func() *forgetest.Fake {
				f := withBody(mergedFake("chore(main): release v0.2.0", "release: pending"), mainBody)
				f.ReleaseDrafts = map[string]bool{"v0.2.0": true}
				return f
			}(),
			comps: []release.Component{mainComponent}, draftUntilAssetsAttach: true,
			want: []releaseOutputs{drafted},
		},
		{
			name: "keeps addresses empty when an existing release cannot be found", run: runOwn,
			fake: func() *forgetest.Fake {
				f := withBody(mergedFake("chore(main): release v0.2.0", "release: pending"), mainBody)
				f.CreateReleaseErrs = map[string]error{"v0.2.0": forge.ErrReleaseExists}
				return f
			}(),
			comps: []release.Component{mainComponent},
			want:  []releaseOutputs{{Tag: "v0.2.0", SHA: "4f2a9c1", Body: notes}},
		},
		{
			name: "reports the draft it resumes", run: runOwn,
			fake: func() *forgetest.Fake {
				f := withBody(mergedFake("chore(main): release v0.2.0", "release: tagged"), mainBody)
				f.ReleaseDrafts = map[string]bool{"v0.2.0": true}
				return f
			}(),
			comps: []release.Component{mainComponent}, draftUntilAssetsAttach: true,
			want: []releaseOutputs{drafted},
		},
		{
			name: "reports every shared component's own release", run: runShared,
			fake: withBody(mergedFake("chore: release api v1.3.0, web v0.4.1", "release: pending"), sharedNotes),
			comps: []release.Component{
				{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true},
				{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true},
			},
			want: []releaseOutputs{
				{
					Tag: "api/v1.3.0", SHA: "4f2a9c1", Body: "## v1.3.0 (2026-03-14)\n\n- add the export endpoint",
					HTMLURL:   "https://github.com/owner/repo/releases/tag/api/v1.3.0",
					UploadURL: "https://uploads.github.com/repos/owner/repo/releases/9001/assets{?name,label}",
				},
				{
					Tag: "web/v0.4.1", SHA: "4f2a9c1", Body: "## v0.4.1 (2026-03-14)\n\n- keep the session on reload",
					HTMLURL:   "https://github.com/owner/repo/releases/tag/web/v0.4.1",
					UploadURL: "https://uploads.github.com/repos/owner/repo/releases/9002/assets{?name,label}",
				},
			},
		},
	}
	shared := cases[len(cases)-1]
	for _, resume := range []bool{false, true} {
		label, name := "release: pending", "reports drafts created for every shared component"
		if resume {
			label, name = "release: tagged", "reports the shared draft it resumes and skips published components"
		}
		f := withBody(mergedFake("chore: release api v1.3.0, web v0.4.1", label), sharedNotes)
		want := append([]releaseOutputs(nil), shared.want...)
		for i := range want {
			want[i].Draft = true
		}
		if resume {
			f.ReleaseDrafts = map[string]bool{"api/v1.3.0": true, "web/v0.4.1": false}
			want = want[:1]
		}
		cases = append(cases, releaseOutputCase{name: name, run: runShared, fake: f, comps: shared.comps, draftUntilAssetsAttach: true, want: want})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results, err := tc.run(tc.fake, tc.comps, tc.draftUntilAssetsAttach, false)
			if err != nil {
				t.Fatalf("release error = %v", err)
			}
			var got []releaseOutputs
			for _, r := range results {
				got = append(got, releaseOutputs{Tag: r.Tag, SHA: r.SHA, Body: r.Body, HTMLURL: r.HTMLURL, UploadURL: r.UploadURL, Draft: r.Draft})
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("results = %+v, want %+v", got, tc.want)
			}
		})
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
			fake:         mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps:        []release.Component{onPublish},
			wantReleases: []releaseTarget{{Tag: "v0.2.0", Target: "4f2a9c1"}},
			wantMessages: []string{"tagged and released v0.2.0"},
		},
		{
			name: "leaves the tag of a draft to its publish", run: runOwn,
			fake:  mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps: []release.Component{onPublish}, draftUntilAssetsAttach: true,
			wantReleases: []releaseTarget{{Tag: "v0.2.0", Target: "4f2a9c1"}},
			wantMessages: []string{"drafted v0.2.0, tagged on publish"},
		},
		{
			name: "still moves the major tag", run: runOwn,
			fake: mergedFake("chore(main): release v1.4.0", "release: pending"),
			comps: []release.Component{{
				Name: "main", Branch: "release-me/main", TagPrefix: "v", TagOnPublish: true, MajorTag: true,
			}},
			wantTags:     []string{"v1"},
			wantReleases: []releaseTarget{{Tag: "v1.4.0", Target: "4f2a9c1"}},
			wantMessages: []string{"tagged and released v1.4.0"},
		},
		{
			name: "tags only the shared component that forces its tag", run: runShared,
			fake:  mergedFake("chore: release api v1.3.0, web v0.4.1", "release: pending"),
			comps: apiOnPublish, draftUntilAssetsAttach: true,
			wantTags:     []string{"web/v0.4.1"},
			wantReleases: []releaseTarget{{Tag: "api/v1.3.0", Target: "4f2a9c1"}, {Tag: "web/v0.4.1"}},
			wantMessages: []string{"drafted api/v1.3.0, tagged on publish", "tagged and released web/v0.4.1"},
		},
		{
			name: "tags only the locked component that forces its tag", run: runLocked,
			fake:  mergedFake("chore: release v0.3.0 (api, web)", "release: pending"),
			comps: apiOnPublish, draftUntilAssetsAttach: true,
			wantTags:     []string{"web/v0.3.0"},
			wantReleases: []releaseTarget{{Tag: "api/v0.3.0", Target: "4f2a9c1"}, {Tag: "web/v0.3.0"}},
			wantMessages: []string{"drafted api/v0.3.0, tagged on publish", "tagged and released web/v0.3.0"},
		},
		{
			name: "dry run reports the tag left to the publish", run: runOwn,
			fake:  mergedFake("chore(main): release v0.2.0", "release: pending"),
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
			// "github-release-draft" never resumes, whatever
			// github-release-draft-until-assets-attach is set to: the draft
			// waits for a human, so resuming it would hand the publish step
			// a release nobody approved.
			name: "resumes no draft left on purpose with github-release-draft-until-assets-attach on too", run: runOwn,
			fake:  withDraft(mergedFake("chore(main): release v0.2.0", "release: tagged"), "v0.2.0"),
			comps: []release.Component{draftMain}, draftUntilAssetsAttach: true,
		},
		{
			name: "resumes only the shared component that did not draft on purpose", run: runShared,
			fake: func() *forgetest.Fake {
				f := mergedFake("chore: release api v1.3.0, web v0.4.1", "release: tagged")
				f.ReleaseDrafts = map[string]bool{"api/v1.3.0": true, "web/v0.4.1": true}
				f.ExistingTags = map[string]bool{"api/v1.3.0": true, "web/v0.4.1": true}
				return f
			}(),
			comps: apiDraft, draftUntilAssetsAttach: true,
			wantMessages: []string{"resuming unpublished release web/v0.4.1"}, wantDraftChecks: 1,
		},
		{
			name: "resumes only the locked component that did not draft on purpose", run: runLocked,
			fake: func() *forgetest.Fake {
				f := mergedFake("chore: release v0.3.0 (api, web)", "release: tagged")
				f.ReleaseDrafts = map[string]bool{"api/v0.3.0": true, "web/v0.3.0": true}
				f.ExistingTags = map[string]bool{"api/v0.3.0": true, "web/v0.3.0": true}
				return f
			}(),
			comps: apiDraft, draftUntilAssetsAttach: true,
			wantMessages: []string{"resuming unpublished release web/v0.3.0"}, wantDraftChecks: 1,
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

// TestReleaseMovesTheMajorTagOntoADraftedRelease pins the documented
// limitation of "major-tag" beside a drafted release: the major tag moves
// to the merge commit when the release is created, not when a human
// publishes it. With "force-tag-creation" on, the default, the version tag
// is on that same commit already, so the two agree. With it off the version
// tag does not exist yet and the major tag is ahead of the newest published
// version until the draft is published. Deferring the move instead would
// never move the tag at all, since nothing runs after that publish; see
// applyMajorTag and docs/configuration.md's major-tag key.
func TestReleaseMovesTheMajorTagOntoADraftedRelease(t *testing.T) {
	t.Parallel()
	component := func(tagOnPublish, draft bool) release.Component {
		return release.Component{
			Name: "main", Branch: "release-me/main", TagPrefix: "v",
			MajorTag: true, TagOnPublish: tagOnPublish, Draft: draft,
		}
	}
	cases := []struct {
		name                   string
		comp                   release.Component
		draftUntilAssetsAttach bool
		existingMajorTag       bool
		wantCreated            []string
		wantMoved              []string
		wantMajorTagMessage    string
	}{
		{
			name:        "creates the major tag beside the version tag of a deliberate draft",
			comp:        component(false, true),
			wantCreated: []string{"v1.4.0", "v1"}, wantMajorTagMessage: "created major tag v1",
		},
		{
			name: "moves the existing major tag onto a deliberate draft",
			comp: component(false, true), existingMajorTag: true,
			wantCreated: []string{"v1.4.0"}, wantMoved: []string{"v1"}, wantMajorTagMessage: "moved major tag v1",
		},
		{
			name: "moves the existing major tag onto a draft from github-release-draft-until-assets-attach",
			comp: component(false, false), draftUntilAssetsAttach: true, existingMajorTag: true,
			wantCreated: []string{"v1.4.0"}, wantMoved: []string{"v1"}, wantMajorTagMessage: "moved major tag v1",
		},
		{
			name: "moves the major tag of a drafted release whose own tag waits for the publish",
			comp: component(true, true), existingMajorTag: true,
			wantMoved: []string{"v1"}, wantMajorTagMessage: "moved major tag v1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := mergedFake("chore(main): release v1.4.0", "release: pending")
			if tc.existingMajorTag {
				f.ExistingTags = map[string]bool{"v1": true}
			}

			results, err := runOwn(f, []release.Component{tc.comp}, tc.draftUntilAssetsAttach, false)
			if err != nil {
				t.Fatalf("release error = %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("results = %+v, want exactly one", results)
			}
			if got := results[0].MajorTagMessage; got != tc.wantMajorTagMessage {
				t.Fatalf("major tag message = %q, want %q", got, tc.wantMajorTagMessage)
			}
			if got := results[0].MajorTag; got != "v1" {
				t.Fatalf("major tag = %q, want v1", got)
			}
			var created []string
			for _, c := range f.CreatedTags {
				created = append(created, c.Tag)
			}
			if !reflect.DeepEqual(created, tc.wantCreated) {
				t.Fatalf("created tags = %q, want %q", created, tc.wantCreated)
			}
			var moved []string
			for _, m := range f.MovedTags {
				moved = append(moved, m.Tag)
				if m.SHA != "4f2a9c1" {
					t.Fatalf("major tag moved onto %q, want the merge commit", m.SHA)
				}
			}
			if !reflect.DeepEqual(moved, tc.wantMoved) {
				t.Fatalf("moved tags = %q, want %q", moved, tc.wantMoved)
			}
			if len(f.CreatedReleases) != 1 || !f.CreatedReleases[0].Draft {
				t.Fatalf("created releases = %+v, want exactly one draft", f.CreatedReleases)
			}
		})
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
			fake:      mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps:     []release.Component{mainComponent},
			wantNames: []string{"v0.2.0"},
		},
		{
			name: "drops the v for the implicit main component", run: runOwn,
			fake:      mergedFake("chore(main): release v0.2.0", "release: pending"),
			comps:     []release.Component{{Name: "main", Branch: "release-me/main", TagPrefix: "v", OmitVInReleaseName: true}},
			wantNames: []string{"0.2.0"},
		},
		{
			name: "drops the v after a configured component's name", run: runOwn,
			fake: mergedFake("chore(api): release v1.3.0-rc.1", "release: pending"),
			comps: []release.Component{{
				Name: "api", Branch: "release-me/main", TagPrefix: "api@v", Multi: true, OmitVInReleaseName: true,
			}},
			wantNames: []string{"api 1.3.0-rc.1"},
		},
		{
			name: "drops the v only for the shared component that asks", run: runShared,
			fake: mergedFake("chore: release api v1.3.0, web v0.4.1", "release: pending"),
			comps: []release.Component{
				{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true, OmitVInReleaseName: true},
				{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true},
			},
			wantNames: []string{"api 1.3.0", "web v0.4.1"},
		},
		{
			name: "drops the v for every locked component that asks", run: runLocked,
			fake: mergedFake("chore: release v0.3.0 (api, web)", "release: pending"),
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
