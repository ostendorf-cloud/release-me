# release-me

Release automation for repositories that merge with merge commits. One changelog entry per
pull request, versions from Conventional Commits, a release pull request, a tag and a release.

## What it does

- Reads the first parent history since the last tag. A merge commit is one entry, its pull
  request title is the text. A commit directly on the default branch is its own entry.
- Computes the next version from the entry types. Pre-releases through a label on the release
  pull request.
- Opens and updates one release pull request per component with the changelog, bumps the
  version in the files you list, and on merge creates the tag and the release.
- Works against GitHub and GitLab, self hosted included, through the same forge interface.

## Install

Not available yet. The first release is cut by hand, from then on the tool releases itself.

```sh
mise use ubi:ostendorf-cloud/release-me
```

## Use

```sh
release-me next        # next version, nothing written
release-me changelog   # entries for the next version, nothing written
release-me pr          # create or update the release pull requests
release-me release     # tag and release merged release pull requests
release-me run         # pr then release, the CI entry point
release-me check       # config, labels and token
```

Every writing command accepts `--dry-run`: it prints what would change and writes nothing, not
even a branch. `--verbose` enables debug logging on stderr. Components and the major tag land
in a later milestone; everything below works today, against a GitHub or GitLab repository with
one component.

`next` prints the next version and the bump reason, for example `0.2.0 (feat)`. `changelog`
prints the rendered entries for that version. Both print nothing and exit 0 when there is
nothing to release.

### pr

Creates or updates the release pull request: branch `release-me/main` off the repository's
default branch, with the changelog update and every configured file bumped to the next
version. The default branch is read from the forge; `git symbolic-ref refs/remotes/origin/HEAD`
is the fallback when that call fails. The branch itself is still forked from whatever commit is
checked out, which must be the default branch's tip, same as `next` and `changelog`. The branch
is written in a temporary checkout, never in your working copy.

`pr` checks that the `pending` and `tagged` labels exist before it writes anything, the same
check `check` runs.

Rerunning `pr` is judged on content, not on the rendered pull request body: the changelog
entries for the version, without the heading's date, and every configured file's bumped
content are compared against the release branch's current tip. Equal content changes nothing,
not even the pull request body, and nothing is pushed. A change pushes the branch and sets the
heading to today's date; the push carries a `--force-with-lease` whose expected value is the
existing pull request's head SHA from the forge, not a locally fetched ref, so nothing can
quietly defeat it. A fresh branch, with no existing pull request to lease against, pushes
without force. Editing only the notes block of an open pull request updates its body, with no
push: the content that would land on the branch did not change.

Updating an existing pull request never replaces its labels: a label added by hand stays, and
the `pending` label is added back if it is ever missing, never set by replacing the full list.

The pull request title is `chore(main): release vX.Y.Z`. Its body starts with
`Release vX.Y.Z`, then the notes block (below) when one is present, then the full changelog for
the version inside a collapsed `<details>` section. When every entry is a hidden type, the
changelog carries one explicit line, `- Version bump without user facing changes.`, instead of
an empty heading.

### release

Tags and releases every merged pull request on the release branch that still carries the
`pending` label: a tag `vX.Y.Z` on the merge commit, a release with the pull request's notes
and changelog, draft and prerelease both false, then the label swapped from `pending` to
`tagged`. A tag that already exists, left over from an earlier, interrupted run, is not an
error: the release and the label swap still happen, so the pull request does not stay pending
forever, and the run reports it as already tagged.

### run

`pr`, then `release`. The CI entry point: one command covers opening or updating the release
pull request and releasing whatever merged since the last run.

### check

Validates `release-me.yaml`, that the `pending` and `tagged` labels exist on the repository,
and that the token environment variable is both set and accepted by the forge: a rejected
token is reported as such, not as a missing label, since the symptom from the forge is the
same 401 either way. Prints which forge it resolved and why, then every missing or invalid
item, and exits 1 when anything is missing, 0 otherwise.

### Forge

Every command talks to the forge the `forge` config key names. Without that key, a GitLab
origin remote selects the GitLab forge, by its host naming `gitlab`; anything else selects
GitHub.

GitHub reads its token from `token-env`, default `GITHUB_TOKEN`. GitLab reads its token from
`token-env` too, but defaults to `GITLAB_TOKEN` instead when `token-env` is left unset, and
sends it in the `PRIVATE-TOKEN` header. GitLab's base URL comes from the `base-url` config key,
then the `CI_SERVER_URL` variable GitLab CI sets, then `https://gitlab.com`, for self hosted
instances.

A `ci/gitlab-ci.yaml` job template in this repository runs `release-me run` from the image on
GHCR, on the default branch only. Include it from the project's `.gitlab-ci.yml` and set a
`GITLAB_TOKEN` CI/CD variable, masked and protected.

### Notes block

A fenced block `release-me-notes` in the release pull request body is free text for the
release:

````markdown
```release-me-notes
Run the migration before upgrading.
```
````

Add it by hand once the pull request is open. `pr` keeps it verbatim on every later update, and
`release` carries it into the release, above the changelog.

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
- `token-env`: the environment variable holding the forge token, default `GITHUB_TOKEN`,
  `GITLAB_TOKEN` when the forge is GitLab and the key is left unset. It governs API calls only;
  `pr`'s push to the release branch authenticates through the git credentials the checkout's
  `origin` remote already carries, the same as any other push.
- `forge`: `github` or `gitlab`, default guessed from the origin remote's host. See
  [Forge](#forge).
- `base-url`: the forge's base URL, GitLab only, default the `CI_SERVER_URL` variable GitLab CI
  sets, then `https://gitlab.com`. Ignored for GitHub.
- `changelog`: the changelog file path, default `CHANGELOG.md`. `pr` prepends the new version's
  section to it, creating the file when it does not exist yet.
- `labels.pending` and `labels.tagged`: the label names `pr`, `release` and `check` use,
  default `release: pending` and `release: tagged`. `check` reports a missing label by name,
  it never creates one.
- `files`: files `pr` bumps to the next version, each a `path` and an optional `updater`,
  default `marker`. The `marker` updater replaces the version on a line ending in
  `# release-me: version` or `// release-me: version`, keeping the rest of the line, including
  the comment, as it is:

  ```go
  const Version = "0.1.0" // release-me: version
  ```

`major-tag` and `components` land with a later milestone. An unknown key is an error. The file
is read from the repository root, found through `git rev-parse --show-toplevel`, whatever
directory the command runs from.

## Development

```sh
task setup
task --list
task check
```

One target per check, one CI job per target. `check` only calls the small targets.

## License

Apache-2.0. See `LICENSE`.
