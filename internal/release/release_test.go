// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package release_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/release"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestVersionFromTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                   string
		title, wantedComponent string
		want                   string
		wantOK                 bool
	}{
		{name: "reads the version", title: "chore(main): release v1.2.3", wantedComponent: "main", want: "1.2.3", wantOK: true},
		{name: "rejects an unrelated title", title: "feat: add the next command", wantedComponent: "main"},
		{name: "rejects a title without the v prefix", title: "chore(main): release 1.2.3", wantedComponent: "main"},
		{
			name: "reads a named component's version", title: "chore(frontend): release v0.4.0",
			wantedComponent: "frontend", want: "0.4.0", wantOK: true,
		},
		{
			name: "rejects a title naming a different component", title: "chore(backend): release v0.4.0",
			wantedComponent: "frontend",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := release.VersionFromTitle(tc.title, tc.wantedComponent)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("VersionFromTitle(%q, %q) = (%q, %v), want (%q, %v)", tc.title, tc.wantedComponent, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// mainComponent is the implicit component a repository without
// "components" carries, the same shape the tests before components existed
// exercised.
var mainComponent = release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v"}

// testLabels are the labels release.Run reads and writes in a test that
// does not exercise a pre-release.
var testLabels = release.Labels{
	Pending: "release: pending", Tagged: "release: tagged",
	Alpha: "release: alpha", Beta: "release: beta", Rc: "release: rc",
}

func TestRunTagsReleasesAndSwapsTheLabel(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{
		Number: 9, Title: "chore(main): release v0.2.0",
		Body:   "Release v0.2.0\n\n<details>\n<summary>Changelog</summary>\n\n## v0.2.0 (2026-01-02)\n\n### Features\n\n- add the thing\n\n</details>\n",
		Labels: []string{"release: pending"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Tag != "v0.2.0" || results[0].Skipped {
		t.Fatalf("Run() = %+v", results)
	}

	if len(f.CreatedTags) != 1 || f.CreatedTags[0] != (forgetest.CreateTagCall{Owner: "owner", Repo: "repo", Tag: "v0.2.0", SHA: "deadbeef"}) {
		t.Fatalf("CreatedTags = %+v", f.CreatedTags)
	}
	if len(f.CreatedReleases) != 1 {
		t.Fatalf("CreatedReleases = %+v", f.CreatedReleases)
	}
	wantBody := "## v0.2.0 (2026-01-02)\n\n### Features\n\n- add the thing"
	if f.CreatedReleases[0].Body != wantBody {
		t.Fatalf("release body = %q, want %q", f.CreatedReleases[0].Body, wantBody)
	}
	// draftUntilAssetsAttach is off, the default: the release publishes
	// immediately, exactly as release-me did before that key existed.
	if f.CreatedReleases[0].Draft {
		t.Fatalf("CreatedReleases = %+v, want Draft false", f.CreatedReleases)
	}
	if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, []string{"release: tagged"}) {
		t.Fatalf("SetLabelsCalls = %+v", f.SetLabelsCalls)
	}
}

// TestRunDraftsTheReleaseWhenDraftUntilAssetsAttachIsOn covers the other
// half of the same choice TestRunTagsReleasesAndSwapsTheLabel already
// covers for the off, default case: draftUntilAssetsAttach true drafts
// the release instead of publishing it immediately, the shape a
// repository with GitHub's immutable releases setting on opts into.
func TestRunDraftsTheReleaseWhenDraftUntilAssetsAttachIsOn(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, true, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Tag != "v0.2.0" {
		t.Fatalf("Run() = %+v", results)
	}
	if len(f.CreatedReleases) != 1 || !f.CreatedReleases[0].Draft {
		t.Fatalf("CreatedReleases = %+v, want Draft true", f.CreatedReleases)
	}
}

func TestRunStillReleasesAndSwapsTheLabelWhenTheTagAlreadyExists(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		ExistingTags: map[string]bool{"v0.2.0": true},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || !results[0].Skipped || results[0].Message != "already tagged v0.2.0" {
		t.Fatalf("Run() = %+v, want an already-tagged result", results)
	}
	// The tag already existing means an earlier run got partway through;
	// this run still finishes the job instead of leaving it stuck pending.
	if len(f.CreatedReleases) != 1 {
		t.Fatalf("CreatedReleases = %+v, want the release created", f.CreatedReleases)
	}
	if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, []string{"release: tagged"}) {
		t.Fatalf("SetLabelsCalls = %+v, want the label swapped", f.SetLabelsCalls)
	}
}

func TestRunToleratesAReleaseThatAlreadyExistsTooWhenTheTagDoes(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits:     map[int]string{9: "deadbeef"},
		ExistingTags:     map[string]bool{"v0.2.0": true},
		ExistingReleases: map[string]bool{"v0.2.0": true},
		PRLabels:         map[int][]string{9: {"release: pending"}},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || !results[0].Skipped {
		t.Fatalf("Run() = %+v, want an already-tagged result", results)
	}
	if len(f.SetLabelsCalls) != 1 {
		t.Fatalf("SetLabelsCalls = %+v, want the label swapped", f.SetLabelsCalls)
	}
}

// TestRunFailsLoudlyWhenCreateReleaseReturnsANonRecoverableError proves that
// a CreateRelease error that is not forge.ErrReleaseExists, the real forge
// returns it for a tag name that was ever used by a now-deleted immutable
// release, stops Run before it swaps the label or moves the major tag: the
// release was never created, so neither may happen.
func TestRunFailsLoudlyWhenCreateReleaseReturnsANonRecoverableError(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: pending"}, Merged: true}
	releaseErr := errors.New("tag_name was used by an immutable release")
	comp := release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v", MajorTag: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits:      map[int]string{9: "deadbeef"},
		PRLabels:          map[int][]string{9: {"release: pending"}},
		CreateReleaseErrs: map[string]error{"v0.2.0": releaseErr},
	}

	_, err := release.Run(context.Background(), f, "owner", "repo", comp, testLabels, false, false, discardLogger())
	if !errors.Is(err, releaseErr) {
		t.Fatalf("Run() error = %v, want it to wrap %v", err, releaseErr)
	}
	if len(f.SetLabelsCalls) != 0 {
		t.Fatalf("SetLabelsCalls = %+v, want no label swap", f.SetLabelsCalls)
	}
	if len(f.MovedTags) != 0 {
		t.Fatalf("MovedTags = %+v, want no major tag move", f.MovedTags)
	}
	if len(f.CreatedTags) != 1 || f.CreatedTags[0].Tag != "v0.2.0" {
		t.Fatalf("CreatedTags = %+v, want only the component tag, no major tag", f.CreatedTags)
	}
}

// TestRunDoesNotDraftASecondReleaseWhenTheLabelSwapFailedAfterTheFirstDraft
// reproduces the race one step earlier than
// TestRunResumesAnAlreadyTaggedPullRequestWhoseReleaseIsStillADraft: a
// pull request that stays labeled Pending because the label swap failed
// after CreateRelease already drafted the release. GitHub's own
// duplicate-tag_name check does not see a draft, so simply re-entering
// releaseOne on the next run and tolerating forge.ErrReleaseExists is not
// enough; Run must notice the existing draft itself and skip straight to
// the label swap instead of asking CreateRelease to draft a second,
// orphaned release at the same tag.
func TestRunDoesNotDraftASecondReleaseWhenTheLabelSwapFailedAfterTheFirstDraft(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: pending"}, Merged: true}
	wantErr := errors.New("forge unavailable")
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
		SetLabelsErr: wantErr,
	}

	// First run: CreateRelease drafts the release, but the label swap that
	// was meant to follow fails, so the pull request stays Pending.
	_, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, true, false, discardLogger())
	if !errors.Is(err, wantErr) {
		t.Fatalf("first Run() error = %v, want it to wrap %v", err, wantErr)
	}
	if len(f.CreatedReleases) != 1 {
		t.Fatalf("CreatedReleases = %+v, want exactly one draft after the first run", f.CreatedReleases)
	}
	if got := f.PRLabels[9]; !reflect.DeepEqual(got, []string{"release: pending"}) {
		t.Fatalf("PRLabels[9] = %v, want the pull request still pending after the failed swap", got)
	}

	// Second run: the pull request is still Pending, so Run re-enters
	// releaseOne for it. SetLabels now succeeds.
	f.SetLabelsErr = nil
	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, true, false, discardLogger())
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Tag != "v0.2.0" {
		t.Fatalf("second Run() = %+v", results)
	}
	if len(f.CreatedReleases) != 1 {
		t.Fatalf("CreatedReleases = %+v, want still exactly one, no second orphaned draft", f.CreatedReleases)
	}
	if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, []string{"release: tagged"}) {
		t.Fatalf("SetLabelsCalls = %+v, want one successful swap to release: tagged", f.SetLabelsCalls)
	}
}

