// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr_test

import (
	"testing"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

func TestCanonicalDatesIgnoreMetadataQuotedInPreservedBlocks(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"release-me-notes", "release-me-version"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			body := "Team header\n```" + kind + "\n<!-- release-me-date: 2040-01-01 -->\n```\nTeam footer\n<!-- release-me-date: 2026-10-01 -->\n"
			got, ok := releasepr.ExtractDate(body, "%d.%m.%Y")
			want := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
			if !ok || !got.Equal(want) {
				t.Fatalf("date=%v,%v;want %v", got, ok, want)
			}
		})
	}
}
