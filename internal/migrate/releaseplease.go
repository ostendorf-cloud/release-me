// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package migrate translates another release tool's configuration into
// release-me.yaml. It reads and returns bytes only: the command layer reads
// the files and decides where the result goes.
package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	stdpath "path"
	"slices"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"gopkg.in/yaml.v3"
)

// ReadFile reads a file relative to the repository root: the package.json
// a node package's name falls back to, and the version file a release type
// bumps. It returns an error wrapping fs.ErrNotExist for a file that is not
// there.
type ReadFile func(path string) ([]byte, error)

// Result is a release-please configuration translated for release-me.
type Result struct {
	// YAML is the release-me.yaml content.
	YAML []byte
	// Warnings names every release-please setting the translation could
	// not carry over, one line each, in a stable order.
	Warnings []string
	// Tags is the tag each package's manifest version was released under,
	// in release-me.yaml's component order, for the caller to look up:
	// release-me continues from such a tag and starts over at the initial
	// version without one.
	Tags []ExpectedTag
}

// ExpectedTag is the tag one package's last release carries, according to
// .release-please-manifest.json.
type ExpectedTag struct {
	Component string
	Tag       string
}

// rpPackage is one release-please package with the top-level defaults
// already applied: the settings this translation reads, plus every other
// key it was given, so each can be reported.
type rpPackage struct {
	path     string
	settings map[string]json.RawMessage
}

// packageKeys are the release-please settings that may appear on a package
// and as top-level defaults, and that this translation carries over.
var packageKeys = []string{
	"release-type", "package-name", "component", "include-component-in-tag", "include-v-in-tag",
	"tag-separator", "changelog-path", "bump-minor-pre-major", "bump-patch-for-minor-pre-major",
	"initial-version", "extra-files",
}

// topLevelKeys are the release-please settings that only exist at the top
// level and that this translation reads or deliberately leaves out.
var topLevelKeys = []string{
	"packages", "plugins", "label", "release-label", "separate-pull-requests", "$schema",
	"bootstrap-sha", "last-release-sha",
}

