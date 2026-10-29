// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr

import (
	"regexp"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
)

// Layout names how the combined changelog of a release pull request
// carrying several components, shared or locked, lays out each
// component's slice. Only the pull request body changes: every slice reads
// back, through ComponentSection, exactly the section that component's own
// CHANGELOG.md and its own release carry, whichever layout rendered it.
type Layout string

// The layouts RenderComponents renders and ParseComponents reads back.
const (
	// LayoutHeadings renders "## <name>" per component, followed by its
	// section shifted one heading level down: "### [vX.Y.Z](…) (date)",
	// "#### Features". The default.
	LayoutHeadings Layout = "headings"
	// LayoutDetails renders one collapsed block per component,
	// "<details><summary><name> vX.Y.Z</summary>", its section at the
	// levels its CHANGELOG.md carries, then "</details>": the shape
	// release-please renders.
	LayoutDetails Layout = "details"
	// LayoutCompact renders "#### <name>" per component, followed by its
	// section at the levels its CHANGELOG.md carries: the layout every
	// release pull request had before the layout became configurable.
	LayoutCompact Layout = "compact"
)

// ComponentChangelog is one component's slice of the combined changelog of
// a release pull request carrying several components.
type ComponentChangelog struct {
	// Name is the component's name, the one its heading or summary shows.
	Name string
	// Version is the version the component releases at, the one a
	// LayoutDetails summary shows. ParseComponents leaves it "": the
	// version is read back from the pull request title, never the body.
	Version string
	// Section is the component's changelog section exactly as its own
	// CHANGELOG.md carries it, the version heading at level two and the
	// "### Features"-shaped section headings at level three, or the
	// section headings alone for a locked family, whose version heading is
	// a combined one above every component.
	Section string
}

// RenderComponents renders components in layout, in order, with no
// trailing newline: the part of a combined changelog ParseComponents reads
// back. An unknown or empty layout renders LayoutHeadings.
func RenderComponents(layout Layout, components []ComponentChangelog) string {
	parts := make([]string, 0, len(components))
	for _, c := range components {
		section := strings.TrimSpace(c.Section)
		switch layout {
		case LayoutDetails:
			parts = append(parts, "<details><summary>"+c.Name+" v"+c.Version+"</summary>\n\n"+section+"\n\n</details>")
		case LayoutCompact:
			parts = append(parts, "#### "+c.Name+"\n\n"+section)
		default:
			parts = append(parts, "## "+c.Name+"\n\n"+shiftHeadings(section, 1))
		}
	}
	return strings.Join(parts, "\n\n")
}

// detailsSummary matches the opening line of one component's LayoutDetails
// block, "<details><summary><name> vX.Y.Z</summary>". The name is greedy,
// so it keeps every word up to the last " v".
var detailsSummary = regexp.MustCompile(`^<details><summary>(.+) v\S+</summary>$`)

// headingsComponent matches the line every LayoutHeadings component opens
// with, "## <name>". A version heading has the same shape, so
// isComponentHeading tells the two apart.
var headingsComponent = regexp.MustCompile(`^## (.+)$`)

// compactComponent matches the line every LayoutCompact component opens
// with, "#### <name>".
var compactComponent = regexp.MustCompile(`^#### (.+)$`)

// DetectLayout reports the layout changelogText, the combined changelog of
// a release pull request, was rendered in, read from its shape alone, so a
// pull request opened under one layout reads back correctly after
// release-pull-request-layout changed: a line opening a details block with
// a "<name> vX.Y.Z" summary is LayoutDetails, a level two heading that is
// not a version heading is LayoutHeadings, and anything else, a body
// carrying a single component's changelog included, is LayoutCompact. The
// order matters: a LayoutHeadings section carries "#### Features" lines a
// LayoutCompact reading would take for components, and a LayoutCompact
// section carries the "## [vX.Y.Z](…)" version headings a LayoutHeadings
// reading must skip.
func DetectLayout(changelogText string) Layout {
	lines := strings.Split(changelogText, "\n")
	for _, line := range lines {
		if detailsSummary.MatchString(line) {
			return LayoutDetails
		}
	}
	for _, line := range lines {
		if isComponentHeading(line) {
			return LayoutHeadings
		}
	}
	return LayoutCompact
}

// isComponentHeading reports whether line is a LayoutHeadings component
// heading: "## <name>", never a "## [vX.Y.Z](…) (date)" or "## vX.Y.Z
// (date)" version heading, which a locked family's combined heading and
// every LayoutCompact section carry at the same level.
func isComponentHeading(line string) bool {
	if !headingsComponent.MatchString(line) {
		return false
	}
	_, isVersion := changelog.ParseHeading(line)
	return !isVersion
}

// ParseComponents splits changelogText, the combined changelog of a
// release pull request, into the text above the first component, a locked
// family's combined version heading or a single component's whole
// changelog, and every component's slice, in order, each Section back at
// the levels its own CHANGELOG.md carries. The layout is detected from the
// text, see DetectLayout, never taken from the configuration. Only a
// component's own opening line, at the top level of the changelog, starts
// a slice: the headings inside a section, and in LayoutDetails anything
// before the block's own closing "</details>", never do.
func ParseComponents(changelogText string) (preamble string, components []ComponentChangelog) {
	layout := DetectLayout(changelogText)
	lines := strings.Split(changelogText, "\n")

	var before []string
	var current *ComponentChangelog
	var body []string
	finish := func() {
		if current == nil {
			return
		}
		section := strings.TrimSpace(strings.Join(body, "\n"))
		if layout == LayoutHeadings {
			section = shiftHeadings(section, -1)
		}
		current.Section = section
		components = append(components, *current)
		current, body = nil, nil
	}

	if layout == LayoutDetails {
		depth := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			switch {
			case depth == 0:
				if m := detailsSummary.FindStringSubmatch(line); m != nil {
					current = &ComponentChangelog{Name: m[1]}
					depth = 1
				} else if components == nil {
					before = append(before, line)
				}
			case strings.HasPrefix(trimmed, "</details>"):
				depth--
				if depth == 0 {
					finish()
					continue
				}
				body = append(body, line)
			default:
				if strings.HasPrefix(trimmed, "<details") {
					depth++
				}
				body = append(body, line)
			}
		}
		finish()
		return strings.TrimSpace(strings.Join(before, "\n")), components
	}

	for _, line := range lines {
		name, opens := "", false
		switch layout {
		case LayoutHeadings:
			if isComponentHeading(line) {
				name, opens = headingsComponent.FindStringSubmatch(line)[1], true
			}
		default:
			if m := compactComponent.FindStringSubmatch(line); m != nil {
				name, opens = m[1], true
			}
		}
		switch {
		case opens:
			finish()
			current = &ComponentChangelog{Name: name}
		case current == nil:
			before = append(before, line)
		default:
			body = append(body, line)
		}
	}
	finish()
	return strings.TrimSpace(strings.Join(before, "\n")), components
}

// shiftHeadings returns section with every Markdown heading line moved by
// levels, one level down for 1, one up for -1. A changelog section's only
// lines opening with "#" are its version heading and its section
// headings; every entry line opens with "-".
func shiftHeadings(section string, levels int) string {
	lines := strings.Split(section, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case levels > 0:
			lines[i] = strings.Repeat("#", levels) + line
		case levels < 0 && strings.HasPrefix(line, strings.Repeat("#", 1-levels)):
			lines[i] = line[-levels:]
		}
	}
	return strings.Join(lines, "\n")
}
