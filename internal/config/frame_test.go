// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func TestLoadReadsTheReleasePullRequestHeaderAndFooter(t *testing.T) {
	t.Parallel()
	got, err := config.Load(writeFile(t, "release-pull-request-header: |\n  Reviewed by the release team.\n"+
		"release-pull-request-footer: \"Questions go to #releases.\"\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ReleasePullRequestHeader != "Reviewed by the release team.\n" || got.ReleasePullRequestFooter != "Questions go to #releases." {
		t.Fatalf("header, footer = %q, %q", got.ReleasePullRequestHeader, got.ReleasePullRequestFooter)
	}
	if frame := config.Default().Frame(); frame.Header != "" || frame.Footer != "" {
		t.Fatalf("Default().Frame() = %+v, want neither", frame)
	}
}

func TestLoadRejectsAHeaderOrFooterABodyWouldBeReadBackBy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "a header opening a changelog block",
			content: "release-pull-request-header: \"<details><summary>Changelog</summary></details>\"\n",
			want:    "release-pull-request-header carries \"<details><summary>Changelog</summary>\"",
		},
		{
			name:    "a footer carrying a release-me comment",
			content: "release-pull-request-footer: \"<!-- release-me-title-pattern: x ${component} ${version} -->\"\n",
			want:    "release-pull-request-footer carries a line opening \"<!-- release-me-\"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(writeFile(t, tc.content))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
