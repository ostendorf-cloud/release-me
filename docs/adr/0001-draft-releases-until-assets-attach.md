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
  a live API probe.
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
- No webhook `release` activity type fires when a draft release is created: `published` and
  `prereleased` fire only on an actual publish, and `created`/`edited`/`deleted` are
  suppressed for drafts, confirmed against GitHub's events-that-trigger-workflows docs.
  Converting a draft to published fires `published` (never `created`), even for a
  pre-release, confirmed against the same docs.

## Decision

Release-me creates every release as a draft; `release.yaml`, the same workflow run that just
created it, builds and attaches the assets and publishes it, instead of waiting on a second,
webhook-triggered workflow.

- `Forge.CreateRelease` now always sets `draft: true`. Unconditional, since release-me cannot
  detect the immutable releases setting, and a draft is harmless whether or not it is on.
- `release.yaml` runs `goreleaser release --clean --skip=announce,validate,publish` right
  after release-me tags and drafts a release, to build archives and checksums without
  goreleaser ever calling the GitHub release API.
- `release.yaml` then attaches those archives with `gh release upload` and publishes with
  `gh release edit --tag <tag> --draft=false`, both confirmed live to work against a draft.
- `publish.yaml`'s `assets` job is removed; its `image` job keeps its `release: types:
  [published]` trigger unchanged, since publishing a draft fires `published`.
- No `Forge.PublishRelease` method was added: nothing in release-me's own Go commands needs to
  un-draft a release, only the workflow does, once assets are attached, through `gh`.

## Consequences

- A release is invisible to anyone outside the repository's write access between tagging and
  the workflow's publish step, a window of roughly one CI run instead of none.
- `publish.yaml`'s `image` job now starts only once that whole window has passed, instead of
  immediately on tagging; it already depended on `release: published`, so it still runs, just
  later.
- `.goreleaser.yaml`'s `release:` block (`mode: keep-existing`, `prerelease: auto`) is dead
  once goreleaser never reaches its publish phase; removed, along with the regression test it
  existed for (issue #41), since goreleaser issuing that PATCH is no longer possible.
- A consumer of release-me other than this repository, whose own CI still waits on `release:
  published` to attach assets, keeps working unchanged only as long as their repository has
  immutable releases off; this ADR does not change the generic action or CLI contract beyond
  "a release starts as a draft", which they must still account for.
- If a CI run fails between tagging and publishing, the release stays a draft until the next
  CI run resumes it: release-me's `CreateRelease` already tolerates `ErrReleaseExists` on
  retry, so a rerun can still find and finish publishing it.

## Alternatives

- Keep releases non-draft and have `publish.yaml` attach assets through an endpoint that
  never touches the release object: rejected, since immutability blocks every asset
  modification after publish, not just a PATCH to the release itself.
- Let goreleaser keep owning the GitHub release call (`mode: keep-existing` against the
  now-draft release): rejected, its lookup-by-tag 404s for a draft, confirmed live.
- Switch `publish.yaml`'s trigger to `release: types: [created]`: rejected, GitHub suppresses
  `created` for draft releases, confirmed against GitHub's own documentation.
