// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package github_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
		expectedHead string
		createRef    int
		getRef       int
		getRefSHA    string
		patchRef     int
		createCommit int
		wantErrIs    error
		wantErrHas   string
		wantForce    any
		wantPatch    bool
	}{
		{
			name:      "creates a fresh branch on the new commit",
			createRef: http.StatusCreated,
		},
		{
			name:      "fast-forwards an existing branch with no expected head",
			createRef: http.StatusUnprocessableEntity,
			patchRef:  http.StatusOK,
			wantPatch: true,
			wantForce: false,
		},
		{
			name:       "refuses to rewind an existing branch with no expected head",
			createRef:  http.StatusUnprocessableEntity,
			patchRef:   http.StatusUnprocessableEntity,
			wantPatch:  true,
			wantForce:  false,
			wantErrIs:  forge.ErrBranchMoved,
			wantErrHas: "release-me/main",
		},
		{
			name:         "replaces the branch while it is at the expected head",
			expectedHead: oldHead,
			getRef:       http.StatusOK,
			getRefSHA:    oldHead,
			patchRef:     http.StatusOK,
			wantPatch:    true,
			wantForce:    true,
		},
		{
			name:         "leaves a branch that moved past the expected head",
			expectedHead: oldHead,
			getRef:       http.StatusOK,
			getRefSHA:    "5ca1ab1e5ca1ab1e5ca1ab1e5ca1ab1e5ca1ab1e",
			wantErrIs:    forge.ErrBranchMoved,
			wantErrHas:   oldHead,
		},
		{
			name:         "leaves a branch deleted since the expected head was read",
			expectedHead: oldHead,
			getRef:       http.StatusNotFound,
			wantErrIs:    forge.ErrBranchMoved,
			wantErrHas:   "no longer exists",
		},
		{
			name:         "names the branch when the commit cannot be created",
			createCommit: http.StatusInternalServerError,
			wantErrHas:   "branch release-me/main: create commit",
		},
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
			mux.HandleFunc("POST /api/v3/repos/owner/repo/git/refs", func(w http.ResponseWriter, r *http.Request) {
				srv.record(t, r)
				w.WriteHeader(tc.createRef)
				if tc.createRef == http.StatusUnprocessableEntity {
					fmt.Fprint(w, `{"message": "Reference already exists"}`)
					return
				}
				fmt.Fprintf(w, `{"ref": "refs/heads/release-me/main", "object": {"sha": %q}}`, newCommit)
			})
			mux.HandleFunc("GET /api/v3/repos/owner/repo/git/ref/heads/release-me/main", func(w http.ResponseWriter, r *http.Request) {
				srv.record(t, r)
				w.WriteHeader(tc.getRef)
				if tc.getRef == http.StatusNotFound {
					fmt.Fprint(w, `{"message": "Not Found"}`)
					return
				}
				fmt.Fprintf(w, `{"ref": "refs/heads/release-me/main", "object": {"sha": %q}}`, tc.getRefSHA)
			})
			mux.HandleFunc("PATCH "+branchPath, func(w http.ResponseWriter, r *http.Request) {
				srv.record(t, r)
				w.WriteHeader(tc.patchRef)
				if tc.patchRef == http.StatusUnprocessableEntity {
					fmt.Fprint(w, `{"message": "Update is not a fast forward"}`)
					return
				}
				fmt.Fprintf(w, `{"ref": "refs/heads/release-me/main", "object": {"sha": %q}}`, newCommit)
			})
			f := newTestForge(t, mux)

			sha, err := f.CommitBranch(context.Background(), "owner", "repo", forge.BranchCommit{
				Branch:       "release-me/main",
				Parent:       parentSHA,
				Message:      "chore(main): release v0.2.0",
				Files:        files,
				ExpectedHead: tc.expectedHead,
			})

			switch {
			case tc.wantErrIs != nil || tc.wantErrHas != "":
				if err == nil {
					t.Fatalf("CommitBranch() = %s, want an error", sha)
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("CommitBranch() error = %v, want %v", err, tc.wantErrIs)
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

			patches := srv.calls("PATCH " + branchPath)
			if !tc.wantPatch {
				if len(patches) != 0 {
					t.Fatalf("ref updates = %v, want none", patches)
				}
			} else if len(patches) != 1 || patches[0]["sha"] != newCommit || patches[0]["force"] != tc.wantForce {
				t.Fatalf("ref updates = %v, want one to %s with force %v", patches, newCommit, tc.wantForce)
			}
			if creates := srv.calls("POST /api/v3/repos/owner/repo/git/refs"); tc.expectedHead == "" {
				if len(creates) != 1 || creates[0]["ref"] != "refs/heads/release-me/main" || creates[0]["sha"] != newCommit {
					t.Fatalf("ref creates = %v, want one for refs/heads/release-me/main at %s", creates, newCommit)
				}
			} else if len(creates) != 0 {
				t.Fatalf("ref creates = %v, want none with an expected head", creates)
			}
		})
	}
}