// TestRunLockedDoesNotDraftASecondReleaseWhenTheLabelSwapFailedAfterTheFirstDraft
// mirrors TestRunDoesNotDraftASecondReleaseWhenTheLabelSwapFailedAfterTheFirstDraft
// for a locked family, where the label swaps once for every component
// together.
func TestRunLockedDoesNotDraftASecondReleaseWhenTheLabelSwapFailedAfterTheFirstDraft(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore: release v0.3.0 (a, b)", Labels: []string{"release: pending"}, Merged: true}
	wantErr := errors.New("forge unavailable")
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
		SetLabelsErr: wantErr,
	}
	comps := []release.Component{
		{Name: "a", Branch: "release-me/main", TagPrefix: "a/v", Multi: true},
		{Name: "b", Branch: "release-me/main", TagPrefix: "b/v", Multi: true},
	}

	_, err := release.RunLocked(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, true, false, discardLogger())
	if !errors.Is(err, wantErr) {
		t.Fatalf("first RunLocked() error = %v, want it to wrap %v", err, wantErr)
	}
	if len(f.CreatedReleases) != 2 {
		t.Fatalf("CreatedReleases = %+v, want exactly two drafts after the first run", f.CreatedReleases)
	}

	f.SetLabelsErr = nil
	results, err := release.RunLocked(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, true, false, discardLogger())
	if err != nil {
		t.Fatalf("second RunLocked() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("second RunLocked() = %+v, want two results", results)
	}
	if len(f.CreatedReleases) != 2 {
		t.Fatalf("CreatedReleases = %+v, want still exactly two, no second orphaned drafts", f.CreatedReleases)
	}
	if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, []string{"release: tagged"}) {
		t.Fatalf("SetLabelsCalls = %+v, want one successful swap to release: tagged", f.SetLabelsCalls)
	}
}

