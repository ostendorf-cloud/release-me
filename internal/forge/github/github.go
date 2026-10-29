// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package github implements internal/forge.Forge on top of the GitHub REST
// API through go-github.
package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	gogithub "github.com/google/go-github/v93/github"
	"github.com/ostendorf-cloud/release-me/internal/forge"
)

// Forge reads pull request data from the GitHub REST API.
type Forge struct {
	client *gogithub.Client
}

// New builds a Forge. An empty token makes unauthenticated requests.
func New(token string, opts ...gogithub.ClientOptionsFunc) (*Forge, error) {
	if token != "" {
		opts = append(opts, gogithub.WithAuthToken(token))
	}
	client, err := gogithub.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("github: new client: %w", err)
	}
	return &Forge{client: client}, nil
}

// PullRequest implements forge.Forge.
func (f *Forge) PullRequest(ctx context.Context, owner, repo string, number int) (forge.PullRequest, error) {
	url := fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, number)

	pr, resp, err := f.client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return forge.PullRequest{URL: url}, forge.ErrNotFound
		}
		return forge.PullRequest{}, fmt.Errorf("github: get pull request #%d: %w", number, err)
	}
	result := toPullRequest(pr)
	result.Number = number
	return result, nil
}

// PullRequests implements forge.Forge.
func (f *Forge) PullRequests(ctx context.Context, owner, repo, head, state string) ([]forge.PullRequest, error) {
	opts := &gogithub.PullRequestListOptions{
		Head:        owner + ":" + head,
		State:       state,
		ListOptions: gogithub.ListOptions{PerPage: 100},
	}

	var out []forge.PullRequest
	for {
		prs, resp, err := f.client.PullRequests.List(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("github: list pull requests for %s: %w", head, err)
		}
		for _, pr := range prs {
			out = append(out, toPullRequest(pr))
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// CreatePullRequest implements forge.Forge.
func (f *Forge) CreatePullRequest(ctx context.Context, owner, repo, base, head, title, body string, labels []string) (forge.PullRequest, error) {
	pr, _, err := f.client.PullRequests.Create(ctx, owner, repo, gogithub.CreatePullRequest{
		Title: &title,
		Head:  head,
		Base:  base,
		Body:  &body,
	})
	if err != nil {
		return forge.PullRequest{}, fmt.Errorf("github: create pull request %s onto %s: %w", head, base, err)
	}
	if len(labels) > 0 {
		if err := f.AddLabels(ctx, owner, repo, pr.GetNumber(), labels); err != nil {
			return forge.PullRequest{}, err
		}
	}
	result := toPullRequest(pr)
	result.Labels = labels
	return result, nil
}

// UpdatePullRequest implements forge.Forge.
func (f *Forge) UpdatePullRequest(ctx context.Context, owner, repo string, number int, title, body string) (forge.PullRequest, error) {
	pr, _, err := f.client.PullRequests.Edit(ctx, owner, repo, number, &gogithub.PullRequest{
		Title: &title,
		Body:  &body,
	})
	if err != nil {
		return forge.PullRequest{}, fmt.Errorf("github: update pull request #%d: %w", number, err)
	}
	return toPullRequest(pr), nil
}

// Labels implements forge.Forge.
func (f *Forge) Labels(ctx context.Context, owner, repo string, number int) ([]string, error) {
	ls, _, err := f.client.Issues.ListLabelsByIssue(ctx, owner, repo, number, nil)
	if err != nil {
		return nil, fmt.Errorf("github: list labels for pull request #%d: %w", number, err)
	}
	return labelNames(ls), nil
}

// AddLabels implements forge.Forge.
func (f *Forge) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	if _, _, err := f.client.Issues.AddLabelsToIssue(ctx, owner, repo, number, labels); err != nil {
		return fmt.Errorf("github: add labels to pull request #%d: %w", number, err)
	}
	return nil
}

// SetLabels implements forge.Forge.
func (f *Forge) SetLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	if _, _, err := f.client.Issues.ReplaceLabelsForIssue(ctx, owner, repo, number, labels); err != nil {
		return fmt.Errorf("github: set labels for pull request #%d: %w", number, err)
	}
	return nil
}

// RepositoryLabels implements forge.Forge.
func (f *Forge) RepositoryLabels(ctx context.Context, owner, repo string) ([]string, error) {
	opts := &gogithub.ListOptions{PerPage: 100}

	var out []string
	for {
		ls, resp, err := f.client.Issues.ListLabels(ctx, owner, repo, opts)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusUnauthorized {
				return nil, fmt.Errorf("github: list repository labels: %w: %w", forge.ErrUnauthorized, err)
			}
			return nil, fmt.Errorf("github: list repository labels: %w", err)
		}
		out = append(out, labelNames(ls)...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// DefaultBranch implements forge.Forge.
func (f *Forge) DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, _, err := f.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", fmt.Errorf("github: get repository %s/%s: %w", owner, repo, err)
	}
	return r.GetDefaultBranch(), nil
}

// CreateTag implements forge.Forge.
func (f *Forge) CreateTag(ctx context.Context, owner, repo, tag, sha string) error {
	_, resp, err := f.client.Git.CreateRef(ctx, owner, repo, gogithub.CreateRef{
		Ref: "refs/tags/" + tag,
		SHA: sha,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnprocessableEntity {
			return forge.ErrTagExists
		}
		return fmt.Errorf("github: create tag %s: %w", tag, err)
	}
	return nil
}

// TagExists implements forge.Forge.
func (f *Forge) TagExists(ctx context.Context, owner, repo, tag string) (bool, error) {
	_, resp, err := f.client.Git.GetRef(ctx, owner, repo, "refs/tags/"+tag)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("github: get tag %s: %w", tag, err)
	}
	return true, nil
}

// MoveTag implements forge.Forge.
func (f *Forge) MoveTag(ctx context.Context, owner, repo, tag, sha string) error {
	force := true
	_, _, err := f.client.Git.UpdateRef(ctx, owner, repo, "refs/tags/"+tag, gogithub.UpdateRef{SHA: sha, Force: &force})
	if err != nil {
		return fmt.Errorf("github: move tag %s: %w", tag, err)
	}
	return nil
}

// CommitBranch implements forge.Forge through the Git Data REST API: a
// blob per file, a tree on top of the parent's, a commit with that one
// parent, then the branch ref created or moved onto it. The commit is
// sent without author or committer, so GitHub fills both from the token
// and signs the commit itself, which it does only when neither is set.
//
// GraphQL's createCommitOnBranch would sign too, but it only appends to a
// branch's current tip: it cannot rebuild a release branch from the
// default branch's tip, which every push to an existing release branch
// does. GitHub's REST ref update takes no expected value, so the lease on
// c.ExpectedHead is a read of the ref right before the forced update; the
// window between the two is the one a concurrent writer could slip into.
func (f *Forge) CommitBranch(ctx context.Context, owner, repo string, c forge.BranchCommit) (string, error) {
	parent, _, err := f.client.Git.GetCommit(ctx, owner, repo, c.Parent)
	if err != nil {
		return "", fmt.Errorf("github: branch %s: read parent commit %s: %w", c.Branch, c.Parent, err)
	}

	entries := make([]*gogithub.TreeEntry, 0, len(c.Files))
	for _, file := range c.Files {
		blob, _, err := f.client.Git.CreateBlob(ctx, owner, repo, gogithub.Blob{
			Content:  base64.StdEncoding.EncodeToString(file.Content),
			Encoding: new("base64"),
		})
		if err != nil {
			return "", fmt.Errorf("github: branch %s: create blob for %s: %w", c.Branch, file.Path, err)
		}
		mode := "100644"
		if file.Executable {
			mode = "100755"
		}
		entries = append(entries, &gogithub.TreeEntry{
			Path: new(file.Path),
			Mode: new(mode),
			Type: new("blob"),
			SHA:  blob.SHA,
		})
	}

	tree, _, err := f.client.Git.CreateTree(ctx, owner, repo, parent.GetTree().GetSHA(), entries)
	if err != nil {
		return "", fmt.Errorf("github: branch %s: create tree: %w", c.Branch, err)
	}

	commit, _, err := f.client.Git.CreateCommit(ctx, owner, repo, gogithub.Commit{
		Message: new(c.Message),
		Tree:    &gogithub.Tree{SHA: tree.SHA},
		Parents: []*gogithub.Commit{{SHA: new(c.Parent)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("github: branch %s: create commit: %w", c.Branch, err)
	}

	sha := commit.GetSHA()
	if err := f.moveBranch(ctx, owner, repo, c, sha); err != nil {
		return "", err
	}
	return sha, nil
}

// moveBranch points c.Branch at sha: replacing it outright when its
// current tip is c.ExpectedHead, or, with no ExpectedHead, creating it or
// fast-forwarding it, GitHub rejecting any other move.
func (f *Forge) moveBranch(ctx context.Context, owner, repo string, c forge.BranchCommit, sha string) error {
	ref := "refs/heads/" + c.Branch

	if c.ExpectedHead == "" {
		_, resp, err := f.client.Git.CreateRef(ctx, owner, repo, gogithub.CreateRef{Ref: ref, SHA: sha})
		if err == nil {
			return nil
		}
		if resp == nil || resp.StatusCode != http.StatusUnprocessableEntity {
			return fmt.Errorf("github: create branch %s: %w", c.Branch, err)
		}
		// The branch already exists: move it the way a plain push would,
		// fast-forward only.
		_, resp, err = f.client.Git.UpdateRef(ctx, owner, repo, ref, gogithub.UpdateRef{SHA: sha, Force: new(false)})
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusUnprocessableEntity {
				return fmt.Errorf("github: update branch %s: not a fast-forward: %w", c.Branch, forge.ErrBranchMoved)
			}
			return fmt.Errorf("github: update branch %s: %w", c.Branch, err)
		}
		return nil
	}

	current, resp, err := f.client.Git.GetRef(ctx, owner, repo, ref)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("github: update branch %s: it no longer exists, expected %s: %w", c.Branch, c.ExpectedHead, forge.ErrBranchMoved)
		}
		return fmt.Errorf("github: read branch %s: %w", c.Branch, err)
	}
	if got := current.GetObject().GetSHA(); got != c.ExpectedHead {
		return fmt.Errorf("github: update branch %s: it is at %s, expected %s: %w", c.Branch, got, c.ExpectedHead, forge.ErrBranchMoved)
	}
	if _, _, err := f.client.Git.UpdateRef(ctx, owner, repo, ref, gogithub.UpdateRef{SHA: sha, Force: new(true)}); err != nil {
		return fmt.Errorf("github: update branch %s: %w", c.Branch, err)
	}
	return nil
}

// CreateRelease implements forge.Forge. draft is the caller's own choice,
// release-me.yaml's "draft-until-assets-attach" key: GitHub's immutable
// releases feature rejects any later change to a published release,
// assets included, and the REST API exposes no field to tell whether a
// repository has it on, so a repository that does needs the release held
// back as a draft until assets attach. A draft release fires no "release"
// webhook at all, so a caller that drafts one and attaches assets
// afterward must publish it itself once they land, for example with
// "gh release edit <tag> --draft=false", rather than wait on a webhook
// that never comes.
func (f *Forge) CreateRelease(ctx context.Context, owner, repo, tag, name, body string, prerelease, draft bool) error {
	_, resp, err := f.client.Repositories.CreateRelease(ctx, owner, repo, gogithub.CreateReleaseRequest{
		TagName:    tag,
		Name:       &name,
		Body:       &body,
		Draft:      &draft,
		Prerelease: &prerelease,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnprocessableEntity && releaseAlreadyExists(err) {
			return forge.ErrReleaseExists
		}
		return fmt.Errorf("github: create release %s: %w", tag, err)
	}
	return nil
}

// releaseAlreadyExists reports whether err is GitHub's validation error for
// a release that genuinely already exists at the requested tag right now:
// a 422 response whose errors slice names "already_exists" on the
// "tag_name" field. GitHub returns 422 on this endpoint for other reasons
// too, including a tag name that was ever used by a now-deleted immutable
// release, which can never host a release again; those are not this case,
// and the caller must see the real error instead of treating them as
// recoverable.
func releaseAlreadyExists(err error) bool {
	var errResp *gogithub.ErrorResponse
	if !errors.As(err, &errResp) {
		return false
	}
	for _, e := range errResp.Errors {
		if e.Field == "tag_name" && e.Code == "already_exists" {
			return true
		}
	}
	return false
}

// IsReleaseDraft implements forge.Forge. It lists the repository's releases
// and matches tag by hand, rather than GitHub's "get a release by tag name"
// endpoint: that endpoint excludes drafts, confirmed live against this
// repository, so it would read a still-draft release as not found. Listing
// releases does include drafts, for a caller authenticated with push
// access, the same access release-me already needs to create one.
//
// Every tagged pull request resuming a release pages through the full
// release list again, so the resume scan is quadratic in release count.
// Left as is: a cache would need invalidating on every CreateRelease in the
// same run to stay correct, which is more than this repository's release
// history currently justifies.
func (f *Forge) IsReleaseDraft(ctx context.Context, owner, repo, tag string) (bool, error) {
	opts := &gogithub.ListOptions{PerPage: 100}
	for {
		releases, resp, err := f.client.Repositories.ListReleases(ctx, owner, repo, opts)
		if err != nil {
			return false, fmt.Errorf("github: list releases for %s/%s: %w", owner, repo, err)
		}
		for _, r := range releases {
			if r.GetTagName() == tag {
				return r.GetDraft(), nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return false, forge.ErrReleaseNotFound
}

// MergeCommit implements forge.Forge.
func (f *Forge) MergeCommit(ctx context.Context, owner, repo string, number int) (string, error) {
	pr, _, err := f.client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return "", fmt.Errorf("github: get pull request #%d: %w", number, err)
	}
	return pr.GetMergeCommitSHA(), nil
}

// CreateComment implements forge.Forge. A pull request is an issue on the
// same number for this endpoint, go-github's own split between the two
// APIs notwithstanding.
func (f *Forge) CreateComment(ctx context.Context, owner, repo string, number int, body string) error {
	if _, _, err := f.client.Issues.CreateComment(ctx, owner, repo, number, gogithub.IssueCommentRequest{Body: body}); err != nil {
		return fmt.Errorf("github: comment on pull request #%d: %w", number, err)
	}
	return nil
}

// CompareURL implements forge.Forge: GitHub's compare view,
// "<web URL>/<owner>/<repo>/compare/<base>...<head>", on the web host
// webURL derives from the API base URL the client was built with.
func (f *Forge) CompareURL(owner, repo, base, head string) string {
	return fmt.Sprintf("%s/%s/%s/compare/%s...%s", webURL(f.client.BaseURL()), owner, repo, base, head)
}

// webURL derives the web address a GitHub instance serves its pages on
// from the address its REST API answers on: a GitHub Enterprise Server's
// "https://<host>/api/v3/" serves its pages on "https://<host>", and an
// API host named "api.<host>", "api.github.com" or a GitHub Enterprise
// Cloud "api.<subdomain>.ghe.com", serves them on "<host>". Any other
// address is used as it is, its trailing slash removed, and so is an
// address that does not parse.
func webURL(api string) string {
	u, err := url.Parse(api)
	if err != nil {
		return strings.TrimSuffix(api, "/")
	}
	u.RawQuery, u.Fragment = "", ""
	path := strings.TrimSuffix(u.Path, "/")
	switch {
	case strings.HasSuffix(path, "/api/v3"):
		path = strings.TrimSuffix(path, "/api/v3")
	case path == "" && strings.HasPrefix(u.Host, "api."):
		u.Host = strings.TrimPrefix(u.Host, "api.")
	}
	u.Path = path
	return strings.TrimSuffix(u.String(), "/")
}

// toPullRequest converts a go-github pull request into a forge.PullRequest.
//
// Merged reads both the single pull request endpoint's merged field and the
// merge timestamp: go-github documents merged as one of the fields the List
// operation, which PullRequests calls, never populates, so a pull request
// read from the list never carries merged. It does carry merged_at, set
// only once a pull request is merged, so a non-zero merge timestamp is as
// reliable a sign of a merge as the merged field itself, and works for both
// endpoints.
func toPullRequest(pr *gogithub.PullRequest) forge.PullRequest {
	return forge.PullRequest{
		Number:  pr.GetNumber(),
		Title:   pr.GetTitle(),
		Body:    pr.GetBody(),
		Labels:  labelNames(pr.Labels),
		URL:     pr.GetHTMLURL(),
		Merged:  pr.GetMerged() || !pr.GetMergedAt().IsZero(),
		HeadSHA: pr.GetHead().GetSHA(),
	}
}

// labelNames reads the names off a slice of go-github labels.
func labelNames(ls []*gogithub.Label) []string {
	names := make([]string, 0, len(ls))
	for _, l := range ls {
		names = append(names, l.GetName())
	}
	return names
}
