# Configuration reference

Full reference for every `release-me.yaml` key. See the [README](../README.md#configuration)
for the quick table and example.

## `initial-version`

The version when no tag exists yet, default `0.1.0`.

## `bump-minor-pre-major`

Turns a major bump into a minor one below `1.0.0`, default `false`. A component overrides it
through its own `bump-minor-pre-major` key; a `locked` or `linked` family computes its one
shared version with the top-level value.

## `versioning-strategy`

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
over the strategy, the same as over the default bump. `bump-minor-pre-major` applies to
`default` and `dependency-manifest` only: an always-bump strategy bumps the part it names below
`1.0.0` too, as in release-please. `next` names the strategy in its reason,
`1.3.0 (fix, always-bump-minor)`. Any other value is a configuration error. A component
overrides it through its own `versioning-strategy` key; a `locked` or `linked` family computes
its one shared version with the top-level value.

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
  stable tag still counts from `1.2.3`. A pre-release label on its release pull request
  replaces `sp` with that type, `1.2.3-rc.1`. It applies with `versioning: independent` only;
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

## `hidden-types`

Hides entries of these types from the changelog, default `chore`, `docs`, `ci`, `style`,
`refactor`, `test`, `build`, `other`. Only `feat`, `fix`, `perf`, `revert` and a breaking
change ever render a section; a type outside those five never renders, hidden or not.

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
`release: pending` and `release: tagged`.

`labels.alpha`, `labels.beta` and `labels.rc`: the pre-release type labels `pr`, `release` and
`check` use, default `release: alpha`, `release: beta` and `release: rc`. `false`, or `""`,
turns that pre-release type off, so a repository that never cuts pre-releases needs none of
the three labels. See [Pre-releases](commands.md#pre-releases). `check` reports any of the
enabled labels missing by name, it never creates one. `labels.pending` and `labels.tagged`
cannot be empty.

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

## `components`

Maps a name to a `path` and optional overrides of `initial-version`, `bump-minor-pre-major`,
`versioning-strategy`, `hidden-types`, `files`, `major-tag`, `changelog-source` and
`changelog`. A key left out of a component falls back to the top-level setting above. A
repository without `components` is one component named `main` at path `.`, tag prefix `v`,
branch `release-me/main`, the default shape a repository with no configuration at all already
has. A configured component gets tag prefix `<name>/v`, branch `release-me/<name>`, or
`release-me/main` when the release pull request is shared, changelog `<path>/CHANGELOG.md`, and
a release title `<name> vX.Y.Z`. A name holds ASCII letters, digits, `-`, `_` and `.` only,
does not start with `.` or `-`, does not end with `.` or `.lock` and does not contain `..`;
anything else is a configuration error naming the component and the character, since the name
has to survive the tag prefix, the branch, the shared pull request title release reads back,
the [Release-Component footer](overrides.md#release-component-footer) and the compare link.

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
names. The component at path `.`, explicit or implicit, matches every entry.
Two components with overlapping paths, or an unknown name on `--component`, are configuration
errors.

An unknown key is an error, inside a component too. The file is read from the repository
root, found through `git rev-parse --show-toplevel`, whatever directory the command runs
from.
