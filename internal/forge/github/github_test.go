// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v93/github"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	forgegithub "github.com/ostendorf-cloud/release-me/internal/forge/github"
	"github.com/ostendorf-cloud/release-me/internal/release"
)

// newTestForge points a Forge at a local httptest server instead of the real
// GitHub API, so the tests never reach the network.
func newTestForge(t *testing.T, mux *http.ServeMux) *forgegithub.Forge {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	f, err := forgegithub.New("", gogithub.WithEnterpriseURLs(srv.URL, srv.URL))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return f
}

func TestPullRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		number    int
		handler   http.HandlerFunc
		want      forge.PullRequest
		wantErrIs error
		wantErr   bool
	}{
		{
			name:   "reads title, body, labels and URL",
			number: 5,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{
					"title": "feat(cli): add the next command",
					"body": "adds release-me next",
					"html_url": "https://github.com/owner/repo/pull/5",
					"labels": [{"name": "release: pending"}, {"name": "feature"}]
				}`)
			},
			want: forge.PullRequest{
				Number: 5,
				Title:  "feat(cli): add the next command",
				Body:   "adds release-me next",
				Labels: []string{"release: pending", "feature"},
				URL:    "https://github.com/owner/repo/pull/5",
			},
		},
		{
			name:   "returns not found with a fallback URL",
			number: 9,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message": "Not Found"}`)
			},
			want:      forge.PullRequest{URL: "https://github.com/owner/repo/pull/9"},
			wantErrIs: forge.ErrNotFound,
		},
		{
			name:   "wraps a server error",
			number: 3,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc(fmt.Sprintf("/api/v3/repos/owner/repo/pulls/%d", tc.number), tc.handler)
			f := newTestForge(t, mux)

			got, err := f.PullRequest(context.Background(), "owner", "repo", tc.number)
			switch {
			case tc.wantErr:
				if err == nil {
					t.Fatal("PullRequest() error = nil, want an error")
				}
				if errors.Is(err, forge.ErrNotFound) {
					t.Fatal("PullRequest() error wraps forge.ErrNotFound, want a plain error")
				}
			case tc.wantErrIs != nil:
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("PullRequest() error = %v, want %v", err, tc.wantErrIs)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("PullRequest() = %+v, want %+v", got, tc.want)
				}
			default:
				if err != nil {
					t.Fatalf("PullRequest() error = %v", err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("PullRequest() = %+v, want %+v", got, tc.want)
				}
			}
		})
	}
}

// TestPullRequests checks the list endpoint's actual shape, as go-github
// documents it: no merged field, a merge timestamp on the merged pull
// request and none on the closed-but-unmerged one.
func TestPullRequests(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("head"); got != "owner:release-me/main" {
			t.Errorf("head query = %q, want %q", got, "owner:release-me/main")
		}
		if got := r.URL.Query().Get("state"); got != "closed" {
			t.Errorf("state query = %q, want %q", got, "closed")
		}
		fmt.Fprint(w, `[
			{"number": 9, "title": "chore(main): release v0.2.0"},
			{"number": 7, "title": "chore(main): release v0.1.0", "merged_at": "2026-01-02T03:04:05Z"}
		]`)
	})
	f := newTestForge(t, mux)

	got, err := f.PullRequests(context.Background(), "owner", "repo", "release-me/main", "closed")
	if err != nil {
		t.Fatalf("PullRequests() error = %v", err)
	}
	want := []forge.PullRequest{
		{Number: 9, Title: "chore(main): release v0.2.0", Labels: []string{}},
		{Number: 7, Title: "chore(main): release v0.1.0", Labels: []string{}, Merged: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PullRequests() = %+v, want %+v", got, want)
	}
}

