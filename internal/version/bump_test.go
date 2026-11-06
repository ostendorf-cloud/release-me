// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package version_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/version"
)

func TestNextComputesTheReleasedVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		last    version.Semver
		lastOK  bool
		changes []version.Change
		rules   version.Rules
		want    version.Result
	}{
		{
			name:    "bumps minor for a feature",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "feat"}},
			want:    version.Result{Version: "1.3.0", Bump: version.BumpMinor, Reason: "feat"},
		},
		{
			name:    "bumps patch for a fix",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "fix"}},
			want:    version.Result{Version: "1.2.4", Bump: version.BumpPatch, Reason: "fix"},
		},
		{
			name:    "bumps patch for a perf change",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "perf"}},
			want:    version.Result{Version: "1.2.4", Bump: version.BumpPatch, Reason: "perf"},
		},
		{
			name:    "bumps patch for a revert",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "revert"}},
			want:    version.Result{Version: "1.2.4", Bump: version.BumpPatch, Reason: "revert"},
		},
		{
			name:    "bumps major for a breaking change",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "feat", Breaking: true}},
			want:    version.Result{Version: "2.0.0", Bump: version.BumpMajor, Reason: "breaking change"},
		},
		{
			name:    "bumps minor for a breaking change below one pre major",
			last:    version.Semver{Major: 0, Minor: 4, Patch: 1},
			lastOK:  true,
			changes: []version.Change{{Type: "feat", Breaking: true}},
			rules:   version.Rules{BumpMinorPreMajor: true},
			want:    version.Result{Version: "0.5.0", Bump: version.BumpMinor, Reason: "breaking change, minor below 1.0.0"},
		},
		{
			name:    "keeps major for a breaking change below one without pre major",
			last:    version.Semver{Major: 0, Minor: 4, Patch: 1},
			lastOK:  true,
			changes: []version.Change{{Type: "feat", Breaking: true}},
			want:    version.Result{Version: "1.0.0", Bump: version.BumpMajor, Reason: "breaking change"},
		},
		{
			name:    "bumps patch for a feature with always bump patch",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "feat"}},
			rules:   version.Rules{Strategy: version.StrategyAlwaysBumpPatch},
			want:    version.Result{Version: "1.2.4", Bump: version.BumpPatch, Reason: "feat, always-bump-patch"},
		},
		{
			name:    "bumps patch for a breaking change with always bump patch",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "fix", Breaking: true}},
			rules:   version.Rules{Strategy: version.StrategyAlwaysBumpPatch},
			want:    version.Result{Version: "1.2.4", Bump: version.BumpPatch, Reason: "breaking change, always-bump-patch"},
		},
		{
			name:    "bumps minor for a fix with always bump minor",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "fix"}},
			rules:   version.Rules{Strategy: version.StrategyAlwaysBumpMinor},
			want:    version.Result{Version: "1.3.0", Bump: version.BumpMinor, Reason: "fix, always-bump-minor"},
		},
		{
			name:    "bumps major for a fix with always bump major",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "fix"}},
			rules:   version.Rules{Strategy: version.StrategyAlwaysBumpMajor},
			want:    version.Result{Version: "2.0.0", Bump: version.BumpMajor, Reason: "fix, always-bump-major"},
		},
		{
			name:    "ignores the pre major rules with always bump major",
			last:    version.Semver{Major: 0, Minor: 4, Patch: 1},
			lastOK:  true,
			changes: []version.Change{{Type: "feat", Breaking: true}},
			rules:   version.Rules{Strategy: version.StrategyAlwaysBumpMajor, BumpMinorPreMajor: true},
			want:    version.Result{Version: "1.0.0", Bump: version.BumpMajor, Reason: "breaking change, always-bump-major"},
		},
		{
			name:    "keeps the last version for a service pack",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "feat", Breaking: true}},
			rules:   version.Rules{Strategy: version.StrategyServicePack},
			want:    version.Result{Version: "1.2.3", Bump: version.BumpServicePack, Reason: "breaking change, service-pack"},
		},
		{
			name:    "uses the initial version for a first release with service pack",
			changes: []version.Change{{Type: "fix"}},
			rules:   version.Rules{InitialVersion: "1.0.0", Strategy: version.StrategyServicePack},
			want:    version.Result{Version: "1.0.0", Bump: version.BumpInitial, Reason: "initial version"},
		},
		{
			name:   "bumps major for a dependabot major update with dependency manifest",
			last:   version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK: true,
			changes: []version.Change{
				{Type: "fix"}, {Type: "chore", Description: "bump github.com/spf13/cobra from 1.8.1 to 2.0.0"},
			},
			rules: version.Rules{Strategy: version.StrategyDependencyManifest},
			want: version.Result{
				Version: "2.0.0", Bump: version.BumpMajor, Reason: "dependency github.com/spf13/cobra major update",
			},
		},
		{
			name:    "bumps minor for a dependabot minor update with dependency manifest",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "build", Description: "Bump golang.org/x/mod from v0.21.0 to v0.22.0"}},
			rules:   version.Rules{Strategy: version.StrategyDependencyManifest},
			want:    version.Result{Version: "1.3.0", Bump: version.BumpMinor, Reason: "dependency golang.org/x/mod minor update"},
		},
		{
			name:    "bumps patch for a dependabot patch update with dependency manifest",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "chore", Description: "bump gopkg.in/yaml.v3 from 3.0.0 to 3.0.1"}},
			rules:   version.Rules{Strategy: version.StrategyDependencyManifest},
			want:    version.Result{Version: "1.2.4", Bump: version.BumpPatch, Reason: "dependency gopkg.in/yaml.v3 patch update"},
		},
		{
			name:    "bumps minor for a renovate minor update with dependency manifest",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "chore", Description: "update dependency eslint to v9.4.0"}},
			rules:   version.Rules{Strategy: version.StrategyDependencyManifest},
			want:    version.Result{Version: "1.3.0", Bump: version.BumpMinor, Reason: "dependency eslint minor update"},
		},
		{
			name:    "bumps major for a renovate major update with dependency manifest",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "deps", Description: "update module github.com/google/go-github to v68"}},
			rules:   version.Rules{Strategy: version.StrategyDependencyManifest},
			want: version.Result{
				Version: "2.0.0", Bump: version.BumpMajor, Reason: "dependency github.com/google/go-github major update",
			},
		},
		{
			name:    "keeps a feature's minor over a dependency patch update with dependency manifest",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "chore", Description: "update dependency vite to v5.4.11"}, {Type: "feat"}},
			rules:   version.Rules{Strategy: version.StrategyDependencyManifest},
			want:    version.Result{Version: "1.3.0", Bump: version.BumpMinor, Reason: "feat"},
		},
		{
			name:    "applies the pre major rules to a dependency major update with dependency manifest",
			last:    version.Semver{Major: 0, Minor: 4, Patch: 1},
			lastOK:  true,
			changes: []version.Change{{Type: "chore", Description: "bump react from 18.3.1 to 19.0.0"}},
			rules:   version.Rules{Strategy: version.StrategyDependencyManifest, BumpMinorPreMajor: true},
			want: version.Result{
				Version: "0.5.0", Bump: version.BumpMinor, Reason: "dependency react major update, minor below 1.0.0",
			},
		},
		{
			name:    "bumps by the changes with the default strategy",
			last:    version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:  true,
			changes: []version.Change{{Type: "feat"}},
			rules:   version.Rules{Strategy: version.StrategyDefault},
			want:    version.Result{Version: "1.3.0", Bump: version.BumpMinor, Reason: "feat"},
		},
		{
			name:    "uses the initial version for a first release with always bump major",
			changes: []version.Change{{Type: "fix"}},
			rules:   version.Rules{InitialVersion: "0.1.0", Strategy: version.StrategyAlwaysBumpMajor},
			want:    version.Result{Version: "0.1.0", Bump: version.BumpInitial, Reason: "initial version"},
		},
		{
			name:    "picks the highest bump among multiple changes",
			last:    version.Semver{Major: 1, Minor: 0, Patch: 0},
			lastOK:  true,
			changes: []version.Change{{Type: "fix"}, {Type: "feat"}, {Type: "chore"}},
			want:    version.Result{Version: "1.1.0", Bump: version.BumpMinor, Reason: "feat"},
		},
		{
			name:    "uses the initial version when no tag exists and a change bumps",
			lastOK:  false,
			changes: []version.Change{{Type: "chore"}, {Type: "feat"}},
			rules:   version.Rules{InitialVersion: "0.3.0"},
			want:    version.Result{Version: "0.3.0", Bump: version.BumpInitial, Reason: "initial version"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, released, _ := version.Next(tc.last, tc.lastOK, tc.changes, tc.rules)
			if !released {
				t.Fatal("released = false, want true")
			}
			if got != tc.want {
				t.Fatalf("Next() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNextReportsNoRelease(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		last     version.Semver
		lastOK   bool
		changes  []version.Change
		strategy version.Strategy
	}{
		{
			name:     "no change bumps the version with always bump major",
			last:     version.Semver{Major: 1, Minor: 0, Patch: 0},
			lastOK:   true,
			changes:  []version.Change{{Type: "chore"}, {Type: "docs"}},
			strategy: version.StrategyAlwaysBumpMajor,
		},
		{
			name:     "a dependency update bumps nothing without dependency manifest",
			last:     version.Semver{Major: 1, Minor: 0, Patch: 0},
			lastOK:   true,
			changes:  []version.Change{{Type: "chore", Description: "bump react from 18.3.1 to 19.0.0"}},
			strategy: version.StrategyDefault,
		},
		{
			name:     "a chore that is no dependency update bumps nothing with dependency manifest",
			last:     version.Semver{Major: 1, Minor: 0, Patch: 0},
			lastOK:   true,
			changes:  []version.Change{{Type: "chore", Description: "update the dependency list in the README"}},
			strategy: version.StrategyDependencyManifest,
		},
		{
			name:     "no change bumps the version with service pack",
			last:     version.Semver{Major: 1, Minor: 0, Patch: 0},
			lastOK:   true,
			changes:  []version.Change{{Type: "docs"}},
			strategy: version.StrategyServicePack,
		},
		{
			name:    "no change bumps the version on a tagged repository",
			last:    version.Semver{Major: 1, Minor: 0, Patch: 0},
			lastOK:  true,
			changes: []version.Change{{Type: "chore"}, {Type: "docs"}, {Type: "other"}},
		},
		{
			name:   "there are no changes on a tagged repository",
			last:   version.Semver{Major: 1, Minor: 0, Patch: 0},
			lastOK: true,
		},
		{
			name:   "no tag exists and there are no changes",
			lastOK: false,
		},
		{
			name:    "no tag exists and no change bumps the version",
			lastOK:  false,
			changes: []version.Change{{Type: "chore"}, {Type: "docs"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, released, _ := version.Next(tc.last, tc.lastOK, tc.changes, version.Rules{
				InitialVersion: "0.1.0", Strategy: tc.strategy,
			})
			if released {
				t.Fatal("released = true, want false")
			}
		})
	}
}

func TestNextLetsAReleaseAsFooterWinOverAnAlwaysBumpStrategy(t *testing.T) {
	t.Parallel()
	last := version.Semver{Major: 1, Minor: 2, Patch: 3}
	changes := []version.Change{{Type: "fix"}, {ReleaseAs: "3.0.0"}}

	got, released, _ := version.Next(last, true, changes, version.Rules{Strategy: version.StrategyAlwaysBumpMinor})

	want := version.Result{Version: "3.0.0", Bump: version.BumpForced, Reason: "Release-As footer"}
	if !released || got != want {
		t.Fatalf("Next() = %+v, %v, want %+v, true", got, released, want)
	}
}

func TestChangedReportsWhetherAnyChangeBumpsTheVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		changes []version.Change
		want    bool
	}{
		{name: "no changes"},
		{name: "only hidden types", changes: []version.Change{{Type: "chore"}, {Type: "docs"}}},
		{name: "a fix bumps", changes: []version.Change{{Type: "chore"}, {Type: "fix"}}, want: true},
		{name: "a breaking change bumps", changes: []version.Change{{Type: "fix", Breaking: true}}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := version.Changed(tc.changes, version.StrategyDefault); got != tc.want {
				t.Fatalf("Changed(%+v) = %v, want %v", tc.changes, got, tc.want)
			}
		})
	}
}

