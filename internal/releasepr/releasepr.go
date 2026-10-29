// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package releasepr renders the branch name, title and body of the release
// pull request, and parses them back out of an existing one. It writes
// nothing: the command layer commits the branch and calls the forge.
package releasepr

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
)

// Title renders the release pull request title for one component's
// version: "chore(<name>): release vX.Y.Z". The implicit "main" component
// a repository without "components" carries renders the same title it
// always has.
func Title(name, version string) string {
	return fmt.Sprintf("chore(%s): release v%s", name, version)
}

// GuideLabels is the label names rendered in the release pull request's
// "How this release works" guide: the tagged label and the three
// pre-release type labels, an empty one for a type turned off. The pending
// label never appears there.
type GuideLabels struct {
	Tagged string
	Alpha  string
	Beta   string
	Rc     string
}

// Body renders the release pull request body: the version line, the "How
// this release works" guide, the preserved release-me-version override
// block when versionOverride is not empty, the preserved release-me-notes
// block when notes is not empty, then the changelog for the version
// collapsed inside a <details> block. tag is the tag merging this pull
// request creates, and labels names the repository's configured label
// values, so the guide always matches what merging and labelling actually
// do.
func Body(version, tag string, labels GuideLabels, notes, versionOverride, changelogSection string) string {
	return render(fmt.Sprintf("Release v%s", version), tag, labels, false, notes, versionOverride, changelogSection)
}

// Release is one component a shared release pull request carries: its
// name, the version it releases at and the tag merging creates for it.
type Release struct {
	Name    string
	Version string
	Tag     string
}

// SharedBody renders the body of a release pull request every component
// shares, in VersioningIndependent or VersioningLinked with
// "release-pull-request: shared": Body's shape, with a version line naming
// every component in releases at its own version and a guide naming every
// tag merging creates. perComponentVersion is true when the components'
// versions are independent of each other, so the guide asks for one
// "<component> X.Y.Z" line per component in the release-me-version block,
// instead of one version for all of them. changelogSection carries one
// slice per component, rendered by RenderComponents in any layout, as
// ComponentSection reads it back.
func SharedBody(releases []Release, labels GuideLabels, perComponentVersion bool, notes, versionOverride, changelogSection string) string {
	parts := make([]string, 0, len(releases))
	tags := make([]string, 0, len(releases))
	for _, r := range releases {
		parts = append(parts, fmt.Sprintf("%s v%s", r.Name, r.Version))
		tags = append(tags, r.Tag)
	}
	headline := "Release " + strings.Join(parts, ", ")
	return render(headline, strings.Join(tags, ", "), labels, perComponentVersion, notes, versionOverride, changelogSection)
}

// render is the body Body and SharedBody share, below the headline line.
func render(headline, tag string, labels GuideLabels, perComponentVersion bool, notes, versionOverride, changelogSection string) string {
	var b strings.Builder
	b.WriteString(headline + "\n\n")
	b.WriteString(guideBlock(tag, labels, perComponentVersion))

	if versionOverride != "" {
		b.WriteString("\n```release-me-version\n")
		b.WriteString(escapeFence(versionOverride))
		b.WriteString("\n```\n")
	}

	if notes != "" {
		b.WriteString("\n```release-me-notes\n")
		b.WriteString(escapeFence(notes))
		b.WriteString("\n```\n")
	}

	b.WriteString("\n<details>\n<summary>Changelog</summary>\n\n")
	b.WriteString(strings.TrimRight(changelogSection, "\n"))
	b.WriteString("\n\n</details>\n")
	return b.String()
}