// TestPullRequestsListedMergeTimestampDrivesRelease drives release.Run
// against the real forge instead of forgetest.Fake, with a list response
// shaped like GitHub's: it carries merged_at and no merged field. Before the
// fix, PullRequests mapped Merged to false for every one of these, so Run
// skipped the pull request and released nothing.
func TestPullRequestsListedMergeTimestampDrivesRelease(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("head"); got != "owner:release-me/main" {
			t.Errorf("head query = %q, want %q", got, "owner:release-me/main")
		}
		if got := r.URL.Query().Get("state"); got != "closed" {
			t.Errorf("state query = %q, want %q", got, "closed")
		}
		fmt.Fprint(w, `[{
			"number": 19,
			"title": "chore(main): release v0.1.0-rc.1",
			"body": "Release v0.1.0-rc.1\n\n<details>\n<summary>Changelog</summary>\n\n## v0.1.0-rc.1 (2026-01-02)\n\n### Features\n\n- add the thing\n\n</details>\n",
			"labels": [{"name": "release: pending"}, {"name": "release: rc"}],
			"html_url": "https://github.com/owner/repo/pull/19",
			"merged_at": "2026-01-02T03:04:05Z",
			"head": {"sha": "cafefeed"}
		}]`)
	})
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls/19", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"number": 19, "merged": true, "merge_commit_sha": "deadbeef"}`)
	})
	var replaced []string
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/19/labels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			if err := json.NewDecoder(r.Body).Decode(&replaced); err != nil {
				t.Fatalf("decode labels body: %v", err)
			}
		}
		fmt.Fprint(w, `[{"name": "release: pending"}, {"name": "release: rc"}]`)
	})
	var createdTag map[string]any
	mux.HandleFunc("/api/v3/repos/owner/repo/git/refs", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&createdTag); err != nil {
			t.Fatalf("decode tag body: %v", err)
		}
		fmt.Fprint(w, `{"ref": "refs/tags/v0.1.0-rc.1", "object": {"sha": "deadbeef"}}`)
	})
	mux.HandleFunc("/api/v3/repos/owner/repo/releases", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"id": 1, "tag_name": "v0.1.0-rc.1"}`)
	})
	f := newTestForge(t, mux)

	comp := release.Component{Name: "main", Branch: "release-me/main", TagPrefix: "v"}
	labels := release.Labels{
		Pending: "release: pending", Tagged: "release: tagged",
		Alpha: "release: alpha", Beta: "release: beta", Rc: "release: rc",
	}

	results, err := release.Run(context.Background(), f, "owner", "repo", comp, labels, false, false, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("release.Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Tag != "v0.1.0-rc.1" || results[0].Skipped {
		t.Fatalf("release.Run() = %+v", results)
	}
	if createdTag["sha"] != "deadbeef" {
		t.Fatalf("created tag = %v, want sha deadbeef", createdTag)
	}
	if want := []string{"release: tagged"}; !reflect.DeepEqual(replaced, want) {
		t.Fatalf("replaced labels = %v, want %v", replaced, want)
	}
}

func TestCreatePullRequestCreatesAndLabelsIt(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"number": 12, "title": "chore(main): release v0.1.0", "body": "Release v0.1.0"}`)
	})
	var labeled []string
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/12/labels", func(w http.ResponseWriter, r *http.Request) {
		var body []string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode labels body: %v", err)
		}
		labeled = body
		fmt.Fprint(w, `[]`)
	})
	f := newTestForge(t, mux)

	got, err := f.CreatePullRequest(context.Background(), "owner", "repo", "main", "release-me/main",
		"chore(main): release v0.1.0", "Release v0.1.0", []string{"release: pending"})
	if err != nil {
		t.Fatalf("CreatePullRequest() error = %v", err)
	}
	want := forge.PullRequest{Number: 12, Title: "chore(main): release v0.1.0", Body: "Release v0.1.0", Labels: []string{"release: pending"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CreatePullRequest() = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(labeled, []string{"release: pending"}) {
		t.Fatalf("labeled = %v, want %v", labeled, []string{"release: pending"})
	}
}

func TestUpdatePullRequestEditsWithoutTouchingLabels(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls/12", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"number": 12, "title": "chore(main): release v0.2.0", "body": "Release v0.2.0", "labels": [{"name": "release: pending"}]}`)
	})
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/12/labels", func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("UpdatePullRequest() must not touch labels")
	})
	f := newTestForge(t, mux)

	got, err := f.UpdatePullRequest(context.Background(), "owner", "repo", 12, "chore(main): release v0.2.0", "Release v0.2.0")
	if err != nil {
		t.Fatalf("UpdatePullRequest() error = %v", err)
	}
	want := forge.PullRequest{Number: 12, Title: "chore(main): release v0.2.0", Body: "Release v0.2.0", Labels: []string{"release: pending"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpdatePullRequest() = %+v, want %+v", got, want)
	}
}

