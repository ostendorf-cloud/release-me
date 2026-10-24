// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/config"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "release-me.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoadReturnsDefaultsWhenTheFileIsMissing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "release-me.yaml")

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, config.Default()) {
		t.Fatalf("Load() = %+v, want %+v", got, config.Default())
	}
}

func TestDefaultSetsTheChangelogPathAndLabels(t *testing.T) {
	t.Parallel()
	got := config.Default()
	if got.ChangelogPath != "CHANGELOG.md" {
		t.Fatalf("ChangelogPath = %q, want %q", got.ChangelogPath, "CHANGELOG.md")
	}
	want := config.Labels{
		Pending: "release: pending", Tagged: "release: tagged",
		Alpha: "release: alpha", Beta: "release: beta", Rc: "release: rc",
	}
	if got.Labels != want {
		t.Fatalf("Labels = %+v, want %+v", got.Labels, want)
	}
}

func TestDefaultSetsTheAuthor(t *testing.T) {
	t.Parallel()
	got := config.Default().Author
	want := config.Author{Name: "release-me", Email: "release-me[bot]@users.noreply.github.com"}
	if got != want {
		t.Fatalf("Author = %+v, want %+v", got, want)
	}
}

func TestDefaultSetsTheCommentMention(t *testing.T) {
	t.Parallel()
	got := config.Default().CommentMention
	if got != "@release-me" {
		t.Fatalf("CommentMention = %q, want %q", got, "@release-me")
	}
}

func TestLoadReadsACustomCommentMention(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "comment-mention: \"@my-release-bot\"\n")

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.CommentMention != "@my-release-bot" {
		t.Fatalf("CommentMention = %q, want %q", got.CommentMention, "@my-release-bot")
	}
}

func TestLoadReadsConfiguredKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    config.Config
	}{
		{
			name:    "applies defaults for keys left out",
			content: "initial-version: 0.5.0\n",
			want: config.Config{
				InitialVersion:  "0.5.0",
				HiddenTypes:     config.DefaultHiddenTypes,
				FallbackType:    config.DefaultFallbackType,
				TokenEnv:        config.DefaultTokenEnv,
				ChangelogPath:   config.DefaultChangelogPath,
				ChangelogSource: config.DefaultChangelogSource,
				Labels: config.Labels{
					Pending: config.DefaultPendingLabel, Tagged: config.DefaultTaggedLabel,
					Alpha: config.DefaultAlphaLabel, Beta: config.DefaultBetaLabel, Rc: config.DefaultRcLabel,
				},
				Author:         config.Author{Name: config.DefaultAuthorName, Email: config.DefaultAuthorEmail},
				Versioning:     config.DefaultVersioning,
				CommentMention: config.DefaultCommentMention,
			},
		},
		{
			name: "reads every known key",
			content: `initial-version: 0.2.0
bump-minor-pre-major: true
hidden-types: [chore, docs]
fallback-type: misc
token-env: RELEASE_TOKEN
changelog: HISTORY.md
changelog-source: all
labels:
  pending: needs-release
  tagged: released
  alpha: needs-alpha
  beta: needs-beta
  rc: needs-rc
author:
  name: renovate
  email: renovate@example.com
major-tag: true
files:
  - path: cmd/cli/version.go
  - path: deploy/values.yaml
    updater: marker
  - path: internal/version/version.go
    updater: go
    key: Version
`,
			want: config.Config{
				InitialVersion:    "0.2.0",
				BumpMinorPreMajor: true,
				HiddenTypes:       []string{"chore", "docs"},
				FallbackType:      "misc",
				TokenEnv:          "RELEASE_TOKEN",
				ChangelogPath:     "HISTORY.md",
				ChangelogSource:   "all",
				Labels: config.Labels{
					Pending: "needs-release", Tagged: "released",
					Alpha: "needs-alpha", Beta: "needs-beta", Rc: "needs-rc",
				},
				Author:         config.Author{Name: "renovate", Email: "renovate@example.com"},
				MajorTag:       true,
				Versioning:     config.DefaultVersioning,
				CommentMention: config.DefaultCommentMention,
				Files: []config.File{
					{Path: "cmd/cli/version.go", Updater: "marker"},
					{Path: "deploy/values.yaml", Updater: "marker"},
					{Path: "internal/version/version.go", Updater: "go", Key: "Version"},
				},
			},
		},
		{
			name:    "reads the fallback type alone",
			content: "fallback-type: misc\n",
			want: config.Config{
				InitialVersion:  config.DefaultInitialVersion,
				HiddenTypes:     config.DefaultHiddenTypes,
				FallbackType:    "misc",
				TokenEnv:        config.DefaultTokenEnv,
				ChangelogPath:   config.DefaultChangelogPath,
				ChangelogSource: config.DefaultChangelogSource,
				Labels: config.Labels{
					Pending: config.DefaultPendingLabel, Tagged: config.DefaultTaggedLabel,
					Alpha: config.DefaultAlphaLabel, Beta: config.DefaultBetaLabel, Rc: config.DefaultRcLabel,
				},
				Author:         config.Author{Name: config.DefaultAuthorName, Email: config.DefaultAuthorEmail},
				Versioning:     config.DefaultVersioning,
				CommentMention: config.DefaultCommentMention,
			},
		},
		{
			name:    "defaults a label left out of the labels block",
			content: "labels:\n  pending: needs-release\n",
			want: config.Config{
				InitialVersion:  config.DefaultInitialVersion,
				HiddenTypes:     config.DefaultHiddenTypes,
				FallbackType:    config.DefaultFallbackType,
				TokenEnv:        config.DefaultTokenEnv,
				ChangelogPath:   config.DefaultChangelogPath,
				ChangelogSource: config.DefaultChangelogSource,
				Labels: config.Labels{
					Pending: "needs-release", Tagged: config.DefaultTaggedLabel,
					Alpha: config.DefaultAlphaLabel, Beta: config.DefaultBetaLabel, Rc: config.DefaultRcLabel,
				},
				Author:         config.Author{Name: config.DefaultAuthorName, Email: config.DefaultAuthorEmail},
				Versioning:     config.DefaultVersioning,
				CommentMention: config.DefaultCommentMention,
			},
		},
		{
			name:    "defaults the author email left out of the author block",
			content: "author:\n  name: renovate\n",
			want: config.Config{
				InitialVersion:  config.DefaultInitialVersion,
				HiddenTypes:     config.DefaultHiddenTypes,
				FallbackType:    config.DefaultFallbackType,
				TokenEnv:        config.DefaultTokenEnv,
				ChangelogPath:   config.DefaultChangelogPath,
				ChangelogSource: config.DefaultChangelogSource,
				Labels: config.Labels{
					Pending: config.DefaultPendingLabel, Tagged: config.DefaultTaggedLabel,
					Alpha: config.DefaultAlphaLabel, Beta: config.DefaultBetaLabel, Rc: config.DefaultRcLabel,
				},
				Author:         config.Author{Name: "renovate", Email: config.DefaultAuthorEmail},
				Versioning:     config.DefaultVersioning,
				CommentMention: config.DefaultCommentMention,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeFile(t, tc.content)

			got, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Load() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLoadAcceptsAnExplicitEmptyHiddenTypesList(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "hidden-types: []\n")

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.HiddenTypes == nil || len(got.HiddenTypes) != 0 {
		t.Fatalf("HiddenTypes = %v, want an empty slice", got.HiddenTypes)
	}
}

func TestLoadRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
	}{
		{name: "rejects an unknown key", content: "release-tag: v1\n"},
		{name: "rejects malformed YAML", content: "initial-version: [0.1.0\n"},
		{name: "rejects a fallback type that bumps the version", content: "fallback-type: feat\n"},
		{name: "rejects an unknown changelog source", content: "changelog-source: rebase\n"},
		{
			name:    "rejects an unknown changelog source on a component",
			content: "components:\n  frontend:\n    path: frontend\n    changelog-source: rebase\n",
		},
		{name: "rejects an unknown updater", content: "files:\n  - path: cmd/cli/version.go\n    updater: xml\n"},
		{
			name:    "rejects a file whose updater requires a key but carries none",
			content: "files:\n  - path: cmd/cli/version.go\n    updater: go\n",
		},
		{
			name:    "rejects a file key on an updater that takes none",
			content: "files:\n  - path: cmd/cli/version.go\n    key: Version\n",
		},
		{name: "rejects an empty comment-mention", content: "comment-mention: \"\"\n"},
		{name: "rejects an empty pending label", content: "labels:\n  pending: \"\"\n"},
		{name: "rejects an empty tagged label", content: "labels:\n  tagged: \"\"\n"},
		{name: "rejects one label for two pre-release types", content: "labels:\n  alpha: \"release: pre\"\n  rc: \"release: pre\"\n"},
		{name: "rejects a pre-release label equal to a default one", content: "labels:\n  alpha: \"release: rc\"\n"},
		{name: "rejects a component without a path", content: "components:\n  frontend:\n    initial-version: 0.1.0\n"},
		{name: "rejects an unknown key inside a component", content: "components:\n  frontend:\n    path: frontend\n    release-tag: v1\n"},
		{
			name:    "rejects two components with the same path",
			content: "components:\n  frontend:\n    path: app\n  frontend2:\n    path: app\n",
		},
		{
			name:    "rejects a component nested inside another",
			content: "components:\n  app:\n    path: app\n  web:\n    path: app/web\n",
		},
		{
			name:    "rejects a root component alongside another",
			content: "components:\n  main:\n    path: .\n  frontend:\n    path: frontend\n",
		},
		{name: "rejects an empty tag prefix", content: "components:\n  web:\n    path: web\n    tag-prefix: \"\"\n"},
		{name: "rejects a tag prefix with a glob character", content: "components:\n  web:\n    path: web\n    tag-prefix: web*v\n"},
		{name: "rejects a tag prefix with whitespace", content: "components:\n  web:\n    path: web\n    tag-prefix: \"web v\"\n"},
		{name: "rejects a tag prefix with a double dot", content: "components:\n  web:\n    path: web\n    tag-prefix: web..v\n"},
		{name: "rejects a tag prefix starting with a dash", content: "components:\n  web:\n    path: web\n    tag-prefix: -web@v\n"},
		{
			name:    "rejects two components configured with the same tag prefix",
			content: "components:\n  web:\n    path: web\n    tag-prefix: app@v\n  api:\n    path: api\n    tag-prefix: app@v\n",
		},
		{
			name:    "rejects a tag prefix equal to another component's derived one",
			content: "components:\n  web:\n    path: web\n    tag-prefix: api/v\n  api:\n    path: api\n",
		},
		{name: "rejects an empty changelog path", content: "components:\n  web:\n    path: web\n    changelog: \"\"\n"},
		{name: "rejects an absolute changelog path", content: "components:\n  web:\n    path: web\n    changelog: /tmp/CHANGELOG.md\n"},
		{
			name:    "rejects a changelog path outside the repository",
			content: "components:\n  web:\n    path: web\n    changelog: web/../../CHANGELOG.md\n",
		},
		{name: "rejects the repository root as a changelog path", content: "components:\n  web:\n    path: web\n    changelog: ./\n"},
		{
			name:    "rejects two components configured with the same changelog",
			content: "components:\n  web:\n    path: web\n    changelog: CHANGELOG.md\n  api:\n    path: api\n    changelog: ./CHANGELOG.md\n",
		},
		{
			name:    "rejects a changelog equal to another component's derived one",
			content: "components:\n  web:\n    path: web\n    changelog: api/CHANGELOG.md\n  api:\n    path: api\n",
		},
		{name: "rejects a changelog path with a backslash", content: "components:\n  web:\n    path: web\n    changelog: 'web\\CHANGELOG.md'\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeFile(t, tc.content)

			if _, err := config.Load(path); err == nil {
				t.Fatal("Load() error = nil, want an error")
			}
		})
	}
}

