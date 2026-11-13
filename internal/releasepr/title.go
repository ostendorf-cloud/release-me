// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// TitleKind names which release pull request a title pattern renders the
// title of: each kind carries a different set of placeholders, since
// release reads a different set of values back from each.
type TitleKind int

const (
	// TitlePerComponent is the title of one component's own release pull
	// request, "release-pull-request-title".
	TitlePerComponent TitleKind = iota
	// TitleLocked is the title of the release pull request a locked family
	// shares, one version for every component,
	// "release-pull-request-shared-title" in versioning locked.
	TitleLocked
	// TitleShared is the title of the release pull request every component
	// shares with "release-pull-request: shared", each component at its own
	// version, "release-pull-request-shared-title" outside versioning
	// locked.
	TitleShared
)

// The default title patterns: the titles release-me rendered before the
// patterns could be configured, so a release pull request opened before a
// pattern was set still reads back.
const (
	DefaultTitlePattern       = "chore(${component}): release v${version}"
	DefaultLockedTitlePattern = "chore: release v${version} (${components})"
	DefaultSharedTitlePattern = "chore: release ${releases}"
)

// The placeholders a title pattern may carry.
const (
	// PlaceholderVersion is the version, "X.Y.Z" or "X.Y.Z-<pre>", without a
	// "v".
	PlaceholderVersion = "version"
	// PlaceholderComponent is the component's name.
	PlaceholderComponent = "component"
	// PlaceholderComponents is every component's name, comma separated, in
	// release-me.yaml's order.
	PlaceholderComponents = "components"
	// PlaceholderReleases is every component with its version,
	// "api v1.3.0, web v0.4.1".
	PlaceholderReleases = "releases"
	// PlaceholderBranch is the branch the release pull request merges into,
	// the repository's default branch.
	PlaceholderBranch = "branch"
)

// titleKinds lists, per kind, the placeholders release reads back from the
// title after the merge, every component and its version, which a pattern
// of that kind must carry, then every other placeholder the kind accepts.
var titleKinds = map[TitleKind]struct {
	required []string
	optional []string
	fallback string
}{
	TitlePerComponent: {[]string{PlaceholderComponent, PlaceholderVersion}, []string{PlaceholderBranch}, DefaultTitlePattern},
	TitleLocked:       {[]string{PlaceholderVersion, PlaceholderComponents}, []string{PlaceholderBranch}, DefaultLockedTitlePattern},
	TitleShared:       {[]string{PlaceholderReleases}, []string{PlaceholderComponents, PlaceholderBranch}, DefaultSharedTitlePattern},
}

// The pieces of the expression a title is parsed with, one per placeholder.
// Each is as narrow as the value it captures, so a placeholder never swallows
// the literal text beside it: a component name holds letters, digits, "-",
// "_" and "." only, see config.ValidateComponentName.
const (
	versionExpr = `\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?`
	nameExpr    = `[A-Za-z0-9._-]+`
)

var placeholderExprs = map[string]string{
	PlaceholderVersion:    versionExpr,
	PlaceholderComponent:  nameExpr,
	PlaceholderComponents: nameExpr + `(?:, ` + nameExpr + `)*`,
	PlaceholderReleases:   nameExpr + ` v` + versionExpr + `(?:, ` + nameExpr + ` v` + versionExpr + `)*`,
	PlaceholderBranch:     `\S+`,
}

// placeholderPattern matches one "${name}" placeholder of a title pattern.
var placeholderPattern = regexp.MustCompile(`\$\{([^}]*)\}`)

// releasePart matches one comma separated entry of a "${releases}" value.
var releasePart = regexp.MustCompile(`^(` + nameExpr + `) v(` + versionExpr + `)$`)

// TitleRelease is one component a shared release pull request title names,
// at the version it releases.
type TitleRelease struct {
	Name    string
	Version string
}

// TitleValues is what a title pattern renders, and what Parse reads back:
// only the fields the pattern's placeholders name are used or set.
type TitleValues struct {
	Version    string
	Component  string
	Components []string
	Releases   []TitleRelease
	Branch     string
}

// TitlePattern is a release pull request title pattern: literal text and
// "${name}" placeholders. It renders a title, and parses one back with the
// same pattern, which is how release reads the version after the merge.
type TitlePattern struct {
	raw          string
	placeholders []string
	expr         *regexp.Regexp
}

