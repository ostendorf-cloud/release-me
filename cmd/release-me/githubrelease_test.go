// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
)

// mergedReleaseFake is a forge holding one merged, pending release pull
// request for the implicit main component, number 9, at v0.1.0.
func mergedReleaseFake() *forgetest.Fake {
	pr := forge.PullRequest{
		Number: 9, Title: "chore(main): release v0.1.0",
		Body: "Release v0.1.0\n\n<details>\n<summary>Changelog</summary>\n\n## v0.1.0 (2026-01-02)\n\n### Features\n\n" +
			"- add the thing\n\n</details>\n",
		Labels: []string{"release: pending"}, Merged: true,
	}
	return &forgetest.Fake{
		ListResponses: []forgetest.ListResponse{{Head: mainBranch, State: "closed", PullRequests: []forge.PullRequest{pr}}},
		MergeCommits:  map[int]string{9: "deadbeef"},
		PRLabels:      map[int][]string{9: {"release: pending"}},
	}
}

func TestDoReleaseReportsTheReleaseFieldsInTheComponentsOutput(t *testing.T) {
	for _, draft := range []bool{false, true} {
		t.Run(map[bool]string{false: "published", true: "draft"}[draft], func(t *testing.T) {
			dir, _ := newPRRepo(t)
			commitEntry(t, dir, "chore: initial commit")
			f := mergedReleaseFake()
			cfg := config.Default()
			cfg.DraftUntilAssetsAttach = draft
			var stdout bytes.Buffer

			out := &runOutputs{}
			if err := doRelease(withOutputs(t.Context(), out), dir, cfg, "", f, discardLogger(), false, &stdout); err != nil {
				t.Fatalf("doRelease() error = %v", err)
			}

			got, err := out.render([]string{"main"})
			if err != nil {
				t.Fatalf("render() error = %v", err)
			}
			for _, want := range []string{
				"released=true\n", `"tag":"v0.1.0","major":0,"minor":1,"patch":0,"sha":"deadbeef"`,
				`"body":"## v0.1.0 (2026-01-02)\n\n### Features\n\n- add the thing"`,
				`"html_url":"https://github.com/owner/repo/releases/tag/v0.1.0"`,
				`"upload_url":"https://uploads.github.com/repos/owner/repo/releases/9001/assets{?name,label}"`,
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("outputs = %q, want it to contain %q", got, want)
				}
			}
			if gotDraft := parseOutputs(t, got).components["main"].Draft; gotDraft != draft {
				t.Fatalf("draft = %t, want %t", gotDraft, draft)
			}
		})
	}
}
