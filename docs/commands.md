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

`pr` checks that the `pending`, `tagged`, `alpha`, `beta` and `rc` labels exist before it writes
anything, the same check `check` runs. The labels are shared across every component; the
default branch is resolved once and shared too.

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
the configured labels for pre-releases and tagging, then the [notes block](overrides.md#notes-block)
when one is present, then the full changelog for the version inside a collapsed `<details>`
section. When every entry is a hidden type, the changelog carries one explicit line,
`- Version bump without user facing changes.`, instead of an empty heading. The guide never
reaches the changelog file or the GitHub release. A shared release pull request's title and
body name every component it releases: see
[shared release pull request](versioning-modes.md#shared-release-pull-request).

## Pre-releases

A pre-release type label on the open release pull request, `release: alpha`, `release: beta`
or `release: rc` by default, turns its next run into a pre-release: version `X.Y.Z-alpha.N`,
`X.Y.Z-beta.N` or `X.Y.Z-rc.N`. `X.Y.Z` is the version a stable release would get from the
entries since the last stable tag; pre-release tags never move that base. Exactly one of the
three labels may be present at a time; two at once is an error naming both. A type whose label
is set to `false`, or `""`, in `release-me.yaml` is turned off: release-me neither expects that
label on the repository nor reads it from the pull request, and the guide in the pull request
body leaves it out. A pull request that already carries the label of a type just turned off
keeps it as an ordinary label: the next `pr` run returns the version to the stable `X.Y.Z`,
and `release` leaves that label in place when it swaps `pending` for `tagged`. One name
configured for two labels is a configuration error.

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
`release-me/main`, that still carries the `pending` label: a tag on the merge commit, a GitHub
release with the pull request's notes and changelog, prerelease true for a pre-release version.
Draft follows `draft-until-assets-attach`, default `false`: off, the release publishes
immediately; on, it starts a draft, exempt from GitHub's immutable releases feature, until a
caller, such as a workflow step that still needs to attach binary assets, publishes it. The
label swap removes `pending`, and the matching pre-release type label when the version carries
one, replacing both with `tagged`. The implicit `main` component tags `vX.Y.Z` and names the
release after the tag, today's shape; a configured component tags `<name>/vX.Y.Z`, or its
`tag-prefix` followed by the version, and names the release `<name> vX.Y.Z`. A tag that already
exists, left over from an earlier, interrupted run, is not an error: the release and the label
swap still happen, so the pull request does not stay pending forever, and the run reports it as
already tagged.

With `draft-until-assets-attach: true`, `release` also looks at every merged pull request
already carrying the `tagged` label: one whose release is still a draft had its asset attach or
publish step fail after an earlier run tagged and drafted it, so this reprints the same
`released-tag` marker for it, without creating anything again, letting the workflow's asset
step retry. Off, the default, this never runs: a published release has nothing left to resume.

With `major-tag: true`, every stable release additionally creates or force-moves the major
tag, `vX` for the implicit `main` component or its tag prefix followed by `X` for a configured
one, onto the same commit. A tag that does not exist yet is created; one that does is moved,
never left behind. A pre-release, once that lands, never moves it. `--dry-run` reports whether
the major tag would be created or moved, writing nothing.

## run

`pr`, then `release`, for every component. The CI entry point: one command covers opening or
updating every release pull request and releasing whatever merged since the last run.

## check

Validates `release-me.yaml`, including an unknown `--component` name, that the `pending`,
`tagged`, `alpha`, `beta` and `rc` labels exist on the repository, and that the token
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
  component at the family's version, changed or not;
- `separate-pull-requests`, default `false` in release-please: more than one package without it,
  or with `false`, becomes `release-pull-request: shared`, one release pull request for every
  package as release-please opened; `true` keeps release-me's default of one per component.

`changelog-source` is set to `all`, because release-please reads every commit since the last
release, not the first parent history.

Every setting it cannot carry over is printed on stderr as a `warning:` line, each naming the
key and what to do instead, as are the differences a reader should know about. It then looks
up the tag each package's manifest version was released under and warns about a missing one,
since release-me would start that component over at its initial version. The translation is
loaded the way every other command loads `release-me.yaml` before it is printed. One
release-me refuses, such as a root package beside other packages, whose paths overlap, is
printed on stderr after the warnings and the reason, to correct by hand.
Exits 0 with warnings, 1 when the files cannot be read or translated or the translation does not
load.