// NewTitlePattern checks pattern for kind and compiles it. It refuses a
// pattern release could not read reliably back: one without every
// placeholder release reads back, the components and their versions, with a
// placeholder kind does not carry, or one used twice, or two placeholders with no literal text between them,
// where nothing marks where one value ends and the next begins.
func NewTitlePattern(pattern string, kind TitleKind) (TitlePattern, error) {
	spec, ok := titleKinds[kind]
	if !ok {
		return TitlePattern{}, fmt.Errorf("unknown title kind %d", kind)
	}
	if strings.TrimSpace(pattern) == "" {
		return TitlePattern{}, errors.New("is empty")
	}
	if strings.ContainsAny(pattern, "\r\n") {
		return TitlePattern{}, errors.New("spans more than one line, a title is one line")
	}
	if strings.Contains(pattern, "-->") {
		return TitlePattern{}, errors.New(`carries "-->", which would close the comment the body records the pattern in`)
	}

	allowed := append(slices.Clone(spec.required), spec.optional...)
	var b strings.Builder
	b.WriteString("^")
	var names []string
	last := 0
	for _, loc := range placeholderPattern.FindAllStringSubmatchIndex(pattern, -1) {
		name := pattern[loc[2]:loc[3]]
		if !slices.Contains(allowed, name) {
			return TitlePattern{}, fmt.Errorf("carries ${%s}, which is none of %s", name, placeholderList(allowed))
		}
		if slices.Contains(names, name) {
			return TitlePattern{}, fmt.Errorf("carries ${%s} twice, release reads each value back from one place", name)
		}
		if len(names) > 0 && loc[0] == last {
			return TitlePattern{}, fmt.Errorf("carries ${%s} right after ${%s}, literal text must separate them so release can tell where one ends",
				name, names[len(names)-1])
		}
		b.WriteString(regexp.QuoteMeta(pattern[last:loc[0]]))
		b.WriteString("(" + placeholderExprs[name] + ")")
		names = append(names, name)
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(pattern[last:]))
	b.WriteString("$")
	for _, required := range spec.required {
		if !slices.Contains(names, required) {
			return TitlePattern{}, fmt.Errorf("carries no ${%s}, which release reads back from the title after the merge", required)
		}
	}
	if strings.Contains(placeholderPattern.ReplaceAllString(pattern, ""), "${") {
		return TitlePattern{}, errors.New(`carries an unclosed "${"`)
	}
	return TitlePattern{raw: pattern, placeholders: names, expr: regexp.MustCompile(b.String())}, nil
}

// DefaultTitle returns the default pattern for kind.
func DefaultTitle(kind TitleKind) TitlePattern {
	p, err := NewTitlePattern(titleKinds[kind].fallback, kind)
	if err != nil {
		panic(fmt.Sprintf("releasepr: default title pattern for kind %d: %v", kind, err))
	}
	return p
}

// TitlePatterns returns the patterns release reads a title of kind back
// with, in order: configured, when it is not empty, then the default, so
// a release pull request opened before the pattern was set still releases.
func TitlePatterns(configured string, kind TitleKind) ([]TitlePattern, error) {
	def := DefaultTitle(kind)
	if configured == "" || configured == def.raw {
		return []TitlePattern{def}, nil
	}
	p, err := NewTitlePattern(configured, kind)
	if err != nil {
		return nil, err
	}
	return []TitlePattern{p, def}, nil
}

// titleMarker matches the comment a release pull request body records a
// configured title pattern in, see TitlePatternMarker.
var titleMarker = regexp.MustCompile(`(?m)^<!-- release-me-title-pattern: (.*) -->$`)

// TitlePatternMarker returns the line a release pull request body carries
// for a title rendered with p, an HTML comment no reader sees, so release
// reads the title back with the pattern it was rendered with even after
// "release-pull-request-title" or "release-pull-request-shared-title"
// changed: "" for the default pattern, which release always tries.
func TitlePatternMarker(p TitlePattern, kind TitleKind) string {
	if p.raw == "" || p.raw == titleKinds[kind].fallback {
		return ""
	}
	return "\n<!-- release-me-title-pattern: " + p.raw + " -->\n"
}

// BodyTitlePattern returns the title pattern body records, see
// TitlePatternMarker, compiled for kind. ok is false when body records none,
// or one that does not compile for kind. body is read with
// normalizeLineEndings first, for the same reason releasepr.go's
// ExtractNotes does: titleMarker's "^"/"$" anchors, and the
// notesBlock/versionBlock exclusion list that keeps a quoted marker inside
// someone's notes from being read as the real one, both need a "\n"-only
// body to match a CRLF one correctly.
func BodyTitlePattern(body string, kind TitleKind) (p TitlePattern, ok bool) {
	body = normalizeLineEndings(body)
	fenced := append(notesBlock.FindAllStringIndex(body, -1), versionBlock.FindAllStringIndex(body, -1)...)
	for _, loc := range titleMarker.FindAllStringSubmatchIndex(body, -1) {
		if insideAny(loc[0], fenced) {
			continue
		}
		p, err := NewTitlePattern(body[loc[2]:loc[3]], kind)
		if err == nil {
			return p, true
		}
	}
	return TitlePattern{}, false
}

