# Overrides

Every pull request body, commit message or comment mechanism that forces a version or changes
what lands in the changelog. See the [README](../README.md#use) for the short version.

## Notes block

A fenced block `release-me-notes` in the release pull request body is free text for the
release:

````markdown
```release-me-notes
Run the migration before upgrading.
```
````

Add it by hand once the pull request is open. `pr` keeps it verbatim on every later update, and
`release` carries it into the GitHub release, above the changelog.

## Override block

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

## Comment commands

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

## Release-As footer

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

## Release-Component footer

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
