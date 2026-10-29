// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package config loads release-me.yaml: initial-version,
// bump-minor-pre-major, hidden-types, fallback-type, token-env, files,
// changelog, changelog-source, labels, author, major-tag, versioning,
// draft-until-assets-attach, comment-mention and components.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	stdpath "path"
	"slices"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/conventional"
	"github.com/ostendorf-cloud/release-me/internal/updater"
	"gopkg.in/yaml.v3"
)

// Defaults applied for a key left out of release-me.yaml, or for a missing
// file entirely.
const (
	DefaultInitialVersion = "0.1.0"
	DefaultTokenEnv       = "GITHUB_TOKEN"
	// DefaultFallbackType is conventional.FallbackType, named here too so
	// callers of this package never need to import internal/conventional
	// just to read the default.
	DefaultFallbackType = conventional.FallbackType
	// DefaultChangelogPath is the changelog file path applied when the
	// "changelog" key is left out of release-me.yaml.
	DefaultChangelogPath = "CHANGELOG.md"
	// DefaultChangelogSource is the changelog source mode applied when
	// the "changelog-source" key is left out of release-me.yaml.
	DefaultChangelogSource = ChangelogSourceFirstParent
	// DefaultPendingLabel and DefaultTaggedLabel are the label names
	// applied when the "labels" block, or one of its keys, is left out.
	DefaultPendingLabel = "release: pending"
	DefaultTaggedLabel  = "release: tagged"
	// DefaultAlphaLabel, DefaultBetaLabel and DefaultRcLabel are the
	// pre-release type label names applied when the "labels" block, or one
	// of its keys, is left out.
	DefaultAlphaLabel = "release: alpha"
	DefaultBetaLabel  = "release: beta"
	DefaultRcLabel    = "release: rc"
	// DefaultAuthorName and DefaultAuthorEmail are the git identity applied
	// when the "author" block, or one of its keys, is left out.
	DefaultAuthorName  = "release-me"
	DefaultAuthorEmail = "release-me[bot]@users.noreply.github.com"
	// DefaultVersioning is the versioning mode applied when the
	// "versioning" key is left out of release-me.yaml.
	DefaultVersioning = VersioningIndependent
	// DefaultCommentMention is the mention prefix applied when the
	// "comment-mention" key is left out of release-me.yaml: a comment on
	// the release pull request naming it may issue a "release-as" or
	// "release-notes" command.
	DefaultCommentMention = "@release-me"
	// sharedBranch is the release branch every component carries when its
	// release pull request is shared, always in VersioningLocked and with
	// "release-pull-request: shared": one release pull request for every
	// component, instead of one branch per component.
	sharedBranch = "release-me/main"
)

// Versioning names how a repository's configured components' versions
// relate to each other.
type Versioning string

// The versioning modes release-me.yaml's top-level "versioning" key
// accepts.
const (
	// VersioningIndependent versions every component on its own: its own
	// tag line, its own bump, its own release pull request unless
	// "release-pull-request" is shared. The default.
	VersioningIndependent Versioning = "independent"
	// VersioningLocked bumps every component to the same new version and
	// releases all of them together, changed or not, through one release
	// pull request for the whole family, branch "release-me/main".
	VersioningLocked Versioning = "locked"
	// VersioningLinked shares one version line across every component, but
	// releases only a component with entries since its own last tag, at
	// the family's next version: the highest version any component in the
	// family carries, bumped by the highest bump type among the changed
	// components. Each changed component gets its own release pull request
	// unless "release-pull-request" is shared.
	VersioningLinked Versioning = "linked"
)

// validVersioning rejects a "versioning" value outside the three modes
// above.
var validVersioning = map[Versioning]bool{
	VersioningIndependent: true, VersioningLocked: true, VersioningLinked: true,
}

// ReleasePullRequest names how many release pull requests a repository's
// configured components share.
type ReleasePullRequest string

// The values release-me.yaml's top-level "release-pull-request" key accepts.
const (
	// ReleasePullRequestPerComponent opens one release pull request per
	// component, branch "release-me/<name>". The default in
	// VersioningIndependent and VersioningLinked; VersioningLocked rejects
	// it.
	ReleasePullRequestPerComponent ReleasePullRequest = "per-component"
	// ReleasePullRequestShared opens one release pull request, branch
	// "release-me/main", carrying every component with something to release
	// this run, each at its own version. Always the case in
	// VersioningLocked.
	ReleasePullRequestShared ReleasePullRequest = "shared"
)

// ReleasePullRequestLayout names how the body of a release pull request
// carrying several components, shared or locked, lays out each component's
// changelog.
type ReleasePullRequestLayout string

