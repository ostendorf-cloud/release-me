// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package updater returns the updated content of a release-managed file. It
// never writes to disk; the command layer does, in a temporary checkout.
package updater

import (
	"fmt"
	"regexp"
	"strings"
)

// The names release-me.yaml accepts for a "files" entry's updater key.
const (
	// Generic text updaters, by marker or by every version-shaped match.
	Marker  = "marker"
	Generic = "generic"
	Plain   = "plain"

	// Structured formats, addressed by a dotted key: JSON, YAML, TOML and XML.
	JSON = "json"
	YAML = "yaml"
	TOML = "toml"
	XML  = "xml"

	// Lockfiles, each recording a resolved version of its own: npm, Cargo and Bundler.
	NpmLock     = "npm-lock"
	CargoLock   = "cargo-lock"
	GemfileLock = "gemfile-lock"

	// Language and ecosystem manifests, one updater per release-please release type.
	Go              = "go"
	Helm            = "helm"
	Python          = "python"
	Ruby            = "ruby"
	Elixir          = "elixir"
	OCaml           = "ocaml"
	R               = "r"
	Bazel           = "bazel"
	Dart            = "dart"
	Sfdx            = "sfdx"
	TerraformModule = "terraform-module"
	Expo            = "expo"
	KrmBlueprint    = "krm-blueprint"
)

// Func is the shape every registered updater implements: given a file's
// current content, the configured key (empty for an updater that takes
// none) and the next version, it returns the file's new content, keeping
// everything else - order, comments, indentation - byte for byte.
type Func func(content, key, version string) (string, error)

// Entry is one registered updater: its implementation and whether a
// "files" entry naming it must carry a key.
type Entry struct {
	Update   Func
	NeedsKey bool
}

// Updaters maps a configured updater name to its implementation.
var Updaters = map[string]Entry{
	// Generic text updaters, by marker or by every version-shaped match.
	Marker:  {Update: ignoreKey(UpdateMarker)},
	Generic: {Update: ignoreKey(UpdateGeneric)},
	Plain:   {Update: ignoreKey(UpdatePlain)},

	// Structured formats, addressed by a dotted key: JSON, YAML, TOML and XML.
	JSON: {Update: UpdateJSON, NeedsKey: true},
	YAML: {Update: UpdateYAML, NeedsKey: true},
	TOML: {Update: UpdateTOML, NeedsKey: true},
	XML:  {Update: UpdateXML, NeedsKey: true},

	// Lockfiles, each recording a resolved version of its own: npm, Cargo and Bundler.
	NpmLock:     {Update: ignoreKey(UpdateNpmLock)},
	CargoLock:   {Update: UpdateCargoLock, NeedsKey: true},
	GemfileLock: {Update: UpdateGemfileLock, NeedsKey: true},

	// Language and ecosystem manifests, one updater per release-please release type.
	Go:              {Update: UpdateGo, NeedsKey: true},
	Helm:            {Update: ignoreKey(UpdateHelm)},
	Python:          {Update: ignoreKey(UpdatePython)},
	Ruby:            {Update: ignoreKey(UpdateRuby)},
	Elixir:          {Update: ignoreKey(UpdateElixir)},
	OCaml:           {Update: ignoreKey(UpdateOCaml)},
	R:               {Update: ignoreKey(UpdateR)},
	Bazel:           {Update: ignoreKey(UpdateBazel)},
	Dart:            {Update: ignoreKey(UpdateDart)},
	Sfdx:            {Update: ignoreKey(UpdateSfdx)},
	TerraformModule: {Update: ignoreKey(UpdateTerraformModule)},
	Expo:            {Update: UpdateExpo, NeedsKey: true},
	KrmBlueprint:    {Update: ignoreKey(UpdateKrmBlueprint)},
}

// ignoreKey adapts an updater that takes no key to the Func shape every
// entry in Updaters carries.
func ignoreKey(f func(content, version string) (string, error)) Func {
	return func(content, _, version string) (string, error) {
		return f(content, version)
	}
}

// markerLine splits a line into the text before a trailing release-me
// marker comment and the comment itself, kept verbatim.
var markerLine = regexp.MustCompile(`^(.*?)((?:#|//)\s*release-me:\s*version\s*)$`)

// versionLiteral matches a dotted-number version such as "1.2.3", together
// with any pre-release suffix introduced by "-" and any build metadata
// suffix introduced by "+" already attached to it, for example
// "1.2.3-rc.1" or "1.2.3+build.5". The grammar matches
// internal/version.tagPattern's own pre-release and build identifiers, so a
// version replaced here is never left with a stale suffix from the old one.
var versionLiteral = regexp.MustCompile(`\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?`)

