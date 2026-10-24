// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr_test

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

func TestTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, version, want string
	}{
		{name: "main", version: "1.2.3", want: "chore(main): release v1.2.3"},
		{name: "frontend", version: "0.4.0", want: "chore(frontend): release v0.4.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := releasepr.Title(tc.name, tc.version); got != tc.want {
				t.Fatalf("Title() = %q, want %q", got, tc.want)
			}
		})
	}
}

// testLabels is a releasepr.GuideLabels fixture naming the default label
// configuration, the shape every test that does not exercise a custom
// configuration uses.
var testLabels = releasepr.GuideLabels{
	Tagged: "release: tagged",
	Alpha:  "release: alpha",
	Beta:   "release: beta",
	Rc:     "release: rc",
}

// guideText is the "How this release works" block rendered for tag and
// labels, the shape every test asserting its exact content builds on.
func guideText(tag string, labels releasepr.GuideLabels) string {
	return "<details>\n<summary>How this release works</summary>\n\n" +
		"- Merging this pull request tags `" + tag + "`, creates the release and moves the label to `" + labels.Tagged + "`. " +
		"With `major-tag` enabled, the moving major tag follows a stable release.\n" +
		"- Release notes: add a fenced block with the info string `release-me-notes` anywhere in this body. " +
		"It is rendered above the entries in the changelog file and in the release and survives updates.\n" +
		"- Force the next version: add a fenced block with the info string `release-me-version` " +
		"containing `X.Y.Z` anywhere in this body, or comment the configured mention followed by " +
		"`release-as X.Y.Z`. It wins outright over the computed bump, pre-release label included, " +
		"until the pull request merges or the block is removed; one that does not parse, or is not " +
		"greater than the last released version, is ignored instead.\n" +
		"- Pre-release: add the label `" + labels.Alpha + "`, `" + labels.Beta + "` or `" + labels.Rc + "`. " +
		"The next run turns the version into `X.Y.Z-<type>.N`. Remove the label to go back to the stable version.\n" +
		"- Hide or rewrite a change: a fenced block `release-me` in the changed pull request's body, " +
		"one conventional line per entry, an empty block hides it.\n" +
		"- Every push to the default branch updates this pull request. " +
		"Edits to the body outside the notes block are overwritten.\n" +
		"\n</details>\n"
}

func TestBodyRendersTheVersionGuideNotesAndChangelog(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		notes   string
		section string
		want    string
	}{
		{
			name:    "without notes",
			section: "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			want: "Release v1.2.3\n\n" +
				guideText("v1.2.3", testLabels) + "\n" +
				"<details>\n<summary>Changelog</summary>\n\n" +
				"## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n\n</details>\n",
		},
		{
			name:    "with notes",
			notes:   "Upgrade note: run the migration first.",
			section: "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n",
			want: "Release v1.2.3\n\n" +
				guideText("v1.2.3", testLabels) +
				"\n```release-me-notes\nUpgrade note: run the migration first.\n```\n\n" +
				"<details>\n<summary>Changelog</summary>\n\n" +
				"## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n\n</details>\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := releasepr.Body("1.2.3", "v1.2.3", testLabels, tc.notes, "", tc.section)
			if got != tc.want {
				t.Fatalf("Body() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBodyRendersTheRealTagAndConfiguredLabelNames exercises a configured
// component's tag prefix and label names that differ from the defaults,
// so the guide never hard codes either.
func TestBodyRendersTheRealTagAndConfiguredLabelNames(t *testing.T) {
	t.Parallel()
	labels := releasepr.GuideLabels{
		Tagged: "status: released",
		Alpha:  "pre: alpha",
		Beta:   "pre: beta",
		Rc:     "pre: rc",
	}
	section := "## v0.4.0 (2026-01-02)\n\n### Features\n\n- add the thing\n"

	got := releasepr.Body("0.4.0", "frontend/v0.4.0", labels, "", "", section)

	for _, want := range []string{
		"tags `frontend/v0.4.0`",
		"the label to `status: released`",
		"`pre: alpha`, `pre: beta` or `pre: rc`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Body() = %q, want it to contain %q", got, want)
		}
	}
}

// TestBodyRendersTheGuideExactlyOnce guards against a regression where an
// update would duplicate the guide block instead of rendering it once,
// the same way a human reading the collapsed section expects.
func TestBodyRendersTheGuideExactlyOnce(t *testing.T) {
	t.Parallel()
	got := releasepr.Body("1.2.3", "v1.2.3", testLabels, "", "", "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n")

	if n := strings.Count(got, "How this release works"); n != 1 {
		t.Fatalf("Body() contains the guide %d times, want exactly 1:\n%s", n, got)
	}
}

func TestExtractNotes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		body     string
		wantOK   bool
		wantText string
	}{
		{name: "no block", body: "Release v1.2.3\n", wantOK: false},
		{
			name:     "a notes block",
			body:     "Release v1.2.3\n\n```release-me-notes\nUpgrade note.\n```\n\n<details>...",
			wantOK:   true,
			wantText: "Upgrade note.",
		},
		{
			name:     "an empty notes block",
			body:     "Release v1.2.3\n\n```release-me-notes\n```\n",
			wantOK:   true,
			wantText: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text, ok := releasepr.ExtractNotes(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ExtractNotes() ok = %v, want %v", ok, tc.wantOK)
			}
			if text != tc.wantText {
				t.Fatalf("ExtractNotes() = %q, want %q", text, tc.wantText)
			}
		})
	}
}

