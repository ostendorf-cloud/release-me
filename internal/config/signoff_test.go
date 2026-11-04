// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func TestLoadReadsSignoff(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, content, want string
	}{
		{name: "left out signs nothing off", content: "initial-version: 0.1.0\n"},
		{name: "empty signs nothing off", content: "signoff: \"\"\n"},
		{
			name: "an identity", content: "signoff: \"Release Bot <release-bot@example.com>\"\n",
			want: "Release Bot <release-bot@example.com>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(writeFile(t, tc.content))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.Signoff != tc.want {
				t.Fatalf("Signoff = %q, want %q", got.Signoff, tc.want)
			}
		})
	}
	_, err := config.Load(writeFile(t, "signoff: release-bot@example.com\n"))
	if err == nil || !strings.Contains(err.Error(), `signoff "release-bot@example.com" is not "Name <email>"`) {
		t.Fatalf("Load() error = %v, want the identity refused", err)
	}
}
