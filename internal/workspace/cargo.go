// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package workspace

import (
	"fmt"
	"regexp"
	"strings"
)

// Cargo.toml is read line by line, the subset of TOML a crate manifest
// uses for its package and its dependencies: table headers, "key = value"
// lines whose value is a string, a boolean or a one-line inline table. A
// dependency declared through a multi-line value is not read.

// cargoHeader matches a table header, "[dependencies]", and captures its
// name; an array of tables, "[[bin]]", does not match.
var cargoHeader = regexp.MustCompile(`^\s*\[\s*([^\[\]]+?)\s*\]\s*(?:#.*)?$`)

// cargoKeyValue matches a "key = value" line: the key, bare, dotted or
// quoted, and the value with any trailing comment.
var cargoKeyValue = regexp.MustCompile(`^\s*("[^"]*"|[A-Za-z0-9_.-]+)\s*=\s*(.*)$`)

// cargoDepTable matches the name of a table declaring dependencies,
// "dependencies", "dev-dependencies", "build-dependencies", the same three
// under "target.<cfg>.", and "workspace.dependencies". Group 2 is the
// dependency a "[dependencies.<name>]" table declares on its own.
var cargoDepTable = regexp.MustCompile(
	`^((?:target\.(?:'[^']*'|"[^"]*"|[^.]+)\.)?(?:dev-|build-)?dependencies|workspace\.dependencies)(?:\.("[^"]*"|[A-Za-z0-9_-]+))?$`,
)

// cargoString matches a basic or literal TOML string and captures its
// content.
var cargoString = regexp.MustCompile(`^(?:"((?:[^"\\]|\\.)*)"|'([^']*)')`)

// cargoDecl is one dependency declaration of a Cargo.toml: the key it is
// declared under, the crate it names, its version requirement, "" for
// none, and where it sits.
type cargoDecl struct {
	key, crate, version  string
	workspace, inherited bool
	// line is the line holding the version, or the declaration's own line
	// when it carries none; table is true for a "[dependencies.<key>]"
	// table.
	line  int
	table bool
}

// parseCargo reads a Cargo.toml.
func parseCargo(content []byte) (Package, error) {
	lines := strings.Split(string(content), "\n")
	pkg := Package{}
	for _, d := range cargoDecls(lines) {
		pkg.Dependencies = append(pkg.Dependencies, Dependency{Name: d.crate, Range: d.version, Workspace: d.inherited})
		if d.workspace && d.key != d.crate {
			if pkg.WorkspaceAliases == nil {
				pkg.WorkspaceAliases = map[string]string{}
			}
			pkg.WorkspaceAliases[d.key] = d.crate
		}
	}
	table := ""
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			table = ""
			if m := cargoHeader.FindStringSubmatch(line); m != nil {
				table = m[1]
			}
			continue
		}
		if table != "package" {
			continue
		}
		if m := cargoKeyValue.FindStringSubmatch(line); m != nil {
			value, _ := cargoStringValue(m[2])
			switch unquoteKey(m[1]) {
			case "name":
				pkg.Name = value
			case "version":
				pkg.Version = value
			}
		}
	}
	if pkg.Name == "" && !strings.Contains(string(content), "[workspace") {
		return Package{}, fmt.Errorf("workspace: Cargo.toml: no [package] name and no [workspace]")
	}
	return pkg, nil
}

// cargoDecls returns every dependency declaration in lines, in file order.
func cargoDecls(lines []string) []cargoDecl {
	var decls []cargoDecl
	table, tableDep := "", -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			table, tableDep = "", -1
			if m := cargoHeader.FindStringSubmatch(line); m != nil {
				table = m[1]
			}
			if t := cargoDepTable.FindStringSubmatch(table); t != nil && t[2] != "" {
				key := unquoteKey(t[2])
				decls = append(decls, cargoDecl{key: key, crate: key, line: i, table: true, workspace: strings.HasPrefix(table, "workspace.dependencies")})
				tableDep = len(decls) - 1
			}
			continue
		}
		m := cargoKeyValue.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, value := unquoteKey(m[1]), strings.TrimSpace(m[2])
		if tableDep >= 0 {
			d := &decls[tableDep]
			switch key {
			case "version":
				d.version, _ = cargoStringValue(value)
				d.line = i
			case "package":
				d.crate, _ = cargoStringValue(value)
			case "workspace":
				d.inherited = cargoTrue(value)
			}
			continue
		}
		if !cargoDepTable.MatchString(table) {
			continue
		}
		name, field, dotted := strings.Cut(key, ".")
		d := cargoDecl{key: name, crate: name, line: i, workspace: strings.HasPrefix(table, "workspace.dependencies")}
		switch {
		case dotted:
			d.inherited = field == "workspace" && cargoTrue(value)
		case strings.HasPrefix(value, "{"):
			fields := cargoInlineTable(value)
			d.version = fields["version"]
			d.inherited = fields["workspace"] == "true"
			if p, ok := fields["package"]; ok {
				d.crate = p
			}
		default:
			d.version, _ = cargoStringValue(value)
		}
		decls = append(decls, d)
	}
	return decls
}

