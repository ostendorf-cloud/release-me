// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package gitlab implements internal/forge.Forge on top of the GitLab REST
// API v4 with net/http and encoding/json, self hosted instances included.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/forge"
)

// DefaultBaseURL is the base URL applied when New is called with an empty
// one: GitLab's own hosted instance.
const DefaultBaseURL = "https://gitlab.com"

// DefaultTokenEnv is the environment variable holding the GitLab token
// when release-me.yaml leaves token-env unset and the forge is gitlab.
const DefaultTokenEnv = "GITLAB_TOKEN"

// Forge reads and writes merge request, label, tag and release data
// through the GitLab REST API v4.
type Forge struct {
	baseURL string
	token   string
	client  *http.Client
}

// New builds a Forge against baseURL, or DefaultBaseURL when baseURL is
// empty. An empty token makes unauthenticated requests.
func New(baseURL, token string) *Forge {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Forge{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		token:   token,
		client:  http.DefaultClient,
	}
}

// mergeRequest is the subset of the GitLab merge request JSON release-me
// reads.
type mergeRequest struct {
	IID            int      `json:"iid"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Labels         []string `json:"labels"`
	WebURL         string   `json:"web_url"`
	State          string   `json:"state"`
	SHA            string   `json:"sha"`
	MergeCommitSHA string   `json:"merge_commit_sha"`
}

// label is one entry of the GitLab project labels response.
type label struct {
	Name string `json:"name"`
}

// project is the subset of the GitLab project JSON release-me reads.
type project struct {
	DefaultBranch string `json:"default_branch"`
}

// toPullRequest converts a GitLab merge request into a forge.PullRequest.
func toPullRequest(mr mergeRequest) forge.PullRequest {
	return forge.PullRequest{
		Number:  mr.IID,
		Title:   mr.Title,
		Body:    mr.Description,
		Labels:  mr.Labels,
		URL:     mr.WebURL,
		Merged:  mr.State == "merged",
		HeadSHA: mr.SHA,
	}
}

// projectPath is the project's namespace and name, URL encoded as the
// single path segment the GitLab API expects as :id.
func projectPath(owner, repo string) string {
	return url.PathEscape(owner + "/" + repo)
}

// mergeRequestURL builds the web URL of a merge request from its project
// and number, for use as a fallback when the API call that would supply it
// failed.
func (f *Forge) mergeRequestURL(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s/%s/-/merge_requests/%d", f.baseURL, owner, repo, number)
}

// PullRequest implements forge.Forge.
func (f *Forge) PullRequest(ctx context.Context, owner, repo string, number int) (forge.PullRequest, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d", projectPath(owner, repo), number)

	var mr mergeRequest
	resp, err := f.do(ctx, http.MethodGet, path, nil, &mr)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return forge.PullRequest{URL: f.mergeRequestURL(owner, repo, number)}, forge.ErrNotFound
		}
		return forge.PullRequest{}, fmt.Errorf("gitlab: get merge request !%d: %w", number, err)
	}
	return toPullRequest(mr), nil
}

// PullRequests implements forge.Forge.
func (f *Forge) PullRequests(ctx context.Context, owner, repo, head, state string) ([]forge.PullRequest, error) {
	query := url.Values{
		"source_branch": {head},
		"state":         {toGitLabState(state)},
		"per_page":      {"100"},
	}

	var out []forge.PullRequest
	page := "1"
	for page != "" {
		query.Set("page", page)
		path := fmt.Sprintf("/projects/%s/merge_requests?%s", projectPath(owner, repo), query.Encode())

		var mrs []mergeRequest
		resp, err := f.do(ctx, http.MethodGet, path, nil, &mrs)
		if err != nil {
			return nil, fmt.Errorf("gitlab: list merge requests for %s: %w", head, err)
		}
		for _, mr := range mrs {
			out = append(out, toPullRequest(mr))
		}
		page = resp.Header.Get("X-Next-Page")
	}
	return out, nil
}

// toGitLabState maps forge.Forge's "open"/"closed" states to the values the
// GitLab API expects.
func toGitLabState(state string) string {
	if state == "open" {
		return "opened"
	}
	return state
}

// CreatePullRequest implements forge.Forge.
func (f *Forge) CreatePullRequest(ctx context.Context, owner, repo, base, head, title, body string, labels []string) (forge.PullRequest, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests", projectPath(owner, repo))
	payload := map[string]string{
		"source_branch": head,
		"target_branch": base,
		"title":         title,
		"description":   body,
	}

	var mr mergeRequest
	if _, err := f.do(ctx, http.MethodPost, path, payload, &mr); err != nil {
		return forge.PullRequest{}, fmt.Errorf("gitlab: create merge request %s onto %s: %w", head, base, err)
	}
	if len(labels) > 0 {
		if err := f.AddLabels(ctx, owner, repo, mr.IID, labels); err != nil {
			return forge.PullRequest{}, err
		}
	}
	result := toPullRequest(mr)
	result.Labels = labels
	return result, nil
}

// UpdatePullRequest implements forge.Forge.
func (f *Forge) UpdatePullRequest(ctx context.Context, owner, repo string, number int, title, body string) (forge.PullRequest, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d", projectPath(owner, repo), number)
	payload := map[string]string{"title": title, "description": body}

	var mr mergeRequest
	if _, err := f.do(ctx, http.MethodPut, path, payload, &mr); err != nil {
		return forge.PullRequest{}, fmt.Errorf("gitlab: update merge request !%d: %w", number, err)
	}
	return toPullRequest(mr), nil
}

// Labels implements forge.Forge.
func (f *Forge) Labels(ctx context.Context, owner, repo string, number int) ([]string, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d", projectPath(owner, repo), number)

	var mr mergeRequest
	if _, err := f.do(ctx, http.MethodGet, path, nil, &mr); err != nil {
		return nil, fmt.Errorf("gitlab: list labels for merge request !%d: %w", number, err)
	}
	return mr.Labels, nil
}

// AddLabels implements forge.Forge.
func (f *Forge) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d", projectPath(owner, repo), number)
	payload := map[string]string{"add_labels": strings.Join(labels, ",")}

	if _, err := f.do(ctx, http.MethodPut, path, payload, nil); err != nil {
		return fmt.Errorf("gitlab: add labels to merge request !%d: %w", number, err)
	}
	return nil
}

// SetLabels implements forge.Forge.
func (f *Forge) SetLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d", projectPath(owner, repo), number)
	payload := map[string]string{"labels": strings.Join(labels, ",")}

	if _, err := f.do(ctx, http.MethodPut, path, payload, nil); err != nil {
		return fmt.Errorf("gitlab: set labels for merge request !%d: %w", number, err)
	}
	return nil
}

// RepositoryLabels implements forge.Forge.
func (f *Forge) RepositoryLabels(ctx context.Context, owner, repo string) ([]string, error) {
	var out []string
	page := "1"
	for page != "" {
		path := fmt.Sprintf("/projects/%s/labels?per_page=100&page=%s", projectPath(owner, repo), page)

		var ls []label
		resp, err := f.do(ctx, http.MethodGet, path, nil, &ls)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusUnauthorized {
				return nil, fmt.Errorf("gitlab: list repository labels: %w: %w", forge.ErrUnauthorized, err)
			}
			return nil, fmt.Errorf("gitlab: list repository labels: %w", err)
		}
		for _, l := range ls {
			out = append(out, l.Name)
		}
		page = resp.Header.Get("X-Next-Page")
	}
	return out, nil
}

// DefaultBranch implements forge.Forge.
func (f *Forge) DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	path := fmt.Sprintf("/projects/%s", projectPath(owner, repo))

	var p project
	if _, err := f.do(ctx, http.MethodGet, path, nil, &p); err != nil {
		return "", fmt.Errorf("gitlab: get project %s/%s: %w", owner, repo, err)
	}
	return p.DefaultBranch, nil
}

// CreateTag implements forge.Forge.
func (f *Forge) CreateTag(ctx context.Context, owner, repo, tag, sha string) error {
	path := fmt.Sprintf("/projects/%s/repository/tags", projectPath(owner, repo))
	payload := map[string]string{"tag_name": tag, "ref": sha}

	resp, err := f.do(ctx, http.MethodPost, path, payload, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusMethodNotAllowed {
			return forge.ErrTagExists
		}
		return fmt.Errorf("gitlab: create tag %s: %w", tag, err)
	}
	return nil
}

// CreateRelease implements forge.Forge.
func (f *Forge) CreateRelease(ctx context.Context, owner, repo, tag, name, body string) error {
	path := fmt.Sprintf("/projects/%s/releases", projectPath(owner, repo))
	payload := map[string]string{"tag_name": tag, "name": name, "description": body}

	resp, err := f.do(ctx, http.MethodPost, path, payload, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusBadRequest {
			return forge.ErrReleaseExists
		}
		return fmt.Errorf("gitlab: create release %s: %w", tag, err)
	}
	return nil
}

// MergeCommit implements forge.Forge.
func (f *Forge) MergeCommit(ctx context.Context, owner, repo string, number int) (string, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d", projectPath(owner, repo), number)

	var mr mergeRequest
	if _, err := f.do(ctx, http.MethodGet, path, nil, &mr); err != nil {
		return "", fmt.Errorf("gitlab: get merge request !%d: %w", number, err)
	}
	return mr.MergeCommitSHA, nil
}

// do sends one GitLab API request and decodes a JSON response into out when
// the status is 2xx. The returned response is non-nil even when err is set,
// so a caller can inspect its status code for a status-specific mapping;
// the token never reaches the returned error.
func (f *Forge) do(ctx context.Context, method, path string, body, out any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("gitlab: encode request to %s: %w", path, err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, f.baseURL+"/api/v4"+path, reader)
	if err != nil {
		return nil, fmt.Errorf("gitlab: build request to %s: %w", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if f.token != "" {
		req.Header.Set("PRIVATE-TOKEN", f.token)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: request to %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, fmt.Errorf("gitlab: read response from %s: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, fmt.Errorf("gitlab: %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp, fmt.Errorf("gitlab: decode response from %s: %w", path, err)
		}
	}
	return resp, nil
}
