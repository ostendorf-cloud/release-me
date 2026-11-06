// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func TestLoadReadsReleasePullRequestDraft(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "left out opens a pull request ready for review", content: "initial-version: 0.1.0\n", want: false},
		{name: "true opens a draft", content: "release-pull-request-draft: true\n", want: true},
		{name: "false opens a pull request ready for review", content: "release-pull-request-draft: false\n", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(writeFile(t, tc.content))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.ReleasePullRequestDraft != tc.want {
				t.Fatalf("ReleasePullRequestDraft = %v, want %v", got.ReleasePullRequestDraft, tc.want)
			}
		})
	}
	if _, err := config.Load(writeFile(t, "release-pull-request-draft: sometimes\n")); err == nil {
		t.Fatal("Load() error = nil, want an error for a value that is not a boolean")
	}
}
