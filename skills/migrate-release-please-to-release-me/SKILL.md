---
name: migrate-release-please-to-release-me
description: >-
  Moves a repository from release-please to release-me without losing a tag or a changelog
  line: translate with release-me migrate, settle every warning, swap the workflow, and prove
  release-me continues from the last release. Use when the user wants to migrate, switch or move
  from release-please to release-me.
---

# migrate-release-please-to-release-me

A **lossless** switch: after it, release-me continues every component from the tag
release-please last cut, writes into the changelog release-please wrote, and nothing that
consumed release-please's results is left waiting for a signal that never comes.
`release-me migrate release-please` does the translation; this skill owns everything around it.
Read the references at the pinned release, never `main`, and take them over the README, which
only summarises them:
`https://github.com/ostendorf-cloud/release-me/blob/v<version>/docs/configuration.md` for every
`release-me.yaml` key, `.../docs/action.md` for every action input and output, and
`.../docs/commands.md#migrate` for what `migrate` translates, sets regardless and leaves out.

## When to use

The repository releases with release-please, a `release-please-config.json` at its root, and
the user wants release-me to take over. A repository with no release tool goes to the
`setup-release-me` skill instead.

## Steps

### 1. Survey

Read the repository; every row below comes from a command or a file.

- **release-please files:** `release-please-config.json` and `.release-please-manifest.json`.
  Without the config the repository has nothing to migrate, and the run hands over to the
  `setup-release-me` skill, an optional call: where it is not installed, the run ends here and
  says the repository has no release-please setup.
- **Workflows:** every file under `.github/workflows/` running
  `googleapis/release-please-action`, or calling a reusable workflow that does.
- **Consumers of release-please's results:** every job or step reading its outputs
  (`releases_created`, `release_created`, `tag_name`, `paths_released`, `<path>--release_created`
  and the like), its labels, or its release pull request branch `release-please--*`. Step 4
  maps each output through the table there; the labels and the branch have no output.
- **Merge methods:** which of merge commit, squash and rebase the repository allows. Step 3
  needs them to settle `changelog-source`, which the translation sets without asking.

  ```sh
  gh api graphql -F owner='{owner}' -F name='{repo}' --jq .data.repository -f query='
    query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {
      mergeCommitAllowed squashMergeAllowed rebaseMergeAllowed } }'
  ```

  GraphQL, because the REST repository endpoint returns the merge fields only to a token with
  push access, the same reason the `setup-release-me` skill asks this way.

- **Tagging steps of its own:** every workflow step creating a tag, `git tag`, `gh api`
  `git/refs` or an action doing it, beside the release-please step. A repository with
  `skip-github-release` has one, and release-me tags the merge commit itself.
- **Open release pull request:** `gh pr list --label "autorelease: pending" --state open`. One
  that is open is merged or closed before the switch lands, so no release is half cut.
- **Version to pin:** `gh api repos/ostendorf-cloud/release-me/releases/latest --jq .tag_name`.
  Without a release, a 404, there is nothing to pin, and the run ends with that finding.
- **An earlier attempt:** a `release-me.yaml` already at the root is named in the plan; step 4
  replaces it only after the human confirms.

Done when the survey lists both files, every release-please workflow, every consumer with its
file and line, the merge methods, every tagging step of the repository's own, the open release
pull request or its absence, the pinned version, and an existing `release-me.yaml` or its
absence.

### 2. Translate

Nothing is written into the repository yet: the translation and its warnings go to the
temporary directory, from any directory of the repository, since `migrate` finds the root
itself.

```sh
mise exec ubi:ostendorf-cloud/release-me@<version> -- release-me migrate release-please \
  > "${TMPDIR:-/tmp}/release-me-migrate.yaml" 2> "${TMPDIR:-/tmp}/release-me-migrate-warnings.txt"
```