func TestAddLabelsAddsToTheIssue(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var added []string
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/12/labels", func(w http.ResponseWriter, r *http.Request) {
		var body []string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode labels body: %v", err)
		}
		added = body
		fmt.Fprint(w, `[]`)
	})
	f := newTestForge(t, mux)

	if err := f.AddLabels(context.Background(), "owner", "repo", 12, []string{"release: pending"}); err != nil {
		t.Fatalf("AddLabels() error = %v", err)
	}
	if want := []string{"release: pending"}; !reflect.DeepEqual(added, want) {
		t.Fatalf("added = %v, want %v", added, want)
	}
}

func TestDefaultBranch(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"default_branch": "trunk"}`)
	})
	f := newTestForge(t, mux)

	got, err := f.DefaultBranch(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("DefaultBranch() error = %v", err)
	}
	if got != "trunk" {
		t.Fatalf("DefaultBranch() = %q, want %q", got, "trunk")
	}
}

func TestLabels(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/12/labels", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"name": "release: pending"}, {"name": "priority: high"}]`)
	})
	f := newTestForge(t, mux)

	got, err := f.Labels(context.Background(), "owner", "repo", 12)
	if err != nil {
		t.Fatalf("Labels() error = %v", err)
	}
	if want := []string{"release: pending", "priority: high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Labels() = %v, want %v", got, want)
	}
}

func TestSetLabelsReplacesTheFullSet(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var replaced []string
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/12/labels", func(w http.ResponseWriter, r *http.Request) {
		var body []string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode labels body: %v", err)
		}
		replaced = body
		fmt.Fprint(w, `[]`)
	})
	f := newTestForge(t, mux)

	if err := f.SetLabels(context.Background(), "owner", "repo", 12, []string{"release: tagged"}); err != nil {
		t.Fatalf("SetLabels() error = %v", err)
	}
	if want := []string{"release: tagged"}; !reflect.DeepEqual(replaced, want) {
		t.Fatalf("replaced = %v, want %v", replaced, want)
	}
}

func TestRepositoryLabels(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/labels", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"name": "release: pending"}, {"name": "release: tagged"}]`)
	})
	f := newTestForge(t, mux)

	got, err := f.RepositoryLabels(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("RepositoryLabels() error = %v", err)
	}
	if want := []string{"release: pending", "release: tagged"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RepositoryLabels() = %v, want %v", got, want)
	}
}

func TestRepositoryLabelsReturnsErrUnauthorizedOnA401(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/labels", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message": "Bad credentials"}`)
	})
	f := newTestForge(t, mux)

	_, err := f.RepositoryLabels(context.Background(), "owner", "repo")
	if !errors.Is(err, forge.ErrUnauthorized) {
		t.Fatalf("RepositoryLabels() error = %v, want %v", err, forge.ErrUnauthorized)
	}
}

func TestCreateTag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		handler   http.HandlerFunc
		wantErrIs error
		wantErr   bool
	}{
		{
			name: "creates the tag reference",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"ref": "refs/tags/v0.1.0", "object": {"sha": "deadbeef"}}`)
			},
		},
		{
			name: "returns ErrTagExists when GitHub refuses a duplicate ref",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				fmt.Fprint(w, `{"message": "Reference already exists"}`)
			},
			wantErrIs: forge.ErrTagExists,
		},
		{
			name: "wraps a server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v3/repos/owner/repo/git/refs", tc.handler)
			f := newTestForge(t, mux)

			err := f.CreateTag(context.Background(), "owner", "repo", "v0.1.0", "deadbeef")
			switch {
			case tc.wantErrIs != nil:
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("CreateTag() error = %v, want %v", err, tc.wantErrIs)
				}
			case tc.wantErr:
				if err == nil {
					t.Fatal("CreateTag() error = nil, want an error")
				}
			default:
				if err != nil {
					t.Fatalf("CreateTag() error = %v", err)
				}
			}
		})
	}
}

