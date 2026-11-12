# Contributing

Thank you for considering a contribution to release-me.

## Proposing a change

Open an issue before starting a non-trivial change. Describe the problem and the proposed
approach, and wait for discussion before opening a pull request.

A trivial fix, such as a typo or a small documentation clarification, can skip straight to a
pull request.

## Commit and pull request conventions

This repository's own release tooling parses commit subjects and pull request titles as
Conventional Commits. A badly formed title breaks changelog generation for the change it
belongs to.

- Commit subjects and pull request titles follow
  [Conventional Commits](https://www.conventionalcommits.org).
- Branch names follow `<type>/<subject>`, for example `fix/123-nil-pointer` or
  `docs/129-community-health`.

## Developer Certificate of Origin

Every commit needs a `Signed-off-by` trailer. Add it with:

```sh
git commit -s
```

The trailer certifies the text below, the Developer Certificate of Origin:

```text
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.


Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Signing off means the contributor certifies they wrote the change, or otherwise have the
right to submit it under this project's license, Apache-2.0. It is required because it gives
the project a clear chain of provenance for every contribution.

CI enforces this. The `dco` workflow checks every commit of a pull request and fails on one
whose trailer is missing, or whose name and address match neither the commit's author nor its
committer. Merge commits and commits authored by a bot are skipped, since neither can sign
off. The failing check names every commit it rejected and how to fix it: amend the most
recent one with `git commit --amend --signoff`, or several with
`git rebase --signoff <base branch>`, then push with `--force-with-lease`.

## Local development

```sh
task setup   # mise install, the pinned toolchain
task check   # every check this repository runs in CI, one after another
```

`task --list` shows the individual targets, `lint:*`, `test:*` and `build`, if you want to
run just one.

## Design decisions

`docs/adr/` records the reasoning behind an existing design decision. Read it before
proposing something that conflicts with one.
