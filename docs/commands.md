# Commands

Full reference for every `release-me` command beyond the one-line descriptions in the
[README](../README.md#use).

## pr

Creates or updates one release pull request per component: branch `release-me/<name>` off the
repository's default branch, with that component's changelog update and its configured files
bumped to its next version. The default branch is read from the forge;
`git symbolic-ref refs/remotes/origin/HEAD` is the fallback when that call fails. The branch
itself is still forked from whatever commit is checked out, which must be the default branch's
tip, same as `next` and `changelog`. The branch is written in a temporary checkout, never in
your working copy. `versioning: locked` and `release-pull-request: shared` open one release pull
request for every component instead, on branch `release-me/main`: see
[shared release pull request](versioning-modes.md#shared-release-pull-request).

`pr` checks that every label `release-me.yaml` configures exists before it writes anything, the
same check `check` runs: the `pending` and `tagged` labels, every
[`labels.extra`](configuration.md#labels) one, and the label of every enabled pre-release type,
`release: alpha`, `release: beta` and `release: rc` by default, a type turned off expecting
none. The labels are shared across every component; the default branch is resolved once and
shared too.

The changelog heading, the same one `changelog` prints, links the version to the forge's compare
view, between the tag the release's entries are collected since and the tag it will get:

```markdown
## [v0.9.0](https://github.com/owner/repo/compare/duxt@v0.8.1...duxt@v0.9.0) (2026-10-01)
```

The link starts at the component's last stable tag, with its own tag prefix; a pre-release tag
never moves that base. A pre-release starts at the previous pre-release tag of the same
version, `v0.9.0-rc.1...v0.9.0-rc.2`, when one exists, since its entries start there too, and at
the last stable tag otherwise. A component's first release has nothing to compare against, so
its heading carries no link: `## v0.1.0 (2026-10-01)`. On GitHub Enterprise the link points at
the instance's own web host. A locked family's combined pull request body keeps one unlinked
heading, as every component has a tag of its own; each component's `CHANGELOG.md` links its own.
A shared release pull request body has no one version for a combined heading, so each
component's section carries its own linked heading, the one its `CHANGELOG.md` gets. The heading
in the GitHub release body is the one from the pull request body. Older headings
without a link are read the same way as linked ones. The link's head tag does not exist until the
release pull request merges, so while it is open the link in its body and on its branch leads to
a 404; the merge creates the tag and the link resolves from then on. A `tag-prefix` holding `(`,
`)`, `#` or `%` is a configuration error, since each one breaks the link or the heading around
it.

Rerunning `pr` is judged on content, not on the rendered pull request body: the version and the
compare link in the changelog heading, the changelog entries for that version without the
heading's date, and every configured file's bumped content are compared against the release
branch's current tip. A version that differs from the one already on the branch, a pre-release
label added, changed or removed, counts as changed even when the entries underneath read the
same, and so does a compare link that differs, a heading written before the link existed
included. A heading the comparison cannot
read, a hand edit, release-please's `## [0.9.0](url) (date)` or one from before this tool wrote
it, counts as changed too, and pushes; the push rewrites it to this tool's own shape, so it
settles after that one push. Should the heading this tool would write not read back either, the
heading is left out of the comparison instead, so `pr` never pushes the same content on every
run. Equal content
changes nothing, not even the pull request body, and nothing is pushed. A change pushes the
branch, publishing an API-created commit (see [signed release commits](#signed-release-commits)), and
sets the heading to today's date. The new commit is always built on the default branch's tip,
and replaces the existing branch only while it is still at the existing pull request's head
SHA from the forge, the lease `--force-with-lease` takes; a branch found anywhere else is left
alone and `pr` fails, naming the branch and the pull request. The commit object is fetched
from the forge and published through Git with an atomic `--force-with-lease` comparison. A fresh
branch, with no existing pull request to lease against, is created, or moved only as a
fast-forward, the rule a push without force follows.
Editing only the notes block of an open pull request updates its body, with no push: the content
that would land on the branch did not change.

Updating an existing pull request never replaces its labels: a label added by hand stays, and
the `pending` label is added back if it is ever missing, never set by replacing the full list.

The pull request title is `chore(<name>): release vX.Y.Z`, `chore(main): release vX.Y.Z` for
the implicit `main` component, or what
[`release-pull-request-title`](configuration.md#release-pull-request-title) renders. Its body
starts with `Release vX.Y.Z`, then a collapsed
`<details><summary>How this release works</summary>` guide naming the tag merging creates and
the configured labels for pre-releases and tagging, left out with
[`release-pull-request-guide: false`](configuration.md#release-pull-request-guide), then the
version override block and the [notes block](overrides.md#notes-block) when present, then the
full changelog for the version inside a collapsed `<details>` section. When every entry is a
hidden type, the changelog carries one explicit line,
`- Version bump without user facing changes.`, instead of an empty heading. With
[`changelog-type: github`](configuration.md#changelog-type), the section under the heading is
the release notes GitHub generates instead, the same in the changelog file, the body and the
release. The guide never reaches the changelog file or the GitHub release. A shared release pull
request's title and body name every component it releases: see
[shared release pull request](versioning-modes.md#shared-release-pull-request).

### Signed release commits

`pr` creates no local commit. It creates the release branch's commit through the
forge's API, on GitHub the Git Data API: a blob per changed file, a tree on top of the default
branch's tip and a commit with that one parent. `pr` then fetches that commit and pushes it to
the branch.

GitHub signs a commit it creates this way only for a GitHub App installation token, a token
from `actions/create-github-app-token` or `secrets.GITHUB_TOKEN`. Then the release pull request
satisfies a ruleset or branch protection that requires signed commits, which a local commit
made with a token that has no signing key never does. A personal access token, classic or
fine-grained, or an OAuth token such as the one `gh auth login` stores, yields an unsigned
commit, and a ruleset requiring signed commits blocks the release pull request from merging.
After every commit `pr` reads back the signature GitHub verified and, when there is none, logs
a warning naming the branch; the run itself still succeeds.

GitHub signs that commit only when the request names no author and no committer, so none is
passed: the commit is attributed to the token's own identity, the app's `<slug>[bot]` account
for a GitHub App token, the user for a personal access token, `github-actions[bot]` for
`secrets.GITHUB_TOKEN`. The `author` configuration key, the `author-name`, `author-email` and
`app-slug` action inputs and the `GIT_AUTHOR_*` and `GIT_COMMITTER_*` environment variables no
longer change the release commit. They are still accepted, so an existing configuration or
workflow keeps working unchanged; `check` and `pr` warn about an `author` key that is set.

The commit's parent must already exist on GitHub: `pr` builds on the checkout's `HEAD`, the
default branch's tip in a workflow triggered by a push to it. A local run on top of unpushed
commits fails, naming the branch and the parent it could not read.

## Pre-releases

A pre-release type label on the open release pull request, `release: alpha`, `release: beta` or
`release: rc` by default, turns its next run into a pre-release: version `X.Y.Z-alpha.N`,
`X.Y.Z-beta.N` or `X.Y.Z-rc.N`. `X.Y.Z` is the version a stable release would get from the
entries since the last stable tag; pre-release tags never move that base. A
[`labels.prerelease`](configuration.md#labels) entry adds a type of any other name,
`X.Y.Z-next.N` for `next`, selected by its own label, and behaving exactly like the three.
Exactly one pre-release type label may be present at a time; two at once is an error naming
both. A type whose label is set to `false`, or `""`, in `release-me.yaml` is turned off:
release-me neither expects that label on the repository nor reads it from the pull request, and
the guide in the pull request body leaves it out. A pull request that already carries the label
of a type just turned off keeps it as an ordinary label: the next `pr` run returns the version
to the stable `X.Y.Z`, and `release` leaves that label in place when it swaps `pending` for
`tagged`. One name configured for two labels is a configuration error.

`N` starts at `1` for a type and counts up from the highest `N` an earlier pre-release tag of
that same type and base version already carries. Switching the label to a different type does
not inherit the previous type's counter: it starts at `1` again, even when tags for the other
type already exist.

The changelog a pre-release renders depends on what came before it: the first pre-release since
the last stable tag collects the same entries a stable release would, since that stable tag; a
pre-release that follows another one collects only the entries since that previous pre-release
tag. Removing the label from the open pull request returns it to the stable version on the next
run, branch content, title and body updated accordingly.

## release

Tags and releases every merged pull request on a component's release branch, or the shared
`release-me/main`, that still carries the `pending` label: a tag on the merge commit, a
GitHub release with the pull request's notes and changelog, prerelease true for a
pre-release version. Draft follows `github-release-draft-until-assets-attach`, default `false`:
off, the release publishes immediately; on, it starts a draft, exempt from GitHub's immutable
releases feature, until a caller, such as a workflow step that still needs to attach
binary assets, publishes it. The label swap removes `pending`, and the matching
pre-release type label when the version carries one, replacing both with `tagged`. The
implicit `main` component tags `vX.Y.Z` and names the release after the tag, today's
shape; a configured component tags `<name>/vX.Y.Z`, or its `tag-prefix` followed by the
version, and names the release `<name> vX.Y.Z`, both without the `v` under
[`include-v-in-release-name: false`](configuration.md#include-v-in-release-name). A tag
that already exists, left over from an earlier, interrupted run, is not an error: the
release and the label swap still happen, so the pull request does not stay pending
forever, and the run reports it as already tagged.

With `github-release-draft-until-assets-attach: true`, `release` also looks at every merged pull request
already carrying the `tagged` label: one whose release is still a draft had its asset attach or
publish step fail after an earlier run tagged and drafted it, so this reprints the same
`released-tag` marker for it, without creating anything again, letting the workflow's asset
step retry. Off, the default, this never runs: a published release has nothing left to resume.

With `force-tag-creation: false`, `release` creates no tag itself: the release names the merge
commit as its target and GitHub tags it when the release is published, so a draft reports
`drafted <tag>, tagged on publish` and has no tag until then. See
[configuration](configuration.md#force-tag-creation).

With `skip-github-release: true`, `release` tags the merge commit, swaps the label and prints
the `released-tag` marker as usual, but creates no GitHub release, for a workflow that creates
it from the tag itself. See [configuration](configuration.md#skip-github-release).

With `github-release-draft: true`, the release starts a draft too, reported as
`tagged and drafted <tag>`, but it is left for a human to publish: a later run never resumes it,
whatever `github-release-draft-until-assets-attach` is set to. See
[configuration](configuration.md#github-release-draft).

With `major-tag: true`, every stable release additionally creates or force-moves the major
tag, `vX` for the implicit `main` component or its tag prefix followed by `X` for a configured
one, onto the same commit. A tag that does not exist yet is created; one that does is moved,
never left behind. A pre-release, once that lands, never moves it. A drafted release moves it
when the draft is created, not when it is published; see
[configuration](configuration.md#major-tag) for what that means with
`force-tag-creation: false`. `--dry-run` reports whether the major tag would be created or
moved, writing nothing.

With `delete-release-branch`, default `true`, `release` then deletes the release branch,
printing `deleted release branch <branch>`. Only once every tag and release of the pull
requests merged from it succeeded, for every component of a shared or locked pull request, and
only while the branch still points at the head the pull request merged with and no open pull
request uses it: a branch `pr` already pushed the next release onto stays. A branch already
gone, deleted by GitHub's own setting to delete head branches on merge, is not an error.
Deletion uses `git push --force-with-lease` against the released head SHA, so a writer
arriving after the lookup keeps its branch and is reported as a warning. A
failed delete, a protected branch say, is a warning and never fails the run, since the tags and
releases exist by then; the branch stays for a human. `pr` pushes the branch afresh for the
next release. Deleting needs `contents: write`, the permission tagging already needs.
`--dry-run` prints `would delete release branch <branch>` and deletes nothing.

Deleting the branch keeps a draft resumable with `github-release-draft-until-assets-attach: true`: `release`
finds the `tagged` pull request to resume by its head branch, and GitHub still lists a merged
pull request by a head branch that has since been deleted. Only the leftover sweep of a shared
release pull request looks at a branch's existence first, so there a swept branch stays; see
[versioning modes](versioning-modes.md#shared-release-pull-request).

## run

`pr`, then `release`, for every component. The CI entry point: one command covers opening or
updating every release pull request and releasing whatever merged since the last run.

## check

Validates `release-me.yaml`, including an unknown `--component` name, that every configured
label exists on the repository, the `pending` and `tagged` ones, every
[`labels.extra`](configuration.md#labels) one and the label of every enabled pre-release type,
and that the token
environment variable is both set and accepted by the forge: a rejected token is reported as
such, not as a missing label, since the symptom from the forge is the same 401 either way.
Prints every missing or invalid item and exits 1 when anything is missing, 0 otherwise.

## comment

`comment` applies one GitHub comment to the release pull request it was posted on: see
[comment commands](overrides.md#comment-commands). It is invoked by
`.github/workflows/release.yaml`'s own `comment` job, through flags carrying that one comment's
pull request number, author association and body, not something a human runs directly.

## migrate

`migrate release-please` reads `release-please-config.json` and `.release-please-manifest.json`
from the repository root and prints the equivalent `release-me.yaml` on stdout, so it can be
reviewed before it is written: `release-me migrate release-please > release-me.yaml`. It writes
nothing itself.

Each release-please package becomes a component named after its `component`, else its
`package-name` with a node scope removed, else the name in its `package.json`. A character a
component name cannot hold becomes `-`, with a warning, and two packages ending up with the same
name are an error. Its tags and
changelog stay where release-please put them, through `tag-prefix` and `changelog-path`, so
release-me continues from the existing tags and changelogs. A single root package with plain
`vX.Y.Z` tags becomes the implicit `main` component instead, and a root package beside other
packages becomes a [root component](configuration.md#root-component), with a warning: it owns
only the files outside the other components' paths, where release-please's root package
counted every commit. It also carries over:

- `bump-minor-pre-major`, `bump-patch-for-minor-pre-major` and `initial-version`;
- `versioning` as `versioning-strategy`, by name, for `always-bump-patch`, `always-bump-minor`,
  `always-bump-major`, `service-pack` and `dependency-manifest`, the last two warned about since
  their release-me form differs from release-please's Java one;
- `force-tag-creation`, at the top level when every package carries the same value, on the
  component otherwise. When absent, release-me's default `true` is kept, rather than
  release-please's `false`; without drafts both create the tag during the release run;
- `include-v-in-release-name`, at the top level when every package carries the same value, on
  the component otherwise;
- `skip-github-release`, at the top level when every package carries the same value, on the
  component otherwise; release-me still creates the tag, unlike release-please, so remove
  any external tagging step the old workflow needed;
- `github-release-draft`, at the top level when every package carries the same value, on the
  component otherwise, read from release-please's `draft`; the GitHub release is left a draft
  for a human to publish, release-me never resumes it the way
  `github-release-draft-until-assets-attach` does;
- `exclude-paths`, per package or as a top-level default, keeping each path that lies inside
  the package it is set on and warning about every other one, which excludes nothing there;
- `release-as`, unless the manifest already records it as released or release-me refuses the
  value, a pre-release for one;
- the version files of the package's `release-type`, `node` being release-please's default,
  each where it exists. A file release-please bumps only where it holds a version is left out
  silently otherwise; a missing main version file is warned about:
  - `node`: `package.json`; `expo`: the same plus `app.json` through `expo`, keyed by the Expo
    SDK version its `package.json` declares the `expo` dependency as, with a warning falling
    back to a major of 0 where it declares none, as release-please did;
  - `rust`: `Cargo.toml`; `maven`: `pom.xml`, its project version or else its parent's, a
    multi-module `pom.xml` warned about, since release-please bumped every `pom.xml` below the
    package; `simple`: its `version-file`, default `version.txt`, through `plain`;
  - `python`: `setup.cfg`, `setup.py`, `pyproject.toml`, and the `__init__.py` and
    `version.py` of the project `pyproject.toml` names, at the package root or under `src/`;
  - `ruby`: its `version-file`, default `lib/<name>/version.rb`, a `-` in the name nesting a
    directory; `elixir`: `mix.exs`; `r`: `DESCRIPTION`, the changelog defaulting to `NEWS.md`;
  - `ocaml`: the `.opam` and `.opam.locked` file named after the package, `dune-project` and
    `esy.json`, with a warning that any other `.opam` file is not searched for;
  - `bazel`: its `version-file`, default `MODULE.bazel`; `php`: `composer.json`, with a
    warning for a `VERSION` file;
  - `dart`: `pubspec.yaml`; `sfdx`: `sfdx-project.json`; `helm`: `Chart.yaml`;
  - `terraform-module`: `versions.tf`, `versions.tf.tmpl` and `metadata.yaml`, with a warning
    that release-please also searched the package for every submodule's `versions.tf` and
    rewrote every README's version constraint;
  - `krm-blueprint` searched the package's YAML files for the `cnrm.cloud.google.com/blueprint`
    annotation, which a warning asks to list under `files` with the `krm-blueprint` updater;
- the lockfile release-please bumps beside it where it exists and holds the package:
  `package-lock.json` and `npm-shrinkwrap.json` for `node`, `Cargo.lock` for `rust`, and
  `Gemfile.lock` for `ruby`, keyed by the crate or gem name;
- every `extra-files` entry that is a plain path or of type `generic` as a
  [`generic`](configuration.md#files) entry, which reads the same `x-release-please-version`
  markers, so the file needs no change. A plain `.json`, `.yaml`, `.yml` or `.toml` path also
  gets its top-level `version` bumped, as release-please does. Each is carried over only where
  the file exists and holds what it bumps; a `glob` entry is warned about, since release-me
  lists its files;
- every `extra-files` entry of type `json`, `yaml` or `toml` with a plain `$.a.b` jsonpath, of
  type `xml` with a plain absolute xpath such as `/project/version`, and of type `pom`;
- `label`, `release-label` and `extra-label` as the `pending`, `tagged` and `extra` labels,
  every label of each comma separated list;
- `prerelease-type` as a pre-release type selected by the label `release: <type>`: `alpha`,
  `beta` or `rc` stays on, any other name becomes a
  [`labels.prerelease`](configuration.md#labels) entry, and every type no package names
  is turned off. release-me cuts the pre-release while the release pull request carries that
  label, not on every release, so this is warned about, as is `prerelease: true`, which has no
  always-on counterpart;
- a `linked-versions` plugin over every component as `versioning: locked`: both release every
  component at the family's version, changed or not;
- a `node-workspace`, `cargo-workspace` or `maven-workspace` plugin as
  [`workspaces`](configuration.md#workspaces) `node`, `cargo` or `maven`, warning
  about each plugin option, and turning `separate-pull-requests: true` into a shared release
  pull request with a warning, since a dependent releases together with its dependency;
- `always-link-local: false` beside a `node-workspace` plugin as
  [`always-link-local`](configuration.md#always-link-local) `false`;
- `separate-pull-requests`, default `false` in release-please: more than one package without it,
  or with `false`, becomes `release-pull-request: shared`, one release pull request for every
  package as release-please opened; `true` keeps release-me's default of one per component;
- `pull-request-title-pattern` as
  [`release-pull-request-title`](configuration.md#release-pull-request-title) and
  `group-pull-request-title-pattern` as
  [`release-pull-request-shared-title`](configuration.md#release-pull-request-shared-title),
  release-please's `${scope}` becoming `(${branch})` and its `${component}` the name after a
  space. A pattern lacking a placeholder release-me reads the title back with, such
  as release-please's default group pattern without any version, is left out with a warning.
- `draft-pull-request` as
  [`release-pull-request-draft`](configuration.md#release-pull-request-draft).
- `signoff` as [`signoff`](configuration.md#signoff).
- `pull-request-header` and `pull-request-footer` as
  [`release-pull-request-header`](configuration.md#release-pull-request-header) and
  [`release-pull-request-footer`](configuration.md#release-pull-request-footer).

`changelog-source` is set to `all`, because release-please reads every commit since the last
release, not the first parent history.

Every setting it cannot carry over is printed on stderr as a `warning:` line, each naming the
key and what to do instead, as are the differences a reader should know about. It then looks
up the tag each package's manifest version was released under and warns about a missing one,
since release-me would start that component over at its initial version. The translation is
loaded the way every other command loads `release-me.yaml` before it is printed. One
release-me refuses, such as a package nested inside another below the root, whose paths
overlap, is
printed on stderr after the warnings and the reason, to correct by hand.
Exits 0 with warnings, 1 when the files cannot be read or translated or the translation does not
load.