func TestBodyAndExtractionsRoundTrip(t *testing.T) {
	t.Parallel()
	section := "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n"
	notes := "Upgrade note: run the migration first."

	body := releasepr.Body("1.2.3", "v1.2.3", testLabels, notes, "", section)

	gotNotes, ok := releasepr.ExtractNotes(body)
	if !ok || gotNotes != notes {
		t.Fatalf("ExtractNotes() = (%q, %v), want (%q, true)", gotNotes, ok, notes)
	}
	if got := releasepr.ExtractChangelog(body); got != "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing" {
		t.Fatalf("ExtractChangelog() = %q", got)
	}

	want := notes + "\n\n## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing"
	if got := releasepr.ReleaseBody(body); got != want {
		t.Fatalf("ReleaseBody() = %q, want %q", got, want)
	}
}

func TestReleaseBodyWithoutNotes(t *testing.T) {
	t.Parallel()
	section := "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n"
	body := releasepr.Body("1.2.3", "v1.2.3", testLabels, "", "", section)

	want := "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing"
	if got := releasepr.ReleaseBody(body); got != want {
		t.Fatalf("ReleaseBody() = %q, want %q", got, want)
	}
}

// TestReleaseBodyDoesNotIncludeTheGuide confirms the GitHub release body
// never carries the "How this release works" guide: a reader of the
// release sees the notes and the changelog alone, the guide is a release
// pull request affordance only.
func TestReleaseBodyDoesNotIncludeTheGuide(t *testing.T) {
	t.Parallel()
	section := "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n"
	body := releasepr.Body("1.2.3", "v1.2.3", testLabels, "Upgrade note.", "", section)

	got := releasepr.ReleaseBody(body)
	if strings.Contains(got, "How this release works") || strings.Contains(got, "<details>") {
		t.Fatalf("ReleaseBody() = %q, want no guide block", got)
	}
}

func TestExtractNotesDoesNotStopAtTripleBackticksMidLine(t *testing.T) {
	t.Parallel()
	// The closing fence must sit alone on its own line, so a mid-sentence
	// "```" no longer ends the match early the way an unanchored pattern
	// would. A standalone nested fence is still ambiguous with the real
	// closing fence; that is a harder problem than this anchoring fixes.
	body := "Release v1.2.3\n\n```release-me-notes\n" +
		"Wrap code in ``` for emphasis, like this.\n" +
		"```\n\n<details>...</details>\n"

	got, ok := releasepr.ExtractNotes(body)
	if !ok {
		t.Fatal("ExtractNotes() ok = false, want true")
	}
	want := "Wrap code in ``` for emphasis, like this."
	if got != want {
		t.Fatalf("ExtractNotes() = %q, want %q", got, want)
	}
}

