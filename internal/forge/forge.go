// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package forge defines the interface release-me uses to read and write
// pull requests, labels, tags and releases on a code hosting platform.
// internal/forge/github implements it for GitHub; the core packages depend
// only on this package, never on a concrete implementation.
package forge

import (
	"context"
	"errors"
)

// ErrNotFound is returned when the forge has no pull request for the given
// number.
var ErrNotFound = errors.New("pull request not found")

// ErrTagExists is returned when CreateTag is asked to create a tag that
// already exists.
var ErrTagExists = errors.New("tag already exists")

// ErrReleaseExists is returned when CreateRelease is asked to create a
// release for a tag that already has one.
var ErrReleaseExists = errors.New("release already exists")

// ErrReleaseNotFound is returned when IsReleaseDraft is asked about a tag
// that has no release at all.
var ErrReleaseNotFound = errors.New("release not found")

// ErrUnauthorized is returned when the forge rejects the request's
// credentials, as opposed to any other failure.
var ErrUnauthorized = errors.New("unauthorized")

// MaxTitleLength is the longest pull request title, in characters, a
// forge accepts: GitHub's limit. A command that renders a title it reads
// back later, such as the shared release pull request's, refuses one past
// it before writing anything.
const MaxTitleLength = 256

// PullRequest is the subset of pull request data release-me needs.
type PullRequest struct {
	Number  int
	Title   string
	Body    string
	Labels  []string
	URL     string
	Merged  bool
	HeadSHA string
}

// Forge reads and writes pull request, label, tag and release data on a
// code hosting platform.
//
// PullRequest may return a partially populated PullRequest alongside
// ErrNotFound: an implementation that can derive a link from owner, repo and
// number alone should still set URL, so a caller can show a link even when
// the pull request itself could not be read.
type Forge interface {
	// PullRequest reads one pull request by number.
	PullRequest(ctx context.Context, owner, repo string, number int) (PullRequest, error)

	// PullRequests lists pull requests with the given head branch and
	// state, "open" or "closed".
	PullRequests(ctx context.Context, owner, repo, head, state string) ([]PullRequest, error)

	// CreatePullRequest opens a pull request from head onto base, with the
	// given title, body and labels.
	CreatePullRequest(ctx context.Context, owner, repo, base, head, title, body string, labels []string) (PullRequest, error)

	// UpdatePullRequest edits the title and body of an existing pull
	// request. It never touches labels: a label a human added stays.
	UpdatePullRequest(ctx context.Context, owner, repo string, number int, title, body string) (PullRequest, error)

	// Labels lists the labels currently set on a pull request.
	Labels(ctx context.Context, owner, repo string, number int) ([]string, error)

	// AddLabels adds labels to a pull request, alongside whatever it
	// already carries.
	AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error

	// SetLabels replaces the full label set of a pull request.
	SetLabels(ctx context.Context, owner, repo string, number int, labels []string) error

	// RepositoryLabels lists every label defined on the repository.
	// ErrUnauthorized is returned when the credentials are rejected.
	RepositoryLabels(ctx context.Context, owner, repo string) ([]string, error)

	// DefaultBranch returns the repository's default branch name.
	DefaultBranch(ctx context.Context, owner, repo string) (string, error)

	// CreateTag creates a tag reference named tag on the commit sha.
	// ErrTagExists is returned when the tag already exists.
	CreateTag(ctx context.Context, owner, repo, tag, sha string) error

	// TagExists reports whether tag already exists on the repository.
	TagExists(ctx context.Context, owner, repo, tag string) (bool, error)

	// MoveTag force-moves the existing tag named tag onto the commit sha.
	// Callers check TagExists first; MoveTag never creates a tag.
	MoveTag(ctx context.Context, owner, repo, tag, sha string) error

	// CreateRelease creates a release for tag, with the given name and
	// body, marked as a pre-release when prerelease is true, as a draft
	// when draft is true. The caller decides draft, rather than this
	// always drafting: only a repository with GitHub's immutable
	// releases setting on needs the release held back as a draft until
	// assets attach, and release-me cannot detect that setting itself.
	// A draft is exempt from immutability either way, so a caller that
	// does ask for one can still attach assets before publishing it.
	// ErrReleaseExists is returned when a release for tag already
	// exists.
	CreateRelease(ctx context.Context, owner, repo, tag, name, body string, prerelease, draft bool) error

	// IsReleaseDraft reports whether the release for tag is still a
	// draft, as opposed to already published. ErrReleaseNotFound is
	// returned when tag has no release at all. A caller uses this to resume a
	// release that was tagged and drafted by an earlier run, but never
	// got its assets attached and published: looking it up by tag alone,
	// the way CreateRelease's own duplicate check or a plain "get a
	// release by tag" would, excludes drafts, so an implementation must
	// find it some other way.
	IsReleaseDraft(ctx context.Context, owner, repo, tag string) (bool, error)

	// MergeCommit returns the SHA of the commit a merged pull request was
	// merged as.
	MergeCommit(ctx context.Context, owner, repo string, number int) (string, error)

	// DeleteBranch deletes the branch named branch, such as a release
	// branch whose pull request is merged and released. A branch that does
	// not exist, deleted already by a human or by the forge's own setting
	// to delete head branches on merge, is not an error: what the caller
	// wants is already the case. Any other refusal, a protected branch
	// among them, is.
	DeleteBranch(ctx context.Context, owner, repo, branch string) error

	// CreateComment posts body as a new comment on pull request number: a
	// release pull request's own comment thread, not a threaded reply to
	// any one existing comment.
	CreateComment(ctx context.Context, owner, repo string, number int, body string) error

	// CompareURL returns the web address of the forge's compare view
	// between the refs base and head, the tag a release is computed from
	// and the tag it will get, for a changelog heading to link to. It is
	// built from owner, repo and the refs alone, never read from the
	// forge, so it makes no request and works before head exists.
	CompareURL(owner, repo, base, head string) string
}