// FromReleasePlease translates release-please-config.json and
// .release-please-manifest.json into release-me.yaml. Every package becomes
// a component tagged and changelogged where release-please put them, so
// release-me continues from the existing tags and changelogs. A package at
// the root with release-please's plain "vX.Y.Z" tags becomes the implicit
// main component instead.
func FromReleasePlease(configJSON, manifestJSON []byte, readFile ReadFile) (Result, error) {
	top, err := orderedObject(configJSON)
	if err != nil {
		return Result{}, fmt.Errorf("migrate: release-please-config.json: %w", err)
	}
	var manifest map[string]string
	if len(manifestJSON) > 0 {
		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
			return Result{}, fmt.Errorf("migrate: .release-please-manifest.json: %w", err)
		}
	}

	var warnings []string
	warn := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	defaults := map[string]json.RawMessage{}
	topValues := map[string]json.RawMessage{}
	for _, kv := range top {
		switch {
		case slices.Contains(packageKeys, kv.key):
			defaults[kv.key] = kv.value
		case slices.Contains(topLevelKeys, kv.key):
			topValues[kv.key] = kv.value
		default:
			warn("%s: no release-me equivalent, left out", kv.key)
		}
	}
	if _, ok := topValues["bootstrap-sha"]; ok {
		warn("bootstrap-sha: not needed, release-me reads the history since each component's last tag")
	}
	if _, ok := topValues["last-release-sha"]; ok {
		warn("last-release-sha: not needed, release-me reads the history since each component's last tag")
	}

	rawPackages, ok := topValues["packages"]
	if !ok {
		return Result{}, errors.New("migrate: release-please-config.json: no packages")
	}
	entries, err := orderedObject(rawPackages)
	if err != nil {
		return Result{}, fmt.Errorf("migrate: release-please-config.json: packages: %w", err)
	}
	if len(entries) == 0 {
		return Result{}, errors.New("migrate: release-please-config.json: no packages")
	}

	packages := make([]rpPackage, 0, len(entries))
	for _, entry := range entries {
		own, err := orderedObject(entry.value)
		if err != nil {
			return Result{}, fmt.Errorf("migrate: release-please-config.json: package %s: %w", entry.key, err)
		}
		settings := make(map[string]json.RawMessage, len(defaults)+len(own))
		for k, v := range defaults {
			settings[k] = v
		}
		for _, kv := range own {
			if !slices.Contains(packageKeys, kv.key) {
				warn("package %s: %s: no release-me equivalent, left out", entry.key, kv.key)
				continue
			}
			settings[kv.key] = kv.value
		}
		packages = append(packages, rpPackage{path: cleanPackagePath(entry.key), settings: settings})
	}

	components := make([]component, 0, len(packages))
	for _, p := range packages {
		c, err := translatePackage(p, readFile, warn)
		if err != nil {
			return Result{}, err
		}
		if i := slices.IndexFunc(components, func(o component) bool { return o.name == c.name }); i >= 0 {
			return Result{}, fmt.Errorf("migrate: package %s and package %s both become component %q, "+
				"give one of them a \"component\" of its own", components[i].path, c.path, c.name)
		}
		components = append(components, c)
	}
	// The manifest keys are cleaned once, so "./web", "web/" and "web" all
	// find the package at "web", for the warning and the expected tag alike.
	versions := make(map[string]string, len(manifest))
	manifestPaths := make([]string, 0, len(manifest))
	for path, v := range manifest {
		versions[cleanPackagePath(path)] = v
		manifestPaths = append(manifestPaths, path)
	}
	slices.Sort(manifestPaths)
	for _, path := range manifestPaths {
		if !slices.ContainsFunc(packages, func(p rpPackage) bool { return p.path == cleanPackagePath(path) }) {
			warn(".release-please-manifest.json: %s: no package configured at this path, left out", path)
		}
	}

	if len(packages) > 1 && slices.ContainsFunc(packages, func(p rpPackage) bool { return p.path == "." }) {
		warn("package .: release-me components cannot overlap, so a root package beside other packages " +
			"needs a path of its own, the directory holding its source, before release-me.yaml loads")
	}

	versioning, err := translatePlugins(topValues["plugins"], components, warn)
	if err != nil {
		return Result{}, err
	}
	// release-please's default changelog links every entry's commit and
	// the issues it closes; release-me's does neither unless asked to.
	st := style{commitLinks: true, issueReferences: true}
	// release-please groups every package into one release pull request
	// unless separate-pull-requests is true; release-me's own default is one
	// per component, so the grouped default is written out. locked always
	// shares one already.
	sharedPR := false
	if len(components) > 1 && versioning != "locked" {
		separate := false
		if raw, ok := topValues["separate-pull-requests"]; ok {
			if err := json.Unmarshal(raw, &separate); err != nil {
				return Result{}, fmt.Errorf("migrate: separate-pull-requests: %w", err)
			}
		}
		sharedPR = !separate
	}

	pending, err := label(topValues["label"], "autorelease: pending", "label", warn)
	if err != nil {
		return Result{}, err
	}
	tagged, err := label(topValues["release-label"], "autorelease: tagged", "release-label", warn)
	if err != nil {
		return Result{}, err
	}

	var tags []ExpectedTag
	for i, p := range packages {
		if v, ok := versions[p.path]; ok {
			tags = append(tags, ExpectedTag{Component: components[i].name, Tag: components[i].tagPrefix + v})
		}
	}

	out, err := render(components, versioning, st, sharedPR, pending, tagged)
	if err != nil {
		return Result{}, err
	}
	return Result{YAML: out, Warnings: warnings, Tags: tags}, nil
}

// style is the top-level changelog style keys the translation writes,
// carried over from release-please's settings.
type style struct {
	commitLinks     bool
	issueReferences bool
}

// component is one translated release-please package.
type component struct {
	name string
	// rpName is the name release-please knows the package by, which a
	// linked-versions plugin lists: name itself unless sanitizeName had to
	// change it.
	rpName            string
	path              string
	tagPrefix         string
	changelog         string
	bumpMinorPreMajor bool
	initialVersion    string
	files             []file
}

// file is one release-me "files" entry.
type file struct {
	path    string
	updater string
	key     string
}

