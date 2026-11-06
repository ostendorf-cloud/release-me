// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package changelog_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
)

func TestTopSection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		wantOK  bool
		want    string
	}{
		{name: "no heading at all", content: "# Changelog\n", wantOK: false},
		{name: "empty file", content: "", wantOK: false},
		{
			name:    "a single section",
			content: "# Changelog\n\n## v0.1.0 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			wantOK:  true,
			want:    "### Features\n\n- add the thing",
		},
		{
			name: "stops before the next version heading",
			content: "# Changelog\n\n## v0.2.0 (2026-02-03)\n\n### Bug Fixes\n\n- stop the panic\n\n" +
				"## v0.1.0 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			wantOK: true,
			want:   "### Bug Fixes\n\n- stop the panic",
		},
		{
			name: "a linked heading, stopping before an older unlinked one",
			content: "# Changelog\n\n## [v0.2.0](https://github.com/ostendorf-cloud/release-me/compare/v0.1.0...v0.2.0) (2026-02-03)\n\n" +
				"### Bug Fixes\n\n- stop the panic\n\n## v0.1.0 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			wantOK: true,
			want:   "### Bug Fixes\n\n- stop the panic",
		},
		{
			name:    "a non-section line under the heading",
			content: "# Changelog\n\n## v0.1.0 (2026-01-02)\n\n- Version bump without user facing changes.\n",
			wantOK:  true,
			want:    "- Version bump without user facing changes.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := changelog.TopSection(tc.content)
			if ok != tc.wantOK {
				t.Fatalf("TopSection() ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("TopSection() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTopSectionMatchesSectionsForARoundTrippedEntry(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{Type: "feat", Description: "add the next command"}}
	body := changelog.Sections(entries, changelog.Options{})

	rendered := changelog.Render("0.1.0", fixtureDate, "", entries, changelog.Options{})
	changelogFile := changelog.Prepend("", rendered)

	got, ok := changelog.TopSection(changelogFile)
	if !ok || got != body {
		t.Fatalf("TopSection() = (%q, %v), want (%q, true)", got, ok, body)
	}
}

func TestHeadingVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		wantOK  bool
		want    string
	}{
		{name: "no heading at all", content: "# Changelog\n", wantOK: false},
		{name: "empty file", content: "", wantOK: false},
		{
			name:    "a stable version",
			content: "# Changelog\n\n## v0.1.0 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			wantOK:  true,
			want:    "0.1.0",
		},
		{
			name:    "a pre-release version",
			content: "# Changelog\n\n## v0.1.0-rc.2 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			wantOK:  true,
			want:    "0.1.0-rc.2",
		},
		{
			name:    "a version with build metadata",
			content: "# Changelog\n\n## v1.2.3+build.5 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			wantOK:  true,
			want:    "1.2.3+build.5",
		},
		{
			name:    "a pre-release version with build metadata",
			content: "# Changelog\n\n## v1.2.3-rc.1+build.5 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			wantOK:  true,
			want:    "1.2.3-rc.1+build.5",
		},
		{
			name: "a linked heading",
			content: "# Changelog\n\n## [v0.9.0](https://github.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0) (2026-10-01)\n\n" +
				"### Features\n\n- add the thing\n",
			wantOK: true,
			want:   "0.9.0",
		},
		{
			name:    "a linked heading of a pre-release version",
			content: "# Changelog\n\n## [v1.0.0-rc.2](https://github.com/o/r/compare/v1.0.0-rc.1...v1.0.0-rc.2) (2026-10-01)\n",
			wantOK:  true,
			want:    "1.0.0-rc.2",
		},
		{
			name:    "a link text that is not a version",
			content: "# Changelog\n\n## [Unreleased](https://github.com/o/r/compare/v1.0.0...HEAD) (2026-10-01)\n",
			wantOK:  false,
		},
		{
			name: "reads the first heading, not a later one",
			content: "# Changelog\n\n## v0.2.0 (2026-02-03)\n\n### Bug Fixes\n\n- stop the panic\n\n" +
				"## v0.1.0 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			wantOK: true,
			want:   "0.2.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := changelog.HeadingVersion(tc.content)
			if ok != tc.wantOK {
				t.Fatalf("HeadingVersion() ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("HeadingVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHeadingVersionMatchesTheVersionRenderPrepended(t *testing.T) {
	t.Parallel()
	entries := []changelog.Entry{{Type: "feat", Description: "add the next command"}}

	rendered := changelog.Render("0.1.0-alpha.1", fixtureDate, "", entries, changelog.Options{})
	changelogFile := changelog.Prepend("", rendered)

	got, ok := changelog.HeadingVersion(changelogFile)
	if !ok || got != "0.1.0-alpha.1" {
		t.Fatalf("HeadingVersion() = (%q, %v), want (%q, true)", got, ok, "0.1.0-alpha.1")
	}
}

func TestParseHeading(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		wantOK  bool
		want    changelog.Heading
	}{
		{name: "empty", content: "", wantOK: false},
		{
			name:    "an unlinked heading from before the compare link",
			content: "# Changelog\n\n## v0.2.1 (2026-09-12)\n\n### Bug Fixes\n\n- stop the panic\n",
			wantOK:  true,
			want:    changelog.Heading{Version: "0.2.1", Date: "2026-09-12"},
		},
		{
			name: "a linked heading",
			content: "# Changelog\n\n## [v0.9.0](https://github.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0) (2026-10-01)\n\n" +
				"### Features\n\n- add the thing\n",
			wantOK: true,
			want: changelog.Heading{
				Version:    "0.9.0",
				CompareURL: "https://github.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0",
				Date:       "2026-10-01",
			},
		},
		{
			name:    "a section on its own, without the changelog header",
			content: "## [v1.2.3](https://github.com/o/r/compare/v1.2.2...v1.2.3) (2026-03-14)\n\n### Features\n",
			wantOK:  true,
			want:    changelog.Heading{Version: "1.2.3", CompareURL: "https://github.com/o/r/compare/v1.2.2...v1.2.3", Date: "2026-03-14"},
		},
		{
			name:    "a link with a space in its target",
			content: "## [v1.2.3](https://github.com/o/r/compare/v1.2.2 v1.2.3) (2026-03-14)\n",
			wantOK:  false,
		},
		{
			name:    "a link with parentheses in its target",
			content: "## [v0.2.0](https://github.com/o/r/compare/(api)v0.1.0...(api)v0.2.0) (2026-03-14)\n",
			wantOK:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := changelog.ParseHeading(tc.content)
			if ok != tc.wantOK {
				t.Fatalf("ParseHeading() ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("ParseHeading() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseHeadingReadsBackWhatRenderHeadingWrote(t *testing.T) {
	t.Parallel()
	for _, compareURL := range []string{"", "https://github.com/o/r/compare/web@v2.0.0...web@v2.1.0-beta.1"} {
		h, ok := changelog.ParseHeading(changelog.RenderHeading("2.1.0-beta.1", fixtureDate, compareURL))
		want := changelog.Heading{Version: "2.1.0-beta.1", CompareURL: compareURL, Date: "2026-03-14"}
		if !ok || h != want {
			t.Fatalf("ParseHeading(RenderHeading(%q)) = (%+v, %v), want (%+v, true)", compareURL, h, ok, want)
		}
	}
}