// The values release-me.yaml's top-level "release-pull-request-layout" key
// accepts.
const (
	// LayoutHeadings renders "## <name>" per component, its changelog
	// section shifted one heading level down below it. The default.
	LayoutHeadings ReleasePullRequestLayout = "headings"
	// LayoutDetails renders one collapsed "<details>" block per component,
	// summarised "<name> vX.Y.Z", the way release-please does.
	LayoutDetails ReleasePullRequestLayout = "details"
	// LayoutCompact renders "#### <name>" per component, its changelog
	// section at the levels its CHANGELOG.md carries: the layout before the
	// key existed.
	LayoutCompact ReleasePullRequestLayout = "compact"
)

// DefaultReleasePullRequestLayout is the layout applied when the key is
// left out of release-me.yaml.
const DefaultReleasePullRequestLayout = LayoutHeadings

var validLayout = map[ReleasePullRequestLayout]bool{
	LayoutHeadings: true, LayoutDetails: true, LayoutCompact: true,
}

// DefaultHiddenTypes is the hidden-types list applied when the key is left
// out of release-me.yaml. DefaultFallbackType is hidden by default.
var DefaultHiddenTypes = []string{"chore", "docs", "ci", "style", "refactor", "test", "build", DefaultFallbackType}

// The changelog source modes "changelog-source" accepts, top level and per
// component. See internal/history for what each mode reads and
// cmd/release-me for how each is assembled into changelog entries.
const (
	// ChangelogSourceFirstParent reads the first parent history: a merge
	// commit is one entry with its pull request title, a direct commit
	// is its own entry. The default.
	ChangelogSourceFirstParent = "first-parent"
	// ChangelogSourceSquash reads the first parent history too, but
	// resolves the pull request number from a trailing "(#N)" on a
	// commit's own subject, the shape a squash merge leaves behind.
	ChangelogSourceSquash = "squash"
	// ChangelogSourceAll reads every commit since the last tag, merge
	// commits excluded, each its own entry, with a pull request link
	// resolved through the forge once per merge commit.
	ChangelogSourceAll = "all"
	// ChangelogSourceNested reads the first parent history like
	// ChangelogSourceFirstParent, with the commits of each merge
	// rendered as indented sub items under it.
	ChangelogSourceNested = "nested"
)

// changelogSources lists every value "changelog-source" accepts, in the
// order check and config errors report them.
var changelogSources = []string{
	ChangelogSourceFirstParent, ChangelogSourceSquash, ChangelogSourceAll, ChangelogSourceNested,
}

// bumpingTypes are refused as fallback-type: text that does not parse must
// never move the version.
var bumpingTypes = map[string]bool{"feat": true, "fix": true, "perf": true, "revert": true}

// File is one entry of the "files" key: a file release-me bumps the version
// in, through the named updater. Key is the dotted path, identifier or
// other locator the updater needs to find the value, empty for an updater
// that takes none.
type File struct {
	Path    string
	Updater string
	Key     string
}

// Labels is the "labels" key: the label names release-me expects on the
// repository. Alpha, Beta and Rc select a pre-release type on the release
// pull request; Pending and Tagged apply to every release, stable or not.
// An empty Alpha, Beta or Rc, configured as "" or false, turns that
// pre-release type off: release-me neither expects the label on the
// repository nor reads it from a pull request. Pending and Tagged are never
// empty.
type Labels struct {
	Pending string
	Tagged  string
	Alpha   string
	Beta    string
	Rc      string
}

// Author is the "author" key: the git identity pr commits the release
// branch as. A GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME or
// GIT_COMMITTER_EMAIL already set in the process environment wins over
// these values.
type Author struct {
	Name  string
	Email string
}