// translatePackage carries one release-please package over: its name, tag
// prefix and changelog the way release-please derived them, and the files
// its release type and extra-files bump.
func translatePackage(p rpPackage, readFile ReadFile, warn func(string, ...any)) (component, error) {
	var (
		releaseType, packageName, componentName, separator, changelogPath, initialVersion string
		includeComponent, includeV                                                        = true, true
		bumpMinor, bumpPatchForMinor                                                      bool
	)
	separator = "-"
	changelogPath = "CHANGELOG.md"
	for key, target := range map[string]any{
		"release-type": &releaseType, "package-name": &packageName, "component": &componentName,
		"tag-separator": &separator, "changelog-path": &changelogPath, "initial-version": &initialVersion,
		"include-component-in-tag": &includeComponent, "include-v-in-tag": &includeV,
		"bump-minor-pre-major": &bumpMinor, "bump-patch-for-minor-pre-major": &bumpPatchForMinor,
	} {
		raw, ok := p.settings[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return component{}, fmt.Errorf("migrate: package %s: %s: %w", p.path, key, err)
		}
	}
	if releaseType == "" {
		releaseType = "node"
	}
	if bumpPatchForMinor {
		warn("package %s: bump-patch-for-minor-pre-major: release-me bumps a feat to a minor below 1.0.0, "+
			"left out", p.path)
	}

	// release-please names a package after its component, else its package
	// name, a node package's scope stripped, else the name package.json
	// carries.
	if componentName == "" {
		if packageName == "" && releaseType == "node" {
			packageName = packageJSONName(readFile, stdpath.Join(p.path, "package.json"))
		}
		componentName = packageName
		if releaseType == "node" {
			if scope, rest, ok := strings.Cut(componentName, "/"); ok && strings.HasPrefix(scope, "@") {
				componentName = rest
			}
		}
	}

	prefix := ""
	if includeComponent && componentName != "" {
		prefix = componentName + separator
	}
	if includeV {
		prefix += "v"
	}
	if prefix == "" {
		return component{}, fmt.Errorf("migrate: package %s: its tags carry neither a component nor a v, "+
			"a bare version release-me cannot tell apart from another component's", p.path)
	}

	name := componentName
	if name == "" {
		name = stdpath.Base(p.path)
		if p.path == "." {
			name = "main"
		}
	}

	rpName := name
	name = sanitizeName(name)
	if err := config.ValidateComponentName(name); err != nil {
		return component{}, fmt.Errorf("migrate: package %s: component name %q %w", p.path, rpName, err)
	}
	if name != rpName {
		warn("package %s: renamed component %q to %q, release-me allows only letters, digits, \"-\", \"_\" and \".\" "+
			"in a component name; its tags keep their prefix %q", p.path, rpName, name, prefix)
	}

	c := component{
		name:              name,
		rpName:            rpName,
		path:              p.path,
		tagPrefix:         prefix,
		changelog:         repoPath(p.path, changelogPath),
		bumpMinorPreMajor: bumpMinor,
		initialVersion:    initialVersion,
	}

	// A version file the release type implies is carried over only where it
	// exists: release-type falls back to node, and an entry for a file that
	// is not there would fail the first release pull request.
	versionFile := func(name, updater, key string) {
		path := stdpath.Join(p.path, name)
		if _, err := readFile(path); err != nil {
			warn("package %s: release-type %s: %s does not exist, left out of files", p.path, releaseType, path)
			return
		}
		c.files = append(c.files, file{path: path, updater: updater, key: key})
	}
	switch releaseType {
	case "node":
		versionFile("package.json", "json", "version")
		if _, err := readFile(stdpath.Join(p.path, "package-lock.json")); err == nil {
			warn("package %s: package-lock.json: release-please bumped it, release-me does not", p.path)
		}
	case "helm":
		versionFile("Chart.yaml", "helm", "")
	case "go":
	default:
		warn("package %s: release-type %s: the files it bumps are not carried over, add them under files", p.path, releaseType)
	}

	if raw, ok := p.settings["extra-files"]; ok {
		files, err := translateExtraFiles(p.path, raw, warn)
		if err != nil {
			return component{}, err
		}
		c.files = append(c.files, files...)
	}
	return c, nil
}

