// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package workspace

import (
	"strconv"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/version"
)

// localProtocols are the node dependency protocols that always resolve
// inside the repository, whatever the version: a workspace package or a
// path on disk.
var localProtocols = []string{"workspace:", "file:", "link:", "portal:"}

// nodeLinksLocal reports whether the node dependency range rng resolves to
// the local package at localVersion when a dependency is linked only where
// it would resolve locally anyway, release-please's "always-link-local"
// turned off: a local protocol always does, any other range only when
// localVersion satisfies it. A range this reader cannot parse, such as a
// dist-tag like "latest" or a URL, never does.
func nodeLinksLocal(rng, localVersion string) bool {
	for _, p := range localProtocols {
		if strings.HasPrefix(rng, p) {
			return true
		}
	}
	v, ok := version.ParseTag("v" + strings.TrimPrefix(localVersion, "v"))
	if !ok {
		return false
	}
	return nodeSatisfies(rng, v)
}

// nodeSatisfies reports whether v satisfies rng, an npm semver range: one
// or more comparator sets joined by "||", each a space separated list of
// comparators that must all match, or a hyphen range "a - b". A comparator
// is an optional operator, "=", ">", ">=", "<", "<=", "~" or "^", and a
// version that may be partial or carry "x", "X" or "*" wildcards. The
// npm rule excluding pre-release versions from a range that names none is
// not applied: the local version a range is checked against is the one
// the repository already carries.
func nodeSatisfies(rng string, v version.Semver) bool {
	for _, set := range strings.Split(rng, "||") {
		if satisfiesSet(strings.Fields(set), v) {
			return true
		}
	}
	return false
}

// satisfiesSet reports whether v matches every comparator of tokens, a
// comparator set split on whitespace, an empty set matching everything.
func satisfiesSet(tokens []string, v version.Semver) bool {
	if len(tokens) == 3 && tokens[1] == "-" {
		lo, okLo := parsePartial(tokens[0])
		hi, okHi := parsePartial(tokens[2])
		return okLo && okHi && compare(">=", lo, v) && compare("<=", hi, v)
	}
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		op := operator(tok)
		if op == tok && i+1 < len(tokens) {
			// An operator written apart from its version, ">= 1.2.0".
			i++
			tok += tokens[i]
		}
		p, ok := parsePartial(strings.TrimPrefix(tok, op))
		if !ok || !compare(op, p, v) {
			return false
		}
	}
	return true
}

// operator returns the comparison operator tok starts with, "" for none.
func operator(tok string) string {
	for _, op := range []string{">=", "<=", ">", "<", "=", "~", "^"} {
		if strings.HasPrefix(tok, op) {
			return op
		}
	}
	return ""
}

// partial is a possibly partial version: n of its three numbers given,
// the rest a wildcard.
type partial struct {
	nums [3]int
	n    int
	pre  string
}

// parsePartial reads s, "1", "1.2", "1.2.3", "1.2.3-rc.1", "1.x" or "*",
// an optional "v" in front.
func parsePartial(s string) (partial, bool) {
	s = strings.TrimPrefix(s, "v")
	var p partial
	if s == "" {
		return p, true
	}
	s, _, _ = strings.Cut(s, "+")
	s, p.pre, _ = strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return partial{}, false
	}
	for i, part := range parts {
		if part == "x" || part == "X" || part == "*" {
			break
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return partial{}, false
		}
		p.nums[i] = n
		p.n = i + 1
	}
	if p.n < 3 {
		p.pre = ""
	}
	return p, true
}

// low returns p with its wildcards set to zero.
func (p partial) low() version.Semver {
	return version.Semver{Major: p.nums[0], Minor: p.nums[1], Patch: p.nums[2], Pre: p.pre}
}

// next returns the lowest version above every one p's wildcards cover:
// "1" gives 2.0.0, "1.2" gives 1.3.0. ok is false for "*", which has none.
func (p partial) next() (version.Semver, bool) {
	switch p.n {
	case 1:
		return version.Semver{Major: p.nums[0] + 1}, true
	case 2:
		return version.Semver{Major: p.nums[0], Minor: p.nums[1] + 1}, true
	case 3:
		return version.Semver{Major: p.nums[0], Minor: p.nums[1], Patch: p.nums[2] + 1}, true
	}
	return version.Semver{}, false
}

// compare reports whether v matches the comparator op p.
func compare(op string, p partial, v version.Semver) bool {
	lo := p.low()
	atLeast := !v.LessPrecedence(lo)
	below := func(hi version.Semver, ok bool) bool { return !ok || v.LessPrecedence(hi) }
	switch op {
	case "", "=":
		if p.n == 3 {
			return atLeast && !lo.LessPrecedence(v)
		}
		return atLeast && below(p.next())
	case ">=":
		return atLeast
	case ">":
		if p.n == 3 {
			return lo.LessPrecedence(v)
		}
		hi, ok := p.next()
		return ok && !v.LessPrecedence(hi)
	case "<":
		return p.n > 0 && v.LessPrecedence(lo)
	case "<=":
		if p.n == 3 {
			return !lo.LessPrecedence(v)
		}
		return below(p.next())
	case "~":
		if p.n >= 2 {
			return atLeast && v.LessPrecedence(version.Semver{Major: lo.Major, Minor: lo.Minor + 1})
		}
		return atLeast && below(p.next())
	case "^":
		switch {
		case p.n == 0:
			return true
		case lo.Major > 0 || p.n == 1:
			return atLeast && v.LessPrecedence(version.Semver{Major: lo.Major + 1})
		case lo.Minor > 0 || p.n == 2:
			return atLeast && v.LessPrecedence(version.Semver{Minor: lo.Minor + 1})
		default:
			return atLeast && v.LessPrecedence(version.Semver{Patch: lo.Patch + 1})
		}
	}
	return false
}
