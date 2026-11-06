// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package updater

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// replaceFirstLine replaces the value on the first line of content pattern
// matches. pattern has three groups: everything before the value, the
// value, and everything after it; the value is rendered through quoteLike,
// so a quoted value stays quoted the same way. It reports whether a line
// matched.
func replaceFirstLine(content string, pattern *regexp.Regexp, version string) (string, bool) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if m := pattern.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + quoteLike(m[2], version) + m[3]
			return strings.Join(lines, "\n"), true
		}
	}
	return "", false
}

// quotedValue matches a single or double quoted string, the value most
// release types' version files assign.
const quotedValue = `("[^"]*"|'[^']*')`

// pythonVersion matches the version assignment of a Python package's
// version files: "__version__ = \"1.2.3\"" in an __init__.py or a
// version.py, the keyword argument "version=\"1.2.3\"," in setup.py, and
// the bare "version = 1.2.3" of setup.cfg. A bare value must start with a
// digit, so "version = attr: pkg.__version__" or
// "version=get_version()," is never mistaken for one.
var pythonVersion = regexp.MustCompile(`^(\s*(?:__version__|version)\s*=\s*)("[^"]*"|'[^']*'|\d[^\s#,)]*)(.*)$`)

// UpdatePython replaces the version a Python package's version file
// assigns: the first "__version__ = ..." or "version = ..." line, the
// shape of an __init__.py, a version.py, setup.py and setup.cfg alike. A
// quoted version stays quoted the same way. An error is returned when no
// such line holds a literal version.
func UpdatePython(content, version string) (string, error) {
	if out, ok := replaceFirstLine(content, pythonVersion, version); ok {
		return out, nil
	}
	return "", fmt.Errorf("updater: python: no __version__ or version assignment holding a version found")
}

// rubyVersion matches a version.rb's "VERSION = \"1.2.3\"" constant, a
// trailing ".freeze" included in what follows the value.
var rubyVersion = regexp.MustCompile(`^(\s*VERSION\s*=\s*)` + quotedValue + `(.*)$`)

// UpdateRuby replaces the string a Ruby gem's version.rb assigns to its
// VERSION constant. An error is returned when there is no such constant
// holding a string.
func UpdateRuby(content, version string) (string, error) {
	if out, ok := replaceFirstLine(content, rubyVersion, version); ok {
		return out, nil
	}
	return "", fmt.Errorf("updater: ruby: no VERSION constant holding a string found")
}

// elixirAttribute matches mix.exs's "@version \"1.2.3\"" module attribute,
// elixirKeyword the "version: \"1.2.3\"" keyword in its project/0.
var (
	elixirAttribute = regexp.MustCompile(`^(\s*@version\s+)("[^"]*")(.*)$`)
	elixirKeyword   = regexp.MustCompile(`^(\s*version:\s*)("[^"]*")(.*)$`)
)

// UpdateElixir replaces the version of a mix.exs: its @version module
// attribute where it has one, which project/0 then refers to, otherwise
// the string of the "version:" keyword, as release-please's mix.exs updater
// does. An error is returned when neither is found.
func UpdateElixir(content, version string) (string, error) {
	if out, ok := replaceFirstLine(content, elixirAttribute, version); ok {
		return out, nil
	}
	if out, ok := replaceFirstLine(content, elixirKeyword, version); ok {
		return out, nil
	}
	return "", fmt.Errorf("updater: elixir: no @version attribute or version keyword holding a string found")
}

// opamVersion matches an opam file's top-level "version: \"1.2.3\"" field,
// duneVersion a dune-project's "(version 1.2.3)" stanza.
var (
	opamVersion = regexp.MustCompile(`^(version:\s*)("[^"]*")(\s*(?:#.*)?)$`)
	duneVersion = regexp.MustCompile(`^(\(version\s+)([^\s)]+)(\).*)$`)
)

// UpdateOCaml replaces the version of an OCaml package file: the top-level
// "version:" field of an .opam or .opam.locked file, or the "(version ...)"
// stanza of a dune-project. An error is returned when neither is found.
func UpdateOCaml(content, version string) (string, error) {
	if out, ok := replaceFirstLine(content, opamVersion, version); ok {
		return out, nil
	}
	if out, ok := replaceFirstLine(content, duneVersion, version); ok {
		return out, nil
	}
	return "", fmt.Errorf("updater: ocaml: no opam version field or dune-project version stanza found")
}

