// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package gitlab_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	forgegitlab "github.com/ostendorf-cloud/release-me/internal/forge/gitlab"
)

// newTestForge points a Forge at a local httptest server instead of the
// real GitLab API, so the tests never reach the network. It returns the
// server's base URL alongside the Forge, for tests that assert on a web
// URL the Forge builds from it.
func newTestForge(t *testing.T, mux *http.ServeMux) (*forgegitlab.Forge, string) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return forgegitlab.New(srv.URL, "a-token"), srv.URL
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
			name:   "reads title, body, labels, URL and head SHA",
			number: 5,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{
					"iid": 5,
					"title": "feat: add the next command",
					"description": "adds release-me next",
					"web_url": "https://gitlab.example.com/group/project/-/merge_requests/5",
					"labels": ["release: pending", "feature"],
					"state": "opened",
					"sha": "deadbeef"
				}`)
			},
			want: forge.PullRequest{
				Number:  5,
				Title:   "feat: add the next command",
				Body:    "adds release-me next",
				Labels:  []string{"release: pending", "feature"},
				URL:     "https://gitlab.example.com/group/project/-/merge_requests/5",
				HeadSHA: "deadbeef",
			},
		},
		{
			name:   "returns not found with a fallback URL",
			number: 9,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message": "404 Not found"}`)
			},
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
			mux.HandleFunc(fmt.Sprintf("/api/v4/projects/owner%%2Frepo/merge_requests/%d", tc.number), tc.handler)
			f, baseURL := newTestForge(t, mux)

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
				wantURL := fmt.Sprintf("%s/owner/repo/-/merge_requests/%d", baseURL, tc.number)
				if got.URL != wantURL {
					t.Fatalf("PullRequest() URL = %q, want %q", got.URL, wantURL)
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

func TestPullRequests(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("source_branch"); got != "release-me/main" {
			t.Errorf("source_branch query = %q, want %q", got, "release-me/main")
		}
		if got := r.URL.Query().Get("state"); got != "opened" {
			t.Errorf("state query = %q, want %q", got, "opened")
		}
		fmt.Fprint(w, `[{"iid": 9, "title": "chore(main): release v0.2.0", "state": "opened"}]`)
	})
	f, _ := newTestForge(t, mux)

	got, err := f.PullRequests(context.Background(), "owner", "repo", "release-me/main", "open")
	if err != nil {
		t.Fatalf("PullRequests() error = %v", err)
	}
	want := []forge.PullRequest{{Number: 9, Title: "chore(main): release v0.2.0"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PullRequests() = %+v, want %+v", got, want)
	}
}

func TestPullRequestsFollowsPagination(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	calls := 0
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("X-Next-Page", "2")
			fmt.Fprint(w, `[{"iid": 1, "title": "first"}]`)
		default:
			fmt.Fprint(w, `[{"iid": 2, "title": "second"}]`)
		}
	})
	f, _ := newTestForge(t, mux)

	got, err := f.PullRequests(context.Background(), "owner", "repo", "head", "open")
	if err != nil {
		t.Fatalf("PullRequests() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	want := []forge.PullRequest{{Number: 1, Title: "first"}, {Number: 2, Title: "second"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PullRequests() = %+v, want %+v", got, want)
	}
}

func TestCreatePullRequestCreatesAndLabelsIt(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"iid": 12, "title": "chore(main): release v0.1.0", "description": "Release v0.1.0"}`)
			return
		}
		t.Fatalf("unexpected method %s", r.Method)
	})
	var labeled string
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/merge_requests/12", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode labels body: %v", err)
		}
		labeled = body["add_labels"]
		fmt.Fprint(w, `{"iid": 12}`)
	})
	f, _ := newTestForge(t, mux)

	got, err := f.CreatePullRequest(context.Background(), "owner", "repo", "main", "release-me/main",
		"chore(main): release v0.1.0", "Release v0.1.0", []string{"release: pending"})
	if err != nil {
		t.Fatalf("CreatePullRequest() error = %v", err)
	}
	want := forge.PullRequest{Number: 12, Title: "chore(main): release v0.1.0", Body: "Release v0.1.0", Labels: []string{"release: pending"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CreatePullRequest() = %+v, want %+v", got, want)
	}
	if labeled != "release: pending" {
		t.Fatalf("labeled = %q, want %q", labeled, "release: pending")
	}
}

func TestUpdatePullRequestEditsWithoutTouchingLabels(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/merge_requests/12", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if _, ok := body["labels"]; ok {
			t.Fatal("UpdatePullRequest() must not touch labels")
		}
		if _, ok := body["add_labels"]; ok {
			t.Fatal("UpdatePullRequest() must not touch labels")
		}
		fmt.Fprint(w, `{"iid": 12, "title": "chore(main): release v0.2.0", "description": "Release v0.2.0", "labels": ["release: pending"]}`)
	})
	f, _ := newTestForge(t, mux)

	got, err := f.UpdatePullRequest(context.Background(), "owner", "repo", 12, "chore(main): release v0.2.0", "Release v0.2.0")
	if err != nil {
		t.Fatalf("UpdatePullRequest() error = %v", err)
	}
	want := forge.PullRequest{Number: 12, Title: "chore(main): release v0.2.0", Body: "Release v0.2.0", Labels: []string{"release: pending"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpdatePullRequest() = %+v, want %+v", got, want)
	}
}

func TestLabels(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/merge_requests/12", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"iid": 12, "labels": ["release: pending", "priority: high"]}`)
	})
	f, _ := newTestForge(t, mux)

	got, err := f.Labels(context.Background(), "owner", "repo", 12)
	if err != nil {
		t.Fatalf("Labels() error = %v", err)
	}
	if want := []string{"release: pending", "priority: high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Labels() = %v, want %v", got, want)
	}
}

