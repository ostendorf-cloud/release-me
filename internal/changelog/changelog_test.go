// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package changelog_test

import (
	"testing"
	"time"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
)

var fixtureDate = time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC)

func TestRender(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		version     string
		compareURL  string
		entries     []changelog.Entry
		hiddenTypes []string
		want        string
	}{
		{
			name:    "orders sections and skips empty ones",
			version: "1.3.0",
			entries: []changelog.Entry{
				{Type: "revert", Description: "revert the bad cache change", PullRequestNumber: 5, URL: "https://example.com/pull/5"},
				{Type: "perf", Description: "skip the redundant walk", PullRequestNumber: 4, URL: "https://example.com/pull/4"},
				{Type: "fix", Description: "stop the panic on empty history", PullRequestNumber: 3, URL: "https://example.com/pull/3"},
				{Type: "feat", Description: "add the next command", PullRequestNumber: 2, URL: "https://example.com/pull/2"},
				{Type: "feat", Breaking: true, Description: "drop the v1 config", PullRequestNumber: 1, URL: "https://example.com/pull/1"},
			},
			want: `## v1.3.0 (2026-03-14)

### Breaking Changes

- drop the v1 config ([#1](https://example.com/pull/1))

### Features

- add the next command ([#2](https://example.com/pull/2))

### Bug Fixes

- stop the panic on empty history ([#3](https://example.com/pull/3))

### Performance

- skip the redundant walk ([#4](https://example.com/pull/4))

### Reverts

- revert the bad cache change ([#5](https://example.com/pull/5))
`,
		},
		{
			name:    "omits the bold scope when none is set",
			version: "0.2.0",
			entries: []changelog.Entry{
				{Type: "feat", Description: "add the next command", PullRequestNumber: 2, URL: "https://example.com/pull/2"},
			},
			want: "## v0.2.0 (2026-03-14)\n\n### Features\n\n- add the next command ([#2](https://example.com/pull/2))\n",
		},
		{
			name:    "bolds the scope when set",
			version: "0.2.0",
			entries: []changelog.Entry{
				{Type: "feat", Scope: "cli", Description: "add the next command", PullRequestNumber: 2, URL: "https://example.com/pull/2"},
			},
			want: "## v0.2.0 (2026-03-14)\n\n### Features\n\n- **cli:** add the next command ([#2](https://example.com/pull/2))\n",
		},
		{
			name:    "omits the link for a direct commit",
			version: "0.1.1",
			entries: []changelog.Entry{
				{Type: "fix", Description: "stop the panic on empty history"},
			},
			want: "## v0.1.1 (2026-03-14)\n\n### Bug Fixes\n\n- stop the panic on empty history\n",
		},
		{
			name:    "omits the link when a pull request number has no URL",
			version: "0.1.1",
			entries: []changelog.Entry{
				{Type: "fix", Description: "stop the panic on empty history", PullRequestNumber: 11},
			},
			want: "## v0.1.1 (2026-03-14)\n\n### Bug Fixes\n\n- stop the panic on empty history\n",
		},
		{
			name:    "hides entries of a hidden type",
			version: "0.2.0",
			entries: []changelog.Entry{
				{Type: "feat", Description: "add the next command"},
				{Type: "chore", Description: "bump a dependency"},
			},
			hiddenTypes: []string{"chore"},
			want:        "## v0.2.0 (2026-03-14)\n\n### Features\n\n- add the next command\n",
		},
		{
			name:    "returns empty when no entry is visible",
			version: "0.2.0",
			entries: []changelog.Entry{
				{Type: "chore", Description: "bump a dependency"},
			},
			hiddenTypes: []string{"chore"},
			want:        "",
		},
		{
			name:       "links the version to the compare view",
			version:    "0.9.0",
			compareURL: "https://github.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0",
			entries: []changelog.Entry{
				{Type: "feat", Description: "add the next command"},
			},
			want: "## [v0.9.0](https://github.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0) (2026-03-14)\n\n" +
				"### Features\n\n- add the next command\n",
		},
		{
			name:       "returns empty when no entry is visible, compare link or not",
			version:    "0.9.0",
			compareURL: "https://github.com/ostendorf-cloud/duxt/compare/duxt@v0.8.1...duxt@v0.9.0",
			entries: []changelog.Entry{
				{Type: "chore", Description: "bump a dependency"},
			},
			hiddenTypes: []string{"chore"},
			want:        "",
		},
		{
			name:    "returns empty for no entries",
			version: "0.2.0",
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := changelog.Render(tc.version, fixtureDate, tc.compareURL, tc.entries, changelog.Options{HiddenTypes: tc.hiddenTypes})
			if got != tc.want {
				t.Fatalf("Render() =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestRenderHeading(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		version    string
		compareURL string
		want       string
	}{
		{
			name:    "a first release carries no link",
			version: "0.1.0",
			want:    "## v0.1.0 (2026-03-14)",
		},
		{
			name:       "wraps the version in the compare link",
			version:    "1.4.0",
			compareURL: "https://github.com/ostendorf-cloud/release-me/compare/v1.3.2...v1.4.0",
			want:       "## [v1.4.0](https://github.com/ostendorf-cloud/release-me/compare/v1.3.2...v1.4.0) (2026-03-14)",
		},
		{
			name:       "keeps a pre-release suffix inside the link text",
			version:    "1.4.0-rc.2",
			compareURL: "https://github.com/ostendorf-cloud/release-me/compare/v1.4.0-rc.1...v1.4.0-rc.2",
			want:       "## [v1.4.0-rc.2](https://github.com/ostendorf-cloud/release-me/compare/v1.4.0-rc.1...v1.4.0-rc.2) (2026-03-14)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := changelog.RenderHeading(tc.version, fixtureDate, tc.compareURL, ""); got != tc.want {
				t.Fatalf("RenderHeading() = %q, want %q", got, tc.want)
			}
		})
	}
}