// translateExtraFiles carries over the extra-files entries a typed updater
// can bump: json, yaml and toml files whose jsonpath is a plain dotted path.
func translateExtraFiles(pkgPath string, raw json.RawMessage, warn func(string, ...any)) ([]file, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("migrate: package %s: extra-files: %w", pkgPath, err)
	}
	var files []file
	for _, e := range entries {
		var plain string
		if json.Unmarshal(e, &plain) == nil {
			warn("package %s: extra-files: %s: release-please's version markers are not release-me's, "+
				"mark the line with \"release-me: version\" and add it under files", pkgPath, plain)
			continue
		}
		var typed struct {
			Type     string `json:"type"`
			Path     string `json:"path"`
			JSONPath string `json:"jsonpath"`
		}
		if err := json.Unmarshal(e, &typed); err != nil {
			return nil, fmt.Errorf("migrate: package %s: extra-files: %w", pkgPath, err)
		}
		key, ok := strings.CutPrefix(typed.JSONPath, "$.")
		if !slices.Contains([]string{"json", "yaml", "toml"}, typed.Type) || !ok || strings.ContainsAny(key, "[]*") {
			warn("package %s: extra-files: %s: type %q at %q has no typed updater, add it under files",
				pkgPath, typed.Path, typed.Type, typed.JSONPath)
			continue
		}
		files = append(files, file{path: repoPath(pkgPath, typed.Path), updater: typed.Type, key: key})
	}
	return files, nil
}

// translatePlugins maps release-please's linked-versions plugin to a
// release-me versioning mode. It releases every component of its group at
// the group's highest version, changed or not, which is release-me's
// locked mode when the group is every component.
func translatePlugins(raw json.RawMessage, components []component, warn func(string, ...any)) (string, error) {
	if raw == nil {
		return "", nil
	}
	var plugins []json.RawMessage
	if err := json.Unmarshal(raw, &plugins); err != nil {
		return "", fmt.Errorf("migrate: plugins: %w", err)
	}
	versioning := ""
	for _, p := range plugins {
		var name string
		if json.Unmarshal(p, &name) == nil {
			warn("plugins: %s: no release-me equivalent, left out", name)
			continue
		}
		var plugin struct {
			Type       string   `json:"type"`
			Components []string `json:"components"`
		}
		if err := json.Unmarshal(p, &plugin); err != nil {
			return "", fmt.Errorf("migrate: plugins: %w", err)
		}
		if plugin.Type != "linked-versions" {
			warn("plugins: %s: no release-me equivalent, left out", plugin.Type)
			continue
		}
		all := len(plugin.Components) == len(components) && slices.IndexFunc(components, func(c component) bool {
			return !slices.Contains(plugin.Components, c.rpName)
		}) == -1
		if !all || versioning != "" {
			warn("plugins: linked-versions: release-me links every component or none, left out")
			continue
		}
		versioning = "locked"
	}
	return versioning, nil
}

// label reads a release-please label key, a comma separated list. release-me
// carries one label per role, so the first one is kept.
func label(raw json.RawMessage, fallback, key string, warn func(string, ...any)) (string, error) {
	if raw == nil {
		return fallback, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("migrate: %s: %w", key, err)
	}
	labels := strings.Split(value, ",")
	if len(labels) > 1 {
		warn("%s: release-me carries one label, kept %q", key, strings.TrimSpace(labels[0]))
	}
	return strings.TrimSpace(labels[0]), nil
}

// sanitizeName turns a release-please component name into one
// config.ValidateComponentName accepts: every character it refuses becomes
// "-", a leading "." or "-" and a trailing "." or ".lock" are dropped and
// ".." collapses to ".". A name
// already valid comes back unchanged.
func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := b.String()
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", ".")
	}
	out = strings.TrimLeft(out, ".-")
	for strings.HasSuffix(out, ".lock") || strings.HasSuffix(out, ".") {
		out = strings.TrimSuffix(strings.TrimSuffix(out, ".lock"), ".")
	}
	return out
}

