#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Builds the archives and checksums goreleaser already produces for this
# project, attaches them to the draft release release-me just created for
# each tag in TAGS, then publishes it.
#
# goreleaser never touches the GitHub release itself here (--skip=publish):
# its own lookup of an existing release by tag cannot see a draft one
# ("get a release by tag name" excludes drafts), confirmed live against
# this repository. Every change to the release instead goes through gh,
# confirmed live to work against a draft: "gh release upload" to attach
# assets, "gh release edit --draft=false" to publish. See
# docs/adr/0001-draft-releases-until-assets-attach.md.
#
# TAGS is release.yaml's own "tags" action output: one tag per line, empty
# when release-me released nothing this run.
set -euo pipefail

if [ -z "${TAGS}" ]; then
  echo "nothing released, nothing to attach"
  exit 0
fi

git fetch origin --tags --force

# Every tag gets checked out by hand before its build: the job's checkout
# step may sit on the base branch or a push's own commit, not necessarily
# the merge commit release-me tagged, in particular on the pull_request
# labeled/unlabeled trigger, and a locked family can tag more than one
# component from more than one merge commit in the same run. goreleaser
# must build the exact commit each tag names, never whatever the job
# happens to have checked out. original_ref is restored once every tag is
# done, by commit SHA rather than branch name: a push-triggered run leaves
# the checkout detached, with no branch name to restore.
original_ref="$(git rev-parse HEAD)"

while IFS= read -r tag; do
  if [ -z "${tag}" ]; then
    continue
  fi

  # A failing command substitution never trips "set -e" inside an "if" or
  # "while" condition, a well known shell pitfall: that exemption would
  # swallow a failure here too, treating a deleted release, an expired
  # token, a 5xx or a rate limit the same as "already published", and
  # exiting 0 having attached nothing. Reading the status into a variable
  # first, on its own statement, keeps "set -e" in effect.
  if ! draft="$(gh release view "${tag}" --json isDraft --jq .isDraft)"; then
    echo "cannot read the release for ${tag}" >&2
    exit 1
  fi
  case "${draft}" in
  true) ;;
  false)
    echo "${tag} is already published, assets must already be attached"
    continue
    ;;
  *)
    echo "unexpected isDraft value for ${tag}: ${draft}" >&2
    exit 1
    ;;
  esac

  echo "checking out ${tag} to build its exact source"
  git checkout --quiet --detach "${tag}"

  echo "building archives for ${tag}"
  GORELEASER_CURRENT_TAG="${tag}" goreleaser release --clean --skip=announce,validate,publish

  echo "attaching assets to the draft release ${tag}"
  gh release upload "${tag}" dist/*.tar.gz dist/*checksums.txt --clobber

  echo "publishing ${tag}"
  gh release edit "${tag}" --draft=false
done <<<"${TAGS}"

git checkout --quiet "${original_ref}"
