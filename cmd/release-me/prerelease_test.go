// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

func TestSelectPreReleaseType(t *testing.T) {
	t.Parallel()
	lbl := config.Labels{
		Pending: []string{"release: pending"}, Tagged: []string{"release: tagged"},
		PreRelease: []config.PreReleaseType{
			{Type: "alpha", Label: "release: alpha"},
			{Type: "beta", Label: "release: beta"},
			{Type: "rc", Label: "release: rc"},
			{Type: "next", Label: "release: next"},
		},
	}

	cases := []struct {
		name     string
		prLabels []string
		wantType string
		wantOK   bool
		wantErr  []string
	}{
		{name: "stays stable without a pre-release label", prLabels: []string{"release: pending"}},
		{name: "selects alpha", prLabels: []string{"release: pending", "release: alpha"}, wantType: "alpha", wantOK: true},
		{name: "selects beta", prLabels: []string{"release: pending", "release: beta"}, wantType: "beta", wantOK: true},
		{name: "selects rc", prLabels: []string{"release: pending", "release: rc"}, wantType: "rc", wantOK: true},
		{name: "selects a configured prerelease type", prLabels: []string{"release: pending", "release: next"}, wantType: "next", wantOK: true},
		{
			name: "rejects a configured prerelease type beside rc", prLabels: []string{"release: pending", "release: next", "release: rc"},
			wantErr: []string{"release: rc", "release: next"},
		},
		{
			name: "rejects two pre-release labels at once", prLabels: []string{"release: pending", "release: alpha", "release: rc"},
			wantErr: []string{"release: alpha", "release: rc"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			preType, ok, err := selectPreReleaseType(tc.prLabels, lbl)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatal("selectPreReleaseType() error = nil, want an error naming both labels")
				}
				for _, name := range tc.wantErr {
					if !strings.Contains(err.Error(), name) {
						t.Fatalf("error = %v, want it to name %q", err, name)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("selectPreReleaseType() error = %v", err)
			}
			if ok != tc.wantOK || preType != tc.wantType {
				t.Fatalf("selectPreReleaseType() = (%q, %v), want (%q, %v)", preType, ok, tc.wantType, tc.wantOK)
			}
		})
	}
}

func TestPreReleaseVersionCountsPerTypeAndResetsOnAChange(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	base := version.Semver{Minor: 1}

	v, since, err := preReleaseVersion(t.Context(), dir, "v", []string{"v"}, base, "alpha")
	if err != nil {
		t.Fatalf("preReleaseVersion() error = %v", err)
	}
	if v.String() != "0.1.0-alpha.1" || since != "" {
		t.Fatalf("first alpha = (%q, %q), want (%q, %q)", v.String(), since, "0.1.0-alpha.1", "")
	}
	runGit(t, dir, "tag", "v0.1.0-alpha.1")

	v, since, err = preReleaseVersion(t.Context(), dir, "v", []string{"v"}, base, "alpha")
	if err != nil {
		t.Fatalf("preReleaseVersion() error = %v", err)
	}
	if v.String() != "0.1.0-alpha.2" || since != "v0.1.0-alpha.1" {
		t.Fatalf("second alpha = (%q, %q), want (%q, %q)", v.String(), since, "0.1.0-alpha.2", "v0.1.0-alpha.1")
	}

	// A type change does not inherit alpha's counter: beta starts at 1,
	// even though an alpha tag for the same base already exists.
	v, since, err = preReleaseVersion(t.Context(), dir, "v", []string{"v"}, base, "beta")
	if err != nil {
		t.Fatalf("preReleaseVersion() error = %v", err)
	}
	if v.String() != "0.1.0-beta.1" || since != "v0.1.0-alpha.1" {
		t.Fatalf("first beta = (%q, %q), want (%q, %q)", v.String(), since, "0.1.0-beta.1", "v0.1.0-alpha.1")
	}
}

// TestPreReleaseVersionSharesTheCounterAcrossTheFamily reproduces a linked
// or locked family's shared pre-release counter: component b's own tag
// prefix carries no pre-release tag yet, but the counter still advances
// past component a's, because familyTagPrefixes scans both.
func TestPreReleaseVersionSharesTheCounterAcrossTheFamily(t *testing.T) {
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	runGit(t, dir, "tag", "a/v0.9.0-alpha.1")
	base := version.Semver{Minor: 9}

	v, since, err := preReleaseVersion(t.Context(), dir, "b/v", []string{"a/v", "b/v"}, base, "alpha")
	if err != nil {
		t.Fatalf("preReleaseVersion() error = %v", err)
	}
	if v.String() != "0.9.0-alpha.2" {
		t.Fatalf("component b's shared alpha = %q, want %q: the family counter must see a's existing tag", v.String(), "0.9.0-alpha.2")
	}
	if since != "" {
		t.Fatalf("since = %q, want empty: b carries no pre-release tag of its own yet", since)
	}

	// component a's own since still comes from its own prefix, unaffected
	// by the shared counter.
	_, sinceA, err := preReleaseVersion(t.Context(), dir, "a/v", []string{"a/v", "b/v"}, base, "alpha")
	if err != nil {
		t.Fatalf("preReleaseVersion() error = %v", err)
	}
	if sinceA != "a/v0.9.0-alpha.1" {
		t.Fatalf("since = %q, want %q", sinceA, "a/v0.9.0-alpha.1")
	}
}

func TestSelectPreReleaseTypeIgnoresATypeConfiguredEmpty(t *testing.T) {
	t.Parallel()
	lbl := config.Labels{
		Pending: []string{"release: pending"}, Tagged: []string{"release: tagged"},
		PreRelease: []config.PreReleaseType{{Type: "alpha", Label: ""}, {Type: "rc", Label: "release: rc"}},
	}

	// A label named like a turned-off type is an ordinary label, not a
	// pre-release request, and a second label beside rc is no conflict.
	preType, ok, err := selectPreReleaseType([]string{"release: pending", "release: alpha", "release: rc"}, lbl)
	if err != nil || !ok || preType != "rc" {
		t.Fatalf("selectPreReleaseType() = %q, %v, %v, want rc, true, nil", preType, ok, err)
	}
	if _, ok, err := selectPreReleaseType([]string{"release: pending", ""}, lbl); ok || err != nil {
		t.Fatalf("selectPreReleaseType() ok = %v, err = %v, want a stable release for an empty label name", ok, err)
	}
}
