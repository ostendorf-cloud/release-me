// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package forgetest_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

func TestFakePullRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		responses map[int]forgetest.Response
		number    int
		want      forge.PullRequest
		wantErr   error
	}{
		{
			name:   "returns the configured response",
			number: 7,
			responses: map[int]forgetest.Response{
				7: {PullRequest: forge.PullRequest{Number: 7, Title: "feat: add the next command", URL: "https://example.com/pull/7"}},
			},
			want: forge.PullRequest{Number: 7, Title: "feat: add the next command", URL: "https://example.com/pull/7"},
		},
		{
			name:    "returns not found for an unconfigured number",
			number:  9,
			wantErr: forge.ErrNotFound,
		},
		{
			name:   "returns a configured not found with a URL",
			number: 9,
			responses: map[int]forgetest.Response{
				9: {PullRequest: forge.PullRequest{URL: "https://example.com/pull/9"}, Err: forge.ErrNotFound},
			},
			want:    forge.PullRequest{URL: "https://example.com/pull/9"},
			wantErr: forge.ErrNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &forgetest.Fake{Responses: tc.responses}

			got, err := f.PullRequest(context.Background(), "owner", "repo", tc.number)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("PullRequest() error = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("PullRequest() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFakePullRequestsReturnsTheConfiguredList(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		fixture forge.PullRequest
		want    forge.PullRequest
	}{
		{
			name:    "an entry without a URL gets the one create and update return",
			fixture: forge.PullRequest{Number: 5, Title: "chore(main): release v0.2.0"},
			want: forge.PullRequest{
				Number: 5, Title: "chore(main): release v0.2.0", URL: "https://github.com/owner/repo/pull/5",
			},
		},
		{
			name: "an entry with a URL keeps it",
			fixture: forge.PullRequest{
				Number: 5, Title: "chore(main): release v0.2.0", URL: "https://github.example.com/owner/repo/pull/5",
			},
			want: forge.PullRequest{
				Number: 5, Title: "chore(main): release v0.2.0", URL: "https://github.example.com/owner/repo/pull/5",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &forgetest.Fake{ListResponses: []forgetest.ListResponse{
				{Head: "release-me/main", State: "open", PullRequests: []forge.PullRequest{tc.fixture}},
			}}

			got, err := f.PullRequests(context.Background(), "owner", "repo", "release-me/main", "open")
			if err != nil {
				t.Fatalf("PullRequests() error = %v", err)
			}
			if !reflect.DeepEqual(got, []forge.PullRequest{tc.want}) {
				t.Fatalf("PullRequests() = %+v, want %+v", got, []forge.PullRequest{tc.want})
			}
			if !reflect.DeepEqual(f.ListResponses[0].PullRequests[0], tc.fixture) {
				t.Fatalf("fixture = %+v, want it left as configured", f.ListResponses[0].PullRequests[0])
			}

			got, err = f.PullRequests(context.Background(), "owner", "repo", "release-me/main", "closed")
			if err != nil {
				t.Fatalf("PullRequests() error = %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("PullRequests() = %+v, want an empty result for an unconfigured state", got)
			}
		})
	}
}

func TestFakeCreatePullRequestRecordsTheCallAndAssignsANumber(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}

	got, err := f.CreatePullRequest(context.Background(), "owner", "repo", "main", "release-me/main",
		"chore(main): release v0.1.0", "Release v0.1.0", []string{"release: pending"})
	if err != nil {
		t.Fatalf("CreatePullRequest() error = %v", err)
	}
	if got.Number != 1 {
		t.Fatalf("CreatePullRequest() number = %d, want 1", got.Number)
	}
	if len(f.CreatedPullRequests) != 1 || f.CreatedPullRequests[0].Base != "main" {
		t.Fatalf("CreatedPullRequests = %+v", f.CreatedPullRequests)
	}
	if labels, _ := f.Labels(context.Background(), "owner", "repo", 1); !reflect.DeepEqual(labels, []string{"release: pending"}) {
		t.Fatalf("Labels() = %v, want the labels set on create", labels)
	}
}

func TestFakeUpdatePullRequestRecordsTheCallWithoutTouchingLabels(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{PRLabels: map[int][]string{3: {"release: pending", "priority: high"}}}

	if _, err := f.UpdatePullRequest(context.Background(), "owner", "repo", 3,
		"chore(main): release v0.2.0", "Release v0.2.0"); err != nil {
		t.Fatalf("UpdatePullRequest() error = %v", err)
	}
	if len(f.UpdatedPullRequests) != 1 || f.UpdatedPullRequests[0].Number != 3 {
		t.Fatalf("UpdatedPullRequests = %+v", f.UpdatedPullRequests)
	}
	labels, _ := f.Labels(context.Background(), "owner", "repo", 3)
	if !reflect.DeepEqual(labels, []string{"release: pending", "priority: high"}) {
		t.Fatalf("Labels() = %v, want the labels untouched", labels)
	}
}

func TestFakeAddLabelsMergesIntoTheExistingSet(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{PRLabels: map[int][]string{3: {"priority: high"}}}

	if err := f.AddLabels(context.Background(), "owner", "repo", 3, []string{"release: pending"}); err != nil {
		t.Fatalf("AddLabels() error = %v", err)
	}
	if len(f.AddLabelsCalls) != 1 || f.AddLabelsCalls[0].Number != 3 {
		t.Fatalf("AddLabelsCalls = %+v", f.AddLabelsCalls)
	}
	labels, _ := f.Labels(context.Background(), "owner", "repo", 3)
	if !reflect.DeepEqual(labels, []string{"priority: high", "release: pending"}) {
		t.Fatalf("Labels() = %v, want the label added alongside the existing one", labels)
	}

	// Adding a label the pull request already carries is a no-op.
	if err := f.AddLabels(context.Background(), "owner", "repo", 3, []string{"release: pending"}); err != nil {
		t.Fatalf("AddLabels() error = %v", err)
	}
	labels, _ = f.Labels(context.Background(), "owner", "repo", 3)
	if !reflect.DeepEqual(labels, []string{"priority: high", "release: pending"}) {
		t.Fatalf("Labels() = %v, want no duplicate", labels)
	}
}

func TestFakeDefaultBranch(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{DefaultBranchName: "trunk"}

	got, err := f.DefaultBranch(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("DefaultBranch() error = %v", err)
	}
	if got != "trunk" {
		t.Fatalf("DefaultBranch() = %q, want %q", got, "trunk")
	}
}

func TestFakeRepositoryLabelsReturnsTheConfiguredError(t *testing.T) {
	t.Parallel()
	wantErr := forge.ErrUnauthorized
	f := &forgetest.Fake{RepoLabels: []string{"release: pending"}, RepoLabelsErr: wantErr}

	_, err := f.RepositoryLabels(context.Background(), "owner", "repo")
	if !errors.Is(err, wantErr) {
		t.Fatalf("RepositoryLabels() error = %v, want %v", err, wantErr)
	}
}

func TestFakeDeleteBranchRecordsTheCallOrReturnsTheConfiguredError(t *testing.T) {
	t.Parallel()
	refused := errors.New("cannot delete this protected branch")
	cases := []struct {
		name    string
		err     error
		want    []forgetest.DeleteBranchCall
		wantErr error
	}{
		{name: "records the deleted branch", want: []forgetest.DeleteBranchCall{{Owner: "owner", Repo: "repo", Branch: "release-me/api"}}},
		{name: "returns the configured error and records nothing", err: refused, wantErr: refused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &forgetest.Fake{DeleteBranchErr: tc.err}

			err := f.DeleteBranch(context.Background(), "owner", "repo", "release-me/api")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("DeleteBranch() error = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(f.DeletedBranches, tc.want) {
				t.Fatalf("DeletedBranches = %+v, want %+v", f.DeletedBranches, tc.want)
			}
		})
	}
}

func TestFakeSetLabelsRecordsTheCall(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}

	if err := f.SetLabels(context.Background(), "owner", "repo", 7, []string{"release: tagged"}); err != nil {
		t.Fatalf("SetLabels() error = %v", err)
	}
	if len(f.SetLabelsCalls) != 1 || f.SetLabelsCalls[0].Number != 7 {
		t.Fatalf("SetLabelsCalls = %+v", f.SetLabelsCalls)
	}
	labels, _ := f.Labels(context.Background(), "owner", "repo", 7)
	if !reflect.DeepEqual(labels, []string{"release: tagged"}) {
		t.Fatalf("Labels() = %v, want %v", labels, []string{"release: tagged"})
	}
}

