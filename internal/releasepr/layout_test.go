// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

var layouts = []releasepr.Layout{releasepr.LayoutHeadings, releasepr.LayoutDetails, releasepr.LayoutCompact}

// sharedComponents is a shared release pull request's three components,
// each section as its own CHANGELOG.md carries it, the linked version
// heading at level two.
var sharedComponents = []releasepr.ComponentChangelog{
	{
		Name: "duxt", Version: "0.10.0",
		Section: "## [v0.10.0](https://github.com/kirchDev/duxt/compare/duxt@v0.9.0...duxt@v0.10.0) (2026-10-29)\n\n" +
			"### Breaking Changes\n\n- **seo:** load the nuxt seo modules by name\n\n" +
			"### Bug Fixes\n\n- **deps:** update workspace dependencies within their minor ranges",
	},
	{
		Name: "duxt-typesense", Version: "0.10.0",
		Section: "## [v0.10.0](https://github.com/kirchDev/duxt/compare/duxt-typesense@v0.1.0...duxt-typesense@v0.10.0) (2026-10-29)\n\n" +
			"### Bug Fixes\n\n- **deps:** update workspace dependencies within their minor ranges",
	},
	{
		Name: "duxt-meilisearch", Version: "0.10.0",
		Section: "## v0.10.0 (2026-10-29)\n\n### Features\n\n- **search:** index the page title\n" +
			"  - **search:** weigh the title above the body",
	},
}

// lockedComponents is a locked family's components: the section headings
// alone, the family's version heading sitting above every component.
var lockedComponents = []releasepr.ComponentChangelog{
	{Name: "api", Version: "0.3.0", Section: "### Features\n\n- **export:** add the CSV endpoint ([#41](https://github.com/owner/repo/pull/41))"},
	{Name: "web", Version: "0.3.0", Section: "- Version bump without user facing changes."},
}

const lockedHeading = "## v0.3.0 (2026-01-02)"

func TestRenderComponentsRendersEachLayoutsShape(t *testing.T) {
	t.Parallel()
	components := []releasepr.ComponentChangelog{
		{Name: "api", Version: "1.3.0", Section: "## [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (2026-03-14)\n\n" +
			"### Features\n\n- add the export endpoint"},
		{Name: "web", Version: "0.4.1", Section: "## v0.4.1 (2026-03-14)\n\n### Bug Fixes\n\n- keep the session on reload"},
	}
	tests := []struct {
		layout releasepr.Layout
		want   string
	}{
		{
			layout: releasepr.LayoutHeadings,
			want: "## api\n\n### [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (2026-03-14)\n\n" +
				"#### Features\n\n- add the export endpoint\n\n" +
				"## web\n\n### v0.4.1 (2026-03-14)\n\n#### Bug Fixes\n\n- keep the session on reload",
		},
		{
			layout: releasepr.LayoutDetails,
			want: "<details><summary>api v1.3.0</summary>\n\n" +
				"## [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (2026-03-14)\n\n" +
				"### Features\n\n- add the export endpoint\n\n</details>\n\n" +
				"<details><summary>web v0.4.1</summary>\n\n## v0.4.1 (2026-03-14)\n\n### Bug Fixes\n\n- keep the session on reload\n\n</details>",
		},
		{
			layout: releasepr.LayoutCompact,
			want: "#### api\n\n## [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (2026-03-14)\n\n" +
				"### Features\n\n- add the export endpoint\n\n" +
				"#### web\n\n## v0.4.1 (2026-03-14)\n\n### Bug Fixes\n\n- keep the session on reload",
		},
	}
	for _, tc := range tests {
		t.Run(string(tc.layout), func(t *testing.T) {
			t.Parallel()
			if got := releasepr.RenderComponents(tc.layout, components); got != tc.want {
				t.Fatalf("RenderComponents(%s) =\n%s\nwant\n%s", tc.layout, got, tc.want)
			}
		})
	}
}

func TestRenderComponentsFallsBackToHeadingsForAnUnknownLayout(t *testing.T) {
	t.Parallel()
	got := releasepr.RenderComponents("", lockedComponents)
	if want := releasepr.RenderComponents(releasepr.LayoutHeadings, lockedComponents); got != want {
		t.Fatalf("RenderComponents(\"\") = %q, want the headings layout %q", got, want)
	}
}

