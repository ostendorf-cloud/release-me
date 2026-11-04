// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package changelog_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
)

func TestSectionsRendersNoHeading(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{Type: "feat", Description: "add the next command"}}

	got := changelog.Sections(entries, changelog.Options{})
	want := "### Features\n\n- add the next command"
	if got != want {
		t.Fatalf("Sections() = %q, want %q", got, want)
	}
}

func TestSectionsReturnsEmptyWhenNoEntryIsVisible(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{Type: "chore", Description: "bump a dependency"}}

	got := changelog.Sections(entries, changelog.Options{HiddenTypes: []string{"chore"}})
	if got != "" {
		t.Fatalf("Sections() = %q, want empty", got)
	}
}

func TestSectionsRendersSubEntriesIndentedUnderTheirParent(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{
		Type: "feat", Description: "add the next command", PullRequestNumber: 5, URL: "https://example.com/pull/5",
		SubEntries: []changelog.Entry{
			{Type: "feat", Description: "wire up the flag"},
			{Type: "fix", Description: "handle the empty case"},
		},
	}}

	got := changelog.Sections(entries, changelog.Options{})
	want := "### Features\n\n" +
		"- add the next command ([#5](https://example.com/pull/5))\n" +
		"  - wire up the flag\n" +
		"  - handle the empty case"
	if got != want {
		t.Fatalf("Sections() = %q, want %q", got, want)
	}
}

func TestSectionsDropsAHiddenTypeSubEntry(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{
		Type: "feat", Description: "add the next command",
		SubEntries: []changelog.Entry{
			{Type: "feat", Description: "wire up the flag"},
			{Type: "chore", Description: "fix a typo in a comment"},
		},
	}}

	got := changelog.Sections(entries, changelog.Options{HiddenTypes: []string{"chore"}})
	want := "### Features\n\n- add the next command\n  - wire up the flag"
	if got != want {
		t.Fatalf("Sections() = %q, want %q", got, want)
	}
}

func TestSectionsRendersAHiddenTypeParentWhenASubEntryIsVisible(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{
		// chore is hidden by default and matches no section on its own;
		// its visible feat sub entry must still surface the whole line.
		Type: "chore", Description: "merge widget work", PullRequestNumber: 10, URL: "https://example.com/pull/10",
		SubEntries: []changelog.Entry{
			{Type: "feat", Description: "add the widget"},
			{Type: "fix", Description: "correct a bug"},
		},
	}}

	got := changelog.Sections(entries, changelog.Options{HiddenTypes: []string{"chore"}})
	want := "### Features\n\n" +
		"- merge widget work ([#10](https://example.com/pull/10))\n" +
		"  - add the widget\n" +
		"  - correct a bug"
	if got != want {
		t.Fatalf("Sections() = %q, want %q", got, want)
	}
}

func TestSectionsDropsAHiddenTypeParentWithNoVisibleSubEntry(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{
		Type: "chore", Description: "merge chore work",
		SubEntries: []changelog.Entry{{Type: "docs", Description: "update a comment"}},
	}}

	got := changelog.Sections(entries, changelog.Options{HiddenTypes: []string{"chore", "docs"}})
	if got != "" {
		t.Fatalf("Sections() = %q, want empty: neither the parent nor its sub entry is visible", got)
	}
}

func TestSectionsIsDateFreeSoTwoRendersOnDifferentDaysMatch(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{Type: "feat", Description: "add the next command"}}

	a := changelog.Sections(entries, changelog.Options{})
	b := changelog.Sections(entries, changelog.Options{})
	if a != b {
		t.Fatalf("Sections() is not stable: %q != %q", a, b)
	}

	// Render embeds the date; Sections, used for comparison, never does.
	renderedToday := changelog.Render("0.1.0", fixtureDate, "", entries, changelog.Options{})
	if a == renderedToday {
		t.Fatalf("Sections() unexpectedly equals the dated Render() output")
	}
}

func TestSectionsRendersConfiguredSectionsInTheirOrder(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{
		{Type: "feat", Description: "add the widget"},
		{Type: "deps", Description: "bump yaml.v3 to 3.0.1"},
		{Type: "fix", Description: "handle the empty case"},
		{Type: "feat", Description: "drop the v1 flag", Breaking: true},
		{Type: "perf", Description: "cache the tag list"},
	}
	cases := []struct {
		name     string
		sections []changelog.Section
		want     string
	}{
		{
			name: "renamed and reordered, with a type the defaults never render",
			sections: []changelog.Section{
				{Heading: "Dependencies", Types: []string{"deps"}},
				{Heading: "New", Types: []string{"feat"}},
			},
			want: "### Breaking Changes\n\n- drop the v1 flag\n\n" +
				"### Dependencies\n\n- bump yaml.v3 to 3.0.1\n\n" +
				"### New\n\n- add the widget",
		},
		{
			name:     "several types under one heading keep the history order",
			sections: []changelog.Section{{Heading: "Changes", Types: []string{"fix", "feat"}}},
			want: "### Breaking Changes\n\n- drop the v1 flag\n\n" +
				"### Changes\n\n- add the widget\n- handle the empty case",
		},
		{
			name:     "an empty list keeps only Breaking Changes",
			sections: []changelog.Section{},
			want:     "### Breaking Changes\n\n- drop the v1 flag",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := changelog.Sections(entries, changelog.Options{Sections: tc.sections})
			if got != tc.want {
				t.Fatalf("Sections() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSectionsReadsBackUnderARenamedSection(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{Type: "deps", Description: "bump yaml.v3 to 3.0.1"}}
	opts := changelog.Options{Sections: []changelog.Section{{Heading: "Dependencies", Types: []string{"deps"}}}}

	rendered := changelog.Render("1.2.0", fixtureDate, "https://github.com/owner/repo/compare/v1.1.0...v1.2.0", entries, opts)
	body, ok := changelog.TopSection(changelog.Prepend("", rendered))
	if !ok || body != changelog.Sections(entries, opts) {
		t.Fatalf("TopSection() = %q, %v, want the rendered sections back", body, ok)
	}
	if h, ok := changelog.ParseHeading(rendered); !ok || h.Version != "1.2.0" {
		t.Fatalf("ParseHeading() = %+v, %v, want version 1.2.0", h, ok)
	}
}
