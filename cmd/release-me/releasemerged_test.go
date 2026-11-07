// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"slices"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// releaseMerged runs release against a merged, pending release pull request
// on mainBranch carrying title and body, and checks it creates wantTags.
func releaseMerged(t *testing.T, dir string, cfg config.Config, f *forgetest.Fake, title, body string, wantTags []string) {
	t.Helper()
	merged := forge.PullRequest{Number: 1, Title: title, Body: body, Labels: []string{"release: pending"}, Merged: true}
	f.ListResponses = []forgetest.ListResponse{{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{merged}}}
	f.MergeCommits = map[int]string{1: "deadbeef"}
	f.PRLabels = map[int][]string{1: {"release: pending"}}

	var stdout bytes.Buffer
	if err := doRelease(t.Context(), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
		t.Fatalf("doRelease() error = %v", err)
	}
	var tags []string
	for _, tag := range f.CreatedTags {
		tags = append(tags, tag.Tag)
	}
	if !slices.Equal(tags, wantTags) {
		t.Fatalf("CreatedTags = %v, want %v", tags, wantTags)
	}
}