Done when the command exited 0 and both files exist. On a non-zero exit, show the warnings file
to the human as it stands: it holds the warnings, the error naming the file or key that could
not be translated, and, for a translation release-me refuses, that translation to correct by
hand.

### 3. Settle every warning

Every `warning:` line in the warnings file gets a row in a table: the warning, the action
taken, and the reason. An action is one of: a change to the translated `release-me.yaml` in the
temporary directory (a file entry, a key), a change elsewhere in the repository, or an
acceptance the human confirms. A **missing tag** is always the human's: release-me would start
that component over at its initial version, and the choices are creating the tag at the commit
that release shipped from, or accepting the restart.

The items below need a row of their own too. The first two change behaviour silently, with no
warning naming them, and are what a migration gets wrong.

**Changelog source** is the largest single change the translation makes. `migrate` writes
`changelog-source: all` whatever the release-please configuration says, because release-please
reads every commit since the last release rather than the first parent history. Every commit
then becomes its own changelog entry, where release-me's own default gives one entry per
merged pull request, its title as the text. Ask the human once, with the repository's merge
methods in front of them: `first-parent` for a repository that merges with merge commits,
`squash` where it squashes, `all` to keep exactly what release-please produced. Whatever is
chosen goes in the row, so the decision is visible rather than inherited.

**Tags with `skip-github-release`** double up. release-please created no tag for a package with
that key, so such a repository carries a tagging step of its own in its workflow. release-me
tags the merge commit even with the key on. The row names that step and removes it; left in, the
migrated repository tags the same version twice, the second one failing or moving a tag someone
already consumed. `force-tag-creation: false` beside the key is refused when `release-me.yaml`
loads, since nothing would ever create the tag.

**`force-tag-creation`** defaults the other way round: release-please's default is `false`,
release-me's is `true`, and `migrate` keeps release-me's where no package sets the key. Without
drafts both create the tag during the release run and nothing changes. With
`github-release-draft` or `github-release-draft-until-assets-attach`, release-me's tag exists
while the draft waits, where release-please's appeared only when the release was published. Keep
`true` where a later step checks out the tag; name the difference in the row either way.

**Workspaces** come from a `node-workspace`, `cargo-workspace` or `maven-workspace` plugin and
override the release pull request choice below: a dependent releases in the same pull request
as its dependency, so a `separate-pull-requests: true` beside such a plugin still becomes
`release-pull-request: shared`, warned about. Each plugin option is warned about separately;
`always-link-local: false` carries over only beside a `node-workspace` plugin, and a single
package has no workspace dependency to move, so the key is left out with a warning. `workspaces`
moves manifest dependencies only, never a lockfile: list each lockfile the old setup bumped
under the dependent's `files`, with `npm-lock`, `cargo-lock` or `gemfile-lock`.

**`versioning-strategy`** is release-please's `versioning` key, renamed because release-me's own
`versioning` names how a family's versions relate. `always-bump-patch`, `always-bump-minor` and
`always-bump-major` mean what they did. The two warned-about values need a decision:

| Value | What it means in release-me |
| --- | --- |
| `service-pack` | Maintenance releases of the last version without moving it, `1.2.3-sp.1` then `1.2.3-sp.2`. Semver reads that as a pre-release, so the GitHub release is one, the major tag stays put and the next stable version still counts from `1.2.3`. It works with `versioning: independent` only, and a pre-release label on its release pull request is refused, since `sp.N` is already that version's pre-release identifier. |
| `dependency-manifest` | The bump rises to the largest dependency update among the changes, whatever their type. release-please read the update from a Java manifest; release-me reads it from the pull request title Dependabot or Renovate writes, so `chore(deps): bump react from 18.3.1 to 19.0.0` releases a major. A repository whose dependency pull requests carry another title shape gets ordinary bumps instead, and the row says so. |

