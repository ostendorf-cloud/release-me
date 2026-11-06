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
// component's own branch or a shared release pull request.
type releaseRun func(f *forgetest.Fake, comps []release.Component, draftUntilAssetsAttach, dryRun bool) ([]release.Result, error)

func runOwn(f *forgetest.Fake, comps []release.Component, draftUntilAssetsAttach, dryRun bool) ([]release.Result, error) {
	return release.Run(context.Background(), f, "owner", "repo", comps[0], testLabels, draftUntilAssetsAttach, dryRun, discardLogger())
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
