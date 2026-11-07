// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package workspace

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// mavenCoordinates is the groupId, artifactId and version of a pom.xml's
// project, its parent or one of its dependencies.
type mavenCoordinates struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
}

// parseMaven reads a pom.xml. A package is named "<groupId>:<artifactId>",
// the groupId inherited from the parent where the project sets none. Its
// dependencies are its parent, its dependencies and its
// dependencyManagement entries, including those in profiles, a "${project.groupId}" resolved to its
// own groupId.
func parseMaven(content []byte) (Package, error) {
	var pom struct {
		mavenCoordinates
		Parent       *mavenCoordinates  `xml:"parent"`
		Dependencies []mavenCoordinates `xml:"dependencies>dependency"`
		Managed      []mavenCoordinates `xml:"dependencyManagement>dependencies>dependency"`
		Profiles     []struct {
			Dependencies []mavenCoordinates `xml:"dependencies>dependency"`
			Managed      []mavenCoordinates `xml:"dependencyManagement>dependencies>dependency"`
		} `xml:"profiles>profile"`
	}
	if err := xml.Unmarshal(content, &pom); err != nil {
		return Package{}, fmt.Errorf("workspace: pom.xml: %w", err)
	}
	group := strings.TrimSpace(pom.GroupID)
	if group == "" && pom.Parent != nil {
		group = strings.TrimSpace(pom.Parent.GroupID)
	}
	artifact := strings.TrimSpace(pom.ArtifactID)
	if artifact == "" {
		return Package{}, errors.New("workspace: pom.xml: no artifactId")
	}
	pkg := Package{Name: group + ":" + artifact, Version: strings.TrimSpace(pom.Version)}
	add := func(c mavenCoordinates) {
		pkg.Dependencies = append(pkg.Dependencies, Dependency{
			Name: mavenName(c.GroupID, c.ArtifactID, group), Range: strings.TrimSpace(c.Version),
		})
	}
	if pom.Parent != nil {
		add(*pom.Parent)
	}
	for _, d := range pom.Dependencies {
		add(d)
	}
	for _, d := range pom.Managed {
		add(d)
	}
	for _, profile := range pom.Profiles {
		for _, d := range profile.Dependencies {
			add(d)
		}
		for _, d := range profile.Managed {
			add(d)
		}
	}
	return pkg, nil
}

// mavenName returns "<groupId>:<artifactId>", a groupId naming the
// project's own through a property resolved to ownGroup.
func mavenName(groupID, artifactID, ownGroup string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "${project.groupId}" || groupID == "${pom.groupId}" {
		groupID = ownGroup
	}
	return groupID + ":" + strings.TrimSpace(artifactID)
}

// mavenElement is an element a pom.xml rewrite tracks the coordinates of:
// the project's parent or a dependency, with where its version text sits.
type mavenElement struct {
	groupID, artifactID, version string
	start, end                   int64
	hasVersion                   bool
}

// setMavenDependency moves the version of every parent, dependency and
// dependencyManagement entry naming name, "<groupId>:<artifactId>", to
// version. A version given through a property, "${core.version}", or as
// a range, "[1.0,2.0)", is left as it is. Everything but the version text
// is kept byte for byte.
func setMavenDependency(content, name, version string) (string, error) {
	pkg, err := parseMaven([]byte(content))
	if err != nil {
		return "", err
	}
	ownGroup, _, _ := strings.Cut(pkg.Name, ":")

	dec := xml.NewDecoder(strings.NewReader(content))
	var stack []string
	var current *mavenElement
	var field string
	type edit struct{ start, end int64 }
	var edits []edit
	for {
		before := dec.InputOffset()
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("workspace: pom.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			switch {
			case current == nil && isMavenCoordinatesElement(stack):
				current = &mavenElement{}
			case current != nil && len(stack) == mavenDepth(stack)+1:
				field = t.Name.Local
			}
		case xml.CharData:
			if current == nil || field == "" {
				continue
			}
			text := string(t)
			switch field {
			case "groupId":
				current.groupID += text
			case "artifactId":
				current.artifactID += text
			case "version":
				current.version += text
				raw := content[before:dec.InputOffset()]
				lead := int64(len(raw) - len(strings.TrimLeft(raw, " \t\r\n")))
				trail := int64(len(raw) - len(strings.TrimRight(raw, " \t\r\n")))
				current.start, current.end, current.hasVersion = before+lead, dec.InputOffset()-trail, true
			}
		case xml.EndElement:
			switch {
			case current != nil && len(stack) == mavenDepth(stack):
				if current.hasVersion && mavenName(current.groupID, current.artifactID, ownGroup) == name &&
					mavenMovable(strings.TrimSpace(current.version)) {
					edits = append(edits, edit{start: current.start, end: current.end})
				}
				current = nil
			case current != nil:
				field = ""
			}
			stack = stack[:len(stack)-1]
		}
	}
	out := content
	slices.Reverse(edits)
	for _, e := range edits {
		out = out[:e.start] + version + out[e.end:]
	}
	return out, nil
}

// isMavenCoordinatesElement reports whether stack, the open elements from
// the root, ends in the project's parent or in a dependency of a
// dependencies list, the project's own, its dependencyManagement's or a
// profile's.
func isMavenCoordinatesElement(stack []string) bool {
	n := len(stack)
	switch {
	case n == 2 && stack[0] == "project" && stack[1] == "parent":
		return true
	case n >= 3 && stack[n-1] == "dependency" && stack[n-2] == "dependencies":
		return !slices.Contains(stack, "plugin")
	}
	return false
}

// mavenDepth returns the depth of the coordinates element open in stack:
// the outermost parent or dependency element.
func mavenDepth(stack []string) int {
	for i := range stack {
		if isMavenCoordinatesElement(stack[:i+1]) {
			return i + 1
		}
	}
	return len(stack)
}

// mavenMovable reports whether v is a plain version to move: neither a
// property reference nor a version range.
func mavenMovable(v string) bool {
	return v != "" && !strings.ContainsAny(v, "$[](), \t")
}
