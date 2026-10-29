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
token can. The release commit is created through the GitHub API with that token, so with a
GitHub App token it is the app's own bot account that authors it, and GitHub signs it:

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
```

The action downloads the archive and checksums file matching its own version for the
runner's os and arch, verifies the checksum, then runs `release-me run`. Inputs: `token`
(required), `component`, `dry-run`, `verbose`, `download-token` for the download step on a
private fork, defaulting to the calling workflow's own token, `version` to pin a different
release than the one the action resolves on its own, or `source` to build from the checkout
with `go build` instead. `author-name`, `author-email` and `app-slug` are still accepted but
no longer change the release commit, see [Signed release commits](#signed-release-commits).
`source` only builds this repository's own code:
it is `go build`'s working directory for `uses: ./action` from inside this repository, not the
caller's checkout for `uses: ostendorf-cloud/release-me/action@ref`.

Without `version`, the action resolves its own release in order: the ref it was invoked at
(`github.action_ref`), when that ref is itself a release tag; failing that, the `VERSION`
file shipped at the pinned commit, which this repository's own release process keeps at the
version released from that exact commit; failing both, an error naming all the ways out.
Pinned by anything other than a release tag, a commit digest or a floating branch alike,
`github.action_ref` never matches a release tag, so the `VERSION` file is what actually
resolves it; pinned by a release tag instead, the ref itself already is the tag, and the
`VERSION` file is never consulted.

Outputs, every one from `release-me` itself: `tags` from the `released-tag: <tag>` line it
prints per release, the rest written by `pr`, `release` and `run` to the file the
`RELEASE_ME_GITHUB_OUTPUT` environment variable names, which the action points at the step's
own `GITHUB_OUTPUT`. A `version` older than these outputs fills `tags` alone. They reflect what
happened even when the step fails, for a later step with `if: always()`: a `run` whose release
phase fails still lists in `prs` the pull request its `pr` phase opened.

- `tags`: the newline-separated tags this run tagged a release for, one per released
  component, empty when nothing was released or `dry-run` was set.
- `released`: `true` when this run tagged a release for any component, `false` otherwise and
  always in dry-run.
- `components`: a JSON object keyed by every component this run handled, every configured
  one unless `component` limits it, the implicit one as `main`. Each entry is
  `{"released", "version", "tag", "pr"}`: `version` (`0.9.0`, no `v`) and `tag`
  (`duxt@v0.9.0`) are empty unless `released`, and `pr` is the release pull request opened,
  updated or found up to date for that component this run, shaped as in `prs`, else `null`.
- `pr`: the one release pull request this run opened, updated or found up to date, as JSON,
  `{"number", "url", "branch", "components"}`; `null` when there was none or several, which
  only happens with one release pull request per component.
- `prs`: a JSON array of every such pull request, `[]` when none. A locked family or a
  [shared release pull request](#shared-release-pull-request) is one entry naming several
  components. Dry-run opens nothing, so it reports none.

```yaml
  - uses: ostendorf-cloud/release-me/action@<commit sha>  # v0.1.1
    id: release
    with:
      token: ${{ steps.app-token.outputs.token }}
  - if: fromJSON(steps.release.outputs.components).duxt.released
    run: echo "released duxt ${{ fromJSON(steps.release.outputs.components).duxt.version }}"
  - if: fromJSON(steps.release.outputs.pr) != null
    run: echo "release pull request ${{ fromJSON(steps.release.outputs.pr).url }}"
