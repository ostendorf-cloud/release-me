# Changelog modes

`changelog-source` selects which commits become changelog entries, top level and per
component, default `first-parent`. In every mode, a pull request's
[override block](overrides.md#override-block) replaces its entries, the merge's or the
individual commits' alike.

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