// guideBlock renders the collapsed "How this release works" section: one
// line each for the merge, the notes block, the pre-release labels, the
// override block and the push behaviour. The pre-release line names only
// the enabled types and is left out when every type is turned off. The
// override line asks for one "<component> X.Y.Z" line per component when
// perComponentVersion is true.
func guideBlock(tag string, labels GuideLabels, perComponentVersion bool) string {
	var b strings.Builder
	b.WriteString("<details>\n<summary>How this release works</summary>\n\n")
	fmt.Fprintf(&b, "- Merging this pull request tags `%s`, creates the release and moves the label to `%s`. "+
		"With `major-tag` enabled, the moving major tag follows a stable release.\n", tag, labels.Tagged)
	b.WriteString("- Release notes: add a fenced block with the info string `release-me-notes` anywhere in this body. " +
		"It is rendered above the entries in the changelog file and in the release and survives updates.\n")
	if perComponentVersion {
		b.WriteString("- Force the next version of a component: add a fenced block with the info string " +
			"`release-me-version` containing one `<component> X.Y.Z` line per component anywhere in this body, " +
			"or comment the configured mention followed by `release-as <component> X.Y.Z`. It wins outright " +
			"over that component's computed bump, pre-release label included, until the pull request merges " +
			"or the line is removed; one that does not parse, or is not greater than the component's last " +
			"released version, is ignored instead.\n")
	} else {
		b.WriteString("- Force the next version: add a fenced block with the info string `release-me-version` " +
			"containing `X.Y.Z` anywhere in this body, or comment the configured mention followed by " +
			"`release-as X.Y.Z`. It wins outright over the computed bump, pre-release label included, " +
			"until the pull request merges or the block is removed; one that does not parse, or is not " +
			"greater than the last released version, is ignored instead.\n")
	}
	if enabled := preReleaseLabelList(labels); enabled != "" {
		fmt.Fprintf(&b, "- Pre-release: add the label %s. The next run turns the version into `X.Y.Z-<type>.N`. "+
			"Remove the label to go back to the stable version.\n", enabled)
	}
	b.WriteString("- Hide or rewrite a change: a fenced block `release-me` in the changed pull request's body, " +
		"one conventional line per entry, an empty block hides it.\n")
	b.WriteString("- Every push to the default branch updates this pull request. " +
		"Edits to the body outside the notes block are overwritten.\n")
	b.WriteString("\n</details>\n")
	return b.String()
}

