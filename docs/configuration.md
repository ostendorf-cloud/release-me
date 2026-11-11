# Configuration reference

Full reference for every `release-me.yaml` key. See the [README](../README.md#configuration)
for the quick table and example.

The keys are grouped the way the configuration surface itself is ordered, version rules first
and the environment last, the one canonical order `AGENTS.md` records for the `Config` struct
and these headings alike.

## Version rules

### `initial-version`

The version when no tag exists yet, default `0.1.0`.

### `release-as`

Forces the next version, default none:

```yaml
release-as: 2.0.0
```

This is release-please's `release-as`. While the value is strictly greater than a component's
last tag, it wins over every [Release-As footer](overrides.md#release-as-footer), the computed
bump and `initial-version`. It releases even when no change bumps the version. Once that version
has shipped, the value is no longer greater than the last tag: it is ignored, the computed bump
applies again, and every run logs a warning to remove it from `release-me.yaml`, so a forgotten
key never holds the version back. A `release-me-version` block on the release pull request,
from a [`release-as` comment](overrides.md#comment-commands) or a hand edit, wins over the key.
See [forced version precedence](overrides.md#forced-version-precedence) for the full order. A
pre-release label on the release pull request builds on the forced version, `2.0.0-rc.1` for
the `rc` label.

Validation goes through the same rule as the comment command and the footer: a plain `X.Y.Z`
version, optionally prefixed with `v`. A value that does not parse is a configuration error, and
so is a pre-release, since the key stays in place after its release and would force the same
pre-release a second time.

At the top level, the key forces every component, each one checked against its own last tag.
That is how release-please applies a top-level `release-as` to every package. In `linked` it
releases every component at the forced version, changed or not, the same as `locked` always
does. In `independent`, a component's own `release-as` key replaces the top-level one for that
component only. `linked` and `locked` refuse a component's own key, since the
[family](versioning-modes.md), every component `release-me.yaml` lists, shares one version.

### `versioning-strategy`

How a release bumps the version, default `default`:

| Value | Version of a release |
| --- | --- |
| `default` | Bumped by its highest change: major for a breaking change, minor for a `feat`, patch for a `fix`, `perf` or `revert`. |
| `always-bump-patch` | The patch bumped, whatever its changes are. |
| `always-bump-minor` | The minor bumped. |
| `always-bump-major` | The major bumped. |
| `service-pack` | A service pack of the last version, `X.Y.Z-sp.N`. |
| `dependency-manifest` | Bumped as `default`, and at least as far as a dependency update among its changes. |

The changes still decide whether there is a release at all: a range of only hidden types, a
`chore` alone for one, releases nothing under any strategy but `dependency-manifest`, where a
dependency update counts. A [Release-As footer](overrides.md#release-as-footer), a
[comment command](overrides.md#comment-commands) and `initial-version` for a first release win
over the strategy, the same as over the default bump. `bump-minor-pre-major` and
`bump-patch-for-minor-pre-major` apply to `default` and `dependency-manifest` only: an
always-bump strategy bumps the part it names below `1.0.0` too, as in release-please. `next`
names the strategy in its reason, `1.3.0 (fix, always-bump-minor)`. Any other value is a
configuration error. A component overrides it through its own `versioning-strategy` key; a
`locked` or `linked` family computes its one shared version with the top-level value. A
dependent that releases only because a [workspace](#workspaces) dependency does ignores its own
strategy and takes a plain patch instead, since it has no changes of its own to bump by.

The key is release-please's `versioning`, renamed because release-me's own
[`versioning`](#versioning) already names how a family's versions relate. release-please's
`service-pack` and `dependency-manifest` are Java specific; release-me carries over what each
means for any repository:

- `service-pack` ships a maintenance release of the last version without moving it:
  `1.2.3-sp.1`, then `1.2.3-sp.2`, `N` counting up from the last `sp` tag of `1.2.3`, the
  counter a [pre-release](commands.md#pre-releases) type keeps. Each service pack collects the
  entries since the previous one, and there is no release until a change since it bumps the
  version. Semver orders `1.2.3-sp.1` below `1.2.3`, so release-me treats it as the pre-release
  it is to semver: the GitHub release is a pre-release, the major tag stays, and the next
  stable tag still counts from `1.2.3`. A [pre-release label](commands.md#pre-releases) on its
  release pull request is refused: `sp.N` is already the pre-release identifier of that
  version, a second one cannot be placed on top of it, and `pr` names both and changes nothing
  until the label is removed. It applies with `versioning: independent` only;
  `linked` and `locked` refuse it, since a family shares one version line.
- `dependency-manifest` raises the bump to the largest dependency update among the changes,
  whatever their type, so `chore(deps): bump react from 18.3.1 to 19.0.0` releases a major
  one. release-please reads the update from a Java manifest; release-me reads it from the
  title Dependabot and Renovate give the change: Dependabot's `bump <name> from <old> to <new>`
  bumps by the highest part that changed, Renovate's `update dependency <name> to v<new>` and
  `update module <name> to v<new>`, which name no old version, by the new version's shape, as
  release-please does: `X.0.0` major, `X.Y.0` minor, anything else patch. The reason names the
  dependency, `2.0.0 (dependency react major update)`. The changelog entry keeps its own
  type, so a hidden `chore` stays hidden.

### `bump-minor-pre-major`

Turns a major bump into a minor one below `1.0.0`, default `false`. A component overrides it
through its own `bump-minor-pre-major` key; a `locked` or `linked` family computes its one
shared version with the top-level value.

### `bump-patch-for-minor-pre-major`

Turns a minor bump, a `feat`, into a patch one below `1.0.0`, default `false`, so `0.4.1` with a
`feat` releases `0.4.2` instead of `0.5.0`. A breaking change is not affected: it still bumps
the major, or the minor with `bump-minor-pre-major: true`, never only the patch. From `1.0.0`
on, and for a component's first release, which gets `initial-version`, the key changes nothing.
A component overrides it through its own `bump-patch-for-minor-pre-major` key; a `locked` or
`linked` family computes its one shared version with the top-level value. `next` names the
rule in its reason, `0.4.2 (feat, patch below 1.0.0)`.

## Changelog

### `changelog-path`

The changelog file path, default `CHANGELOG.md`, release-please's own `changelog-path` key.
`pr` prepends the new version's section to it, creating the file when it does not exist yet.

### `changelog-source`

Which commits become changelog entries, default `first-parent`. See
[changelog modes](changelog-modes.md) for `first-parent`, `squash`, `all` and `nested`. A
component overrides it through its own `changelog-source` key.

### `changelog-type`

Where the changelog section comes from, release-please's `changelog-type`: `default`, the
entries `changelog-source` reads, or `github`, the release notes GitHub generates for the release,
the ones its "Generate release notes" button fills in:

```markdown
## [v0.2.0](https://github.com/owner/repo/compare/v0.1.0...v0.2.0) (2026-11-04)

### What's Changed
* feat: add the widget by @octocat in https://github.com/owner/repo/pull/12

**Full Changelog**: https://github.com/owner/repo/compare/v0.1.0...v0.2.0
```

- The version still comes from the conventional commits since the last tag; only the section
  under the heading changes, in the changelog file, the release pull request body and the
  release alike. `release-me changelog` prints it the same way, so it calls GitHub too.
- `pr` asks GitHub for the notes of the tag the release will get, on the commit it cuts the
  release pull request from, since the component's last tag; for a first release GitHub picks
  the previous release itself, or takes the whole history. GitHub's notes cover the whole
  repository between those two points, not only a component's path, and follow the
  repository's `.github/release.yml` categories when it has one.
- The notes are fitted under the version heading: GitHub's `## What's Changed` and every heading
  up to the third level become `###`, a deeper one a bold line, since a `##` heading would read
  as the next version's and a `####` one as a component of a shared release pull request.
- `hidden-types` shapes the default type only.
- The request is a `POST` to `releases/generate-notes`, which creates nothing and needs the
  `contents: write` permission the job already has for its tags.

A component overrides it through its own `changelog-type` key, so a shared release pull request
can carry GitHub's notes for one component and entries for another. Any other value is a
configuration error.

### `skip-changelog`

Leaves the changelog file alone, default `false`, release-please's `skip-changelog`: `pr`
neither creates nor prepends to it, for a repository that keeps its changelog elsewhere, or by
hand. Everything else stays: the release pull request body and the release still carry the
version's section, and the files under `files` are still bumped. With no file to bump, the
release branch carries an empty release commit, so the release pull request still has one to
merge. A component overrides it through its own `skip-changelog` key, so one component of a
shared release pull request can skip its changelog while the others keep theirs.

### `hidden-types`

Hides entries of these types from the changelog, default `chore`, `docs`, `ci`, `style`,
`refactor`, `test`, `build`, `other`. Without [`changelog-sections`](#changelog-sections), only
`feat`, `fix`, `perf`, `revert` and a breaking change ever render a section; a type outside
those five never renders, hidden or not.

### `fallback-type`

The type assigned to text that does not parse as a conventional commit, default `other`.
`feat`, `fix`, `perf` and `revert` are refused, unparseable text never bumps the version.

### `changelog-sections`

Renames, reorders or adds the sections after `Breaking Changes`, release-please's shape: a list
of `type`, `section` and an optional `hidden`, default `false`. Left out, the sections are
`Features` (`feat`), `Bug Fixes` (`fix`), `Performance` (`perf`) and `Reverts` (`revert`). Given,
the list replaces them:

```yaml
changelog-sections:
  - type: feat
    section: Features
  - type: fix
    section: Bug Fixes
  - type: deps
    section: Dependencies
  - type: chore
    hidden: true
```

- Sections render in the list's order. Types sharing a `section` render under one heading,
  where that heading first appears.
- A type the list names decides its own visibility: listed with a `section`, it renders even
  when `hidden-types` names it; listed with `hidden: true`, it never renders. `section` is
  required unless `hidden` is `true`, and ignored when it is `true`.
- A type the list leaves out renders nowhere, the same as a type outside the default sections
  today. A breaking change always renders under `Breaking Changes`, first, unless its type is
  hidden.
- An empty list, `changelog-sections: []`, keeps `Breaking Changes` only.

A type listed twice, an entry without a `type` and a `section` spanning more than one line are
configuration errors. A component overrides the list through its own `changelog-sections` key.
Renaming a section changes the next release only: `CHANGELOG.md` keeps every earlier section as
it was written, and an open release pull request picks the new headings up on the next `pr`
run.

### `changelog-sentence-case`

Upper cases the first letter of every entry's description, default `false`, the counterpart of
release-please's `sentence-case` plugin: `feat(cli): add the widget` renders as
`**cli:** Add the widget`. Only a lower case ASCII letter at the very start changes. A first
word that already holds an upper case letter, such as `iOS` or `gRPC`, stays as written, and so
do `npm` and a description starting with a backtick or a digit. The pull request title or
commit stays as it is; only the rendered line changes. There is no per-component override.

### `changelog-authors`

Credits each entry's author after its description, default `false`, the counterpart of
release-please's `include-commit-authors`:

```markdown
- add the widget (@octocat) ([#12](https://github.com/owner/repo/pull/12))
- handle the empty case (Ada Lovelace)
```

An entry whose text a pull request gave, its title or an
[override block](overrides.md#override-block), credits the pull request's author as `@<login>`.
That is every pull request entry in `first-parent`, `squash` and `nested`. An entry whose text
a commit gave credits that commit's git author name: a direct commit, and every entry in
`all`, which reads each commit's own subject. A `nested` sub item credits no one, since its
parent line already credits the pull request. An `@<login>` mentions that account where GitHub
renders the text, in the release pull request body and the release, which notifies it the way
release-please's mention does. There is no per-component override.

### `changelog-commit-links`

Links each entry's commit after the pull request link, default `false`, the shape
release-please renders:

```markdown
- handle the empty case ([#12](https://github.com/owner/repo/pull/12)) ([8fa6c1b](https://github.com/owner/repo/commit/8fa6c1b0d3e2f4a5b6c7d8e9f0a1b2c3d4e5f6a7))
```

The commit is the one the entry comes from: the merge commit for a pull request in
`first-parent` and `nested`, the squash commit in `squash`, each commit's own in `all`, where an
[override block](overrides.md#override-block) links the oldest commit of its pull request. A
`nested` sub item links its own commit. There is no per-component override.

### `changelog-issue-references`

Links the issues an entry closes, default `false`, after every other link:

```markdown
- handle the empty case ([#12](https://github.com/owner/repo/pull/12)), refs [#7](https://github.com/owner/repo/issues/7)
```

An issue counts when the entry's text names it after one of GitHub's closing keywords, `close`,
`closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves` or `resolved`, any case, an
optional `:` after it: a pull request's title and body, a commit's message. A plain mention, or
an issue in another repository, `owner/repo#7`, is no reference. It renders as `refs`, the way
release-please renders it, not `closes`. The release pull request body carries the changelog,
and a closing keyword there would close the issue again when that pull request merges. There is
no per-component override.

### `changelog-host`

The web address every changelog link points at, release-please's `changelog-host`, default
none: the forge's own. Set it when the changelog should link a host other than the one
release-me's API calls go to, such as a GitHub Enterprise Server's `https://github.example.com`:

```yaml
changelog-host: https://github.example.com
```

It moves the heading's compare link and each entry's pull request link. Each link keeps its
path, `/<owner>/<repo>/pull/12`, behind the configured address, which may carry a path prefix of
its own, `https://code.example.com/github`. It changes no request and no link outside the
changelog: the release pull request's own address, the action outputs and the API calls all
stay on the forge. The address is an absolute `http` or `https` one with a host and no query,
fragment or credentials; a trailing `/` is dropped. Whitespace and parentheses are refused, as
they break the markdown link the heading's compare link is read back from. There is no
per-component override.

### `changelog-date-format`

The format of the date in each version heading, a strftime expression, default `%Y-%m-%d`:

```yaml
changelog-date-format: "%B %e, %Y"   # ## [v1.2.0](…) (November 4, 2026)
```

The directives are `%Y` (2026), `%y` (26), `%m` (11), `%d` (04), `%e` (4), `%B` (November), `%b`
(Nov), `%A` (Wednesday), `%a` (Wed), `%j` (308, the day of the year) and `%%` for a literal `%`;
names are English. Any other text renders as it is. Quote the value, YAML reads a leading `%`
as a directive of its own.

release-me reads the date back from an open release pull request, to keep it while nothing new
lands, so the format has to read back: an unknown directive, a parenthesis or a line break, which
end the heading's `(<date>)` early, and a format whose dates run together, such as `%e%m`, are
configuration errors. Changing the format changes the next heading only: `CHANGELOG.md` keeps
every earlier heading as it was written, and a release pull request opened before the change
keeps its date, rendered in the new format on the next `pr` run. There is no per-component
override.

Custom-formatted pull request bodies record the canonical date in machine metadata outside
the rendered body. Changing to another custom format, or back to the default, keeps that
date until release content changes. Older bodies fall back to the current format and ISO.

## Version files

### `files`

Files `pr` bumps to the next version, each a `path`, an optional `updater`, default `marker`,
and a `key` where the updater needs one. Every updater keeps the rest of the file byte for
byte: order, comments, indentation. A file that does not exist, a file whose shape the updater
does not match, or a `key` not found in it, fails `pr`, naming the path and the key. Every
entry is validated when release-me.yaml loads too: an unknown `updater`, a `key` missing where
the updater requires one, and a `key` given to an updater that takes none are all errors.

Pick an updater by the file in front of you:

| Updater | `key` | What it bumps |
| --- | --- | --- |
| `marker` | none | Every line ending in `# release-me: version` or `// release-me: version`, the rest of the line kept. The default updater. |
| `generic` | none | Every line and block release-please's `x-release-please-*` markers name, so a file release-please bumped needs no change. |
| `plain` | none | The whole content of a file holding nothing but the version, a `VERSION` or `version.txt`, where a line has no room for a marker comment. The surrounding whitespace, a trailing newline or none, is kept; an empty file, or one holding anything besides the version, is an error. |
| `json` | dotted path of object keys | The first string value at that path, `version` or `a.b.version`. A `composer.json`, which release-please's `php` release type bumps, takes `version`. |
| `yaml` | dotted path of mapping keys | The first scalar at that path. |
| `toml` | table and key | The first scalar at that path, `package.version` meaning `version` under `[package]`, `tool.poetry.version` under `[tool.poetry]`. |
| `xml` | dotted path of element names | The text of every element at that path, `project.version` for a `pom.xml`, `Project.PropertyGroup.Version` for a `.csproj`. |
| `npm-lock` | none | A `package-lock.json`'s or `npm-shrinkwrap.json`'s own version: its top-level `version` and, in lockfile version 2 or 3, the root package's entry `packages[""].version`. Dependencies and workspace packages are left alone. |
| `cargo-lock` | crate name | That crate's `[[package]]` entry in a `Cargo.lock`. |
| `gemfile-lock` | gem name | That gem's specs entry, `name (version)`, in a `Gemfile.lock`. |
| `go` | constant or variable name | The string literal that `const` or `var` declaration assigns. |
| `helm` | none | A `Chart.yaml`'s top-level `version` field, `version: 0.1.0`. |
| `python` | none | The first `__version__ = "..."` or `version = 1.2.3` line, the shape of an `__init__.py`, a `version.py`, `setup.py`'s `version="..."` keyword and `setup.cfg`'s bare value alike. A bare value must start with a digit, so `version=get_version()` is never taken for one. A `pyproject.toml` takes `toml`. |
| `ruby` | none | A `version.rb`'s `VERSION = "..."` constant, a trailing `.freeze` kept. |
| `elixir` | none | A `mix.exs`'s `@version "..."` attribute, else its `version: "..."` keyword. |
| `ocaml` | none | An `.opam` or `.opam.locked` file's top-level `version: "..."`, or a `dune-project`'s `(version ...)` stanza. |
| `r` | none | A `DESCRIPTION` file's `Version:` field, a development suffix such as `.9000` included. |
| `bazel` | none | The `version` attribute of a `MODULE.bazel`'s `module()` call, never a `bazel_dep()`'s. |
| `dart` | none | A `pubspec.yaml`'s top-level `version`, its numeric build number counted up by one: `1.4.0+12` becomes `1.5.0+13`, as release-please bumps it. |
| `sfdx` | none | The `versionNumber` of an `sfdx-project.json`'s default package directory, written as the version followed by `.NEXT`. Other package directories and dependencies are left alone. |
| `terraform-module` | none | Every `v`-prefixed version of a Terraform module's `versions.tf` or `versions.tf.tmpl`. |
| `expo` | Expo SDK version | A React Native Expo project's `app.json`: `expo.version`, plus `expo.ios.buildNumber` and `expo.android.versionCode` where the file carries them. |
| `krm-blueprint` | none | Every `cnrm/<blueprint>/v1.4.0` value of a Kubernetes Resource Model manifest. |

The updaters below carry constraints a row cannot hold; the rest behave exactly as their row
says.

#### `marker` and `generic`

`marker` needs nothing but the comment at the end of the line, in a file of any language:

```go
const Version = "0.1.0" // release-me: version
```

`generic` reads release-please's own markers instead. A line carrying
`x-release-please-version` gets its first full version replaced; one carrying
`x-release-please-major`, `-minor` or `-patch` gets its first number replaced by that part of
the version. A block opened by `x-release-please-start-version` (or `-major`, `-minor`,
`-patch`) bumps every line up to and including the one carrying `x-release-please-end`. The
markers sit in any comment, which is how a Markdown file is bumped:

```markdown
<!-- x-release-please-start-version -->
curl -fsSL https://example.com/notes/1.4.0/install.sh | sh
<!-- x-release-please-end -->
```

The `date` and `version-date` scopes are an error, since an updater is given the version only,
not the release date. So are a file with no marker and a block that is never closed. As in
release-please, an inline marker inside a block takes precedence for that line; put the end
marker on its own line so it closes the block. An end marker outside a block has no effect.
The major scope also matches a number prefixed with `v`, whereas minor and patch require a
word boundary before the number.

#### `json`, `yaml` and `toml`

Each one reads the file as lines rather than parsing it, which is what keeps every other byte
in place, so the layout is part of the contract:

- `json`: every object a path segment but the last names has to open on a line of its own,
  `"segment": {`, and the leaf has to be a string on a line of its own, `"version": "1.2.3"`.
  A single-segment key names the top level only, and nesting is tracked through unquoted
  braces, so a same-named leaf at another depth is never taken for the match.
- `yaml`: every mapping a path segment but the last names has to be a bare `segment:` line,
  its block found through indentation, any consistent width. A single-segment key matches at
  indentation zero only, never a deeper leaf of the same name. The leaf's value may be bare,
  single or double quoted, and a trailing comment is kept.
- `toml`: the leading segments name the enclosing table, `a.b.version` meaning key `version`
  under `[a.b]`, and a key before any table header is addressed by its name alone. The leaf's
  value may be bare, single or double quoted.

#### `go`

The `const` or `var` keyword has to sit on the same line as the identifier, with an optional
type between the name and the `=`: `const Version = "0.1.0"`, `var Version = "0.1.0"` and
`const Version string = "0.1.0"` match. An identifier inside a grouped `const (` block does
not, and the entry fails `pr` naming the identifier as not found.

```yaml
files:
  - path: internal/version/version.go
    updater: go
    key: Version
```

#### `cargo-lock`, `gemfile-lock` and `xml`

- `cargo-lock`: only a `[[package]]` entry without a `source` matches, which is how Cargo
  records a crate of the workspace itself, so a same-named crate from a registry is never
  touched. A workspace releasing several crates lists the file once per crate.
- `gemfile-lock`: the specs entry is indented by four spaces, as Bundler lists a resolved gem;
  a dependency's constraint, indented deeper, is left alone. A pre-release is written the way
  Bundler records it, `1.2.0-rc.1` as `1.2.0.pre.rc.1`.
- `xml`: element names match without their namespace prefix, and the whitespace around the
  text is kept. An element that is empty-tagged, `<version/>`, or holds a child element or a
  comment is an error.

```yaml
files:
  - path: Cargo.toml
    updater: toml
    key: package.version
  - path: Cargo.lock
    updater: cargo-lock
    key: release-core
  - path: pom.xml
    updater: xml
    key: project.version
```

#### `expo`

`expo.version` must be there; `expo.ios.buildNumber` and `expo.android.versionCode` are bumped
only where the file carries them. The version code is an integer Android requires to grow with
every upload, so it is computed rather than copied, the way release-please computes it:

```text
sdk-major * 10000000 + major * 10000 + minor * 100 + patch
```

A pre-release suffix does not reach it, so `1.5.0-rc.1` and `1.5.0` give the same code. The
`key` is the Expo SDK version, which only the version code needs: release-please reads it from
the `expo` dependency of the project's `package.json`, which an updater given one file cannot
do. The dependency's own value is accepted, so `54`, `54.0.0` and `~54.0.0` all mean SDK 54.
The project's `package.json` version takes a `json` entry of its own:

```yaml
files:
  - path: package.json
    updater: json
    key: version
  - path: app.json
    updater: expo
    key: "54"
```

Under SDK 54, version `1.5.0` gives the version code `540010500`.

#### `terraform-module` and `krm-blueprint`

Both bump a version that is part of a longer string rather than a field of its own, which is
why they replace every match in the file.

- `terraform-module`: the module's own version is the last segment of the module name its
  `provider_meta` block reports, such as
  `blueprints/terraform/terraform-google-notes/v1.4.0`. A constraint carries no `v`, so
  `required_version = ">= 1.3"` and `version = "~> 5.0"` in the same file are left alone. A
  module's `metadata.yaml` takes `yaml` with key `version`; the `version = "~> 1.4"` usage
  example of its README is not rewritten.
- `krm-blueprint`: the version is the last segment of the `cnrm.cloud.google.com/blueprint`
  annotation's value. That `cnrm/` anchor is what keeps a pinned kpt function image such as
  `gcr.io/kpt-fn/set-annotations:v0.1.4` out of it. A manifest carrying no such value is an
  error, which is how a YAML that is no blueprint is rejected.

## Pull request

### `release-pull-request`

`per-component`, the default for `independent` and `linked`, or `shared`, always the case for
`locked`, which rejects `per-component`. See
[shared release pull request](versioning-modes.md#shared-release-pull-request).

### `release-pull-request-layout`

How the changelog in the body of a release pull request carrying several components lays out
each component, default `headings`. It applies to every such body, the
[shared release pull request](versioning-modes.md#shared-release-pull-request) and the one a
`locked` family shares; a pull request releasing one component has no component layout. Only
the pull request body changes: every `CHANGELOG.md` and every GitHub release keep the heading
levels they always had, and each component's release body is the same whichever layout the
pull request was rendered in. `locked` keeps its one combined, unlinked version heading above
every component in all three layouts, each component's slice then carrying only its sections.

- `headings` gives each component a level two heading of its own, its section shifted one level
  down below it:

  ```markdown
  ## api

  ### [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (2026-03-14)

  #### Features

  - add the export endpoint
  ```

- `details` collapses each component into a block of its own, summarised with its name and
  version, the shape release-please renders. The section inside keeps its `CHANGELOG.md`
  levels, a level two version heading and level three sections, since the summary already names
  the component:

  ```markdown
  <details><summary>api v1.3.0</summary>

  ## [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (2026-03-14)

  ### Features

  - add the export endpoint

  </details>
  ```

- `compact` is the layout before the key existed: a level four heading per component, its
  section at its `CHANGELOG.md` levels, so the version heading renders larger than the name:

  ```markdown
  #### api

  ## [v1.3.0](https://github.com/owner/repo/compare/api/v1.2.0...api/v1.3.0) (2026-03-14)

  ### Features

  - add the export endpoint
  ```

Any other value is a configuration error. Every command reads the layout back from the body
itself, never from the key: a pull request opened before the key changed still releases every
component it carries, and a `release-as <component>` comment still finds them. The next `pr`
run rewrites the body in the new layout without pushing the release branch. A `<details>`
block whose summary reads `<name> vX.Y.Z`, at the changelog's top level and closed by its own
`</details>`, marks `details`, a level two heading that is not a version heading marks
`headings`, anything else is `compact`. A line shaped like that summary inside a component's
section, an override block's text carried into an entry, never marks `details`; the release
notes and version override blocks are never read for components.

### `release-pull-request-guide`

Every release pull request body, per component, shared or locked, opens with a collapsed
`<details><summary>How this release works</summary>` guide below its version line. The
top-level `release-pull-request-guide` key turns it off for every one of them, default `true`:

```yaml
release-pull-request-guide: false
```

The guide is a reminder for whoever reviews the pull request, never something release-me reads:
the notes block, the `release-me-version` block, the components of a shared or locked body in
every layout, the changelog date and the release bodies all read back the same with the guide
or without it, and both comment commands work on either. Changing the key while a release pull
request is open is safe: the next `pr` run adds or drops the guide through a body update alone,
without pushing the release branch, the notes block and a version override kept.

A team that turns the guide off finds everything it says documented here:

- What merging does, the tag, the release, the label swap and the moving major tag:
  [release](commands.md#release).
- Release notes, the `release-me-notes` block: [notes block](overrides.md#notes-block).
- Forcing the next version, the `release-me-version` block and the `release-as` comment, per
  component on a shared pull request with `independent` versions:
  [comment commands](overrides.md#comment-commands), the
  [Release-As footer](overrides.md#release-as-footer) for doing it from a commit, and
  [shared release pull request](versioning-modes.md#shared-release-pull-request).
- Pre-releases through the `alpha`, `beta` and `rc` labels:
  [pre-releases](commands.md#pre-releases).
- Hiding or rewriting a merged pull request's entry, the `release-me` block in its body:
  [override block](overrides.md#override-block).
- What a push to the default branch updates and which edits survive it: [pr](commands.md#pr)
  and the [notes block](overrides.md#notes-block).

### `release-pull-request-title`

The title pattern of every component's own release pull request, default
`chore(${component}): release v${version}`. Literal text with these placeholders:

- `${component}`: the component's name, `main` for the implicit one. Required.
- `${version}`: the version, `X.Y.Z` or `X.Y.Z-<type>.N`, without a `v`. Required.
- `${branch}`: the branch the pull request merges into, the default branch. Optional.

```yaml
release-pull-request-title: "chore(${branch}): release ${component} ${version}"
```

The title is where `release` reads the version back from after the merge, so the pattern has
to carry every value it reads: a pattern without `${component}` or `${version}` is a
configuration error, the same as an unknown placeholder such as release-please's `${scope}`, a
placeholder used twice, two placeholders with no literal text between them, a line break, a
`-->`, or a title that reads as a conventional commit bumping the version, such as
`feat: release ${version}`: the title is the release commit's subject too. `pr` parses every
title it renders back with the same pattern before it pushes or writes anything, and refuses
one that does not give the same values back, such as `release ${component} ${version}-${branch}`
onto a branch named `rc-2`, or one past GitHub's 256 characters.

Changing the pattern while a release pull request is open is safe. The next `pr` run retitles
it through a body update alone, without pushing the release branch. A pull request merged
before that run still releases: `release` first reads the pattern the body records in an HTML
comment, `<!-- release-me-title-pattern: ... -->`, written for any non-default pattern. It
then tries the legacy default shape before the current configured pattern, so an ambiguous
new setting never reinterprets an older title.

### `release-pull-request-shared-title`

The title pattern of the release pull request every component shares, with
[`release-pull-request: shared`](#release-pull-request) or in `locked`. The placeholders depend
on the versioning mode, since `release` reads every component and version back from the title:

- `locked`, default `chore: release v${version} (${components})`: `${version}`, the family's
  one version, and `${components}`, every component's name, comma separated, both required.
- `independent` and `linked`, default `chore: release ${releases}`: `${releases}`, every
  component with its version, `api v1.3.0, web v0.3.1`, required; `${components}` optional.
- `${branch}` is optional in every mode.

```yaml
release-pull-request: shared
release-pull-request-shared-title: "chore(${branch}): release ${releases}"
```

The pattern is checked, parsed back, length checked and read back after a change the same way
as [`release-pull-request-title`](#release-pull-request-title).

### `release-pull-request-draft`

Opens every release pull request as a draft, default `false`:

```yaml
release-pull-request-draft: true
```

GitHub refuses to merge a draft until someone marks it ready for review, so a release needs
that one deliberate click on top of the merge. The key applies when `pr` opens a release pull
request; one already open keeps its state, draft or ready, through every update, and changing
the key never converts it. `release` reads a merged draft exactly like any other pull request.
The action's `pr`, `prs` and per-component `pr` outputs do not include the draft state; a
workflow that merges pull requests must read their current state from GitHub first.
GitHub offers drafts on public repositories and on private ones with a paid plan; where it
does not, creating the pull request fails with the forge's error and `pr` reports it, the
release branch already pushed.

### `release-pull-request-header`

Free text every release pull request body carries, per component, shared or locked: the header
above the version line, and its counterpart
[`release-pull-request-footer`](#release-pull-request-footer) below the changelog. Both default
to none.

```yaml
release-pull-request-header: |
  Reviewed by the release team before every merge.
release-pull-request-footer: "Questions go to #releases."
```

Nothing reads either back, so they never change what a release does. A line in either that
opens a fence, three or more backticks at the start of the line, is indented by one space, the
way the [notes block](overrides.md#notes-block) escapes its content: a `release-me-notes`,
`release-me-version` or `release-me` block pasted into a header renders as a code block and is
never read as notes, a version override or an entry override. A text carrying
`<details><summary>Changelog</summary>`, the opening the changelog is read from, or a line
opening a `<!-- release-me-` comment, which release-me reserves for what it records in a body,
is a configuration error. Every other `<details>` block is fine. Changing either while a release
pull request is open updates its body on the next `pr` run without pushing the release branch,
the notes block and a version override kept. Neither reaches the changelog file or the GitHub
release.

### `release-pull-request-footer`

Free text below the changelog of every release pull request body, default none. It follows the
same rules as [`release-pull-request-header`](#release-pull-request-header).

## Release commit

### `author`

`author.name` and `author.email`: accepted, default `release-me` and
`release-me[bot]@users.noreply.github.com`, but ignored. The forge creates the release commit
under the token's own identity, since GitHub signs it only when no author is passed, see
[signed release commits](commands.md#signed-release-commits). The keys still load, so an
existing configuration keeps working, and `check` and `pr` warn about each one that is set,
naming it as ignored. `GIT_AUTHOR_*` and `GIT_COMMITTER_*` in the environment change nothing either.

### `signoff`

The identity, `Name <email>`, the release commit is signed off by, default none:

```yaml
signoff: "Release Bot <release-bot@example.com>"
```

`pr` ends the release commit's message with a `Signed-off-by: Release Bot
<release-bot@example.com>` trailer, the line `git commit --signoff` writes, for a repository
whose DCO check requires one on every commit. The trailer is part of the message, so it is the
same whichever way the commit is created, and it changes neither the commit's author nor its
committer, which are the token's own, see [`author`](#author). Anything other than a name and
an email in angle brackets on one line is a configuration error. Enabling or changing
`signoff` also refreshes an already open release commit whose trailer is missing or names the
previous identity. An unchanged rerun then leaves that commit alone.

## Tags and releases

### `major-tag`

Moves the major tag on every stable release, default `false`. See [release](commands.md#release)
for what `true` changes. A component overrides it through its own `major-tag` key.

Known limitation: the major tag moves when the release is created, not when a human publishes
it. With [`force-tag-creation`](#force-tag-creation) on, the default, the version tag sits on
that same commit already, draft or not, so the two agree. With it off and the release drafted
by [`github-release-draft`](#github-release-draft) or
[`github-release-draft-until-assets-attach`](#github-release-draft-until-assets-attach), the version
tag does not exist yet, so the major tag points at a version that is not published until
someone publishes that draft. Deferring the move is not an option: nothing in release-me runs
after that publish, so a deferred major tag would never be created at all. Keep
`force-tag-creation: true` where the major tag matters, which is the default.

### `include-v-in-release-name`

Keeps the `v` before the version in the GitHub release's name, default `true`: `vX.Y.Z` for
the implicit `main` component, `<name> vX.Y.Z` for a configured one. `false` names it
`X.Y.Z` or `<name> X.Y.Z`. Only the release's name changes: the tag keeps its prefix, and the
release pull request title, which `release` reads the version back from, keeps its `v`. A
component overrides it through its own `include-v-in-release-name` key.

### `skip-github-release`

Tags a merged release pull request without creating a GitHub release, default `false`, for a
repository whose own workflow creates the release from the tag, such as a GoReleaser step that
reads the action's `tags` output. Everything else still happens: the tag, the
[major tag](#major-tag), the label swap from `pending` to `tagged`, the `released-tag` marker
and every action output, so a later step knows which tag to release. With
[`github-release-draft-until-assets-attach`](#github-release-draft-until-assets-attach) on, a
component that skips its release has no draft to attach assets to or to resume, and
[`github-release-draft`](#github-release-draft) has no effect at
all. [`force-tag-creation: false`](#force-tag-creation) beside this key is refused when
release-me.yaml loads: it leaves the tag to a release that is never created. A component
overrides it through its own `skip-github-release` key.

Unlike release-please, release-me creates the tag even with this key on; see
[migrate](commands.md#migrate) for what a migrated repository has to change because of it.

### `github-release-draft`

Creates each GitHub release as a draft and leaves it one, default `false`, for a human to
review and publish. The tag, the label swap, the `released-tag` marker and the action outputs
still happen on the same run, exactly as for a published release, so a publish step already
sees this tag before any human has acted. It differs from
[`github-release-draft-until-assets-attach`](#github-release-draft-until-assets-attach) in one way:
a draft left on purpose is never resumed, so a later run does not print its `released-tag` marker
again while it waits. That holds with both keys on too: the component that drafts on purpose is
left out of the resume. Neither key makes a publish step wait for a human on its own: the
[draft until assets attach](action.md#draft-until-assets-attach) workflow this repository
runs publishes every draft its `tags` output carries, this one included, with no way to tell
a deliberate draft from one only waiting on assets. Filter the component out of that step, or
run no automatic publish step for it, where the release genuinely needs a human to publish it
first. A component overrides `github-release-draft` through its own `github-release-draft` key.

With [`skip-github-release`](#skip-github-release) on, this key has no effect: there is no
release to draft. With [`major-tag`](#major-tag) on, the major tag moves when the draft is
created, not when it is published; see `major-tag`.

### `github-release-draft-until-assets-attach`

Drafts every release instead of publishing it immediately, default `false`. There is no
per-component override: it applies to the whole repository. Only a repository with GitHub's
immutable releases setting turned on needs `true`; it rejects any later change to a published
release, assets included, and release-me has no way to detect that setting through the API.
See [release](commands.md#release) for what `true` changes, and
[draft until assets attach](action.md#draft-until-assets-attach) for the calling workflow each
setting needs: `publish.yaml`'s own webhook-triggered asset step for `false`, `release.yaml`
attaching and publishing the release itself for `true`.

### `force-tag-creation`

Creates the tag on the merge commit before the release, default `true`, the way release-me
always has, so a draft's tag exists while the draft waits. `false` leaves the tag to GitHub
instead, the release-please default: the release is created with the merge commit as its
target, and GitHub creates the tag on it when the release is published, right away for a
published release, on publish for a draft from
[`github-release-draft-until-assets-attach`](#github-release-draft-until-assets-attach). That tag
creation on publish is GitHub behaviour release-me relies on but GitHub does not document anywhere,
and it is not verified against a live repository either. If the tag does not appear after a
publish, `pr` reports the missing tag and waits instead of releasing the same version again. The
run then reports `drafted <tag>, tagged on publish`. Until that publish the tag does not exist: a
later step must not expect it, a rerun cannot tell a draft it tagged before, and the [major
tag](#major-tag) already points at the merge commit. The draft is still found by its tag name, so
the resume of `github-release-draft-until-assets-attach` and the guard against a second draft work
the same. A component overrides it through its own `force-tag-creation` key.

`force-tag-creation: false` together with [`skip-github-release`](#skip-github-release) is
refused when release-me.yaml loads, at the top level and for every component it resolves to:
the tag is left to a release that is never created, so nothing would ever create it.

Keep `force-tag-creation: true` when using this repository's
`scripts/publish-release-assets.sh` with `github-release-draft-until-assets-attach`: the script
checks out the tag and GoReleaser resolves it before publishing, so both need an existing git ref.
A workflow that defers the tag must build from the release's `targetCommitish` instead of
checking out the tag, and use tooling that can build without that tag ref.

While a deferred version tag is absent and its release is an unpublished draft, `pr` waits
for `release` to resume that draft. It does not open another pull request for the same version,
including after switching per-component releases to a shared pull request. Publishing the
draft creates the tag and unblocks `pr`; deleting the draft also unblocks it, so the next
run can create the release again.

### `delete-release-branch`

Deletes a release branch once `release` has tagged and released every pull request merged from
it, default `true`; `false` keeps it. There is no per-component override. See
[release](commands.md#release) for when a branch stays and what `--dry-run` prints.

GitHub's repository setting "Automatically delete head branches" deletes the branch at merge
time, before `release` has tagged anything; `delete-release-branch` deletes it only after the
tag and the GitHub release both succeeded. The branch stays available until every pull request
merged from it has been released. The
[shared release pull request's leftover sweep](versioning-modes.md#shared-release-pull-request)
depends on finding that branch. Both can be on: a branch GitHub already deleted is not an error.

## Labels and comments

### `labels`

`labels.pending` and `labels.tagged`: the label names `pr`, `release` and `check` use, default
`release: pending` and `release: tagged`. Each is one name or a list of names, the way
release-please's comma separated `label` and `release-label` work:

```yaml
labels:
  pending: ["autorelease: pending", release]
  tagged: "autorelease: tagged"
  extra: ["team: platform", automerge]
```

`pr` opens a release pull request with every pending label and adds back any one it lost. A
merged release pull request is pending, and `release` tags it, only while it carries every
pending label; `release` then removes all of them and adds every tagged label, and a draft is
resumed only on a pull request carrying every tagged label. A configuration written with one
name per role keeps working unchanged. Neither list can be empty, no name in it can be empty,
and one name configured for two roles, or twice in one, is a configuration error.

`labels.extra`: labels `pr` adds to a release pull request it opens, beside the pending ones,
release-please's `extra-label`, default none. Nothing reads them, `release` leaves them in
place, and `pr` never adds one back after a human removed it. `check` and `pr` expect them on
the repository like every other configured label.

`labels.prerelease`: the pre-release type labels `pr`, `release` and `check` use. The built-in
types are `alpha`, `beta` and `rc`, in that order, default `release: alpha`, `release: beta` and
`release: rc`:

```yaml
labels:
  prerelease:
    alpha: "release: alpha"
    beta: false
    rc: "release: rc"
    next: "release: next"
```

A key naming `alpha`, `beta` or `rc` renames its label, keeping its position in the order.
`false`, or `""`, turns that type off, so a repository that never cuts pre-releases needs none
of the three labels. Any other key, `next` above, adds a further pre-release type, appended
after the built-in three in the order this block declares it; its value must be a non-empty
string, `false` for a type that does not exist yet is pointless and a configuration error.

See [Pre-releases](commands.md#pre-releases): a type's label on the release pull request turns
the next run into `X.Y.Z-<type>.N`, with a counter of its own, `check` reports the label missing
by name, the guide in the pull request body lists every enabled type in order, and `release`
swaps it for `tagged`. A pull request carrying more than one type label is an error naming them
in that same order.

A type name is one semver pre-release identifier: ASCII letters, digits and `-`, not only
digits, so `next`, `preview` and `nightly-build` work, `pre.view` and `2024` do not. A label
must differ from every other label, `labels.pending` and `labels.tagged` included; anything
else is a configuration error naming the key.

This is release-please's `prerelease-type` with a free name. A pre-release stays something a
human asks for on the release pull request: there is no key that cuts every release as a
pre-release, as release-please's `prerelease: true` does.

### `comment-mention`

The mention prefix a comment on the release pull request names to issue a `release-as` or
`release-notes` command, default `@release-me`. See
[comment commands](overrides.md#comment-commands). Cannot be empty.

## Components and workspaces

### `versioning`

`independent` (default), `locked` or `linked`. See [versioning modes](versioning-modes.md).
There is no per-component override: it applies to the whole family.

### `components`

Maps a name to a `path` and optional overrides of `initial-version`, `bump-minor-pre-major`,
`bump-patch-for-minor-pre-major`, `release-as` (only with `versioning: independent`),
`versioning-strategy`, `hidden-types`, `changelog-sections`, `files`, `major-tag`,
`changelog-source`, `changelog-type`, `force-tag-creation`, `include-v-in-release-name`,
`skip-github-release`, `github-release-draft`, `skip-changelog` and `changelog-path`, plus two
keys only a component has: `tag-prefix`, described below, and
[`exclude-paths`](#exclude-paths), which the top-level key cannot apply beside configured
components. A key left out of a component falls back to the top-level setting above. A
repository without `components` is one component named `main` at path `.`, tag prefix `v`,
branch `release-me/main`, the default shape a repository with no configuration at all already
has. A configured component gets tag prefix `<name>/v`, branch `release-me/<name>`, or
`release-me/main` when the release pull request is shared, changelog `<path>/CHANGELOG.md`,
and a release title `<name> vX.Y.Z`, without the `v` under `include-v-in-release-name: false`.
A name holds ASCII letters, digits, `-`, `_` and `.` only,
does not start with `.` or `-`, does not end with `.` or `.lock` and does not contain `..`;
anything else is a configuration error naming the
component and the character, since the name has to survive the tag prefix, the branch, the
shared pull request title release reads back, the
[Release-Component footer](overrides.md#release-component-footer) and the compare link.

`tag-prefix` replaces the derived prefix, for a component whose existing tags follow another
scheme, such as `api@v1.4.0`: release-me continues from its last such tag instead of starting
over at `initial-version`. The version is appended to the prefix as it is. A prefix that is
empty, holds whitespace or one of `~ ^ : ? * [ \`, contains `..`, `@{` or `//`, or starts with
`/`, `-` or `.` is a configuration error, as are two components with the same prefix. On top
of these git ref rules, a prefix holding `(`, `)`, `#` or `%` is one too, as those break the
changelog heading's compare link. The prefix is how a component finds its history: adding,
changing or removing `tag-prefix` once a component has tags under its previous prefix restarts
it at `initial-version`, the same way renaming the component would.

`changelog-path` replaces the derived changelog path, relative to the repository root, for a
component whose changelog lives outside its own path, such as one kept at the root while the
code sits in `packages/<name>`; a directory it names that does not exist yet is created. A
changelog path that is empty, absolute, holds a backslash, or is not a file inside the
repository is a configuration error, as are two components writing the same changelog:

```yaml
components:
  frontend:
    path: web/frontend
    initial-version: 0.2.0
    files:
      - path: web/frontend/package.json
  backend:
    path: services/backend
    hidden-types: [chore, docs, test]
    tag-prefix: backend@v
    changelog-path: CHANGELOG.md
```

A first parent entry belongs to every component whose path it touches, through the files a
merge commit's branch changed; a pull request touching several components lands in every one
of their changelogs. An empty commit lands in every component whose `path` exists at that
commit, and a [Release-Component footer](overrides.md#release-component-footer) adds the
components it names, while [`exclude-paths`](#exclude-paths) takes subpaths out of a component. The
component at path `.`, explicit or implicit, matches every entry outside its excluded paths
and, beside other components, every entry outside their paths too.
Two components with overlapping paths, apart from a [root component](#root-component) beside
others, or an unknown name on `--component`, are configuration errors.

An unknown key is an error, inside a component too. The file is read from the repository
root, found through `git rev-parse --show-toplevel`, whatever directory the command runs
from.

#### Root component

A component at path `.` may sit beside other components, the shape of a root package that
versions the whole repository while its packages version themselves. It owns every file
outside the other components' paths, and they keep their own: a feature in `packages/web`
releases `web` only, a fix to the root `README.md` releases the root component only, and a
pull request touching both releases both. A
[Release-Component footer](overrides.md#release-component-footer) naming it still counts, and
an empty commit counts for it as for every component. A repository with a root component
alone, explicit or implicit, keeps every entry as before. Being a configured component, it
gets the derived tag prefix `<name>/v` and changelog `CHANGELOG.md`; a root component
continuing plain `vX.Y.Z` tags sets `tag-prefix: v`. Only one component can sit at the root,
and every other path still must not overlap another:

```yaml
components:
  acme:
    path: .
    tag-prefix: v
  web:
    path: packages/web
  api:
    path: packages/api
```

### `exclude-paths`

Paths whose changes never count for a component, default none. Each one is relative to the
repository root, like a `files` path, and names a directory or a file nested inside the
component's `path`: a commit, or a merged pull request, that changed only files under its
excluded paths does not reach the component's version or changelog, one that changed a file
outside them does. A [Release-Component footer](overrides.md#release-component-footer) naming
the component still counts, and an empty commit counts as it does without the key.

```yaml
components:
  web:
    path: packages/web
    exclude-paths:
      - packages/web/docs
      - packages/web/package-lock.json
```

A component carries its own `exclude-paths`; the top-level key applies to the implicit `main`
component of a repository without `components` and is a configuration error beside them. An
entry that is empty, absolute, holds a backslash, leaves the repository, equals the
component's `path` or lies outside it is a configuration error.

### `workspaces`

The package managers whose manifests link components to each other, default none:

- `node`, an npm, pnpm or yarn workspace, read from each component's `<path>/package.json`;
- `cargo`, a Cargo workspace, read from each component's `<path>/Cargo.toml`;
- `maven`, a Maven multi-module build, read from each component's `<path>/pom.xml`.

A component without that manifest is no member of the workspace. Without a component at path
`.`, the manifest at the repository root, such as a virtual Cargo workspace's `Cargo.toml` or a
Maven aggregator `pom.xml`, is read too: its dependencies on a released component move,
`[workspace.dependencies]` or `<dependencyManagement>` included, but it releases nothing
itself.

```yaml
release-pull-request: shared
workspaces: [node]
components:
  core:
    path: packages/core
  web:
    path: packages/web
```

A component that releases moves every other component's dependency on it, declared under
`dependencies`, `devDependencies`, `peerDependencies` or `optionalDependencies` by the package
name, to its new version, and that dependent releases with it in the same release pull request:
a patch bump on its own last tag in `independent`, the family's version in `linked`, and in
`locked` every component releases anyway. A dependent of a dependent releases too. In
`independent`, the dependent's own `versioning-strategy` is ignored for that patch, since it
has no changes of its own to bump by. Each dependent's changelog gets a
`- **deps:** update <package> to <version>` line under Bug Fixes.
A range keeps its operator and protocol: `^1.2.0` becomes `^1.3.0`, `workspace:~1.2.0`
becomes `workspace:~1.3.0`, while one naming no single version, such as `workspace:*`, `*`,
`file:../core` or `>=1.0.0 <2`, stays as written and still releases the dependent. A
component's own version moves through its `files` as always. `next` and `changelog` report a
component's own changes only: the dependents join the release at `pr`.

For `node`, the dependency objects must open on a line of their own and declare one
dependency per line, the layout npm, pnpm and yarn write; `pr` fails naming the file and the
dependency otherwise.

For `cargo`, a dependency on a crate by its `[package]` name, or through `package =` when
renamed, links under `[dependencies]`, `[dev-dependencies]`, `[build-dependencies]`, their
`[target.<cfg>.…]` forms and `[workspace.dependencies]`. Its version moves as a plain
requirement, `acme-core = "1.2.0"`, inside a one-line inline table,
`{ path = "../core", version = "=1.2.0" }`, or as the `version` of a
`[dependencies.acme-core]` table, keeping its operator; `1.2`, a partial version, becomes the
full new one. A dependency without a version, `{ workspace = true }` or a bare `path`, stays as
written and still releases the dependent. A dependency spread over several lines is not read.

For `maven`, a module is named `<groupId>:<artifactId>`, its `groupId` inherited from its
`<parent>` where it sets none. It links through its `<parent>`, its `<dependencies>` and its
`<dependencyManagement>`, a `${project.groupId}` read as its own group; a build plugin's
dependencies do not link. The `<version>` text moves, in a profile's dependencies too, and
every other byte stays. A version given through a property, `${core.version}`, or as a range,
`[1.0,2.0)`, stays as written and still releases the dependent. A module's own `<version>` is
not moved by `workspaces`, only by its `files`.

Lockfiles, `package-lock.json`, `pnpm-lock.yaml` or `Cargo.lock`, are not updated.

A dependent releases together with its dependency, so `workspaces` needs `components` and
one release pull request for all of them, `release-pull-request: shared` or
`versioning: locked`; an unknown or repeated package manager is a configuration error too.

Inherited `workspace = true` dependencies resolve renamed package aliases from the root
workspace declarations. The root keeps the version requirement, and the child keeps its
workspace declaration unchanged.

### `always-link-local`

Whether every `node` dependency on a local package name links the two components, default
`true`, release-please's default too. `false` links one only where it resolves to the local
package anyway: a `workspace:`, `file:`, `link:` or `portal:` protocol, or a range the local
package's current version satisfies, such as `^1.2.0` for a local `1.4.1`. Any other
dependency on the name, `~1.1.0` for that `1.4.1` or a dist-tag like `latest`, is left to the
registry: its range stays as written and it releases nothing. A dependent still linked through
another dependency releases as usual. Ranges are read the way npm reads them: `||`, hyphen
ranges, partial and `x` versions and the `=`, `<`, `<=`, `>`, `>=`, `~` and `^` operators.
Setting the key without `node` in [`workspaces`](#workspaces) is a configuration error.

## Environment

### `token-env`

The environment variable holding the GitHub token, default `GITHUB_TOKEN`. It governs every API
call, the release commit `pr` creates included, so the kind of token decides whether GitHub
signs that commit: see [signed release commits](commands.md#signed-release-commits). Reading
and publishing the release branch go through the git credentials the checkout's
`origin` remote already carries.