// TestRunResumesAnAlreadyTaggedPullRequestWhoseReleaseIsStillADraft
// reproduces the orphaned-draft hole left by the Tagged label swapping in
// immediately after CreateRelease, before any asset attaches or the
// release publishes: a pull request already carrying Tagged, exactly what
// an earlier, successful releaseOne call leaves behind, still has its
// GitHub release sitting as a draft, because the asset attach or publish
// step that was meant to run next, in the same or a later workflow run,
// never finished. Run must find it anyway and resume it, instead of only
// ever looking at pull requests still labeled Pending.
func TestRunResumesAnAlreadyTaggedPullRequestWhoseReleaseIsStillADraft(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: tagged"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		ReleaseDrafts: map[string]bool{"v0.2.0": true},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, true, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Tag != "v0.2.0" || results[0].Message != "resuming unpublished release v0.2.0" {
		t.Fatalf("Run() = %+v, want one resumed result for the still-draft release", results)
	}
	// Resuming writes nothing: the tag and the release already exist, from
	// the run that tagged this pull request and swapped its label.
	// Calling CreateTag or CreateRelease again would risk a second,
	// duplicate draft for the same tag instead of finishing the first.
	if len(f.CreatedTags) != 0 || len(f.CreatedReleases) != 0 || len(f.SetLabelsCalls) != 0 {
		t.Fatalf("resume wrote something: tags=%+v releases=%+v labels=%+v", f.CreatedTags, f.CreatedReleases, f.SetLabelsCalls)
	}
}

func TestEveryResultCarriesTheHeadCommitOfThePullRequestItTagged(t *testing.T) {
	t.Parallel()
	const head = "4f2c9a1e8b7d6c5f4e3d2c1b0a9f8e7d6c5b4a39"
	comps := []release.Component{
		{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true},
		{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true},
	}
	cases := []struct {
		name   string
		title  string
		labels []string
		drafts map[string]bool
		dryRun bool
		run    func(f *forgetest.Fake, dryRun bool) ([]release.Result, error)
		want   []string
	}{
		{
			name: "per component", title: "chore(main): release v0.2.0", labels: []string{"release: pending"},
			run: func(f *forgetest.Fake, dryRun bool) ([]release.Result, error) {
				return release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, dryRun, discardLogger())
			},
			want: []string{head},
		},
		{
			name: "per component, dry run", title: "chore(main): release v0.2.0", labels: []string{"release: pending"}, dryRun: true,
			run: func(f *forgetest.Fake, dryRun bool) ([]release.Result, error) {
				return release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, dryRun, discardLogger())
			},
			want: []string{head},
		},
		{
			name: "locked family", title: "chore: release v1.4.0 (api, web)", labels: []string{"release: pending"},
			run: func(f *forgetest.Fake, dryRun bool) ([]release.Result, error) {
				return release.RunLocked(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, false, dryRun, discardLogger())
			},
			want: []string{head, head},
		},
		{
			name: "shared release pull request", title: "chore: release api v1.4.0, web v0.3.1", labels: []string{"release: pending"},
			run: func(f *forgetest.Fake, dryRun bool) ([]release.Result, error) {
				return release.RunShared(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, false, dryRun, discardLogger())
			},
			want: []string{head, head},
		},
		{
			name: "a resumed draft an earlier run tagged carries none", title: "chore(main): release v0.2.0",
			labels: []string{"release: tagged"}, drafts: map[string]bool{"v0.2.0": true},
			run: func(f *forgetest.Fake, dryRun bool) ([]release.Result, error) {
				return release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, true, dryRun, discardLogger())
			},
			want: []string{""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pr := forge.PullRequest{Number: 9, Title: tc.title, Labels: tc.labels, Merged: true, HeadSHA: head}
			f := &forgetest.Fake{
				ListResponses: []forgetest.ListResponse{
					{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
				},
				MergeCommits:  map[int]string{9: "deadbeef"},
				PRLabels:      map[int][]string{9: tc.labels},
				ReleaseDrafts: tc.drafts,
			}

			results, err := tc.run(f, tc.dryRun)
			if err != nil {
				t.Fatalf("run error = %v", err)
			}
			var got []string
			for _, r := range results {
				got = append(got, r.HeadSHA)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("HeadSHA per result = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunDoesNotResumeAnAlreadyTaggedPullRequestOnceItsReleaseIsPublished(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: tagged"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		ReleaseDrafts: map[string]bool{"v0.2.0": false},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, true, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Run() = %+v, want no results once the release is already published", results)
	}
}

func TestRunIgnoresAPullRequestWithoutThePendingLabel(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"priority: high"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Run() = %+v, want no results", results)
	}
}

// TestRunDoesNotResumeATaggedPullRequestWhenDraftUntilAssetsAttachIsOff
// proves the resumeOne scan itself is gated on draftUntilAssetsAttach,
// not merely that it happens to find nothing: the fake forge's
// ReleaseDrafts is primed with an actual, outstanding draft for this
// pull request's tag, so if the "!draftUntilAssetsAttach" guard in Run
// were ever deleted, resumeOne would find that draft and produce a
// result, and this test would then correctly fail. Priming
// ReleaseDrafts is what tells this test apart from one where resumeOne
// runs and legitimately finds nothing, which forge.ErrReleaseNotFound
// would also produce with an empty ReleaseDrafts map.
func TestRunDoesNotResumeATaggedPullRequestWhenDraftUntilAssetsAttachIsOff(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: tagged"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		ReleaseDrafts: map[string]bool{"v0.2.0": true},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Run() = %+v, want no results: resuming is off", results)
	}
}