// preReleaseLabelList renders the enabled pre-release labels as prose,
// "`a`", "`a` or `b`" or "`a`, `b` or `c`", or "" when every type is
// turned off.
func preReleaseLabelList(labels GuideLabels) string {
	var names []string
	for _, l := range []string{labels.Alpha, labels.Beta, labels.Rc} {
		if l != "" {
			names = append(names, "`"+l+"`")
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
	}
}

// notesBlock matches the "release-me-notes" fenced block of a release pull
// request body. Both fences are anchored to the start of their own line,
// so a nested fence inside the notes does not truncate the match early.
var notesBlock = regexp.MustCompile("(?sm)^```release-me-notes[ \t]*$\n(.*?)^```[ \t]*$")

// ExtractNotes returns the free text of the release-me-notes fenced block in
// body. ok is false when body carries no such block.
func ExtractNotes(body string) (notes string, ok bool) {
	m := notesBlock.FindStringSubmatch(body)
	if m == nil {
		return "", false
	}
	return strings.TrimRight(m[1], "\n"), true
}

// versionBlock matches the "release-me-version" fenced block of a release
// pull request body, the version override a write-access commenter's
// "release-as" command, or a hand edit, sets. Parallel to notesBlock: both
// fences are anchored to the start of their own line, and the distinct info
// string keeps it from ever matching the "release-me-notes" block.
var versionBlock = regexp.MustCompile("(?sm)^```release-me-version[ \t]*$\n(.*?)^```[ \t]*$")

// ExtractVersionOverride returns the free text of the release-me-version
// fenced block in body, trimmed. ok is false when body carries no such
// block. The text is read back exactly as written; a caller that needs a
// real version decides for itself whether it parses.
func ExtractVersionOverride(body string) (version string, ok bool) {
	m := versionBlock.FindStringSubmatch(body)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// SetNotes returns body with its release-me-notes fenced block's content
// replaced by notes, or a new block appended at the end when body carries
// none yet. It is how a "release-notes" comment command sets the block
// immediately, without the full render Body does: the next pr run's own
// render moves it to Body's usual place.
func SetNotes(body, notes string) string {
	return setFencedBlock(body, "release-me-notes", notesBlock, notes)
}

// SetVersionOverride returns body with its release-me-version fenced
// block's content replaced by version, or a new block appended at the end
// when body carries none yet. It is how a "release-as" comment command
// sets the override immediately, without the full render Body does: the
// next pr run's own render moves it to Body's usual place.
func SetVersionOverride(body, version string) string {
	return setFencedBlock(body, "release-me-version", versionBlock, version)
}

// VersionOverrides reads the text of a release-me-version block the way a
// shared release pull request with independent versions reads it: a
// "<component> X.Y.Z" line forces that component's version, the last such
// line for a component counting, and any other non-empty line is kept as
// unscoped, the last one counting, for the caller to apply where only one
// component is released or to refuse otherwise. Values are returned as
// written; the caller decides whether they parse.
func VersionOverrides(raw string) (byComponent map[string]string, unscoped string) {
	byComponent = map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		switch len(fields) {
		case 0:
			continue
		case 2:
			byComponent[fields[0]] = fields[1]
		default:
			unscoped = strings.TrimSpace(line)
		}
	}
	return byComponent, unscoped
}

// SetComponentVersionOverride returns body with its release-me-version
// fenced block carrying the line "<name> <version>", replacing an earlier
// line for name and keeping every other line, or a new block appended at
// the end when body carries none yet: SetVersionOverride for one component
// of a shared release pull request with independent versions.
func SetComponentVersionOverride(body, name, version string) string {
	raw, _ := ExtractVersionOverride(body)
	var lines []string
	replaced := false
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) == 2 && fields[0] == name {
			if !replaced {
				lines = append(lines, name+" "+version)
				replaced = true
			}
			continue
		}
		lines = append(lines, strings.TrimSpace(line))
	}
	if !replaced {
		lines = append(lines, name+" "+version)
	}
	return setFencedBlock(body, "release-me-version", versionBlock, strings.Join(lines, "\n"))
}

// ComponentSection returns name's own slice of changelogText, the combined
// changelog of a release pull request every component shares, locked or
// not, in any layout: the section its own CHANGELOG.md carries, at the
// same heading levels, see ParseComponents. "" when name is not one of
// the components.
func ComponentSection(changelogText, name string) string {
	_, components := ParseComponents(changelogText)
	for _, c := range components {
		if c.Name == name {
			return c.Section
		}
	}
	return ""
}

// ComponentNames returns the name of every component in changelogText, the
// combined changelog of a shared release pull request, in order and in any
// layout: the components that pull request releases.
func ComponentNames(changelogText string) []string {
	_, components := ParseComponents(changelogText)
	var names []string
	for _, c := range components {
		names = append(names, c.Name)
	}
	return names
}

// setFencedBlock replaces pattern's match in body with a fenced block
// carrying info and escapeFence(content), or appends that block at the end
// of body when pattern does not match: the shape every comment command
// that edits a release pull request body in place, outside of Body's own
// full render, shares.
func setFencedBlock(body, info string, pattern *regexp.Regexp, content string) string {
	block := fmt.Sprintf("```%s\n%s\n```", info, escapeFence(content))
	if pattern.MatchString(body) {
		return pattern.ReplaceAllLiteralString(body, block)
	}
	return strings.TrimRight(body, "\n") + "\n\n" + block + "\n"
}

// fenceLine matches a line of fenced block content that would itself be
// read as a fence delimiter, opening or closing, by notesBlock,
// versionBlock or conventional's own override block pattern: three or
// more backticks at the true start of the line, column zero, with
// nothing, not even a space, before them. ExtractNotes and
// ExtractVersionOverride both anchor on exactly that "^```", so a line
// already indented, including one this function already escaped, never
// matches again: escapeFence is idempotent.
var fenceLine = regexp.MustCompile("(?m)^`{3,}")

