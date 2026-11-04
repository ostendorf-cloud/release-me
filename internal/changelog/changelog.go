// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package changelog renders the Markdown changelog for one version from
// classified conventional entries.
package changelog

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Entry is one changelog line.
type Entry struct {
	Type              string
	Scope             string
	Description       string
	Breaking          bool
	PullRequestNumber int
	URL               string
	// SubEntries renders as indented list items under this entry's line,
	// in the nested changelog source mode: the commits of the pull
	// request's merge, each parsed on its own. A sub entry's own
	// SubEntries, if any, is never rendered: nesting goes one level
	// deep. Options.HiddenTypes drops a sub entry from rendering the same way it
	// drops a top level one. A sub entry never renders under a section of
	// its own: it always renders under its parent's line, but see
	// Sections for the one case where it decides which section that is.
	SubEntries []Entry
}

// Options decides how Sections and Render turn entries into Markdown. The
// zero value renders every section with nothing hidden.
type Options struct {
	// HiddenTypes drops an entry of one of these types: it produces no
	// line of its own, see sectionFor for the one case where its sub
	// entries still earn it one.
	HiddenTypes []string
	// SentenceCase upper cases the first letter of every entry's
	// description, see sentenceCase for the words it leaves alone.
	SentenceCase bool
	// DateFormat is the strftime expression Render's heading renders its
	// date in, "" for DefaultDateFormat. See FormatDate.
	DateFormat string
}

type section struct {
	heading string
	match   func(Entry) bool
}

var sections = []section{
	{heading: "Breaking Changes", match: func(e Entry) bool { return e.Breaking }},
	{heading: "Features", match: func(e Entry) bool { return !e.Breaking && e.Type == "feat" }},
	{heading: "Bug Fixes", match: func(e Entry) bool { return !e.Breaking && e.Type == "fix" }},
	{heading: "Performance", match: func(e Entry) bool { return !e.Breaking && e.Type == "perf" }},
	{heading: "Reverts", match: func(e Entry) bool { return !e.Breaking && e.Type == "revert" }},
}

// Sections renders the section bodies for one version's entries, with no
// heading, so a caller can compare them independently of the version and
// the date. It applies the same opts and section rules as Render: an
// entry whose own type is in opts.HiddenTypes, or that belongs to none of
// the five sections, breaking already counted as one, produces no line of
// its own; its SubEntries can still earn it one, see sectionFor. Sections
// returns "" when no entry is visible.
func Sections(entries []Entry, opts Options) string {
	var body strings.Builder
	for i, s := range sections {
		var lines []string
		for _, e := range entries {
			if idx, ok := sectionFor(e, opts.HiddenTypes); ok && idx == i {
				lines = append(lines, renderEntry(e, opts)...)
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&body, "### %s\n\n", s.heading)
		for _, line := range lines {
			fmt.Fprintf(&body, "%s\n", line)
		}
		body.WriteString("\n")
	}
	return strings.TrimRight(body.String(), "\n")
}

// sectionFor decides which section e, and the line its own line renders
// as, belongs under. e's own type decides it when that type is not in
// hiddenTypes and matches one of the five sections, same as always. When
// it is hidden, or belongs to none of them, SubEntries gets a second
// chance: the first section, in Breaking Changes, Features, Bug Fixes,
// Performance, Reverts order, that one of e's own non-hidden sub entries
// matches. That keeps a merge whose own title is a hidden type, a chore,
// say, from losing a feat or fix sub commit's entire section: the parent
// line still renders, as the heading of its sub items, wherever the
// highest-priority visible one belongs. ok is false, no line at all, when
// neither e nor any of its sub entries matches a section.
func sectionFor(e Entry, hiddenTypes []string) (idx int, ok bool) {
	if !slices.Contains(hiddenTypes, e.Type) {
		for i, s := range sections {
			if s.match(e) {
				return i, true
			}
		}
	}
	for i, s := range sections {
		for _, sub := range e.SubEntries {
			if !slices.Contains(hiddenTypes, sub.Type) && s.match(sub) {
				return i, true
			}
		}
	}
	return 0, false
}

// Render renders the Markdown changelog for one version: the heading, see
// RenderHeading, then Sections. Render returns "" when Sections does, no
// entry visible.
func Render(version string, date time.Time, compareURL string, entries []Entry, opts Options) string {
	body := Sections(entries, opts)
	if body == "" {
		return ""
	}

	var out strings.Builder
	out.WriteString(RenderHeading(version, date, compareURL, opts.DateFormat))
	out.WriteString("\n\n")
	out.WriteString(body)
	out.WriteString("\n")
	return out.String()
}

// RenderHeading renders one version section's heading line, with no
// trailing newline: "## vX.Y.Z (<date>)", the version wrapped in a link to
// compareURL, "## [vX.Y.Z](<compareURL>) (<date>)", when compareURL is not
// "". The caller passes "" when there is nothing to compare against, a
// component's first release. The date renders in dateFormat, see
// FormatDate, "" for DefaultDateFormat, "YYYY-MM-DD".
func RenderHeading(version string, date time.Time, compareURL, dateFormat string) string {
	text := "v" + version
	if compareURL != "" {
		text = "[" + text + "](" + compareURL + ")"
	}
	return fmt.Sprintf("## %s (%s)", text, FormatDate(date, dateFormat))
}

// renderEntry renders one visible entry's own line, followed by one
// indented line per sub entry that opts.HiddenTypes does not drop.
func renderEntry(e Entry, opts Options) []string {
	lines := []string{renderLine(e, opts)}
	for _, sub := range e.SubEntries {
		if slices.Contains(opts.HiddenTypes, sub.Type) {
			continue
		}
		lines = append(lines, "  "+renderLine(sub, opts))
	}
	return lines
}

// renderLine renders one entry's own list item, as opts shapes it.
func renderLine(e Entry, opts Options) string {
	var b strings.Builder
	b.WriteString("- ")
	if e.Scope != "" {
		fmt.Fprintf(&b, "**%s:** ", e.Scope)
	}
	description := e.Description
	if opts.SentenceCase {
		description = sentenceCase(description)
	}
	b.WriteString(description)
	if e.PullRequestNumber > 0 && e.URL != "" {
		fmt.Fprintf(&b, " ([#%d](%s))", e.PullRequestNumber, e.URL)
	}
	return b.String()
}
