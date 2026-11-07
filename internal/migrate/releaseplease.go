// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package migrate translates another release tool's configuration into
// release-me.yaml. It reads and returns bytes only: the command layer reads
// the files and decides where the result goes.
package migrate

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	stdpath "path"
	"regexp"
	"slices"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
	"github.com/ostendorf-cloud/release-me/internal/updater"
	"github.com/ostendorf-cloud/release-me/internal/workspace"
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
	"initial-version", "extra-files", "version-file",
}

// topLevelKeys are the release-please settings that only exist at the top
// level and that this translation reads or deliberately leaves out.
var topLevelKeys = []string{
	"packages", "plugins", "label", "release-label", "extra-label", "separate-pull-requests", "$schema",
	"bootstrap-sha", "last-release-sha", "pull-request-title-pattern", "group-pull-request-title-pattern",
}

// unsupportedReasons explains a release-please key the translation leaves
// out where "no release-me equivalent" alone would mislead.
var unsupportedReasons = map[string]string{
	"date-format": "release-please formats the date its generic updater writes with it, not the " +
		"changelog's, which release-me's changelog-date-format sets, left out",
}

// unsupported is the warning text for a release-please key the
// translation leaves out.
func unsupported(key string) string {
	if reason, ok := unsupportedReasons[key]; ok {
		return reason
	}
	return "no release-me equivalent, left out"
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
			warn("%s: %s", kv.key, unsupported(kv.key))
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
				warn("package %s: %s: %s", entry.key, kv.key, unsupported(kv.key))
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
		warn("package .: release-please counted every commit for the root package, release-me's root " +
			"component counts only the files outside the other components' paths")
	}

	versioning, st, workspaces, err := translatePlugins(topValues["plugins"], components, warn)
	if err != nil {
		return Result{}, err
	}
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
		if separate && len(workspaces) > 0 {
			// release-me releases a dependent together with its
			// dependency, which only one shared release pull request does.
			warn("separate-pull-requests: release-me releases a workspace dependent together with its dependency, " +
				"so release-pull-request is shared")
			sharedPR = true
		}
	}
	if len(components) == 1 && len(workspaces) > 0 {
		warn("workspaces: %s: a single package has no workspace dependency to move, left out", strings.Join(workspaces, ", "))
		workspaces = nil
	}

	pending, err := labels(topValues["label"], "autorelease: pending", "label")
	if err != nil {
		return Result{}, err
	}
	tagged, err := labels(topValues["release-label"], "autorelease: tagged", "release-label")
	if err != nil {
		return Result{}, err
	}
	extra, err := labels(topValues["extra-label"], "", "extra-label")
	if err != nil {
		return Result{}, err
	}

	var tags []ExpectedTag
	for i, p := range packages {
		if v, ok := versions[p.path]; ok {
			tags = append(tags, ExpectedTag{Component: components[i].name, Tag: components[i].tagPrefix + v})
		}
	}

	opts := topLevel{
		versioning: versioning, sharedPR: sharedPR, pending: pending, tagged: tagged, extra: extra, style: st,
		workspaces: workspaces,
	}
	if opts.title, err = titlePattern(topValues["pull-request-title-pattern"], "pull-request-title-pattern",
		releasepr.TitlePerComponent, warn); err != nil {
		return Result{}, err
	}
	if raw, ok := topValues["group-pull-request-title-pattern"]; ok {
		if !sharedPR && versioning != "locked" {
			warn("group-pull-request-title-pattern: release-me opens one release pull request per component here, left out")
		} else {
			kind := releasepr.TitleShared
			if versioning == "locked" {
				kind = releasepr.TitleLocked
			}
			if opts.sharedTitle, err = titlePattern(raw, "group-pull-request-title-pattern", kind, warn); err != nil {
				return Result{}, err
			}
		}
	}

	out, err := render(components, opts)
	if err != nil {
		return Result{}, err
	}
	return Result{YAML: out, Warnings: warnings, Tags: tags}, nil
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
	// bumpPatchForMinorPreMajor is release-please's key of the same name,
	// release-me's too.
	bumpPatchForMinorPreMajor bool
	initialVersion            string
	files                     []file
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
		versionFileName                                                                   string
		includeComponent, includeV                                                        = true, true
		bumpMinor, bumpPatchForMinor                                                      bool
	)
	separator = "-"
	changelogPath = "CHANGELOG.md"
	for key, target := range map[string]any{
		"release-type": &releaseType, "package-name": &packageName, "component": &componentName,
		"tag-separator": &separator, "changelog-path": &changelogPath, "initial-version": &initialVersion,
		"version-file":             &versionFileName,
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
		name:                      name,
		rpName:                    rpName,
		path:                      p.path,
		tagPrefix:                 prefix,
		changelog:                 repoPath(p.path, changelogPath),
		bumpMinorPreMajor:         bumpMinor,
		bumpPatchForMinorPreMajor: bumpPatchForMinor,
		initialVersion:            initialVersion,
	}

	// A version file the release type implies is carried over only where it
	// exists: release-type falls back to node, and an entry for a file that
	// is not there would fail the first release pull request. A lockfile is
	// optional: release-please bumps it only where it exists and holds the
	// package, so one that does not is left out without a warning.
	versionFile := func(name, updaterName, key string, optional bool) {
		path := stdpath.Join(p.path, name)
		data, err := readFile(path)
		if err != nil {
			if !optional {
				warn("package %s: release-type %s: %s does not exist, left out of files", p.path, releaseType, path)
			}
			return
		}
		if optional {
			if _, err := updater.Updaters[updaterName].Update(string(data), key, "0.0.0"); err != nil {
				return
			}
		}
		c.files = append(c.files, file{path: path, updater: updaterName, key: key})
	}
	switch releaseType {
	case "node":
		versionFile("package.json", updater.JSON, "version", false)
		versionFile("package-lock.json", updater.NpmLock, "", true)
		versionFile("npm-shrinkwrap.json", updater.NpmLock, "", true)
	case "rust":
		manifest := stdpath.Join(p.path, "Cargo.toml")
		crate := cargoPackageName(readFile, manifest)
		if crate == "" {
			warn("package %s: release-type rust: %s declares no [package] name and version, "+
				"add the crates it releases under files", p.path, manifest)
			break
		}
		versionFile("Cargo.toml", updater.TOML, "package.version", false)
		versionFile("Cargo.lock", updater.CargoLock, crate, true)
	case "ruby":
		warn("package %s: release-type ruby: its version.rb is not carried over, add it under files", p.path)
		if componentName != "" {
			versionFile("Gemfile.lock", updater.GemfileLock, componentName, true)
		} else {
			path := stdpath.Join(p.path, "Gemfile.lock")
			if _, err := readFile(path); err == nil {
				warn("package %s: release-type ruby: %s is not carried over without component or package-name, "+
					"add it under files with a gem name as key", p.path, path)
			}
		}
	case "maven":
		translatePom(&c, p.path, readFile, warn)
	case "simple":
		if versionFileName == "" {
			versionFileName = "version.txt"
		}
		versionFile(versionFileName, updater.Plain, "", false)
	case "helm":
		versionFile("Chart.yaml", updater.Helm, "", false)
	case "go":
	default:
		warn("package %s: release-type %s: the files it bumps are not carried over, add them under files", p.path, releaseType)
	}

	if raw, ok := p.settings["extra-files"]; ok {
		files, err := translateExtraFiles(p.path, raw, readFile, warn)
		if err != nil {
			return component{}, err
		}
		c.files = append(c.files, files...)
	}
	return c, nil
}

