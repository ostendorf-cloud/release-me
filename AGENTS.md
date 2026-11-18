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
- `internal/workspace/`: package manifests, the local dependencies between components and
  their rewrite.
- `internal/releasepr/`: branch, body, labels and the notes block of a release pull request.
- `internal/release/`: tag, release and major tag.
- `internal/migrate/`: another release tool's configuration translated into `release-me.yaml`.
- `action/`: the composite action. `Dockerfile`: the image for runners with an image cache.
- `scripts/`: CI glue specific to this repository's own workflows, such as attaching and
  publishing release assets after `release.yaml` drafts a release, and `check-dco.sh`, a local
  copy of the check the shared DCO workflow runs from its own repository. The license checks
  live here too: `license-files.sh`, `check-license-spdx.sh` and its test.
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
- The config surface has one canonical key order: identity (components only), version rules,
  changelog, files, pull request, release commit, tags and releases, labels and comments,
  components, environment. It lives in the `Config` struct in `internal/config/config.go` and
  in `docs/configuration.md`'s headings. Every config struct, `Load` and `Resolve`'s literals,
  the doc headings and a new key follow it.
- License header first in every `.go` and `.sh` file, a blank line, then the package comment
  where one exists. Shell scripts carry it after the shebang, with `#` instead of `//`:

```text
// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0
```

Two checks enforce it, and neither replaces the other:

- `lint:license` runs `addlicense -check`. It fails a file that has no license header at all. It
  accepts any file that already holds a header, whatever the license, holder or year: a file
  with only `// SPDX-License-Identifier: Elastic-2.0` passes it. It skips generated Go files,
  the ones marked `// Code generated ... DO NOT EDIT.`.
- `lint:license-spdx` runs `scripts/check-license-spdx.sh`. It reads the license from the
  opening lines of `LICENSE` and compares it with the `SPDX-License-Identifier` line in the
  first 5 lines of every covered file. It fails a wrong or missing identifier. It also fails
  when `LICENSE` is swapped and the headers are not. It does not read the copyright holder or
  the year. It cannot catch a `LICENSE` that is itself wrong, or a license text edited below
  its opening lines.

The script recognises three `LICENSE` texts. Any other text exits 2.

- Apache-2.0: opens with `Apache License` and `Version 2.0, January 2004`.
- Elastic-2.0: opens with `Elastic License 2.0`.
- MIT: the script does not check the title. It checks two phrases and two exclusions.
  - The sentence `Permission is hereby granted, free of charge, to any person obtaining a
    copy` must be in the first 10 lines. Letter case matters here, line wrapping does not.
  - The clause `The above copyright notice and this permission notice shall be included` must
    be somewhere in the file. Letter case and line wrapping do not matter.
  - A first line of `MIT No Attribution` (MIT-0) is refused, and so is a `Good, not Evil`
    clause (the JSON license).

ISC and texts that lack either phrase exit 2. A look-alike that carries both phrases is read
as MIT. The X11 license and a proprietary text that quotes both phrases are such cases. The
check is a guard against a wrong header, not a license classifier.

A new license needs a new row in the script and a test case.

Both checks read the covered files from `scripts/license-files.sh`. It lists every regular `.go`
and `.sh` file, except copies under `.worktrees/` and `.claude/worktrees/`. Nothing else is
excluded. A symlink to a source file is covered by neither check.

- Markdown, YAML, JSON, TOML, the `Dockerfile`, `go.mod`, `go.sum`, `LICENSE` and `VERSION`
  carry no header. That is a decision, not an oversight.
- A new file type that should carry a header is added in `scripts/license-files.sh` only. The
  CI `license` job calls that script, so the file set cannot drift between the checks.
- No file is generated today. The day one arrives, `lint:license` skips it and
  `lint:license-spdx` fails it. Then either give the generator the header line, or exclude
  the file in `scripts/license-files.sh` with a comment that says why.
- `scripts/check-license-spdx_test.sh` runs both directions on scratch copies. A wrong header
  fails. A swapped `LICENSE` fails.

## Git

- Update a branch with a rebase, never a merge: a rebase replays its commits onto the new
  base, it never adds a merge commit of its own. Merging main, or another branch, into a
  feature branch litters its history with "Merge branch" commits that carry into the project's
  permanent history the moment the pull request merges.
- A merge commit exists only where a feature branch lands in the default branch, created by
  merging the pull request itself.

## Deviations

- The `license` job in `.github/workflows/checks.yaml` calls
  `ostendorf-cloud/workflows/.github/workflows/reusable-license.yaml` instead of
  `task lint:license`, the one CI job that does not call its matching task target. The check is
  shared infrastructure now, maintained in one place for every repository in the organisation.
  `task lint:license` stays for local development and runs the same tool with the same flags
  over the same files, listed by `scripts/license-files.sh`.

- `scripts/check-dco.sh` and its test stay in this repository although
  `.github/workflows/dco.yaml` only calls the shared `reusable-dco.yaml`. That reusable workflow
  runs its own copy of the script from its own repository and reads no file of this one. The
  copy here serves local runs and as a parity reference, and is byte-identical to the shared
  one today. `scripts/check-dco_test.sh` checks only the behaviour of this copy, so a drift
  shows only in a comparison of the two files, for example of their `git hash-object` values.