// Config is the release-me.yaml configuration.
type Config struct {
	InitialVersion    string
	BumpMinorPreMajor bool
	HiddenTypes       []string
	FallbackType      string
	TokenEnv          string
	Files             []File
	ChangelogPath     string
	// ChangelogSource is the top-level "changelog-source" key: which
	// commits become changelog entries. See the ChangelogSource*
	// constants. A component overrides it through its own
	// "changelog-source" key.
	ChangelogSource string
	Labels          Labels
	Author          Author
	// MajorTag is the top-level "major-tag" key: when true, a stable
	// release moves the major tag "vX" to the release commit. A component
	// overrides it through its own "major-tag" key.
	MajorTag bool
	// Versioning is the top-level "versioning" key: how the configured
	// components' versions relate to each other, default
	// VersioningIndependent. It applies to the whole family; there is no
	// per-component override.
	Versioning Versioning
	// ReleasePullRequest is the top-level "release-pull-request" key: one
	// release pull request per component or one shared by every component.
	// Left out, it is ReleasePullRequestShared in VersioningLocked and
	// ReleasePullRequestPerComponent otherwise. A repository without
	// "components" has one component, so the key changes nothing there.
	ReleasePullRequest ReleasePullRequest
	// ReleasePullRequestLayout is the top-level
	// "release-pull-request-layout" key: how a release pull request body
	// carrying several components, shared or locked, lays out each one's
	// changelog, default DefaultReleasePullRequestLayout. Only the body
	// changes: every CHANGELOG.md and every release body read back from it
	// stays the same whichever layout is set.
	ReleasePullRequestLayout ReleasePullRequestLayout
	// ReleasePullRequestGuide is the top-level "release-pull-request-guide"
	// key, default true: every release pull request body, per component,
	// shared or locked, opens with the collapsed "How this release works"
	// guide. false leaves the guide out. Nothing reads the body back by the
	// guide, so the key can change while a release pull request is open.
	ReleasePullRequestGuide bool
	// DraftUntilAssetsAttach is the top-level "draft-until-assets-attach"
	// key, default false: when true, release creates every release as a
	// draft instead of publishing it immediately, and resumes publishing
	// one left a draft by an earlier, interrupted run. Only a repository
	// with GitHub's immutable releases setting turned on needs this; it
	// applies to the whole repository, there is no per-component
	// override. See docs/adr/0001-draft-releases-until-assets-attach.md.
	DraftUntilAssetsAttach bool
	// CommentMention is the top-level "comment-mention" key: the mention
	// prefix a write-access commenter's "release-as" or "release-notes"
	// command on the release pull request must start with, default
	// DefaultCommentMention. There is no per-component override.
	CommentMention string
	// Components is the "components" key, in the order release-me.yaml
	// declares them, or nil when the key is absent: a repository without
	// it is one component at the repository root, as Resolve reports.
	Components []Component
}

// Component is one entry of the "components" key: a name mapped to a path
// and optional overrides of the top-level initial-version,
// bump-minor-pre-major, hidden-types, files, major-tag and
// changelog-source, plus its own tag-prefix and changelog path. A nil
// field was left out of release-me.yaml and falls back to the top-level
// value, the same left-out-versus-zero-value distinction Load applies to
// the top level itself.
type Component struct {
	Name              string
	Path              string
	InitialVersion    *string
	BumpMinorPreMajor *bool
	HiddenTypes       *[]string
	Files             *[]File
	MajorTag          *bool
	ChangelogSource   *string
	// TagPrefix is the component's "tag-prefix" key, nil when it was left
	// out and the component keeps the derived "<name>/v".
	TagPrefix *string
	// Changelog is the component's "changelog" key, nil when it was left
	// out and the component keeps the derived "<path>/CHANGELOG.md".
	Changelog *string
}

// ResolvedComponent is one component ready for a command to release: its
// name and path, the tag prefix configured or derived from the name, the
// release branch derived from it, the changelog path configured or derived
// from the path, and its settings with the top-level ones applied where it
// carries no override of its own.
type ResolvedComponent struct {
	Name          string
	Path          string
	TagPrefix     string
	Branch        string
	ChangelogPath string
	// Multi is true when release-me.yaml declares a "components" key. It
	// is false for the single implicit "main" component, which keeps
	// today's tag prefix "v" and release name instead of the generic
	// "<name>/v" prefix and "<name> vX.Y.Z" release name a configured
	// component carries.
	Multi             bool
	InitialVersion    string
	BumpMinorPreMajor bool
	HiddenTypes       []string
	Files             []File
	MajorTag          bool
	// ChangelogSource is one of the ChangelogSource* constants: which
	// commits become this component's changelog entries.
	ChangelogSource string
}

// Resolve returns the components a command should loop over, in
// release-me.yaml's order: the configured "components", each merged with
// the top-level settings it does not override, or the single implicit
// "main" component at the repository root when the "components" key is
// absent.
func (cfg Config) Resolve() []ResolvedComponent {
	if len(cfg.Components) == 0 {
		return []ResolvedComponent{{
			Name:              "main",
			Path:              ".",
			TagPrefix:         "v",
			Branch:            "release-me/main",
			ChangelogPath:     cfg.ChangelogPath,
			InitialVersion:    cfg.InitialVersion,
			BumpMinorPreMajor: cfg.BumpMinorPreMajor,
			HiddenTypes:       cfg.HiddenTypes,
			Files:             cfg.Files,
			MajorTag:          cfg.MajorTag,
			ChangelogSource:   cfg.ChangelogSource,
		}}
	}

	branch := func(name string) string {
		if cfg.SharedPullRequest() {
			// A shared release pull request keeps every component on one
			// branch, "release-me/main", instead of one branch per
			// component.
			return sharedBranch
		}
		return PerComponentBranch(name)
	}

	out := make([]ResolvedComponent, 0, len(cfg.Components))
	for _, c := range cfg.Components {
		rc := ResolvedComponent{
			Name:              c.Name,
			Path:              c.Path,
			TagPrefix:         c.tagPrefix(),
			Branch:            branch(c.Name),
			ChangelogPath:     c.changelogPath(),
			Multi:             true,
			InitialVersion:    cfg.InitialVersion,
			BumpMinorPreMajor: cfg.BumpMinorPreMajor,
			HiddenTypes:       cfg.HiddenTypes,
			Files:             cfg.Files,
			MajorTag:          cfg.MajorTag,
			ChangelogSource:   cfg.ChangelogSource,
		}
		if c.InitialVersion != nil {
			rc.InitialVersion = *c.InitialVersion
		}
		if c.BumpMinorPreMajor != nil {
			rc.BumpMinorPreMajor = *c.BumpMinorPreMajor
		}
		if c.HiddenTypes != nil {
			rc.HiddenTypes = *c.HiddenTypes
		}
		if c.Files != nil {
			rc.Files = *c.Files
		}
		if c.MajorTag != nil {
			rc.MajorTag = *c.MajorTag
		}
		if c.ChangelogSource != nil {
			rc.ChangelogSource = *c.ChangelogSource
		}
		out = append(out, rc)
	}
	return out
}