// TestFakeSetLabelsReturnsTheConfiguredErrorAndChangesNothing proves the
// fake mirrors a real forge rejecting the write: PRLabels stays whatever it
// was before the call, and no call is recorded, so a caller checking either
// one after a failed SetLabels sees no trace of it.
func TestFakeSetLabelsReturnsTheConfiguredErrorAndChangesNothing(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("forge unavailable")
	f := &forgetest.Fake{PRLabels: map[int][]string{7: {"release: pending"}}, SetLabelsErr: wantErr}

	err := f.SetLabels(context.Background(), "owner", "repo", 7, []string{"release: tagged"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("SetLabels() error = %v, want %v", err, wantErr)
	}
	if len(f.SetLabelsCalls) != 0 {
		t.Fatalf("SetLabelsCalls = %+v, want none recorded", f.SetLabelsCalls)
	}
	labels, _ := f.Labels(context.Background(), "owner", "repo", 7)
	if !reflect.DeepEqual(labels, []string{"release: pending"}) {
		t.Fatalf("Labels() = %v, want the original labels unchanged", labels)
	}
}

func TestFakeRepositoryLabelsReturnsTheConfiguredList(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{RepoLabels: []string{"release: pending", "release: tagged"}}

	got, err := f.RepositoryLabels(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("RepositoryLabels() error = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"release: pending", "release: tagged"}) {
		t.Fatalf("RepositoryLabels() = %v", got)
	}
}

func TestFakeCreateTag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		existingTags map[string]bool
		tag          string
		wantErr      error
		wantCalls    int
	}{
		{name: "records a new tag", tag: "v0.1.0", wantCalls: 1},
		{
			name:         "returns ErrTagExists for a tag that already exists",
			existingTags: map[string]bool{"v0.1.0": true},
			tag:          "v0.1.0",
			wantErr:      forge.ErrTagExists,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &forgetest.Fake{ExistingTags: tc.existingTags}

			err := f.CreateTag(context.Background(), "owner", "repo", tc.tag, "deadbeef")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CreateTag() error = %v, want %v", err, tc.wantErr)
			}
			if len(f.CreatedTags) != tc.wantCalls {
				t.Fatalf("CreatedTags = %+v, want %d calls", f.CreatedTags, tc.wantCalls)
			}
		})
	}
}

