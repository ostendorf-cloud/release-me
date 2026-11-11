// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

func dateTransitionRepo(t *testing.T, mode string) (string, string, config.Config) {
	t.Helper()
	if mode == "shared" {
		return newSharedRepo(t)
	}
	dir, remote := newPRRepo(t)
	if mode == "locked" {
		writeConfig(t, dir, "versioning: locked\ncomponents:\n  a:\n    path: a\n  b:\n    path: b\n")
		writeFiles(t, dir, "feat: add export", map[string]string{"a/export.go": "package a\n", "b/README.md": "b\n"})
		return dir, remote, loadConfig(t, dir)
	}
	commitEntry(t, dir, "feat: add export")
	return dir, remote, config.Default()
}

func TestChangelogDateSurvivesCustomFormatTransitionsUntilContentChanges(t *testing.T) {
	for _, mode := range []string{"single", "shared", "locked"} {
		for _, layout := range []config.ReleasePullRequestLayout{config.LayoutHeadings, config.LayoutDetails, config.LayoutCompact} {
			for _, next := range []string{"%d.%m.%Y", ""} {
				t.Run(mode+"/"+string(layout)+"/"+next, func(t *testing.T) {
					dir, remote, cfg := dateTransitionRepo(t, mode)
					cfg.ReleasePullRequestLayout = layout
					cfg.ChangelogStyle.DateFormat = "%B %e, %Y"
					f := newPRFake(remote)
					var out bytes.Buffer
					if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
						t.Fatal(err)
					}
					created := f.CreatedPullRequests[0]
					before := bareBranchSHA(t, remote)
					older := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
					body := strings.ReplaceAll(created.Body, "("+changelog.FormatDate(time.Now(), cfg.ChangelogStyle.DateFormat)+")", "("+changelog.FormatDate(older, cfg.ChangelogStyle.DateFormat)+")")
					body = strings.ReplaceAll(body, "<!-- release-me-date: "+time.Now().Format("2006-01-02")+" -->", "<!-- release-me-date: 2026-10-01 -->")
					f.ListResponses = []forgetest.ListResponse{{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{{Number: 1, Title: created.Title, Body: body, HeadSHA: before, Labels: []string{"release: pending"}}}}}
					cfg.ChangelogStyle.DateFormat = next
					if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
						t.Fatal(err)
					}
					if len(f.UpdatedPullRequests) != 1 {
						t.Fatalf("updates=%+v", f.UpdatedPullRequests)
					}
					updated := f.UpdatedPullRequests[0]
					want := "(" + changelog.FormatDate(older, next) + ")"
					if !strings.Contains(updated.Body, want) {
						t.Fatalf("date reset; want %s in %s", want, updated.Body)
					}
					if got := bareBranchSHA(t, remote); got != before {
						t.Fatal("format-only change pushed the branch")
					}
					f.ListResponses[0].PullRequests[0] = forge.PullRequest{Number: 1, Title: updated.Title, Body: updated.Body, HeadSHA: before, Labels: []string{"release: pending"}}
					commitEntry(t, dir, "feat: add another export")
					if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &out); err != nil {
						t.Fatal(err)
					}
					changed := f.UpdatedPullRequests[len(f.UpdatedPullRequests)-1].Body
					if want := "(" + changelog.FormatDate(time.Now(), next) + ")"; !strings.Contains(changed, want) {
						t.Fatalf("changed content did not get today's date: %s", changed)
					}
					if got := bareBranchSHA(t, remote); got == before {
						t.Fatal("changed content did not update the branch")
					}
				})
			}
		}
	}
}
