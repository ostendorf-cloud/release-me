// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package workspace

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// nodeSections are the package.json objects a dependency is declared in,
// in the order Parse reports them.
var nodeSections = []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"}

// parseNode reads a package.json.
func parseNode(content []byte) (Package, error) {
	var raw struct {
		Name                 string            `json:"name"`
		Version              string            `json:"version"`
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal(content, &raw); err != nil {
		return Package{}, fmt.Errorf("workspace: package.json: %w", err)
	}
	pkg := Package{Name: raw.Name, Version: raw.Version}
	for _, section := range []map[string]string{
		raw.Dependencies, raw.DevDependencies, raw.PeerDependencies, raw.OptionalDependencies,
	} {
		names := make([]string, 0, len(section))
		for name := range section {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			pkg.Dependencies = append(pkg.Dependencies, Dependency{Name: name, Range: section[name]})
		}
	}
	return pkg, nil
}

// nodeRange matches a node dependency range that names one version to
// move: an optional "workspace:" protocol, an optional operator and the
// version, "^1.2.0", "~1.2.0", ">=1.2.0", "1.2.0" or "workspace:^1.2.0".
var nodeRange = regexp.MustCompile(`^(workspace:)?(\^|~|>=|=)?v?(` + semverPattern + `)$`)

// moveNodeRange returns rng with the version it names replaced by version,
// keeping its protocol and operator, and false when rng names no single
// version to move: "workspace:*", "*", "file:../core" and a compound range
// stay as written.
func moveNodeRange(rng, version string) (string, bool) {
	m := nodeRange.FindStringSubmatch(rng)
	if m == nil {
		return rng, false
	}
	return m[1] + m[2] + version, true
}

// nodeSectionOpen matches the line opening one of nodeSections' objects at
// the top level of a package.json.
var nodeSectionOpen = regexp.MustCompile(`^\s*"(` + strings.Join(nodeSections, "|") + `)"\s*:\s*\{\s*$`)

// setNodeDependency moves every declaration of name in a package.json's
// dependency objects to version. The objects must open on a line of their
// own and declare one dependency per line, the layout npm, pnpm and yarn
// write; everything else in the file is kept byte for byte.
func setNodeDependency(content, name, version string) (string, error) {
	return setNodeDependencyWhere(content, name, version, nil)
}

func setNodeDependencyWhere(content, name, version string, eligible func(string) bool) (string, error) {
	entry := regexp.MustCompile(`^(\s*"` + regexp.QuoteMeta(name) + `"\s*:\s*)"((?:[^"\\]|\\.)*)"(.*)$`)
	lines := strings.Split(content, "\n")
	depth := -1
	inSection := false
	found := false
	for i, line := range lines {
		switch {
		case depth == 0 && nodeSectionOpen.MatchString(line):
			inSection = true
		case inSection && depth == 1:
			if m := entry.FindStringSubmatch(line); m != nil {
				found = true
				if moved, ok := moveNodeRange(m[2], version); ok && (eligible == nil || eligible(m[2])) {
					lines[i] = m[1] + `"` + moved + `"` + m[3]
				}
			}
		}
		depth += braceDelta(line)
		if depth < 1 {
			inSection = false
		}
	}
	if !found {
		pkg, err := parseNode([]byte(content))
		if err == nil && slices.ContainsFunc(pkg.Dependencies, func(d Dependency) bool { return d.Name == name }) {
			return "", fmt.Errorf("workspace: package.json: dependency %q is not declared on a line of its own "+
				"inside a dependency object opening on its own line", name)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// braceDelta returns the net change in JSON object nesting depth line
// contributes: the count of "{" minus "}", both outside quoted strings.
func braceDelta(line string) int {
	delta := 0
	inString, escaped := false, false
	for _, r := range line {
		switch {
		case escaped:
			escaped = false
		case inString && r == '\\':
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
		case r == '{':
			delta++
		case r == '}':
			delta--
		}
	}
	return delta
}