// translatePom carries a maven package's pom.xml over the way
// release-please's pom.xml updater bumps it: the project's own version, or
// its parent's where the project inherits its version. release-please bumps
// every pom.xml below the package; release-me lists its files, so a
// multi-module project is warned about, each module's pom.xml to be added
// by hand.
func translatePom(c *component, pkgPath string, readFile ReadFile, warn func(string, ...any)) {
	path := stdpath.Join(pkgPath, "pom.xml")
	data, err := readFile(path)
	if err != nil {
		warn("package %s: release-type maven: %s does not exist, left out of files", pkgPath, path)
		return
	}
	key := ""
	for _, k := range []string{"project.version", "project.parent.version"} {
		if _, err := updater.UpdateXML(string(data), k, "0.0.0"); err == nil {
			key = k
			break
		}
	}
	if key == "" {
		warn("package %s: release-type maven: %s has no project or parent version, left out of files", pkgPath, path)
		return
	}
	c.files = append(c.files, file{path: path, updater: updater.XML, key: key})
	dec := xml.NewDecoder(bytes.NewReader(data))
	// As in UpdateXML, only ASCII element names matter here.
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "modules" {
			warn("package %s: release-type maven: release-please bumped every pom.xml below the package, "+
				"only %s is carried over, add each module's pom.xml under files", pkgPath, path)
			break
		}
	}
}

