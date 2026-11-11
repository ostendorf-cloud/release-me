# 2. Name the bump strategy versioning-strategy and reinterpret its Java values

## Status

Accepted

## Context

- release-please's `versioning` key names how a release bumps the version: `always-bump-patch`,
  `always-bump-minor`, `always-bump-major`, `service-pack` or `dependency-manifest`.
- release-me's own `versioning` key already names how a family's versions relate to each other,
  `independent`, `locked` or `linked`, and predates the bump strategy.
- A migrated repository has to keep the strategy it releases under, so the key cannot simply be
  dropped.
- release-please defines `service-pack` and `dependency-manifest` against Java artifacts: a
  maintenance release of a Maven artifact, and a bump read out of a Java dependency manifest.
- release-me reads a repository's first parent history and the titles of the merged pull
  requests behind it; it has no Java-specific input of any kind.
- The version is already decided jointly by the computed bump, the pre-major rules, a forced
  version, a pre-release label and a workspace dependency, so a strategy has to say where it
  sits among them.

## Decision

release-me carries the key over as `versioning-strategy` and gives both Java-specific values a
meaning any repository can use.

- The name is `versioning-strategy`; `versioning` keeps release-me's own meaning, and `migrate`
  renames the key for a migrating repository.
- `always-bump-patch`, `always-bump-minor` and `always-bump-major` mean what they do in
  release-please, below `1.0.0` included.
- `service-pack` ships `X.Y.Z-sp.N` against the last version without moving it, `N` counting up
  from the last `sp` tag of `X.Y.Z`, treated as the semver pre-release it is: the GitHub release
  is a pre-release, the major tag stays, the next stable version still counts from `X.Y.Z`.
- `service-pack` applies with `versioning: independent` only, since the counter is read off one
  component's own tags, and a pre-release label on its release pull request is refused, since
  `sp.N` is already that version's pre-release identifier.
- `dependency-manifest` raises the bump to the largest dependency update among the changes, read
  from the pull request title Dependabot and Renovate write: Dependabot's `bump <name> from <old>
  to <new>` by the highest part that changed, Renovate's `update dependency <name> to v<new>` by
  the new version's shape, as release-please does for a manifest.
- A forced version, `initial-version` and a workspace dependent's plain patch all win over the
  strategy; `bump-minor-pre-major` and `bump-patch-for-minor-pre-major` apply to `default` and
  `dependency-manifest` only.
- `next` names the strategy in its reason, so the version it produced is traceable to it.

## Consequences

- `dependency-manifest` depends on a title shape two external bots own: a repository whose
  dependency pull requests are titled differently gets ordinary bumps, with no error and no
  warning, visible only in `next`'s reason.
- A repository on `service-pack` with a `locked` or `linked` family cannot keep that strategy:
  loading the configuration refuses it, and `migrate` warns and drops it for the locked family a
  `linked-versions` plugin becomes.
- Two keys whose names differ by one word carry two unrelated meanings, which is a real reading
  cost; `docs/configuration.md` states the rename at the key itself.
- The reinterpretation is not release-please's behaviour, so `migrate` warns about both values
  instead of carrying them silently.
- A service pack ends up ordered below its own base version by semver, which is correct but
  means tooling that reads the newest tag sees a pre-release.

## Alternatives

- Keep release-please's name `versioning` for the strategy: rejected, it collides with
  release-me's existing key and one of the two meanings would have to move.
- Keep release-me's `versioning` and refuse the strategy key outright: rejected, every migrating
  repository on an always-bump strategy would silently change its bump.
- Refuse `service-pack` and `dependency-manifest` and warn at migration: rejected, a repository
  loses its maintenance release line or its dependency majors with nothing to switch to.
- Read the dependency update from the manifests, as release-please does: rejected, that needs
  every dependency's version before and after the change, while the bot's own title already
  carries both.
- Let `service-pack` work in `locked` and `linked` too: rejected, the family shares one version
  line and the `sp` counter is read off a single component's tags.
