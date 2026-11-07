// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package migrate_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/migrate"
)

func TestFromReleasePleaseCarriesTheSignoffOver(t *testing.T) {
	t.Parallel()
	cases := []struct {
		signoff, want, warning string
	}{
		{signoff: "Release Bot <release-bot@example.com>", want: "Release Bot <release-bot@example.com>"},
		{signoff: "release-bot", warning: `signoff: "release-bot" is not "Name <email>" on one line, left out`},
	}
	for _, tc := range cases {
		configJSON := fmt.Sprintf(`{"signoff": %q, "packages": {".": {"release-type": "go"}}}`, tc.signoff)
		got, err := migrate.FromReleasePlease([]byte(configJSON), nil, files(nil))
		if err != nil {
			t.Fatalf("FromReleasePlease() error = %v", err)
		}
		if cfg := load(t, got.YAML); cfg.Signoff != tc.want {
			t.Fatalf("Signoff = %q, want %q\n%s", cfg.Signoff, tc.want, got.YAML)
		}
		if tc.warning != "" && !slices.Contains(got.Warnings, tc.warning) {
			t.Fatalf("Warnings = %q, want it to contain %q", got.Warnings, tc.warning)
		}
	}
}
