// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

func TestNewTitlePatternRefusesAPatternReleaseCannotReadBack(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pattern string
		kind    releasepr.TitleKind
		want    string
	}{
		{name: "empty", pattern: "  ", kind: releasepr.TitlePerComponent, want: "is empty"},
		{name: "no version", pattern: "chore(${component}): release", kind: releasepr.TitlePerComponent, want: "carries no ${version}"},
		{name: "no component", pattern: "chore: release v${version}", kind: releasepr.TitlePerComponent, want: "carries no ${component}"},
		{name: "no components locked", pattern: "chore: release v${version}", kind: releasepr.TitleLocked, want: "carries no ${components}"},
		{name: "comment close", pattern: "chore(${component}): release ${version} -->", kind: releasepr.TitlePerComponent, want: `carries "-->"`},
		{name: "no version locked", pattern: "chore: release ${components}", kind: releasepr.TitleLocked, want: "carries no ${version}"},
		{name: "no releases shared", pattern: "chore: release ${components}", kind: releasepr.TitleShared, want: "carries no ${releases}"},
		{name: "version on a shared title", pattern: "chore: release ${releases} at ${version}", kind: releasepr.TitleShared, want: "carries ${version}, which is none of"},
		{name: "unknown placeholder", pattern: "chore${scope}: release ${version}", kind: releasepr.TitlePerComponent, want: "carries ${scope}, which is none of"},
		{name: "component on a locked title", pattern: "chore(${component}): release ${version}", kind: releasepr.TitleLocked, want: "carries ${component}"},
		{name: "placeholder twice", pattern: "${version}: release ${version}", kind: releasepr.TitlePerComponent, want: "carries ${version} twice"},
		{name: "adjacent placeholders", pattern: "release ${component}${version}", kind: releasepr.TitlePerComponent, want: "right after ${component}"},
		{name: "unclosed placeholder", pattern: "release ${component} ${version} ${branch", kind: releasepr.TitlePerComponent, want: `unclosed "${"`},
		{name: "two lines", pattern: "release ${version}\nmore", kind: releasepr.TitlePerComponent, want: "more than one line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := releasepr.NewTitlePattern(tc.pattern, tc.kind)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewTitlePattern(%q) error = %v, want it to contain %q", tc.pattern, err, tc.want)
			}
		})
	}
}

func TestTitlePatternRoundTripsEveryKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pattern string
		kind    releasepr.TitleKind
		values  releasepr.TitleValues
		want    string
	}{
		{
			name: "default per component", pattern: releasepr.DefaultTitlePattern, kind: releasepr.TitlePerComponent,
			values: releasepr.TitleValues{Version: "1.2.3", Component: "api"},
			want:   "chore(api): release v1.2.3",
		},
		{
			name: "release-please shaped", pattern: "chore(${branch}): release ${component} ${version}", kind: releasepr.TitlePerComponent,
			values: releasepr.TitleValues{Version: "2.0.0-rc.1", Component: "web-app", Branch: "main"},
			want:   "chore(main): release web-app 2.0.0-rc.1",
		},
		{
			name: "pre-release before literal text", pattern: "release v${version} (${component})", kind: releasepr.TitlePerComponent,
			values: releasepr.TitleValues{Version: "0.4.0-beta.2", Component: "cli"},
			want:   "release v0.4.0-beta.2 (cli)",
		},
		{
			name: "default locked", pattern: releasepr.DefaultLockedTitlePattern, kind: releasepr.TitleLocked,
			values: releasepr.TitleValues{Version: "1.0.0", Components: []string{"api", "web"}},
			want:   "chore: release v1.0.0 (api, web)",
		},
		{
			name: "locked onto a branch", pattern: "release(${branch}): ${version} for ${components}", kind: releasepr.TitleLocked,
			values: releasepr.TitleValues{Version: "1.0.0", Components: []string{"api", "web"}, Branch: "main"},
			want:   "release(main): 1.0.0 for api, web",
		},
		{
			name: "default shared", pattern: releasepr.DefaultSharedTitlePattern, kind: releasepr.TitleShared,
			values: releasepr.TitleValues{Releases: []releasepr.TitleRelease{{Name: "api", Version: "1.3.0"}, {Name: "web", Version: "0.4.1-rc.1"}}},
			want:   "chore: release api v1.3.0, web v0.4.1-rc.1",
		},
		{
			name: "shared onto a branch", pattern: "chore(${branch}): ship ${releases}", kind: releasepr.TitleShared,
			values: releasepr.TitleValues{
				Releases: []releasepr.TitleRelease{{Name: "api", Version: "1.3.0"}}, Branch: "main",
			},
			want: "chore(main): ship api v1.3.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := releasepr.NewTitlePattern(tc.pattern, tc.kind)
			if err != nil {
				t.Fatalf("NewTitlePattern(%q) error = %v", tc.pattern, err)
			}
			got, err := p.Render(tc.values)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("Render() = %q, want %q", got, tc.want)
			}
			back, ok := p.Parse(got)
			if !ok || !reflect.DeepEqual(back, tc.values) {
				t.Fatalf("Parse(%q) = (%+v, %v), want (%+v, true)", got, back, ok, tc.values)
			}
		})
	}
}

