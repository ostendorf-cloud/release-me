// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr

import (
	"regexp"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
)

var canonicalDateMarker = regexp.MustCompile(`(?m)^<!-- release-me-date: (\d{4}-\d{2}-\d{2}) -->$`)

// DateMarker records the canonical date for a custom-formatted body. The
// caller appends it outside the rendered body and any header/footer frame.
// Default-format headings already record the canonical date themselves.
func DateMarker(date time.Time, format string) string {
	if format == "" || format == changelog.DefaultDateFormat {
		return ""
	}
	return "\n<!-- release-me-date: " + date.Format("2006-01-02") + " -->\n"
}

func recordedDate(body string) (time.Time, bool) {
	fenced := append(notesBlock.FindAllStringIndex(body, -1), versionBlock.FindAllStringIndex(body, -1)...)
	for _, loc := range canonicalDateMarker.FindAllStringSubmatchIndex(body, -1) {
		if insideAny(loc[0], fenced) {
			continue
		}
		date, err := time.Parse("2006-01-02", body[loc[2]:loc[3]])
		if err == nil {
			return date, true
		}
	}
	return time.Time{}, false
}