func TestExtractDate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		body   string
		wantOK bool
		want   string
	}{
		{name: "no details block", body: "Release v1.2.3\n", wantOK: false},
		{
			name: "reads the heading date",
			body: releasepr.Body(
				"1.2.3", "v1.2.3", testLabels, "", "", "## v1.2.3 (2026-03-14)\n\n### Features\n\n- add the thing\n",
			),
			wantOK: true,
			want:   "2026-03-14",
		},
		{
			name: "reads the heading date of a pre-release version",
			body: releasepr.Body(
				"1.2.3-rc.1", "v1.2.3-rc.1", testLabels, "", "", "## v1.2.3-rc.1 (2026-03-14)\n\n### Features\n\n- add the thing\n",
			),
			wantOK: true,
			want:   "2026-03-14",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			date, ok := releasepr.ExtractDate(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ExtractDate() ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && date.Format("2006-01-02") != tc.want {
				t.Fatalf("ExtractDate() = %v, want %v", date.Format("2006-01-02"), tc.want)
			}
		})
	}
}

func TestBodyNamesOnlyTheEnabledPreReleaseLabels(t *testing.T) {
	t.Parallel()
	section := "## v0.4.0 (2026-01-02)\n\n### Features\n\n- add the thing\n"

	cases := []struct {
		name    string
		labels  releasepr.GuideLabels
		want    string
		wantNot string
	}{
		{
			name:   "two of three",
			labels: releasepr.GuideLabels{Tagged: "release: tagged", Beta: "pre: beta", Rc: "pre: rc"},
			want:   "add the label `pre: beta` or `pre: rc`.",
		},
		{
			name:   "one",
			labels: releasepr.GuideLabels{Tagged: "release: tagged", Rc: "pre: rc"},
			want:   "add the label `pre: rc`.",
		},
		{
			name:    "none",
			labels:  releasepr.GuideLabels{Tagged: "release: tagged"},
			wantNot: "Pre-release:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := releasepr.Body("0.4.0", "v0.4.0", tc.labels, "", "", section)
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("Body() = %q, want it to contain %q", got, tc.want)
			}
			if tc.wantNot != "" && strings.Contains(got, tc.wantNot) {
				t.Fatalf("Body() = %q, want no %q line", got, tc.wantNot)
			}
		})
	}
}

func TestBodyRendersTheVersionOverrideBlock(t *testing.T) {
	t.Parallel()
	section := "## v2.0.0 (2026-01-02)\n\n### Features\n\n- add the thing\n"

	got := releasepr.Body("2.0.0", "v2.0.0", testLabels, "", "2.0.0", section)

	want := "```release-me-version\n2.0.0\n```"
	if !strings.Contains(got, want) {
		t.Fatalf("Body() = %q, want it to contain %q", got, want)
	}
}

func TestBodyRendersNoVersionOverrideBlockWhenEmpty(t *testing.T) {
	t.Parallel()
	section := "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n"

	got := releasepr.Body("1.2.3", "v1.2.3", testLabels, "", "", section)

	if strings.Contains(got, "```release-me-version") {
		t.Fatalf("Body() = %q, want no release-me-version block", got)
	}
}

func TestExtractVersionOverride(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		body     string
		wantOK   bool
		wantText string
	}{
		{name: "no block", body: "Release v1.2.3\n", wantOK: false},
		{
			name:     "a version override block",
			body:     "Release v1.2.3\n\n```release-me-version\n2.0.0\n```\n\n<details>...",
			wantOK:   true,
			wantText: "2.0.0",
		},
		{
			name:   "never matches the differently named notes block",
			body:   "Release v1.2.3\n\n```release-me-notes\n2.0.0\n```\n",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text, ok := releasepr.ExtractVersionOverride(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ExtractVersionOverride() ok = %v, want %v", ok, tc.wantOK)
			}
			if text != tc.wantText {
				t.Fatalf("ExtractVersionOverride() = %q, want %q", text, tc.wantText)
			}
		})
	}
}