// UpdateMarker replaces the version on every line of content that ends in
// "# release-me: version" or "// release-me: version" with version. The
// version replaced is the first dotted-number literal, together with any
// pre-release or build suffix already attached to it, found before the
// marker comment on that line; everything else on the line, including the
// comment itself, is kept unchanged.
//
// An error is returned when content has no line ending in a marker comment,
// or when a marked line carries no version literal to replace.
func UpdateMarker(content, version string) (string, error) {
	lines := strings.Split(content, "\n")
	found := false
	for i, line := range lines {
		m := markerLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		prefix, suffix := m[1], m[2]
		loc := versionLiteral.FindStringIndex(prefix)
		if loc == nil {
			return "", fmt.Errorf("updater: marker: line %d has no version before the marker: %q", i+1, line)
		}
		lines[i] = prefix[:loc[0]] + version + prefix[loc[1]:] + suffix
		found = true
	}
	if !found {
		return "", fmt.Errorf("updater: marker: no line ends in a release-me version marker")
	}
	return strings.Join(lines, "\n"), nil
}

// helmVersionLine matches a Chart.yaml's top-level "version" field: no
// leading indentation, since a dependency's own version field is always
// indented under its list entry.
var helmVersionLine = valueLinePattern(`version\s*:\s*`)

// UpdateHelm replaces the value of a Chart.yaml's top-level "version"
// field with version, keeping everything else - the rest of the line
// included - unchanged. An error is returned when no such field is found.
func UpdateHelm(content, version string) (string, error) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		m := helmVersionLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lines[i] = m[1] + quoteLike(m[2], version) + m[3]
		return strings.Join(lines, "\n"), nil
	}
	return "", fmt.Errorf("updater: helm: no top-level version field found")
}

// goAssignment matches a line declaring key as a string constant or
// variable: "const key = \"...\"" or "var key = \"...\"", with an optional
// type between the name and "=". The keyword has to sit on the same line as
// the identifier, so an identifier inside a grouped "const (" block is not
// matched.
func goAssignment(key string) *regexp.Regexp {
	return regexp.MustCompile(`^(\s*(?:const|var)\s+` + regexp.QuoteMeta(key) + `\s+(?:[A-Za-z_][\w.]*\s+)?=\s*)"([^"]*)"(.*)$`)
}

// UpdateGo replaces the string literal assigned to the constant or variable
// named key with version, keeping everything else - the rest of the line
// included - unchanged. Only the first such declaration is replaced. An
// error is returned when no such declaration is found, a grouped
// "const (" block's entry among them, see goAssignment.
func UpdateGo(content, key, version string) (string, error) {
	pattern := goAssignment(key)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		m := pattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lines[i] = m[1] + `"` + version + `"` + m[3]
		return strings.Join(lines, "\n"), nil
	}
	return "", fmt.Errorf("updater: go: identifier %q not found", key)
}

// UpdateJSON replaces the string value at key, a dotted path of JSON object
// keys such as "a.b.version", with version. Every object named by a path
// segment but the last must open on its own line, "\"segment\": {"; the
// final segment's value must be a JSON string on its own line, "\"key\":
// \"value\"". A single-segment key names the top level only. Nesting is
// tracked through unquoted "{" and "}" characters, so a leaf absent from
// the targeted object, or a same-named leaf at the wrong depth, is never
// mistaken for a match. Everything else - the rest of the line included -
// is kept unchanged. An error is returned when the path is not found in
// that shape.
func UpdateJSON(content, key, version string) (string, error) {
	if out, ok := updateJSONPath(content, strings.Split(key, "."), version); ok {
		return out, nil
	}
	return "", fmt.Errorf("updater: json: key %q not found", key)
}

// updateJSONPath is UpdateJSON over a path already split into its
// segments, so a segment may be empty, as the key of a package-lock.json's
// root package is. It reports whether the path was found.
func updateJSONPath(content string, segments []string, version string) (string, bool) {
	last := valueLinePattern(`\s*"` + regexp.QuoteMeta(segments[len(segments)-1]) + `"\s*:\s*`)
	return updateJSONValue(content, segments, last, func(string) string { return `"` + version + `"` })
}

// updateJSONValue is updateJSONPath with the leaf's line matched by last
// and its new value rendered by render, which is given the old value as the
// line carried it, quotes included. Splitting those out is what lets the
// expo updater write a bare number where an app.json holds the Android
// version code as one, which valueLinePattern's bare alternative would
// otherwise read together with the member's trailing comma.
//
// last has valueLinePattern's three groups: everything up to and including
// the ":", the value, and everything after it. It must match the same key
// as segments' last element, since that is the segment the walk descends to.
func updateJSONValue(content string, segments []string, last *regexp.Regexp, render func(old string) string) (string, bool) {
	lines := strings.Split(content, "\n")

	// depth starts below zero so the file's own enclosing "{" brings the
	// top level to depth 0, not 1.
	depth := -1
	matched := 0
	matchDepths := make([]int, 0, len(segments))
	for i, line := range lines {
		switch {
		case matched < len(segments)-1 && depth == matched:
			seg := segments[matched]
			opens := regexp.MustCompile(`^\s*"` + regexp.QuoteMeta(seg) + `"\s*:\s*\{\s*$`)
			if opens.MatchString(line) {
				depth += braceDelta(line)
				matched++
				matchDepths = append(matchDepths, depth)
				continue
			}
		case matched == len(segments)-1 && depth == matched:
			if m := last.FindStringSubmatch(line); m != nil {
				lines[i] = m[1] + render(m[2]) + m[3]
				return strings.Join(lines, "\n"), true
			}
		}

		depth += braceDelta(line)
		for matched > 0 && depth < matchDepths[matched-1] {
			matched--
			matchDepths = matchDepths[:matched]
		}
	}
	return "", false
}