// withoutVersions returns components with every Version cleared, the shape
// ParseComponents reads back: the version comes from the title.
func withoutVersions(components []releasepr.ComponentChangelog) []releasepr.ComponentChangelog {
	out := slices.Clone(components)
	for i := range out {
		out[i].Version = ""
	}
	return out
}

func TestParseComponentsReadsBackWhatEachLayoutRenders(t *testing.T) {
	t.Parallel()
	families := []struct {
		name       string
		preamble   string
		components []releasepr.ComponentChangelog
	}{
		{name: "shared", components: sharedComponents},
		{name: "locked", preamble: lockedHeading, components: lockedComponents},
	}
	for _, family := range families {
		for _, layout := range layouts {
			t.Run(family.name+"/"+string(layout), func(t *testing.T) {
				t.Parallel()
				text := releasepr.RenderComponents(layout, family.components)
				if family.preamble != "" {
					text = family.preamble + "\n\n" + text
				}
				if got := releasepr.DetectLayout(text); got != layout {
					t.Fatalf("DetectLayout() = %q, want %q", got, layout)
				}
				preamble, got := releasepr.ParseComponents(text)
				if preamble != family.preamble {
					t.Fatalf("preamble = %q, want %q", preamble, family.preamble)
				}
				if want := withoutVersions(family.components); !reflect.DeepEqual(got, want) {
					t.Fatalf("ParseComponents() =\n%+v\nwant\n%+v", got, want)
				}
			})
		}
	}
}

func TestDetectLayoutReadsASingleComponentChangelogAsCompactWithNoComponents(t *testing.T) {
	t.Parallel()
	text := "## [v1.2.3](https://github.com/owner/repo/compare/v1.2.2...v1.2.3) (2026-01-02)\n\n### Features\n\n- add the thing"
	if got := releasepr.DetectLayout(text); got != releasepr.LayoutCompact {
		t.Fatalf("DetectLayout() = %q, want compact", got)
	}
	preamble, components := releasepr.ParseComponents(text)
	if preamble != text || components != nil {
		t.Fatalf("ParseComponents() = (%q, %+v), want the whole text as the preamble and no component", preamble, components)
	}
}

