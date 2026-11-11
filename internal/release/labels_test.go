// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package release_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/release"
)

// listLabels names two labels per role, the way release-please's
// comma separated label and release-label do.
var listLabels = release.Labels{
	Pending:    []string{"autorelease: pending", "release"},
	Tagged:     []string{"autorelease: tagged", "shipped"},
	PreRelease: map[string]string{"rc": "release: rc"},
}

func TestRunReleasesOnlyAPullRequestCarryingEveryPendingLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		labels     []string
		wantTagged bool
		wantLabels []string
	}{
		{
			name:       "every pending label, a pre-release type and a human's label",
			labels:     []string{"autorelease: pending", "release: rc", "release", "priority: high"},
			wantTagged: true,
			wantLabels: []string{"priority: high", "autorelease: tagged", "shipped"},
		},
		{name: "one pending label of two", labels: []string{"autorelease: pending"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pr := forge.PullRequest{Number: 9, Title: "chore(main): release v1.0.0-rc.1", Labels: tc.labels, Merged: true}
			f := &forgetest.Fake{
				ListResponses: []forgetest.ListResponse{
					{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
				},
				MergeCommits: map[int]string{9: "deadbeef"},
				PRLabels:     map[int][]string{9: tc.labels},
			}
			results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, listLabels, false, false, discardLogger())
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := len(results) == 1; got != tc.wantTagged {
				t.Fatalf("Run() = %+v, want released %v", results, tc.wantTagged)
			}
			if !tc.wantTagged {
				if len(f.CreatedTags) != 0 || len(f.SetLabelsCalls) != 0 {
					t.Fatalf("tags %+v, labels %+v, want nothing written", f.CreatedTags, f.SetLabelsCalls)
				}
				return
			}
			if len(f.SetLabelsCalls) != 1 || !reflect.DeepEqual(f.SetLabelsCalls[0].Labels, tc.wantLabels) {
				t.Fatalf("SetLabelsCalls = %+v, want %q", f.SetLabelsCalls, tc.wantLabels)
			}
		})
	}
}

func TestRunResumesADraftOnlyWhenThePullRequestCarriesEveryTaggedLabel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		labels     []string
		wantResume bool
	}{
		{labels: []string{"autorelease: tagged", "shipped"}, wantResume: true},
		{labels: []string{"shipped"}},
	} {
		pr := forge.PullRequest{Number: 9, Title: "chore(main): release v1.0.0", Labels: tc.labels, Merged: true}
		f := &forgetest.Fake{
			ListResponses: []forgetest.ListResponse{
				{Head: "release-me/main", State: "closed", PullRequests: []forge.PullRequest{pr}},
			},
			ReleaseDrafts: map[string]bool{"v1.0.0": true},
		}
		results, err := release.Run(context.Background(), f, "owner", "repo", mainComponent, listLabels, true, false, discardLogger())
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := len(results) == 1; got != tc.wantResume {
			t.Fatalf("labels %q: Run() = %+v, want resumed %v", tc.labels, results, tc.wantResume)
		}
	}
}