// rawFile mirrors File, the yaml shape of one "files" entry.
type rawFile struct {
	Path    string  `yaml:"path"`
	Updater *string `yaml:"updater"`
	Key     *string `yaml:"key"`
}

// rawLabels mirrors Labels with pointer fields, so Load can tell a key left
// out of the "labels" block apart from one set to its zero value.
type rawLabels struct {
	Pending *string          `yaml:"pending"`
	Tagged  *string          `yaml:"tagged"`
	Alpha   *preReleaseLabel `yaml:"alpha"`
	Beta    *preReleaseLabel `yaml:"beta"`
	Rc      *preReleaseLabel `yaml:"rc"`
}

// preReleaseLabel is one of "labels.alpha", "labels.beta" and "labels.rc":
// a label name, or false for that pre-release type turned off, the same as
// "". Without it yaml decodes an unquoted false into the label name "false".
type preReleaseLabel string

// UnmarshalYAML accepts a string, or the boolean false as the empty name.
// true is rejected: it names no label and turns nothing off.
func (l *preReleaseLabel) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!bool" {
		var on bool
		if err := node.Decode(&on); err != nil {
			return err
		}
		if on {
			return fmt.Errorf("line %d: a pre-release label is a name, or false to turn it off, not true", node.Line)
		}
		*l = ""
		return nil
	}
	var name string
	if err := node.Decode(&name); err != nil {
		return err
	}
	*l = preReleaseLabel(name)
	return nil
}

// rawAuthor mirrors Author with pointer fields, so Load can tell a key left
// out of the "author" block apart from one set to its zero value.
type rawAuthor struct {
	Name  *string `yaml:"name"`
	Email *string `yaml:"email"`
}

// raw mirrors Config with pointer fields, so Load can tell a key left out of
// the file apart from one set to its zero value.
type raw struct {
	InitialVersion           *string    `yaml:"initial-version"`
	BumpMinorPreMajor        *bool      `yaml:"bump-minor-pre-major"`
	HiddenTypes              *[]string  `yaml:"hidden-types"`
	FallbackType             *string    `yaml:"fallback-type"`
	TokenEnv                 *string    `yaml:"token-env"`
	Files                    *[]rawFile `yaml:"files"`
	Changelog                *string    `yaml:"changelog"`
	ChangelogSource          *string    `yaml:"changelog-source"`
	Labels                   *rawLabels `yaml:"labels"`
	Author                   *rawAuthor `yaml:"author"`
	MajorTag                 *bool      `yaml:"major-tag"`
	Versioning               *string    `yaml:"versioning"`
	ReleasePullRequest       *string    `yaml:"release-pull-request"`
	ReleasePullRequestLayout *string    `yaml:"release-pull-request-layout"`
	ReleasePullRequestGuide  *bool      `yaml:"release-pull-request-guide"`
	DraftUntilAssetsAttach   *bool      `yaml:"draft-until-assets-attach"`
	CommentMention           *string    `yaml:"comment-mention"`
	// Components is decoded as a node, not a Go map, so decodeComponents
	// can read its entries back in release-me.yaml's order: a Go map
	// gives no such guarantee. A zero Kind means the "components" key was
	// left out: yaml.v3 only special cases decoding into a Node field by
	// value, never through a pointer.
	Components yaml.Node `yaml:"components"`
}

// rawComponent mirrors Component with pointer fields, the yaml shape of one
// "components" entry.
type rawComponent struct {
	Path              *string    `yaml:"path"`
	InitialVersion    *string    `yaml:"initial-version"`
	BumpMinorPreMajor *bool      `yaml:"bump-minor-pre-major"`
	HiddenTypes       *[]string  `yaml:"hidden-types"`
	Files             *[]rawFile `yaml:"files"`
	MajorTag          *bool      `yaml:"major-tag"`
	ChangelogSource   *string    `yaml:"changelog-source"`
	TagPrefix         *string    `yaml:"tag-prefix"`
	Changelog         *string    `yaml:"changelog"`
}