// cargoInlineTable reads the string fields of a one-line inline table,
// "{ path = "../core", version = "1.2.0" }".
func cargoInlineTable(value string) map[string]string {
	fields := map[string]string{}
	inner, _, _ := strings.Cut(strings.TrimPrefix(value, "{"), "}")
	for _, part := range strings.Split(inner, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if s, ok := cargoStringValue(strings.TrimSpace(v)); ok {
			fields[unquoteKey(strings.TrimSpace(k))] = s
		} else if cargoTrue(v) {
			fields[unquoteKey(strings.TrimSpace(k))] = "true"
		}
	}
	return fields
}

func cargoTrue(value string) bool {
	value, _, _ = strings.Cut(value, "#")
	return strings.TrimSpace(value) == "true"
}

// cargoStringValue returns the content of the string value starts with.
func cargoStringValue(value string) (string, bool) {
	m := cargoString.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return "", false
	}
	if m[1] != "" {
		return m[1], true
	}
	return m[2], true
}

// unquoteKey drops the quotes around a quoted TOML key.
func unquoteKey(key string) string {
	if len(key) >= 2 && key[0] == '"' && key[len(key)-1] == '"' {
		return key[1 : len(key)-1]
	}
	return key
}

// cargoRequirement matches a Cargo version requirement that names one
// version to move: an optional operator and a full or partial version.
var cargoRequirement = regexp.MustCompile(`^(\^|~|=|>=)?(\s*)\d+(?:\.\d+){0,2}(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// moveCargoRequirement returns req with its version replaced by version,
// keeping its operator, and false when req names no single version to
// move, such as "*" or ">=1.0, <2".
func moveCargoRequirement(req, version string) (string, bool) {
	m := cargoRequirement.FindStringSubmatch(req)
	if m == nil {
		return req, false
	}
	return m[1] + m[2] + version, true
}

// cargoVersionField matches a "version = "..."" pair inside an inline
// table, capturing the text up to the value and the value itself.
var cargoVersionField = regexp.MustCompile(`(\bversion\s*=\s*)"([^"]*)"`)

// setCargoDependencyWhere adapts setCargoDependency to the manager.set
// signature: Cargo has no linksLocal, so eligible is always nil and every
// declaration moves.
func setCargoDependencyWhere(content, name, version string, _ func(string) bool) (string, error) {
	return setCargoDependency(content, name, version)
}

// setCargoDependency moves every declaration of the crate name to
// version: a plain requirement string, the version of an inline table, or
// the version line of a "[dependencies.<name>]" table. A declaration
// without a version, a path or workspace one, is left as it is.
func setCargoDependency(content, name, version string) (string, error) {
	lines := strings.Split(content, "\n")
	for _, d := range cargoDecls(lines) {
		if d.crate != name || d.version == "" {
			continue
		}
		moved, ok := moveCargoRequirement(d.version, version)
		if !ok {
			continue
		}
		line := lines[d.line]
		if d.table || !strings.Contains(line, "{") {
			m := cargoKeyValue.FindStringSubmatch(line)
			if m == nil {
				return "", fmt.Errorf("workspace: Cargo.toml: line %d: dependency %q: no version to move", d.line+1, name)
			}
			value := m[2]
			at := len(line) - len(value)
			quote := value[:1]
			end := strings.Index(value[1:], quote) + 1
			lines[d.line] = line[:at] + quote + moved + value[end:]
			continue
		}
		replaced := false
		lines[d.line] = cargoVersionField.ReplaceAllStringFunc(line, func(field string) string {
			if replaced {
				return field
			}
			replaced = true
			sub := cargoVersionField.FindStringSubmatch(field)
			return sub[1] + `"` + moved + `"`
		})
		if !replaced {
			return "", fmt.Errorf("workspace: Cargo.toml: line %d: dependency %q: version is not a basic string", d.line+1, name)
		}
	}
	return strings.Join(lines, "\n"), nil
}