func TestNextLetsAReleaseAsFooterWinOverTheComputedBump(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		last         version.Semver
		lastOK       bool
		changes      []version.Change
		want         version.Result
		wantReleased bool
		wantRefused  []string
	}{
		{
			name:   "forces the footer's version over a feature's minor bump",
			last:   version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK: true,
			changes: []version.Change{
				{Type: "feat"},
				{Type: "chore", ReleaseAs: "2.0.0"},
			},
			want:         version.Result{Version: "2.0.0", Bump: version.BumpForced, Reason: "Release-As footer"},
			wantReleased: true,
		},
		{
			name:         "releases on a footer alone, without a bumping change",
			last:         version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:       true,
			changes:      []version.Change{{Type: "chore", ReleaseAs: "1.3.0"}},
			want:         version.Result{Version: "1.3.0", Bump: version.BumpForced, Reason: "Release-As footer"},
			wantReleased: true,
		},
		{
			name:         "replaces the initial version without a tag",
			changes:      []version.Change{{Type: "feat", ReleaseAs: "1.0.0"}},
			want:         version.Result{Version: "1.0.0", Bump: version.BumpForced, Reason: "Release-As footer"},
			wantReleased: true,
		},
		{
			name:         "falls back to the computed bump for a footer not ahead of the last tag",
			last:         version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:       true,
			changes:      []version.Change{{Type: "fix", ReleaseAs: "1.2.3"}},
			want:         version.Result{Version: "1.2.4", Bump: version.BumpPatch, Reason: "fix"},
			wantReleased: true,
			wantRefused:  []string{"1.2.3"},
		},
		{
			name:        "releases nothing for a refused footer without a bumping change",
			last:        version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK:      true,
			changes:     []version.Change{{Type: "chore", ReleaseAs: "1.0.0"}},
			wantRefused: []string{"1.0.0"},
		},
		{
			name:   "returns the refused footer beside the forced version",
			last:   version.Semver{Major: 1, Minor: 2, Patch: 3},
			lastOK: true,
			changes: []version.Change{
				{Type: "chore", ReleaseAs: "2.0.0-rc.1"},
				{Type: "chore", ReleaseAs: "2.0.0"},
			},
			want:         version.Result{Version: "2.0.0", Bump: version.BumpForced, Reason: "Release-As footer"},
			wantReleased: true,
			wantRefused:  []string{"2.0.0-rc.1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, released, refused := version.Next(tc.last, tc.lastOK, tc.changes, version.Rules{InitialVersion: "0.1.0"})
			if got != tc.want || released != tc.wantReleased {
				t.Fatalf("Next() = %+v, %v, want %+v, %v", got, released, tc.want, tc.wantReleased)
			}
			var gotRefused []string
			for _, r := range refused {
				gotRefused = append(gotRefused, r.Value)
			}
			if !slices.Equal(gotRefused, tc.wantRefused) {
				t.Fatalf("Next() refused = %+v, want values %v", refused, tc.wantRefused)
			}
		})
	}
}

