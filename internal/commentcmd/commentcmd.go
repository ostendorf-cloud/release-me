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

// writeAssociations are the GitHub "author_association" values Parse
// treats as close to write access. See Authorized's own doc comment for
// exactly what this does and does not guarantee.
var writeAssociations = map[string]bool{"OWNER": true, "MEMBER": true, "COLLABORATOR": true}

// Authorized reports whether association, a comment event's
// "author_association" field, is OWNER, MEMBER or COLLABORATOR.
//
// This is close to, but not the same as, write access or higher on the
// repository: MEMBER is any member of the owning organization, regardless
// of that member's own permission on this particular repository, and
// COLLABORATOR includes a collaborator added with only read or triage
// access. Checking the repository's actual collaborator permission would
// need an extra API call per comment; author_association is already on
// the webhook payload GitHub computed server side, so Parse accepts the
// wider set these three values actually describe instead.
func Authorized(association string) bool {
	return writeAssociations[association]
}

// Outcome is what Parse resolved one comment body to.
type Outcome struct {
	// Matched is true when some line of body, once stripped of quoted,
	// fenced, inline code and HTML comment text, starts with mention,
	// whether or not a recognized command word follows it.
	Matched bool
	// Allowed reports whether the comment's author association is close
	// to write access or higher; see Authorized. Meaningless when Matched
	// is false.
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

// htmlComment, fencedBlock and inlineCode match the markdown constructs
// sanitize removes before Parse ever looks for mention: a comment, quote
// or illustrative snippet discussing a command must never be read as one.
var (
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	fencedBlock = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")
	inlineCode  = regexp.MustCompile("`[^`\n]*`")
)

// sanitize strips HTML comments, fenced code blocks, inline code spans and
// blockquote lines from body, so Parse never scans text the commenter
// quoted, fenced or commented out rather than typed as a live command. A
// GitHub "Quote reply" prefixes every quoted line with ">", which this
// drops entirely rather than merely ignoring the prefix, since a quoted
// command is never meant to run.
func sanitize(body string) string {
	body = htmlComment.ReplaceAllString(body, "")
	body = fencedBlock.ReplaceAllString(body, "")
	body = inlineCode.ReplaceAllString(body, "")

	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), ">") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// Parse sanitizes body, then scans its lines for one that starts, after
// only leading whitespace, with mention at a real token boundary, and
// resolves the recognized command word that follows the first such line
// one does, returning what it resolved and whether association
// authorizes it. mention is compared literally; a repository's configured
// "comment-mention" is passed as is.
//
// Requiring mention to open its own line, not merely appear anywhere in
// body, is deliberate: a command embedded mid sentence, in a blockquote, a
// fenced block, an inline code span or an HTML comment must never execute,
// including when a maintainer's own "Quote reply" repeats a refused
// command verbatim to say no to it. sanitize already drops every one of
// those constructs; the line-start rule is a second, independent reason
// each of them fails to match even if sanitize somehow missed one.
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

	clean := sanitize(body)
	offset := 0
	for _, line := range strings.Split(clean, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		leading := len(line) - len(trimmed)

		if strings.HasPrefix(trimmed, mention) {
			after := trimmed[len(mention):]
			if after == "" || isBoundary(after[0]) {
				out.Matched = true
				out.Allowed = Authorized(association)

				afterAbs := offset + leading + len(mention)
				tail := strings.TrimLeft(clean[afterAbs:], " \t\r\n")
				if m := commandWord.FindStringSubmatch(tail); m != nil {
					if done := resolveCommand(&out, m[1], m[2]); done {
						return out
					}
				}
				// No recognized command word on this line: keep scanning
				// in case a later line carries a real command.
			}
		}
		offset += len(line) + 1
	}
	return out
}

// resolveCommand fills out from word and rest, the command word and
// remaining text commandWord matched on one line. done is true when word
// is recognized, so Parse stops scanning; false for an unrecognized word,
// so Parse keeps looking at later lines.
func resolveCommand(out *Outcome, word, rest string) (done bool) {
	switch word {
	case string(KindReleaseAs):
		out.Command = KindReleaseAs
		applyReleaseAs(out, rest)
		return true
	case string(KindReleaseNotes):
		out.Command = KindReleaseNotes
		out.Notes = strings.TrimSpace(rest)
		out.Valid = true
		return true
	default:
		return false
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

// isBoundary reports whether b cannot continue a mention token: every byte
// except a letter, a digit, "-", "_" and "@". Parse only ever checks the
// byte right after a line-starting match, since the mention is already
// known to open the line.
func isBoundary(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '-', b == '_', b == '@':
		return false
	default:
		return true
	}
}
