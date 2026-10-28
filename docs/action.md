# GitHub Action reference

Full reference for the composite action: every input and output, the
`draft-until-assets-attach` workflow shapes, the labeled-trigger guard, and bootstrapping the
first release. See the [README](../README.md#install) for the short version.

## Inputs

The action downloads the archive and checksums file matching its own version for the
runner's os and arch, verifies the checksum, then runs `release-me run`. Inputs: `token`
(required), `component`, `dry-run`, `verbose`, `download-token` for the download step on a
private fork, defaulting to the calling workflow's own token, `version` to pin a different
release than the one the action resolves on its own, or `source` to build from the checkout
with `go build` instead, and `author-name`, `author-email`, `app-slug` to attribute the
release commit. An explicit `author-name` or `author-email` wins for that field; with
`app-slug` set, the action resolves the app's bot account, `<slug>[bot]`, and its id through
the API with `download-token`, and attributes the commit to it. Nothing set leaves the tool's
own configured or default identity in place. `source` only builds this repository's own code:
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

## Outputs

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
  [shared release pull request](versioning-modes.md#shared-release-pull-request) is one entry
  naming several components. Dry-run opens nothing, so it reports none.

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

## Draft until assets attach

`draft-until-assets-attach` in `release-me.yaml` decides whether each tagged release
publishes immediately or starts as a draft, default `false`, no behavior change from before
this key existed. See [configuration](configuration.md#draft-until-assets-attach) for what
each setting needs from the calling workflow:

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

## Triggering the workflow

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

## Bootstrap

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