// rDescriptionVersion matches the "Version: 1.2.3" field of an R package's
// DESCRIPTION file, a development suffix such as ".9000" included in the
// value.
var rDescriptionVersion = regexp.MustCompile(`^(Version:\s*)(\S+)(\s*)$`)

// UpdateR replaces the Version field of an R package's DESCRIPTION file.
// An error is returned when there is no such field.
func UpdateR(content, version string) (string, error) {
	if out, ok := replaceFirstLine(content, rDescriptionVersion, version); ok {
		return out, nil
	}
	return "", fmt.Errorf("updater: r: no Version field found")
}

// bazelModule matches the start of a MODULE.bazel's module() call, and
// bazelVersion its version attribute.
var (
	bazelModule  = regexp.MustCompile(`(?m)^module\s*\(`)
	bazelVersion = regexp.MustCompile(`\bversion\s*=\s*` + quotedValue)
)

// UpdateBazel replaces the version attribute of a MODULE.bazel's module()
// call. The version attributes of bazel_dep() and every other call are
// left alone. An error is returned when there is no module() call, or when
// it carries no version attribute.
func UpdateBazel(content, version string) (string, error) {
	loc := bazelModule.FindStringIndex(content)
	if loc == nil {
		return "", fmt.Errorf("updater: bazel: no module() call found")
	}
	end := closingParen(content, loc[1])
	if end < 0 {
		return "", fmt.Errorf("updater: bazel: the module() call is never closed")
	}
	call := content[loc[1]:end]
	m := bazelVersionArgument(call)
	if m == nil {
		return "", fmt.Errorf("updater: bazel: the module() call has no version attribute")
	}
	start, stop := loc[1]+m[2], loc[1]+m[3]
	return content[:start] + quoteLike(content[start:stop], version) + content[stop:], nil
}

// bazelVersionArgument locates the top-level keyword and its string span,
// skipping comments, other strings and nested expressions in the call.
func bazelVersionArgument(call string) []int {
	depth := 0
	for i := 0; i < len(call); i++ {
		switch c := call[i]; c {
		case '#':
			for i < len(call) && call[i] != '\n' {
				i++
			}
		case '\'', '"':
			quote := c
			for i++; i < len(call); i++ {
				if call[i] == '\\' {
					i++
					continue
				}
				if call[i] == quote {
					break
				}
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		default:
			if depth == 0 && strings.HasPrefix(call[i:], "version") {
				if i > 0 {
					prev := call[i-1]
					if prev == '_' || prev >= 'a' && prev <= 'z' || prev >= 'A' && prev <= 'Z' || prev >= '0' && prev <= '9' {
						continue
					}
				}
				if m := bazelVersion.FindStringSubmatchIndex(call[i:]); m != nil && m[0] == 0 {
					for n := range m {
						m[n] += i
					}
					return m
				}
			}
		}
	}
	return nil
}

// closingParen returns the index of the ")" that closes the call whose
// arguments start at from, skipping parentheses inside strings and
// comments, or -1 when the call is never closed.
func closingParen(content string, from int) int {
	depth := 1
	var quote byte
	for i := from; i < len(content); i++ {
		c := content[i]
		switch {
		case quote != 0:
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			for i < len(content) && content[i] != '\n' {
				i++
			}
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// pubspecVersion matches a pubspec.yaml's top-level "version: 1.2.3+4"
// field: group 2 the version, group 3 a "+build" suffix, if any.
var pubspecVersion = regexp.MustCompile(`^(version:\s*)(\d[^\s+#]*)(\+[^\s#]+)?(.*)$`)

// UpdateDart replaces the version of a Dart or Flutter package's
// pubspec.yaml. A numeric build number, "+4", counts up by one, the way
// release-please bumps it, since a Flutter app store refuses a build
// number it has seen before; any other build suffix is kept as it is. An
// error is returned when there is no top-level version field.
func UpdateDart(content, version string) (string, error) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		m := pubspecVersion.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		build := m[3]
		if n, err := strconv.Atoi(strings.TrimPrefix(build, "+")); err == nil {
			build = "+" + strconv.Itoa(n+1)
		}
		lines[i] = m[1] + version + build + m[4]
		return strings.Join(lines, "\n"), nil
	}
	return "", fmt.Errorf("updater: dart: no top-level version field found")
}
