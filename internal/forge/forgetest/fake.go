// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package forgetest provides a fake forge.Forge for tests, so callers never
// reach the network.
package forgetest

import (
	"context"
	"slices"

	"github.com/ostendorf-cloud/release-me/internal/forge"
)

// Response is the configured answer for one pull request number.
type Response struct {
	PullRequest forge.PullRequest
	Err         error
}

// ListResponse configures what PullRequests returns for one head and state.
type ListResponse struct {
	Head         string
	State        string
	PullRequests []forge.PullRequest
}

// CreatePullRequestCall records one call to CreatePullRequest.
type CreatePullRequestCall struct {
	Owner, Repo, Base, Head, Title, Body string
	Labels                               []string
}

// UpdatePullRequestCall records one call to UpdatePullRequest. It never
// carries labels: UpdatePullRequest does not touch them.
type UpdatePullRequestCall struct {
	Owner, Repo string
	Number      int
	Title, Body string
}

// AddLabelsCall records one call to AddLabels.
type AddLabelsCall struct {
	Owner, Repo string
	Number      int
	Labels      []string
}

// SetLabelsCall records one call to SetLabels.
type SetLabelsCall struct {
	Owner, Repo string
	Number      int
	Labels      []string
}

// CreateTagCall records one call to CreateTag.
type CreateTagCall struct {
	Owner, Repo, Tag, SHA string
}

// MoveTagCall records one call to MoveTag.
type MoveTagCall struct {
	Owner, Repo, Tag, SHA string
}

// CreateReleaseCall records one call to CreateRelease.
type CreateReleaseCall struct {
	Owner, Repo, Tag, Name, Body string
	Prerelease, Draft            bool
}

// Fake is a forge.Forge backed by fixed, pre-configured responses. It
// records every write call, so tests can assert on them.
type Fake struct {
	// Responses configures PullRequest, one entry per pull request number.
	// A number without a configured response yields forge.ErrNotFound with
	// a zero forge.PullRequest.
	Responses map[int]Response

	// ListResponses configures PullRequests. A head and state without a
	// matching entry yields an empty, non-nil result.
	ListResponses []ListResponse

	// PRLabels is the current label set per pull request number, read by
	// Labels and written by AddLabels and SetLabels. CreatePullRequest
	// seeds it with the labels it was called with.
	PRLabels map[int][]string

	// RepoLabels is what RepositoryLabels returns, unless RepoLabelsErr is
	// set.
	RepoLabels []string

	// RepoLabelsErr, when set, is returned by RepositoryLabels instead of
	// RepoLabels, to simulate a rejected token or another forge failure.
	RepoLabelsErr error

	// DefaultBranchName is what DefaultBranch returns, unless
	// DefaultBranchErr is set.
	DefaultBranchName string

	// DefaultBranchErr, when set, is returned by DefaultBranch instead of
	// DefaultBranchName.
	DefaultBranchErr error

	// MergeCommits maps a pull request number to the SHA MergeCommit
	// returns for it.
	MergeCommits map[int]string

	// ExistingTags is the set of tag names CreateTag treats as already
	// present, returning forge.ErrTagExists instead of recording a call,
	// and that TagExists reports as present.
	ExistingTags map[string]bool

	// TagExistsErr, when set, is returned by TagExists instead of
	// consulting ExistingTags.
	TagExistsErr error

	// ExistingReleases is the set of tag names CreateRelease treats as
	// already released, returning forge.ErrReleaseExists instead of
	// recording a call.
	ExistingReleases map[string]bool

	// CreateReleaseErrs maps a tag name to the error CreateRelease returns
	// for it instead of recording a call, checked before ExistingReleases.
	// It simulates a non-recoverable forge error, distinct from
	// forge.ErrReleaseExists, such as GitHub rejecting a tag name that was
	// ever used by a now-deleted immutable release.
	CreateReleaseErrs map[string]error

	// ReleaseDrafts is the draft status IsReleaseDraft reports, by tag
	// name: true for a draft, false for a published release. A
	// successful CreateRelease call sets its tag here to whatever draft
	// it was called with, the same as the real forge. A tag absent from
	// this map has no release at all, as far as IsReleaseDraft is
	// concerned, and is reported as forge.ErrReleaseNotFound.
	ReleaseDrafts map[string]bool

	// NextNumber is the pull request number CreatePullRequest assigns next.
	NextNumber int

	CreatedPullRequests []CreatePullRequestCall
	UpdatedPullRequests []UpdatePullRequestCall
	AddLabelsCalls      []AddLabelsCall
	SetLabelsCalls      []SetLabelsCall
	CreatedTags         []CreateTagCall
	MovedTags           []MoveTagCall
	CreatedReleases     []CreateReleaseCall
}

// PullRequest implements forge.Forge.
func (f *Fake) PullRequest(_ context.Context, _, _ string, number int) (forge.PullRequest, error) {
	r, ok := f.Responses[number]
	if !ok {
		return forge.PullRequest{}, forge.ErrNotFound
	}
	return r.PullRequest, r.Err
}

// PullRequests implements forge.Forge.
func (f *Fake) PullRequests(_ context.Context, _, _, head, state string) ([]forge.PullRequest, error) {
	for _, r := range f.ListResponses {
		if r.Head == head && r.State == state {
			return r.PullRequests, nil
		}
	}
	return nil, nil
}