func TestReleaseAsPicksTheHighestUsableFooter(t *testing.T) {
	t.Parallel()
	last := version.Semver{Major: 1, Minor: 2, Patch: 3}
	cases := []struct {
		name        string
		lastOK      bool
		changes     []version.Change
		want        string
		wantRefused []string
	}{
		{
			name:    "picks the highest of several, whatever their order",
			lastOK:  true,
			changes: []version.Change{{ReleaseAs: "1.5.0"}, {ReleaseAs: "2.0.0"}, {ReleaseAs: "1.9.0"}},
			want:    "2.0.0",
		},
		{
			name:    "accepts a v prefix",
			lastOK:  true,
			changes: []version.Change{{ReleaseAs: "v1.4.0"}},
			want:    "1.4.0",
		},
		{
			name:        "refuses a value that does not parse",
			lastOK:      true,
			changes:     []version.Change{{ReleaseAs: "next"}},
			wantRefused: []string{"next"},
		},
		{
			name:        "refuses a pre-release value",
			lastOK:      true,
			changes:     []version.Change{{ReleaseAs: "2.0.0-rc.1"}},
			wantRefused: []string{"2.0.0-rc.1"},
		},
		{
			name:        "refuses a value equal to or below the last tag, keeping a usable one",
			lastOK:      true,
			changes:     []version.Change{{ReleaseAs: "1.2.3"}, {ReleaseAs: "1.3.0"}, {ReleaseAs: "1.0.0"}},
			want:        "1.3.0",
			wantRefused: []string{"1.2.3", "1.0.0"},
		},
		{
			name:        "refuses the same value once when several components carry its commit",
			lastOK:      true,
			changes:     []version.Change{{ReleaseAs: "next"}, {ReleaseAs: "1.0.0"}, {ReleaseAs: "next"}},
			wantRefused: []string{"next", "1.0.0"},
		},
		{
			name:    "accepts any version without a tag",
			changes: []version.Change{{ReleaseAs: "0.0.1"}},
			want:    "0.0.1",
		},
		{
			name:    "ignores a change without a footer",
			lastOK:  true,
			changes: []version.Change{{Type: "feat"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forced, ok, refused := version.ReleaseAs(last, tc.lastOK, tc.changes)
			got := ""
			if ok {
				got = forced.String()
			}
			if got != tc.want {
				t.Fatalf("ReleaseAs() forced = %q, want %q", got, tc.want)
			}
			var gotRefused []string
			for _, r := range refused {
				gotRefused = append(gotRefused, r.Value)
			}
			if !slices.Equal(gotRefused, tc.wantRefused) {
				t.Fatalf("ReleaseAs() refused = %+v, want values %v", refused, tc.wantRefused)
			}
		})
	}
}

func TestParseForcedAppliesTheOneForcedVersionRule(t *testing.T) {
	t.Parallel()
	last := version.Semver{Major: 1, Minor: 2, Patch: 3}
	cases := []struct {
		name            string
		value           string
		lastOK          bool
		allowPreRelease bool
		want            string
		wantErr         error
	}{
		{name: "accepts a greater plain version", value: "1.3.0", lastOK: true, want: "1.3.0"},
		{name: "accepts a v prefix", value: "v2.0.0", lastOK: true, want: "2.0.0"},
		{name: "accepts a capital V prefix", value: "V2.0.0", lastOK: true, want: "2.0.0"},
		{name: "refuses a value that does not parse", value: "next", lastOK: true, wantErr: version.ErrNotAVersion},
		{name: "refuses a two-part version", value: "2.0", lastOK: true, wantErr: version.ErrNotAVersion},
		{name: "refuses the last version itself", value: "1.2.3", lastOK: true, wantErr: version.ErrNotGreater},
		{name: "refuses a lower version", value: "1.0.0", lastOK: true, wantErr: version.ErrNotGreater},
		{name: "accepts any version without a tag", value: "0.0.1", want: "0.0.1"},
		{
			name:  "refuses a pre-release value when pre-releases are not allowed",
			value: "2.0.0-rc.1", lastOK: true, wantErr: version.ErrPreRelease,
		},
		{
			name:  "accepts a pre-release value when pre-releases are allowed",
			value: "2.0.0-rc.1", lastOK: true, allowPreRelease: true, want: "2.0.0-rc.1",
		},
		{
			name:  "refuses a pre-release of the last version even when allowed",
			value: "1.2.3-rc.1", lastOK: true, allowPreRelease: true, wantErr: version.ErrNotGreater,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := version.ParseForced(tc.value, last, tc.lastOK, tc.allowPreRelease)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ParseForced(%q) error = %v, want %v", tc.value, err, tc.wantErr)
			}
			if err == nil && got.String() != tc.want {
				t.Fatalf("ParseForced(%q) = %s, want %s", tc.value, got, tc.want)
			}
		})
	}
}
