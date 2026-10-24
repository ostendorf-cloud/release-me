# 1. Create releases as drafts until assets attach, then publish

## Status

Accepted

## Context

- `Forge.CreateRelease` published a release immediately, non-draft, the moment release-me
  tagged a merged release pull request.
- `publish.yaml`'s `assets` job ran `goreleaser release --clean` afterward, triggered by the
  `release: published` webhook, to attach binary archives and checksums to that release.
- GitHub's immutable releases feature, once on for a repository, rejects any change to a
  published release, assets included: the attach step fails with "release vX.Y.Z already
  exists and is immutable, it cannot be updated", confirmed live for v0.1.0 and v0.1.1.
- A release that already hit this cannot be recovered: deleting and recreating it under the
  same tag is permanently blocked ("tag_name was used by an immutable release"), confirmed by
  a live API probe. v0.1.0 and v0.1.1 have both since been deleted outright, tag and release;
  a fresh probe afterward, drafting and then trying to publish a new release at either tag
  name, still fails with the same message. Neither version comes back, no matter what, if
  anything, exists at that tag name today.
- GitHub's REST API has no field on the repository object exposing the immutable releases
  setting, so release-me cannot read or react to it; the fix must work whether the setting is
  on or off.
- GitHub's own documentation states immutability blocks adding, modifying or deleting release
  assets after publish, regardless of which endpoint does the attaching; a draft release is
  exempt, since immutability only starts at publish.