// packageJSONName reads the name field of a package.json, "" when the file
// or the field is missing.
func packageJSONName(readFile ReadFile, path string) string {
	data, err := readFile(path)
	if err != nil {
		return ""
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	return pkg.Name
}

// cleanPackagePath turns a release-please package path, "." or a path
// that may carry a trailing slash, into release-me's form.
func cleanPackagePath(p string) string {
	return stdpath.Clean(strings.TrimPrefix(p, "./"))
}

// repoPath resolves a file release-please names relative to a package:
// relative to the package path, or to the repository root with a leading
// slash, the way release-please's own addPath reads it.
func repoPath(pkgPath, file string) string {
	if strings.HasPrefix(file, "/") {
		return stdpath.Clean(strings.TrimLeft(file, "/"))
	}
	return stdpath.Join(pkgPath, file)
}

// render writes release-me.yaml. A single root package tagged "vX.Y.Z" is
// the implicit main component and needs no components key; everything else
// is a components mapping in release-please's package order, carrying only
// the keys whose value differs from what release-me would derive.
func render(components []component, versioning string, st style, sharedPR bool, pending, tagged string) ([]byte, error) {
	doc := &yaml.Node{Kind: yaml.MappingNode}
	add := func(m *yaml.Node, key string, value *yaml.Node) {
		m.Content = append(m.Content, scalar(key), value)
	}

	add(doc, "changelog-source", scalar("all"))
	if st.commitLinks {
		add(doc, "changelog-commit-links", boolean(true))
	}
	if st.issueReferences {
		add(doc, "changelog-issue-references", boolean(true))
	}
	if versioning != "" {
		add(doc, "versioning", scalar(versioning))
	}
	if sharedPR {
		add(doc, "release-pull-request", scalar("shared"))
	}
	labels := &yaml.Node{Kind: yaml.MappingNode}
	add(labels, "pending", scalar(pending))
	add(labels, "tagged", scalar(tagged))
	for _, t := range []string{"alpha", "beta", "rc"} {
		add(labels, t, boolean(false))
	}
	add(doc, "labels", labels)

	// bump-minor-pre-major carried by every component is written once, at
	// the top level, instead of repeated per component.
	shared := slices.IndexFunc(components, func(c component) bool { return !c.bumpMinorPreMajor }) == -1
	if shared {
		add(doc, "bump-minor-pre-major", boolean(true))
	}

	if len(components) == 1 && components[0].path == "." && components[0].tagPrefix == "v" {
		c := components[0]
		if c.changelog != "CHANGELOG.md" {
			add(doc, "changelog", scalar(c.changelog))
		}
		addSettings(doc, c, shared, add)
	} else {
		comps := &yaml.Node{Kind: yaml.MappingNode}
		for _, c := range components {
			m := &yaml.Node{Kind: yaml.MappingNode}
			add(m, "path", scalar(c.path))
			if c.tagPrefix != c.name+"/v" {
				add(m, "tag-prefix", scalar(c.tagPrefix))
			}
			if c.changelog != stdpath.Join(c.path, "CHANGELOG.md") {
				add(m, "changelog", scalar(c.changelog))
			}
			addSettings(m, c, shared, add)
			add(comps, c.name, m)
		}
		add(doc, "components", comps)
	}

	var buf bytes.Buffer
	buf.WriteString("# Generated by release-me migrate release-please.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("migrate: render release-me.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("migrate: render release-me.yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// addSettings writes the settings release-me reads per component or at the
// top level alike: initial-version, bump-minor-pre-major and files.
// bump-minor-pre-major is left out where the top level already carries it.
func addSettings(m *yaml.Node, c component, shared bool, add func(*yaml.Node, string, *yaml.Node)) {
	if c.initialVersion != "" {
		add(m, "initial-version", scalar(c.initialVersion))
	}
	if c.bumpMinorPreMajor && !shared {
		add(m, "bump-minor-pre-major", boolean(true))
	}
	if len(c.files) > 0 {
		list := &yaml.Node{Kind: yaml.SequenceNode}
		for _, f := range c.files {
			entry := &yaml.Node{Kind: yaml.MappingNode}
			add(entry, "path", scalar(f.path))
			add(entry, "updater", scalar(f.updater))
			if f.key != "" {
				add(entry, "key", scalar(f.key))
			}
			list.Content = append(list.Content, entry)
		}
		add(m, "files", list)
	}
}

// scalar is a plain string node; yaml.v3 quotes it where YAML needs that,
// such as a value holding ": ".
func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// boolean is a YAML boolean node.
func boolean(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(value)}
}

// keyValue is one entry of a JSON object, in the order the file lists it.
type keyValue struct {
	key   string
	value json.RawMessage
}

// orderedObject decodes a JSON object into its entries in file order: a Go
// map would lose the package order release-me.yaml's components keep.
func orderedObject(data []byte) ([]keyValue, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("is not a JSON object")
	}
	var out []keyValue
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		out = append(out, keyValue{key: key, value: value})
	}
	return out, nil
}
