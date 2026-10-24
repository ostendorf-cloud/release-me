# release-me

Go CLI that releases repositories from their first parent history. The baseline rules apply
and are not repeated here.

## Layout

- `cmd/release-me/`: the entry point, flags and command dispatch only.
- `internal/config/`: loads `release-me.yaml`, unknown keys rejected.
- `internal/forge/`: the forge interface and the GitHub implementation, with a fake in
  `internal/forge/forgetest` for tests. GitLab lands next to it.
- `internal/history/`: first parent walk, tags, components by path.
- `internal/conventional/`: pull request titles, commit subjects and override blocks.
- `internal/version/`: bump rules and pre-releases.
- `internal/changelog/`: sections and rendering.
- `internal/updater/`: marker and typed file updaters.
- `internal/releasepr/`: branch, body, labels and the notes block of a release pull request.
- `internal/release/`: tag, release and major tag.
- `internal/migrate/`: another release tool's configuration translated into `release-me.yaml`.
- `action/`: the composite action. `Dockerfile`: the image for runners with an image cache.
- `scripts/`: CI glue specific to this repository's own workflows, such as attaching and
  publishing release assets after `release.yaml` drafts a release.
- `release-me.yaml`: the tool's own release configuration.
- `skills/`: agent skills for using release-me, one folder each, shipped through skills.sh.

## Commands

```sh
task setup           # mise install
task check           # every check, one after another
task --list          # one target per check: lint:*, test:*, build
task run -- next     # run the CLI from source
```

- One task target per check and one CI job per target. `check` aggregates them and holds no
  command of its own. CI calls the small targets, never `check`.

## Constraints

- Standard library plus `go-github` for the forge and `go-git` only where the git CLI is not
  enough. Every other dependency needs a reason in the pull request.
- The core never imports a forge package. Forges implement `internal/forge.Forge`.
- Nothing in the core writes to the repository. Updaters return the new content, the command
  layer writes it.
- Tests: table driven, a fake forge, generated git repositories in a temporary directory, no
  network. Test names state the behaviour. Fixtures look like real data.
- Errors wrap with `%w` and name the component and the pull request where one is involved.
- Logging through `log/slog`, configured in `main`, quiet by default, `--verbose` for debug.
- Secrets never reach a log line or an error message. The token is read from the environment.
- License header first in every `.go` file, a blank line, then the package comment where one
  exists:

```text
// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0
```

## Deviations

- None.
