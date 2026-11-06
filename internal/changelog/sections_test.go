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
