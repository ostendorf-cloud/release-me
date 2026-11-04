// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

func TestDoPRWritesTheHeadingDateInTheConfiguredFormatAboveOlderHeadings(t *testing.T) {
	dir, origin := newPRRepo(t)
	writeConfig(t, dir, "changelog-date-format: \"%d.%m.%Y\"\n")
	writeAndCommit(t, dir, "CHANGELOG.md",
		"# Changelog\n\n## v0.1.0 (2026-10-01)\n\n### Features\n\n- add the parser\n", "chore: initial commit")
	tagRepo(t, dir)
	commitEntry(t, dir, "feat: add the widget")

	cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	f := newFake()
	if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
		t.Fatalf("doPR() error = %v", err)
	}

	content := showBareBranchFile(t, origin, "CHANGELOG.md")
	h, ok := changelog.ParseHeading(content)
	if !ok || h.Version != "0.2.0" {
		t.Fatalf("ParseHeading() = %+v, %v, want the new v0.2.0 heading", h, ok)
	}
	if _, err := time.Parse("02.01.2006", h.Date); err != nil {
		t.Fatalf("heading date = %q, want it in %%d.%%m.%%Y", h.Date)
	}
	if !strings.Contains(content, "## v0.1.0 (2026-10-01)") {
		t.Fatalf("CHANGELOG.md = %q, want the older heading kept as it was written", content)
	}
}

func TestDoPRReadsTheDateOfAnOpenReleasePullRequestBackInTheConfiguredFormat(t *testing.T) {
	cases := []struct {
		name string
		// format is the configured changelog-date-format.
		format string
		// writtenFormat is the format the open pull request's body was
		// rendered in, "" when it predates changelog-date-format.
		writtenFormat string
	}{
		{name: "a body rendered in the configured format", format: "%B %e, %Y", writtenFormat: "%B %e, %Y"},
		{name: "a body rendered before the format was configured", format: "%d.%m.%Y", writtenFormat: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin := newPRRepo(t)
			writeConfig(t, dir, "changelog-date-format: \""+tc.format+"\"\n")
			commitEntry(t, dir, "feat: add the widget")
			cfg, err := config.Load(filepath.Join(dir, "release-me.yaml"))
			if err != nil {
				t.Fatalf("config.Load() error = %v", err)
			}

			f := newFake()
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, io.Discard); err != nil {
				t.Fatalf("doPR() first run error = %v", err)
			}
			created := f.CreatedPullRequests[0]

			// The open pull request carries an older date than today's,
			// in the format it was rendered in: a rerun with nothing new
			// keeps that date, so it must read it back.
			older := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
			body := strings.Replace(created.Body,
				"("+changelog.FormatDate(time.Now(), tc.format)+")", "("+changelog.FormatDate(older, tc.writtenFormat)+")", 1)
			if tc.writtenFormat == "" {
				body = strings.Replace(body, releasepr.DateMarker(time.Now(), tc.format), "", 1)
			} else {
				body = strings.Replace(body, releasepr.DateMarker(time.Now(), tc.format), releasepr.DateMarker(older, tc.format), 1)
			}
			if body == created.Body {
				t.Fatalf("body = %q, want a heading date to replace", body)
			}
			f.ListResponses = []forgetest.ListResponse{{Head: mainBranch, State: "open", PullRequests: []forge.PullRequest{
				{Number: 1, Title: created.Title, Body: body, HeadSHA: bareBranchSHA(t, origin)},
			}}}

			var second bytes.Buffer
			if err := doPR(t.Context(), dir, cfg, "", f, discardLogger(), false, &second); err != nil {
				t.Fatalf("doPR() second run error = %v", err)
			}
			if tc.writtenFormat == tc.format {
				if !strings.Contains(second.String(), "up to date") || len(f.UpdatedPullRequests) != 0 {
					t.Fatalf("stdout = %q, updates = %+v, want the pull request up to date", second.String(), f.UpdatedPullRequests)
				}
				return
			}
			// A body from before the format keeps its date, now rendered in
			// the configured format.
			if len(f.UpdatedPullRequests) != 1 {
				t.Fatalf("UpdatedPullRequests = %+v, want one update", f.UpdatedPullRequests)
			}
			if want := "(" + changelog.FormatDate(older, tc.format) + ")"; !strings.Contains(f.UpdatedPullRequests[0].Body, want) {
				t.Fatalf("updated body = %q, want the older date kept as %q", f.UpdatedPullRequests[0].Body, want)
			}
		})
	}
}