// Default is the configuration applied when release-me.yaml is absent.
func Default() Config {
	return Config{
		InitialVersion:  DefaultInitialVersion,
		HiddenTypes:     DefaultHiddenTypes,
		FallbackType:    DefaultFallbackType,
		TokenEnv:        DefaultTokenEnv,
		ChangelogPath:   DefaultChangelogPath,
		ChangelogSource: DefaultChangelogSource,
		Labels: Labels{
			Pending: DefaultPendingLabel, Tagged: DefaultTaggedLabel,
			Alpha: DefaultAlphaLabel, Beta: DefaultBetaLabel, Rc: DefaultRcLabel,
		},
		Author:                   Author{Name: DefaultAuthorName, Email: DefaultAuthorEmail},
		Versioning:               DefaultVersioning,
		ReleasePullRequest:       ReleasePullRequestPerComponent,
		ReleasePullRequestLayout: DefaultReleasePullRequestLayout,
		ReleasePullRequestGuide:  true,
		CommentMention:           DefaultCommentMention,
	}
}

// SharedPullRequest reports whether every component shares one release
// pull request, branch "release-me/main": always in VersioningLocked, and
// with "release-pull-request: shared" and a "components" key in the other
// modes. A repository without "components" has the one implicit "main"
// component, whose own release pull request already is the only one.
func (cfg Config) SharedPullRequest() bool {
	return cfg.Versioning == VersioningLocked ||
		(cfg.ReleasePullRequest == ReleasePullRequestShared && len(cfg.Components) > 0)
}

// PerComponentBranch returns the release branch a component named name
// carries with one release pull request per component, "release-me/<name>".
func PerComponentBranch(name string) string {
	return "release-me/" + name
}