**Versioning** is the human's too, asked whenever there is more than one component, since no
warning names it: the translation is `independent`, or `locked` for a `linked-versions` group
over every component, and either is carried over silently. Offer the translated mode first,
then the others, `linked` among them: one shared version line where only changed components
release, which release-please has no equivalent for.

**Release pull requests** follow release-please's `separate-pull-requests`, also silently:
left out or `false`, its default, translates to `release-pull-request: shared`, one release pull
request for every package as before; `true` keeps release-me's default of one per component.
`locked` always shares one. A shared title names every component and version and must fit
GitHub's 256 characters, about eight 20-character names, or `pr` refuses it: with more packages,
recommend `per-component`. Confirm the translated choice with the human alongside the
versioning mode. A shared body gives each component a `## <name>` heading by default, where
release-please collapsed each package into a `<details>` block: `migrate` leaves
`release-pull-request-layout` out, add `release-pull-request-layout: details` only when the
human wants the collapsed look kept. Every release pull request body also opens with a
collapsed "How this release works" guide release-please never rendered: `migrate` leaves
`release-pull-request-guide` out, add `release-pull-request-guide: false` only when the human
wants the body without it.
`migrate` carries `pull-request-title-pattern` and
`group-pull-request-title-pattern` over as `release-pull-request-title` and
`release-pull-request-shared-title`; one it left out lacked a placeholder release-me reads the
title back with, and the row offers the default title or a pattern carrying them.

Its `pull-request-header` and `pull-request-footer` carry over as
`release-pull-request-header` and `release-pull-request-footer`.

A **root package** beside other packages, package `.`, translates to a root component with a
warning: it owns only the files outside the other components' paths, where release-please's
root package counted every commit, so a change inside a package no longer releases the root.
Its row names the packages whose changes the root stops picking up, and the human accepts that
reach.

**Changelog type** carries over silently too: release-please's `changelog-type: github`
translates to release-me's, GitHub's generated release notes in place of the entries.

**Release branches** are deleted once released: release-please leaves its
`release-please--*` branch behind, release-me's `release` deletes `release-me/*` after
tagging what merged from it. release-please has no such key, so the translation leaves
`delete-release-branch` at its default, `true`; mention the difference, and set it to `false`
only when the human wants the branches kept.

Its `signoff` carries over as `signoff`.

Its `draft-pull-request` carries over as `release-pull-request-draft`.

**Changelog style** carries over silently too, so name each one to the human with the
versioning mode and let a different choice be a deliberate one:

- `changelog-commit-links` and `changelog-issue-references` are turned on, the two links
  release-please's default changelog carries.
- `include-commit-authors` becomes `changelog-authors`, but only when every package sets it:
  release-me credits authors for every component or for none, and a value only some packages
  carry is warned about and left out.
- a `sentence-case` plugin becomes `changelog-sentence-case`; its `specialWords` option is
  warned about, since release-me keeps `gRPC`, `npm` and any first word already holding an
  upper case letter as written.
- `changelog-sections` and `changelog-host` carry over as they are, the host only when every
  package names the same one, since release-me links every component on one host.

Done when the table has one row per warning line plus a row for each item above, every row has
an action, the human has chosen the changelog source and accepted what happens to the
repository's own tagging steps, and with more than one component has chosen the versioning mode
and the release pull requests.

### 4. Plan, then write

Show the final `release-me.yaml`, the table, and the workflow diff, then change after
confirmation:

- **Configuration:** the settled translation copied from the temporary directory to
  `release-me.yaml` at the repository root.
- **Workflow:** replace the release-please step with `ostendorf-cloud/release-me/action` as
  `docs/action.md`'s "Triggering the workflow" section shows it: `push` to the default branch
  plus `pull_request` with types
  `labeled` and `unlabeled`, a job guard listing exactly the pending, tagged and pre-release
  labels in `release-me.yaml`, `labels.extra` left out, a
  checkout of the base branch with `fetch-depth: 0`, the action pinned to the release's commit
  SHA with the version as a trailing comment.