func TestFakeTagExists(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{ExistingTags: map[string]bool{"v1": true}}

	got, err := f.TagExists(context.Background(), "owner", "repo", "v1")
	if err != nil {
		t.Fatalf("TagExists() error = %v", err)
	}
	if !got {
		t.Fatal("TagExists() = false, want true for a configured tag")
	}

	got, err = f.TagExists(context.Background(), "owner", "repo", "v2")
	if err != nil {
		t.Fatalf("TagExists() error = %v", err)
	}
	if got {
		t.Fatal("TagExists() = true, want false for an unconfigured tag")
	}
}

func TestFakeTagExistsReturnsTheConfiguredError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("forge unavailable")
	f := &forgetest.Fake{TagExistsErr: wantErr}

	_, err := f.TagExists(context.Background(), "owner", "repo", "v1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("TagExists() error = %v, want %v", err, wantErr)
	}
}

func TestFakeMoveTagRecordsTheCall(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}

	if err := f.MoveTag(context.Background(), "owner", "repo", "v1", "deadbeef"); err != nil {
		t.Fatalf("MoveTag() error = %v", err)
	}
	want := forgetest.MoveTagCall{Owner: "owner", Repo: "repo", Tag: "v1", SHA: "deadbeef"}
	if len(f.MovedTags) != 1 || f.MovedTags[0] != want {
		t.Fatalf("MovedTags = %+v, want %+v", f.MovedTags, want)
	}
}

func TestFakeCreateReleaseRecordsTheCall(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}

	if err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0", "v0.1.0", "Release v0.1.0", false, true); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if len(f.CreatedReleases) != 1 || f.CreatedReleases[0].Tag != "v0.1.0" || f.CreatedReleases[0].Prerelease {
		t.Fatalf("CreatedReleases = %+v", f.CreatedReleases)
	}
}

func TestFakeCreateReleaseRecordsAPreRelease(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}

	if err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0-alpha.1", "v0.1.0-alpha.1", "Release v0.1.0-alpha.1", true, true); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if len(f.CreatedReleases) != 1 || !f.CreatedReleases[0].Prerelease {
		t.Fatalf("CreatedReleases = %+v, want a pre-release recorded", f.CreatedReleases)
	}
}