// ReadPatterns returns the patterns release reads the title of a merged
// release pull request of kind back with: its recorded pattern first, then
// the legacy default shape, then configured fallbacks. A current setting
// must never reinterpret a title created with an earlier pattern. body is
// read with normalizeLineEndings first, before being handed to
// BodyTitlePattern, which normalizes it again itself: redundant, but it
// keeps "a function with a body parameter normalizes it first" true here
// too, not just in the function that happens to need it today.
func ReadPatterns(configured []TitlePattern, body string, kind TitleKind) []TitlePattern {
	body = normalizeLineEndings(body)
	out := make([]TitlePattern, 0, len(configured)+2)
	add := func(p TitlePattern) {
		if !slices.ContainsFunc(out, func(o TitlePattern) bool { return o.raw == p.raw }) {
			out = append(out, p)
		}
	}
	if p, ok := BodyTitlePattern(body, kind); ok {
		add(p)
	}
	add(DefaultTitle(kind))
	for _, p := range configured {
		add(p)
	}
	return out
}

// String returns the pattern as configured.
func (p TitlePattern) String() string {
	return p.raw
}

// Has reports whether the pattern carries the placeholder name.
func (p TitlePattern) Has(name string) bool {
	return slices.Contains(p.placeholders, name)
}

// Render renders the title for v. It parses the result back with the same
// pattern and fails when that does not give v's values back, so a title is
// never written that release would read differently after the merge: a
// literal text after ${version} that itself looks like a pre-release
// suffix, say.
func (p TitlePattern) Render(v TitleValues) (string, error) {
	title := placeholderPattern.ReplaceAllStringFunc(p.raw, func(m string) string {
		switch m[2 : len(m)-1] {
		case PlaceholderVersion:
			return v.Version
		case PlaceholderComponent:
			return v.Component
		case PlaceholderComponents:
			return strings.Join(v.Components, ", ")
		case PlaceholderReleases:
			parts := make([]string, 0, len(v.Releases))
			for _, r := range v.Releases {
				parts = append(parts, fmt.Sprintf("%s v%s", r.Name, r.Version))
			}
			return strings.Join(parts, ", ")
		case PlaceholderBranch:
			return v.Branch
		}
		return m
	})
	back, ok := p.Parse(title)
	if !ok || !p.sameValues(back, v) {
		return "", fmt.Errorf("title pattern %q: title %q does not read back as the values it was rendered from", p.raw, title)
	}
	return title, nil
}

// sameValues reports whether got carries want's value for every placeholder
// the pattern names.
func (p TitlePattern) sameValues(got, want TitleValues) bool {
	for _, name := range p.placeholders {
		switch name {
		case PlaceholderVersion:
			if got.Version != want.Version {
				return false
			}
		case PlaceholderComponent:
			if got.Component != want.Component {
				return false
			}
		case PlaceholderComponents:
			if !slices.Equal(got.Components, want.Components) {
				return false
			}
		case PlaceholderReleases:
			if !slices.Equal(got.Releases, want.Releases) {
				return false
			}
		case PlaceholderBranch:
			if got.Branch != want.Branch {
				return false
			}
		}
	}
	return true
}

// Parse reads title back with the pattern. ok is false when title does not
// match it. Only the fields the pattern's placeholders name are set.
func (p TitlePattern) Parse(title string) (v TitleValues, ok bool) {
	m := p.expr.FindStringSubmatch(title)
	if m == nil {
		return TitleValues{}, false
	}
	for i, name := range p.placeholders {
		value := m[i+1]
		switch name {
		case PlaceholderVersion:
			v.Version = value
		case PlaceholderComponent:
			v.Component = value
		case PlaceholderComponents:
			v.Components = strings.Split(value, ", ")
		case PlaceholderReleases:
			for _, part := range strings.Split(value, ", ") {
				pm := releasePart.FindStringSubmatch(part)
				if pm == nil {
					return TitleValues{}, false
				}
				v.Releases = append(v.Releases, TitleRelease{Name: pm[1], Version: pm[2]})
			}
		case PlaceholderBranch:
			v.Branch = value
		}
	}
	return v, true
}

// ParseAny reads title back with the first of patterns it matches. ok is
// false when it matches none.
func ParseAny(patterns []TitlePattern, title string) (v TitleValues, matched TitlePattern, ok bool) {
	for _, p := range patterns {
		if v, ok := p.Parse(title); ok {
			return v, p, true
		}
	}
	return TitleValues{}, TitlePattern{}, false
}

// placeholderList renders names as "${a}, ${b} or ${c}".
func placeholderList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, "${"+n+"}")
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}
