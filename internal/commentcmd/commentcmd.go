// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package commentcmd parses a "release-as" or "release-notes" command from
// a comment on the release pull request, and decides whether the
// commenter's author association authorizes it. It reaches no forge and
// writes nothing: cmd/release-me's comment command acts on the Outcome it
// returns.
package commentcmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/version"
)

// Kind names a recognized comment command word.
type Kind string

// The command words a comment may name after the configured mention.
const (
	// KindReleaseAs is "release-as X.Y.Z": force the next version.
	KindReleaseAs Kind = "release-as"
	// KindReleaseNotes is "release-notes <text>": set the release-me-notes
	// block.
	KindReleaseNotes Kind = "release-notes"
)

// writeAssociations are the GitHub "author_association" values that carry
// write access or higher on the repository, the minimum a comment command
// requires.
var writeAssociations = map[string]bool{"OWNER": true, "MEMBER": true, "COLLABORATOR": true}

// Authorized reports whether association, a comment event's
// "author_association" field, carries write access or higher: OWNER,
// MEMBER or COLLABORATOR.
func Authorized(association string) bool {
	return writeAssociations[association]
}

// Outcome is what Parse resolved one comment body to.
type Outcome struct {
	// Matched is true when body names mention at all, whether or not a
	// recognized command word follows it.
	Matched bool
	// Allowed reports whether the comment's author association carries
	// write access or higher. Meaningless when Matched is false.
	Allowed bool
	// Command is the recognized command word after the first Matched
	// occurrence of mention that one follows, or "" when body names
	// mention with no recognized command word anywhere after any
	// occurrence of it.
	Command Kind
	// Version is Command's KindReleaseAs argument, normalized to "X.Y.Z"
	// without a "v" prefix. Set only when Command is KindReleaseAs and
	// Valid is true.
	Version string
	// Notes is Command's KindReleaseNotes argument, the free text that
	// follows the command word to the end of the comment, trimmed. Set
	// only when Command is KindReleaseNotes; Valid is always true for it.
	Notes string
	// Valid reports whether Command's argument parsed. Always true for
	// KindReleaseNotes; false for KindReleaseAs when Version did not parse
	// as a real semantic version. Meaningless when Command is "".
	Valid bool
	// Reason explains, in one line, why Command is "" or Valid is false.
	Reason string
}

// commandWord matches the first whitespace-delimited word of a trimmed
// tail and the rest of it, body's remaining text to the end for
// KindReleaseNotes to use verbatim.
var commandWord = regexp.MustCompile(`(?s)^(\S+)[ \t\r\n]*(.*)$`)

// Parse scans body for mention, GitHub's "@name" convention assumed, and
// the recognized command word that follows the first occurrence it finds
// one after, returning what it resolved and whether association
// authorizes it. mention is compared literally; a repository's configured
// "comment-mention" is passed as is.
//
// A comment that never names mention at all returns Matched false: the
// comment was not directed at release-me, and the caller replies to
// nothing. A comment naming mention with no command word after any
// occurrence of it, "the mention alone", or one followed by an
// unrecognized word, returns Matched true and Command "", so the caller
// still explains why nothing happened. mention is matched at a word
// boundary, so a longer handle sharing its prefix, "@release-me-fan" for
// mention "@release-me", never matches.
func Parse(body, mention, association string) Outcome {
	var out Outcome
	if mention == "" {
		return out
	}

	pos := 0
	for {
		idx := strings.Index(body[pos:], mention)
		if idx < 0 {
			return out
		}
		start := pos + idx
		end := start + len(mention)
		pos = end
		if !boundaryOK(body, start, end) {
			continue
		}
		out.Matched = true
		out.Allowed = Authorized(association)

		tail := strings.TrimLeft(body[end:], " \t\r\n")
		m := commandWord.FindStringSubmatch(tail)
		if m == nil {
			// Nothing but whitespace after this occurrence: "the mention
			// alone". Keep scanning in case a later occurrence carries a
			// real command.
			continue
		}
		word, rest := m[1], m[2]

		switch word {
		case string(KindReleaseAs):
			out.Command = KindReleaseAs
			applyReleaseAs(&out, rest)
			return out
		case string(KindReleaseNotes):
			out.Command = KindReleaseNotes
			out.Notes = strings.TrimSpace(rest)
			out.Valid = true
			return out
		default:
			continue
		}
	}
}

// applyReleaseAs parses rest's first token as a semantic version, filling
// out.Version and out.Valid on success, out.Reason on failure.
func applyReleaseAs(out *Outcome, rest string) {
	arg := firstToken(rest)
	if arg == "" {
		out.Reason = "release-as needs a version, for example \"release-as 1.2.3\""
		return
	}
	normalized, ok := parseVersion(arg)
	if !ok {
		out.Reason = fmt.Sprintf("%q after release-as is not a parseable X.Y.Z version", arg)
		return
	}
	out.Version = normalized
	out.Valid = true
}

// firstToken returns s's first whitespace-delimited token, "" when s holds
// none.
func firstToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// parseVersion parses s, an optional "v" prefix stripped, as a semantic
// version through version.ParseTag, the same parser a tag name is checked
// against. normalized is s rendered back without the "v" prefix and
// without build metadata, ok is false when s does not parse.
func parseVersion(s string) (normalized string, ok bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	sv, ok := version.ParseTag("v" + s)
	if !ok {
		return "", false
	}
	return sv.String(), true
}

// boundaryOK reports whether body[start:end] is a whole mention token: the
// byte right before start, when one exists, and the byte right after end,
// when one exists, are both outside the set a mention could continue
// with, so "@release-me" never matches inside "@release-me-fan" or
// "foo@release-me".
func boundaryOK(body string, start, end int) bool {
	if start > 0 && !isBoundary(body[start-1]) {
		return false
	}
	if end < len(body) && !isBoundary(body[end]) {
		return false
	}
	return true
}

// isBoundary reports whether b cannot continue a mention token: every byte
// except a letter, a digit, "-", "_" and "@".
func isBoundary(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '-', b == '_', b == '@':
		return false
	default:
		return true
	}
}