// Load reads release-me.yaml at path. A missing file yields Default(). An
// unknown key is an error.
func Load(path string) (cfg Config, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("config: close %s: %w", path, cerr)
		}
	}()

	var r raw
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg = Default()
	if r.InitialVersion != nil {
		cfg.InitialVersion = *r.InitialVersion
	}
	if r.BumpMinorPreMajor != nil {
		cfg.BumpMinorPreMajor = *r.BumpMinorPreMajor
	}
	if r.HiddenTypes != nil {
		cfg.HiddenTypes = *r.HiddenTypes
	}
	if r.FallbackType != nil {
		cfg.FallbackType = *r.FallbackType
	}
	if bumpingTypes[cfg.FallbackType] {
		return Config{}, fmt.Errorf(
			"config: parse %s: fallback-type %q bumps the version, unparseable text must not",
			path, cfg.FallbackType,
		)
	}
	if r.TokenEnv != nil {
		cfg.TokenEnv = *r.TokenEnv
	}
	if r.Changelog != nil {
		cfg.ChangelogPath = *r.Changelog
	}
	if r.ChangelogSource != nil {
		if !slices.Contains(changelogSources, *r.ChangelogSource) {
			return Config{}, fmt.Errorf(
				"config: parse %s: changelog-source %q is none of %s",
				path, *r.ChangelogSource, strings.Join(changelogSources, ", "),
			)
		}
		cfg.ChangelogSource = *r.ChangelogSource
	}
	if r.Labels != nil {
		if r.Labels.Pending != nil {
			if *r.Labels.Pending == "" {
				return Config{}, fmt.Errorf("config: parse %s: labels.pending is empty", path)
			}
			cfg.Labels.Pending = *r.Labels.Pending
		}
		if r.Labels.Tagged != nil {
			if *r.Labels.Tagged == "" {
				return Config{}, fmt.Errorf("config: parse %s: labels.tagged is empty", path)
			}
			cfg.Labels.Tagged = *r.Labels.Tagged
		}
		if r.Labels.Alpha != nil {
			cfg.Labels.Alpha = string(*r.Labels.Alpha)
		}
		if r.Labels.Beta != nil {
			cfg.Labels.Beta = string(*r.Labels.Beta)
		}
		if r.Labels.Rc != nil {
			cfg.Labels.Rc = string(*r.Labels.Rc)
		}
		if err := validateLabels(path, cfg.Labels); err != nil {
			return Config{}, err
		}
	}
	if r.Author != nil {
		if r.Author.Name != nil {
			cfg.Author.Name = *r.Author.Name
		}
		if r.Author.Email != nil {
			cfg.Author.Email = *r.Author.Email
		}
	}
	if r.Files != nil {
		files, err := decodeFiles(path, *r.Files)
		if err != nil {
			return Config{}, err
		}
		cfg.Files = files
	}
	if r.MajorTag != nil {
		cfg.MajorTag = *r.MajorTag
	}
	if r.Versioning != nil {
		cfg.Versioning = Versioning(*r.Versioning)
		if !validVersioning[cfg.Versioning] {
			return Config{}, fmt.Errorf("config: parse %s: versioning %q is not independent, locked or linked", path, *r.Versioning)
		}
	}
	if cfg.Versioning == VersioningLocked {
		cfg.ReleasePullRequest = ReleasePullRequestShared
	}
	if r.ReleasePullRequest != nil {
		value := ReleasePullRequest(*r.ReleasePullRequest)
		switch {
		case value != ReleasePullRequestPerComponent && value != ReleasePullRequestShared:
			return Config{}, fmt.Errorf(
				"config: parse %s: release-pull-request %q is not per-component or shared", path, *r.ReleasePullRequest,
			)
		case value == ReleasePullRequestPerComponent && cfg.Versioning == VersioningLocked:
			return Config{}, fmt.Errorf(
				"config: parse %s: release-pull-request per-component does not apply to versioning locked, "+
					"which releases every component through one shared release pull request", path,
			)
		}
		cfg.ReleasePullRequest = value
	}
	if r.ReleasePullRequestLayout != nil {
		cfg.ReleasePullRequestLayout = ReleasePullRequestLayout(*r.ReleasePullRequestLayout)
		if !validLayout[cfg.ReleasePullRequestLayout] {
			return Config{}, fmt.Errorf(
				"config: parse %s: release-pull-request-layout %q is not headings, details or compact",
				path, *r.ReleasePullRequestLayout,
			)
		}
	}
	if r.ReleasePullRequestGuide != nil {
		cfg.ReleasePullRequestGuide = *r.ReleasePullRequestGuide
	}
	if r.DraftUntilAssetsAttach != nil {
		cfg.DraftUntilAssetsAttach = *r.DraftUntilAssetsAttach
	}
	if r.CommentMention != nil {
		if *r.CommentMention == "" {
			return Config{}, fmt.Errorf("config: parse %s: comment-mention is empty", path)
		}
		cfg.CommentMention = *r.CommentMention
	}
	if r.Components.Kind != 0 {
		components, err := decodeComponents(path, &r.Components)
		if err != nil {
			return Config{}, err
		}
		if err := validateComponentPaths(path, components); err != nil {
			return Config{}, err
		}
		if err := validateTagPrefixes(path, components); err != nil {
			return Config{}, err
		}
		if err := validateChangelogPaths(path, components); err != nil {
			return Config{}, err
		}
		cfg.Components = components
		if err := validateFileUpdates(path, cfg); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// decodeFiles converts the yaml shape of a "files" key into []File,
// defaulting a left-out updater to the marker updater and rejecting one
// that names no known updater, that is missing a key it requires, or that
// carries a key it takes none of.
func decodeFiles(path string, raw []rawFile) ([]File, error) {
	files := make([]File, 0, len(raw))
	for _, rf := range raw {
		name := updater.Marker
		if rf.Updater != nil {
			name = *rf.Updater
		}
		entry, ok := updater.Updaters[name]
		if !ok {
			return nil, fmt.Errorf("config: parse %s: file %s: unknown updater %q", path, rf.Path, name)
		}
		key := ""
		if rf.Key != nil {
			key = *rf.Key
		}
		if entry.NeedsKey && key == "" {
			return nil, fmt.Errorf("config: parse %s: file %s: updater %q requires a key", path, rf.Path, name)
		}
		if !entry.NeedsKey && key != "" {
			return nil, fmt.Errorf("config: parse %s: file %s: updater %q takes no key", path, rf.Path, name)
		}
		files = append(files, File{Path: rf.Path, Updater: name, Key: key})
	}
	return files, nil
}

// decodeComponents converts the "components" mapping node into []Component,
// in the order its keys appear in release-me.yaml. Each entry's value is
// decoded with the same strict, unknown-key-rejecting rules as the rest of
// release-me.yaml.
func decodeComponents(path string, node *yaml.Node) ([]Component, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config: parse %s: components: must be a mapping of name to settings", path)
	}

	components := make([]Component, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		valueNode := node.Content[i+1]
		if err := ValidateComponentName(name); err != nil {
			return nil, fmt.Errorf("config: parse %s: component %q: name %w", path, name, err)
		}

		data, err := yaml.Marshal(valueNode)
		if err != nil {
			return nil, fmt.Errorf("config: parse %s: component %s: %w", path, name, err)
		}
		var rc rawComponent
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&rc); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("config: parse %s: component %s: %w", path, name, err)
		}
		if rc.Path == nil {
			return nil, fmt.Errorf("config: parse %s: component %s: path is required", path, name)
		}
		if rc.TagPrefix != nil {
			if err := validateTagPrefix(*rc.TagPrefix); err != nil {
				return nil, fmt.Errorf("config: parse %s: component %s: tag-prefix %q %w", path, name, *rc.TagPrefix, err)
			}
		}
		if rc.Changelog != nil {
			if err := validateChangelogPath(*rc.Changelog); err != nil {
				return nil, fmt.Errorf("config: parse %s: component %s: changelog %q %w", path, name, *rc.Changelog, err)
			}
		}
		if rc.ChangelogSource != nil && !slices.Contains(changelogSources, *rc.ChangelogSource) {
			return nil, fmt.Errorf(
				"config: parse %s: component %s: changelog-source %q is none of %s",
				path, name, *rc.ChangelogSource, strings.Join(changelogSources, ", "),
			)
		}

		c := Component{
			Name: name, Path: *rc.Path, InitialVersion: rc.InitialVersion,
			BumpMinorPreMajor: rc.BumpMinorPreMajor, HiddenTypes: rc.HiddenTypes, MajorTag: rc.MajorTag,
			ChangelogSource: rc.ChangelogSource, TagPrefix: rc.TagPrefix, Changelog: rc.Changelog,
		}
		if rc.Files != nil {
			files, err := decodeFiles(path, *rc.Files)
			if err != nil {
				return nil, err
			}
			c.Files = &files
		}
		components = append(components, c)
	}
	return components, nil
}

