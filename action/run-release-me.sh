#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
# Runs release-me with the inputs mapped to flags, and fills the action's
# "tags" output from every "released-tag: <tag>" line it prints: the tags
# this run tagged a release for, one per released component. With
# draft-until-assets-attach off, the default, each one already published;
# on, each is still a draft, so a caller attaches assets and publishes
# those releases itself.
set -euo pipefail

args=(run)
if [ -n "${COMPONENT}" ]; then
  args+=(--component "${COMPONENT}")
fi
if [ "${DRY_RUN}" = "true" ]; then
  args+=(--dry-run)
fi
if [ "${VERBOSE}" = "true" ]; then
  args+=(--verbose)
fi

output_file="${RUNNER_TEMP}/release-me-run-output.txt"
"${RUNNER_TEMP}/release-me" "${args[@]}" | tee "${output_file}"

{
  echo "tags<<RELEASE_ME_TAGS_EOF"
  sed -n 's/^released-tag: //p' "${output_file}"
  echo "RELEASE_ME_TAGS_EOF"
} >>"${GITHUB_OUTPUT}"