func TestParseComponentsNeverTakesASectionsOwnHeadingsForAComponent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "the shifted section headings of the headings layout",
			text: "## api\n\n### v1.3.0 (2026-03-14)\n\n#### Features\n\n- add the export endpoint\n\n#### Bug Fixes\n\n- fix the export",
			want: []string{"api"},
		},
		{
			name: "the version heading of the compact layout",
			text: "#### api\n\n## v1.3.0 (2026-03-14)\n\n### Features\n\n- add the export endpoint\n\n" +
				"#### web\n\n## [v0.4.1-rc.1](https://github.com/owner/repo/compare/web/v0.4.0...web/v0.4.1-rc.1) (2026-03-14)\n\n- fix",
			want: []string{"api", "web"},
		},
		{
			name: "a locked family's combined version heading in the headings layout",
			text: "## v0.3.0 (2026-01-02)\n\n## api\n\n#### Features\n\n- add thing a\n\n## web\n\n- Version bump without user facing changes.",
			want: []string{"api", "web"},
		},
		{
			name: "every heading inside a details block",
			text: "<details><summary>api v1.3.0</summary>\n\n## v1.3.0 (2026-03-14)\n\n#### api\n\n## web\n\n- add\n\n</details>",
			want: []string{"api"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := releasepr.ComponentNames(tc.text); !slices.Equal(got, tc.want) {
				t.Fatalf("ComponentNames() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseComponentsKeepsAComponentNameWithSpacesInADetailsSummary(t *testing.T) {
	t.Parallel()
	text := releasepr.RenderComponents(releasepr.LayoutDetails, []releasepr.ComponentChangelog{
		{Name: "docs site", Version: "2.0.0-rc.1", Section: "## v2.0.0-rc.1 (2026-03-14)\n\n- Version bump without user facing changes."},
	})
	if got := releasepr.ComponentNames(text); !slices.Equal(got, []string{"docs site"}) {
		t.Fatalf("ComponentNames() = %v, want [docs site]", got)
	}
}

// sharedPRBody renders a shared release pull request body as pr does, in
// layout, with notes.
func sharedPRBody(layout releasepr.Layout, notes string) string {
	releases := make([]releasepr.Release, 0, len(sharedComponents))
	for _, c := range sharedComponents {
		releases = append(releases, releasepr.Release{Name: c.Name, Version: c.Version, Tag: c.Name + "@v" + c.Version})
	}
	return releasepr.SharedBody(releases, testGuideLabels, true, notes, "", releasepr.RenderComponents(layout, sharedComponents)+"\n")
}

var testGuideLabels = releasepr.GuideLabels{
	Tagged: "autorelease: tagged", Alpha: "release: alpha", Beta: "release: beta", Rc: "release: rc",
}

func TestEveryLayoutReadsBackTheSameComponentsAndDateFromAPullRequestBody(t *testing.T) {
	t.Parallel()
	for _, layout := range layouts {
		t.Run(string(layout), func(t *testing.T) {
			t.Parallel()
			body := sharedPRBody(layout, "Ship the search packages together.")
			changelogText := releasepr.ExtractChangelog(body)
			if got := releasepr.ComponentNames(changelogText); !slices.Equal(got, []string{"duxt", "duxt-typesense", "duxt-meilisearch"}) {
				t.Fatalf("ComponentNames() = %v", got)
			}
			for _, c := range sharedComponents {
				if got := releasepr.ComponentSection(changelogText, c.Name); got != c.Section {
					t.Errorf("ComponentSection(%s) =\n%s\nwant\n%s", c.Name, got, c.Section)
				}
			}
			date, ok := releasepr.ExtractDate(body)
			if !ok || !date.Equal(time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("ExtractDate() = (%v, %v), want 2026-10-29", date, ok)
			}
		})
	}
}

func TestEveryLayoutReadsBackALockedFamilysDate(t *testing.T) {
	t.Parallel()
	for _, layout := range layouts {
		t.Run(string(layout), func(t *testing.T) {
			t.Parallel()
			section := lockedHeading + "\n\n" + releasepr.RenderComponents(layout, lockedComponents) + "\n"
			body := releasepr.Body("0.3.0", "api/v0.3.0, web/v0.3.0", testGuideLabels, "", "", section)
			date, ok := releasepr.ExtractDate(body)
			if !ok || !date.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("ExtractDate() = (%v, %v), want 2026-01-02", date, ok)
			}
			if got := releasepr.ComponentSection(releasepr.ExtractChangelog(body), "api"); got != lockedComponents[0].Section {
				t.Fatalf("ComponentSection(api) = %q, want %q", got, lockedComponents[0].Section)
			}
		})
	}
}

func TestANotesBlockNeverChangesWhichComponentsAPullRequestReleases(t *testing.T) {
	t.Parallel()
	notes := []struct {
		name  string
		notes string
	}{
		{name: "a component heading", notes: "## docs\n\n#### docs\n\nThe docs move next week."},
		{name: "a details block", notes: "<details><summary>docs v9.0.0</summary>\n\nNot yet.\n\n</details>"},
		{name: "a stray closing tag", notes: "Read the migration guide first.\n\n</details>"},
		{name: "a changelog block of its own", notes: "<details>\n<summary>Changelog</summary>\n\n## docs\n\n</details>"},
	}
	for _, layout := range layouts {
		for _, n := range notes {
			t.Run(string(layout)+"/"+n.name, func(t *testing.T) {
				t.Parallel()
				rendered := sharedPRBody(layout, n.notes)
				appended := releasepr.SetNotes(sharedPRBody(layout, ""), n.notes)
				for _, body := range []string{rendered, appended} {
					changelogText := releasepr.ExtractChangelog(body)
					if got := releasepr.ComponentNames(changelogText); !slices.Equal(got, []string{"duxt", "duxt-typesense", "duxt-meilisearch"}) {
						t.Fatalf("ComponentNames() = %v, want the three components, body:\n%s", got, body)
					}
					if got := releasepr.ComponentSection(changelogText, "duxt-meilisearch"); got != sharedComponents[2].Section {
						t.Fatalf("ComponentSection(duxt-meilisearch) = %q, want %q", got, sharedComponents[2].Section)
					}
				}
			})
		}
	}
}

func TestExtractChangelogKeepsEveryNestedDetailsBlock(t *testing.T) {
	t.Parallel()
	body := sharedPRBody(releasepr.LayoutDetails, "")
	got := releasepr.ExtractChangelog(body)
	if want := releasepr.RenderComponents(releasepr.LayoutDetails, sharedComponents); got != want {
		t.Fatalf("ExtractChangelog() =\n%s\nwant\n%s", got, want)
	}
	if strings.Count(got, "</details>") != len(sharedComponents) {
		t.Fatalf("ExtractChangelog() = %q, want one closing tag per component", got)
	}
}