func TestAddLabelsAddsWithoutReplacing(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var added string
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/merge_requests/12", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		added = body["add_labels"]
		fmt.Fprint(w, `{"iid": 12}`)
	})
	f, _ := newTestForge(t, mux)

	if err := f.AddLabels(context.Background(), "owner", "repo", 12, []string{"release: pending", "feature"}); err != nil {
		t.Fatalf("AddLabels() error = %v", err)
	}
	if want := "release: pending,feature"; added != want {
		t.Fatalf("added = %q, want %q", added, want)
	}
}

func TestSetLabelsReplacesTheFullSet(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var replaced string
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/merge_requests/12", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		replaced = body["labels"]
		fmt.Fprint(w, `{"iid": 12}`)
	})
	f, _ := newTestForge(t, mux)

	if err := f.SetLabels(context.Background(), "owner", "repo", 12, []string{"release: tagged"}); err != nil {
		t.Fatalf("SetLabels() error = %v", err)
	}
	if want := "release: tagged"; replaced != want {
		t.Fatalf("replaced = %q, want %q", replaced, want)
	}
}

func TestRepositoryLabels(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/labels", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"name": "release: pending"}, {"name": "release: tagged"}]`)
	})
	f, _ := newTestForge(t, mux)

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
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/labels", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message": "401 Unauthorized"}`)
	})
	f, _ := newTestForge(t, mux)

	_, err := f.RepositoryLabels(context.Background(), "owner", "repo")
	if !errors.Is(err, forge.ErrUnauthorized) {
		t.Fatalf("RepositoryLabels() error = %v, want %v", err, forge.ErrUnauthorized)
	}
}

func TestDefaultBranch(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"default_branch": "trunk"}`)
	})
	f, _ := newTestForge(t, mux)

	got, err := f.DefaultBranch(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("DefaultBranch() error = %v", err)
	}
	if got != "trunk" {
		t.Fatalf("DefaultBranch() = %q, want %q", got, "trunk")
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
			name: "creates the tag",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"name": "v0.1.0"}`)
			},
		},
		{
			name: "returns ErrTagExists when GitLab refuses a duplicate tag",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusMethodNotAllowed)
				fmt.Fprint(w, `{"message": "Tag v0.1.0 already exists"}`)
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
			mux.HandleFunc("/api/v4/projects/owner%2Frepo/repository/tags", tc.handler)
			f, _ := newTestForge(t, mux)

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

func TestCreateRelease(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var body map[string]string
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/releases", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode release body: %v", err)
		}
		fmt.Fprint(w, `{"tag_name": "v0.1.0"}`)
	})
	f, _ := newTestForge(t, mux)

	if err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0", "v0.1.0", "Release v0.1.0"); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if body["tag_name"] != "v0.1.0" || body["description"] != "Release v0.1.0" {
		t.Fatalf("body = %v", body)
	}
}

func TestCreateReleaseReturnsErrReleaseExistsOnADuplicateTag(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/releases", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"message": "Release already exists for this tag"}`)
	})
	f, _ := newTestForge(t, mux)

	err := f.CreateRelease(context.Background(), "owner", "repo", "v0.1.0", "v0.1.0", "Release v0.1.0")
	if !errors.Is(err, forge.ErrReleaseExists) {
		t.Fatalf("CreateRelease() error = %v, want %v", err, forge.ErrReleaseExists)
	}
}

func TestMergeCommitReturnsTheMergeCommitSHA(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo/merge_requests/12", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"iid": 12, "state": "merged", "merge_commit_sha": "deadbeef"}`)
	})
	f, _ := newTestForge(t, mux)

	got, err := f.MergeCommit(context.Background(), "owner", "repo", 12)
	if err != nil {
		t.Fatalf("MergeCommit() error = %v", err)
	}
	if got != "deadbeef" {
		t.Fatalf("MergeCommit() = %q, want %q", got, "deadbeef")
	}
}

func TestNewDefaultsTheBaseURL(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "a-token" {
			t.Errorf("PRIVATE-TOKEN header = %q, want %q", got, "a-token")
		}
		fmt.Fprint(w, `{"default_branch": "main"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	f := forgegitlab.New(srv.URL, "a-token")
	if _, err := f.DefaultBranch(context.Background(), "owner", "repo"); err != nil {
		t.Fatalf("DefaultBranch() error = %v", err)
	}
}

func TestNewSendsNoTokenHeaderWhenEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/owner%2Frepo", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "" {
			t.Errorf("PRIVATE-TOKEN header = %q, want empty", got)
		}
		fmt.Fprint(w, `{"default_branch": "main"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	f := forgegitlab.New(srv.URL, "")
	if _, err := f.DefaultBranch(context.Background(), "owner", "repo"); err != nil {
		t.Fatalf("DefaultBranch() error = %v", err)
	}
}
