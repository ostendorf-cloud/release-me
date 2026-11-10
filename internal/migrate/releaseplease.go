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
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
	"github.com/ostendorf-cloud/release-me/internal/updater"
	"github.com/ostendorf-cloud/release-me/internal/version"
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
var packageKeys = append([]string{
	"release-type", "package-name", "component", "include-component-in-tag", "include-v-in-tag",
	"tag-separator", "changelog-path", "bump-minor-pre-major", "bump-patch-for-minor-pre-major",
	"initial-version", "extra-files", "skip-changelog", "version-file", "exclude-paths",
	"changelog-type", "release-as", "include-commit-authors", "changelog-host", "prerelease-type",
	"prerelease", "changelog-sections", "versioning",
}, releaseFlagKeys()...)

// releaseFlag is a boolean release-please package setting release-me reads
// under the same key, top level and per component, and release-me's own
// default for it: a value equal to the default is left out of
// release-me.yaml. An absent source key also adopts this target default.
// For force-tag-creation that deliberately differs from release-please:
// release-please's draft bears on when it creates the tag, release-me's
// own draft never does, so both defaults still create the tag during
// immediate publication regardless of draft. A setting with different
// absence semantics needs a separate source default.
type releaseFlag struct {
	key string
	def bool
}

// releaseFlags are the release-flag settings this translation carries
// over, in the order release-me.yaml lists them.
var releaseFlags = []releaseFlag{
	{key: "force-tag-creation", def: true},
	{key: "skip-github-release", def: false},
	{key: "include-v-in-release-name", def: true},
	{key: "draft", def: false},
}

// releaseFlagKeys returns the key of every entry in releaseFlags.
func releaseFlagKeys() []string {
	keys := make([]string, 0, len(releaseFlags))
	for _, f := range releaseFlags {
		keys = append(keys, f.key)
	}
	return keys
}