// TestRunLockedDoesNotResumeATaggedPullRequestWhenDraftUntilAssetsAttachIsOff
// mirrors TestRunDoesNotResumeATaggedPullRequestWhenDraftUntilAssetsAttachIsOff
// for a locked family, for the same reason
// TestRunLockedResumesOnlyTheComponentWhoseReleaseIsStillADraft mirrors
// TestRunResumesAnAlreadyTaggedPullRequestWhoseReleaseIsStillADraft: this
// file mirrors resume's own coverage across both Run paths.
func TestRunLockedDoesNotResumeATaggedPullRequestWhenDraftUntilAssetsAttachIsOff(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore: release v0.3.0 (a, b)", Labels: []string{"release: tagged"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		ReleaseDrafts: map[string]bool{"a/v0.3.0": true, "b/v0.3.0": true},
	}
	comps := []release.Component{
		{Name: "a", Branch: "release-me/main", TagPrefix: "a/v", Multi: true},
		{Name: "b", Branch: "release-me/main", TagPrefix: "b/v", Multi: true},
	}

	results, err := release.RunLocked(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("RunLocked() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("RunLocked() = %+v, want no results: resuming is off", results)
	}
}

func TestRunIgnoresAnUnmergedPullRequest(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: pending"}, Merged: false}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Run() = %+v, want no results", results)
	}
}

func TestRunTagsAndNamesTheReleaseForAConfiguredComponent(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{
		Number: 9, Title: "chore(frontend): release v0.4.0",
		Body:   "Release v0.4.0\n\n<details>\n<summary>Changelog</summary>\n\n## v0.4.0 (2026-01-02)\n\n### Features\n\n- add the thing\n\n</details>\n",
		Labels: []string{"release: pending"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/frontend", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	comp := release.Component{Name: "frontend", Branch: "release-me/frontend", TagPrefix: "frontend/v", Multi: true}

	results, err := release.Run(context.Background(), f, "owner", "repo", comp, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Tag != "frontend/v0.4.0" {
		t.Fatalf("Run() = %+v", results)
	}
	if len(f.CreatedTags) != 1 || f.CreatedTags[0].Tag != "frontend/v0.4.0" {
		t.Fatalf("CreatedTags = %+v", f.CreatedTags)
	}
	if len(f.CreatedReleases) != 1 || f.CreatedReleases[0].Name != "frontend v0.4.0" {
		t.Fatalf("CreatedReleases = %+v, want the name %q", f.CreatedReleases, "frontend v0.4.0")
	}
}

func TestRunIgnoresAPullRequestTitledForADifferentComponent(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(backend): release v0.4.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/frontend", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
	}
	comp := release.Component{Name: "frontend", Branch: "release-me/frontend", TagPrefix: "frontend/v", Multi: true}

	_, err := release.Run(context.Background(), f, "owner", "repo", comp, testLabels, false, false, discardLogger())
	if err == nil {
		t.Fatal("Run() error = nil, want an error naming the mismatched title")
	}
}

func TestMajorTagName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		tagPrefix string
		version   string
		wantTag   string
		wantOK    bool
	}{
		{name: "builds the bare major tag for the main component", tagPrefix: "v", version: "1.2.3", wantTag: "v1", wantOK: true},
		{
			name: "builds the component-prefixed major tag", tagPrefix: "frontend/v", version: "2.0.0",
			wantTag: "frontend/v2", wantOK: true,
		},
		{name: "does not apply to a pre-release version", tagPrefix: "v", version: "1.2.3-rc.1"},
		{name: "does not apply to an unparseable version", tagPrefix: "v", version: "not-a-version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tag, ok := release.MajorTagName(tc.tagPrefix, tc.version)
			if ok != tc.wantOK || tag != tc.wantTag {
				t.Fatalf("MajorTagName(%q, %q) = (%q, %v), want (%q, %v)", tc.tagPrefix, tc.version, tag, ok, tc.wantTag, tc.wantOK)
			}
		})
	}
}

