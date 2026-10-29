// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

func TestBodyLeavesTheGuideOutWithoutGuideLabels(t *testing.T) {
	t.Parallel()
	section := "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n"
	cases := []struct {
		name            string
		notes           string
		versionOverride string
		want            string
	}{
		{
			name: "without notes or an override",
			want: "Release v1.2.3\n\n" +
				"<details>\n<summary>Changelog</summary>\n\n" +
				"## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n\n</details>\n",
		},
		{
			name:            "with notes and an override",
			notes:           "Upgrade note: run the migration first.",
			versionOverride: "1.2.3",
			want: "Release v1.2.3\n\n" +
				"```release-me-version\n1.2.3\n```\n\n" +
				"```release-me-notes\nUpgrade note: run the migration first.\n```\n\n" +
				"<details>\n<summary>Changelog</summary>\n\n" +
				"## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n\n</details>\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := releasepr.Body("1.2.3", "v1.2.3", nil, tc.notes, tc.versionOverride, section)
			if got != tc.want {
				t.Fatalf("Body() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSharedBodyLeavesTheGuideOutWithoutGuideLabels(t *testing.T) {
	t.Parallel()
	releases := []releasepr.Release{
		{Name: "api", Version: "1.3.0", Tag: "api/v1.3.0"},
		{Name: "web", Version: "0.4.1", Tag: "web/v0.4.1"},
	}
	section := "## api\n\n### v1.3.0 (2026-03-14)\n\n#### Features\n\n- add the export endpoint\n\n" +
		"## web\n\n### v0.4.1 (2026-03-14)\n\n#### Bug Fixes\n\n- fix the login redirect\n"

	got := releasepr.SharedBody(releases, nil, true, "", "", section)

	want := "Release api v1.3.0, web v0.4.1\n\n" +
		"<details>\n<summary>Changelog</summary>\n\n" + strings.TrimRight(section, "\n") + "\n\n</details>\n"
	if got != want {
		t.Fatalf("SharedBody() = %q, want %q", got, want)
	}
}

// guideBodies is a release pull request body of every kind pr renders, a
// single component, a shared one and a locked family in every layout, each
// carrying notes and a version override, rendered with guide, nil for
// "release-pull-request-guide: false".
func guideBodies(guide *releasepr.GuideLabels) map[string]string {
	single := "## [v1.4.0](https://github.com/owner/repo/compare/v1.3.2...v1.4.0) (2026-10-29)\n\n" +
		"### Features\n\n- add the export endpoint (#41)\n"
	bodies := map[string]string{
		"single": releasepr.Body("1.4.0", "v1.4.0", guide, "Run the migration before upgrading.", "1.4.0", single),
	}
	releases := make([]releasepr.Release, 0, len(sharedComponents))
	for _, c := range sharedComponents {
		releases = append(releases, releasepr.Release{Name: c.Name, Version: c.Version, Tag: c.Name + "@v" + c.Version})
	}
	for _, layout := range layouts {
		bodies["shared/"+string(layout)] = releasepr.SharedBody(
			releases, guide, true, "Ship the search packages together.", "duxt-typesense 0.5.0",
			releasepr.RenderComponents(layout, sharedComponents)+"\n",
		)
		bodies["locked/"+string(layout)] = releasepr.Body(
			"0.3.0", "api/v0.3.0, web/v0.3.0", guide, "Locked notes.", "0.3.0",
			lockedHeading+"\n\n"+releasepr.RenderComponents(layout, lockedComponents)+"\n",
		)
	}
	return bodies
}

// readBack is everything a command reads out of a release pull request
// body.
type readBack struct {
	NotesText            string
	NotesOK              bool
	Override             string
	OverrideOK           bool
	Changelog            string
	Date                 time.Time
	DateOK               bool
	Components           []string
	Sections             []string
	Release              string
	NotesAfterSet        string
	OverrideAfterSet     string
	ComponentOverrideSet string
}

func readBody(body string) readBack {
	var r readBack
	r.NotesText, r.NotesOK = releasepr.ExtractNotes(body)
	r.Override, r.OverrideOK = releasepr.ExtractVersionOverride(body)
	r.Changelog = releasepr.ExtractChangelog(body)
	r.Date, r.DateOK = releasepr.ExtractDate(body)
	r.Components = releasepr.ComponentNames(r.Changelog)
	for _, name := range r.Components {
		r.Sections = append(r.Sections, releasepr.ComponentSection(r.Changelog, name))
	}
	r.Release = releasepr.ReleaseBody(body)
	r.NotesAfterSet, _ = releasepr.ExtractNotes(releasepr.SetNotes(body, "Set by a comment."))
	r.OverrideAfterSet, _ = releasepr.ExtractVersionOverride(releasepr.SetVersionOverride(body, "2.0.0"))
	r.ComponentOverrideSet, _ = releasepr.ExtractVersionOverride(releasepr.SetComponentVersionOverride(body, "duxt", "1.0.0"))
	return r
}

func TestABodyReadsBackTheSameWithTheGuideOrWithout(t *testing.T) {
	t.Parallel()
	with := guideBodies(&testGuideLabels)
	without := guideBodies(nil)
	for name, withBody := range with {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			withoutBody := without[name]
			if strings.Contains(withoutBody, "How this release works") {
				t.Fatalf("body without the guide carries it:\n%s", withoutBody)
			}
			got, want := readBody(withoutBody), readBody(withBody)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("read back without the guide = %+v\nwant, with the guide, %+v", got, want)
			}
			if !want.NotesOK || !want.OverrideOK || want.Changelog == "" || !want.DateOK {
				t.Fatalf("read back with the guide = %+v, want notes, an override, a changelog and a date", want)
			}
		})
	}
}

func TestTheGuideIsTheOnlyPartTheKeyChanges(t *testing.T) {
	t.Parallel()
	with := guideBodies(&testGuideLabels)
	without := guideBodies(nil)
	for name, withBody := range with {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start := strings.Index(withBody, "<details>\n<summary>How this release works</summary>")
			end := strings.Index(withBody, "\n</details>\n") + len("\n</details>\n")
			if start < 0 || end < start {
				t.Fatalf("body with the guide carries no guide:\n%s", withBody)
			}
			// The guide block and the blank line that separates it from the
			// headline.
			stripped := withBody[:start-1] + withBody[end:]
			if stripped != without[name] {
				t.Fatalf("body with the guide removed =\n%q\nwant the body without it\n%q", stripped, without[name])
			}
		})
	}
}