// ValidateComponentName rejects a component name release-me cannot carry
// through every place it puts one: the derived tag prefix "<name>/v", the
// branch "release-me/<name>", the shared release pull request title
// "chore: release <name> vX.Y.Z, ...", which release reads back after the
// merge, the comma separated Release-Component footer and the changelog
// compare link. A name holds ASCII letters, digits, "-", "_" and "." only, and
// neither starts with "." or "-", ends with "." or ".lock", nor holds "..",
// which git refuses in a tag or branch name. migrate calls it to vet a name it derived.
func ValidateComponentName(name string) error {
	for _, r := range name {
		if !isComponentNameRune(r) {
			return fmt.Errorf(`contains %q: a component name holds ASCII letters, digits, "-", "_" and "." only`, string(r))
		}
	}
	switch {
	case name == "":
		return errors.New("is empty")
	case strings.HasPrefix(name, ".") || strings.HasPrefix(name, "-"):
		return errors.New(`starts with "." or "-", which git refuses at the start of a tag or branch name`)
	case strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock"):
		return errors.New(`ends with "." or ".lock", which git refuses in a tag or branch name`)
	case strings.Contains(name, ".."):
		return errors.New(`contains "..", which git refuses in a tag or branch name`)
	}
	return nil
}

// isComponentNameRune reports whether r may appear in a component name:
// an ASCII letter or digit, "-", "_" or ".".
func isComponentNameRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
}

// validateComponentPaths rejects two components whose paths overlap: equal,
// or one nested inside the other, including the root path ".", which
// overlaps with every other component.
func validateComponentPaths(path string, components []Component) error {
	for i := 0; i < len(components); i++ {
		for j := i + 1; j < len(components); j++ {
			a, b := components[i], components[j]
			if pathsOverlap(a.Path, b.Path) {
				return fmt.Errorf("config: parse %s: component %q and %q have overlapping paths %q and %q",
					path, a.Name, b.Name, a.Path, b.Path)
			}
		}
	}
	return nil
}

// tagPrefix returns the component's configured "tag-prefix", or the
// derived "<name>/v" when it carries none.
func (c Component) tagPrefix() string {
	if c.TagPrefix != nil {
		return *c.TagPrefix
	}
	return c.Name + "/v"
}

// validateTagPrefix rejects a "tag-prefix" that cannot start a git tag
// name, or that git would read as a pattern: LastTag lists a component's
// tags through "git tag --list <prefix>*", so a glob character in the
// prefix would match other tags than the component's own. The version is
// appended to the prefix as it is, so a trailing "/" is allowed. It also
// rejects "(", ")", "#" and "%", which git allows in a tag but which break
// the changelog heading's compare link: "#" starts the URL's fragment, "%"
// starts a percent escape, and a parenthesis ends the markdown link target
// early, so the heading would no longer read back.
func validateTagPrefix(prefix string) error {
	if i := strings.IndexAny(prefix, "()#%"); i >= 0 {
		return fmt.Errorf("contains %q, which breaks the changelog compare link", prefix[i:i+1])
	}
	switch {
	case prefix == "":
		return errors.New("is empty")
	case strings.ContainsAny(prefix, " \t\n~^:?*[\\"):
		return errors.New("contains whitespace or one of ~ ^ : ? * [ \\")
	case strings.Contains(prefix, "..") || strings.Contains(prefix, "@{") || strings.Contains(prefix, "//"):
		return errors.New(`contains "..", "@{" or "//"`)
	case strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "-") || strings.HasPrefix(prefix, "."):
		return errors.New(`starts with "/", "-" or "."`)
	case strings.Contains(prefix, "/."):
		return errors.New(`has a path segment starting with "."`)
	}
	return nil
}