func TestRunCreatesTheMajorTagOnAStableRelease(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v1.0.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	comp := release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v", MajorTag: true}

	results, err := release.Run(context.Background(), f, "owner", "repo", comp, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].MajorTag != "v1" || results[0].MajorTagMessage != "created major tag v1" {
		t.Fatalf("Run() = %+v, want the major tag v1 created", results)
	}
	if len(f.CreatedTags) != 2 || f.CreatedTags[1] != (forgetest.CreateTagCall{Owner: "owner", Repo: "repo", Tag: "v1", SHA: "deadbeef"}) {
		t.Fatalf("CreatedTags = %+v", f.CreatedTags)
	}
	if len(f.MovedTags) != 0 {
		t.Fatalf("MovedTags = %+v, want none", f.MovedTags)
	}
}

func TestRunMovesAnExistingMajorTagOnAStableRelease(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v1.3.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
		ExistingTags: map[string]bool{"v1": true},
	}
	comp := release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v", MajorTag: true}

	results, err := release.Run(context.Background(), f, "owner", "repo", comp, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].MajorTag != "v1" || results[0].MajorTagMessage != "moved major tag v1" {
		t.Fatalf("Run() = %+v, want the major tag v1 moved", results)
	}
	if len(f.MovedTags) != 1 || f.MovedTags[0] != (forgetest.MoveTagCall{Owner: "owner", Repo: "repo", Tag: "v1", SHA: "deadbeef"}) {
		t.Fatalf("MovedTags = %+v", f.MovedTags)
	}
}

func TestRunSkipsTheMajorTagWhenItIsOffForTheComponent(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v1.0.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].MajorTag != "" || results[0].MajorTagMessage != "" {
		t.Fatalf("Run() = %+v, want no major tag activity", results)
	}
	if len(f.CreatedTags) != 1 || len(f.MovedTags) != 0 {
		t.Fatalf("CreatedTags = %+v, MovedTags = %+v, want only the release tag", f.CreatedTags, f.MovedTags)
	}
}

func TestRunDryRunReportsTheMajorTagWithoutWritingIt(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v1.3.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		ExistingTags: map[string]bool{"v1": true},
	}
	comp := release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v", MajorTag: true}

	results, err := release.Run(context.Background(), f, "owner", "repo", comp, testLabels, false, true, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].MajorTag != "v1" || results[0].MajorTagMessage != "would move major tag v1" {
		t.Fatalf("Run() = %+v, want a dry-run report of moving v1", results)
	}
	if len(f.CreatedTags) != 0 || len(f.MovedTags) != 0 {
		t.Fatalf("dry run wrote something: tags=%+v moved=%+v", f.CreatedTags, f.MovedTags)
	}
}

func TestRunDoesNotMoveTheMajorTagForAPreReleaseVersion(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v1.0.0-rc.1", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	comp := release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v", MajorTag: true}

	results, err := release.Run(context.Background(), f, "owner", "repo", comp, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].MajorTag != "" || results[0].MajorTagMessage != "pre-release, major tag not moved" {
		t.Fatalf("Run() = %+v, want no major tag activity for a pre-release", results)
	}
	// Neither CreateTag nor MoveTag is ever called for the major tag; the
	// only tag call left is the version tag itself.
	if len(f.MovedTags) != 0 {
		t.Fatalf("MovedTags = %+v, want none for a pre-release", f.MovedTags)
	}
	if len(f.CreatedTags) != 1 || f.CreatedTags[0].Tag != "v1.0.0-rc.1" {
		t.Fatalf("CreatedTags = %+v, want only the version tag, no major tag", f.CreatedTags)
	}
}

func TestRunDryRunChangesNothing(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, true, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Message == "" {
		t.Fatalf("Run() = %+v", results)
	}
	if len(f.CreatedTags) != 0 || len(f.CreatedReleases) != 0 || len(f.SetLabelsCalls) != 0 {
		t.Fatalf("dry run wrote something: tags=%+v releases=%+v labels=%+v", f.CreatedTags, f.CreatedReleases, f.SetLabelsCalls)
	}
}

func TestRunMarksAPreReleaseVersionAsAPreReleaseAndSwapsItsTypeLabel(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{
		Number: 9, Title: "chore(main): release v0.2.0-alpha.1",
		Labels: []string{"release: pending", "release: alpha"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending", "release: alpha"}},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Tag != "v0.2.0-alpha.1" {
		t.Fatalf("Run() = %+v", results)
	}
	if len(f.CreatedReleases) != 1 || !f.CreatedReleases[0].Prerelease {
		t.Fatalf("CreatedReleases = %+v, want a pre-release", f.CreatedReleases)
	}
	want := []string{"release: tagged"}
	if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, want) {
		t.Fatalf("SetLabelsCalls = %+v, want the pending and alpha labels both swapped for %v", f.SetLabelsCalls, want)
	}
}