func TestTagExists(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    bool
		wantErr bool
	}{
		{
			name: "reports true for a tag that exists",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"ref": "refs/tags/v1", "object": {"sha": "deadbeef"}}`)
			},
			want: true,
		},
		{
			name: "reports false for a tag that does not exist",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message": "Not Found"}`)
			},
		},
		{
			name: "wraps a server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v3/repos/owner/repo/git/ref/tags/v1", tc.handler)
			f := newTestForge(t, mux)

			got, err := f.TagExists(context.Background(), "owner", "repo", "v1")
			if tc.wantErr {
				if err == nil {
					t.Fatal("TagExists() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("TagExists() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("TagExists() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMoveTagForceUpdatesTheRef(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var body map[string]any
	mux.HandleFunc("/api/v3/repos/owner/repo/git/refs/tags/v1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		fmt.Fprint(w, `{"ref": "refs/tags/v1", "object": {"sha": "deadbeef"}}`)
	})
	f := newTestForge(t, mux)

	if err := f.MoveTag(context.Background(), "owner", "repo", "v1", "deadbeef"); err != nil {
		t.Fatalf("MoveTag() error = %v", err)
	}
	if body["sha"] != "deadbeef" || body["force"] != true {
		t.Fatalf("body = %v, want sha deadbeef and force true", body)
	}
}

func TestMoveTagWrapsAServerError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/git/refs/tags/v1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message": "Reference does not exist"}`)
	})
	f := newTestForge(t, mux)

	if err := f.MoveTag(context.Background(), "owner", "repo", "v1", "deadbeef"); err == nil {
		t.Fatal("MoveTag() error = nil, want an error")
	}
}

func TestCreateReleaseCreatesADraftRelease(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var body map[string]any
	mux.HandleFunc("/api/v3/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode release body: %v", err)
		}
		fmt.Fprint(w, `{"id": 1, "tag_name": "v0.1.0"}`)
	})
	f := newTestForge(t, mux)

	if err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0", "v0.1.0", "Release v0.1.0", false, true); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	// draft-until-assets-attach is on: immutable releases rejects any
	// later change to a published release, assets included, and a
	// draft is exempt from that, so the caller asks CreateRelease to
	// hold it back until assets are ready to attach.
	if body["draft"] != true || body["prerelease"] != false {
		t.Fatalf("body = %v, want draft true and prerelease false", body)
	}
	if body["tag_name"] != "v0.1.0" || body["body"] != "Release v0.1.0" {
		t.Fatalf("body = %v", body)
	}
}

func TestCreateReleasePublishesImmediatelyWhenDraftIsFalse(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var body map[string]any
	mux.HandleFunc("/api/v3/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode release body: %v", err)
		}
		fmt.Fprint(w, `{"id": 1, "tag_name": "v0.1.0"}`)
	})
	f := newTestForge(t, mux)

	// draft-until-assets-attach is off, the default: CreateRelease
	// publishes immediately, exactly as release-me did before that key
	// existed, so a repository that never turns it on sees no behavior
	// change at all.
	if err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0", "v0.1.0", "Release v0.1.0", false, false); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if body["draft"] != false || body["prerelease"] != false {
		t.Fatalf("body = %v, want draft false and prerelease false", body)
	}
}

func TestCreateReleaseMarksAPreReleaseVersionAsAPreReleaseDraft(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var body map[string]any
	mux.HandleFunc("/api/v3/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode release body: %v", err)
		}
		fmt.Fprint(w, `{"id": 1, "tag_name": "v0.1.0-alpha.1"}`)
	})
	f := newTestForge(t, mux)

	if err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0-alpha.1", "v0.1.0-alpha.1", "Release v0.1.0-alpha.1", true, true); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if body["draft"] != true || body["prerelease"] != true {
		t.Fatalf("body = %v, want draft true and prerelease true", body)
	}
}

