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
func (f *Forge) CreatePullRequest(
	ctx context.Context, owner, repo, base, head, title, body string, labels []string, draft bool,
) (forge.PullRequest, error) {
	pr, _, err := f.client.PullRequests.Create(ctx, owner, repo, gogithub.CreatePullRequest{
		Title: &title,
		Head:  head,
		Base:  base,
		Body:  &body,
		Draft: &draft,
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

// CreateLabel implements forge.Forge.
func (f *Forge) CreateLabel(ctx context.Context, owner, repo, name, color, description string) error {
	_, _, err := f.client.Issues.CreateLabel(ctx, owner, repo, gogithub.CreateIssueLabelRequest{
		Name: name, Color: &color, Description: &description,
	})
	if err != nil {
		return fmt.Errorf("github: create label %q: %w", name, err)
	}
	return nil
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
// blob per file, a tree on top of the parent's and a commit with that
// one parent. No reference moves. The commit is sent without author or
// committer, so GitHub fills both from the token. GitHub signs it only
// when neither is set and the token is a GitHub App installation token,
// secrets.GITHUB_TOKEN included; for a personal access token or an
// OAuth token the commit stays unsigned, which CommitSignature reports.
//
// GraphQL's createCommitOnBranch is documented to sign its commits "if
// supported", without saying which credentials that covers, but it only
// appends to a branch's current tip: it cannot rebuild a release branch
// from the default branch's tip, which every push to an existing release
// branch does.
// Resetting the branch to the default branch's tip first and appending
// after is no way around that: for a moment the open release pull
// request's head is a commit its base already contains, which GitHub may
// take as the pull request merged or close it. The command layer publishes
// the returned commit with a Git transport lease; the API never moves a ref.
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

	// A commit changing no file, a release with skip-changelog and no
	// version file, stays on the parent's tree: there is nothing to build.
	treeSHA := parent.GetTree().GetSHA()
	if len(entries) > 0 {
		tree, _, err := f.client.Git.CreateTree(ctx, owner, repo, treeSHA, entries)
		if err != nil {
			return "", fmt.Errorf("github: branch %s: create tree: %w", c.Branch, err)
		}
		treeSHA = tree.GetSHA()
	}

	commit, _, err := f.client.Git.CreateCommit(ctx, owner, repo, gogithub.Commit{
		Message: new(c.Message),
		Tree:    &gogithub.Tree{SHA: new(treeSHA)},
		Parents: []*gogithub.Commit{{SHA: new(c.Parent)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("github: branch %s: create commit: %w", c.Branch, err)
	}

	return commit.GetSHA(), nil
}

// CommitSignature implements forge.Forge through the commits REST API,
// "GET /repos/{owner}/{repo}/commits/{sha}", whose commit.verification
// carries GitHub's verdict on the commit's signature.
func (f *Forge) CommitSignature(ctx context.Context, owner, repo, sha string) (forge.Signature, error) {
	commit, _, err := f.client.Repositories.GetCommit(ctx, owner, repo, sha, nil)
	if err != nil {
		return forge.Signature{}, fmt.Errorf("github: read signature of commit %s: %w", sha, err)
	}
	v := commit.GetCommit().GetVerification()
	return forge.Signature{Verified: v.GetVerified(), Reason: v.GetReason()}, nil
}

// CreateRelease implements forge.Forge. draft is the caller's own choice,
// release-me.yaml's "github-release-draft-until-assets-attach" key: GitHub's immutable
// releases feature rejects any later change to a published release,
// assets included, and the REST API exposes no field to tell whether a
// repository has it on, so a repository that does needs the release held
// back as a draft until assets attach. A draft release fires no "release"
// webhook at all, so a caller that drafts one and attaches assets
// afterward must publish it itself once they land, for example with
// "gh release edit <tag> --draft=false", rather than wait on a webhook
// that never comes.
//
// target is sent as target_commitish only when set. What GitHub documents
// about that field is the half that is verified: the REST reference for
// creating a release says it "Specifies the commitish value that
// determines where the Git tag is created from" and is "Unused if the Git
// tag already exists", so a tag of the same name that is already in the
// repository is never redirected through this path, whatever target holds.
//
// What GitHub does not document is when the tag appears for a release
// whose tag does not exist yet. That a draft created this way stays
// untagged until it is published, and only then gets its tag on target, is
// an assumption release-me makes, not documented behaviour, and it has not
// been verified against a live repository either. Searched without finding
// it stated: the REST reference for creating and for updating a release,
// the public GraphQL schema, which exposes no release mutation at all, the
// documentation pages "About releases" and "Managing releases in a
// repository", the immutable releases page, the GitHub Changelog and the
// gh CLI manual. The only direct statement of it found anywhere is a
// community forum post with no staff answer, so a later reader does not
// need to repeat that search.
//
// The "force-tag-creation: false" path therefore does not rely on the
// assumption holding. A published release whose tag never appeared is
// caught by unpublishedDeferredRelease in cmd/release-me, which holds the
// next release pull request back and names the missing tag instead of
// releasing the same version a second time.
func (f *Forge) CreateRelease(
	ctx context.Context, owner, repo, tag, target, name, body string, prerelease, draft bool,
) (forge.Release, error) {
	req := gogithub.CreateReleaseRequest{
		TagName:    tag,
		Name:       &name,
		Body:       &body,
		Draft:      &draft,
		Prerelease: &prerelease,
	}
	if target != "" {
		req.TargetCommitish = &target
	}
	created, resp, err := f.client.Repositories.CreateRelease(ctx, owner, repo, req)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnprocessableEntity && releaseAlreadyExists(err) {
			return forge.Release{}, forge.ErrReleaseExists
		}
		return forge.Release{}, fmt.Errorf("github: create release %s: %w", tag, err)
	}
	return toRelease(created), nil
}

// toRelease converts a go-github release to forge.Release.
func toRelease(r *gogithub.RepositoryRelease) forge.Release {
	return forge.Release{Draft: r.GetDraft(), HTMLURL: r.GetHTMLURL(), UploadURL: r.GetUploadURL()}
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

// Release implements forge.Forge. It lists the repository's releases
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
func (f *Forge) Release(ctx context.Context, owner, repo, tag string) (forge.Release, error) {
	opts := &gogithub.ListOptions{PerPage: 100}
	for {
		releases, resp, err := f.client.Repositories.ListReleases(ctx, owner, repo, opts)
		if err != nil {
			return forge.Release{}, fmt.Errorf("github: list releases for %s/%s: %w", owner, repo, err)
		}
		for _, r := range releases {
			if r.GetTagName() == tag {
				return toRelease(r), nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return forge.Release{}, forge.ErrReleaseNotFound
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

// CommitURL implements forge.Forge: "<web URL>/<owner>/<repo>/commit/<sha>",
// on the same web host CompareURL links.
func (f *Forge) CommitURL(owner, repo, sha string) string {
	return fmt.Sprintf("%s/%s/%s/commit/%s", webURL(f.client.BaseURL()), owner, repo, sha)
}

// IssueURL implements forge.Forge: "<web URL>/<owner>/<repo>/issues/<number>",
// on the same web host CompareURL links. GitHub redirects it to the pull
// request when number is one.
func (f *Forge) IssueURL(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s/%s/issues/%d", webURL(f.client.BaseURL()), owner, repo, number)
}

// GenerateReleaseNotes implements forge.Forge through GitHub's
// "generate release notes" endpoint, the notes GitHub's own release form
// fills in: it reads, never writes, though it is a POST.
func (f *Forge) GenerateReleaseNotes(ctx context.Context, owner, repo, tag, previousTag, target string) (string, error) {
	req := gogithub.GenerateNotesRequest{TagName: tag, TargetCommitish: &target}
	if previousTag != "" {
		req.PreviousTagName = &previousTag
	}
	notes, _, err := f.client.Repositories.GenerateReleaseNotes(ctx, owner, repo, req)
	if err != nil {
		return "", fmt.Errorf("github: generate release notes for %s: %w", tag, err)
	}
	return notes.Body, nil
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
		Number: pr.GetNumber(),
		Title:  pr.GetTitle(),
		// GitHub's web editor can save an entire pull request body with CRLF
		// line endings. releasepr's notesBlock and versionBlock regexes
		// anchor each fence line on "\n" only, so a body saved that way
		// would otherwise lose its release-me-notes or release-me-version
		// block without any error. Normalizing here, the one place a GitHub
		// pull request's body enters this tool, means no reader downstream
		// has to remember this on its own.
		Body:    normalizeLineEndings(pr.GetBody()),
		Labels:  labelNames(pr.Labels),
		URL:     pr.GetHTMLURL(),
		Merged:  pr.GetMerged() || !pr.GetMergedAt().IsZero(),
		HeadSHA: pr.GetHead().GetSHA(),
		Author:  pr.GetUser().GetLogin(),
	}
}

// normalizeLineEndings replaces every CRLF line ending in body with a lone
// "\n". GitHub is never observed to store a lone "\r" not preceded by
// "\n", only "\r\n" or "\n", so this one pass covers every case that
// matters.
func normalizeLineEndings(body string) string {
	return strings.ReplaceAll(body, "\r\n", "\n")
}

// labelNames reads the names off a slice of go-github labels.
func labelNames(ls []*gogithub.Label) []string {
	names := make([]string, 0, len(ls))
	for _, l := range ls {
		names = append(names, l.GetName())
	}
	return names
}