func TestRunKeepsAHumanAddedLabelWhenSwappingAPreReleaseType(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{
		Number: 9, Title: "chore(main): release v0.2.0-rc.2",
		Labels: []string{"release: pending", "release: rc", "priority: high"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending", "release: rc", "priority: high"}},
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Tag != "v0.2.0-rc.2" {
		t.Fatalf("Run() = %+v", results)
	}
	want := []string{"priority: high", "release: tagged"}
	if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, want) {
		t.Fatalf("SetLabelsCalls = %+v, want %v", f.SetLabelsCalls, want)
	}
}

func TestVersionFromLockedTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		title     string
		want      string
		wantNames []string
		wantOK    bool
	}{
		{
			name: "reads the version and every component", title: "chore: release v0.3.0 (a, b)",
			want: "0.3.0", wantNames: []string{"a", "b"}, wantOK: true,
		},
		{name: "rejects an unrelated title", title: "feat: add the next command"},
		{name: "rejects a per-component title", title: "chore(a): release v0.3.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, names, ok := release.VersionFromLockedTitle(tc.title)
			if ok != tc.wantOK || got != tc.want || (ok && !reflect.DeepEqual(names, tc.wantNames)) {
				t.Fatalf("VersionFromLockedTitle(%q) = (%q, %v, %v), want (%q, %v, %v)", tc.title, got, names, ok, tc.want, tc.wantNames, tc.wantOK)
			}
		})
	}
}

func TestLockedTitleRendersTheVersionAndEveryComponent(t *testing.T) {
	t.Parallel()
	got := release.LockedTitle("0.3.0", []string{"a", "b", "c"})
	want := "chore: release v0.3.0 (a, b, c)"
	if got != want {
		t.Fatalf("LockedTitle() = %q, want %q", got, want)
	}
}