func TestTitlePatternRenderRefusesATitleThatReadsBackDifferently(t *testing.T) {
	t.Parallel()
	p, err := releasepr.NewTitlePattern("release ${component} ${version}-${branch}", releasepr.TitlePerComponent)
	if err != nil {
		t.Fatalf("NewTitlePattern() error = %v", err)
	}
	// "1.2.3-x-y" reads back as version 1.2.3-x on branch y.
	if got, err := p.Render(releasepr.TitleValues{Component: "api", Version: "1.2.3", Branch: "x-y"}); err == nil {
		t.Fatalf("Render() = %q, want an error", got)
	}
}

func TestTitlePatternsReadTheConfiguredPatternThenTheDefault(t *testing.T) {
	t.Parallel()
	patterns, err := releasepr.TitlePatterns("release ${component} ${version}", releasepr.TitlePerComponent)
	if err != nil {
		t.Fatalf("TitlePatterns() error = %v", err)
	}
	for title, want := range map[string]string{
		"release api 1.3.0":          "1.3.0",
		"chore(api): release v1.2.0": "1.2.0",
	} {
		v, _, ok := releasepr.ParseAny(patterns, title)
		if !ok || v.Version != want {
			t.Fatalf("ParseAny(%q) = (%+v, %v), want version %q", title, v, ok, want)
		}
	}
	if _, _, ok := releasepr.ParseAny(patterns, "feat: add the export"); ok {
		t.Fatal("ParseAny() matched a title neither pattern renders")
	}

	defaults, err := releasepr.TitlePatterns("", releasepr.TitleShared)
	if err != nil || len(defaults) != 1 || defaults[0].String() != releasepr.DefaultSharedTitlePattern {
		t.Fatalf("TitlePatterns(\"\") = (%v, %v), want the default alone", defaults, err)
	}
}

func TestReadPatternsReadsThePatternTheBodyRecords(t *testing.T) {
	t.Parallel()
	previous, err := releasepr.NewTitlePattern("release ${component} ${version}", releasepr.TitlePerComponent)
	if err != nil {
		t.Fatalf("NewTitlePattern() error = %v", err)
	}
	body := releasepr.Body("1.3.0", "api/v1.3.0", releasepr.Guide{Show: true, Labels: releasepr.GuideLabels{Tagged: []string{"release: tagged"}}}, "", "", "## v1.3.0 (2026-03-14)\n") +
		releasepr.TitlePatternMarker(previous, releasepr.TitlePerComponent)
	if got := releasepr.ExtractChangelog(body); got != "## v1.3.0 (2026-03-14)" {
		t.Fatalf("ExtractChangelog() = %q, want the marker kept out of the changelog", got)
	}
	if got := releasepr.ReleaseBody(body); strings.Contains(got, "release-me-title-pattern") {
		t.Fatalf("ReleaseBody() = %q, want the marker kept out of the release", got)
	}

	configured, err := releasepr.TitlePatterns("ship(${component}): ${version}", releasepr.TitlePerComponent)
	if err != nil {
		t.Fatalf("TitlePatterns() error = %v", err)
	}
	patterns := releasepr.ReadPatterns(configured, body, releasepr.TitlePerComponent)
	var got []string
	for _, p := range patterns {
		got = append(got, p.String())
	}
	want := []string{"release ${component} ${version}", releasepr.DefaultTitlePattern, "ship(${component}): ${version}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadPatterns() = %q, want %q", got, want)
	}
	v, _, ok := releasepr.ParseAny(patterns, "release api 1.3.0")
	if !ok || v.Version != "1.3.0" || v.Component != "api" {
		t.Fatalf("ParseAny() = (%+v, %v), want api 1.3.0", v, ok)
	}

	if marker := releasepr.TitlePatternMarker(releasepr.DefaultTitle(releasepr.TitleShared), releasepr.TitleShared); marker != "" {
		t.Fatalf("TitlePatternMarker(default) = %q, want none", marker)
	}
}

