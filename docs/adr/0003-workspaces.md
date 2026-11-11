# 3. One workspaces key for three package managers, manifests only

## Status

Accepted

## Context

- A component whose published manifest depends on a sibling component has to be released when
  that sibling is, or its manifest points at a version that does not exist yet.
- release-please solves this with one plugin per package manager, `node-workspace`,
  `cargo-workspace` and `maven-workspace`, each with options of its own.
- The three managers model a local dependency differently: npm, pnpm and yarn write a
  dependency object per section, Cargo a requirement, an inline table or a table of its own,
  Maven a `<groupId>:<artifactId>` coordinate inherited through `<parent>`.
- Every updater keeps the rest of the file byte for byte, so a dependency is rewritten in place
  rather than through a round trip that would drop comments and ordering.
- A dependent and its dependency have to be tagged in one run, so they have to sit in one
  release pull request.
- A lockfile records a resolved version for every dependency, not only for the components of
  the repository, and release-me is given one file per `files` entry.
- A dependent that releases only because its dependency did has no change of its own to compute
  a bump from.

## Decision

One top-level `workspaces` key names the package managers whose manifests link components, and
the behaviour is the same for all three.

- The value is a list of `node`, `cargo` and `maven`; each component's manifest is read at
  `<path>/package.json`, `<path>/Cargo.toml` or `<path>/pom.xml`, and a component without that
  file is no member.
- A released component's version is moved in every other component's dependency on it, and that
  dependent releases in the same release pull request; a dependent of a dependent follows.
- `workspaces` requires `components` and one release pull request for all of them,
  `release-pull-request: shared` or `versioning: locked`; anything else is a configuration
  error.
- A dependent that releases only for its dependency takes a plain patch on its own last tag in
  `independent`, the family's version in `linked`, and ignores its own `versioning-strategy`,
  which has no changes to apply to.
- A requirement that names no single version, `workspace:*`, `file:../core`, `${core.version}`
  or a range, stays as written and still releases the dependent.
- Each manifest is read line by line: the node dependency objects have to declare one
  dependency per line, and a Cargo dependency spread over several lines is not read.
- `workspaces` moves manifest dependencies only. Lockfiles are out of scope: a repository that
  bumps one lists it under `files` with the `npm-lock`, `cargo-lock` or `gemfile-lock` updater.
- `always-link-local` modifies `node` alone, where a range that does not resolve to the local
  package is left to the registry.

## Consequences

- One key covering three managers means one set of rules to learn, but the layout requirement is
  per manager, and that is where a reader is surprised: node fails `pr` naming the file and the
  dependency, Cargo leaves a multi-line dependency as written.
- The lockfile scope cut sits next to three lockfile updaters for the same files, so the split
  has to be read twice: `workspaces` moves the manifest, a `files` entry moves the lockfile.
- A repository adopting `workspaces` gives up per-component release pull requests for every
  component, including those outside the workspace.
- The plain patch surprises a dependent configured with an always-bump strategy: it releases
  below the strategy it names, and only because a sibling did.
- `next` and `changelog` report a component's own changes, so the dependents a release will
  carry are not visible until `pr` runs.
- A manager release-me does not name has no link at all, Bundler among them, even though a
  `gemfile-lock` updater exists for its lockfile; such a repository releases its dependents by
  hand.

## Alternatives

- One key per manager, as release-please's plugins are: rejected, the three would carry the same
  rules three times and a repository with two managers would set two keys that cannot disagree.
- Parse each manifest with a real parser and write it back: rejected, every updater in
  release-me keeps the untouched bytes exactly, which a round trip through a parser gives up.
- Release a dependent at the bump its own `versioning-strategy` names: rejected, it has no
  change to bump, so the strategy would invent a minor or major release out of nothing.
- Let dependents release in their own pull requests: rejected, a dependency and its dependent
  would be tagged in different runs, so a published manifest would point at a version that does
  not exist yet.
- Update the lockfiles from `workspaces` as well: rejected, a lockfile records resolved versions
  for the whole dependency graph, and the `npm-lock`, `cargo-lock` and `gemfile-lock` updaters
  already bump the entry a release owns.
