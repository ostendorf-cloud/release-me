// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package changelog

import (
	"regexp"
	"strings"
)

// TopSection returns the body of the first version section in rendered
// CHANGELOG.md content, the text under its heading line up to the next
// version heading or the end of the file, excluding the heading itself so
// a caller can compare it without the date the heading carries. ok is
// false when content carries no version heading at all.
func TopSection(content string) (body string, ok bool) {
	rest := strings.TrimLeft(strings.TrimPrefix(content, header), "\n")
	if !strings.HasPrefix(rest, "## ") {
		return "", false
	}

	_, afterHeading, found := strings.Cut(rest, "\n")
	if !found {
		return "", true
	}
	afterHeading = strings.TrimLeft(afterHeading, "\n")
	if idx := strings.Index(afterHeading, "\n## "); idx >= 0 {
		afterHeading = afterHeading[:idx+1]
	}
	return strings.TrimRight(afterHeading, "\n"), true
}

// headingVersionPattern is the regexp fragment matching the version in a
// changelog section's heading line, the full semantic version shape
// RenderHeading can write: major.minor.patch, with an optional
// pre-release suffix and an optional build metadata suffix.
const headingVersionPattern = `\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?`

// headingLine matches a changelog section's heading line in either shape
// RenderHeading writes: "## vX.Y.Z (<date>)", the only shape before the
// compare link, which older entries still carry, and
// "## [vX.Y.Z](<compare URL>) (<date>)". The date is any text up to the
// closing parenthesis, in whatever "changelog-date-format" it was written
// in: "YYYY-MM-DD" by default and for every heading written before the key
// existed. The groups are the linked version, the compare URL, the
// unlinked version and the date.
var headingLine = regexp.MustCompile(
	`^## (?:\[v(` + headingVersionPattern + `)\]\(([^()\s]+)\)|v(` + headingVersionPattern + `)) \(([^()\n]+)\)`,
)

// Heading is what a changelog section's heading line carries.
type Heading struct {
	// Version is the version without its "v", pre-release and build
	// metadata suffixes included.
	Version string
	// CompareURL is the link the version is wrapped in, "" for a heading
	// without one.
	CompareURL string
	// Date is the heading's date as written, in the date format it was
	// rendered in, "YYYY-MM-DD" by default.
	Date string
}

// ParseHeading reads the first version heading of content, either
// rendered CHANGELOG.md content, its "# Changelog" header skipped, or a
// changelog section on its own, such as the one inside a release pull
// request body. It reads a heading with a compare link and one without
// alike. ok is false when content does not start with a version heading,
// or that heading's version is not shaped like a semantic version, with an
// optional pre-release and build metadata suffix.
func ParseHeading(content string) (h Heading, ok bool) {
	rest := strings.TrimLeft(strings.TrimPrefix(content, header), "\n")
	m := headingLine.FindStringSubmatch(rest)
	if m == nil {
		return Heading{}, false
	}
	h = Heading{Version: m[1], CompareURL: m[2], Date: m[4]}
	if h.Version == "" {
		h.Version = m[3]
	}
	return h, true
}

// HeadingVersion returns the version rendered in the first version
// heading of rendered CHANGELOG.md content, the same heading TopSection
// strips, linked or not. ok is false when ParseHeading's is.
func HeadingVersion(content string) (version string, ok bool) {
	h, ok := ParseHeading(content)
	return h.Version, ok
}