func TestUnmarkedLegacyTitlesPreferTheirDefaultShape(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		kind              releasepr.TitleKind
		configured, title string
		want              releasepr.TitleValues
	}{
		{name: "component", kind: releasepr.TitlePerComponent, configured: "chore(${component}): release v1.2.3-${version}", title: "chore(api): release v1.2.3-2.0.0", want: releasepr.TitleValues{Component: "api", Version: "1.2.3-2.0.0"}},
		{name: "shared", kind: releasepr.TitleShared, configured: "chore: release api v1.3.0, ${releases}", title: "chore: release api v1.3.0, web v0.3.1", want: releasepr.TitleValues{Releases: []releasepr.TitleRelease{{Name: "api", Version: "1.3.0"}, {Name: "web", Version: "0.3.1"}}}},
		{name: "locked", kind: releasepr.TitleLocked, configured: "chore: release v1.2.3-${version} (${components})", title: "chore: release v1.2.3-2.0.0 (api, web)", want: releasepr.TitleValues{Version: "1.2.3-2.0.0", Components: []string{"api", "web"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configured, err := releasepr.TitlePatterns(tc.configured, tc.kind)
			if err != nil {
				t.Fatal(err)
			}
			got, _, ok := releasepr.ParseAny(releasepr.ReadPatterns(configured, "", tc.kind), tc.title)
			if !ok || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parsed = %+v,%v; want %+v", got, ok, tc.want)
			}
		})
	}
}

func TestTitleMetadataIgnoresPreservedFreeText(t *testing.T) {
	t.Parallel()
	old := "build(${component}): ship ${version} after 2.0.0"
	quoted := "<!-- release-me-title-pattern: build(${component}): ship 1.2.3 after ${version} -->"
	for _, tc := range []struct{ name, prefix, suffix string }{
		{name: "notes with no guide", prefix: "```release-me-notes\n" + quoted + "\n```\n"},
		{name: "version block", prefix: "```release-me-version\n" + quoted + "\n```\n"},
		{name: "notes in a framed body", prefix: "Team header\n```release-me-notes\n" + quoted + "\n```\n", suffix: "Team footer\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			actual, err := releasepr.NewTitlePattern(old, releasepr.TitlePerComponent)
			if err != nil {
				t.Fatal(err)
			}
			body := tc.prefix + "Release v1.2.3\n" + tc.suffix + releasepr.TitlePatternMarker(actual, releasepr.TitlePerComponent)
			got, ok := releasepr.BodyTitlePattern(body, releasepr.TitlePerComponent)
			if !ok || got.String() != old {
				t.Fatalf("pattern = %q,%v", got.String(), ok)
			}
			configured, err := releasepr.TitlePatterns("", releasepr.TitlePerComponent)
			if err != nil {
				t.Fatal(err)
			}
			values, _, ok := releasepr.ParseAny(releasepr.ReadPatterns(configured, body, releasepr.TitlePerComponent), "build(api): ship 1.2.3 after 2.0.0")
			if !ok || values.Version != "1.2.3" {
				t.Fatalf("version = %+v,%v", values, ok)
			}
			if p, ok := releasepr.BodyTitlePattern(tc.prefix, releasepr.TitlePerComponent); ok {
				t.Fatalf("quoted-only marker = %q", p.String())
			}
		})
	}
}

// TestTitleMetadataIgnoresAPreservedMarkerInACRLFBody is
// TestTitleMetadataIgnoresPreservedFreeText on a body saved with CRLF line
// endings throughout: BodyTitlePattern's titleMarker pattern and its
// notesBlock/versionBlock exclusion list both carry the same "\n"-only
// anchors ExtractNotes needed normalizeLineEndings for, so without
// normalizing first neither the quoted marker inside the notes block nor
// the real one further down is found at all.
func TestTitleMetadataIgnoresAPreservedMarkerInACRLFBody(t *testing.T) {
	t.Parallel()
	old := "build(${component}): ship ${version} after 2.0.0"
	quoted := "<!-- release-me-title-pattern: build(${component}): ship 1.2.3 after ${version} -->"
	actual, err := releasepr.NewTitlePattern(old, releasepr.TitlePerComponent)
	if err != nil {
		t.Fatal(err)
	}
	marker := strings.ReplaceAll(releasepr.TitlePatternMarker(actual, releasepr.TitlePerComponent), "\n", "\r\n")
	body := "```release-me-notes\r\n" + quoted + "\r\n```\r\n" + "Release v1.2.3\r\n" + marker

	got, ok := releasepr.BodyTitlePattern(body, releasepr.TitlePerComponent)
	if !ok || got.String() != old {
		t.Fatalf("pattern = %q,%v, want %q,true", got.String(), ok, old)
	}
}
