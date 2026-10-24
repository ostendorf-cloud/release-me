# release-me

Release automation for repositories that merge with merge commits. One changelog entry per
pull request, versions from Conventional Commits, a release pull request, a tag and a release.

## What it does

- Reads commit history since the last tag in a configurable changelog mode, the first parent
  history by default: a merge commit is one entry, its pull request title is the text, a
  commit directly on the default branch is its own entry. See
  [Changelog modes](#changelog-modes).
- Computes the next version from the entry types. Pre-releases through a label on the release
  pull request.
- Opens and updates one release pull request per component with the changelog, bumps the
  version in the files you list, and on merge creates the tag and the release.
- GitHub first. GitLab follows through the same forge interface.

## Install

Through [mise](https://mise.jdx.dev), from the GitHub release's binaries:

```sh
mise use ubi:ostendorf-cloud/release-me
```

As a GitHub Action, pinned to a release tag. `release-me run` opens and updates pull requests
and creates tags and releases, so the job needs `contents: write` and `pull-requests: write`;
`secrets.GITHUB_TOKEN` cannot trigger the checks on the release pull request it opens, a
repository or GitHub App token can. A GitHub App token also lets the action attribute the
release commit to the app's own bot account, through `app-slug`:

```yaml
permissions:
  contents: write
  pull-requests: write

steps:
  - uses: actions/create-github-app-token@v3
    id: app-token
    with:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
  - uses: ostendorf-cloud/release-me/action@v0.1.0
    with:
      token: ${{ steps.app-token.outputs.token }}
      app-slug: ${{ steps.app-token.outputs.app-slug }}
```

The action downloads the archive and checksums file matching its own version for the
runner's os and arch, verifies the checksum, then runs `release-me run`. Inputs: `token`
(required), `component`, `dry-run`, `verbose`, `download-token` for the download step on a
private fork, defaulting to the calling workflow's own token, `version` to pin a different
release than the action's own tag, or `source` to build from the checkout with `go build`
instead, and `author-name`, `author-email`, `app-slug` to attribute the release commit.
An explicit `author-name` or `author-email` wins for that field; with `app-slug` set, the action resolves
the app's bot account, `<slug>[bot]`, and its id through the API with `download-token`, and
attributes the commit to it. Nothing set leaves the tool's own configured or default identity
in place. `source` only builds this repository's own code: it is `go build`'s working
directory for `uses: ./action` from inside this repository, not the caller's checkout for
`uses: ostendorf-cloud/release-me/action@ref`.

Output: `tags`, the newline-separated tags this run tagged and drafted a release for, one per
released component, empty when nothing was released. Every release release-me creates is a
draft: GitHub's immutable releases setting, when on for the repository, rejects a change to a
published release, so release-me never publishes one itself, draft or not. A caller that
attaches binary assets, such as a step running `goreleaser` right after this action, publishes
each tag afterward, for example with `gh release upload` followed by
`gh release edit --draft=false`; see this repository's own `release.yaml` for the full shape.

Trigger the calling workflow on `push` to the default branch, and also on `pull_request` with
types `labeled` and `unlabeled`. The labeled trigger reruns `release-me run` right away when a
release label changes, instead of waiting for the next push. Guard the job so the label run
only fires for a release pull request: the pull request from the repository itself rather than
a fork, head branch under `release-me/`, and the changed label one of the release labels in use,
`release: pending`, `release: tagged`, `release: alpha`, `release: beta` or `release: rc` by
default, without the pre-release types turned off. A repository that renames its labels
through `labels` in `release-me.yaml` names the renamed ones in that guard instead: the
workflow condition is plain text, it does not read the configuration. Check out the pull
request's base branch on a label event, not the merge ref, so the action computes against the
default branch.

As a container image, for runners with an image cache:

```sh
docker run --rm -e GITHUB_TOKEN ghcr.io/ostendorf-cloud/release-me:0.1.0 run
```

Images are tagged with the released version. A stable release also moves `latest`, a
pre-release does not.

### Bootstrap

Binaries, the action and the image all come from a GitHub release, and the first one has
nothing earlier to download from. It is tagged and released by hand once: create the tag,
create the GitHub release for it, then run `goreleaser release --clean` locally against that
tag to build and upload its binaries, archives and checksums file (`task build:release` runs
the same build as a `--snapshot`, without a tag or an upload, to check the build itself
locally). `publish.yaml` carries every later release the same way, automatically.

Until that first release exists, this repository's own `release.yaml` runs the action with
`version: source`, which builds `release-me` with `go build` from the checkout instead of
downloading it. Once the first release is tagged, point `release.yaml` at that version and the
tool releases itself from then on.

## Use

```sh
release-me next        # next version, nothing written
release-me changelog   # entries for the next version, nothing written
release-me pr          # create or update the release pull requests
release-me release     # tag and release merged release pull requests
release-me run         # pr then release, the CI entry point
release-me check       # config, labels and token
release-me migrate release-please   # translate a release-please setup, nothing written
```

Every writing command accepts `--dry-run`: it prints what would change and writes nothing, not
even a branch. `--verbose` enables debug logging on stderr.

Every command loops over the configured components, in `release-me.yaml`'s order, or the
single implicit `main` component a repository without `components` carries. `--component
<name>` limits a command to one. Output lines are prefixed with the component's name, `"<name>:
<line>"`, only when a command runs more than one component; a run limited to one component, by
`--component` or because the repository has only one, prints unprefixed lines, the same shape a
single-component repository always has.

`next` prints the next version and the bump reason per component, for example `0.2.0 (feat)`.
`changelog` prints the rendered entries for that version. Both print nothing for a component
with nothing to release.

### pr

Creates or updates one release pull request per component: branch `release-me/<name>` off the
repository's default branch, with that component's changelog update and its configured files
bumped to its next version. The default branch is read from the forge;
`git symbolic-ref refs/remotes/origin/HEAD` is the fallback when that call fails. The branch
itself is still forked from whatever commit is checked out, which must be the default branch's
tip, same as `next` and `changelog`. The branch is written in a temporary checkout, never in
your working copy.

`pr` checks that the `pending`, `tagged`, `alpha`, `beta` and `rc` labels exist before it writes
anything, the same check `check` runs. The labels are shared across every component; the
default branch is resolved once and shared too.

Rerunning `pr` is judged on content, not on the rendered pull request body: the version in the
changelog heading, the changelog entries for that version without the heading's date, and every
configured file's bumped content are compared against the release branch's current tip. A version
that differs from the one already on the branch, a pre-release label added, changed or removed,
counts as changed even when the entries underneath read the same. A heading the comparison cannot
read, a hand edit or one from before this tool wrote it, counts as changed too, and pushes; the
push rewrites it to this tool's own shape, so it settles after that one push. Equal content
changes nothing, not even the pull request body, and nothing is pushed. A change pushes the
branch and sets the heading to today's date; the push carries a `--force-with-lease` whose
expected value is the existing pull request's head SHA from the forge, not a locally fetched
ref, so nothing can quietly defeat it. A fresh branch, with no existing pull request to lease
against, pushes without force.
Editing only the notes block of an open pull request updates its body, with no push: the content
that would land on the branch did not change.

Updating an existing pull request never replaces its labels: a label added by hand stays, and
the `pending` label is added back if it is ever missing, never set by replacing the full list.

The pull request title is `chore(<name>): release vX.Y.Z`, `chore(main): release vX.Y.Z` for
the implicit `main` component. Its body starts with `Release vX.Y.Z`, then a collapsed
`<details><summary>How this release works</summary>` guide naming the tag merging creates and
the configured labels for pre-releases and tagging, then the notes block (below) when one is
present, then the full changelog for the version inside a collapsed `<details>` section. When
every entry is a hidden type, the changelog carries one explicit line,
`- Version bump without user facing changes.`, instead of an empty heading. The guide never
reaches the changelog file or the GitHub release.

### Pre-releases

A pre-release type label on the open release pull request, `release: alpha`, `release: beta`
or `release: rc` by default, turns its next run into a pre-release: version `X.Y.Z-alpha.N`,
`X.Y.Z-beta.N` or `X.Y.Z-rc.N`. `X.Y.Z` is the version a stable release would get from the
entries since the last stable tag; pre-release tags never move that base. Exactly one of the
three labels may be present at a time; two at once is an error naming both. A type whose label
is set to `""` in `release-me.yaml` is turned off: release-me neither expects that label on the
repository nor reads it from the pull request, and the guide in the pull request body leaves
it out. A pull request that already carries the label of a type just turned off keeps it as an
ordinary label: the next `pr` run returns the version to the stable `X.Y.Z`, and `release`
leaves that label in place when it swaps `pending` for `tagged`. One name configured for two
labels is a configuration error.

`N` starts at `1` for a type and counts up from the highest `N` an earlier pre-release tag of
that same type and base version already carries. Switching the label to a different type does
not inherit the previous type's counter: it starts at `1` again, even when tags for the other
type already exist.

The changelog a pre-release renders depends on what came before it: the first pre-release since
the last stable tag collects the same entries a stable release would, since that stable tag; a
pre-release that follows another one collects only the entries since that previous pre-release
tag. Removing the label from the open pull request returns it to the stable version on the next
run, branch content, title and body updated accordingly.

### release

Tags and releases every merged pull request on a component's release branch that still carries
the `pending` label: a tag on the merge commit, a GitHub release with the pull request's notes
and changelog, draft always true and prerelease true for a pre-release version. A draft keeps
the release safe from GitHub's immutable releases feature until a caller, such as a workflow
that still needs to attach binary assets, publishes it. The label
swap removes `pending`, and the matching pre-release type label when the version carries one,
replacing both with `tagged`. The implicit `main` component tags `vX.Y.Z` and names the release
after the tag, today's shape; a configured component tags `<name>/vX.Y.Z`, or its `tag-prefix`
followed by the version, and names the release `<name> vX.Y.Z`. A tag that already exists,
left over from an earlier, interrupted run, is not an error: the release and the label swap
still happen, so the pull request does not stay pending forever, and the run reports it as
already tagged.

With `major-tag: true`, every stable release additionally creates or force-moves the major
tag, `vX` for the implicit `main` component or its tag prefix followed by `X` for a configured
one, onto the same commit. A tag that does not exist yet is created; one that does is moved,
never left behind. A pre-release, once that lands, never moves it. `--dry-run` reports whether
the major tag would be created or moved, writing nothing.

### run

`pr`, then `release`, for every component. The CI entry point: one command covers opening or
updating every release pull request and releasing whatever merged since the last run.

### check

Validates `release-me.yaml`, including an unknown `--component` name, that the `pending`,
`tagged`, `alpha`, `beta` and `rc` labels exist on the repository, and that the token
environment variable is both set and accepted by the forge: a rejected token is reported as
such, not as a missing label, since the symptom from the forge is the same 401 either way.
Prints every missing or invalid item and exits 1 when anything is missing, 0 otherwise.

### migrate

`migrate release-please` reads `release-please-config.json` and `.release-please-manifest.json`
from the repository root and prints the equivalent `release-me.yaml` on stdout, so it can be
reviewed before it is written: `release-me migrate release-please > release-me.yaml`. It writes
nothing itself.

Each release-please package becomes a component named after its `component`, else its
`package-name` with a node scope removed, else the name in its `package.json`. Its tags and
changelog stay where release-please put them, through `tag-prefix` and `changelog`, so
release-me continues from the existing tags and changelogs. A single root package with plain
`vX.Y.Z` tags becomes the implicit `main` component instead. It also carries over:

- `bump-minor-pre-major` and `initial-version`;
- the version file of a `node` (`package.json`) or `helm` (`Chart.yaml`) package where that
  file exists, `node` being release-please's default type, and every `extra-files` entry of
  type `json`, `yaml` or `toml` with a plain `$.a.b` jsonpath;
- `label` and `release-label` as the `pending` and `tagged` labels, with the pre-release types
  turned off, since release-please has none;
- a `linked-versions` plugin over every component as `versioning: locked`: both release every
  component at the family's version, changed or not.

`changelog-source` is set to `all`, because release-please reads every commit since the last
release, not the first parent history.

Every setting it cannot carry over is printed on stderr as a `warning:` line, each naming the
key and what to do instead, as are the differences a reader should know about, such as
release-me opening one release pull request per component where release-please opened one for
all of them. It then looks up the tag each package's manifest version was released under and
warns about a missing one, since release-me would start that component over at its initial
version. The translation is loaded the way every other command loads `release-me.yaml` before
it is printed. One release-me refuses, such as a root package beside other packages, whose
paths overlap, is printed on stderr after the warnings and the reason, to correct by hand.
Exits 0 with warnings, 1 when the files cannot be read or translated or the translation does not
load.

### Notes block

A fenced block `release-me-notes` in the release pull request body is free text for the
release:

````markdown
```release-me-notes
Run the migration before upgrading.
```
````

Add it by hand once the pull request is open. `pr` keeps it verbatim on every later update, and
`release` carries it into the GitHub release, above the changelog.

### Override block

A fenced block `release-me` in an ordinary pull request body overrides its changelog entries,
one Conventional Commits line per entry:

````markdown
```release-me
feat(cli): add the next command
fix: stop the panic on empty history
```
````

An empty block hides the pull request from the changelog and the version computation
entirely.

### Changelog modes

`changelog-source` selects which commits become changelog entries, top level and per
component, default `first-parent`. In every mode, a pull request's
[override block](#override-block) replaces its entries, the merge's or the individual
commits' alike.

- `first-parent`: today's behaviour. The first parent history since the last tag: a merge
  commit is one entry, its pull request title is the text; a commit directly on the default
  branch is its own entry.

  ```text
  Merge pull request #42 from owner/add-widget
  ```

  renders as one entry, its description read from pull request #42's title.

- `squash`: every commit on the default branch is one entry, the shape a repository that
  squash merges leaves behind: it has no merge commits of its own. The pull request number
  comes from a trailing `(#N)` on the commit subject, the title is the subject with that
  suffix removed, and the commit body is the fallback text for the override block, the same
  role a merge commit's body plays in `first-parent`. A genuine merge commit is still one
  entry.

  ```text
  feat(cli): add the next command (#42)
  ```

  renders as pull request #42, description "add the next command".

- `all`: every commit reachable since the last tag, merge commits excluded, each its own
  entry, parsed as a conventional commit from its own subject, never a pull request title. A
  commit a merge introduced carries that merge's pull request link, resolved through the
  forge once per merge commit, not once per commit it introduced; a commit directly on the
  default branch carries none. A rebase merge leaves no merge commit either, so it already
  reads the way `all` reads any other commit: its own entry.

- `nested`: the first parent entry as today, with the commits the merge introduced rendered
  as indented list items under it, each parsed as a conventional commit on its own; a hidden
  type is dropped from that list the same way it is from the top level.

  ```markdown
  - add the widget ([#42](https://example.com/pull/42))
    - wire up the flag
    - handle the empty case
  ```

  The version bump considers the sub items too: a merge whose own title is a hidden type,
  `chore`, say, still bumps the version when one of the commits it introduced is a `feat` or a
  `fix`.

`next` and `changelog --verbose` log the resolved mode as `source`.

## Versioning modes

The top-level `versioning` key chooses how a repository's configured components' versions
relate to each other: `independent` (default), `locked` or `linked`.

`independent` versions every component on its own: its own tag line, its own bump, its own
release pull request, the shape every example above already is.

`locked` bumps every component to the same new version and releases all of them together,
changed or not: one release pull request, branch `release-me/main`, with one changelog section
per component, each updating its own `CHANGELOG.md`. Its title names the version and every
component it covers, `chore: release vX.Y.Z (a, b, c)`. On merge, every component is tagged
`<name>/vX.Y.Z` with the same version and released the same way an independent component is.
`locked` always releases the whole family, so `--component` naming one of them is a usage
error for `next`, `changelog`, `pr`, `release` and `run` alike.

`linked` shares one version line across every component, but releases only a component with
entries since its own last tag, at the family's next version: one release pull request per
changed component, as `independent` already opens, the shared version applied to each. An
unchanged component keeps its version and gets no release.

In both `locked` and `linked`, the next version is `bump(highest version in the family, highest
bump type among the changed components)`: the highest version any component in the family
carries, bumped by the highest bump type among the components with something to release. Every
changed component in a run gets that version, even one already behind the family's highest.
Pre-release counters are shared across the family in both modes: `N` counts up from the highest
`N` any component's own pre-release tag of that type and base version already carries.

`next` prints the family version and which components move; `changelog` prints only the changed
ones, nothing for a component with nothing to release, even in `locked`, where it still gets
tagged and released.

The example below is `linked` with a family on `0.8.0` and a fix landing in one or two
components per release:

| Step  | Change         | a     | b     | c     |
| ----- | -------------- | ----- | ----- | ----- |
| start |                | 0.8.0 | 0.8.0 | 0.8.0 |
| 1     | fix in a       | 0.8.1 | 0.8.0 | 0.8.0 |
| 2     | fix in c       | 0.8.1 | 0.8.0 | 0.8.2 |
| 3     | fix in a       | 0.8.3 | 0.8.0 | 0.8.2 |
| 4     | fix in b and c | 0.8.3 | 0.8.4 | 0.8.4 |

`locked` for the same family releases every component at every step: a fix anywhere still
bumps and releases a, b and c together, even the ones without an entry of their own.

## Configuration

`release-me.yaml` at the repository root. A repository without `components` is one component.

```yaml
initial-version: 0.1.0
bump-minor-pre-major: true
hidden-types: [chore, docs, ci, style, refactor, test, build]
changelog: CHANGELOG.md
labels:
  pending: "release: pending"
  tagged: "release: tagged"
  alpha: "release: alpha"
  beta: "release: beta"
  rc: "release: rc"
author:
  name: release-me
  email: release-me[bot]@users.noreply.github.com
files:
  - path: cmd/cli/version.go
    updater: marker
```

- `initial-version`: the version when no tag exists yet, default `0.1.0`.
- `bump-minor-pre-major`: turns a major bump into a minor one below `1.0.0`, default `false`.
- `hidden-types`: hides entries of these types from the changelog, default `chore`, `docs`,
  `ci`, `style`, `refactor`, `test`, `build`, `other`. Only `feat`, `fix`, `perf`, `revert` and
  a breaking change ever render a section; a type outside those five never renders, hidden or
  not.
- `fallback-type`: the type assigned to text that does not parse as a conventional commit,
  default `other`. `feat`, `fix`, `perf` and `revert` are refused, unparseable text never
  bumps the version.
- `token-env`: the environment variable holding the GitHub token, default `GITHUB_TOKEN`. It
  governs API calls only; `pr`'s push to the release branch authenticates through the git
  credentials the checkout's `origin` remote already carries, the same as any other push.
- `changelog`: the changelog file path, default `CHANGELOG.md`. `pr` prepends the new version's
  section to it, creating the file when it does not exist yet.
- `changelog-source`: which commits become changelog entries, default `first-parent`. See
  [Changelog modes](#changelog-modes) for `first-parent`, `squash`, `all` and `nested`. A
  component overrides it through its own `changelog-source` key.
- `labels.pending` and `labels.tagged`: the label names `pr`, `release` and `check` use,
  default `release: pending` and `release: tagged`.
- `labels.alpha`, `labels.beta` and `labels.rc`: the pre-release type labels `pr`, `release`
  and `check` use, default `release: alpha`, `release: beta` and `release: rc`. `""` turns
  that pre-release type off, so a repository that never cuts pre-releases needs none of the
  three labels. See [Pre-releases](#pre-releases). `check` reports any of the enabled labels
  missing by name, it never creates one. `labels.pending` and `labels.tagged` cannot be empty.
- `author.name` and `author.email`: the git identity `pr` commits the release branch as,
  default `release-me` and `release-me[bot]@users.noreply.github.com`. Passed to the commit
  through the git subprocess's environment, never written into git config. A
  `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME` or `GIT_COMMITTER_EMAIL` already
  set in the environment wins over these values. `check --verbose` logs the resolved author.
- `files`: files `pr` bumps to the next version, each a `path`, an optional `updater`, default
  `marker`, and a `key` where the updater needs one. Every updater keeps the rest of the file
  byte for byte: order, comments, indentation. A file that does not exist, or a `key` not found
  in it, fails `pr`, naming the path and the key.

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

  - `json`, `yaml` and `toml` replace the value at `key`, a dotted path of object keys,
    mapping keys or table-and-key, such as `tool.poetry.version`.

  Every file entry is validated when release-me.yaml loads: an unknown `updater`, a `key`
  missing where the updater requires one, and a `key` given to an updater that takes none are
  all errors.

- `major-tag`: moves the major tag on every stable release, default `false`. See `release`
  above. A component overrides it through its own `major-tag` key.
- `versioning`: `independent` (default), `locked` or `linked`. See
  [Versioning modes](#versioning-modes). There is no per-component override: it applies to the
  whole family.
- `components`: maps a name to a `path` and optional overrides of `initial-version`,
  `bump-minor-pre-major`, `hidden-types`, `files`, `major-tag`, `changelog-source` and
  `changelog`. A key
  left out of a component falls back to the top-level setting above. A repository without
  `components` is
  one component named `main` at path `.`, tag prefix `v`, branch `release-me/main`, the shape
  every example above already is. A configured component gets tag prefix `<name>/v`, branch
  `release-me/<name>`, changelog `<path>/CHANGELOG.md`, and a release title `<name> vX.Y.Z`.
  `tag-prefix` replaces the derived prefix, for a component whose existing tags follow
  another scheme, such as `api@v1.4.0`: release-me continues from its last such tag instead of
  starting over at `initial-version`. The version is appended to the prefix as it is. A prefix
  that is empty, holds whitespace or one of `~ ^ : ? * [ \`, contains `..`, `@{` or `//`, or
  starts with `/`, `-` or `.` is a configuration error, as are two components with the same
  prefix. The prefix is how a component finds its history: adding, changing or removing
  `tag-prefix` once a component has tags under its previous prefix restarts it at
  `initial-version`, the same way renaming the component would. `changelog` replaces the
  derived changelog path, relative to the repository root, for a component whose changelog
  lives outside its own path, such as one kept at the root while the code sits in
  `packages/<name>`; a directory it names that does not exist yet is created. A changelog path
  that is empty, absolute, holds a backslash, or is not a file inside the repository is a
  configuration error, as are two components writing the same changelog:

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
  of their changelogs. The component at path `.`, explicit or implicit, matches every entry.
  Two components with overlapping paths, or an unknown name on `--component`, are configuration
  errors.

An unknown key is an error, inside a component too. The file is read from the repository
root, found through `git rev-parse --show-toplevel`, whatever directory the command runs
from.

## Agent skills

`skills/` carries agent skills for coding agents, installed through [skills.sh](https://skills.sh):

```sh
npx skills add ostendorf-cloud/release-me
```

- `setup-release-me`: sets up release-me in a repository with no release tool yet, from the
  survey of its packages and tags to a `release-me check` and `release-me next` that pass.
- `migrate-release-please-to-release-me`: moves a repository from release-please to release-me
  through `release-me migrate`, settling every warning it prints, rewiring whatever consumed
  release-please's outputs, and proving each component continues from its last tag.

## Development

```sh
task setup
task --list
task check
```

One target per check, one CI job per target. `check` only calls the small targets.

## License

Apache-2.0. See `LICENSE`.