// CreatePullRequest implements forge.Forge.
func (f *Fake) CreatePullRequest(_ context.Context, owner, repo, base, head, title, body string, labels []string) (forge.PullRequest, error) {
	f.NextNumber++
	number := f.NextNumber
	f.CreatedPullRequests = append(f.CreatedPullRequests, CreatePullRequestCall{
		Owner: owner, Repo: repo, Base: base, Head: head, Title: title, Body: body, Labels: labels,
	})
	f.setPRLabels(number, labels)
	return forge.PullRequest{Number: number, Title: title, Body: body, Labels: labels}, nil
}

// UpdatePullRequest implements forge.Forge. It never touches labels.
func (f *Fake) UpdatePullRequest(_ context.Context, owner, repo string, number int, title, body string) (forge.PullRequest, error) {
	f.UpdatedPullRequests = append(f.UpdatedPullRequests, UpdatePullRequestCall{
		Owner: owner, Repo: repo, Number: number, Title: title, Body: body,
	})
	return forge.PullRequest{Number: number, Title: title, Body: body, Labels: f.PRLabels[number]}, nil
}

// Labels implements forge.Forge.
func (f *Fake) Labels(_ context.Context, _, _ string, number int) ([]string, error) {
	return f.PRLabels[number], nil
}

// AddLabels implements forge.Forge: it adds to the existing label set,
// never replacing it.
func (f *Fake) AddLabels(_ context.Context, owner, repo string, number int, labels []string) error {
	f.AddLabelsCalls = append(f.AddLabelsCalls, AddLabelsCall{Owner: owner, Repo: repo, Number: number, Labels: labels})
	current := f.PRLabels[number]
	for _, l := range labels {
		if !slices.Contains(current, l) {
			current = append(current, l)
		}
	}
	f.setPRLabels(number, current)
	return nil
}

// SetLabels implements forge.Forge.
func (f *Fake) SetLabels(_ context.Context, owner, repo string, number int, labels []string) error {
	f.SetLabelsCalls = append(f.SetLabelsCalls, SetLabelsCall{Owner: owner, Repo: repo, Number: number, Labels: labels})
	f.setPRLabels(number, labels)
	return nil
}

// RepositoryLabels implements forge.Forge.
func (f *Fake) RepositoryLabels(_ context.Context, _, _ string) ([]string, error) {
	if f.RepoLabelsErr != nil {
		return nil, f.RepoLabelsErr
	}
	return f.RepoLabels, nil
}

// DefaultBranch implements forge.Forge.
func (f *Fake) DefaultBranch(_ context.Context, _, _ string) (string, error) {
	if f.DefaultBranchErr != nil {
		return "", f.DefaultBranchErr
	}
	return f.DefaultBranchName, nil
}

// CreateTag implements forge.Forge.
func (f *Fake) CreateTag(_ context.Context, owner, repo, tag, sha string) error {
	if f.ExistingTags[tag] {
		return forge.ErrTagExists
	}
	f.CreatedTags = append(f.CreatedTags, CreateTagCall{Owner: owner, Repo: repo, Tag: tag, SHA: sha})
	return nil
}

// TagExists implements forge.Forge.
func (f *Fake) TagExists(_ context.Context, _, _, tag string) (bool, error) {
	if f.TagExistsErr != nil {
		return false, f.TagExistsErr
	}
	return f.ExistingTags[tag], nil
}

// MoveTag implements forge.Forge.
func (f *Fake) MoveTag(_ context.Context, owner, repo, tag, sha string) error {
	f.MovedTags = append(f.MovedTags, MoveTagCall{Owner: owner, Repo: repo, Tag: tag, SHA: sha})
	return nil
}

// CreateRelease implements forge.Forge.
func (f *Fake) CreateRelease(_ context.Context, owner, repo, tag, name, body string, prerelease, draft bool) error {
	if err, ok := f.CreateReleaseErrs[tag]; ok {
		return err
	}
	if f.ExistingReleases[tag] {
		return forge.ErrReleaseExists
	}
	f.CreatedReleases = append(
		f.CreatedReleases,
		CreateReleaseCall{Owner: owner, Repo: repo, Tag: tag, Name: name, Body: body, Prerelease: prerelease, Draft: draft},
	)
	if f.ReleaseDrafts == nil {
		f.ReleaseDrafts = map[string]bool{}
	}
	f.ReleaseDrafts[tag] = draft
	return nil
}

// IsReleaseDraft implements forge.Forge.
func (f *Fake) IsReleaseDraft(_ context.Context, _, _, tag string) (bool, error) {
	draft, ok := f.ReleaseDrafts[tag]
	if !ok {
		return false, forge.ErrReleaseNotFound
	}
	return draft, nil
}

// MergeCommit implements forge.Forge.
func (f *Fake) MergeCommit(_ context.Context, _, _ string, number int) (string, error) {
	return f.MergeCommits[number], nil
}

func (f *Fake) setPRLabels(number int, labels []string) {
	if f.PRLabels == nil {
		f.PRLabels = map[int][]string{}
	}
	f.PRLabels[number] = labels
}