// translateExtraFiles carries over the extra-files entries an updater can
// bump: a plain path and a generic entry through release-please's version
// markers, json, yaml and toml files whose jsonpath is a plain dotted path,
// xml files whose xpath is a plain absolute path, and pom files.
func translateExtraFiles(pkgPath string, raw json.RawMessage, readFile ReadFile, warn func(string, ...any)) ([]file, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("migrate: package %s: extra-files: %w", pkgPath, err)
	}
	var files []file
	for _, e := range entries {
		var plain string
		if json.Unmarshal(e, &plain) == nil {
			files = append(files, markedExtraFile(pkgPath, plain, true, readFile, warn)...)
			continue
		}
		var typed struct {
			Type     string `json:"type"`
			Path     string `json:"path"`
			JSONPath string `json:"jsonpath"`
			XPath    string `json:"xpath"`
			Glob     bool   `json:"glob"`
		}
		if err := json.Unmarshal(e, &typed); err != nil {
			return nil, fmt.Errorf("migrate: package %s: extra-files: %w", pkgPath, err)
		}
		if typed.Glob {
			warn("package %s: extra-files: %s: a glob, release-me lists its files, add each file it matches under files",
				pkgPath, typed.Path)
			continue
		}
		if typed.Type == "generic" {
			files = append(files, markedExtraFile(pkgPath, typed.Path, false, readFile, warn)...)
			continue
		}
		path := repoPath(pkgPath, typed.Path)
		var (
			key string
			ok  bool
		)
		name := typed.Type
		switch typed.Type {
		case "json", "yaml", "toml":
			key, ok = strings.CutPrefix(typed.JSONPath, "$.")
			ok = ok && !strings.ContainsAny(key, "[]*")
		case "xml":
			key, ok = xpathKey(typed.XPath)
		case "pom":
			// release-please's pom type bumps the project's own version.
			name, key, ok = updater.XML, "project.version", true
		}
		if !ok {
			at := typed.JSONPath
			if typed.XPath != "" {
				at = typed.XPath
			}
			warn("package %s: extra-files: %s: type %q at %q has no typed updater, add it under files",
				pkgPath, typed.Path, typed.Type, at)
			continue
		}
		files = append(files, file{path: path, updater: name, key: key})
	}
	return files, nil
}

