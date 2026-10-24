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

while IFS= read -r tag; do
  if [ -z "${tag}" ]; then
    continue
  fi

  if [ "$(gh release view "${tag}" --json isDraft --jq .isDraft)" != "true" ]; then
    echo "${tag} is already published, assets must already be attached"
    continue
  fi

  echo "building archives for ${tag}"
  rm -rf dist
  GORELEASER_CURRENT_TAG="${tag}" goreleaser release --clean --skip=announce,validate,publish

  echo "attaching assets to the draft release ${tag}"
  gh release upload "${tag}" dist/*.tar.gz dist/*checksums.txt --clobber

  echo "publishing ${tag}"
  gh release edit "${tag}" --draft=false
done <<<"${TAGS}"
