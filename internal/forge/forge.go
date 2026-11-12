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

// ErrReleaseNotFound is returned when Release is asked about a tag that
// has no release at all.
var ErrReleaseNotFound = errors.New("release not found")

// ErrBranchMoved reports that publishing a release commit was rejected
// because the branch is somewhere other than the caller expected. The
// command layer returns it when git refuses the push, a stale
// "--force-with-lease" or a non-fast-forward, and the branch is left where
// it is then. No Forge method returns it: CommitBranch moves no reference.
var ErrBranchMoved = errors.New("branch moved")

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
	// Author is the login of the account that opened the pull request,
	// without "@", "" when the forge does not report one.
	Author string
}

// Release is the subset of release data release-me needs: whether it is
// still a draft, its web page and the address its assets upload to, in
// the RFC 6570 URI template shape GitHub returns it,
// "https://uploads.github.com/repos/<owner>/<repo>/releases/<id>/assets{?name,label}".
type Release struct {
	Draft     bool
	HTMLURL   string
	UploadURL string
}

// CommitFile is one file a commit made through CommitBranch writes: its
// path from the repository root, its full new content and whether it is
// executable. Every path of the parent's tree no CommitFile names stays
// exactly as it is.
type CommitFile struct {
	Path       string
	Content    []byte
	Executable bool
}

// BranchCommit describes the commit to create for Branch, built on
// Parent's tree. Parent must already exist on the forge. CommitBranch
// creates objects only; the command layer publishes the returned SHA and
// holds the lease that guards it.
type BranchCommit struct {
	Branch  string
	Parent  string
	Message string
	Files   []CommitFile
}

// Signature is what the forge reports about one commit's signature.
// Verified is true only when the forge checked a signature on the commit
// and accepted it, the condition a ruleset requiring signed commits
// enforces. Reason is the forge's own word for the outcome, on GitHub
// "valid" for a verified commit and "unsigned" for one with no signature
// at all.
type Signature struct {
	Verified bool
	Reason   string
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
	// given title, body and labels, as a draft when draft is true: one the
	// forge refuses to merge until a human marks it ready for review.
	CreatePullRequest(ctx context.Context, owner, repo, base, head, title, body string, labels []string, draft bool) (PullRequest, error)

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

	// CreateLabel creates a label named name on the repository, colored
	// color, a 6 character hex string without a leading "#", and carrying
	// description. A caller names a label missingLabels already reported
	// absent; the forge is left to report a name that already exists.
	CreateLabel(ctx context.Context, owner, repo, name, color, description string) error

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
	// target is the commit the forge creates tag on when tag does not
	// exist yet, once the release is published: right away for a
	// published release, on publish for a draft. "" leaves it to the
	// forge's default, for a caller that created tag itself first.
	// It returns the release it created. ErrReleaseExists is returned
	// when a release for tag already exists.
	CreateRelease(ctx context.Context, owner, repo, tag, target, name, body string, prerelease, draft bool) (Release, error)

	// Release reads the release for tag, draft or published.
	// ErrReleaseNotFound is returned when tag has no release at all. A
	// caller uses it to resume a release that was tagged and drafted by an
	// earlier run, but never got its assets attached and published, and
	// to report the web and upload addresses of a release it did not
	// create in this run: looking it up by tag alone, the way
	// CreateRelease's own duplicate check or a plain "get a release by
	// tag" would, excludes drafts, so an implementation must find it some
	// other way.
	Release(ctx context.Context, owner, repo, tag string) (Release, error)

	// MergeCommit returns the SHA of the commit a merged pull request was
	// merged as.
	MergeCommit(ctx context.Context, owner, repo string, number int) (string, error)

	// CreateComment posts body as a new comment on pull request number: a
	// release pull request's own comment thread, not a threaded reply to
	// any one existing comment.
	CreateComment(ctx context.Context, owner, repo string, number int, body string) error

	// CommitBranch creates c on the forge and returns its SHA without
	// changing any branch reference. The command layer publishes it.
	// No author or committer is supplied: GitHub signs API commits for
	// App installation tokens using the token's identity. User tokens
	// yield unsigned commits, as reported by CommitSignature.
	CommitBranch(ctx context.Context, owner, repo string, c BranchCommit) (string, error)

	// CommitSignature reports what the forge verified about the
	// signature of the commit sha, for a caller to tell a signed commit
	// CommitBranch created from an unsigned one.
	CommitSignature(ctx context.Context, owner, repo, sha string) (Signature, error)

	// CompareURL returns the web address of the forge's compare view
	// between the refs base and head, the tag a release is computed from
	// and the tag it will get, for a changelog heading to link to. It is
	// built from owner, repo and the refs alone, never read from the
	// forge, so it makes no request and works before head exists.
	CompareURL(owner, repo, base, head string) string

	// CommitURL returns the web address of commit sha, for a changelog
	// entry to link to. Like CompareURL it is built from its arguments
	// alone and makes no request.
	CommitURL(owner, repo, sha string) string

	// IssueURL returns the web address of issue number, for a changelog
	// entry's issue reference to link to. Like CompareURL it is built from
	// its arguments alone and makes no request.
	IssueURL(owner, repo string, number int) string

	// GenerateReleaseNotes returns the release notes the forge itself
	// writes for a release tagged tag on the commit target, covering the
	// changes since previousTag, "" to let the forge pick the previous
	// release. tag need not exist yet. The forge renders the notes as
	// Markdown; nothing is created or stored.
	GenerateReleaseNotes(ctx context.Context, owner, repo, tag, previousTag, target string) (string, error)
}