// escapeFence returns content with every line that opens with three or
// more backticks, column zero, indented by one space, so content written
// verbatim into a release-me-notes or release-me-version fenced block can
// never prematurely close that block or open a second, different one
// inside it: release notes or a version override containing, say, a
// pasted "```release-me-version" block stays inert text instead of being
// read back as a real override on the next run. Markdown renderers still
// treat up to three leading spaces as an ordinary fenced code block, so
// the visible effect on a genuine code sample in release notes is
// negligible.
func escapeFence(content string) string {
	return fenceLine.ReplaceAllString(content, " $0")
}

// changelogOpen matches the opening of the collapsed changelog block of a
// release pull request body.
var changelogOpen = regexp.MustCompile(`<details>\s*<summary>Changelog</summary>`)

// ExtractChangelog returns the changelog Markdown from inside the
// <details> block of a release pull request body, with the wrapper
// removed. It returns "" when body carries no such block. An opening
// inside a release-me-notes or release-me-version block is not the
// changelog's, and the block ends at the "</details>" line that closes
// it, not at the first one: a LayoutDetails changelog nests one details
// block per component.
func ExtractChangelog(body string) string {
	fenced := append(notesBlock.FindAllStringIndex(body, -1), versionBlock.FindAllStringIndex(body, -1)...)
	for _, loc := range changelogOpen.FindAllStringIndex(body, -1) {
		if insideAny(loc[0], fenced) {
			continue
		}
		return untilClosingDetails(body[loc[1]:])
	}
	return ""
}

// insideAny reports whether offset falls inside one of ranges.
func insideAny(offset int, ranges [][]int) bool {
	for _, r := range ranges {
		if offset >= r[0] && offset < r[1] {
			return true
		}
	}
	return false
}

// untilClosingDetails returns rest, trimmed, up to the line that closes
// the details block rest is the inside of, counting every nested block a
// line opens, or "" when no line closes it. Only a line opening with the
// tag counts: a changelog entry line opens with "-".
func untilClosingDetails(rest string) string {
	lines := strings.Split(rest, "\n")
	depth := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "</details>"):
			if depth == 0 {
				return strings.TrimSpace(strings.Join(lines[:i], "\n"))
			}
			depth--
		case strings.HasPrefix(trimmed, "<details"):
			depth++
		}
	}
	return ""
}

// ExtractDate returns the date rendered in the first changelog heading
// inside the <details> block of a release pull request body, read by
// changelog.ParseHeading, with a compare link or without: the heading the
// block opens with, a locked family's combined one, or, for a shared
// release pull request, the first component's own, in any layout. ok is
// false when body carries no such block, or no heading with a parseable
// date.
func ExtractDate(body string) (date time.Time, ok bool) {
	preamble, components := ParseComponents(ExtractChangelog(body))
	texts := []string{preamble}
	for _, c := range components {
		texts = append(texts, c.Section)
	}
	for _, line := range strings.Split(strings.Join(texts, "\n"), "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		h, ok := changelog.ParseHeading(line)
		if !ok {
			continue
		}
		d, err := time.Parse("2006-01-02", h.Date)
		if err != nil {
			return time.Time{}, false
		}
		return d, true
	}
	return time.Time{}, false
}

// ReleaseBody renders the GitHub release body from a release pull request
// body: the free text of the release-me-notes block, when present, followed
// by the changelog, with the <details> wrapper removed.
func ReleaseBody(body string) string {
	var parts []string
	if notes, ok := ExtractNotes(body); ok && notes != "" {
		parts = append(parts, notes)
	}
	if changelog := ExtractChangelog(body); changelog != "" {
		parts = append(parts, changelog)
	}
	return strings.Join(parts, "\n\n")
}
