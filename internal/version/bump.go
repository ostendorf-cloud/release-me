// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package version

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Bump names the kind of version increment applied.
type Bump string

// The bump kinds a release can have.
const (
	BumpNone    Bump = "none"
	BumpPatch   Bump = "patch"
	BumpMinor   Bump = "minor"
	BumpMajor   Bump = "major"
	BumpInitial Bump = "initial"
	// BumpForced is a version a "Release-As" footer forces, see ReleaseAs.
	BumpForced Bump = "forced"
	// BumpServicePack is a service pack of the last version, see
	// StrategyServicePack: Result.Version is that last version itself, for
	// the caller to give its next "sp.N" pre-release identifier.
	BumpServicePack Bump = "service-pack"
)

// Change is the bump-relevant part of one history entry.
type Change struct {
	Type     string
	Breaking bool
	// Description is the entry's description, the text after the type and
	// scope, which StrategyDependencyManifest reads a dependency update
	// from.
	Description string
	// ReleaseAs is the value of the entry's "Release-As" footer, "" when
	// it carries none. A Change may carry only ReleaseAs, no Type: it then
	// forces a version without bumping one.
	ReleaseAs string
}

// Rules are the configured bump rules Next applies on top of the changes.
type Rules struct {
	// InitialVersion is the version a component's first release gets.
	InitialVersion string
	// BumpMinorPreMajor turns a major bump into a minor one below 1.0.0.
	BumpMinorPreMajor bool
	// BumpPatchForMinorPreMajor turns a minor bump, a feat, into a patch
	// one below 1.0.0. A minor bump BumpMinorPreMajor lowered a breaking
	// change to stays minor: a breaking change never ships as a patch.
	BumpPatchForMinorPreMajor bool
	// ReleaseAs is the configured "release-as" key, "" when it is left
	// out: a version that wins over every "Release-As" footer and the
	// computed bump while ParseForced accepts it against the last version.
	ReleaseAs string
	// Strategy picks how a release bumps the version, StrategyDefault
	// when "".
	Strategy Strategy
}

// Strategy names how a release bumps the version, release-please's
// versioning strategies of the same names.
type Strategy string

// The strategies Rules.Strategy accepts.
const (
	// StrategyDefault bumps by the highest change: major for a breaking
	// change, minor for a feat, patch for a fix, perf or revert, with the
	// pre-major rules applied below 1.0.0.
	StrategyDefault Strategy = "default"
	// StrategyAlwaysBumpPatch, StrategyAlwaysBumpMinor and
	// StrategyAlwaysBumpMajor bump the patch, minor or major of every
	// release, whatever its changes are. The changes still decide whether
	// there is a release at all; the pre-major rules do not apply.
	StrategyAlwaysBumpPatch Strategy = "always-bump-patch"
	StrategyAlwaysBumpMinor Strategy = "always-bump-minor"
	StrategyAlwaysBumpMajor Strategy = "always-bump-major"
	// StrategyServicePack releases a service pack of the last version
	// instead of bumping it: Next returns BumpServicePack with the last
	// version unchanged, and the caller, which can read the tags, turns
	// it into "X.Y.Z-sp.N", N counting up from the last service pack tag
	// of that version.
	StrategyServicePack Strategy = "service-pack"
	// StrategyDependencyManifest bumps like StrategyDefault, and on top
	// of that by every dependency update among the changes, see
	// dependencyBump: a dependency's major update bumps the major, a minor
	// update the minor, any other update the patch, whatever the type of
	// the change carrying it.
	StrategyDependencyManifest Strategy = "dependency-manifest"
)

// Strategies lists every Strategy, in the order a configuration error
// names them.
var Strategies = []Strategy{
	StrategyDefault, StrategyAlwaysBumpPatch, StrategyAlwaysBumpMinor, StrategyAlwaysBumpMajor,
	StrategyServicePack, StrategyDependencyManifest,
}

// fixedBump returns the bump an always-bump strategy gives every release.
// ok is false for the three strategies that have no fixed bump, which the
// default arm returns together: StrategyDefault and
// StrategyDependencyManifest both bump by the changes, and
// StrategyServicePack never reaches here, since Next returns its service
// pack of the last version before asking.
func (s Strategy) fixedBump() (bump Bump, ok bool) {
	switch s {
	case StrategyAlwaysBumpPatch:
		return BumpPatch, true
	case StrategyAlwaysBumpMinor:
		return BumpMinor, true
	case StrategyAlwaysBumpMajor:
		return BumpMajor, true
	default:
		return BumpNone, false
	}
}

// The reasons a forced Result names, and the Source of a Refusal.
const (
	ReasonReleaseAsKey    = "release-as key"
	ReasonReleaseAsFooter = "Release-As footer"
)

// Result is the computed next version.
type Result struct {
	Version string
	Bump    Bump
	Reason  string
}