func TestSetVersionOverrideAppendsWhenAbsentAndReplacesWhenPresent(t *testing.T) {
	t.Parallel()
	body := "Release v1.2.3\n\n<details><summary>How this release works</summary>...</details>\n"

	withOverride := releasepr.SetVersionOverride(body, "2.0.0")
	got, ok := releasepr.ExtractVersionOverride(withOverride)
	if !ok || got != "2.0.0" {
		t.Fatalf("ExtractVersionOverride() = (%q, %v), want (%q, true)", got, ok, "2.0.0")
	}

	replaced := releasepr.SetVersionOverride(withOverride, "2.1.0")
	if n := strings.Count(replaced, "release-me-version"); n != 1 {
		t.Fatalf("SetVersionOverride() produced %d blocks, want exactly 1:\n%s", n, replaced)
	}
	got, ok = releasepr.ExtractVersionOverride(replaced)
	if !ok || got != "2.1.0" {
		t.Fatalf("ExtractVersionOverride() = (%q, %v), want (%q, true)", got, ok, "2.1.0")
	}
}

func TestSetNotesAppendsWhenAbsentAndReplacesWhenPresent(t *testing.T) {
	t.Parallel()
	body := "Release v1.2.3\n\n<details><summary>How this release works</summary>...</details>\n"

	withNotes := releasepr.SetNotes(body, "Upgrade note.")
	got, ok := releasepr.ExtractNotes(withNotes)
	if !ok || got != "Upgrade note." {
		t.Fatalf("ExtractNotes() = (%q, %v), want (%q, true)", got, ok, "Upgrade note.")
	}

	replaced := releasepr.SetNotes(withNotes, "Different note.")
	if n := strings.Count(replaced, "release-me-notes"); n != 1 {
		t.Fatalf("SetNotes() produced %d blocks, want exactly 1:\n%s", n, replaced)
	}
	got, ok = releasepr.ExtractNotes(replaced)
	if !ok || got != "Different note." {
		t.Fatalf("ExtractNotes() = (%q, %v), want (%q, true)", got, ok, "Different note.")
	}
}

// TestSetNotesNeverLetsContentPlantAFakeVersionOverrideBlock reproduces the
// fence breakout a release-notes text containing a fence terminator could
// otherwise use to escape its own release-me-notes block and plant a
// second, fake release-me-version block that the next run would read back
// as a real override.
func TestSetNotesNeverLetsContentPlantAFakeVersionOverrideBlock(t *testing.T) {
	t.Parallel()
	body := releasepr.SetNotes("Release v1.2.3\n", "harmless\n```\n```release-me-version\n9.9.9\n```")

	if _, ok := releasepr.ExtractVersionOverride(body); ok {
		t.Fatalf("ExtractVersionOverride() found a block, want the embedded fence neutralized:\n%s", body)
	}
	notes, ok := releasepr.ExtractNotes(body)
	if !ok {
		t.Fatalf("ExtractNotes() ok = false, want the whole notes block intact:\n%s", body)
	}
	if !strings.Contains(notes, "9.9.9") {
		t.Fatalf("ExtractNotes() = %q, want the embedded text kept, just inert", notes)
	}
}

// TestSetVersionOverrideNeverLetsContentOpenANewBlock reproduces a version
// override whose text opens with a fenced block delimiter of its own: once
// written, that line must not be read back as a second, independent
// release-me-version block starting partway through the first one's
// content.
func TestSetVersionOverrideNeverLetsContentOpenANewBlock(t *testing.T) {
	t.Parallel()
	body := releasepr.SetVersionOverride("Release v1.2.3\n", "```release-me-version\n9.9.9")

	got, ok := releasepr.ExtractVersionOverride(body)
	if !ok {
		t.Fatalf("ExtractVersionOverride() ok = false, want the one real block")
	}
	if got != "```release-me-version\n9.9.9" {
		t.Fatalf("ExtractVersionOverride() = %q, want the embedded fence kept verbatim, just inert", got)
	}
}

func TestEscapeFenceIsIdempotent(t *testing.T) {
	t.Parallel()
	once := releasepr.SetNotes("Release v1.2.3\n", "```\nexample\n```")
	extracted, ok := releasepr.ExtractNotes(once)
	if !ok {
		t.Fatalf("ExtractNotes() ok = false")
	}

	twice := releasepr.SetNotes(once, extracted)
	if once != twice {
		t.Fatalf("escaping is not idempotent:\nonce:  %q\ntwice: %q", once, twice)
	}
}