func TestRunLockedTagsAndReleasesEveryComponentFromOneMergedPullRequest(t *testing.T) {
	t.Parallel()
	body := "Release v0.3.0\n\n<details>\n<summary>Changelog</summary>\n\n## v0.3.0 (2026-01-02)\n\n" +
		"#### a\n\n### Features\n\n- add thing a\n\n#### b\n\n- Version bump without user facing changes.\n\n</details>\n"
	pr := forge.PullRequest{
		Number: 9, Title: "chore: release v0.3.0 (a, b)", Body: body,
		Labels: []string{"release: pending"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	comps := []release.Component{
		{Name: "a", Branch: "release-me/main", TagPrefix: "a/v", Multi: true},
		{Name: "b", Branch: "release-me/main", TagPrefix: "b/v", Multi: true},
	}

	results, err := release.RunLocked(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("RunLocked() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("RunLocked() = %+v, want two results", results)
	}

	wantTags := []forgetest.CreateTagCall{
		{Owner: "owner", Repo: "repo", Tag: "a/v0.3.0", SHA: "deadbeef"},
		{Owner: "owner", Repo: "repo", Tag: "b/v0.3.0", SHA: "deadbeef"},
	}
	if !reflect.DeepEqual(f.CreatedTags, wantTags) {
		t.Fatalf("CreatedTags = %+v, want %+v", f.CreatedTags, wantTags)
	}

	if len(f.CreatedReleases) != 2 {
		t.Fatalf("CreatedReleases = %+v, want two", f.CreatedReleases)
	}
	if got := f.CreatedReleases[0].Body; got != "### Features\n\n- add thing a" {
		t.Fatalf("component a release body = %q", got)
	}
	if got := f.CreatedReleases[1].Body; got != "- Version bump without user facing changes." {
		t.Fatalf("component b release body = %q", got)
	}

	// The label swaps once for the whole family, not once per component.
	if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, []string{"release: tagged"}) {
		t.Fatalf("SetLabelsCalls = %+v, want one swap to release: tagged", f.SetLabelsCalls)
	}
	// draftUntilAssetsAttach is off, the default: every component's
	// release publishes immediately, exactly as release-me did before
	// that key existed.
	if f.CreatedReleases[0].Draft || f.CreatedReleases[1].Draft {
		t.Fatalf("CreatedReleases = %+v, want Draft false for both", f.CreatedReleases)
	}
}

// TestRunLockedDraftsEveryReleaseWhenDraftUntilAssetsAttachIsOn covers the
// other half of the same choice
// TestRunLockedTagsAndReleasesEveryComponentFromOneMergedPullRequest already
// covers for the off, default case.
func TestRunLockedDraftsEveryReleaseWhenDraftUntilAssetsAttachIsOn(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{
		Number: 9, Title: "chore: release v0.3.0 (a, b)",
		Labels: []string{"release: pending"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{9: "deadbeef"},
		PRLabels:     map[int][]string{9: {"release: pending"}},
	}
	comps := []release.Component{
		{Name: "a", Branch: "release-me/main", TagPrefix: "a/v", Multi: true},
		{Name: "b", Branch: "release-me/main", TagPrefix: "b/v", Multi: true},
	}

	results, err := release.RunLocked(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, true, false, discardLogger())
	if err != nil {
		t.Fatalf("RunLocked() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("RunLocked() = %+v, want two results", results)
	}
	if len(f.CreatedReleases) != 2 || !f.CreatedReleases[0].Draft || !f.CreatedReleases[1].Draft {
		t.Fatalf("CreatedReleases = %+v, want Draft true for both", f.CreatedReleases)
	}
}

func TestRunLockedDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore: release v0.3.0 (a, b)", Labels: []string{"release: pending"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		PRLabels: map[int][]string{9: {"release: pending"}},
	}
	comps := []release.Component{
		{Name: "a", Branch: "release-me/main", TagPrefix: "a/v"},
		{Name: "b", Branch: "release-me/main", TagPrefix: "b/v"},
	}

	results, err := release.RunLocked(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, false, true, discardLogger())
	if err != nil {
		t.Fatalf("RunLocked() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("RunLocked() = %+v, want two results", results)
	}
	if len(f.CreatedTags) != 0 || len(f.CreatedReleases) != 0 || len(f.SetLabelsCalls) != 0 {
		t.Fatalf("dry run wrote something: tags=%+v releases=%+v labels=%+v", f.CreatedTags, f.CreatedReleases, f.SetLabelsCalls)
	}
}

// TestRunLockedResumesOnlyTheComponentWhoseReleaseIsStillADraft reproduces
// the same orphaned-draft hole as the single-component case, for a locked
// family: the shared pull request already carries Tagged, from an earlier
// run that tagged and drafted every component's release, but the asset
// step only finished publishing one of them. RunLocked must resume only
// the one still a draft, by name, not by position: a caller rendering one
// result per component cannot assume every result belongs to the next
// component in comps, once a resume can skip one already published.
func TestRunLockedResumesOnlyTheComponentWhoseReleaseIsStillADraft(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{Number: 9, Title: "chore: release v0.3.0 (a, b)", Labels: []string{"release: tagged"}, Merged: true}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		ReleaseDrafts: map[string]bool{"a/v0.3.0": false, "b/v0.3.0": true},
	}
	comps := []release.Component{
		{Name: "a", Branch: "release-me/main", TagPrefix: "a/v", Multi: true},
		{Name: "b", Branch: "release-me/main", TagPrefix: "b/v", Multi: true},
	}

	results, err := release.RunLocked(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, true, false, discardLogger())
	if err != nil {
		t.Fatalf("RunLocked() error = %v", err)
	}
	if len(results) != 1 || results[0].Name != "b" || results[0].Tag != "b/v0.3.0" {
		t.Fatalf("RunLocked() = %+v, want only component b resumed", results)
	}
	if len(f.CreatedTags) != 0 || len(f.CreatedReleases) != 0 || len(f.SetLabelsCalls) != 0 {
		t.Fatalf("resume wrote something: tags=%+v releases=%+v labels=%+v", f.CreatedTags, f.CreatedReleases, f.SetLabelsCalls)
	}
}

func TestSharedTitleRoundTrips(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		releases []release.TitleRelease
		want     string
	}{
		{
			name:     "one component",
			releases: []release.TitleRelease{{Name: "api", Version: "1.3.0"}},
			want:     "chore: release api v1.3.0",
		},
		{
			name:     "every component at its own version",
			releases: []release.TitleRelease{{Name: "api", Version: "1.3.0"}, {Name: "web", Version: "0.4.1-rc.1"}},
			want:     "chore: release api v1.3.0, web v0.4.1-rc.1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			title := release.SharedTitle(tc.releases)
			if title != tc.want {
				t.Fatalf("SharedTitle() = %q, want %q", title, tc.want)
			}
			got, ok := release.ParseSharedTitle(title)
			if !ok || !reflect.DeepEqual(got, tc.releases) {
				t.Fatalf("ParseSharedTitle(%q) = %+v, %v, want %+v", title, got, ok, tc.releases)
			}
		})
	}
}

func TestParseSharedTitleRejectsOtherTitles(t *testing.T) {
	t.Parallel()
	for _, title := range []string{
		"chore(api): release v1.3.0",
		"chore: release v1.3.0 (api, web)",
		"chore: release api 1.3.0",
		"feat: add the next command",
	} {
		if got, ok := release.ParseSharedTitle(title); ok {
			t.Errorf("ParseSharedTitle(%q) = %+v, true, want false", title, got)
		}
	}
}