// markedExtraFile carries over an extra-files entry release-please bumps
// through its version markers: a generic entry, or a plain path, which for
// a .json, .yaml, .yml or .toml file release-please also bumps at its
// top-level "version" key. Each becomes an entry only where the file exists
// and the updater finds what to bump in it, since an entry that does not
// would fail the first release pull request.
func markedExtraFile(pkgPath, rel string, plain bool, readFile ReadFile, warn func(string, ...any)) []file {
	path := repoPath(pkgPath, rel)
	data, err := readFile(path)
	if err != nil {
		warn("package %s: extra-files: %s does not exist, left out of files", pkgPath, path)
		return nil
	}
	content := string(data)

	var out []file
	if plain {
		typed := map[string]string{".json": updater.JSON, ".yaml": updater.YAML, ".yml": updater.YAML, ".toml": updater.TOML}
		if name, ok := typed[stdpath.Ext(path)]; ok {
			if _, err := updater.Updaters[name].Update(content, "version", "0.0.0"); err == nil {
				out = append(out, file{path: path, updater: name, key: "version"})
			}
		}
	}
	_, err = updater.UpdateGeneric(content, "0.0.0")
	switch {
	case err == nil:
		out = append(out, file{path: path, updater: updater.Generic})
	case len(out) == 0 || strings.Contains(content, "x-release-please-"):
		warn("package %s: extra-files: %s: %v, left out of files", pkgPath, path, err)
	}
	return out
}

// xpathStep matches one step of an absolute XPath the xml updater can
// follow: an element name, with or without a namespace prefix, or the
// namespace-blind *[local-name()="name"] form release-please's own pom.xml
// updater uses.
var xpathStep = regexp.MustCompile(`^(?:(?:[A-Za-z_][\w-]*:)?([A-Za-z_][\w-]*)|\*\[local-name\(\)\s*=\s*(?:"([A-Za-z_][\w-]*)"|'([A-Za-z_][\w-]*)')\])$`)

// xpathKey turns an absolute XPath of plain element steps, such as
// "/project/version", into the xml updater's dotted key, "project.version".
// Any other XPath, one searching the whole document with "//", or one with
// a predicate or an attribute, reports false.
func xpathKey(xpath string) (string, bool) {
	rest, ok := strings.CutPrefix(xpath, "/")
	if !ok || rest == "" {
		return "", false
	}
	steps := strings.Split(rest, "/")
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		m := xpathStep.FindStringSubmatch(step)
		if m == nil {
			return "", false
		}
		names = append(names, m[1]+m[2]+m[3])
	}
	return strings.Join(names, "."), true
}

// style is the top-level changelog style keys the translation writes,
// carried over from release-please's plugins and settings.
type style struct {
	sentenceCase bool
}

// workspacePlugins maps each release-please workspace plugin to the
// package manager release-me's "workspaces" names for it.
var workspacePlugins = map[string]string{
	"node-workspace":  workspace.Node,
	"cargo-workspace": workspace.Cargo,
}

// workspacePluginOptions are the keys a workspace plugin object carries
// that need no translation: its type, and the options release-me already
// behaves as.
var workspacePluginOptions = []string{"type"}

