// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func TestLoadReadsALabelRoleAsOneNameOrAList(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		content     string
		wantPending []string
		wantTagged  []string
		wantExtra   []string
	}{
		{
			name:        "one name, the form before lists",
			content:     "labels:\n  pending: \"autorelease: pending\"\n  tagged: \"autorelease: tagged\"\n",
			wantPending: []string{"autorelease: pending"}, wantTagged: []string{"autorelease: tagged"},
		},
		{
			name: "lists",
			content: "labels:\n  pending: [\"autorelease: pending\", \"release\"]\n" +
				"  tagged:\n    - \"autorelease: tagged\"\n    - shipped\n",
			wantPending: []string{"autorelease: pending", "release"}, wantTagged: []string{"autorelease: tagged", "shipped"},
		},
		{
			name:        "extra labels beside the defaults",
			content:     "labels:\n  extra: [\"team: platform\", automerge]\n",
			wantPending: []string{config.DefaultPendingLabel}, wantTagged: []string{config.DefaultTaggedLabel},
			wantExtra: []string{"team: platform", "automerge"},
		},
		{
			name:        "one extra label as a name",
			content:     "labels:\n  extra: automerge\n",
			wantPending: []string{config.DefaultPendingLabel}, wantTagged: []string{config.DefaultTaggedLabel},
			wantExtra: []string{"automerge"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(writeFile(t, tc.content))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			l := got.Labels
			if !reflect.DeepEqual(l.Pending, tc.wantPending) || !reflect.DeepEqual(l.Tagged, tc.wantTagged) ||
				!reflect.DeepEqual(l.Extra, tc.wantExtra) {
				t.Fatalf("Labels = %+v, want pending %q, tagged %q, extra %q", l, tc.wantPending, tc.wantTagged, tc.wantExtra)
			}
			if want := append(append([]string{}, tc.wantPending...), tc.wantExtra...); !reflect.DeepEqual(l.Opened(), want) {
				t.Fatalf("Opened() = %q, want %q", l.Opened(), want)
			}
		})
	}
}

func TestCreateMissingDefaultsToTrueAndReadsAnExplicitFalse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "the default, no release-me.yaml", content: "", want: true},
		{name: "an empty labels block", content: "labels:\n  pending: release\n", want: true},
		{name: "turned off", content: "labels:\n  create-missing: false\n", want: false},
		{name: "turned on explicitly", content: "labels:\n  create-missing: true\n", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeFile(t, tc.content)
			got, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.Labels.CreateMissing != tc.want {
				t.Fatalf("Labels.CreateMissing = %v, want %v", got.Labels.CreateMissing, tc.want)
			}
		})
	}
}

func TestDefaultSetsCreateMissingToTrue(t *testing.T) {
	t.Parallel()
	if !config.Default().Labels.CreateMissing {
		t.Fatal("Default().Labels.CreateMissing = false, want true")
	}
}

func TestLoadRejectsALabelListThatCannotWork(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{name: "an empty pending list", content: "labels:\n  pending: []\n", want: "labels.pending is empty"},
		{name: "an empty tagged name", content: "labels:\n  tagged: \"\"\n", want: "a label name is empty"},
		{name: "an empty name in a list", content: "labels:\n  extra: [automerge, \"\"]\n", want: "a label name is empty"},
		{
			name:    "a pending label also tagged",
			content: "labels:\n  pending: [release, \"release: pending\"]\n  tagged: [release]\n",
			want:    `labels.pending and labels.tagged are both "release"`,
		},
		{
			name:    "an extra label that selects a pre-release",
			content: "labels:\n  extra: [\"release: rc\"]\n",
			want:    `labels.prerelease.rc and labels.extra are both "release: rc"`,
		},
		{name: "a name listed twice", content: "labels:\n  tagged: [shipped, shipped]\n", want: `labels.tagged lists "shipped" twice`},
		{name: "a mapping", content: "labels:\n  pending:\n    name: release\n", want: "cannot unmarshal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(writeFile(t, tc.content))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