func TestCreateReleaseMarksAPreReleaseVersionAsAPreReleaseWhenDraftIsFalse(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var body map[string]any
	mux.HandleFunc("/api/v3/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode release body: %v", err)
		}
		fmt.Fprint(w, `{"id": 1, "tag_name": "v0.1.0-alpha.1"}`)
	})
	f := newTestForge(t, mux)

	if err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0-alpha.1", "v0.1.0-alpha.1", "Release v0.1.0-alpha.1", true, false); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if body["draft"] != false || body["prerelease"] != true {
		t.Fatalf("body = %v, want draft false and prerelease true", body)
	}
}

// TestCreateReleaseTellsAGenuineDuplicateApartFromAnImmutableRelease pins
// CreateRelease's mapping of a 422 response against the two real GitHub
// response bodies for POST /repos/{owner}/{repo}/releases: a release that
// already exists at the tag right now, which is recoverable, and a tag
// name that was ever used by a now-deleted immutable release, which is
// permanent and can never host a release.
func TestCreateReleaseTellsAGenuineDuplicateApartFromAnImmutableRelease(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		body          string
		wantErrExists bool
	}{
		{
			name:          "a release already exists at the tag right now",
			body:          `{"message":"Validation Failed","errors":[{"resource":"Release","field":"tag_name","code":"already_exists","message":"already_exists"}],"documentation_url":"https://docs.github.com/rest/releases/releases#create-a-release"}`,
			wantErrExists: true,
		},
		{
			name:          "the tag name was used by a now-deleted immutable release",
			body:          `{"message":"Validation Failed","errors":[{"resource":"Release","field":"tag_name","code":"custom","message":"tag_name was used by an immutable release"}],"documentation_url":"https://docs.github.com/rest/releases/releases#create-a-release"}`,
			wantErrExists: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v3/repos/owner/repo/releases", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				fmt.Fprint(w, tc.body)
			})
			f := newTestForge(t, mux)

			err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.1", "v0.1.1", "Release v0.1.1", false, false)
			if err == nil {
				t.Fatalf("CreateRelease() error = nil, want an error")
			}
			if got := errors.Is(err, forge.ErrReleaseExists); got != tc.wantErrExists {
				t.Fatalf("CreateRelease() errors.Is(err, ErrReleaseExists) = %v, want %v (err = %v)", got, tc.wantErrExists, err)
			}
			if !tc.wantErrExists {
				var errResp *gogithub.ErrorResponse
				if !errors.As(err, &errResp) {
					t.Fatalf("CreateRelease() error = %v, want it to wrap a *gogithub.ErrorResponse", err)
				}
				if len(errResp.Errors) != 1 || errResp.Errors[0].Message != "tag_name was used by an immutable release" {
					t.Fatalf("CreateRelease() error's wrapped Errors = %+v, want GitHub's validation message", errResp.Errors)
				}
			}
		})
	}
}

