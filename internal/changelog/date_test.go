// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package changelog_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
)

func TestFormatDateAndParseDateRoundTrip(t *testing.T) {
	t.Parallel()
	date := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		format string
		want   string
	}{
		{format: "", want: "2026-03-07"},
		{format: "%Y-%m-%d", want: "2026-03-07"},
		{format: "%d.%m.%Y", want: "07.03.2026"},
		{format: "%B %e, %Y", want: "March 7, 2026"},
		{format: "%a %d %b %y", want: "Sat 07 Mar 26"},
		{format: "%A, %Y/%j", want: "Saturday, 2026/066"},
		{format: "released %Y-%m-%d, 100%%", want: "released 2026-03-07, 100%"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			t.Parallel()
			got := changelog.FormatDate(date, tc.format)
			if got != tc.want {
				t.Fatalf("FormatDate() = %q, want %q", got, tc.want)
			}
			back, err := changelog.ParseDate(got, tc.format)
			if err != nil || !back.Equal(date) {
				t.Fatalf("ParseDate(%q) = %v, %v, want %v", got, back, err, date)
			}
		})
	}
}

func TestParseDateRefusesTextOutsideTheFormat(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"2026-3-07", "2026-13-01", "07.03.2026", "2026-03-07 extra", ""} {
		if _, err := changelog.ParseDate(text, "%Y-%m-%d"); err == nil {
			t.Fatalf("ParseDate(%q) error = nil, want it refused", text)
		}
	}
}

func TestValidateDateFormatRejectsAFormatAHeadingCannotCarry(t *testing.T) {
	t.Parallel()
	cases := []struct {
		format string
		want   string
	}{
		{format: "", want: "is empty"},
		{format: "%Y-%m-%d %H:%M", want: `unknown directive at "%H"`},
		{format: "%Y-%m-%d %", want: `unknown directive at "%"`},
		{format: "(%Y)", want: "parenthesis"},
		{format: "%Y\n%m", want: "line break"},
		{format: "%e%m%Y", want: "does not read back"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			t.Parallel()
			err := changelog.ValidateDateFormat(tc.format)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateDateFormat(%q) error = %v, want one containing %q", tc.format, err, tc.want)
			}
		})
	}
	for _, ok := range []string{"%Y-%m-%d", "%d.%m.%Y", "%B %e, %Y", "%e %B %Y"} {
		if err := changelog.ValidateDateFormat(ok); err != nil {
			t.Fatalf("ValidateDateFormat(%q) error = %v, want it accepted", ok, err)
		}
	}
}

func TestParseHeadingReadsADateInAnyFormatAndAnOldDefaultOne(t *testing.T) {
	t.Parallel()
	date := time.Date(2026, time.November, 4, 0, 0, 0, 0, time.UTC)
	compare := "https://github.com/owner/repo/compare/v1.1.0...v1.2.0"
	for _, format := range []string{"", "%d.%m.%Y", "%B %e, %Y"} {
		rendered := changelog.RenderHeading("1.2.0", date, compare, format)
		h, ok := changelog.ParseHeading(rendered)
		if !ok || h.Version != "1.2.0" || h.CompareURL != compare || h.Date != changelog.FormatDate(date, format) {
			t.Fatalf("ParseHeading(%q) = %+v, %v, want version, link and date back", rendered, h, ok)
		}
	}

	// A CHANGELOG.md whose newest section uses the configured format sits
	// on top of sections written before it, in the default one.
	content := "# Changelog\n\n" +
		changelog.RenderHeading("1.2.0", date, compare, "%d.%m.%Y") + "\n\n### Features\n\n- add the widget\n\n" +
		"## [v1.1.0](https://github.com/owner/repo/compare/v1.0.0...v1.1.0) (2026-10-01)\n\n### Bug Fixes\n\n- fix the parser\n"
	if v, ok := changelog.HeadingVersion(content); !ok || v != "1.2.0" {
		t.Fatalf("HeadingVersion() = %q, %v, want 1.2.0", v, ok)
	}
	if body, ok := changelog.TopSection(content); !ok || body != "### Features\n\n- add the widget" {
		t.Fatalf("TopSection() = %q, %v, want the newest section only", body, ok)
	}
	if h, ok := changelog.ParseHeading("## v1.1.0 (2026-10-01)"); !ok || h.Date != "2026-10-01" {
		t.Fatalf("ParseHeading() of an unlinked default heading = %+v, %v", h, ok)
	}
}
