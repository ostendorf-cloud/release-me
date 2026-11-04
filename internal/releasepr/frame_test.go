// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

// hostileFrame is a header and footer carrying everything a body is read
// back by: fenced notes, version and override blocks and details blocks,
// closed and unclosed.
var hostileFrame = releasepr.Frame{
	Header: "> Maintained by the platform team.\n\n```release-me-notes\nNot the notes.\n```\n\n" +
		"<details><summary>api v9.9.9</summary>\n\n## v9.9.9 (2020-01-01)\n\n</details>\n",
	Footer: "```release-me-version\n9.9.9\n```\n\n```release-me\nfeat: not an entry\n```\n\n" +
		"<details>\n<summary>Checklist</summary>\n\n- [ ] smoke test\n\n</details>\n</details>\n",
}

func TestFrameWrapsTheBodyBetweenHeaderAndFooter(t *testing.T) {
	t.Parallel()
	frame := releasepr.Frame{Header: "Reviewed by the release team.\n", Footer: "\nQuestions go to #releases."}
	got := frame.Wrap("Release v1.2.3\n\n<details>\n<summary>Changelog</summary>\n\n## v1.2.3\n\n</details>\n")
	want := "Reviewed by the release team.\n\nRelease v1.2.3\n\n<details>\n<summary>Changelog</summary>\n\n" +
		"## v1.2.3\n\n</details>\n\nQuestions go to #releases.\n"
	if got != want {
		t.Fatalf("Wrap() = %q, want %q", got, want)
	}
	if got := (releasepr.Frame{}).Wrap("Release v1.2.3\n"); got != "Release v1.2.3\n" {
		t.Fatalf("Wrap() without header or footer = %q, want the body unchanged", got)
	}
}

func TestFrameNeverChangesWhatABodyReadsBack(t *testing.T) {
	t.Parallel()
	section := "## v1.2.3 (2026-01-02)\n\n### Features\n\n- add the thing\n"
	shared := "#### api\n\n## v1.3.0 (2026-03-14)\n\n### Features\n\n- add the export endpoint\n\n" +
		"#### web\n\n## v0.4.1 (2026-03-14)\n\n### Bug Fixes\n\n- fix the login redirect\n"
	releases := []releasepr.Release{{Name: "api", Version: "1.3.0", Tag: "api/v1.3.0"}, {Name: "web", Version: "0.4.1", Tag: "web/v0.4.1"}}
	guide := releasepr.Guide{Show: true, Labels: releasepr.GuideLabels{Tagged: []string{"release: tagged"}, Alpha: "release: alpha", Beta: "release: beta", Rc: "release: rc"}}
	bodies := map[string]string{
		"single": releasepr.Body("1.2.3", "v1.2.3", guide, "Run the migration first.", "1.2.4", section),
		"shared": releasepr.SharedBody(releases, guide, true, "Run the migration first.", "api 1.3.0", shared),
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			framed := hostileFrame.Wrap(body)
			if !strings.HasPrefix(framed, "> Maintained by the platform team.") ||
				!strings.HasSuffix(framed, "</details>\n</details>\n") {
				t.Fatalf("Wrap() = %q, want the header first and the footer last", framed)
			}
			read := func(b string) []any {
				notes, notesOK := releasepr.ExtractNotes(b)
				override, overrideOK := releasepr.ExtractVersionOverride(b)
				date, dateOK := releasepr.ExtractDate(b)
				changelogText := releasepr.ExtractChangelog(b)
				return []any{
					notes, notesOK, override, overrideOK, date, dateOK, changelogText,
					releasepr.ReleaseBody(b), releasepr.ComponentNames(changelogText),
					releasepr.ComponentSection(changelogText, "web"),
				}
			}
			if got, want := read(framed), read(body); !reflect.DeepEqual(got, want) {
				t.Fatalf("framed body reads back %#v, want %#v", got, want)
			}
		})
	}
}

func TestCheckFrameTextRefusesWhatABodyIsReadBackBy(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"<details>\n<summary>Changelog</summary>\n\nnot the changelog\n</details>",
		"<details><summary>Changelog</summary></details>",
		"<!-- release-me-title-pattern: release ${component} ${version} -->",
	} {
		if err := releasepr.CheckFrameText(text); err == nil {
			t.Errorf("CheckFrameText(%q) error = nil, want an error", text)
		}
	}
	if err := releasepr.CheckFrameText(hostileFrame.Header + hostileFrame.Footer); err != nil {
		t.Fatalf("CheckFrameText() error = %v, want the escaped blocks accepted", err)
	}
}
