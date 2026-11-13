// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr

import (
	"errors"
	"regexp"
	"strings"
)

// Frame is the free text a repository puts around every release pull
// request body: "release-pull-request-header" above the version line and
// "release-pull-request-footer" below the changelog. Empty leaves that side
// out.
type Frame struct {
	Header string
	Footer string
}

// Wrap returns body with the header above it and the footer below it. Every
// line of either that opens a fence is escaped the way the notes block's
// content is, so a header or footer never opens a release-me-notes,
// release-me-version or release-me block that a later run would read back:
// nothing in them is ever read, only rendered.
//
// This is the one exported function in this package taking a body that is
// exempt from normalizeLineEndings' invariant: body here is already this
// package's own render output, assembled from already-normalized pieces,
// never a raw GitHub pull request body a fence-matching pattern still has
// to read.
func (f Frame) Wrap(body string) string {
	if header := strings.Trim(f.Header, "\n"); header != "" {
		body = escapeFence(header) + "\n\n" + body
	}
	if footer := strings.Trim(f.Footer, "\n"); footer != "" {
		body = strings.TrimRight(body, "\n") + "\n\n" + escapeFence(footer) + "\n"
	}
	return body
}

// frameMarker matches a line opening an HTML comment in the release-me-
// namespace, the comments a body records what release-me reads back in.
var frameMarker = regexp.MustCompile(`(?m)^<!-- release-me-`)

// CheckFrameText refuses a header or footer text that would change how a
// body reads back: one carrying the changelog block's opening, which
// ExtractChangelog would take for the real one, or a line opening a
// release-me- comment, which release-me reserves for what it records in a
// body and reads back.
func CheckFrameText(text string) error {
	if changelogOpen.MatchString(text) {
		return errors.New(`carries "<details><summary>Changelog</summary>", the opening release-me reads the changelog from`)
	}
	if frameMarker.MatchString(text) {
		return errors.New(`carries a line opening "<!-- release-me-", a comment release-me reserves for what it reads back from a body`)
	}
	return nil
}
