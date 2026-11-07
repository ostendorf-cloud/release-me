# Configuration reference

Full reference for every `release-me.yaml` key. See the [README](../README.md#configuration)
for the quick table and example.

## `initial-version`

The version when no tag exists yet, default `0.1.0`.

## `bump-minor-pre-major`

Turns a major bump into a minor one below `1.0.0`, default `false`. A component overrides it
through its own `bump-minor-pre-major` key; a `locked` or `linked` family computes its one
shared version with the top-level value.

## `bump-patch-for-minor-pre-major`

Turns a minor bump, a `feat`, into a patch one below `1.0.0`, default `false`, so `0.4.1` with a
`feat` releases `0.4.2` instead of `0.5.0`. A breaking change is not affected: it still bumps
the major, or the minor with `bump-minor-pre-major: true`, never only the patch. From `1.0.0`
on, and for a component's first release, which gets `initial-version`, the key changes nothing.
A component overrides it through its own `bump-patch-for-minor-pre-major` key; a `locked` or
`linked` family computes its one shared version with the top-level value. `next` names the
rule in its reason, `0.4.2 (feat, patch below 1.0.0)`.

## `hidden-types`

Hides entries of these types from the changelog, default `chore`, `docs`, `ci`, `style`,
`refactor`, `test`, `build`, `other`. Only `feat`, `fix`, `perf`, `revert` and a breaking
change ever render a section; a type outside those five never renders, hidden or not.

## `changelog-sentence-case`

Upper cases the first letter of every entry's description, default `false`, the counterpart of
release-please's `sentence-case` plugin: `feat(cli): add the widget` renders as
`**cli:** Add the widget`. Only a lower case ASCII letter at the very start changes. A first
word that already holds an upper case letter, such as `iOS` or `gRPC`, stays as written, and so
do `npm` and a description starting with a backtick or a digit. The pull request title or
commit stays as it is; only the rendered line changes. There is no per-component override.

## `fallback-type`

The type assigned to text that does not parse as a conventional commit, default `other`.
`feat`, `fix`, `perf` and `revert` are refused, unparseable text never bumps the version.

## `token-env`

The environment variable holding the GitHub token, default `GITHUB_TOKEN`. It governs API
calls only; `pr`'s push to the release branch authenticates through the git credentials the
checkout's `origin` remote already carries, the same as any other push.

## `changelog`

The changelog file path, default `CHANGELOG.md`. `pr` prepends the new version's section to
it, creating the file when it does not exist yet.

## `changelog-source`

Which commits become changelog entries, default `first-parent`. See
[changelog modes](changelog-modes.md) for `first-parent`, `squash`, `all` and `nested`. A
component overrides it through its own `changelog-source` key.

## `labels`

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
name per role keeps working unchanged.

`labels.extra`: labels `pr` adds to a release pull request it opens, beside the pending ones,
release-please's `extra-label`, default none. Nothing reads them, `release` leaves them in
place, and `pr` never adds one back after a human removed it. `check` and `pr` expect them on
the repository like every other configured label.

`labels.alpha`, `labels.beta` and `labels.rc`: the pre-release type labels `pr`, `release` and
`check` use, default `release: alpha`, `release: beta` and `release: rc`. `false`, or `""`,
turns that pre-release type off, so a repository that never cuts pre-releases needs none of
the three labels. See [Pre-releases](commands.md#pre-releases). `check` reports any of the
enabled labels missing by name, it never creates one. `labels.pending` and `labels.tagged`
cannot be empty, no name can be empty, and one name in two roles, or twice in one, is a
configuration error: an extra label named like a pre-release label would select that type.

## `author`

`author.name` and `author.email`: the git identity `pr` commits the release branch as, default
`release-me` and `release-me[bot]@users.noreply.github.com`. Passed to the commit through the
git subprocess's environment, never written into git config. A `GIT_AUTHOR_NAME`,
`GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME` or `GIT_COMMITTER_EMAIL` already set in the
environment wins over these values. `check --verbose` logs the resolved author.

## `files`

Files `pr` bumps to the next version, each a `path`, an optional `updater`, default `marker`,
and a `key` where the updater needs one. Every updater keeps the rest of the file byte for
byte: order, comments, indentation. A file that does not exist, or a `key` not found in it,
fails `pr`, naming the path and the key.

- `marker` replaces the version on a line ending in `# release-me: version` or
  `// release-me: version`, keeping the rest of the line, including the comment, as it is. No
  `key`:

  ```go
  const Version = "0.1.0" // release-me: version
  ```

- `generic` reads release-please's version markers, so a file release-please bumped needs no
  change. A line carrying `x-release-please-version` gets its first full version replaced; one
  carrying `x-release-please-major`, `-minor` or `-patch` gets its first number replaced by
  that part of the version. A block opened by `x-release-please-start-version` (or `-major`,
  `-minor`, `-patch`) bumps every line up to and including the one carrying
  `x-release-please-end`. The markers sit in any comment, which is how a Markdown file is
  bumped. No `key`:

  ```markdown
  <!-- x-release-please-start-version -->
  curl -fsSL https://example.com/notes/1.4.0/install.sh | sh
  <!-- x-release-please-end -->
  ```

  The `date` and `version-date` scopes are an error, since an updater is given the version
  only, not the release date. So are a file with no marker and a block that is never closed.
  As in release-please, an inline marker inside a block takes precedence for that line;
  put the end marker on its own line so it closes the block. An end marker outside a block
  has no effect. The major scope also matches a number prefixed with `v`,
  whereas minor and patch require a word boundary before the number.

- `helm` replaces a `Chart.yaml`'s top-level `version` field. No `key`:

  ```yaml
  version: 0.1.0
  ```

- `go` replaces the string literal assigned to the constant or variable `key` names:

  ```yaml
  files:
    - path: internal/version/version.go
      updater: go
      key: Version
  ```

- `json`, `yaml` and `toml` replace the value at `key`, a dotted path of object keys, mapping
  keys or table-and-key, such as `tool.poetry.version`.

- `npm-lock` bumps a `package-lock.json` or `npm-shrinkwrap.json`: its top-level `version` and,
  in lockfile version 2 or 3, the root package's entry `packages[""].version`. Dependencies and
  workspace packages are left alone. No `key`.

- `cargo-lock` bumps the `[[package]]` entry of the crate `key` names in a `Cargo.lock`. Only an
  entry without a `source` matches, which is how Cargo records a crate of the workspace itself,
  so a same-named crate from a registry is never touched. A workspace releasing several crates
  lists the file once per crate:

  ```yaml
  files:
    - path: Cargo.toml
      updater: toml
      key: package.version
    - path: Cargo.lock
      updater: cargo-lock
      key: release-core
  ```

- `gemfile-lock` bumps the specs entry `name (version)` of the gem `key` names in a
  `Gemfile.lock`, indented by four spaces as Bundler lists a resolved gem; a dependency's
  constraint, indented deeper, is left alone. A pre-release is written the way Bundler records
  it, `1.2.0-rc.1` as `1.2.0.pre.rc.1`.

- `xml` replaces the text of every element at `key`, a dotted path of element names from the
  root element down. Names match without their namespace prefix, and the whitespace around the
  text is kept. An element that is empty-tagged, `<version/>`, or holds a child element or a
  comment is an error. A `pom.xml` and a `.csproj`:

  ```yaml
  files:
    - path: pom.xml
      updater: xml
      key: project.version
    - path: src/Acme.Notes/Acme.Notes.csproj
      updater: xml
      key: Project.PropertyGroup.Version
  ```

- `plain` replaces the whole content of a file holding nothing but the version, such as a
  `version.txt` or a `VERSION` file, where a line has no room for a marker comment. The
  whitespace around the version, a trailing newline or none, is kept. An empty file, or one
  holding anything besides the version, is an error. No `key`.

Every file entry is validated when release-me.yaml loads: an unknown `updater`, a `key`
missing where the updater requires one, and a `key` given to an updater that takes none are
all errors.

## `major-tag`

Moves the major tag on every stable release, default `false`. See [release](commands.md#release)
for what `true` changes. A component overrides it through its own `major-tag` key.

## `versioning`

`independent` (default), `locked` or `linked`. See [versioning modes](versioning-modes.md).
There is no per-component override: it applies to the whole family.

## `release-pull-request`

`per-component`, the default for `independent` and `linked`, or `shared`, always the case for
`locked`, which rejects `per-component`. See
[shared release pull request](versioning-modes.md#shared-release-pull-request).

## `release-pull-request-layout`

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

## `release-pull-request-guide`

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

`migrate` leaves the key out, so a migrated repository keeps the guide.

## `release-pull-request-title`

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

## `release-pull-request-shared-title`

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

## `draft-until-assets-attach`

Drafts every release instead of publishing it immediately, default `false`. There is no
per-component override: it applies to the whole repository. Only a repository with GitHub's
immutable releases setting turned on needs `true`; it rejects any later change to a published
release, assets included, and release-me has no way to detect that setting through the API.
See [release](commands.md#release) for what `true` changes, and
[draft until assets attach](action.md#draft-until-assets-attach) for the calling workflow each
setting needs: `publish.yaml`'s own webhook-triggered asset step for `false`, `release.yaml`
attaching and publishing the release itself for `true`.

## `comment-mention`

The mention prefix a comment on the release pull request names to issue a `release-as` or
`release-notes` command, default `@release-me`. See
[comment commands](overrides.md#comment-commands). Cannot be empty.

## `workspaces`

The package managers whose manifests link components to each other, default none:

- `node`, an npm, pnpm or yarn workspace, read from each component's `<path>/package.json`;
- `cargo`, a Cargo workspace, read from each component's `<path>/Cargo.toml`.

A component without that manifest is no member of the workspace. Without a component at path
`.`, the manifest at the repository root, such as a virtual Cargo workspace's `Cargo.toml`, is
read too: its dependencies on a released component move,
`[workspace.dependencies]` included, but it releases nothing itself.

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
`locked` every component releases anyway. A dependent of a dependent releases too. Each
dependent's changelog gets a `- **deps:** update <package> to <version>` line under Bug Fixes.
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

Lockfiles, `package-lock.json`, `pnpm-lock.yaml` or `Cargo.lock`, are not updated.

A dependent releases together with its dependency, so `workspaces` needs `components` and
one release pull request for all of them, `release-pull-request: shared` or
`versioning: locked`; an unknown or repeated package manager is a configuration error too.

Inherited `workspace = true` dependencies resolve renamed package aliases from the root
workspace declarations. The root keeps the version requirement, and the child keeps its
workspace declaration unchanged.

## `exclude-paths`

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

## `components`

Maps a name to a `path` and optional overrides of `initial-version`, `bump-minor-pre-major`,
`bump-patch-for-minor-pre-major`, `hidden-types`, `files`, `major-tag`, `changelog-source` and
`changelog`, plus its own [`exclude-paths`](#exclude-paths). A key left out of a component
falls back to the top-level setting above. A repository without `components` is one component
named `main` at path `.`, tag prefix `v`, branch `release-me/main`, the default shape a
repository with no configuration at all already has. A configured component gets tag prefix
`<name>/v`, branch `release-me/<name>`, or `release-me/main` when the release pull request is
shared, changelog `<path>/CHANGELOG.md`, and a release title `<name> vX.Y.Z`. A name holds
ASCII letters, digits, `-`, `_` and `.` only, does not start with `.` or `-`, does not end with
`.` or `.lock` and does not contain `..`; anything else is a configuration error naming the
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

`changelog` replaces the derived changelog path, relative to the repository root, for a
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
    changelog: CHANGELOG.md
```

A first parent entry belongs to every component whose path it touches, through the files a
merge commit's branch changed; a pull request touching several components lands in every one
of their changelogs. An empty commit lands in every component, and a
[Release-Component footer](overrides.md#release-component-footer) adds the components it
names, while [`exclude-paths`](#exclude-paths) takes subpaths out of a component. The
component at path `.`, explicit or implicit, matches every entry outside its excluded paths
and, beside other components, every entry outside their paths too.
Two components with overlapping paths, apart from a [root component](#root-component) beside
others, or an unknown name on `--component`, are configuration errors.

An unknown key is an error, inside a component too. The file is read from the repository
root, found through `git rev-parse --show-toplevel`, whatever directory the command runs
from.

### Root component

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
