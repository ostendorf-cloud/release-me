// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

func TestRunRetriesAnUnpublishedDeferredTagWithoutAnotherReleasePR(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		legacy       bool
	}{
		{name: "single"},
		{name: "shared", config: "release-pull-request: shared\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n"},
		{name: "locked", config: "versioning: locked\ncomponents:\n  api:\n    path: api\n  web:\n    path: web\n"},
		{name: "legacy per-component draft after switching to shared", config: "components:\n  api:\n    path: api\n  web:\n    path: web\n", legacy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, remote := newPRRepo(t)
			writeConfig(t, dir, tc.config)
			writeFiles(t, dir, "chore: scaffold packages", map[string]string{"api/README.md": "api\n", "web/README.md": "web\n"})
			cfg := loadConfig(t, dir)
			if tc.config == "" {
				runGit(t, dir, "tag", "v1.0.0")
			} else {
				runGit(t, dir, "tag", "api/v1.0.0")
				runGit(t, dir, "tag", "web/v1.0.0")
			}
			writeFiles(t, dir, "feat: export reports", map[string]string{"api/export.go": "package api\n"})
			cfg.ForceTagCreation = false
			cfg.DraftUntilAssetsAttach = true
			f := newFake()
			var out bytes.Buffer
			if err := doRun(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
				t.Fatal(err)
			}
			if len(f.CreatedPullRequests) != 1 {
				t.Fatalf("first PRs = %+v", f.CreatedPullRequests)
			}
			created := f.CreatedPullRequests[0]
			runGit(t, dir, "fetch", "-q", remote, "refs/heads/"+created.Head)
			runGit(t, dir, "merge", "-q", "--no-ff", "FETCH_HEAD", "-m", "Merge release pull request")
			merged := headSHA(t, dir)
			f.ListResponses = []forgetest.ListResponse{{Head: created.Head, State: "closed", PullRequests: []forge.PullRequest{{Number: 9, Title: created.Title, Body: created.Body, Merged: true, HeadSHA: merged, Labels: []string{"release: pending"}}}}}
			f.MergeCommits = map[int]string{9: merged}
			f.PRLabels = map[int][]string{9: {"release: pending"}}
			if err := doRun(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
				t.Fatal(err)
			}
			releases := len(f.CreatedReleases)
			if releases == 0 || len(f.CreatedTags) != 0 {
				t.Fatalf("draft releases=%+v, tags=%+v", f.CreatedReleases, f.CreatedTags)
			}
			f.ListResponses[0].PullRequests[0].Labels = f.PRLabels[9]
			if tc.legacy {
				cfg.ReleasePullRequest = config.ReleasePullRequestShared
			}
			for retry := 0; retry < 2; retry++ {
				out.Reset()
				if err := doRun(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
					t.Fatal(err)
				}
				if len(f.CreatedPullRequests) != 1 || len(f.CreatedReleases) != releases || len(f.CreatedTags) != 0 {
					t.Fatalf("retry duplicated work: PRs=%+v releases=%+v tags=%+v", f.CreatedPullRequests, f.CreatedReleases, f.CreatedTags)
				}
				if !bytes.Contains(out.Bytes(), []byte("waiting for unpublished release")) || !bytes.Contains(out.Bytes(), []byte("publish or delete its draft")) {
					t.Fatalf("missing draft deferral: %s", out.String())
				}
				if !bytes.Contains(out.Bytes(), []byte("released-tag:")) {
					t.Fatalf("draft not resumed: %s", out.String())
				}
			}
		})
	}
}

func TestDeferredDraftDeferralRequiresAnAbsentTagAndAnUnpublishedRelease(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		tagExists, draft, force bool
		missing                 bool
		title                   string
		want                    bool
	}{
		{name: "unpublished draft", draft: true, want: true},
		{name: "tag already exists", tagExists: true, draft: true},
		{name: "published release"},
		{name: "deleted draft", missing: true},
		{name: "forced tag creation", draft: true, force: true},
		{name: "unrelated title", draft: true, title: "feat: add a command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			f.ExistingTags = map[string]bool{"v1.1.0": tc.tagExists}
			if !tc.missing {
				f.ReleaseDrafts = map[string]bool{"v1.1.0": tc.draft}
			}
			cfg := config.Default()
			cfg.ForceTagCreation = tc.force
			title := tc.title
			if title == "" {
				title = "chore(main): release v1.1.0"
			}
			got, err := unpublishedDeferredRelease(t.Context(), f, "owner", "repo", cfg, forge.PullRequest{Number: 9, Title: title})
			if err != nil || got != tc.want {
				t.Fatalf("deferred=%v,%v;want %v", got, err, tc.want)
			}
		})
	}
}

// deferredLookupForge records remote probes made while deciding whether
// an unpublished release should hold back a new release pull request.
type deferredLookupForge struct {
	*forgetest.Fake
	tags, drafts []string
}

func (f *deferredLookupForge) TagExists(ctx context.Context, owner, repo, tag string) (bool, error) {
	f.tags = append(f.tags, tag)
	return f.Fake.TagExists(ctx, owner, repo, tag)
}

func (f *deferredLookupForge) IsReleaseDraft(ctx context.Context, owner, repo, tag string) (bool, error) {
	f.drafts = append(f.drafts, tag)
	return f.Fake.IsReleaseDraft(ctx, owner, repo, tag)
}

func TestReleaseDeferralOnlyChecksTheNewestMergedTaggedPullRequest(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse listing %t", reverse), func(t *testing.T) {
			prs := []forge.PullRequest{
				{Number: 10, Title: "chore(main): release v1.0.0", Merged: true, Labels: []string{"release: tagged"}},
				{Number: 20, Title: "chore(main): release v1.1.0", Merged: true, Labels: []string{"release: tagged"}},
				{Number: 30, Title: "chore(main): release v1.2.0", Labels: []string{"release: tagged"}},
			}
			if reverse {
				slices.Reverse(prs)
			}
			f := &deferredLookupForge{Fake: &forgetest.Fake{
				ListResponses: []forgetest.ListResponse{{Head: mainBranch, State: "closed", PullRequests: prs}},
				ExistingTags:  map[string]bool{"v1.1.0": true},
			}}
			cfg := config.Default()
			cfg.ForceTagCreation, cfg.DraftUntilAssetsAttach = false, true
			deferral, err := mergedReleaseDeferral(t.Context(), f, "owner", "repo", mainBranch, cfg, discardLogger())
			if err != nil || deferral != "" {
				t.Fatalf("deferral = %q, %v; want empty, nil", deferral, err)
			}
			if !reflect.DeepEqual(f.tags, []string{"v1.1.0"}) || len(f.drafts) != 0 {
				t.Fatalf("remote lookups: tags = %q, drafts = %q; want only newest merged tag", f.tags, f.drafts)
			}
		})
	}
}
