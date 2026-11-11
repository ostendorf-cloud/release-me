// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package config loads release-me.yaml: initial-version, release-as,
// versioning-strategy, bump-minor-pre-major, bump-patch-for-minor-pre-major,
// changelog-path, changelog-source, changelog-type, skip-changelog,
// hidden-types, fallback-type, changelog-sections, changelog-sentence-case,
// changelog-authors, changelog-commit-links, changelog-issue-references,
// changelog-host, changelog-date-format, files, release-pull-request,
// release-pull-request-layout, release-pull-request-guide,
// release-pull-request-title, release-pull-request-shared-title,
// release-pull-request-draft, release-pull-request-header,
// release-pull-request-footer, author, signoff, major-tag,
// include-v-in-release-name, skip-github-release, github-release-draft,
// github-release-draft-until-assets-attach, force-tag-creation,
// delete-release-branch, labels, comment-mention, versioning, components,
// exclude-paths, workspaces, always-link-local and token-env.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	stdpath "path"
	"regexp"
	"slices"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/conventional"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
	"github.com/ostendorf-cloud/release-me/internal/updater"
	"github.com/ostendorf-cloud/release-me/internal/version"
	"github.com/ostendorf-cloud/release-me/internal/workspace"
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
	// "changelog-path" key is left out of release-me.yaml.
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

