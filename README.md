# release-me

Release automation for repositories that merge with merge commits. One changelog entry per
pull request, versions from Conventional Commits, a release pull request, a tag and a release.

## What it does

- Reads commit history since the last tag in a configurable changelog mode, the first parent
  history by default: a merge commit is one entry, its pull request title is the text, a
  commit directly on the default branch is its own entry. See
  [Changelog modes](docs/changelog-modes.md).
- Computes the next version from the entry types. Pre-releases through a label on the release
  pull request.
- Opens and updates one release pull request per component, or one shared by every component,
  with the changelog, bumps the version in the files you list, and on merge creates the tag and
  the release.
- GitHub first. GitLab follows through the same forge interface.

## Install

Through [mise](https://mise.jdx.dev), from the GitHub release's binaries:

```sh
mise use ubi:ostendorf-cloud/release-me
```

As a GitHub Action, pinned to a commit digest, the convention every action in this
organisation follows. `release-me run` opens and updates pull requests and creates tags and
releases, so the job needs `contents: write` and `pull-requests: write`; `secrets.GITHUB_TOKEN`
cannot trigger the checks on the release pull request it opens, a repository or GitHub App
token can.

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
  - uses: ostendorf-cloud/release-me/action@<commit sha>  # v0.1.1
    with:
      token: ${{ steps.app-token.outputs.token }}
      app-slug: ${{ steps.app-token.outputs.app-slug }}
```

A GitHub App token also lets the action attribute the release commit to the app's own bot
account, through `app-slug`. See [the Action reference](docs/action.md) for every input and
output, the `draft-until-assets-attach` workflow shapes, the labeled-trigger guard, and
[Bootstrap](docs/action.md#bootstrap) for the first release before any binary exists.

As a container image, for runners with an image cache:

```sh
docker run --rm -e GITHUB_TOKEN ghcr.io/ostendorf-cloud/release-me:0.1.0 run
```

Images are tagged with the released version. A stable release also moves `latest`, a
pre-release does not.

## Use

```sh
release-me next        # next version, nothing written
release-me changelog   # entries for the next version, nothing written
release-me pr          # create or update the release pull requests
release-me release     # tag and release merged release pull requests
release-me run         # pr then release, the CI entry point
release-me check       # config, labels and token
release-me comment     # apply a release-as or release-notes comment command
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

`next` prints the next version and the bump reason per component, for example `0.2.0 (feat)`,
or a forced version's own reason; see [commands](docs/commands.md) and
[overrides](docs/overrides.md). `changelog` prints the rendered entries for that version. Both
print nothing for a component with nothing to release.

`pr` creates or updates the release pull requests, one per component or one shared by every
component. `release` tags and releases merged release pull requests still carrying the pending
label, and resumes publishing a release left a draft under `draft-until-assets-attach`. `run`
is `pr` then `release` for every component, the CI entry point. `check` validates the
configuration, the release labels and the token. `comment` applies a release-as or
release-notes comment command posted on the release pull request. `migrate release-please`
translates a release-please setup to `release-me.yaml`, writing nothing itself. A pre-release
type label on the open release pull request turns its next run into a pre-release. See
[commands](docs/commands.md) for the full reference, including pull request branch naming,
rerun behavior and pre-releases.

A pull request body can carry a notes block for the release, an override block replacing its
changelog entries, or a `Release-As`/`Release-Component` footer in a commit message forcing
the version or attributing it to a component. A comment on the release pull request can do the
same for version and notes without editing the body. See [overrides](docs/overrides.md) for
the full reference.

`changelog-source` selects which commits become changelog entries: `first-parent` (default),
`squash`, `all` or `nested`. See [changelog modes](docs/changelog-modes.md) for what each mode
reads and renders.

The top-level `versioning` key chooses how configured components' versions relate to each
other: `independent` (default), `locked` or `linked`. A shared release pull request, one pull
request for every component instead of one each, is available as an option in any mode. See
[versioning modes](docs/versioning-modes.md) for the full reference.

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

| Key | Purpose | Default | Required |
| --- | --- | --- | --- |
| `initial-version` | Version used when no tag exists yet. | `0.1.0` | No |
| `bump-minor-pre-major` | Turns a major bump into a minor one below `1.0.0`. | `false` | No |
| `hidden-types` | Entry types hidden from the changelog. | `chore, docs, ci, style, refactor, test, build, other` | No |
| `fallback-type` | Type assigned to text that does not parse as a conventional commit. | `other` | No |
| `token-env` | Environment variable holding the GitHub token. | `GITHUB_TOKEN` | No |
| `changelog` | Changelog file path. | `CHANGELOG.md` | No |
| `changelog-source` | Which commits become changelog entries. | `first-parent` | No |
| `labels.pending` / `labels.tagged` | Label names `pr`, `release` and `check` use. | `release: pending` / `release: tagged` | No |
| `labels.alpha` / `labels.beta` / `labels.rc` | Pre-release type labels. | `release: alpha` / `release: beta` / `release: rc` | No |
| `author.name` / `author.email` | Git identity `pr` commits the release branch as. | `release-me` / `release-me[bot]@users.noreply.github.com` | No |
| `files` | Files `pr` bumps to the next version. | none | No |
| `major-tag` | Moves the major tag on every stable release. | `false` | No |
| `versioning` | How components' versions relate to each other. | `independent` | No |
| `release-pull-request` | One release pull request per component or one shared by every component. | `per-component` | No |
| `release-pull-request-layout` | How a release pull request with several components lays out each one: `headings`, `details` or `compact`. | `headings` | No |
| `draft-until-assets-attach` | Drafts every release instead of publishing it immediately. | `false` | No |
| `comment-mention` | Mention prefix for `release-as`/`release-notes` comment commands. | `@release-me` | No |
| `components` | Maps a name to a path and optional per-component overrides. | none, one implicit `main` component | No |

See [the configuration reference](docs/configuration.md) for every key's full behavior, the
six `files` updater types, and the `components` rules.

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
