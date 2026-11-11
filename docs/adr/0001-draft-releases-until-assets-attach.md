# 1. Create releases as drafts until assets attach, then publish

## Status

Accepted

## Context

- `Forge.CreateRelease` published every release immediately, the moment release-me tagged a
  merged release pull request, and `publish.yaml`'s `assets` job attached the binaries
  afterwards on the `release: published` webhook.
- GitHub's immutable releases feature rejects every change to a published release, assets
  included, so that attach step fails with "already exists and is immutable".
- A release that hit this cannot be recovered under the same tag name, even after deleting it:
  v0.1.0 and v0.1.1 were deleted outright and both tag names stay blocked.
- GitHub's REST API exposes no field for the immutable releases setting, so release-me cannot
  read it and the fix has to work whether it is on or off.
- A draft release is exempt, since immutability starts at publish, but `GET
  /repos/{owner}/{repo}/releases/tags/{tag}` returns 404 for a draft, while `gh release upload`,
  `view` and `edit --draft=false` all resolve one.
- No `release` webhook activity fires for a draft: `created`, `edited` and `deleted` are
  suppressed, and converting a draft to published fires `published` only, never `created` or
  `prereleased`.

## Decision

Release-me gains a repository-level opt-in, `github-release-draft-until-assets-attach`, default
`false`, and drafts every release when it is on.

- Off, every release publishes immediately, exactly as before this ADR: a repository without
  GitHub's immutable releases setting sees no change at all.
- On, the same workflow run that tagged and drafted the release builds, attaches and publishes
  it itself, since a draft fires no webhook a second workflow could wait on.
- `Forge.CreateRelease` takes the draft choice as a parameter, threaded through the release
  commands the way `dryRun` already is.
- No `Forge.PublishRelease` was added: only the workflow needs to un-draft a release, through
  `gh`.
- The label swap from pending to tagged still happens right after `CreateRelease`, as before:
  it marks that a pull request has its tag and release, not that the release is finished.
- A rerun resumes instead: the release commands scan merged pull requests already carrying the
  tagged label and reprint the released-tag marker for any whose release is still a draft,
  through `Forge.IsReleaseDraft`, which lists releases and matches the tag by hand because a
  draft 404s on get-by-tag.
- That scan runs only with the key on; off, a tagged pull request has nothing left to resume.
- [`github-release-draft`](../configuration.md#github-release-draft) is the sibling key added
  later: it drafts a release for a human to publish and is never resumed, so a deliberate draft
  is not mistaken for one waiting on assets.

## Consequences

- With the key on, a release is invisible outside the repository's write access between tagging
  and the publish step, a window of roughly one CI run.
- The key and the workflow shape have to agree: turning it on without the build-attach-publish
  step leaves every release an unpublished draft forever.
- With [`force-tag-creation: false`](../configuration.md#force-tag-creation) the version tag is
  left to the release's publish. GitHub creates it then; release-me relies on this and GitHub
  does not document it. If the tag does not appear, `pr` reports the missing tag and waits
  instead of releasing the same version again.
- `.goreleaser.yaml`'s `release:` block is dead in this repository's own configuration, since
  goreleaser never reaches its publish phase here; a repository on the old workflow shape keeps
  it.
- A consumer that never sets the key sees no behaviour change: the default stays `false` and the
  action and CLI contract is what it was.
- The immutable-releases half of the fix rests on GitHub's documented behaviour and on probes of
  the individual `gh` and API calls, not on a complete run against a repository with the setting
  on, which this repository does not have.

## Alternatives

- Draft unconditionally for every consumer: rejected, most consumers do not have immutable
  releases on, and it would change the action's contract for no benefit to them.
- Keep releases published and attach assets through an endpoint that never touches the release
  object: rejected, immutability blocks every asset modification after publish.
- Let goreleaser keep owning the GitHub release call with `mode: keep-existing`: rejected, its
  lookup by tag 404s for a draft.
- Switch `publish.yaml`'s trigger to `release: types: [created]`: rejected, GitHub suppresses
  `created` for draft releases.