- **Consumers:** each one from step 1 rewired to a signal release-me does produce: one of the
  action's outputs through the table below, a pushed tag matching the component's
  `tag-prefix`, a published GitHub release, or a check against the package registry. The
  action step needs an `id`, `release` below. Outputs are keyed by component name, never by
  path; `<name>` is the component's key in `release-me.yaml`, `main` for the implicit one,
  and a name with a `-` takes the index form `components)['duxt-ui'].released`.

  | release-please-action output | release-me action output |
  | --- | --- |
  | `releases_created`, `release_created` | `steps.release.outputs.released` |
  | `<path>--release_created` | `fromJSON(steps.release.outputs.components).<name>.released` |
  | `tag_name`, `<path>--tag_name` | `fromJSON(steps.release.outputs.components).<name>.tag` |
  | `version`, `<path>--version` | `fromJSON(steps.release.outputs.components).<name>.version` |
  | `major`, `minor`, `patch`, `<path>--major` and the like | `fromJSON(steps.release.outputs.components).<name>.major`, `.minor`, `.patch` |
  | `sha`, `body`, `html_url`, `upload_url`, `<path>--sha` and the like | `fromJSON(steps.release.outputs.components).<name>.sha`, `.body`, `.html_url`, `.upload_url` |
  | `draft`, `<path>--draft` | `fromJSON(steps.release.outputs.components).<name>.draft` |
  | `name`, `<path>--name` | none: `gh release view <tag> --json name` in the consuming step |
  | `paths_released` | `steps.release.outputs.tags`, one tag name per released component; with `force-tag-creation: false` a draft's ref exists only after publish |
  | `pr` | `steps.release.outputs.pr`: `number`, `url`, `branch`, `components` |
  | `prs`, `prs_created` | `steps.release.outputs.prs`, `prs != '[]'` |
  | `id`, `<path>--id` | none: `gh release view <tag> --json databaseId` in the consuming step |

  `pr` is `null` with several release pull requests, where release-please gave the first;
  read `prs` there. Its JSON is release-me's own shape, not release-please's: a consumer
  reading `headBranchName`, `title` or `labels` off it reads `branch` or asks the API.
- **Labels:** release-please's labels carry over by name, every one of a comma separated
  `label`, `release-label` and `extra-label`. Each one `release-me.yaml` names and
  `gh api repos/{owner}/{repo}/labels --paginate --jq '.[].name'` lacks is listed for the human
  to create; labels are repository settings, and the skill creates none.
- **Removal:** `release-please-config.json`, `.release-please-manifest.json`, the
  release-please workflow steps and every tagging step of the repository's own that step 1
  found, since release-me tags the merge commit itself even with `skip-github-release` on.
  Changelogs stay, since release-me prepends to them. So do the
  `x-release-please-version` markers of every file `release-me.yaml` lists with the `generic`
  updater, which reads them as they are.

Done when `git grep -n release-please` returns only history, such as changelog entries and
release notes, and the markers of files listed with `generic`, and each remaining hit is named
in the plan as one of those.

### 5. Prove

```sh
mise exec ubi:ostendorf-cloud/release-me@<version> -- release-me check
mise exec ubi:ostendorf-cloud/release-me@<version> -- release-me next
mise exec ubi:ostendorf-cloud/release-me@<version> -- release-me changelog
```

- `check` passes. It needs the token in the environment; without one locally, report the check
  as still open instead of calling the migration proven.
- `next` continues every component from its last release-please tag. A component at its
  initial version without a confirmed restart from step 3 sends the run back to step 3.
- `changelog` lists only changes after that tag.

Done when the three outputs are shown to the human.

## Guardrails

- Hand the change over uncommitted; the human commits and pushes it, ideally with no release
  pull request of either tool open.
- Tags come from release-me, or from the human's explicit choice in step 3.
- Labels and other repository settings are reported, never changed.