```

A component name with a `-` in it needs the index form,
`fromJSON(steps.release.outputs.components)['duxt-ui'].released`.

`draft-until-assets-attach` in `release-me.yaml` decides whether each tagged release
publishes immediately or starts as a draft, default `false`, no behavior change from before
this key existed. See [Configuration](#configuration) for what each setting needs from the
calling workflow:

- `false`, the default: the release publishes the moment it is tagged, same as every
  release-me release before this key existed. A caller that attaches binary assets waits on
  the `release: published` webhook, for example `publish.yaml`'s `image`-shaped job running
  `goreleaser release --clean` right after.
- `true`: only a repository with GitHub's immutable releases setting on needs this, since that
  setting rejects a change to an already published release, assets included, and release-me
  cannot detect it itself through the API. The release starts as a draft, exempt from
  immutability, and a draft fires no `release` webhook at all, so the same job that tagged and
  drafted it must also build, attach and publish it itself, for example with
  `gh release upload` followed by `gh release edit --draft=false`; see this repository's own
  `release.yaml` and `scripts/publish-release-assets.sh` for the full shape, the one this
  repository uses.

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
locally). Every later release carries the same build automatically, through whichever of the
two workflow shapes above the repository uses: `publish.yaml`'s `release: published`-triggered
job with `draft-until-assets-attach` left off, or `release.yaml`'s own build-attach-publish
step with it turned on, the shape this repository's own `release-me.yaml` configures.

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
or `2.0.0 (Release-As footer)` for a version a [Release-As footer](#release-as-footer) forces.
`changelog` prints the rendered entries for that version. Both print nothing for a component
with nothing to release.

### pr

Creates or updates one release pull request per component: branch `release-me/<name>` off the
repository's default branch, with that component's changelog update and its configured files
bumped to its next version. The default branch is read from the forge;
`git symbolic-ref refs/remotes/origin/HEAD` is the fallback when that call fails. The branch
itself is still forked from whatever commit is checked out, which must be the default branch's
tip, same as `next` and `changelog`. The branch is written in a temporary checkout, never in
your working copy. `versioning: locked` and `release-pull-request: shared` open one release pull
request for every component instead, on branch `release-me/main`: see
[Shared release pull request](#shared-release-pull-request).

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
branch, through the forge's API (see [Signed release commits](#signed-release-commits)), and
sets the heading to today's date. The new commit is always built on the default branch's tip,
and replaces the existing branch only while it is still at the existing pull request's head
SHA from the forge, the lease `--force-with-lease` takes; a branch found anywhere else is left
alone and `pr` fails, naming the branch and the pull request. GitHub's ref update takes no
expected value, so that check is a read of the branch right before the forced update. A fresh
branch, with no existing pull request to lease against, is created, or moved only as a
fast-forward, the rule a push without force follows.
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
reaches the changelog file or the GitHub release. A shared release pull request's title and
body name every component it releases: see
[Shared release pull request](#shared-release-pull-request).

### Signed release commits

`pr` never commits locally and pushes. It creates the release branch's commit through the
forge's API, on GitHub the Git Data API: a blob per changed file, a tree on top of the default
branch's tip, a commit with that one parent, then the branch created or moved onto it. GitHub
signs a commit it creates for an app or a user token, so the release pull request satisfies a
ruleset or branch protection that requires signed commits, which a local commit made with a
token that has no signing key never does.

GitHub signs that commit only when the request names no author and no committer, so none is
passed: the commit is attributed to the token's own identity, the app's `<slug>[bot]` account
for a GitHub App token, the user for a personal access token, `github-actions[bot]` for
`secrets.GITHUB_TOKEN`. The `author` configuration key, the `author-name`, `author-email` and
`app-slug` action inputs and the `GIT_AUTHOR_*` and `GIT_COMMITTER_*` environment variables no
longer change the release commit. They are still accepted, so an existing configuration or
workflow keeps working unchanged.

The commit's parent must already exist on GitHub: `pr` builds on the checkout's `HEAD`, the
default branch's tip in a workflow triggered by a push to it. A local run on top of unpushed
commits fails, naming the branch and the parent it could not read.

### Pre-releases

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

### release

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

### run

`pr`, then `release`, for every component. The CI entry point: one command covers opening or
updating every release pull request and releasing whatever merged since the last run.

### check

Validates `release-me.yaml`, including an unknown `--component` name, that the `pending`,
`tagged`, `alpha`, `beta` and `rc` labels exist on the repository, and that the token
environment variable is both set and accepted by the forge: a rejected token is reported as
such, not as a missing label, since the symptom from the forge is the same 401 either way.
Prints every missing or invalid item and exits 1 when anything is missing, 0 otherwise.

### comment

`comment` applies one GitHub comment to the release pull request it was posted on: see
[Comment commands](#comment-commands). It is invoked by `.github/workflows/release.yaml`'s own
`comment` job, through flags carrying that one comment's pull request number, author
association and body, not something a human runs directly.

### migrate

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

### Comment commands

A comment on the release pull request, prefixed with the configured `comment-mention`,
`@release-me` by default, lets a commenter force the next version or set the release notes
without editing the pull request body by hand:

```text
@release-me release-as 1.4.0
@release-me release-notes Run the migration before upgrading.
```

A command only ever runs from a line that opens with the mention: one as the whole comment, or
as the first thing on its own line inside a longer one. A mention quoted with a leading `>`,
fenced, inside inline code, or inside an HTML comment is inert text, never a command, so a
maintainer can always quote a refused command back to say no to it without that quote
executing.

Only a commenter whose GitHub `author_association` with the repository is `OWNER`, `MEMBER` or
`COLLABORATOR` may issue either command; anyone else's comment naming the mention is refused,
never acted on. This is close to, but not exactly, write access: `MEMBER` is any member of the
owning organization regardless of that member's own permission on this repository, and
`COLLABORATOR` includes one added with only read or triage access. Checking the repository's
actual collaborator permission would need an extra API call per comment; `release-me` accepts
the wider set these three values describe instead, the same trade-off avoiding an extra
network round trip.

`release-as` takes a plain `X.Y.Z` version, optionally prefixed with `v`, and rejects one that
is not strictly greater than the last tag already released, so it can move a burned or wrong
version forward but never backward. On a shared release pull request with
`versioning: independent`, every component has a version of its own, so the command names the
component first, `release-as api 2.0.0`, checked against that component's last tag; with no
component named it applies to the one component the pull request releases and is refused as
ambiguous when it releases several. A component the pull request does not release is refused
too. Which components it releases is read from the body's `#### <name>` headings, the comment
command's source of truth: a heading added or deleted by hand changes what the command accepts,
and the recompute that follows still applies an override only to a component the pull request
actually releases. Everywhere else there is one version to force, and naming a component is refused.
`release-notes` takes the rest of the comment, verbatim, as the new content of the
[notes block](#notes-block), shared by every component of a shared release pull request; a
fence terminator inside either argument is escaped so it can never plant a second, different
block.

The workflow always replies on the pull request stating what it did, or why it refused: the
commenter is not authorized, the pull request the comment was posted on is not the release
one, the command word after the mention was not recognized, or the `release-as` argument did
not parse or was not greater than the last released version. A comment that never names the
mention at all is left alone entirely, not replied to. No reply ever repeats the mention
itself, and the workflow job's own guard ignores a comment from a bot account, so the bot's
own reply can never start another run of itself.

`release-as` writes a fenced block `release-me-version`, read back the same way the notes
block is, and read before the normal computed bump, pre-release label included: present,
parseable and greater than the last released version, it replaces the version for that run
outright, changelog and all, until the pull request merges or the block is removed. It
survives a later push the same way the notes block does:

````markdown
```release-me-version
1.4.0
```
````

The same workflow run also re-renders the pull request's title and changelog from the override,
through the same logic a push to the default branch would run, so there is no window in which
the pull request's title and changelog still show a stale version: merging right after the
command releases the version it just forced, not an old one. The reply confirms the update. A
version that is not greater than the last released version is still ignored during that
re-render, exactly as it always was; the reply names the refusal instead, and the title and
changelog stay untouched.

Both commands work today through this repository's own `release.yaml` `comment` job; the
composite action does not yet wire the `issue_comment` trigger for other repositories using
it.

### Release-As footer

A `Release-As: X.Y.Z` footer in a commit message forces the next version the same way
`release-as` does, but from the history itself: it needs no comment and stays in git log. The
key is case-insensitive, as in release-please, and the version takes an optional `v`:

```text
chore: release 2.0.0

Release-As: 2.0.0
```

The footer counts on any commit release-me reads for a component since that component's last
tag: a direct commit, a squash merge's commit body, a merged pull request's body, and in
`all` or `nested` mode every commit on the branch. A pull request with an [override
block](#override-block) contributes only its block's lines, so a footer on it is not read.
The footer forces the components the commit counts for, the same attribution its changelog
entry gets: those whose `path` it touches and those a
[Release-Component footer](#release-component-footer) names, or every component for an empty
commit. An empty commit carrying both footers forces only the named components.

The footer is read only from the message's trailer block, the way `git interpret-trailers`
finds it: the last paragraph below the first line, and only when every line in it is a
`Key: value` trailer, `Co-authored-by:` and `Signed-off-by:` included. A `Release-As:` line
anywhere else is inert text: in an earlier paragraph, fenced, quoted with `>`, in a list item,
inside an HTML comment, or in a last paragraph that also holds prose. A pull request body that
documents the footer with a fenced example therefore never forces a version.

A forced version wins outright over the computed bump and the initial version, and releases
even when no commit bumps the version, so an empty commit carrying the footer is enough. With
more than one footer in range, the highest version wins, whatever order the commits came in;
inside one message, the last footer counts. In `locked` and `linked`, a footer on any
component forces the family's shared version: `locked` releases every component at it,
`linked` every changed component, the one carrying the footer counted as changed even when
nothing else in it bumps.

The same rule as `release-as` applies: a value that does not parse, or is not strictly greater
than the last tag, is ignored with a warning in the log, never failing the run, and the
computed bump applies. A pre-release value is ignored too: the footer stays in range until the
next stable tag, so it would force the same pre-release again after it shipped. A pre-release
label on the release pull request builds on the forced version instead, `2.0.0-rc.1` for a
footer of `2.0.0` and the `rc` label.

A `release-me-version` block on the release pull request, from a `release-as` comment or a
hand edit, wins over every footer.

### Release-Component footer

With `components`, a commit counts for every component whose `path` it touches. Two rules
reach a component beyond that, to attribute a change after the fact, a docs-only `feat` for
one:

- A commit that changes no files counts for every component, the way release-please treats an
  empty commit. A merge commit is compared against its first parent, so it is empty only when
  its whole branch is.
- A `Release-Component: <name>` footer attributes the commit to the named component on top of
  the components its files touch. The key is case-insensitive, the footer may repeat, and one
  footer may list several names separated by commas. On an empty commit, the footer narrows it
  to the named components instead of every one.

```text
feat(duxt): document the layout prop

Release-Component: duxt
```

The footer is read where a [Release-As footer](#release-as-footer) is: the commit's own
message, a squash merge's commit body, a merged pull request's body, and in `all` mode every
commit on the branch. In `nested` mode the messages of the commits a merge introduced
attribute the whole merge; `first-parent` and `squash` never read those.

Inside a message, the footer is read only from the trailer block, exactly as a `Release-As`
footer is: the last paragraph below the first line, and only when every line in it is a
`Key: value` trailer. A `Release-Component:` line anywhere else is inert text: in an earlier
paragraph, fenced, quoted with `>`, in a list item, inside an HTML comment, or in a last
paragraph that also holds prose. The fenced example above, quoted into a pull request body,
therefore attributes nothing.

A name no component carries reaches no component. It is logged as a warning, never failing
the run, and the commit counts only for what its files and its other names reach: an empty
commit naming only unknown components counts for none, never for every one, so a typo shows
as a missing entry rather than an entry in every changelog. Without `components`, every
commit already counts for the implicit `main` component, so the footer is not read at all, and
a footer naming anything else changes nothing, without a warning.

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
release pull request, the shape every example above already is, unless
`release-pull-request: shared` puts every component in one.

`locked` bumps every component to the same new version and releases all of them together,
changed or not: one release pull request, branch `release-me/main`, with one changelog section
per component, each updating its own `CHANGELOG.md`. Its title names the version and every
component it covers, `chore: release vX.Y.Z (a, b, c)`. On merge, every component is tagged
`<name>/vX.Y.Z` with the same version and released the same way an independent component is.
`locked` always releases the whole family, so `--component` naming one of them is a usage
error for `next`, `changelog`, `pr`, `release` and `run` alike.

`linked` shares one version line across every component, but releases only a component with
entries since its own last tag, at the family's next version: one release pull request per
changed component, as `independent` already opens, the shared version applied to each, or one
for all of them with `release-pull-request: shared`. An unchanged component keeps its version
and gets no release.

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

### Shared release pull request

The top-level `release-pull-request` key chooses between one release pull request per
component, `per-component`, and one for every component, `shared`. `per-component` is the
default for `independent` and `linked`, so nothing changes without the key; `locked` always
shares one, and `per-component` there is a configuration error. A repository without
`components` has one release pull request either way.

```yaml
release-pull-request: shared
components:
  api:
    path: services/api
  web:
    path: apps/web
```

The shared pull request uses branch `release-me/main` and carries every component with
something to release this run, each at its own version for `independent` and at the family's
for `linked`, with its own `CHANGELOG.md` section and its own `files` bumped to its own version.
A component with nothing to release is left out and printed as `nothing to release`, the same
as it gets no pull request of its own per component. Two components listing the same file
each update their own `key` in it. Two `independent` components bumping the same `key` of one
file, or both a `marker` or `helm` file, inherited top-level `files` included, are a
configuration error, since one would overwrite the other's version; `locked` and `linked`
write the one family version there and allow it. The title names every component and its
version, in `release-me.yaml`'s order, `chore: release api v1.3.0, web v0.3.1`, the same as
release-please's grouped pull request names the packages in its body. GitHub caps a title at
256 characters, so how many components fit depends on their names and versions, about eight
with 20-character names: `pr` refuses a longer title before it pushes or writes anything,
naming its length and the components, since `release` could not read it back after the merge.
Release more often, so fewer components carry changes at once, or switch to `per-component`.
The body starts with
`Release api v1.3.0, web v0.3.1`, the guide names every tag merging creates, and the changelog
has one `#### <name>` section per component, each with that component's own heading and compare
link. On merge, `release` reads the components and versions back from the title and tags each
one on the merge commit, creates one GitHub release per component from the notes block and that
component's own section, moves each component's major tag where `major-tag` applies, and swaps
the labels once for the whole pull request.

The pull request's pre-release label turns every component it carries into a pre-release of
that type, each counting `N` on its own tag prefix for `independent` and across the family for
`linked`. The notes block is shared: every component's release gets it. A `release-me-version`
block forces one version for the whole family in `linked`; in `independent` it takes one
`<component> X.Y.Z` line per component, which a `release-as <component> X.Y.Z` comment writes,
each checked against that component's last tag, and a line naming a component the pull request
does not release is ignored with a warning. A plain `X.Y.Z` line there only applies when the
pull request releases one component. The `Release-As` and `Release-Component` footers work the
same as with one pull request per component. `pr`, `release` and `run` refuse `--component`,
since the shared pull request carries every component; `next` and `changelog` keep it.

Switching to `shared` while per-component pull requests are open leaves them open: every `pr`
run prints each one on `release-me/<name>` as superseded by the shared one, to close by hand,
and `release` still tags one that is merged instead, so it never stays pending. `release` only
looks for one while the component's own `release-me/<name>` branch exists on the remote, one
`git ls-remote` per run. A repository that deletes head branches on merge loses that branch as
the pull request merges, so merge the switch only after `release` has tagged every
per-component pull request merged before it, or tag a missed one by hand. Switching back
to `per-component` does not read `release-me/main` any more: merge or close the shared pull
request first.

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
  governs every API call, the release commit `pr` creates included. Reading the release
  branch back, to compare it, goes through the git credentials the checkout's `origin` remote
  already carries.
- `changelog`: the changelog file path, default `CHANGELOG.md`. `pr` prepends the new version's
  section to it, creating the file when it does not exist yet.
- `changelog-source`: which commits become changelog entries, default `first-parent`. See
  [Changelog modes](#changelog-modes) for `first-parent`, `squash`, `all` and `nested`. A
  component overrides it through its own `changelog-source` key.
- `labels.pending` and `labels.tagged`: the label names `pr`, `release` and `check` use,
  default `release: pending` and `release: tagged`.
- `labels.alpha`, `labels.beta` and `labels.rc`: the pre-release type labels `pr`, `release`
  and `check` use, default `release: alpha`, `release: beta` and `release: rc`. `false`, or
  `""`, turns that pre-release type off, so a repository that never cuts pre-releases needs
  none of the three labels. See [Pre-releases](#pre-releases). `check` reports any of the
  enabled labels missing by name, it never creates one. `labels.pending` and `labels.tagged`
  cannot be empty.
- `author.name` and `author.email`: accepted, default `release-me` and
  `release-me[bot]@users.noreply.github.com`, but no longer applied. The forge creates the
  release commit under the token's own identity and signs it, which it does only when no author
  is passed, see [Signed release commits](#signed-release-commits).
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
- `release-pull-request`: `per-component`, the default for `independent` and `linked`, or
  `shared`, always the case for `locked`, which rejects `per-component`. See
  [Shared release pull request](#shared-release-pull-request).
- `draft-until-assets-attach`: drafts every release instead of publishing it immediately,
  default `false`. There is no per-component override: it applies to the whole repository.
  Only a repository with GitHub's immutable releases setting turned on needs `true`; it
  rejects any later change to a published release, assets included, and release-me has no way
  to detect that setting through the API. See `release` above for what `true` changes, and
  [Install](#install) for the calling workflow each setting needs: `publish.yaml`'s own
  webhook-triggered asset step for `false`, `release.yaml` attaching and publishing the
  release itself for `true`.
- `comment-mention`: the mention prefix a comment on the release pull request names to issue a
  `release-as` or `release-notes` command, default `@release-me`. See
  [Comment commands](#comment-commands). Cannot be empty.
- `components`: maps a name to a `path` and optional overrides of `initial-version`,
  `bump-minor-pre-major`, `hidden-types`, `files`, `major-tag`, `changelog-source` and
  `changelog`. A key
  left out of a component falls back to the top-level setting above. A repository without
  `components` is
  one component named `main` at path `.`, tag prefix `v`, branch `release-me/main`, the shape
  every example above already is. A configured component gets tag prefix `<name>/v`, branch
  `release-me/<name>`, or `release-me/main` when the release pull request is shared, changelog
  `<path>/CHANGELOG.md`, and a release title `<name> vX.Y.Z`. A name holds ASCII letters,
  digits, `-`, `_` and `.` only, does not start with `.` or `-`, does not end with `.` or
  `.lock` and does not contain `..`; anything else is a configuration error naming the
  component and the character, since the name has to survive the tag prefix, the branch, the
  shared pull request title release reads back, the
  [Release-Component footer](#release-component-footer) and the compare link.
  `tag-prefix` replaces the derived prefix, for a component whose existing tags follow
  another scheme, such as `api@v1.4.0`: release-me continues from its last such tag instead of
  starting over at `initial-version`. The version is appended to the prefix as it is. A prefix
  that is empty, holds whitespace or one of `~ ^ : ? * [ \`, contains `..`, `@{` or `//`, or
  starts with `/`, `-` or `.` is a configuration error, as are two components with the same
  prefix. On top of these git ref rules, a prefix holding `(`, `)`, `#` or `%` is one too, as
  those break the changelog heading's compare link. The prefix is how a component finds its
  history: adding, changing or removing `tag-prefix` once a component has tags under its
  previous prefix restarts it at `initial-version`, the same way renaming the component would.
  `changelog` replaces the
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
  of their changelogs. An empty commit lands in every component, and a
  [Release-Component footer](#release-component-footer) adds the components it names. The
  component at path `.`, explicit or implicit, matches every entry.
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