- `GET /repos/{owner}/{repo}/releases/tags/{tag}` (go-github's `GetReleaseByTag`, and what
  goreleaser's `mode: keep-existing` is believed to use to find an existing release) returns
  404 for a draft; verified live against this repository. `gh release upload`, `gh release
  view` and `gh release edit --draft=false` all resolve a draft release by tag correctly,
  also verified live.
- No webhook `release` activity type fires when a draft release is created: `created`,
  `edited` and `deleted` are suppressed outright for drafts, confirmed against GitHub's
  events-that-trigger-workflows docs. Converting a draft to published fires `published`, never
  `created`, even for a pre-release. `prereleased` does not fire at all for a pre-release
  published from a draft release, only `published` does, confirmed against the same docs: a
  workflow that subscribes to `prereleased` instead of `published` would never see one of
  release-me's own pre-releases.

## Decision

Release-me gains a repository-level opt-in, `release-me.yaml`'s `draft-until-assets-attach`
key, default `false`. Off, `Forge.CreateRelease` keeps publishing every release immediately,
exactly as release-me did before this ADR: a repository without GitHub's immutable releases
setting on, the common case, sees no behavior change at all. On, release-me drafts every
release instead, and `release.yaml`, the same workflow run that just created it, builds and
attaches the assets and publishes it itself, instead of waiting on a second, webhook-triggered
workflow. This repository is the one that actually needs it, so its own `release-me.yaml`
sets `draft-until-assets-attach: true`.

- `Forge.CreateRelease` takes a `draft bool` parameter now, the caller's own choice, instead
  of always passing `draft: true`. `Run`, `RunLocked`, `releaseOne` and `releaseLockedFamily`
  thread `draft-until-assets-attach`'s value through to it, the same way they already thread
  `dryRun`.
- `release.yaml` runs `goreleaser release --clean --skip=announce,validate,publish` right
  after release-me tags and drafts a release, to build archives and checksums without
  goreleaser ever calling the GitHub release API, then attaches those archives with
  `gh release upload` and publishes with `gh release edit --tag <tag> --draft=false`, both
  confirmed live to work against a draft. This is the workflow shape a repository runs once it
  sets `draft-until-assets-attach: true`; it carries no `publish.yaml` `assets` job at all.
- A repository that leaves `draft-until-assets-attach` off keeps the workflow shape from
  before this ADR instead: `publish.yaml`'s `assets` job, triggered by the `release: published`
  webhook release-me's immediate publish already fires, runs plain `goreleaser release --clean`
  to attach assets the old way. Either shape's `image` job keeps its `release: types:
  [published]` trigger unchanged, since publishing, immediate or from a draft, fires
  `published` either way.
- No `Forge.PublishRelease` method was added: nothing in release-me's own Go commands needs to
  un-draft a release, only the workflow does, once assets are attached, through `gh`.
- The pull request's label still swaps from pending to tagged immediately after
  `CreateRelease` succeeds, the same point as before this ADR, whichever way
  `draft-until-assets-attach` is set: that swap is release-me's own signal that a pull request
  already has a tag and a release, the one issue #45's fix relies on, not a signal that the
  release is finished. `Run` and `RunLocked` additionally scan merged pull requests already
  carrying the tagged label, through a new `Forge.IsReleaseDraft`, and resume any whose
  release is still a draft by reprinting its released-tag marker, without calling `CreateTag`
  or `CreateRelease` again, but only when `draft-until-assets-attach` is on: off, a release
  publishes the moment it is created, so a tagged pull request never has anything left to
  resume, and this scan, and the extra `IsReleaseDraft` call it would otherwise make per
  tagged pull request, is skipped outright. `IsReleaseDraft` needs its own lookup, listing
  releases and matching the tag by hand, for the same reason attaching assets does: a draft
  release 404s on "get a release by tag". This keeps the label swap, and every existing test
  built around its timing, exactly where it already was, while letting a rerun after a failed
  asset attach or publish step still find and finish the release, driven by the release's own
  draft status on GitHub rather than by this run's output.

## Consequences

- Turning `draft-until-assets-attach` on makes a release invisible to anyone outside the
  repository's write access between tagging and the workflow's publish step, a window of
  roughly one CI run instead of none. Off, the default, there is no such window: a repository
  that never sets the key sees nothing about this ADR at all.
- The config key and the workflow shape must agree: turning `draft-until-assets-attach` on
  without adopting `release.yaml`'s build-attach-publish step leaves every release stuck an
  unpublished draft forever, since a draft fires no webhook a second workflow could ever wait
  on. Turning it off while still running `scripts/publish-release-assets.sh` is harmless, its
  already-published guard skips every tag immediately, but pointless.
- `.goreleaser.yaml`'s `release:` block (`mode: keep-existing`, `prerelease: auto`) is dead in
  this repository's own configuration, since it runs the new workflow shape and goreleaser
  never reaches its publish phase; removed here, along with the regression test it existed
  for (issue #41). A repository that leaves `draft-until-assets-attach` off and keeps the old
  workflow shape keeps that block too: goreleaser still owns the GitHub release call there,
  same as before this ADR.
- A consumer of release-me other than this repository sees no behavior change at all unless it
  explicitly sets `draft-until-assets-attach: true` in its own `release-me.yaml`: the default
  stays `false`, `CreateRelease` keeps publishing immediately, and the generic action and CLI
  contract is exactly what it was before this ADR. Turning the key on is only useful, and only
  safe, together with adopting the new workflow shape above.
- If a CI run fails between tagging and publishing, with `draft-until-assets-attach` on, the
  release stays a draft until the next CI run resumes it: not because `CreateRelease` tolerates
  `ErrReleaseExists` on retry, since by then the pull request's label has already swapped away
  from pending and a rerun would never reach that call again. It resumes instead because `Run`
  and `RunLocked` also look at merged pull requests already carrying the tagged label and check
  their release's draft status directly, as the Decision section above describes; a rerun of
  the whole pipeline, not only of release-me's own step, still finds and finishes a release
  orphaned this way.
- Pull request #50 (issue #49) rewrites `CreateRelease`'s 422 handling in
  `internal/forge/github`, to distinguish a genuine "already exists" 422 from any other reason
  GitHub might return one. This branch left that handling as it already was, pre-#50, since
  the two touch the same function for unrelated reasons; they will need an ordinary rebase or
  merge once both land, but neither depends on the other landing first.
- Not verified in this environment: a draft release's assets actually attaching and
  publishing against a repository with GitHub's immutable releases setting turned on. That
  setting is off on this repository today, and no `release.yaml` run has exercised the new
  path end to end, so the immutable-releases-specific half of this fix rests on GitHub's
  documented behavior and this ADR's own live probes against individual `gh` and API calls,
  not on an observed, complete run.

## Alternatives

- Draft unconditionally for every consumer, this ADR's own first shape: rejected on
  reconsideration. Most consumers of release-me do not have GitHub's immutable releases
  setting on, so forcing the draft-then-publish-in-one-job flow on everyone adds unnecessary
  complexity, and changes the generic action's contract, for no benefit to them. An opt-in
  config key keeps the default exactly what release-me's behavior was before this ADR.
- Keep releases non-draft and have `publish.yaml` attach assets through an endpoint that
  never touches the release object: rejected, since immutability blocks every asset
  modification after publish, not just a PATCH to the release itself.
- Let goreleaser keep owning the GitHub release call (`mode: keep-existing` against the
  now-draft release): rejected, its lookup-by-tag 404s for a draft, confirmed live.
- Switch `publish.yaml`'s trigger to `release: types: [created]`: rejected, GitHub suppresses
  `created` for draft releases, confirmed against GitHub's own documentation.