// validateTagPrefixes rejects two components with the same tag prefix,
// configured or derived: both would tag the same name and read each
// other's releases as their own. A prefix that merely starts with another
// is fine, LastTag discards a tag whose remainder is no version.
func validateTagPrefixes(path string, components []Component) error {
	for i := 0; i < len(components); i++ {
		for j := i + 1; j < len(components); j++ {
			a, b := components[i], components[j]
			if pa, pb := a.tagPrefix(), b.tagPrefix(); pa == pb {
				return fmt.Errorf("config: parse %s: component %q and %q have the same tag prefix %q",
					path, a.Name, b.Name, pa)
			}
		}
	}
	return nil
}

// changelogPath returns the component's configured "changelog", cleaned,
// or the derived "<path>/CHANGELOG.md" when it carries none.
func (c Component) changelogPath() string {
	if c.Changelog != nil {
		return stdpath.Clean(*c.Changelog)
	}
	return stdpath.Join(c.Path, "CHANGELOG.md")
}

// validateChangelogPath rejects a "changelog" that is not a file inside the
// repository: pr writes it relative to the checkout, so an absolute path or
// one that climbs out through ".." would write outside it.
func validateChangelogPath(p string) error {
	clean := stdpath.Clean(p)
	switch {
	case p == "":
		return errors.New("is empty")
	case stdpath.IsAbs(p) || strings.Contains(p, "\\"):
		return errors.New("is not a relative path with forward slashes")
	case clean == "." || clean == ".." || strings.HasPrefix(clean, "../"):
		return errors.New("is not a file inside the repository")
	}
	return nil
}

// validateChangelogPaths rejects two components writing the same
// changelog, configured or derived: each prepends its own section to the
// file, so a locked release pull request would write it twice and two
// independent ones would conflict on it.
func validateChangelogPaths(path string, components []Component) error {
	for i := 0; i < len(components); i++ {
		for j := i + 1; j < len(components); j++ {
			a, b := components[i], components[j]
			if pa, pb := a.changelogPath(), b.changelogPath(); pa == pb {
				return fmt.Errorf("config: parse %s: component %q and %q write the same changelog %q",
					path, a.Name, b.Name, pa)
			}
		}
	}
	return nil
}

// validateFileUpdates rejects two components bumping the same version in
// one file: the same path with the same key, an empty key, as the marker
// and helm updaters carry, counting as equal. Their files are compared as
// resolved, so two components inheriting the top-level "files" collide
// too. Both updates would apply to the same value, so the component later
// in release-me.yaml would silently overwrite the other's version. Only
// VersioningIndependent is checked: in VersioningLocked and
// VersioningLinked every component a run releases carries the family's
// one next version, so both write the same value.
func validateFileUpdates(path string, cfg Config) error {
	if cfg.Versioning != VersioningIndependent {
		return nil
	}
	type target struct{ path, key string }
	owner := make(map[target]string)
	for _, comp := range cfg.Resolve() {
		seen := make(map[target]bool)
		for _, f := range comp.Files {
			t := target{path: stdpath.Clean(f.Path), key: f.Key}
			if seen[t] {
				continue
			}
			seen[t] = true
			if other, ok := owner[t]; ok {
				where := fmt.Sprintf("%q", t.path)
				if t.key != "" {
					where += fmt.Sprintf(" at key %q", t.key)
				}
				return fmt.Errorf("config: parse %s: component %q and %q bump the same version in %s, "+
					"one would overwrite the other's", path, other, comp.Name, where)
			}
			owner[t] = comp.Name
		}
	}
	return nil
}

// validateLabels rejects one label name configured for two roles: a pull
// request carrying it would select both, and the error release-me reports
// then names the same label twice and points at no fix. Turned-off
// pre-release types, configured empty, are not compared.
func validateLabels(path string, l Labels) error {
	roles := []struct{ key, name string }{
		{"pending", l.Pending}, {"tagged", l.Tagged}, {"alpha", l.Alpha}, {"beta", l.Beta}, {"rc", l.Rc},
	}
	for i := 0; i < len(roles); i++ {
		for j := i + 1; j < len(roles); j++ {
			if roles[i].name != "" && roles[i].name == roles[j].name {
				return fmt.Errorf("config: parse %s: labels.%s and labels.%s are both %q",
					path, roles[i].key, roles[j].key, roles[i].name)
			}
		}
	}
	return nil
}

// pathsOverlap reports whether a and b name the same component path, or one
// is nested inside the other. "." is the repository root and overlaps with
// every path.
func pathsOverlap(a, b string) bool {
	return pathContains(a, b) || pathContains(b, a)
}

// pathContains reports whether child is parent itself or nested under it.
// parent "." contains every path.
func pathContains(parent, child string) bool {
	if parent == "." {
		return true
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
}
