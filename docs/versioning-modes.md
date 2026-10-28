# Versioning modes

The top-level `versioning` key chooses how a repository's configured components' versions
relate to each other: `independent` (default), `locked` or `linked`.

`independent` versions every component on its own: its own tag line, its own bump, its own
release pull request, the default shape a configured component has, unless
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

## Shared release pull request

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