func TestFakeCreateReleaseReturnsErrReleaseExistsForAnExistingTag(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{ExistingReleases: map[string]bool{"v0.1.0": true}}

	err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0", "v0.1.0", "Release v0.1.0", false, true)
	if !errors.Is(err, forge.ErrReleaseExists) {
		t.Fatalf("CreateRelease() error = %v, want %v", err, forge.ErrReleaseExists)
	}
	if len(f.CreatedReleases) != 0 {
		t.Fatalf("CreatedReleases = %+v, want none", f.CreatedReleases)
	}
}

func TestFakeCreateReleaseRecordsItAsADraftWhenAskedFor(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}

	if err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0", "v0.1.0", "Release v0.1.0", false, true); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if len(f.CreatedReleases) != 1 || !f.CreatedReleases[0].Draft {
		t.Fatalf("CreatedReleases = %+v, want Draft true", f.CreatedReleases)
	}
	draft, err := f.IsReleaseDraft(context.Background(), "owner", "repo", "v0.1.0")
	if err != nil {
		t.Fatalf("IsReleaseDraft() error = %v", err)
	}
	if !draft {
		t.Fatalf("IsReleaseDraft() = %v, want true", draft)
	}
}

func TestFakeCreateReleaseRecordsItAsPublishedWhenDraftIsFalse(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}

	if err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0", "v0.1.0", "Release v0.1.0", false, false); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if len(f.CreatedReleases) != 1 || f.CreatedReleases[0].Draft {
		t.Fatalf("CreatedReleases = %+v, want Draft false", f.CreatedReleases)
	}
	draft, err := f.IsReleaseDraft(context.Background(), "owner", "repo", "v0.1.0")
	if err != nil {
		t.Fatalf("IsReleaseDraft() error = %v", err)
	}
	if draft {
		t.Fatalf("IsReleaseDraft() = %v, want false", draft)
	}
}

func TestFakeIsReleaseDraftReportsAPublishedRelease(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{ReleaseDrafts: map[string]bool{"v0.1.0": false}}

	draft, err := f.IsReleaseDraft(context.Background(), "owner", "repo", "v0.1.0")
	if err != nil {
		t.Fatalf("IsReleaseDraft() error = %v", err)
	}
	if draft {
		t.Fatalf("IsReleaseDraft() = %v, want false", draft)
	}
}

func TestFakeIsReleaseDraftReturnsErrReleaseNotFoundForAnUnknownTag(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}

	_, err := f.IsReleaseDraft(context.Background(), "owner", "repo", "v0.1.0")
	if !errors.Is(err, forge.ErrReleaseNotFound) {
		t.Fatalf("IsReleaseDraft() error = %v, want %v", err, forge.ErrReleaseNotFound)
	}
}

func TestFakeMergeCommitReturnsTheConfiguredSHA(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{MergeCommits: map[int]string{11: "deadbeef"}}

	got, err := f.MergeCommit(context.Background(), "owner", "repo", 11)
	if err != nil {
		t.Fatalf("MergeCommit() error = %v", err)
	}
	if got != "deadbeef" {
		t.Fatalf("MergeCommit() = %q, want %q", got, "deadbeef")
	}
}

func TestFakeCreateCommentRecordsTheCall(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}

	if err := f.CreateComment(context.Background(), "owner", "repo", 7, "forced the next version to 2.0.0."); err != nil {
		t.Fatalf("CreateComment() error = %v", err)
	}

	want := []forgetest.CreateCommentCall{{Owner: "owner", Repo: "repo", Number: 7, Body: "forced the next version to 2.0.0."}}
	if !reflect.DeepEqual(f.CreatedComments, want) {
		t.Fatalf("CreatedComments = %+v, want %+v", f.CreatedComments, want)
	}
}

func TestFakeCreateCommentReturnsTheConfiguredError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("boom")
	f := &forgetest.Fake{CreateCommentErr: wantErr}

	if err := f.CreateComment(context.Background(), "owner", "repo", 7, "hello"); !errors.Is(err, wantErr) {
		t.Fatalf("CreateComment() error = %v, want %v", err, wantErr)
	}
	if len(f.CreatedComments) != 0 {
		t.Fatalf("CreatedComments = %+v, want none recorded", f.CreatedComments)
	}
}

func TestFakeCompareURLHasGitHubsShape(t *testing.T) {
	t.Parallel()
	f := &forgetest.Fake{}
	got := f.CompareURL("ostendorf-cloud", "duxt", "duxt@v0.8.1", "duxt@v0.9.0")
	want := "https://github.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0"
	if got != want {
		t.Fatalf("CompareURL() = %q, want %q", got, want)
	}
}