func TestIsReleaseDraft(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		tag       string
		handler   http.HandlerFunc
		want      bool
		wantErrIs error
		wantErr   bool
	}{
		{
			name: "reports true for a draft release, which get-by-tag alone would 404 on",
			tag:  "v0.1.0",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `[{"tag_name": "v0.1.0", "draft": true}]`)
			},
			want: true,
		},
		{
			name: "reports false for an already published release",
			tag:  "v0.1.0",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `[{"tag_name": "v0.1.0", "draft": false}]`)
			},
			want: false,
		},
		{
			name: "returns ErrReleaseNotFound when no release matches the tag",
			tag:  "v0.9.0",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `[{"tag_name": "v0.1.0", "draft": true}]`)
			},
			wantErrIs: forge.ErrReleaseNotFound,
		},
		{
			name: "wraps a server error",
			tag:  "v0.1.0",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v3/repos/owner/repo/releases", tc.handler)
			f := newTestForge(t, mux)

			got, err := f.IsReleaseDraft(context.Background(), "owner", "repo", tc.tag)
			switch {
			case tc.wantErrIs != nil:
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("IsReleaseDraft() error = %v, want %v", err, tc.wantErrIs)
				}
			case tc.wantErr:
				if err == nil {
					t.Fatal("IsReleaseDraft() error = nil, want an error")
				}
			default:
				if err != nil {
					t.Fatalf("IsReleaseDraft() error = %v", err)
				}
				if got != tc.want {
					t.Fatalf("IsReleaseDraft() = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestMergeCommitReturnsTheMergeCommitSHA(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls/12", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"number": 12, "merged": true, "merge_commit_sha": "deadbeef"}`)
	})
	f := newTestForge(t, mux)

	got, err := f.MergeCommit(context.Background(), "owner", "repo", 12)
	if err != nil {
		t.Fatalf("MergeCommit() error = %v", err)
	}
	if got != "deadbeef" {
		t.Fatalf("MergeCommit() = %q, want %q", got, "deadbeef")
	}
}

func TestDeleteBranch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "deletes the branch reference", status: http.StatusNoContent},
		{
			name: "treats a reference that does not exist as already deleted", status: http.StatusUnprocessableEntity,
			body: `{"message": "Reference does not exist", "documentation_url": "https://docs.github.com/rest/git/refs#delete-a-reference"}`,
		},
		{name: "treats a missing reference on a 404 as already deleted", status: http.StatusNotFound, body: `{"message": "Not Found"}`},
		{
			name: "reports a protected branch", status: http.StatusUnprocessableEntity,
			body:    `{"message": "Cannot delete this protected branch"}`,
			wantErr: true,
		},
		{name: "reports a missing permission", status: http.StatusForbidden, body: `{"message": "Resource not accessible by integration"}`, wantErr: true},
		{name: "wraps a server error", status: http.StatusInternalServerError, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			var method string
			mux.HandleFunc("/api/v3/repos/owner/repo/git/refs/heads/release-me/api", func(w http.ResponseWriter, r *http.Request) {
				method = r.Method
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			f := newTestForge(t, mux)

			err := f.DeleteBranch(context.Background(), "owner", "repo", "release-me/api")
			if method != http.MethodDelete {
				t.Fatalf("method = %q, want DELETE", method)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatal("DeleteBranch() error = nil, want an error")
				}
				if !strings.Contains(err.Error(), "release-me/api") {
					t.Fatalf("DeleteBranch() error = %q, want it to name the branch", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeleteBranch() error = %v", err)
			}
		})
	}
}

func TestCreateCommentPostsTheBody(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var posted string
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode comment body: %v", err)
		}
		posted = body.Body
		fmt.Fprint(w, `{"id": 1}`)
	})
	f := newTestForge(t, mux)

	if err := f.CreateComment(context.Background(), "owner", "repo", 7, "forced the next version to 2.0.0."); err != nil {
		t.Fatalf("CreateComment() error = %v", err)
	}
	if posted != "forced the next version to 2.0.0." {
		t.Fatalf("posted = %q, want %q", posted, "forced the next version to 2.0.0.")
	}
}

func TestCreateCommentWrapsAServerError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/7/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	f := newTestForge(t, mux)

	if err := f.CreateComment(context.Background(), "owner", "repo", 7, "hello"); err == nil {
		t.Fatal("CreateComment() error = nil, want an error")
	}
}

func TestNew(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		token string
	}{
		{name: "accepts an empty token for unauthenticated use", token: ""},
		{name: "accepts a token", token: "a-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := forgegithub.New(tc.token); err != nil {
				t.Fatalf("New(%q) error = %v", tc.token, err)
			}
		})
	}
}

func TestCompareURLLinksTheWebHostOfTheAPIBaseURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts []gogithub.ClientOptionsFunc
		want string
	}{
		{
			name: "github.com by default",
			want: "https://github.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0",
		},
		{
			name: "a GitHub Enterprise Server host",
			opts: []gogithub.ClientOptionsFunc{
				gogithub.WithEnterpriseURLs("https://github.example.com/api/v3/", "https://github.example.com/api/uploads/"),
			},
			want: "https://github.example.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0",
		},
		{
			name: "a GitHub Enterprise Cloud host with data residency",
			opts: []gogithub.ClientOptionsFunc{
				gogithub.WithEnterpriseURLs("https://api.octocorp.ghe.com/", "https://uploads.octocorp.ghe.com/"),
			},
			want: "https://octocorp.ghe.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := forgegithub.New("", tc.opts...)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := f.CompareURL("ostendorf-cloud", "duxt", "duxt@v0.8.1", "duxt@v0.9.0"); got != tc.want {
				t.Fatalf("CompareURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