func TestLoadReadsComponentsInConfigOrder(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `components:
  backend:
    path: backend
    hidden-types: [chore]
  frontend:
    path: frontend
    initial-version: 0.2.0
    bump-minor-pre-major: true
    files:
      - path: frontend/package.json
`)

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Components) != 2 {
		t.Fatalf("Components = %+v, want two entries in declaration order", got.Components)
	}
	if got.Components[0].Name != "backend" || got.Components[1].Name != "frontend" {
		t.Fatalf("Components order = [%s, %s], want [backend, frontend]", got.Components[0].Name, got.Components[1].Name)
	}
	if got.Components[0].Path != "backend" || got.Components[1].Path != "frontend" {
		t.Fatalf("Components paths = [%s, %s], want [backend, frontend]", got.Components[0].Path, got.Components[1].Path)
	}
}

func TestResolveReturnsTheImplicitMainComponentWithoutAComponentsKey(t *testing.T) {
	t.Parallel()
	cfg := config.Default()

	got := cfg.Resolve()
	want := []config.ResolvedComponent{{
		Name: "main", Path: ".", TagPrefix: "v", Branch: "release-me/main",
		ChangelogPath: "CHANGELOG.md", Multi: false,
		InitialVersion: config.DefaultInitialVersion, HiddenTypes: config.DefaultHiddenTypes,
		ChangelogSource: config.DefaultChangelogSource,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %+v, want %+v", got, want)
	}
}

func TestResolveMergesComponentOverridesWithTheTopLevel(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `initial-version: 0.5.0
bump-minor-pre-major: true
hidden-types: [chore, docs]
components:
  backend:
    path: backend
  frontend:
    path: frontend
    initial-version: 0.2.0
    bump-minor-pre-major: false
    hidden-types: [chore]
    files:
      - path: frontend/package.json
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.Resolve()
	want := []config.ResolvedComponent{
		{
			Name: "backend", Path: "backend", TagPrefix: "backend/v", Branch: "release-me/backend",
			ChangelogPath: "backend/CHANGELOG.md", Multi: true,
			InitialVersion: "0.5.0", BumpMinorPreMajor: true, HiddenTypes: []string{"chore", "docs"},
			ChangelogSource: config.DefaultChangelogSource,
		},
		{
			Name: "frontend", Path: "frontend", TagPrefix: "frontend/v", Branch: "release-me/frontend",
			ChangelogPath: "frontend/CHANGELOG.md", Multi: true,
			InitialVersion: "0.2.0", BumpMinorPreMajor: false, HiddenTypes: []string{"chore"},
			Files:           []config.File{{Path: "frontend/package.json", Updater: "marker"}},
			ChangelogSource: config.DefaultChangelogSource,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %+v, want %+v", got, want)
	}
}

func TestResolveAppliesMajorTagFromTheTopLevelAndPerComponentOverride(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `major-tag: true
components:
  backend:
    path: backend
  frontend:
    path: frontend
    major-tag: false
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.Resolve()
	if len(got) != 2 || got[0].Name != "backend" || got[1].Name != "frontend" {
		t.Fatalf("Resolve() = %+v, want [backend, frontend]", got)
	}
	if !got[0].MajorTag {
		t.Fatalf("backend MajorTag = %v, want true from the top level", got[0].MajorTag)
	}
	if got[1].MajorTag {
		t.Fatalf("frontend MajorTag = %v, want false from its own override", got[1].MajorTag)
	}
}

func TestResolveAppliesChangelogSourceFromTheTopLevelAndPerComponentOverride(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `changelog-source: squash
components:
  backend:
    path: backend
  frontend:
    path: frontend
    changelog-source: nested
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.Resolve()
	if len(got) != 2 || got[0].Name != "backend" || got[1].Name != "frontend" {
		t.Fatalf("Resolve() = %+v, want [backend, frontend]", got)
	}
	if got[0].ChangelogSource != "squash" {
		t.Fatalf("backend ChangelogSource = %q, want %q from the top level", got[0].ChangelogSource, "squash")
	}
	if got[1].ChangelogSource != "nested" {
		t.Fatalf("frontend ChangelogSource = %q, want %q from its own override", got[1].ChangelogSource, "nested")
	}
}

func TestDefaultSetsVersioningToIndependent(t *testing.T) {
	t.Parallel()
	if got := config.Default().Versioning; got != config.VersioningIndependent {
		t.Fatalf("Versioning = %q, want %q", got, config.VersioningIndependent)
	}
}

func TestLoadReadsVersioning(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    config.Versioning
	}{
		{name: "defaults to independent", content: "", want: config.VersioningIndependent},
		{name: "reads locked", content: "versioning: locked\n", want: config.VersioningLocked},
		{name: "reads linked", content: "versioning: linked\n", want: config.VersioningLinked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeFile(t, tc.content)

			got, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.Versioning != tc.want {
				t.Fatalf("Versioning = %q, want %q", got.Versioning, tc.want)
			}
		})
	}
}

func TestLoadRejectsAnUnknownVersioningValue(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "versioning: mixed\n")

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
}

func TestResolveSharesOneBranchAcrossEveryComponentWhenLocked(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `versioning: locked
components:
  backend:
    path: backend
  frontend:
    path: frontend
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.Resolve()
	for _, c := range got {
		if c.Branch != "release-me/main" {
			t.Fatalf("component %s Branch = %q, want the shared %q", c.Name, c.Branch, "release-me/main")
		}
		if c.TagPrefix != c.Name+"/v" {
			t.Fatalf("component %s TagPrefix = %q, want %q", c.Name, c.TagPrefix, c.Name+"/v")
		}
	}
}

func TestResolveKeepsOneBranchPerComponentWhenLinked(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `versioning: linked
components:
  backend:
    path: backend
  frontend:
    path: frontend
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.Resolve()
	want := []string{"release-me/backend", "release-me/frontend"}
	for i, c := range got {
		if c.Branch != want[i] {
			t.Fatalf("component %s Branch = %q, want %q", c.Name, c.Branch, want[i])
		}
	}
}

func TestResolveUsesAConfiguredTagPrefixAndDerivesTheRest(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `components:
  duxt:
    path: packages/duxt
    tag-prefix: duxt@v
  duxt-typesense:
    path: packages/duxt-typesense
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.Resolve()
	if got[0].TagPrefix != "duxt@v" || got[1].TagPrefix != "duxt-typesense/v" {
		t.Fatalf("TagPrefix = [%q, %q], want [%q, %q]", got[0].TagPrefix, got[1].TagPrefix, "duxt@v", "duxt-typesense/v")
	}
}

func TestLoadAcceptsATagPrefixThatStartsWithAnother(t *testing.T) {
	t.Parallel()
	// "web/v" and "web/v2/v" share a start, but a "web/v2/v1.0.0" tag never
	// parses as a version of web, so the two cannot read each other's tags.
	path := writeFile(t, "components:\n  web:\n    path: web\n  web/v2:\n    path: web-v2\n")

	if _, err := config.Load(path); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
}

func TestResolveUsesAConfiguredChangelogAndDerivesTheRest(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `components:
  duxt:
    path: packages/duxt
    changelog: ./CHANGELOG.md
  duxt-typesense:
    path: packages/duxt-typesense
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.Resolve()
	want := []string{"CHANGELOG.md", "packages/duxt-typesense/CHANGELOG.md"}
	if got[0].ChangelogPath != want[0] || got[1].ChangelogPath != want[1] {
		t.Fatalf("ChangelogPath = [%q, %q], want %q", got[0].ChangelogPath, got[1].ChangelogPath, want)
	}
}

func TestLoadTurnsAPreReleaseTypeOffWithAnEmptyLabel(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "labels:\n  alpha: \"\"\n  beta: \"\"\n")

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Labels.Alpha != "" || got.Labels.Beta != "" || got.Labels.Rc != config.DefaultRcLabel {
		t.Fatalf("Labels = %+v, want alpha and beta off and rc at its default", got.Labels)
	}
}

func TestLoadAcceptsSeveralPreReleaseTypesTurnedOff(t *testing.T) {
	t.Parallel()
	// Two empty labels are two types turned off, not one name used twice.
	path := writeFile(t, "labels:\n  alpha: \"\"\n  beta: \"\"\n  rc: \"\"\n")

	if _, err := config.Load(path); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
}