// topLevelKeys are the release-please settings this translation reads at
// the top level or deliberately leaves out there. Some also allow per-package
// settings that release-me cannot represent, reported separately below.
var topLevelKeys = []string{
	"packages", "plugins", "label", "release-label", "extra-label", "separate-pull-requests", "$schema",
	"bootstrap-sha", "last-release-sha", "draft-pull-request", "pull-request-title-pattern",
	"group-pull-request-title-pattern", "always-link-local", "signoff", "pull-request-header",
	"pull-request-footer",
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
			if kv.key == "draft-pull-request" {
				warn("package %s: draft-pull-request: release-me's release-pull-request-draft is repository-wide, set it at the top level if every release pull request should be a draft", entry.key)
				continue
			}
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
	// release-me credits authors for every component or for none.
	switch authors := slices.IndexFunc(components, func(c component) bool { return !c.includeCommitAuthors }) == -1; {
	case authors:
		st.authors = true
	case slices.ContainsFunc(components, func(c component) bool { return c.includeCommitAuthors }):
		warn("include-commit-authors: release-me credits authors for every component or none, left out")
	}
	st.host = translateChangelogHost(components, warn)
	// release-please's default changelog links every entry's commit and
	// the issues it closes; release-me's does neither unless asked to.
	st.commitLinks = true
	st.issueReferences = true

	if versioning == "locked" {
		// release-me's service pack counts on one component's own tags,
		// so a locked family, one shared version line, refuses it.
		for i := range components {
			if components[i].strategy == "service-pack" {
				warn("package %s: versioning service-pack: release-me releases service packs only with "+
					"versioning independent, not in the locked family linked-versions became, left out", components[i].path)
				components[i].strategy = ""
			}
		}
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

	// always-link-local is a node-workspace option, default true, release-me's
	// default too; only false needs writing.
	linkLocal := true
	if raw, ok := topValues["always-link-local"]; ok {
		if err := json.Unmarshal(raw, &linkLocal); err != nil {
			return Result{}, fmt.Errorf("migrate: always-link-local: %w", err)
		}
		if !slices.Contains(workspaces, workspace.Node) {
			warn("always-link-local: applies to the node-workspace plugin, left out")
			linkLocal = true
		}
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
	dropShippedReleaseAs(packages, components, versions, warn)
	if versioning != "" {
		// A linked or locked family shares one version: release-me takes
		// release-as only at the top level there, so only a value every
		// component carries survives.
		if slices.ContainsFunc(components, func(c component) bool { return c.releaseAs != components[0].releaseAs }) {
			for i := range components {
				if components[i].releaseAs != "" {
					warn("package %s: release-as: release-me forces a %s family's one version at the top level only, "+
						"and the packages force different ones, left out", components[i].path, versioning)
					components[i].releaseAs = ""
				}
			}
		}
	}

	opts := topLevel{
		versioning: versioning, sharedPR: sharedPR, pending: pending, tagged: tagged, extra: extra, style: st,
		workspaces: workspaces, separateLinks: !linkLocal,
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
	if raw, ok := topValues["signoff"]; ok {
		if err := json.Unmarshal(raw, &opts.signoff); err != nil {
			return Result{}, fmt.Errorf("migrate: signoff: %w", err)
		}
		if opts.signoff != "" {
			if err := releasepr.CheckSignoff(opts.signoff); err != nil {
				warn("signoff: %q %v, left out", opts.signoff, err)
				opts.signoff = ""
			}
		}
	}
	if raw, ok := topValues["draft-pull-request"]; ok {
		if err := json.Unmarshal(raw, &opts.draft); err != nil {
			return Result{}, fmt.Errorf("migrate: draft-pull-request: %w", err)
		}
	}
	for _, text := range []struct {
		key string
		dst *string
	}{{"pull-request-header", &opts.header}, {"pull-request-footer", &opts.footer}} {
		if *text.dst, err = frameText(topValues[text.key], text.key, warn); err != nil {
			return Result{}, err
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
	// changelogType is release-please's changelog-type, "" for default.
	changelogType string
	// includeCommitAuthors is release-please's include-commit-authors.
	includeCommitAuthors bool
	// changelogHost is release-please's changelog-host, "" for none.
	changelogHost string
	// flags holds every releaseFlags key's value for this package: the
	// one release-please was given, else release-me's default.
	flags map[string]bool
	// preReleaseType is release-please's prerelease-type, a type
	// release-me selects through a label, "" when it is left out or
	// cannot carry over.
	preReleaseType string
	// excludePaths are the package's exclude-paths that lie inside it,
	// cleaned, relative to the repository root as in release-please.
	excludePaths []string
	// skipChangelog is release-please's skip-changelog.
	skipChangelog bool
	// releaseAs is release-please's release-as, release-me's too, ""
	// when it is left out or cannot carry over.
	releaseAs string
	// strategy is release-me's versioning-strategy, "" for release-please's
	// default.
	strategy string
	// changelogSections is release-please's changelog-sections, nil when
	// the package leaves the default sections.
	changelogSections *[]config.ChangelogSection
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
		releaseType, packageName, componentName, separator, changelogPath string
		initialVersion, changelogType, changelogHost, preReleaseType      string
		versionFileName, releaseAs, versioning                            string
		preRelease, skipChangelog                                         bool
		includeComponent, includeV                                        = true, true
		bumpMinor, bumpPatchForMinor, includeAuthors                      bool
	)
	separator = "-"
	changelogPath = "CHANGELOG.md"
	for key, target := range map[string]any{
		"release-type": &releaseType, "package-name": &packageName, "component": &componentName,
		"tag-separator": &separator, "changelog-path": &changelogPath, "initial-version": &initialVersion,
		"version-file":             &versionFileName,
		"include-component-in-tag": &includeComponent, "include-v-in-tag": &includeV,
		"bump-minor-pre-major": &bumpMinor, "bump-patch-for-minor-pre-major": &bumpPatchForMinor,
		"changelog-type": &changelogType, "include-commit-authors": &includeAuthors,
		"changelog-host":  &changelogHost,
		"prerelease-type": &preReleaseType, "prerelease": &preRelease,
		"skip-changelog": &skipChangelog, "release-as": &releaseAs,
		"versioning": &versioning,
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
	preReleaseType = translatePreReleaseType(p.path, preReleaseType, preRelease, warn)
	strategy := translateVersioning(p.path, versioning, warn)

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
		preReleaseType:            preReleaseType,
		releaseAs:                 translateReleaseAs(p.path, releaseAs, warn),
		strategy:                  strategy,
		initialVersion:            initialVersion,
		changelogType:             changelogType,
		includeCommitAuthors:      includeAuthors,
		changelogHost:             changelogHost,
		skipChangelog:             skipChangelog,
		flags:                     make(map[string]bool, len(releaseFlags)),
	}
	for _, flag := range releaseFlags {
		value := flag.def
		if raw, ok := p.settings[flag.key]; ok {
			if err := json.Unmarshal(raw, &value); err != nil {
				return component{}, fmt.Errorf("migrate: package %s: %s: %w", p.path, flag.key, err)
			}
		}
		c.flags[flag.key] = value
	}

	// A version file the release type implies is carried over only where it
	// exists: release-type falls back to node, and an entry for a file that
	// is not there would fail the first release pull request. An optional
	// file is one release-please bumps only where it exists and holds a
	// version, so one that does not is left out without a warning.
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
		// release-please looks for the version.rb under the gem's name, a
		// "-" nesting a directory, unless version-file names it.
		switch {
		case versionFileName != "":
			versionFile(versionFileName, updater.Ruby, "", false)
		case componentName != "":
			versionFile("lib/"+strings.ReplaceAll(componentName, "-", "/")+"/version.rb", updater.Ruby, "", false)
		default:
			warn("package %s: release-type ruby: no component or package-name to find its version.rb by, "+
				"add it under files", p.path)
		}
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
	case "python":
		// release-please bumps each of these only where it exists and
		// holds a version, finding the package's __init__.py and
		// version.py by the project name pyproject.toml declares, else by
		// the component.
		before := len(c.files)
		versionFile("setup.cfg", updater.Python, "", true)
		versionFile("setup.py", updater.Python, "", true)
		pyproject, _ := readFile(stdpath.Join(p.path, "pyproject.toml"))
		project := componentName
		for _, table := range []string{"project", "tool.poetry"} {
			if name := tomlString(string(pyproject), table, "name"); name != "" {
				project = name
				break
			}
		}
		beforePyproject := len(c.files)
		for _, table := range []string{"project", "tool.poetry"} {
			versionFile("pyproject.toml", updater.TOML, table+".version", true)
			if len(c.files) > beforePyproject {
				break
			}
		}
		if project != "" {
			for _, dir := range slices.Compact([]string{project, strings.ReplaceAll(project, "-", "_")}) {
				for _, f := range []string{"__init__.py", "version.py"} {
					versionFile(stdpath.Join(dir, f), updater.Python, "", true)
					versionFile(stdpath.Join("src", dir, f), updater.Python, "", true)
				}
			}
		}
		if len(c.files) == before {
			warn("package %s: release-type python: no setup.cfg, setup.py, pyproject.toml, __init__.py or version.py "+
				"with a version found, add the version file under files", p.path)
		}
	case "elixir":
		versionFile("mix.exs", updater.Elixir, "", false)
	case "ocaml":
		before := len(c.files)
		if componentName != "" {
			versionFile(componentName+".opam", updater.OCaml, "", true)
			versionFile(componentName+".opam.locked", updater.OCaml, "", true)
		}
		versionFile("dune-project", updater.OCaml, "", true)
		versionFile("esy.json", updater.JSON, "version", true)
		versionFile("package.json", updater.JSON, "version", true)
		if len(c.files) == before {
			warn("package %s: release-type ocaml: no %s.opam, dune-project or esy.json with a version found, "+
				"add the version files under files", p.path, componentName)
			break
		}
		warn("package %s: release-type ocaml: release-please bumped every .opam and esy JSON file of the package, "+
			"only those named after the package are carried over, add any other under files", p.path)
	case "r":
		versionFile("DESCRIPTION", updater.R, "", false)
		// release-please's r type writes its changelog into NEWS.md.
		if _, ok := p.settings["changelog-path"]; !ok {
			c.changelog = repoPath(p.path, "NEWS.md")
		}
	case "bazel":
		if versionFileName == "" {
			versionFileName = "MODULE.bazel"
		}
		versionFile(versionFileName, updater.Bazel, "", false)
	case "php":
		// release-please bumps composer.json's version only where it has
		// one, and a VERSION file only where it exists.
		versionFile("composer.json", updater.JSON, "version", true)
		if _, err := readFile(stdpath.Join(p.path, "VERSION")); err == nil {
			warn("package %s: VERSION: release-please bumped it, release-me does not", p.path)
		}
	case "dart":
		versionFile("pubspec.yaml", updater.Dart, "", false)
	case "expo":
		versionFile("package.json", updater.JSON, "version", false)
		if _, err := readFile(stdpath.Join(p.path, "package-lock.json")); err == nil {
			warn("package %s: package-lock.json: release-please bumped it, release-me does not", p.path)
		}
		// The expo updater computes app.json's expo.android.versionCode
		// from the Expo SDK's major version, which release-please reads off
		// the expo dependency of the same package.json and defaults to 0.
		sdk := expoSDKVersion(readFile, stdpath.Join(p.path, "package.json"))
		if sdk == "" {
			sdk = "0"
			warn("package %s: release-type expo: package.json declares no expo dependency to read the Expo SDK "+
				"version off, app.json's expo.android.versionCode is computed with a major of 0, as release-please "+
				"did, set the key under files to the SDK version", p.path)
		}
		versionFile("app.json", updater.Expo, sdk, false)
	case "sfdx":
		versionFile("sfdx-project.json", updater.Sfdx, "", false)
	case "terraform-module":
		before := len(c.files)
		versionFile("versions.tf", updater.TerraformModule, "", true)
		versionFile("versions.tf.tmpl", updater.TerraformModule, "", true)
		versionFile("metadata.yaml", updater.YAML, "version", true)
		if len(c.files) == before {
			warn("package %s: release-type terraform-module: no versions.tf, versions.tf.tmpl or metadata.yaml "+
				"holding a version found, add the module's version files under files", p.path)
			break
		}
		warn("package %s: release-type terraform-module: release-please searched the package for the versions.tf of "+
			"every submodule and rewrote the version constraint of every README, only the module's own files are "+
			"carried over, add any other under files", p.path)
	case "krm-blueprint":
		warn("package %s: release-type krm-blueprint: release-please searched the package's YAML files for the "+
			"cnrm.cloud.google.com/blueprint annotation, release-me lists them: add each under files with "+
			"updater krm-blueprint", p.path)
	case "helm":
		versionFile("Chart.yaml", updater.Helm, "", false)
	case "go":
	default:
		warn("package %s: release-type %s: the files it bumps are not carried over, add them under files", p.path, releaseType)
	}

	switch changelogType {
	case "", config.ChangelogTypeDefault:
		c.changelogType = ""
	case config.ChangelogTypeGitHub:
	default:
		warn("package %s: changelog-type %s: release-me knows default and github, left out", p.path, changelogType)
		c.changelogType = ""
	}

	if raw, ok := p.settings["changelog-sections"]; ok {
		var sections []struct {
			Type    string `json:"type"`
			Section string `json:"section"`
			Hidden  bool   `json:"hidden"`
		}
		if err := json.Unmarshal(raw, &sections); err != nil {
			return component{}, fmt.Errorf("migrate: package %s: changelog-sections: %w", p.path, err)
		}
		translated := make([]config.ChangelogSection, 0, len(sections))
		for _, s := range sections {
			translated = append(translated, config.ChangelogSection{Type: s.Type, Section: s.Section, Hidden: s.Hidden})
		}
		c.changelogSections = &translated
	}

	if raw, ok := p.settings["extra-files"]; ok {
		files, err := translateExtraFiles(p.path, raw, readFile, warn)
		if err != nil {
			return component{}, err
		}
		c.files = append(c.files, files...)
	}
	if raw, ok := p.settings["exclude-paths"]; ok {
		exclude, err := translateExcludePaths(p.path, raw, warn)
		if err != nil {
			return component{}, err
		}
		c.excludePaths = exclude
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

// translateChangelogHost carries over a changelog-host every component
// shares: release-me links every component's changelog on one host.
func translateChangelogHost(components []component, warn func(string, ...any)) string {
	host := components[0].changelogHost
	for _, c := range components[1:] {
		if c.changelogHost != host {
			warn("changelog-host: release-me links every component on one host, left out")
			return ""
		}
	}
	if host == "" {
		return ""
	}
	valid, err := config.ValidateChangelogHost(host)
	if err != nil {
		warn("changelog-host: %s, left out", err)
		return ""
	}
	return valid
}

// preReleaseTypeName matches a type name release-me.yaml's
// "labels.prerelease" accepts: one semver pre-release identifier.
var preReleaseTypeName = regexp.MustCompile(`^[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*$`)

// translatePreReleaseType carries release-please's prerelease-type over as
// a pre-release type release-me selects through its label: alpha, beta and
// rc stay on, any other valid name becomes a further "labels.prerelease"
// entry. release-please cuts it on every release with prerelease true;
// release-me cuts it while the label is on the release pull request, so
// either way the difference is warned about. A name release-me cannot
// carry is left out.
func translatePreReleaseType(pkgPath, preReleaseType string, preRelease bool, warn func(string, ...any)) string {
	if preReleaseType == "" {
		if preRelease {
			warn("package %s: prerelease: release-me cuts a pre-release only while the release pull request "+
				"carries a pre-release type label, left out", pkgPath)
		}
		return ""
	}
	if !preReleaseTypeName.MatchString(preReleaseType) {
		warn("package %s: prerelease-type %s: not a pre-release type release-me can carry, left out", pkgPath, preReleaseType)
		return ""
	}
	warn("package %s: prerelease-type %s: release-me cuts it only while the release pull request carries the label %q",
		pkgPath, preReleaseType, preReleaseLabel(preReleaseType))
	return preReleaseType
}

// preReleaseLabel is the label a translated pre-release type is selected
// with, release-me's own "release: <type>" shape.
func preReleaseLabel(preReleaseType string) string {
	return "release: " + preReleaseType
}

// translateExcludePaths carries over the exclude-paths of the package at
// pkgPath. release-please reads them relative to the repository root, as
// release-me does; one that does not lie inside the package excludes
// nothing there, the shape a top-level default listing every package's
// subpaths leaves on each package, and is left out.
func translateExcludePaths(pkgPath string, raw json.RawMessage, warn func(string, ...any)) ([]string, error) {
	var paths []string
	if err := json.Unmarshal(raw, &paths); err != nil {
		return nil, fmt.Errorf("migrate: package %s: exclude-paths: %w", pkgPath, err)
	}
	var out []string
	for _, p := range paths {
		clean := cleanPackagePath(p)
		inside := clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") &&
			(pkgPath == "." || strings.HasPrefix(clean, pkgPath+"/"))
		if !inside {
			warn("package %s: exclude-paths: %s does not lie inside the package, left out", pkgPath, p)
			continue
		}
		if !slices.Contains(out, clean) {
			out = append(out, clean)
		}
	}
	return out, nil
}

// translateReleaseAs vets a release-please release-as value the way
// release-me.yaml's release-as key is: one that does not parse as a
// version, or a pre-release, release-me refuses, so it is warned about and
// left out.
func translateReleaseAs(pkgPath, releaseAs string, warn func(string, ...any)) string {
	if releaseAs == "" {
		return ""
	}
	if _, err := version.ParseForced(releaseAs, version.Semver{}, false, false); err != nil {
		warn("package %s: release-as %s: the value %v, left out", pkgPath, releaseAs, err)
		return ""
	}
	return releaseAs
}

// dropShippedReleaseAs leaves out every component's release-as that is not
// greater than the version .release-please-manifest.json records for its
// package: release-please already released it, and release-me would only
// warn about it on every run.
func dropShippedReleaseAs(packages []rpPackage, components []component, versions map[string]string, warn func(string, ...any)) {
	for i, p := range packages {
		manifest, ok := versions[p.path]
		if !ok || components[i].releaseAs == "" {
			continue
		}
		last, ok := version.ParseTag("v" + manifest)
		if !ok {
			continue
		}
		if _, err := version.ParseForced(components[i].releaseAs, last, true, false); err != nil {
			warn("package %s: release-as %s: already released, the manifest records %s, left out",
				p.path, components[i].releaseAs, manifest)
			components[i].releaseAs = ""
		}
	}
}

// translateVersioning maps release-please's "versioning" strategy to
// release-me's versioning-strategy, "" for the default one. Every
// strategy release-me has carries over by name, a difference a reader
// should know about warned; any other has no release-me equivalent and is
// warned about.
func translateVersioning(pkgPath, versioning string, warn func(string, ...any)) string {
	switch versioning {
	case "", "default":
		return ""
	case "always-bump-patch", "always-bump-minor", "always-bump-major":
		return versioning
	case "service-pack":
		warn("package %s: versioning service-pack: release-me releases X.Y.Z-sp.N as a GitHub pre-release, "+
			"semver orders it below X.Y.Z", pkgPath)
		return versioning
	case "dependency-manifest":
		warn("package %s: versioning dependency-manifest: release-me reads the dependency update from the "+
			"Renovate or Dependabot title, not from a Java manifest", pkgPath)
		return versioning
	default:
		warn("package %s: versioning %s: no release-me equivalent, left out", pkgPath, versioning)
	}
	return ""
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
	authors      bool
	// host is release-please's changelog-host, "" for none.
	host            string
	commitLinks     bool
	issueReferences bool
}

// workspacePlugins maps each release-please workspace plugin to the
// package manager release-me's "workspaces" names for it.
var workspacePlugins = map[string]string{
	"node-workspace":  workspace.Node,
	"cargo-workspace": workspace.Cargo,
	"maven-workspace": workspace.Maven,
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

// frameText reads release-please's pull-request-header or
// pull-request-footer, carried over as release-pull-request-header or
// release-pull-request-footer. A text release-me refuses, one carrying what
// a body is read back by, is warned about and left out. "" when raw is nil.
func frameText(raw json.RawMessage, key string, warn func(string, ...any)) (string, error) {
	if raw == nil {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", fmt.Errorf("migrate: %s: %w", key, err)
	}
	if err := releasepr.CheckFrameText(text); err != nil {
		warn("%s: %v, left out", key, err)
		return "", nil
	}
	return text, nil
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

// expoSDKVersion reads the version a package.json declares the "expo"
// dependency as, which is where release-please's expo strategy reads the
// Expo SDK version an app.json's expo.android.versionCode is computed from.
// The dependency sections are read in release-please's own order, the first
// one declaring expo winning. It is "" when the file is missing, is no JSON
// object or declares no expo dependency, which release-please treated as
// SDK version 0.0.0.
func expoSDKVersion(readFile ReadFile, path string) string {
	data, err := readFile(path)
	if err != nil {
		return ""
	}
	var pkg struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	for _, deps := range []map[string]string{
		pkg.Dependencies, pkg.DevDependencies, pkg.PeerDependencies, pkg.OptionalDependencies,
	} {
		if v := deps["expo"]; v != "" {
			return v
		}
	}
	return ""
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

// tomlString reads the string a TOML file assigns to key under the
// [table] header, "" when the table, the key or a string value is missing.
// It reads the plain "key = \"value\"" lines a Cargo.toml or a
// pyproject.toml carries, not every shape TOML allows.
func tomlString(content, table, key string) string {
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(key) + `\s*=\s*"([^"]*)"`)
	inTable := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inTable = line == "["+table+"]"
			continue
		}
		if !inTable {
			continue
		}
		if m := pattern.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
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
	// separateLinks writes always-link-local: false.
	separateLinks bool
	signoff       string
	draft         bool
	header        string
	footer        string
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
	if top.style.commitLinks {
		add(doc, "changelog-commit-links", boolean(true))
	}
	if top.style.issueReferences {
		add(doc, "changelog-issue-references", boolean(true))
	}
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
	if top.header != "" {
		add(doc, "release-pull-request-header", scalar(top.header))
	}
	if top.footer != "" {
		add(doc, "release-pull-request-footer", scalar(top.footer))
	}
	if len(top.workspaces) > 0 {
		list := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, w := range top.workspaces {
			list.Content = append(list.Content, scalar(w))
		}
		add(doc, "workspaces", list)
	}
	if top.separateLinks {
		add(doc, "always-link-local", boolean(false))
	}
	if top.signoff != "" {
		add(doc, "signoff", scalar(top.signoff))
	}
	if top.draft {
		add(doc, "release-pull-request-draft", boolean(true))
	}
	labels := &yaml.Node{Kind: yaml.MappingNode}
	add(labels, "pending", labelNode(top.pending))
	add(labels, "tagged", labelNode(top.tagged))
	if len(top.extra) > 0 {
		add(labels, "extra", labelNode(top.extra))
	}
	// alpha, beta and rc stay off unless a package's prerelease-type
	// names one; any other type is appended after them, in the order the
	// packages first name it.
	var others []string
	for _, c := range components {
		if c.preReleaseType != "" && !slices.Contains(others, c.preReleaseType) {
			others = append(others, c.preReleaseType)
		}
	}
	preRelease := &yaml.Node{Kind: yaml.MappingNode}
	for _, t := range []string{"alpha", "beta", "rc"} {
		if slices.Contains(others, t) {
			add(preRelease, t, scalar(preReleaseLabel(t)))
		} else {
			add(preRelease, t, boolean(false))
		}
	}
	for _, t := range others {
		if t == "alpha" || t == "beta" || t == "rc" {
			continue
		}
		add(preRelease, t, scalar(preReleaseLabel(t)))
	}
	add(labels, "prerelease", preRelease)
	add(doc, "labels", labels)
	if top.style.sentenceCase {
		add(doc, "changelog-sentence-case", boolean(true))
	}
	if top.style.authors {
		add(doc, "changelog-authors", boolean(true))
	}
	if top.style.host != "" {
		add(doc, "changelog-host", scalar(top.style.host))
	}

	// bump-minor-pre-major and bump-patch-for-minor-pre-major carried by
	// every component are written once, at the top level, instead of
	// repeated per component. So are a changelog-type, skip-changelog, a
	// release-as value, a versioning-strategy and a changelog-sections
	// list every component carries alike.
	shared := sharedRules{
		bumpMinorPreMajor: slices.IndexFunc(components, func(c component) bool { return !c.bumpMinorPreMajor }) == -1,
		bumpPatchForMinorPreMajor: slices.IndexFunc(components, func(c component) bool {
			return !c.bumpPatchForMinorPreMajor
		}) == -1,
		changelogType: !slices.ContainsFunc(components, func(c component) bool {
			return c.changelogType != config.ChangelogTypeGitHub
		}),
		skipChangelog: !slices.ContainsFunc(components, func(c component) bool { return !c.skipChangelog }),
		changelogSections: components[0].changelogSections != nil &&
			slices.IndexFunc(components, func(c component) bool {
				return !reflect.DeepEqual(c.changelogSections, components[0].changelogSections)
			}) == -1,
	}
	if !slices.ContainsFunc(components, func(c component) bool { return c.releaseAs != components[0].releaseAs }) {
		shared.releaseAs = components[0].releaseAs
	}
	if !slices.ContainsFunc(components, func(c component) bool { return c.strategy != components[0].strategy }) {
		shared.strategy = components[0].strategy
	}
	if shared.bumpMinorPreMajor {
		add(doc, "bump-minor-pre-major", boolean(true))
	}
	if shared.bumpPatchForMinorPreMajor {
		add(doc, "bump-patch-for-minor-pre-major", boolean(true))
	}
	if shared.changelogType {
		add(doc, "changelog-type", scalar(config.ChangelogTypeGitHub))
	}
	if shared.skipChangelog {
		add(doc, "skip-changelog", boolean(true))
	}
	if shared.releaseAs != "" {
		add(doc, "release-as", scalar(shared.releaseAs))
	}
	if shared.strategy != "" {
		add(doc, "versioning-strategy", scalar(shared.strategy))
	}
	if shared.changelogSections {
		add(doc, "changelog-sections", sectionsNode(*components[0].changelogSections, add))
	}
	// A release flag every component carries with the same value, other
	// than release-me's default, is written once at the top level too.
	topFlags := make(map[string]bool, len(releaseFlags))
	for _, flag := range releaseFlags {
		value := components[0].flags[flag.key]
		same := slices.IndexFunc(components, func(c component) bool { return c.flags[flag.key] != value }) == -1
		if same && value != flag.def {
			add(doc, flag.key, boolean(value))
			topFlags[flag.key] = true
		}
	}

	if len(components) == 1 && components[0].path == "." && components[0].tagPrefix == "v" {
		c := components[0]
		if c.changelog != "CHANGELOG.md" {
			add(doc, "changelog", scalar(c.changelog))
		}
		addSettings(doc, c, shared, topFlags, add)
		addExcludePaths(doc, c, add)
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
			addSettings(m, c, shared, topFlags, add)
			addExcludePaths(m, c, add)
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

// sharedRules names the per-component settings every component carries
// alike, which render writes once at the top level instead of repeating
// per component: the pre-major rules, changelog-type, skip-changelog,
// release-as, the versioning strategy and changelog-sections.
type sharedRules struct {
	bumpMinorPreMajor         bool
	bumpPatchForMinorPreMajor bool
	changelogType             bool
	skipChangelog             bool
	// releaseAs is "" when the components differ or none carries one.
	releaseAs string
	// strategy is "" when the components differ or all carry the default.
	strategy          string
	changelogSections bool
}

// sectionsNode is a changelog-sections list, release-please's shape.
func sectionsNode(sections []config.ChangelogSection, add func(*yaml.Node, string, *yaml.Node)) *yaml.Node {
	list := &yaml.Node{Kind: yaml.SequenceNode}
	for _, s := range sections {
		entry := &yaml.Node{Kind: yaml.MappingNode}
		add(entry, "type", scalar(s.Type))
		if s.Section != "" {
			add(entry, "section", scalar(s.Section))
		}
		if s.Hidden {
			add(entry, "hidden", boolean(true))
		}
		list.Content = append(list.Content, entry)
	}
	return list
}

// addSettings writes the settings release-me reads per component or at the
// top level alike: initial-version, bump-minor-pre-major,
// bump-patch-for-minor-pre-major, changelog-type, skip-changelog,
// release-as, versioning-strategy, changelog-sections, the release flags
// and files. A setting shared names is left out, the top level already
// carries it; a release flag is also left out where it is release-me's
// default.
func addSettings(
	m *yaml.Node, c component, shared sharedRules, topFlags map[string]bool, add func(*yaml.Node, string, *yaml.Node),
) {
	if c.initialVersion != "" {
		add(m, "initial-version", scalar(c.initialVersion))
	}
	if c.bumpMinorPreMajor && !shared.bumpMinorPreMajor {
		add(m, "bump-minor-pre-major", boolean(true))
	}
	if c.bumpPatchForMinorPreMajor && !shared.bumpPatchForMinorPreMajor {
		add(m, "bump-patch-for-minor-pre-major", boolean(true))
	}
	if c.changelogType == config.ChangelogTypeGitHub && !shared.changelogType {
		add(m, "changelog-type", scalar(c.changelogType))
	}
	if c.skipChangelog && !shared.skipChangelog {
		add(m, "skip-changelog", boolean(true))
	}
	if c.releaseAs != "" && shared.releaseAs == "" {
		add(m, "release-as", scalar(c.releaseAs))
	}
	if c.strategy != "" && shared.strategy == "" {
		add(m, "versioning-strategy", scalar(c.strategy))
	}
	if c.changelogSections != nil && !shared.changelogSections {
		add(m, "changelog-sections", sectionsNode(*c.changelogSections, add))
	}
	for _, flag := range releaseFlags {
		if value := c.flags[flag.key]; !topFlags[flag.key] && value != flag.def {
			add(m, flag.key, boolean(value))
		}
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

// addExcludePaths writes the component's exclude-paths, at the top level for
// the implicit main component and on the component otherwise.
func addExcludePaths(m *yaml.Node, c component, add func(*yaml.Node, string, *yaml.Node)) {
	if len(c.excludePaths) == 0 {
		return
	}
	list := &yaml.Node{Kind: yaml.SequenceNode}
	for _, p := range c.excludePaths {
		list.Content = append(list.Content, scalar(p))
	}
	add(m, "exclude-paths", list)
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
