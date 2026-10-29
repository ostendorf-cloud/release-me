---
name: setup-release-me
description: >-
  Sets up release-me in a repository with no release tool yet: release-me.yaml and the release
  workflow, the missing labels listed for the human, proven by release-me check and next. Use
  when the user wants to set up, add or adopt release-me, or start releasing a repository with it.
---

# setup-release-me

Give a repository a working **release-me** setup: one `release-me.yaml`, the labels it names,
handed to the human to create, one release workflow, and a dry run proving the three agree. The
[release-me README](https://github.com/ostendorf-cloud/release-me#readme) at the pinned
release is the single source of truth for every key and action input; this skill decides which
of them the repository needs.

## When to use

The repository has no release tool yet, or one other than release-please, and the user wants
to release it with release-me. A repository on release-please goes to the
`migrate-release-please-to-release-me` skill instead.

## Steps

### 1. Survey

Read the repository; every row below comes from a command or a file, never from a guess.

- **Forge and merge methods:**

  ```sh
  gh api graphql -F owner='{owner}' -F name='{repo}' --jq .data.repository -f query='
    query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {
      nameWithOwner defaultBranchRef { name } mergeCommitAllowed squashMergeAllowed rebaseMergeAllowed } }'
  ```

  GraphQL, because the REST repository endpoint returns the merge fields only to a token with
  push access.

  A repository off GitHub ends the run here: release-me releases through the GitHub API.
- **Release tooling already present:** a `release-please-config.json` hands the run to the
  `migrate-release-please-to-release-me` skill, an optional call: where it is not installed,
  the run ends here and names the release-please setup as the reason. Any other tool
  (`.releaserc*`, `.changeset/`, a workflow running semantic-release) is named in the plan and
  removed in the same change, because two tools tagging one history race each other.
- **Existing tags:** `git tag --list --sort=-v:refname | head -20`. Their scheme becomes the tag
  prefix, so release-me continues from the last one: `vX.Y.Z` is the implicit main component,
  `name@vX.Y.Z` a component with `tag-prefix: name@v`.
- **Components:** each separately versioned package and the file carrying its version
  (`package.json`, `Chart.yaml`, `Cargo.toml`, `pyproject.toml`, a Go constant). One package at
  the root is the implicit main component and needs no `components` key.
- **Version to pin:** `gh api repos/ostendorf-cloud/release-me/releases/latest --jq .tag_name`.
  Without a release, a 404, there is nothing to pin, and the run ends with that finding.

Done when a survey table lists forge, merge methods, every component with its path, version
file and tag scheme, and the pinned version, each row filled or marked absent.

### 2. Decide

Four choices belong to the human; ask each once, recommendation first:

- **Versioning**, only with more than one component: `independent` (each its own line),
  `linked` (one shared line, only changed components release) or `locked` (one shared line,
  every component releases together).
- **Release pull requests**, only with more than one component and not `locked`, which always
  shares one: `per-component` (the default, one each) or `shared` (one for every component with
  something to release), written as `release-pull-request: shared`. A shared title names every
  component and version and must fit GitHub's 256 characters, about eight 20-character names;
  `pr` refuses a longer one, so recommend `per-component` for a larger family. A body with
  several components, `shared` or `locked`, gives each a `## <name>` heading by default; leave
  `release-pull-request-layout` out unless the human asks for `details` or `compact`.
  Every release pull request body opens with a collapsed "How this release works" guide; leave
  `release-pull-request-guide` out too, and write `release-pull-request-guide: false` only
  when the human asks to drop it.
- **Pre-releases:** wanted, or turned off with `alpha`, `beta` and `rc` set to `false` under
  `labels`, so the repository needs only the pending and tagged labels.
- **Token:** a GitHub App (recommended: its release pull requests trigger checks and the
  release commit is attributed to its bot) or a repository token.

Everything else is derived:

- **`changelog-source`** from the merge methods, first match wins: merge commits allowed, in
  any combination, is `first-parent`; otherwise squash allowed, alone or beside rebase, is
  `squash`; rebase only is `all`.
- **`files`** per component from its version file: `package.json` with updater `json` and key
  `version`, `Chart.yaml` with `helm`, `Cargo.toml` with `toml` and key `package.version`,
  `pyproject.toml` with `toml` and key `project.version`, or `tool.poetry.version` where the
  version sits under `[tool.poetry]` instead, a Go constant with `go` and the constant's name
  as key.

Done when every key the plan writes is either answered by the human or derived above.

### 3. Plan, then write

Show the complete `release-me.yaml`, the labels the repository lacks and the workflow, then
write after confirmation:

- `release-me.yaml` at the repository root, carrying only keys that differ from the defaults.
- The labels: every one `release-me.yaml` names, compared against
  `gh api repos/{owner}/{repo}/labels --paginate --jq '.[].name'`. Each missing one is listed
  for the human to create; labels are repository settings, and the skill creates none.
- `.github/workflows/release.yaml` from the README's action section: `push` to the default
  branch plus `pull_request` with types `labeled` and `unlabeled`, the job guard listing
  exactly the configured label names, a checkout of the base branch with `fetch-depth: 0`, and
  `ostendorf-cloud/release-me/action` pinned to the release's commit SHA with the version as a
  trailing comment.

Done when both files exist, the missing labels are handed over, and the guard's label list
equals the labels in `release-me.yaml`, name for name.

### 4. Prove

Run the pinned version locally, through mise:

```sh
mise exec ubi:ostendorf-cloud/release-me@<version> -- release-me check
mise exec ubi:ostendorf-cloud/release-me@<version> -- release-me next
```

- `check` passes. It needs the token in the environment; without one locally, report that the
  check is still open instead of calling the setup proven.
- `next` prints one line per component. A component with existing tags continues from its last
  one; one that reports its initial version despite such tags has the wrong prefix, and the run
  goes back to step 1.

Done when both outputs are shown to the human.

## Guardrails

- Hand the change over uncommitted; the human commits and pushes it.
- Labels and other repository settings are reported, never changed: the human creates the
  missing labels.
- Tags are release-me's from the first run on: the setup creates none.