// braceDelta returns the net change in JSON object nesting depth line
// contributes: the count of "{" characters minus "}" characters, both
// outside quoted strings.
func braceDelta(line string) int {
	delta := 0
	inString := false
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			escaped = false
		case inString && r == '\\':
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
			// A brace inside a string is part of the value, not structure.
		case r == '{':
			delta++
		case r == '}':
			delta--
		}
	}
	return delta
}

// UpdateYAML replaces the scalar value at key, a dotted path of YAML
// mapping keys such as "a.b.version", with version. Every mapping named by
// a path segment but the last must be a bare "segment:" line; the nested
// block it introduces is found through indentation, any consistent width.
// A single-segment key names the top level only, indent zero: it is never
// matched against a more indented, unrelated leaf of the same name. The
// final segment's value may be bare, single or double quoted; everything
// else on its line, a trailing comment included, is kept unchanged. An
// error is returned when the path is not found in that shape.
func UpdateYAML(content, key, version string) (string, error) {
	segments := strings.Split(key, ".")
	lines := strings.Split(content, "\n")
	indents := make([]int, 0, len(segments))
	matched := 0
	final := valueLinePattern(`\s*` + regexp.QuoteMeta(segments[len(segments)-1]) + `\s*:\s*`)
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(trimmed)
		for matched > 0 && indent <= indents[matched-1] {
			matched--
			indents = indents[:matched]
		}
		if matched < len(segments)-1 {
			seg := segments[matched]
			if regexp.MustCompile(`^` + regexp.QuoteMeta(seg) + `:\s*$`).MatchString(trimmed) {
				indents = append(indents, indent)
				matched++
			}
			continue
		}
		if matched == 0 && indent != 0 {
			// A single-segment key names the top level; a deeper,
			// unrelated leaf of the same name is not a match.
			continue
		}
		if m := final.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + quoteLike(m[2], version) + m[3]
			return strings.Join(lines, "\n"), nil
		}
	}
	return "", fmt.Errorf("updater: yaml: key %q not found", key)
}

// sectionHeader matches a TOML table header, "[a.b.c]".
var sectionHeader = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)

// UpdateTOML replaces the scalar value at key, a dotted path whose leading
// segments name the enclosing table, "a.b.version" meaning key "version"
// under table "[a.b]", or just "version" for a key before any table
// header. The value may be bare, single or double quoted; everything else
// on its line, a trailing comment included, is kept unchanged. An error is
// returned when the path is not found in that shape.
func UpdateTOML(content, key, version string) (string, error) {
	segments := strings.Split(key, ".")
	table := strings.Join(segments[:len(segments)-1], ".")
	last := segments[len(segments)-1]
	pattern := valueLinePattern(`\s*` + regexp.QuoteMeta(last) + `\s*=\s*`)

	lines := strings.Split(content, "\n")
	inTable := table == ""
	for i, line := range lines {
		if m := sectionHeader.FindStringSubmatch(line); m != nil {
			inTable = strings.TrimSpace(m[1]) == table
			continue
		}
		if !inTable {
			continue
		}
		if m := pattern.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + quoteLike(m[2], version) + m[3]
			return strings.Join(lines, "\n"), nil
		}
	}
	return "", fmt.Errorf("updater: toml: key %q not found", key)
}

// valueLinePattern builds a regular expression matching a line that
// assigns a scalar through keyAndSep, a regular expression fragment with
// no capturing group of its own that matches everything up to and
// including the separator, "=" or ":". Group 1 is keyAndSep's match, group
// 2 the scalar value - double quoted, single quoted or bare - and group 3
// everything after it, a trailing comment included.
func valueLinePattern(keyAndSep string) *regexp.Regexp {
	return regexp.MustCompile(`^(` + keyAndSep + `)("(?:[^"\\]|\\.)*"|'[^']*'|[^\s#]+)(.*)$`)
}

// quoteLike renders version in the same quoting style as old, a value
// valueLinePattern captured: double quotes, single quotes, or bare.
func quoteLike(old, version string) string {
	switch {
	case strings.HasPrefix(old, `"`):
		return `"` + version + `"`
	case strings.HasPrefix(old, "'"):
		return "'" + version + "'"
	default:
		return version
	}
}