// The changelog types "changelog-type" accepts, top level and per
// component, release-please's own.
const (
	// ChangelogTypeDefault renders the changelog from the entries
	// changelog-source reads, the default.
	ChangelogTypeDefault = "default"
	// ChangelogTypeGitHub renders it from the release notes GitHub
	// generates for the release.
	ChangelogTypeGitHub = "github"
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

// ChangelogStyle is the top-level keys that shape how a changelog line
// renders, the same for every component.
type ChangelogStyle struct {
	// SentenceCase is "changelog-sentence-case", default false: upper case
	// the first letter of every entry's description.
	SentenceCase bool
	// Authors is "changelog-authors", default false: credit each entry's
	// author after its description.
	Authors bool
	// CommitLinks is "changelog-commit-links", default false: link each
	// entry's commit.
	CommitLinks bool
	// IssueReferences is "changelog-issue-references", default false:
	// link the issues each entry closes.
	IssueReferences bool
	// Host is "changelog-host", "" by default: the web address every
	// changelog link points at instead of the forge's own, such as a
	// GitHub Enterprise Server's "https://github.example.com". No
	// trailing slash.
	Host string
	// DateFormat is "changelog-date-format", "" by default for
	// "%Y-%m-%d": the strftime expression a version heading's date
	// renders in.
	DateFormat string
}

// ChangelogSection is one entry of the "changelog-sections" key: the entry
// type it is about, the section heading that type renders under, and
// whether it is hidden instead. Section may be "" for a hidden type and
// is ignored whenever Hidden is true.
type ChangelogSection struct {
	Type    string
	Section string
	Hidden  bool
}

// Labels is the "labels" key: the label names release-me expects on the
// repository. PreRelease selects a pre-release type on the release pull
// request, each entry's label naming it; Pending and Tagged apply to every
// release, stable or not. Pending and Tagged are each one name or a list of
// them, never empty: a release pull request is pending while it carries
// every pending label, and releasing it swaps all of them for every tagged
// label. Extra is the labels pr adds to a release pull request it opens
// beside the pending ones, read by nothing. A PreRelease entry with an
// empty Label turns that pre-release type off: release-me neither expects
// the label on the repository nor reads it from a pull request.
type Labels struct {
	Pending []string
	Tagged  []string
	Extra   []string
	// PreRelease is the "labels.prerelease" key: every pre-release type
	// release-me selects through a label on the release pull request, in
	// order, alpha, beta and rc by default, each further type
	// release-me.yaml adds appended after them, in the order it declares
	// them.
	PreRelease []PreReleaseType
}

// PreReleaseType is one pre-release type and the label that selects it on
// the release pull request: alpha, beta and rc by default, or a further
// type "labels.prerelease" adds. An empty Label turns the type off.
type PreReleaseType struct {
	Type  string
	Label string
}

// PreReleaseLabels returns the label of every enabled pre-release type, in
// Labels.PreRelease's order, a type turned off, with an empty label,
// carrying none. It is the order a pull request carrying more than one of
// the labels is reported in.
func (l Labels) PreReleaseLabels() []string {
	names := make([]string, 0, len(l.PreRelease))
	for _, p := range l.PreRelease {
		if p.Label != "" {
			names = append(names, p.Label)
		}
	}
	return names
}

// Opened returns the labels pr opens a release pull request with: every
// pending label, then every extra one.
func (l Labels) Opened() []string {
	return append(slices.Clone(l.Pending), l.Extra...)
}

// Author is the "author" key, still accepted so an existing configuration
// keeps loading, but no longer applied: pr creates the release commit
// through the forge, which signs it only when no author or committer is
// passed, so the commit carries the token's own identity instead. check
// warns when either field is set, which Set records.
type Author struct {
	Name  string
	Email string
	// Set names every key of the "author" block release-me.yaml sets,
	// "author.name" and "author.email", in that order, nil when the
	// block is left out: the defaults alone are never reported as set.
	Set []string
}

// Config is the release-me.yaml configuration.
type Config struct {
	InitialVersion string
	// ReleaseAs is the top-level "release-as" key, "" when it is left out:
	// the version every component's next release is forced to while it is
	// greater than that component's last version. A component overrides
	// it through its own "release-as" key, in VersioningIndependent only.
	ReleaseAs string
	// VersioningStrategy is the top-level "versioning-strategy" key: how
	// a release bumps the version, "" when the key is left out, which
	// version.Next reads as version.StrategyDefault. A component overrides
	// it through its own "versioning-strategy" key.
	VersioningStrategy        version.Strategy
	BumpMinorPreMajor         bool
	BumpPatchForMinorPreMajor bool

	ChangelogPath string
	// ChangelogSource is the top-level "changelog-source" key: which
	// commits become changelog entries. See the ChangelogSource*
	// constants. A component overrides it through its own
	// "changelog-source" key.
	ChangelogSource string
	// ChangelogType is the top-level "changelog-type" key, one of the
	// ChangelogType* constants, "" when left out, which is
	// ChangelogTypeDefault. A component overrides it through its own
	// "changelog-type" key.
	ChangelogType string
	// SkipChangelog is the top-level "skip-changelog" key, default false:
	// when true, pr leaves the changelog file alone, the release pull
	// request and the release still carry the changelog. A component
	// overrides it through its own "skip-changelog" key.
	SkipChangelog bool
	HiddenTypes   []string
	FallbackType  string
	// ChangelogSections is the top-level "changelog-sections" key, nil
	// when it was left out and the default sections apply. A component
	// overrides it through its own "changelog-sections" key.
	ChangelogSections []ChangelogSection
	// ChangelogStyle is the top-level keys that shape every changelog
	// line. There is no per-component override: every component a shared
	// release pull request carries renders alike.
	ChangelogStyle ChangelogStyle

	Files []File

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
	// guide. false leaves the guide out and disables nothing it advertises:
	// the release-me-notes block, the release-me-version block, the
	// pre-release labels and both comment commands keep working, they only
	// stop being advertised in the body. Nothing reads the body back by the
	// guide, so the key can change while a release pull request is open.
	ReleasePullRequestGuide bool
	// ReleasePullRequestTitle is the top-level "release-pull-request-title"
	// key: the title pattern of every component's own release pull
	// request, "" for releasepr.DefaultTitlePattern. Load checks it with
	// releasepr.NewTitlePattern, so it always carries ${version}.
	ReleasePullRequestTitle string
	// ReleasePullRequestSharedTitle is the top-level
	// "release-pull-request-shared-title" key: the title pattern of the
	// release pull request every component shares, "" for
	// releasepr.DefaultLockedTitlePattern in VersioningLocked and
	// releasepr.DefaultSharedTitlePattern otherwise. Load checks it for the
	// configured versioning mode.
	ReleasePullRequestSharedTitle string
	// ReleasePullRequestDraft is the top-level "release-pull-request-draft"
	// key, default false: when true, pr opens every release pull request as
	// a draft. It never touches one already open.
	ReleasePullRequestDraft bool
	// ReleasePullRequestHeader is the top-level "release-pull-request-header"
	// key: free text above the version line of every release pull request
	// body, "" for none. Nothing reads it back.
	ReleasePullRequestHeader string
	// ReleasePullRequestFooter is the top-level "release-pull-request-footer"
	// key: free text below the changelog of every release pull request
	// body, "" for none. Nothing reads it back.
	ReleasePullRequestFooter string

	Author Author
	// Signoff is the top-level "signoff" key: "Name <email>" the release
	// commit's message carries a "Signed-off-by:" trailer for, "" for none.
	Signoff string

	// MajorTag is the top-level "major-tag" key: when true, a stable
	// release moves the major tag "vX" to the release commit. A component
	// overrides it through its own "major-tag" key.
	MajorTag bool
	// IncludeVInReleaseName is the top-level "include-v-in-release-name"
	// key, default true: the GitHub release is named "vX.Y.Z", or
	// "<name> vX.Y.Z" for a configured component. False drops the "v",
	// "X.Y.Z" and "<name> X.Y.Z". The tag keeps its prefix either way. A
	// component overrides it through its own "include-v-in-release-name"
	// key.
	IncludeVInReleaseName bool
	// SkipGitHubRelease is the top-level "skip-github-release" key,
	// default false: when true, release tags a merged release pull request
	// and swaps its label but creates no GitHub release, for a repository
	// whose own workflow creates the release from the tag. A component
	// overrides it through its own "skip-github-release" key.
	SkipGitHubRelease bool
	// GitHubReleaseDraft is the top-level "github-release-draft" key,
	// default false: when true, release creates the GitHub release as a
	// draft and leaves it one, for a human to publish. Unlike
	// GitHubReleaseDraftUntilAssetsAttach it never resumes a draft on a
	// later run. A component overrides it through its own
	// "github-release-draft" key.
	GitHubReleaseDraft bool
	// GitHubReleaseDraftUntilAssetsAttach is the top-level
	// "github-release-draft-until-assets-attach" key, default false: when
	// true, release creates every release as a draft instead of publishing
	// it immediately, and resumes publishing one left a draft by an
	// earlier, interrupted run. Only a repository with GitHub's immutable
	// releases setting turned on needs this; it applies to the whole
	// repository, there is no per-component override. See
	// docs/adr/0001-draft-releases-until-assets-attach.md.
	GitHubReleaseDraftUntilAssetsAttach bool
	// ForceTagCreation is the top-level "force-tag-creation" key, default
	// true: release creates the tag itself, before the release, even for
	// a draft. False leaves the tag to GitHub, which creates it from the
	// release's target commit once the release is published, immediately
	// for a published release, on publish for a draft. A component
	// overrides it through its own "force-tag-creation" key.
	ForceTagCreation bool
	// DeleteReleaseBranch is the top-level "delete-release-branch" key,
	// default true: release deletes a release branch once every pull
	// request it released from it this run is tagged and released, the
	// branch still points at the merged head and no open pull request
	// uses it. pr pushes the branch afresh for the next release, so
	// nothing is lost; false keeps it, as release-me did before the key
	// existed. There is no per-component override.
	DeleteReleaseBranch bool

	Labels Labels
	// CommentMention is the top-level "comment-mention" key: the mention
	// prefix a write-access commenter's "release-as" or "release-notes"
	// command on the release pull request must start with, default
	// DefaultCommentMention. There is no per-component override.
	CommentMention string

	// Versioning is the top-level "versioning" key: how the configured
	// components' versions relate to each other, default
	// VersioningIndependent. It applies to the whole family; there is no
	// per-component override.
	Versioning Versioning
	// Components is the "components" key, in the order release-me.yaml
	// declares them, or nil when the key is absent: a repository without
	// it is one component at the repository root, as Resolve reports.
	Components []Component
	// ExcludePaths is the top-level "exclude-paths" key: paths, relative to
	// the repository root and cleaned, whose changes never count for the
	// implicit main component. Only a repository without "components"
	// carries it; a configured component has its own "exclude-paths".
	ExcludePaths []string
	// Workspaces is the top-level "workspaces" key: the package managers,
	// workspace.Managers, whose manifests link components to each other. A
	// component released this run moves every other component's dependency
	// on it to its new version and releases that dependent too. Empty
	// unless configured, which needs "components" and a shared release pull
	// request.
	Workspaces []string
	// AlwaysLinkLocal is the top-level "always-link-local" key, default
	// true: every node dependency on a local package name links the two
	// components. false links one only where it resolves to the local
	// package anyway, through a local protocol or a range the local
	// version satisfies, see workspace.Options.
	AlwaysLinkLocal bool

	TokenEnv string
}

// Component is one entry of the "components" key: a name mapped to a path
// and optional overrides of the top-level initial-version,
// bump-minor-pre-major, bump-patch-for-minor-pre-major, release-as,
// versioning-strategy, hidden-types, files, major-tag, changelog-source,
// changelog-type, skip-changelog, force-tag-creation,
// include-v-in-release-name, skip-github-release and github-release-draft,
// plus its own tag-prefix, changelog path and exclude-paths. A nil
// field was left out of release-me.yaml and falls back to the top-level
// value, the same left-out-versus-zero-value distinction Load applies to
// the top level itself.
type Component struct {
	Name string
	Path string
	// TagPrefix is the component's "tag-prefix" key, nil when it was left
	// out and the component keeps the derived "<name>/v".
	TagPrefix *string

	InitialVersion            *string
	ReleaseAs                 *string
	VersioningStrategy        *version.Strategy
	BumpMinorPreMajor         *bool
	BumpPatchForMinorPreMajor *bool

	// ChangelogPath is the component's "changelog-path" key, nil when it was
	// left out and the component keeps the derived "<path>/CHANGELOG.md".
	ChangelogPath     *string
	ChangelogSource   *string
	ChangelogType     *string
	SkipChangelog     *bool
	HiddenTypes       *[]string
	ChangelogSections *[]ChangelogSection

	Files *[]File

	MajorTag *bool
	// IncludeVInReleaseName is the component's
	// "include-v-in-release-name" key.
	IncludeVInReleaseName *bool
	SkipGitHubRelease     *bool
	// GitHubReleaseDraft is the component's "github-release-draft" key.
	GitHubReleaseDraft *bool
	ForceTagCreation   *bool

	// ExcludePaths is the component's "exclude-paths" key: paths inside
	// Path, relative to the repository root and cleaned, whose changes never
	// count for the component. There is no top-level fallback, a path names
	// one component's files.
	ExcludePaths []string
}

// ResolvedComponent is one component ready for a command to release: its
// name and path, the tag prefix configured or derived from the name, the
// release branch derived from it, the changelog path configured or derived
// from the path, and its settings with the top-level ones applied where it
// carries no override of its own.
type ResolvedComponent struct {
	Name      string
	Path      string
	TagPrefix string
	Branch    string
	// Multi is true when release-me.yaml declares a "components" key. It
	// is false for the single implicit "main" component, which keeps
	// today's tag prefix "v" and release name instead of the generic
	// "<name>/v" prefix and "<name> vX.Y.Z" release name a configured
	// component carries, with the "v" dropped when
	// include-v-in-release-name is false.
	Multi bool

	InitialVersion            string
	ReleaseAs                 string
	VersioningStrategy        version.Strategy
	BumpMinorPreMajor         bool
	BumpPatchForMinorPreMajor bool

	ChangelogPath string
	// ChangelogSource is one of the ChangelogSource* constants: which
	// commits become this component's changelog entries.
	ChangelogSource string
	// ChangelogType is one of the ChangelogType* constants, or "" for
	// ChangelogTypeDefault: where this component's changelog comes from.
	ChangelogType string
	// SkipChangelog is true when pr leaves this component's changelog
	// file alone.
	SkipChangelog bool
	HiddenTypes   []string
	// ChangelogSections is the "changelog-sections" this component's
	// changelog renders with, nil for the default sections.
	ChangelogSections []ChangelogSection
	// ChangelogStyle is the top level's, the same for every component.
	ChangelogStyle ChangelogStyle

	Files []File

	MajorTag bool
	// IncludeVInReleaseName is true when this component's release name
	// carries the "v" before the version.
	IncludeVInReleaseName bool
	// SkipGitHubRelease is true when release tags this component without
	// creating a GitHub release for the tag.
	SkipGitHubRelease bool
	// GitHubReleaseDraft is true when release creates this component's
	// GitHub release as a draft and leaves it one.
	GitHubReleaseDraft bool
	// ForceTagCreation is true when release creates this component's tag
	// itself, false when it leaves the tag to GitHub's release publish.
	ForceTagCreation bool

	// ExcludePaths are the paths inside Path whose changes never count for
	// the component, see history.Counts.
	ExcludePaths []string
	// OtherPaths are, for a root component beside others, every other
	// component's path, in release-me.yaml's order: the root component owns
	// every file outside them, so a change inside them does not count for
	// it, see history.Counts. Empty for every other component.
	OtherPaths []string
}

// Resolve returns the components a command should loop over, in
// release-me.yaml's order: the configured "components", each merged with
// the top-level settings it does not override, or the single implicit
// "main" component at the repository root when the "components" key is
// absent.
func (cfg Config) Resolve() []ResolvedComponent {
	if len(cfg.Components) == 0 {
		return []ResolvedComponent{{
			Name:                      "main",
			Path:                      ".",
			TagPrefix:                 "v",
			Branch:                    "release-me/main",
			InitialVersion:            cfg.InitialVersion,
			ReleaseAs:                 cfg.ReleaseAs,
			VersioningStrategy:        cfg.VersioningStrategy,
			BumpMinorPreMajor:         cfg.BumpMinorPreMajor,
			BumpPatchForMinorPreMajor: cfg.BumpPatchForMinorPreMajor,
			ChangelogPath:             cfg.ChangelogPath,
			ChangelogSource:           cfg.ChangelogSource,
			ChangelogType:             cfg.ChangelogType,
			SkipChangelog:             cfg.SkipChangelog,
			HiddenTypes:               cfg.HiddenTypes,
			ChangelogSections:         cfg.ChangelogSections,
			ChangelogStyle:            cfg.ChangelogStyle,
			Files:                     cfg.Files,
			MajorTag:                  cfg.MajorTag,
			IncludeVInReleaseName:     cfg.IncludeVInReleaseName,
			SkipGitHubRelease:         cfg.SkipGitHubRelease,
			GitHubReleaseDraft:        cfg.GitHubReleaseDraft,
			ForceTagCreation:          cfg.ForceTagCreation,
			ExcludePaths:              cfg.ExcludePaths,
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
			Name:                      c.Name,
			Path:                      c.Path,
			TagPrefix:                 c.tagPrefix(),
			Branch:                    branch(c.Name),
			Multi:                     true,
			InitialVersion:            cfg.InitialVersion,
			ReleaseAs:                 cfg.ReleaseAs,
			VersioningStrategy:        cfg.VersioningStrategy,
			BumpMinorPreMajor:         cfg.BumpMinorPreMajor,
			BumpPatchForMinorPreMajor: cfg.BumpPatchForMinorPreMajor,
			ChangelogPath:             c.changelogPath(),
			ChangelogSource:           cfg.ChangelogSource,
			ChangelogType:             cfg.ChangelogType,
			SkipChangelog:             cfg.SkipChangelog,
			HiddenTypes:               cfg.HiddenTypes,
			ChangelogSections:         cfg.ChangelogSections,
			ChangelogStyle:            cfg.ChangelogStyle,
			Files:                     cfg.Files,
			MajorTag:                  cfg.MajorTag,
			IncludeVInReleaseName:     cfg.IncludeVInReleaseName,
			SkipGitHubRelease:         cfg.SkipGitHubRelease,
			GitHubReleaseDraft:        cfg.GitHubReleaseDraft,
			ForceTagCreation:          cfg.ForceTagCreation,
			ExcludePaths:              c.ExcludePaths,
		}
		if c.InitialVersion != nil {
			rc.InitialVersion = *c.InitialVersion
		}
		if c.ReleaseAs != nil {
			rc.ReleaseAs = *c.ReleaseAs
		}
		if c.VersioningStrategy != nil {
			rc.VersioningStrategy = *c.VersioningStrategy
		}
		if c.BumpMinorPreMajor != nil {
			rc.BumpMinorPreMajor = *c.BumpMinorPreMajor
		}
		if c.BumpPatchForMinorPreMajor != nil {
			rc.BumpPatchForMinorPreMajor = *c.BumpPatchForMinorPreMajor
		}
		if c.ChangelogSource != nil {
			rc.ChangelogSource = *c.ChangelogSource
		}
		if c.ChangelogType != nil {
			rc.ChangelogType = *c.ChangelogType
		}
		if c.SkipChangelog != nil {
			rc.SkipChangelog = *c.SkipChangelog
		}
		if c.HiddenTypes != nil {
			rc.HiddenTypes = *c.HiddenTypes
		}
		if c.ChangelogSections != nil {
			rc.ChangelogSections = *c.ChangelogSections
		}
		if c.Files != nil {
			rc.Files = *c.Files
		}
		if c.MajorTag != nil {
			rc.MajorTag = *c.MajorTag
		}
		if c.IncludeVInReleaseName != nil {
			rc.IncludeVInReleaseName = *c.IncludeVInReleaseName
		}
		if c.SkipGitHubRelease != nil {
			rc.SkipGitHubRelease = *c.SkipGitHubRelease
		}
		if c.GitHubReleaseDraft != nil {
			rc.GitHubReleaseDraft = *c.GitHubReleaseDraft
		}
		if c.ForceTagCreation != nil {
			rc.ForceTagCreation = *c.ForceTagCreation
		}
		if c.Path == "." {
			for _, other := range cfg.Components {
				if other.Path != "." {
					rc.OtherPaths = append(rc.OtherPaths, other.Path)
				}
			}
		}
		out = append(out, rc)
	}
	return out
}

// Rules returns the component's bump rules, for version.Next.
func (rc ResolvedComponent) Rules() version.Rules {
	return version.Rules{
		InitialVersion:            rc.InitialVersion,
		BumpMinorPreMajor:         rc.BumpMinorPreMajor,
		BumpPatchForMinorPreMajor: rc.BumpPatchForMinorPreMajor,
		ReleaseAs:                 rc.ReleaseAs,
		Strategy:                  rc.VersioningStrategy,
	}
}

// Rules returns the top-level bump rules, for version.Next: the rules a
// linked or locked family computes its one shared version with.
func (cfg Config) Rules() version.Rules {
	return version.Rules{
		InitialVersion:            cfg.InitialVersion,
		BumpMinorPreMajor:         cfg.BumpMinorPreMajor,
		BumpPatchForMinorPreMajor: cfg.BumpPatchForMinorPreMajor,
		ReleaseAs:                 cfg.ReleaseAs,
		Strategy:                  cfg.VersioningStrategy,
	}
}

// rawFile mirrors File, the yaml shape of one "files" entry.
type rawFile struct {
	Path    string  `yaml:"path"`
	Updater *string `yaml:"updater"`
	Key     *string `yaml:"key"`
}

// rawChangelogSection mirrors ChangelogSection, the yaml shape of one
// "changelog-sections" entry, release-please's own shape.
type rawChangelogSection struct {
	Type    *string `yaml:"type"`
	Section *string `yaml:"section"`
	Hidden  *bool   `yaml:"hidden"`
}

// rawLabels mirrors Labels with pointer fields, so Load can tell a key left
// out of the "labels" block apart from one set to its zero value.
type rawLabels struct {
	Pending *labelList `yaml:"pending"`
	Tagged  *labelList `yaml:"tagged"`
	Extra   *labelList `yaml:"extra"`
	// PreRelease is decoded as a node, like Components, so
	// applyPreReleaseTypes can read its entries back in release-me.yaml's
	// order: a Go map gives no such guarantee. A zero Kind means the
	// "labels.prerelease" key was left out.
	PreRelease yaml.Node `yaml:"prerelease"`
}

// labelList is "labels.pending", "labels.tagged" or "labels.extra": one
// label name, the form every release-me.yaml before lists existed carries,
// or a list of them.
type labelList []string

// UnmarshalYAML accepts a string or a list of strings. An empty name is
// rejected wherever it appears.
func (l *labelList) UnmarshalYAML(node *yaml.Node) error {
	var names []string
	if node.Kind == yaml.SequenceNode {
		if err := node.Decode(&names); err != nil {
			return err
		}
	} else {
		var name string
		if err := node.Decode(&name); err != nil {
			return err
		}
		names = []string{name}
	}
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			return fmt.Errorf("line %d: a label name is empty", node.Line)
		}
	}
	*l = names
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
	InitialVersion            *string `yaml:"initial-version"`
	ReleaseAs                 *string `yaml:"release-as"`
	VersioningStrategy        *string `yaml:"versioning-strategy"`
	BumpMinorPreMajor         *bool   `yaml:"bump-minor-pre-major"`
	BumpPatchForMinorPreMajor *bool   `yaml:"bump-patch-for-minor-pre-major"`

	ChangelogPath         *string                `yaml:"changelog-path"`
	ChangelogSource       *string                `yaml:"changelog-source"`
	ChangelogType         *string                `yaml:"changelog-type"`
	SkipChangelog         *bool                  `yaml:"skip-changelog"`
	HiddenTypes           *[]string              `yaml:"hidden-types"`
	FallbackType          *string                `yaml:"fallback-type"`
	ChangelogSections     *[]rawChangelogSection `yaml:"changelog-sections"`
	ChangelogSentenceCase *bool                  `yaml:"changelog-sentence-case"`
	ChangelogAuthors      *bool                  `yaml:"changelog-authors"`
	ChangelogCommitLinks  *bool                  `yaml:"changelog-commit-links"`
	ChangelogIssueRefs    *bool                  `yaml:"changelog-issue-references"`
	ChangelogHost         *string                `yaml:"changelog-host"`
	ChangelogDateFormat   *string                `yaml:"changelog-date-format"`

	Files *[]rawFile `yaml:"files"`

	ReleasePullRequest            *string `yaml:"release-pull-request"`
	ReleasePullRequestLayout      *string `yaml:"release-pull-request-layout"`
	ReleasePullRequestGuide       *bool   `yaml:"release-pull-request-guide"`
	ReleasePullRequestTitle       *string `yaml:"release-pull-request-title"`
	ReleasePullRequestSharedTitle *string `yaml:"release-pull-request-shared-title"`
	ReleasePullRequestDraft       *bool   `yaml:"release-pull-request-draft"`
	ReleasePullRequestHeader      *string `yaml:"release-pull-request-header"`
	ReleasePullRequestFooter      *string `yaml:"release-pull-request-footer"`

	Author  *rawAuthor `yaml:"author"`
	Signoff *string    `yaml:"signoff"`

	MajorTag                            *bool `yaml:"major-tag"`
	IncludeVInReleaseName               *bool `yaml:"include-v-in-release-name"`
	SkipGitHubRelease                   *bool `yaml:"skip-github-release"`
	GitHubReleaseDraft                  *bool `yaml:"github-release-draft"`
	GitHubReleaseDraftUntilAssetsAttach *bool `yaml:"github-release-draft-until-assets-attach"`
	ForceTagCreation                    *bool `yaml:"force-tag-creation"`
	DeleteReleaseBranch                 *bool `yaml:"delete-release-branch"`

	Labels         *rawLabels `yaml:"labels"`
	CommentMention *string    `yaml:"comment-mention"`

	Versioning      *string   `yaml:"versioning"`
	ExcludePaths    *[]string `yaml:"exclude-paths"`
	Workspaces      *[]string `yaml:"workspaces"`
	AlwaysLinkLocal *bool     `yaml:"always-link-local"`

	TokenEnv *string `yaml:"token-env"`

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
	Path      *string `yaml:"path"`
	TagPrefix *string `yaml:"tag-prefix"`

	InitialVersion            *string `yaml:"initial-version"`
	ReleaseAs                 *string `yaml:"release-as"`
	VersioningStrategy        *string `yaml:"versioning-strategy"`
	BumpMinorPreMajor         *bool   `yaml:"bump-minor-pre-major"`
	BumpPatchForMinorPreMajor *bool   `yaml:"bump-patch-for-minor-pre-major"`

	ChangelogPath     *string                `yaml:"changelog-path"`
	ChangelogSource   *string                `yaml:"changelog-source"`
	ChangelogType     *string                `yaml:"changelog-type"`
	SkipChangelog     *bool                  `yaml:"skip-changelog"`
	HiddenTypes       *[]string              `yaml:"hidden-types"`
	ChangelogSections *[]rawChangelogSection `yaml:"changelog-sections"`

	Files *[]rawFile `yaml:"files"`

	MajorTag              *bool `yaml:"major-tag"`
	IncludeVInReleaseName *bool `yaml:"include-v-in-release-name"`
	SkipGitHubRelease     *bool `yaml:"skip-github-release"`
	GitHubReleaseDraft    *bool `yaml:"github-release-draft"`
	ForceTagCreation      *bool `yaml:"force-tag-creation"`

	ExcludePaths *[]string `yaml:"exclude-paths"`
}