// sharedBody is a shared release pull request body as pr renders it: the
// notes block, then one "#### <name>" slice per component, each with its
// own heading.
const sharedBody = "Release api v1.3.0, web v0.4.1-rc.1\n\n```release-me-notes\nShip it.\n```\n\n" +
	"<details>\n<summary>Changelog</summary>\n\n" +
	"#### api\n\n## [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (2026-03-14)\n\n" +
	"### Features\n\n- add the export endpoint\n\n" +
	"#### web\n\n## v0.4.1-rc.1 (2026-03-14)\n\n### Bug Fixes\n\n- keep the session on reload\n\n</details>\n"

func TestRunSharedTagsAndReleasesEveryComponentAtItsOwnVersion(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{
		Number: 12, Title: "chore: release api v1.3.0, web v0.4.1-rc.1", Body: sharedBody,
		Labels: []string{"release: pending", "release: rc"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{12: "cafe1234"},
		PRLabels:     map[int][]string{12: {"release: pending", "release: rc"}},
	}
	comps := []release.Component{
		{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true, MajorTag: true},
		{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true, MajorTag: true},
		{Name: "docs", Branch: "release-me/main", TagPrefix: "docs/v", Multi: true},
	}

	results, err := release.RunShared(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, false, false, discardLogger())
	if err != nil {
		t.Fatalf("RunShared() error = %v", err)
	}

	wantTags := []forgetest.CreateTagCall{
		{Owner: "owner", Repo: "repo", Tag: "api/v1.3.0", SHA: "cafe1234"},
		{Owner: "owner", Repo: "repo", Tag: "api/v1", SHA: "cafe1234"},
		{Owner: "owner", Repo: "repo", Tag: "web/v0.4.1-rc.1", SHA: "cafe1234"},
	}
	if !reflect.DeepEqual(f.CreatedTags, wantTags) {
		t.Fatalf("CreatedTags = %+v, want %+v", f.CreatedTags, wantTags)
	}
	wantReleases := []forgetest.CreateReleaseCall{
		{
			Owner: "owner", Repo: "repo", Tag: "api/v1.3.0", Name: "api v1.3.0",
			Body: "Ship it.\n\n## [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (2026-03-14)\n\n" +
				"### Features\n\n- add the export endpoint",
		},
		{
			Owner: "owner", Repo: "repo", Tag: "web/v0.4.1-rc.1", Name: "web v0.4.1-rc.1",
			Body:       "Ship it.\n\n## v0.4.1-rc.1 (2026-03-14)\n\n### Bug Fixes\n\n- keep the session on reload",
			Prerelease: true,
		},
	}
	if !reflect.DeepEqual(f.CreatedReleases, wantReleases) {
		t.Fatalf("CreatedReleases = %+v, want %+v", f.CreatedReleases, wantReleases)
	}
	if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, []string{"release: tagged"}) {
		t.Fatalf("SetLabelsCalls = %+v, want one swap of pending and rc to release: tagged", f.SetLabelsCalls)
	}
	if len(results) != 2 || results[0].Name != "api" || results[1].Name != "web" {
		t.Fatalf("RunShared() = %+v, want one result each for api and web", results)
	}
}

func TestRunSharedRejectsATitleNamingAnUnconfiguredComponent(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{
		Number: 12, Title: "chore: release api v1.3.0, mobile v2.0.0", Body: sharedBody,
		Labels: []string{"release: pending"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		MergeCommits: map[int]string{12: "cafe1234"},
	}
	comps := []release.Component{{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true}}

	_, err := release.RunShared(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, false, false, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "component mobile is not configured") {
		t.Fatalf("RunShared() error = %v, want it to name the unconfigured component", err)
	}
	if len(f.CreatedTags) != 0 {
		t.Fatalf("CreatedTags = %+v, want none before every component is known", f.CreatedTags)
	}
}

func TestRunSharedResumesADraftLeftByAnEarlierRun(t *testing.T) {
	t.Parallel()
	pr := forge.PullRequest{
		Number: 12, Title: "chore: release api v1.3.0, web v0.4.1-rc.1", Body: sharedBody,
		Labels: []string{"release: tagged"}, Merged: true,
	}
	f := &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{
			{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
		},
		ReleaseDrafts: map[string]bool{"api/v1.3.0": false, "web/v0.4.1-rc.1": true},
	}
	comps := []release.Component{
		{Name: "api", Branch: "release-me/main", TagPrefix: "api/v", Multi: true},
		{Name: "web", Branch: "release-me/main", TagPrefix: "web/v", Multi: true},
	}

	results, err := release.RunShared(context.Background(), f, "owner", "repo", "release-me/main", comps, testLabels, true, false, discardLogger())
	if err != nil {
		t.Fatalf("RunShared() error = %v", err)
	}
	if len(results) != 1 || results[0].Tag != "web/v0.4.1-rc.1" {
		t.Fatalf("RunShared() = %+v, want only the web draft resumed", results)
	}
	if len(f.CreatedTags) != 0 || len(f.CreatedReleases) != 0 {
		t.Fatalf("CreatedTags = %+v, CreatedReleases = %+v, want nothing written on resume", f.CreatedTags, f.CreatedReleases)
	}
}