// translatePlugins maps release-please's plugins onto release-me. The
// linked-versions plugin releases every component of its group at the
// group's highest version, changed or not, which is release-me's locked
// mode when the group is every component. The sentence-case plugin is
// changelog-sentence-case, and its workspace plugins become "workspaces".
func translatePlugins(raw json.RawMessage, components []component, warn func(string, ...any)) (string, style, []string, error) {
	var st style
	if raw == nil {
		return "", st, nil, nil
	}
	var plugins []json.RawMessage
	if err := json.Unmarshal(raw, &plugins); err != nil {
		return "", st, nil, fmt.Errorf("migrate: plugins: %w", err)
	}
	versioning := ""
	var workspaces []string
	addWorkspace := func(plugin string, options map[string]json.RawMessage) {
		keys := make([]string, 0, len(options))
		for key := range options {
			if !slices.Contains(workspacePluginOptions, key) {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range keys {
			warn("plugins: %s: %s: no release-me equivalent, left out", plugin, key)
		}
		if manager := workspacePlugins[plugin]; !slices.Contains(workspaces, manager) {
			workspaces = append(workspaces, manager)
		}
	}
	for _, p := range plugins {
		var name string
		if json.Unmarshal(p, &name) == nil {
			if name == "sentence-case" {
				st.sentenceCase = true
				continue
			}
			if _, ok := workspacePlugins[name]; ok {
				addWorkspace(name, nil)
				continue
			}
			warn("plugins: %s: no release-me equivalent, left out", name)
			continue
		}
		var plugin struct {
			Type         string   `json:"type"`
			Components   []string `json:"components"`
			SpecialWords []string `json:"specialWords"`
		}
		if err := json.Unmarshal(p, &plugin); err != nil {
			return "", st, nil, fmt.Errorf("migrate: plugins: %w", err)
		}
		if plugin.Type == "sentence-case" {
			st.sentenceCase = true
			if len(plugin.SpecialWords) > 0 {
				warn("plugins: sentence-case: specialWords: release-me keeps gRPC, npm and any first word "+
					"already holding an upper case letter as written, %s left out", strings.Join(plugin.SpecialWords, ", "))
			}
			continue
		}
		if _, ok := workspacePlugins[plugin.Type]; ok {
			var options map[string]json.RawMessage
			if err := json.Unmarshal(p, &options); err != nil {
				return "", st, nil, fmt.Errorf("migrate: plugins: %w", err)
			}
			addWorkspace(plugin.Type, options)
			continue
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
	return versioning, st, workspaces, nil
}

// labels reads every name in a release-please comma separated label list.
// A missing key uses fallback; an explicitly empty list is refused when
// fallback names a required release label, and allowed for optional extras.
func labels(raw json.RawMessage, fallback, key string) ([]string, error) {
	value := fallback
	if raw != nil {
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("migrate: %s: %w", key, err)
		}
	}
	var out []string
	for _, l := range strings.Split(value, ",") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	if len(out) == 0 && fallback != "" {
		return nil, fmt.Errorf("migrate: %s: no label names", key)
	}
	return out, nil
}

// titlePattern translates a release-please title pattern into release-me's:
// release-please's ${scope} renders the branch in parentheses and its
// ${component} the name after a space, both "" where it has none, and
// release-me names the branch and the component without either. A pattern
// release-me refuses, one without every value release reads back, is warned
// about and left out. "" when raw is nil.
func titlePattern(raw json.RawMessage, key string, kind releasepr.TitleKind, warn func(string, ...any)) (string, error) {
	if raw == nil {
		return "", nil
	}
	var pattern string
	if err := json.Unmarshal(raw, &pattern); err != nil {
		return "", fmt.Errorf("migrate: %s: %w", key, err)
	}
	translated := strings.NewReplacer("${scope}", "(${branch})", "${component}", " ${component}").Replace(pattern)
	if err := config.CheckTitlePattern(translated, kind); err != nil {
		warn("%s: %q %v; release-me reads every component and its version back from the title, left out", key, translated, err)
		return "", nil
	}
	return translated, nil
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

// cargoPackageName reads the name of the crate a Cargo.toml's [package]
// table declares together with a version of its own. It is "" when the
// file, the table, the name or the version is missing: a virtual workspace
// manifest declares no package, and a crate inheriting
// "version.workspace = true" carries no version the toml updater can bump.
func cargoPackageName(readFile ReadFile, path string) string {
	data, err := readFile(path)
	if err != nil {
		return ""
	}
	inPackage, name, hasVersion := false, "", false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inPackage = line == "[package]"
			continue
		}
		if !inPackage {
			continue
		}
		if m := cargoName.FindStringSubmatch(line); m != nil {
			name = m[1]
		}
		if cargoVersion.MatchString(line) {
			hasVersion = true
		}
	}
	if !hasVersion {
		return ""
	}
	return name
}

// cargoName matches a Cargo.toml's "name = \"crate\"" line, cargoVersion
// its "version = \"1.2.3\"" line.
var (
	cargoName    = regexp.MustCompile(`^name\s*=\s*"([^"]+)"`)
	cargoVersion = regexp.MustCompile(`^version\s*=\s*"`)
)

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

// topLevel is every top-level release-me key the translation writes besides
// the components.
type topLevel struct {
	versioning  string
	sharedPR    bool
	pending     []string
	tagged      []string
	extra       []string
	style       style
	title       string
	sharedTitle string
	workspaces  []string
}

// render writes release-me.yaml. A single root package tagged "vX.Y.Z" is
// the implicit main component and needs no components key; everything else
// is a components mapping in release-please's package order, carrying only
// the keys whose value differs from what release-me would derive.
func render(components []component, top topLevel) ([]byte, error) {
	doc := &yaml.Node{Kind: yaml.MappingNode}
	add := func(m *yaml.Node, key string, value *yaml.Node) {
		m.Content = append(m.Content, scalar(key), value)
	}

	add(doc, "changelog-source", scalar("all"))
	if top.versioning != "" {
		add(doc, "versioning", scalar(top.versioning))
	}
	if top.sharedPR {
		add(doc, "release-pull-request", scalar("shared"))
	}
	if top.title != "" {
		add(doc, "release-pull-request-title", scalar(top.title))
	}
	if top.sharedTitle != "" {
		add(doc, "release-pull-request-shared-title", scalar(top.sharedTitle))
	}
	if len(top.workspaces) > 0 {
		list := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, w := range top.workspaces {
			list.Content = append(list.Content, scalar(w))
		}
		add(doc, "workspaces", list)
	}
	labels := &yaml.Node{Kind: yaml.MappingNode}
	add(labels, "pending", labelNode(top.pending))
	add(labels, "tagged", labelNode(top.tagged))
	if len(top.extra) > 0 {
		add(labels, "extra", labelNode(top.extra))
	}
	for _, t := range []string{"alpha", "beta", "rc"} {
		add(labels, t, boolean(false))
	}
	add(doc, "labels", labels)
	if top.style.sentenceCase {
		add(doc, "changelog-sentence-case", boolean(true))
	}

	// A pre-major rule carried by every component is written once, at the
	// top level, instead of repeated per component.
	shared := sharedRules{
		bumpMinorPreMajor: !slices.ContainsFunc(components, func(c component) bool { return !c.bumpMinorPreMajor }),
		bumpPatchForMinorPreMajor: !slices.ContainsFunc(components, func(c component) bool {
			return !c.bumpPatchForMinorPreMajor
		}),
	}
	if shared.bumpMinorPreMajor {
		add(doc, "bump-minor-pre-major", boolean(true))
	}
	if shared.bumpPatchForMinorPreMajor {
		add(doc, "bump-patch-for-minor-pre-major", boolean(true))
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

// sharedRules names the pre-major rules every component carries, which
// render writes once at the top level.
type sharedRules struct {
	bumpMinorPreMajor         bool
	bumpPatchForMinorPreMajor bool
}

// addSettings writes the settings release-me reads per component or at the
// top level alike: initial-version, bump-minor-pre-major,
// bump-patch-for-minor-pre-major and files. A pre-major rule is left out
// where the top level already carries it.
func addSettings(m *yaml.Node, c component, shared sharedRules, add func(*yaml.Node, string, *yaml.Node)) {
	if c.initialVersion != "" {
		add(m, "initial-version", scalar(c.initialVersion))
	}
	if c.bumpMinorPreMajor && !shared.bumpMinorPreMajor {
		add(m, "bump-minor-pre-major", boolean(true))
	}
	if c.bumpPatchForMinorPreMajor && !shared.bumpPatchForMinorPreMajor {
		add(m, "bump-patch-for-minor-pre-major", boolean(true))
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

// labelNode is one label name as a plain string, the form release-me.yaml
// always accepted, or several as a list.
func labelNode(names []string) *yaml.Node {
	if len(names) == 1 {
		return scalar(names[0])
	}
	list := &yaml.Node{Kind: yaml.SequenceNode}
	for _, n := range names {
		list.Content = append(list.Content, scalar(n))
	}
	return list
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