// Next computes the next version from changes since the last tag.
//
// lastOK is false when no version tag exists yet; Next then returns
// rules.InitialVersion, still only once at least one change bumps the version.
// released is false when there is nothing to release: either no changes at
// all, or no change of a type that bumps the version. An always-bump
// rules.Strategy replaces the bump the changes compute, never whether
// there is a release.
//
// rules.ReleaseAs, when ParseForced accepts it against last, a pre-release
// refused, wins outright over every footer, the computed bump and the
// initial version, and releases even when no change bumps the version.
// Without it, a usable "Release-As" footer among changes, see ReleaseAs,
// wins the same way. refused lists the rules.ReleaseAs value and every footer
// value it could not use, whatever Next returns otherwise: Next never logs,
// so the caller reports them, once per computation.
func Next(last Semver, lastOK bool, changes []Change, rules Rules) (result Result, released bool, refused []Refusal) {
	forced, ok, refused := ReleaseAs(last, lastOK, changes)
	if rules.ReleaseAs != "" {
		v, err := ParseForced(rules.ReleaseAs, last, lastOK, false)
		if err == nil {
			return Result{Version: v.String(), Bump: BumpForced, Reason: ReasonReleaseAsKey}, true, refused
		}
		keyRefusal := Refusal{Value: rules.ReleaseAs, Reason: err.Error() + ", remove it from release-me.yaml", Source: ReasonReleaseAsKey}
		refused = append([]Refusal{keyRefusal}, refused...)
	}
	if ok {
		return Result{Version: forced.String(), Bump: BumpForced, Reason: ReasonReleaseAsFooter}, true, refused
	}

	bump, reason := highestBump(changes, rules.Strategy)
	if bump == BumpNone {
		return Result{}, false, refused
	}

	if !lastOK {
		return Result{Version: rules.InitialVersion, Bump: BumpInitial, Reason: "initial version"}, true, refused
	}

	if rules.Strategy == StrategyServicePack {
		return Result{Version: last.String(), Bump: BumpServicePack, Reason: reason + ", " + string(rules.Strategy)}, true, refused
	}
	if fixed, ok := rules.Strategy.fixedBump(); ok {
		return Result{Version: bumped(last, fixed).String(), Bump: fixed, Reason: reason + ", " + string(rules.Strategy)}, true, refused
	}

	switch {
	case bump == BumpMajor && rules.BumpMinorPreMajor && last.Major < 1:
		bump = BumpMinor
		reason += ", minor below 1.0.0"
	case bump == BumpMinor && rules.BumpPatchForMinorPreMajor && last.Major < 1:
		bump = BumpPatch
		reason += ", patch below 1.0.0"
	}
	return Result{Version: bumped(last, bump).String(), Bump: bump, Reason: reason}, true, refused
}

// bumped returns last with bump applied: the bumped part counted up, every
// part below it reset to 0. BumpNone, BumpInitial and BumpForced return
// last unchanged.
func bumped(last Semver, bump Bump) Semver {
	switch bump {
	case BumpMajor:
		return Semver{Major: last.Major + 1}
	case BumpMinor:
		return Semver{Major: last.Major, Minor: last.Minor + 1}
	case BumpPatch:
		return Semver{Major: last.Major, Minor: last.Minor, Patch: last.Patch + 1}
	case BumpNone, BumpInitial, BumpForced, BumpServicePack:
		return last
	default:
		return last
	}
}

// Changed reports whether changes contains at least one change that bumps
// the version under strategy, a dependency update counting under
// StrategyDependencyManifest only. Next returns released true for exactly
// these changes, regardless of the previous version: callers that need to
// know whether a component has anything to release, without computing its
// next version yet, can use Changed instead.
func Changed(changes []Change, strategy Strategy) bool {
	bump, _ := highestBump(changes, strategy)
	return bump != BumpNone
}

func highestBump(changes []Change, strategy Strategy) (bump Bump, reason string) {
	bump = BumpNone
	for _, c := range changes {
		var b Bump
		var r string
		if strategy == StrategyDependencyManifest {
			if db, name, ok := dependencyBump(c.Description); ok && severity(db) > severity(bump) {
				bump, reason = db, "dependency "+name+" "+string(db)+" update"
			}
		}
		switch {
		case c.Breaking:
			b, r = BumpMajor, "breaking change"
		case c.Type == "feat":
			b, r = BumpMinor, "feat"
		case c.Type == "fix":
			b, r = BumpPatch, "fix"
		case c.Type == "perf":
			b, r = BumpPatch, "perf"
		case c.Type == "revert":
			b, r = BumpPatch, "revert"
		default:
			continue
		}
		if severity(b) > severity(bump) {
			bump, reason = b, r
		}
	}
	return bump, reason
}

func severity(b Bump) int {
	switch b {
	case BumpMajor:
		return 3
	case BumpMinor:
		return 2
	case BumpPatch:
		return 1
	case BumpNone, BumpInitial, BumpForced, BumpServicePack:
		return 0
	default:
		return 0
	}
}

