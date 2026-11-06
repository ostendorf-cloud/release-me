// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package github_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
)

const (
	parentSHA  = "4f1d2c3b5a69788796a5b4c3d2e1f0a9b8c7d6e5"
	parentTree = "9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c"
	newTree    = "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"
	newCommit  = "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"
	oldHead    = "0ddba11e0ddba11e0ddba11e0ddba11e0ddba11e"
	branchPath = "/api/v3/repos/owner/repo/git/refs/heads/release-me/main"
)

// gitDataServer is a GitHub Git Data API stand-in that records every
// request body it is sent, by "METHOD path".
type gitDataServer struct {
	mu     sync.Mutex
	bodies map[string][]map[string]any
	blobs  int
}

func (s *gitDataServer) record(t *testing.T, r *http.Request) {
	t.Helper()
	var body map[string]any
	if r.Body != nil && r.Method != http.MethodGet {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode %s %s: %v", r.Method, r.URL.Path, err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	s.bodies[key] = append(s.bodies[key], body)
}

func (s *gitDataServer) calls(key string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[key]
}

func TestCommitBranch(t *testing.T) {
	t.Parallel()
	files := []forge.CommitFile{
		{Path: "CHANGELOG.md", Content: []byte("# Changelog\n\n## [v0.2.0](https://github.com/owner/repo/compare/v0.1.0...v0.2.0) (2026-10-29)\n")},
		{Path: "scripts/install.sh", Content: []byte("#!/bin/sh\nVERSION=0.2.0 # release-me: version\n"), Executable: true},
	}
	cases := []struct {
		name         string
		createCommit int
		wantErrHas   string
	}{
		{name: "creates signed commit objects without publishing a ref"},
		{name: "names the branch when the commit cannot be created", createCommit: http.StatusInternalServerError, wantErrHas: "branch release-me/main: create commit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := &gitDataServer{bodies: map[string][]map[string]any{}}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v3/repos/owner/repo/git/commits/"+parentSHA, func(w http.ResponseWriter, r *http.Request) {
				srv.record(t, r)
				fmt.Fprintf(w, `{"sha": %q, "tree": {"sha": %q}}`, parentSHA, parentTree)
			})
			mux.HandleFunc("POST /api/v3/repos/owner/repo/git/blobs", func(w http.ResponseWriter, r *http.Request) {
				srv.record(t, r)
				srv.mu.Lock()
				srv.blobs++
				n := srv.blobs
				srv.mu.Unlock()
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintf(w, `{"sha": "%040d"}`, n)
			})
			mux.HandleFunc("POST /api/v3/repos/owner/repo/git/trees", func(w http.ResponseWriter, r *http.Request) {
				srv.record(t, r)
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintf(w, `{"sha": %q}`, newTree)
			})
			mux.HandleFunc("POST /api/v3/repos/owner/repo/git/commits", func(w http.ResponseWriter, r *http.Request) {
				srv.record(t, r)
				if tc.createCommit != 0 {
					w.WriteHeader(tc.createCommit)
					return
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintf(w, `{"sha": %q}`, newCommit)
			})
			f := newTestForge(t, mux)

			sha, err := f.CommitBranch(context.Background(), "owner", "repo", forge.BranchCommit{
				Branch:  "release-me/main",
				Parent:  parentSHA,
				Message: "chore(main): release v0.2.0",
				Files:   files,
			})

			switch {
			case tc.wantErrHas != "":
				if err == nil {
					t.Fatalf("CommitBranch() = %s, want an error", sha)
				}
				if !strings.Contains(err.Error(), tc.wantErrHas) {
					t.Fatalf("CommitBranch() error = %q, want it to contain %q", err, tc.wantErrHas)
				}
			case err != nil:
				t.Fatalf("CommitBranch() error = %v", err)
			case sha != newCommit:
				t.Fatalf("CommitBranch() = %s, want %s", sha, newCommit)
			}

			blobs := srv.calls("POST /api/v3/repos/owner/repo/git/blobs")
			if len(blobs) != len(files) {
				t.Fatalf("blobs = %d, want %d", len(blobs), len(files))
			}
			for i, b := range blobs {
				want := base64.StdEncoding.EncodeToString(files[i].Content)
				if b["content"] != want || b["encoding"] != "base64" {
					t.Fatalf("blob %d = %v, want %s base64 encoded", i, b, files[i].Path)
				}
			}

			trees := srv.calls("POST /api/v3/repos/owner/repo/git/trees")
			if len(trees) != 1 || trees[0]["base_tree"] != parentTree {
				t.Fatalf("trees = %v, want one on base tree %s", trees, parentTree)
			}
			wantEntries := []any{
				map[string]any{"path": "CHANGELOG.md", "mode": "100644", "type": "blob", "sha": fmt.Sprintf("%040d", 1)},
				map[string]any{"path": "scripts/install.sh", "mode": "100755", "type": "blob", "sha": fmt.Sprintf("%040d", 2)},
			}
			if !reflect.DeepEqual(trees[0]["tree"], wantEntries) {
				t.Fatalf("tree entries = %v, want %v", trees[0]["tree"], wantEntries)
			}

			commits := srv.calls("POST /api/v3/repos/owner/repo/git/commits")
			if len(commits) != 1 {
				t.Fatalf("commits = %v, want one", commits)
			}
			wantCommit := map[string]any{
				"message": "chore(main): release v0.2.0",
				"tree":    newTree,
				"parents": []any{parentSHA},
			}
			if !reflect.DeepEqual(commits[0], wantCommit) {
				t.Fatalf("commit = %v, want exactly %v: no author or committer, or GitHub leaves it unsigned", commits[0], wantCommit)
			}
			if tc.createCommit != 0 {
				if n := len(srv.calls("POST /api/v3/repos/owner/repo/git/refs")) + len(srv.calls("PATCH "+branchPath)); n != 0 {
					t.Fatalf("ref writes = %d after a failed commit, want none", n)
				}
				return
			}
		})
	}
}

func TestCommitSignature(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		status     int
		body       string
		want       forge.Signature
		wantErrHas string
	}{
		{
			name:   "reports a commit GitHub signed for an app installation token as verified",
			status: http.StatusOK,
			body:   `{"sha": %q, "commit": {"verification": {"verified": true, "reason": "valid"}}}`,
			want:   forge.Signature{Verified: true, Reason: "valid"},
		},
		{
			name:   "reports a commit a personal access token created as unsigned",
			status: http.StatusOK,
			body:   `{"sha": %q, "commit": {"verification": {"verified": false, "reason": "unsigned", "signature": null, "payload": null}}}`,
			want:   forge.Signature{Verified: false, Reason: "unsigned"},
		},
		{
			name:       "names the commit when its signature cannot be read",
			status:     http.StatusNotFound,
			body:       `{"message": "No commit found for SHA: %s"}`,
			wantErrHas: "read signature of commit " + newCommit,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v3/repos/owner/repo/commits/"+newCommit, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, tc.body, newCommit)
			})
			f := newTestForge(t, mux)

			got, err := f.CommitSignature(context.Background(), "owner", "repo", newCommit)
			if tc.wantErrHas != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrHas) {
					t.Fatalf("CommitSignature() error = %v, want it to contain %q", err, tc.wantErrHas)
				}
				return
			}
			if err != nil {
				t.Fatalf("CommitSignature() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("CommitSignature() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