// Default is the configuration applied when release-me.yaml is absent.
func Default() Config {
	return Config{
		InitialVersion: DefaultInitialVersion,

		ChangelogPath:   DefaultChangelogPath,
		ChangelogSource: DefaultChangelogSource,
		HiddenTypes:     DefaultHiddenTypes,
		FallbackType:    DefaultFallbackType,

		ReleasePullRequest:       ReleasePullRequestPerComponent,
		ReleasePullRequestLayout: DefaultReleasePullRequestLayout,
		ReleasePullRequestGuide:  true,

		Author: Author{Name: DefaultAuthorName, Email: DefaultAuthorEmail},

		IncludeVInReleaseName: true,
		ForceTagCreation:      true,
		DeleteReleaseBranch:   true,

		Labels: Labels{
			Pending: []string{DefaultPendingLabel}, Tagged: []string{DefaultTaggedLabel},
			PreRelease: []PreReleaseType{
				{Type: "alpha", Label: DefaultAlphaLabel},
				{Type: "beta", Label: DefaultBetaLabel},
				{Type: "rc", Label: DefaultRcLabel},
			},
		},
		CommentMention: DefaultCommentMention,

		Versioning:      DefaultVersioning,
		AlwaysLinkLocal: true,

		TokenEnv: DefaultTokenEnv,
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

// CheckTitlePattern refuses a title pattern release cannot read back, see
// releasepr.NewTitlePattern, and one whose title reads as a conventional
// commit that bumps the version: the title is the release commit's subject
// and the merged pull request's entry, so a component whose history picks
// up another component's release commit, through a file outside that
// component's path, would release again for it.
func CheckTitlePattern(pattern string, kind releasepr.TitleKind) error {
	p, err := releasepr.NewTitlePattern(pattern, kind)
	if err != nil {
		return err
	}
	sample, err := p.Render(releasepr.TitleValues{
		Version: "1.2.3", Component: "api", Components: []string{"api", "web"},
		Releases: []releasepr.TitleRelease{{Name: "api", Version: "1.2.3"}}, Branch: "main",
	})
	if err != nil {
		return err
	}
	if msg := conventional.Parse(sample, DefaultFallbackType); bumpingTypes[msg.Type] || msg.Breaking {
		return fmt.Errorf("renders %q, a conventional %s that bumps the version, keep the release commit a type that does not", sample, msg.Type)
	}
	return nil
}

// SharedTitleKind returns the kind of title the release pull request every
// component shares carries: one version for the whole family in
// VersioningLocked, one version per component otherwise.
func (cfg Config) SharedTitleKind() releasepr.TitleKind {
	if cfg.Versioning == VersioningLocked {
		return releasepr.TitleLocked
	}
	return releasepr.TitleShared
}

// TitlePatterns returns the patterns every component's own release pull
// request title renders with, the first, and is read back with, in order:
// the configured "release-pull-request-title", then the default.
func (cfg Config) TitlePatterns() []releasepr.TitlePattern {
	patterns, err := releasepr.TitlePatterns(cfg.ReleasePullRequestTitle, releasepr.TitlePerComponent)
	if err != nil {
		// Load already refused a pattern that does not compile.
		return []releasepr.TitlePattern{releasepr.DefaultTitle(releasepr.TitlePerComponent)}
	}
	return patterns
}

// SharedTitlePatterns returns the patterns the release pull request every
// component shares renders its title with, the first, and reads it back
// with, in order: the configured "release-pull-request-shared-title", then
// the default for SharedTitleKind.
func (cfg Config) SharedTitlePatterns() []releasepr.TitlePattern {
	patterns, err := releasepr.TitlePatterns(cfg.ReleasePullRequestSharedTitle, cfg.SharedTitleKind())
	if err != nil {
		return []releasepr.TitlePattern{releasepr.DefaultTitle(cfg.SharedTitleKind())}
	}
	return patterns
}

// Frame returns the header and footer every release pull request body
// carries.
func (cfg Config) Frame() releasepr.Frame {
	return releasepr.Frame{Header: cfg.ReleasePullRequestHeader, Footer: cfg.ReleasePullRequestFooter}
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
	if r.ReleaseAs != nil {
		if err := validateReleaseAs(*r.ReleaseAs); err != nil {
			return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
		}
		cfg.ReleaseAs = *r.ReleaseAs
	}
	if r.VersioningStrategy != nil {
		strategy, err := parseStrategy(*r.VersioningStrategy)
		if err != nil {
			return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
		}
		cfg.VersioningStrategy = strategy
	}
	if r.BumpMinorPreMajor != nil {
		cfg.BumpMinorPreMajor = *r.BumpMinorPreMajor
	}
	if r.BumpPatchForMinorPreMajor != nil {
		cfg.BumpPatchForMinorPreMajor = *r.BumpPatchForMinorPreMajor
	}

	if r.ChangelogPath != nil {
		cfg.ChangelogPath = *r.ChangelogPath
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
	if r.ChangelogType != nil {
		if err := validateChangelogType(*r.ChangelogType); err != nil {
			return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
		}
		cfg.ChangelogType = *r.ChangelogType
	}
	if r.SkipChangelog != nil {
		cfg.SkipChangelog = *r.SkipChangelog
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
	if r.ChangelogSections != nil {
		sections, err := decodeChangelogSections(*r.ChangelogSections)
		if err != nil {
			return Config{}, fmt.Errorf("config: parse %s: changelog-sections: %w", path, err)
		}
		cfg.ChangelogSections = sections
	}
	if r.ChangelogSentenceCase != nil {
		cfg.ChangelogStyle.SentenceCase = *r.ChangelogSentenceCase
	}
	if r.ChangelogAuthors != nil {
		cfg.ChangelogStyle.Authors = *r.ChangelogAuthors
	}
	if r.ChangelogCommitLinks != nil {
		cfg.ChangelogStyle.CommitLinks = *r.ChangelogCommitLinks
	}
	if r.ChangelogIssueRefs != nil {
		cfg.ChangelogStyle.IssueReferences = *r.ChangelogIssueRefs
	}
	if r.ChangelogHost != nil {
		host, err := ValidateChangelogHost(*r.ChangelogHost)
		if err != nil {
			// The value is left out: it may carry credentials.
			return Config{}, fmt.Errorf("config: parse %s: changelog-host %w", path, err)
		}
		cfg.ChangelogStyle.Host = host
	}
	if r.ChangelogDateFormat != nil {
		if err := changelog.ValidateDateFormat(*r.ChangelogDateFormat); err != nil {
			return Config{}, fmt.Errorf("config: parse %s: changelog-date-format %q %w", path, *r.ChangelogDateFormat, err)
		}
		cfg.ChangelogStyle.DateFormat = *r.ChangelogDateFormat
	}

	if r.Files != nil {
		files, err := decodeFiles(path, *r.Files)
		if err != nil {
			return Config{}, err
		}
		cfg.Files = files
	}

	if r.Author != nil {
		if r.Author.Name != nil {
			cfg.Author.Name = *r.Author.Name
			cfg.Author.Set = append(cfg.Author.Set, "author.name")
		}
		if r.Author.Email != nil {
			cfg.Author.Email = *r.Author.Email
			cfg.Author.Set = append(cfg.Author.Set, "author.email")
		}
	}
	if r.Signoff != nil {
		if *r.Signoff != "" {
			if err := releasepr.CheckSignoff(*r.Signoff); err != nil {
				return Config{}, fmt.Errorf("config: parse %s: signoff %q %w", path, *r.Signoff, err)
			}
		}
		cfg.Signoff = *r.Signoff
	}

	if r.MajorTag != nil {
		cfg.MajorTag = *r.MajorTag
	}
	if r.IncludeVInReleaseName != nil {
		cfg.IncludeVInReleaseName = *r.IncludeVInReleaseName
	}
	if r.SkipGitHubRelease != nil {
		cfg.SkipGitHubRelease = *r.SkipGitHubRelease
	}
	if r.GitHubReleaseDraft != nil {
		cfg.GitHubReleaseDraft = *r.GitHubReleaseDraft
	}
	if r.GitHubReleaseDraftUntilAssetsAttach != nil {
		cfg.GitHubReleaseDraftUntilAssetsAttach = *r.GitHubReleaseDraftUntilAssetsAttach
	}
	if r.ForceTagCreation != nil {
		cfg.ForceTagCreation = *r.ForceTagCreation
	}
	if r.DeleteReleaseBranch != nil {
		cfg.DeleteReleaseBranch = *r.DeleteReleaseBranch
	}

	if r.Labels != nil {
		if r.Labels.Pending != nil {
			if len(*r.Labels.Pending) == 0 {
				return Config{}, fmt.Errorf("config: parse %s: labels.pending is empty", path)
			}
			cfg.Labels.Pending = *r.Labels.Pending
		}
		if r.Labels.Tagged != nil {
			if len(*r.Labels.Tagged) == 0 {
				return Config{}, fmt.Errorf("config: parse %s: labels.tagged is empty", path)
			}
			cfg.Labels.Tagged = *r.Labels.Tagged
		}
		if r.Labels.Extra != nil {
			cfg.Labels.Extra = *r.Labels.Extra
		}
		if r.Labels.PreRelease.Kind != 0 {
			types, err := applyPreReleaseTypes(path, &r.Labels.PreRelease, cfg.Labels.PreRelease)
			if err != nil {
				return Config{}, err
			}
			cfg.Labels.PreRelease = types
		}
	}
	if err := validateLabels(path, cfg.Labels); err != nil {
		return Config{}, err
	}
	if r.CommentMention != nil {
		if *r.CommentMention == "" {
			return Config{}, fmt.Errorf("config: parse %s: comment-mention is empty", path)
		}
		cfg.CommentMention = *r.CommentMention
	}

	// The versioning mode is read before release-pull-request is validated
	// against it, and before the top-level versioning-strategy service-pack
	// check below: both depend on it, so this cluster stays together and in
	// this order even though "versioning" and "components" otherwise group
	// with the components-related keys further down.
	if r.Versioning != nil {
		cfg.Versioning = Versioning(*r.Versioning)
		if !validVersioning[cfg.Versioning] {
			return Config{}, fmt.Errorf("config: parse %s: versioning %q is not independent, locked or linked", path, *r.Versioning)
		}
	}
	if cfg.VersioningStrategy == version.StrategyServicePack && cfg.Versioning != VersioningIndependent {
		return Config{}, fmt.Errorf(
			"config: parse %s: versioning-strategy service-pack applies to versioning independent only, "+
				"a %s family shares one version line", path, cfg.Versioning,
		)
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
	if r.ReleasePullRequestTitle != nil {
		if err := CheckTitlePattern(*r.ReleasePullRequestTitle, releasepr.TitlePerComponent); err != nil {
			return Config{}, fmt.Errorf("config: parse %s: release-pull-request-title %q %w", path, *r.ReleasePullRequestTitle, err)
		}
		cfg.ReleasePullRequestTitle = *r.ReleasePullRequestTitle
	}
	if r.ReleasePullRequestSharedTitle != nil {
		if err := CheckTitlePattern(*r.ReleasePullRequestSharedTitle, cfg.SharedTitleKind()); err != nil {
			return Config{}, fmt.Errorf("config: parse %s: release-pull-request-shared-title %q %w", path, *r.ReleasePullRequestSharedTitle, err)
		}
		cfg.ReleasePullRequestSharedTitle = *r.ReleasePullRequestSharedTitle
	}
	if r.ReleasePullRequestDraft != nil {
		cfg.ReleasePullRequestDraft = *r.ReleasePullRequestDraft
	}
	for _, text := range []struct {
		key   string
		value *string
		dst   *string
	}{
		{"release-pull-request-header", r.ReleasePullRequestHeader, &cfg.ReleasePullRequestHeader},
		{"release-pull-request-footer", r.ReleasePullRequestFooter, &cfg.ReleasePullRequestFooter},
	} {
		if text.value == nil {
			continue
		}
		if err := releasepr.CheckFrameText(*text.value); err != nil {
			return Config{}, fmt.Errorf("config: parse %s: %s %w", path, text.key, err)
		}
		*text.dst = *text.value
	}

	if r.ExcludePaths != nil {
		if r.Components.Kind != 0 {
			return Config{}, fmt.Errorf("config: parse %s: exclude-paths: a configured component carries its own "+
				"exclude-paths, the top-level key applies only without components", path)
		}
		exclude, err := cleanExcludePaths(".", *r.ExcludePaths)
		if err != nil {
			return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
		}
		cfg.ExcludePaths = exclude
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
		if cfg.Versioning != VersioningIndependent {
			for _, c := range components {
				if c.ReleaseAs != nil {
					return Config{}, fmt.Errorf("config: parse %s: component %s: release-as does not apply to versioning %s, "+
						"which shares one version across the family, set it at the top level", path, c.Name, cfg.Versioning)
				}
			}
		}
		if err := validateFileUpdates(path, cfg); err != nil {
			return Config{}, err
		}
	}
	if r.Workspaces != nil {
		workspaces, err := validateWorkspaces(path, cfg, *r.Workspaces)
		if err != nil {
			return Config{}, err
		}
		cfg.Workspaces = workspaces
	}
	if r.AlwaysLinkLocal != nil {
		if !slices.Contains(cfg.Workspaces, workspace.Node) {
			return Config{}, fmt.Errorf("config: parse %s: always-link-local: applies to node dependencies, "+
				"add node to workspaces", path)
		}
		cfg.AlwaysLinkLocal = *r.AlwaysLinkLocal
	}
	if cfg.Versioning != VersioningIndependent {
		for _, comp := range cfg.Resolve() {
			if comp.VersioningStrategy == version.StrategyServicePack {
				return Config{}, fmt.Errorf(
					"config: parse %s: component %s: versioning-strategy service-pack applies to versioning independent only, "+
						"a %s family shares one version line", path, comp.Name, cfg.Versioning,
				)
			}
		}
	}
	if err := validateTagCreation(path, cfg); err != nil {
		return Config{}, err
	}

	if r.TokenEnv != nil {
		cfg.TokenEnv = *r.TokenEnv
	}
	return cfg, nil
}

// validateTagCreation rejects force-tag-creation turned off together with
// skip-github-release turned on, at the top level and for every resolved
// component: force-tag-creation off leaves the tag to the GitHub release's
// publish step, and skip-github-release creates no release for it to be
// left to, so nothing ever creates the tag. The release is lost while the
// merged release pull request is relabelled as tagged anyway, and no later
// run recovers it: the next one recomputes the same version against the
// same still-current last tag.
func validateTagCreation(path string, cfg Config) error {
	const why = "force-tag-creation false leaves the tag to the GitHub release's publish, " +
		"and skip-github-release true creates no release, so nothing would ever create the tag: " +
		"turn force-tag-creation on, or skip-github-release off"
	if !cfg.ForceTagCreation && cfg.SkipGitHubRelease {
		return fmt.Errorf("config: parse %s: %s", path, why)
	}
	for _, comp := range cfg.Resolve() {
		if !comp.ForceTagCreation && comp.SkipGitHubRelease {
			return fmt.Errorf("config: parse %s: component %s: %s", path, comp.Name, why)
		}
	}
	return nil
}

// validateWorkspaces rejects a "workspaces" entry naming no known package
// manager or naming one twice, and the key itself where no dependent can
// release with its dependency: without "components" there is one
// component, and with one release pull request per component a dependent's
// pull request would ask for a dependency version whose own pull request
// may not have merged yet.
func validateWorkspaces(path string, cfg Config, managers []string) ([]string, error) {
	out := make([]string, 0, len(managers))
	for _, m := range managers {
		switch {
		case !slices.Contains(workspace.Managers, m):
			return nil, fmt.Errorf("config: parse %s: workspaces: %q is none of %s",
				path, m, strings.Join(workspace.Managers, ", "))
		case slices.Contains(out, m):
			return nil, fmt.Errorf("config: parse %s: workspaces: %q is listed twice", path, m)
		}
		out = append(out, m)
	}
	if len(out) > 0 && len(cfg.Components) == 0 {
		return nil, fmt.Errorf("config: parse %s: workspaces: links components to each other, "+
			"a repository without components has one", path)
	}
	if len(out) > 0 && !cfg.SharedPullRequest() {
		return nil, fmt.Errorf("config: parse %s: workspaces: a dependent releases together with its dependency, "+
			"set release-pull-request: shared or versioning: locked", path)
	}
	return out, nil
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

// validateChangelogType rejects a "changelog-type" outside the
// ChangelogType* constants.
func validateChangelogType(value string) error {
	if value != ChangelogTypeDefault && value != ChangelogTypeGitHub {
		return fmt.Errorf("changelog-type %q is not %s or %s", value, ChangelogTypeDefault, ChangelogTypeGitHub)
	}
	return nil
}

// ValidateChangelogHost checks a "changelog-host" and returns it without a
// trailing slash. It rejects anything but an absolute http or https
// address with a host and nothing past its path: a query or a fragment
// would end up in the middle of every link. It also rejects whitespace,
// "(" and ")", which end a markdown link target early, so the changelog
// heading's compare link would no longer read back. migrate calls it to
// vet a release-please changelog-host.
func ValidateChangelogHost(host string) (string, error) {
	if strings.ContainsAny(host, " \t\n()") {
		return "", errors.New("contains whitespace or a parenthesis, which break a changelog link")
	}
	u, err := url.Parse(host)
	switch {
	case err != nil:
		return "", errors.New("does not parse as an address")
	case u.Scheme != "http" && u.Scheme != "https", u.Host == "":
		return "", errors.New("is not an absolute http or https address")
	case u.RawQuery != "" || u.Fragment != "" || u.User != nil:
		return "", errors.New("carries a query, a fragment or credentials")
	}
	return strings.TrimRight(host, "/"), nil
}

// decodeChangelogSections converts the yaml shape of a "changelog-sections"
// key into []ChangelogSection, never nil, so a key given as an empty list
// still replaces the default sections. It rejects an entry without a type,
// a type listed twice, a visible entry without a section, and a section
// that is not one line of text: the section renders as a "### <section>"
// heading, and a line break inside it would start a line of its own.
func decodeChangelogSections(raw []rawChangelogSection) ([]ChangelogSection, error) {
	sections := make([]ChangelogSection, 0, len(raw))
	for i, rs := range raw {
		s := ChangelogSection{}
		if rs.Type != nil {
			s.Type = strings.TrimSpace(*rs.Type)
		}
		if rs.Section != nil {
			s.Section = strings.TrimSpace(*rs.Section)
		}
		if rs.Hidden != nil {
			s.Hidden = *rs.Hidden
		}
		switch {
		case s.Type == "":
			return nil, fmt.Errorf("entry %d: type is required", i+1)
		case slices.ContainsFunc(sections, func(o ChangelogSection) bool { return o.Type == s.Type }):
			return nil, fmt.Errorf("type %q is listed twice", s.Type)
		case s.Section == "" && !s.Hidden:
			return nil, fmt.Errorf("type %q: section is required unless hidden is true", s.Type)
		case strings.ContainsAny(s.Section, "\r\n"):
			return nil, fmt.Errorf("type %q: section %q spans more than one line", s.Type, s.Section)
		}
		sections = append(sections, s)
	}
	return sections, nil
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
		if rc.ChangelogPath != nil {
			if err := validateChangelogPath(*rc.ChangelogPath); err != nil {
				return nil, fmt.Errorf("config: parse %s: component %s: changelog-path %q %w", path, name, *rc.ChangelogPath, err)
			}
		}
		if rc.ChangelogType != nil {
			if err := validateChangelogType(*rc.ChangelogType); err != nil {
				return nil, fmt.Errorf("config: parse %s: component %s: %w", path, name, err)
			}
		}
		if rc.ReleaseAs != nil {
			if err := validateReleaseAs(*rc.ReleaseAs); err != nil {
				return nil, fmt.Errorf("config: parse %s: component %s: %w", path, name, err)
			}
		}
		var strategy *version.Strategy
		if rc.VersioningStrategy != nil {
			s, err := parseStrategy(*rc.VersioningStrategy)
			if err != nil {
				return nil, fmt.Errorf("config: parse %s: component %s: %w", path, name, err)
			}
			strategy = &s
		}
		if rc.ChangelogSource != nil && !slices.Contains(changelogSources, *rc.ChangelogSource) {
			return nil, fmt.Errorf(
				"config: parse %s: component %s: changelog-source %q is none of %s",
				path, name, *rc.ChangelogSource, strings.Join(changelogSources, ", "),
			)
		}

		// The repository root is always ".", so "./" is the root component
		// too, for the path validation and the attribution alike.
		componentPath := *rc.Path
		if stdpath.Clean(componentPath) == "." {
			componentPath = "."
		}

		var exclude []string
		if rc.ExcludePaths != nil {
			exclude, err = cleanExcludePaths(stdpath.Clean(componentPath), *rc.ExcludePaths)
			if err != nil {
				return nil, fmt.Errorf("config: parse %s: component %s: %w", path, name, err)
			}
		}

		c := Component{
			ExcludePaths: exclude,
			Name:         name, Path: componentPath, InitialVersion: rc.InitialVersion,
			BumpMinorPreMajor: rc.BumpMinorPreMajor, BumpPatchForMinorPreMajor: rc.BumpPatchForMinorPreMajor,
			ReleaseAs:          rc.ReleaseAs,
			VersioningStrategy: strategy,
			HiddenTypes:        rc.HiddenTypes, MajorTag: rc.MajorTag, ChangelogType: rc.ChangelogType,
			SkipChangelog:   rc.SkipChangelog,
			ChangelogSource: rc.ChangelogSource, TagPrefix: rc.TagPrefix, ChangelogPath: rc.ChangelogPath,
			ForceTagCreation:      rc.ForceTagCreation,
			IncludeVInReleaseName: rc.IncludeVInReleaseName,
			SkipGitHubRelease:     rc.SkipGitHubRelease,
			GitHubReleaseDraft:    rc.GitHubReleaseDraft,
		}
		if rc.Files != nil {
			files, err := decodeFiles(path, *rc.Files)
			if err != nil {
				return nil, err
			}
			c.Files = &files
		}
		if rc.ChangelogSections != nil {
			sections, err := decodeChangelogSections(*rc.ChangelogSections)
			if err != nil {
				return nil, fmt.Errorf("config: parse %s: component %s: changelog-sections: %w", path, name, err)
			}
			c.ChangelogSections = &sections
		}
		components = append(components, c)
	}
	return components, nil
}

// validateReleaseAs rejects a "release-as" value version.ParseForced
// refuses whatever the last version: one that does not parse, and a
// pre-release, which would force the same pre-release again after it
// shipped, since the key stays in release-me.yaml until it is removed.
func validateReleaseAs(value string) error {
	_, err := version.ParseForced(value, version.Semver{}, false, false)
	switch {
	case errors.Is(err, version.ErrPreRelease):
		return fmt.Errorf("release-as %q %w, set one with a pre-release label instead", value, err)
	case err != nil:
		return fmt.Errorf("release-as %q %w", value, err)
	}
	return nil
}

// parseStrategy reads a "versioning-strategy" value, rejecting one that
// names no version.Strategy.
func parseStrategy(value string) (version.Strategy, error) {
	s := version.Strategy(value)
	if !slices.Contains(version.Strategies, s) {
		names := make([]string, len(version.Strategies))
		for i, v := range version.Strategies {
			names[i] = string(v)
		}
		return "", fmt.Errorf("versioning-strategy %q is none of %s", value, strings.Join(names, ", "))
	}
	return s, nil
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

// cleanExcludePaths cleans every "exclude-paths" entry of the component at
// componentPath and rejects one that is not a path nested inside it,
// relative to the repository root: an entry outside the component excludes
// nothing, and one equal to it would exclude every change.
func cleanExcludePaths(componentPath string, paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		clean := stdpath.Clean(p)
		switch {
		case p == "":
			return nil, errors.New("exclude-paths: an entry is empty")
		case stdpath.IsAbs(p) || strings.Contains(p, "\\"):
			return nil, fmt.Errorf("exclude-paths: %q is not a relative path with forward slashes", p)
		case clean == "." || clean == ".." || strings.HasPrefix(clean, "../"):
			return nil, fmt.Errorf("exclude-paths: %q is not a path inside the repository", p)
		case clean == componentPath || componentPath != "." && !strings.HasPrefix(clean, componentPath+"/"):
			return nil, fmt.Errorf("exclude-paths: %q is not nested inside the component path %q, "+
				"the path is relative to the repository root", p, componentPath)
		}
		if !slices.Contains(out, clean) {
			out = append(out, clean)
		}
	}
	return out, nil
}

// validateComponentPaths rejects two components whose paths overlap: equal,
// or one nested inside the other. The root path "." is the one exception:
// a root component beside others is allowed and owns every file outside
// their paths, see ResolvedComponent.OtherPaths. Two components at the
// root are still rejected.
func validateComponentPaths(path string, components []Component) error {
	for i := 0; i < len(components); i++ {
		for j := i + 1; j < len(components); j++ {
			a, b := components[i], components[j]
			if a.Path != b.Path && (a.Path == "." || b.Path == ".") {
				continue
			}
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

// changelogPath returns the component's configured "changelog-path",
// cleaned, or the derived "<path>/CHANGELOG.md" when it carries none.
func (c Component) changelogPath() string {
	if c.ChangelogPath != nil {
		return stdpath.Clean(*c.ChangelogPath)
	}
	return stdpath.Join(c.Path, "CHANGELOG.md")
}

// validateChangelogPath rejects a "changelog-path" that is not a file inside
// the repository: pr writes it relative to the checkout, so an absolute path
// or one that climbs out through ".." would write outside it.
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
	var roles []struct{ key, name string }
	for _, group := range []struct {
		key   string
		names []string
	}{
		{"labels.pending", l.Pending},
		{"labels.tagged", l.Tagged},
	} {
		for _, name := range group.names {
			roles = append(roles, struct{ key, name string }{group.key, name})
		}
	}
	for _, t := range l.PreRelease {
		roles = append(roles, struct{ key, name string }{"labels.prerelease." + t.Type, t.Label})
	}
	for _, name := range l.Extra {
		roles = append(roles, struct{ key, name string }{"labels.extra", name})
	}
	for i := 0; i < len(roles); i++ {
		for j := i + 1; j < len(roles); j++ {
			if roles[i].name != "" && roles[i].name == roles[j].name {
				if roles[i].key == roles[j].key {
					return fmt.Errorf("config: parse %s: %s lists %q twice", path, roles[i].key, roles[i].name)
				}
				return fmt.Errorf("config: parse %s: %s and %s are both %q",
					path, roles[i].key, roles[j].key, roles[i].name)
			}
		}
	}
	return nil
}

// applyPreReleaseTypes merges the "labels.prerelease" mapping node, type
// name to label, onto base, cfg.Labels.PreRelease already holding alpha,
// beta and rc at their defaults: a key naming one of the three, or an
// earlier line of this same block, renames or turns it off in place,
// keeping its position in the order; any other key appends a further
// pre-release type after them, in release-me.yaml's order. A type name is
// one semver pre-release identifier that is not only digits, the "<type>"
// of "X.Y.Z-<type>.N".
func applyPreReleaseTypes(path string, node *yaml.Node, base []PreReleaseType) ([]PreReleaseType, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config: parse %s: labels.prerelease: must be a mapping of type to label", path)
	}
	types := slices.Clone(base)
	for i := 0; i+1 < len(node.Content); i += 2 {
		name, valueNode := node.Content[i].Value, node.Content[i+1]
		if !preReleaseTypeName.MatchString(name) || strings.Trim(name, "0123456789") == "" {
			return nil, fmt.Errorf("config: parse %s: labels.prerelease: type %q is not one pre-release identifier "+
				"of ASCII letters, digits and \"-\" with at least one letter or \"-\"", path, name)
		}
		label, off, err := decodePreReleaseLabelValue(valueNode)
		if err != nil {
			return nil, fmt.Errorf("config: parse %s: labels.prerelease.%s: %w", path, name, err)
		}
		if idx := slices.IndexFunc(types, func(t PreReleaseType) bool { return t.Type == name }); idx >= 0 {
			types[idx].Label = label
			continue
		}
		if off {
			return nil, fmt.Errorf("config: parse %s: labels.prerelease.%s: turning off a type that is not alpha, "+
				"beta, rc or configured earlier in this block is pointless", path, name)
		}
		types = append(types, PreReleaseType{Type: name, Label: label})
	}
	return types, nil
}

// decodePreReleaseLabelValue reads one "labels.prerelease" entry's value: a
// label name, or false, or an empty string, for off. true is rejected: it
// names no label and turns nothing off.
func decodePreReleaseLabelValue(node *yaml.Node) (label string, off bool, err error) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!bool" {
		var on bool
		if err := node.Decode(&on); err != nil {
			return "", false, err
		}
		if on {
			return "", false, errors.New("a pre-release label is a name, or false to turn it off, not true")
		}
		return "", true, nil
	}
	var name string
	if err := node.Decode(&name); err != nil {
		return "", false, err
	}
	return name, name == "", nil
}

// preReleaseTypeName matches one semver pre-release identifier.
var preReleaseTypeName = regexp.MustCompile(`^[0-9A-Za-z-]+$`)

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