// The dependency update descriptions dependencyBump reads: Renovate's
// "update dependency <name> to v<version>", "update module" for a Go
// module, and Dependabot's "bump <name> from <old> to <new>".
var (
	renovateUpdate   = regexp.MustCompile(`(?i)^update (?:dependency|module) (\S+) to v?(\d+(?:\.\d+){0,2})\b`)
	dependabotUpdate = regexp.MustCompile(`(?i)^bump (\S+) from v?(\d+(?:\.\d+){0,2})\S* to v?(\d+(?:\.\d+){0,2})\b`)
)

// dependencyBump reads description as a dependency update and returns the
// bump that update is, with the dependency's name. ok is false for a
// description that is no dependency update. Dependabot names both
// versions, so the bump is the highest part that changed between them.
// Renovate names only the new one, so the bump follows its shape, the
// rule release-please's dependency-manifest strategy applies: "X.0.0" is
// a major update, "X.Y.0" a minor one, anything else a patch one.
func dependencyBump(description string) (bump Bump, name string, ok bool) {
	if m := dependabotUpdate.FindStringSubmatch(description); m != nil {
		from, to := versionParts(m[2]), versionParts(m[3])
		switch {
		case from[0] != to[0]:
			return BumpMajor, m[1], true
		case from[1] != to[1]:
			return BumpMinor, m[1], true
		default:
			return BumpPatch, m[1], true
		}
	}
	if m := renovateUpdate.FindStringSubmatch(description); m != nil {
		to := versionParts(m[2])
		switch {
		case to[1] == 0 && to[2] == 0:
			return BumpMajor, m[1], true
		case to[2] == 0:
			return BumpMinor, m[1], true
		default:
			return BumpPatch, m[1], true
		}
	}
	return BumpNone, "", false
}

// versionParts splits a "X", "X.Y" or "X.Y.Z" version into its three
// numbers, a missing part 0.
func versionParts(v string) [3]int {
	var parts [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		parts[i], _ = strconv.Atoi(p)
	}
	return parts
}

// Refusal is one forced version Next or ReleaseAs could not use, with the
// reason in one line and its Source, ReasonReleaseAsFooter or
// ReasonReleaseAsKey.
type Refusal struct {
	Value  string
	Reason string
	Source string
}

// The reasons ParseForced refuses a value for, in the one-line form a log
// line or a reply names them in.
var (
	ErrNotAVersion = errors.New("does not parse as an X.Y.Z version")
	ErrPreRelease  = errors.New("is a pre-release version")
	ErrNotGreater  = errors.New("is not greater than the last released version")
)

// ParseForced is the one rule every forced version follows, a
// "Release-As" footer, a release-me-version block and a release-as
// comment command alike: value parses as a plain X.Y.Z version, with an
// optional "v" or "V" prefix and an optional pre-release and build
// suffix, and is strictly greater than last, the pre-release suffix not
// compared. With no last tag yet (lastOK false) any such version passes.
// A pre-release value passes only when allowPreRelease is true. The error
// wraps ErrNotAVersion, ErrPreRelease or ErrNotGreater; its text is the
// reason alone, ErrNotGreater's naming last.
func ParseForced(value string, last Semver, lastOK, allowPreRelease bool) (Semver, error) {
	v, ok := ParseTag("v" + strings.TrimPrefix(strings.TrimPrefix(value, "v"), "V"))
	switch {
	case !ok:
		return Semver{}, ErrNotAVersion
	case v.IsPreRelease() && !allowPreRelease:
		return Semver{}, ErrPreRelease
	case lastOK && !last.Less(v):
		return Semver{}, fmt.Errorf("%w %s", ErrNotGreater, last)
	default:
		return v, nil
	}
}

// ReleaseAs picks the version the "Release-As" footers among changes
// force. A value is usable when ParseForced accepts it against last, a
// pre-release value refused. Of several usable values the highest wins,
// whatever order the commits carrying them came in. A pre-release value
// is refused because the footer stays in the history until the next
// stable tag, so it would force the same pre-release again after it
// shipped; a pre-release label on the release pull request builds on the
// forced version instead. ok is false when no value is usable; refused
// lists every distinct value that is not, once, in the order changes
// first carries it, so a commit attributed to several components of one
// family is refused once.
func ReleaseAs(last Semver, lastOK bool, changes []Change) (forced Semver, ok bool, refused []Refusal) {
	seen := make(map[string]bool)
	for _, c := range changes {
		if c.ReleaseAs == "" {
			continue
		}
		v, err := ParseForced(c.ReleaseAs, last, lastOK, false)
		switch {
		case err != nil:
			if seen[c.ReleaseAs] {
				continue
			}
			seen[c.ReleaseAs] = true
			reason := err.Error()
			if errors.Is(err, ErrPreRelease) {
				reason += ", set one with a pre-release label instead"
			}
			refused = append(refused, Refusal{Value: c.ReleaseAs, Reason: reason, Source: ReasonReleaseAsFooter})
		case !ok || forced.Less(v):
			forced, ok = v, true
		}
	}
	return forced, ok, refused
}
