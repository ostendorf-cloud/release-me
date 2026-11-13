#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
# Unit tests for resolve-version.sh, including a consumer pinned by commit
# digest: github.action_ref is then the 40 character sha, never a release
# tag, so resolution must fall back to the VERSION file shipped at that
# commit instead of failing.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
resolver="${script_dir}/resolve-version.sh"
# An explicit template, because macOS mktemp ignores TMPDIR without one
# and always lands in the system temporary directory.
work="$(mktemp -d "${TMPDIR:-/tmp}/resolve-version.XXXXXXXX")"
trap 'rm -rf "${work}"' EXIT

# with-version/action is a sibling of a VERSION file, the shape the action
# ships in a consumer's checkout at a tagged commit. no-version/action has
# no such sibling, the shape a commit before this fix shipped.
mkdir -p "${work}/with-version/action" "${work}/no-version/action"
echo "0.1.1 # release-me: version" >"${work}/with-version/VERSION"

failures=0

# run_case name input_version action_ref action_path want_status want_output
run_case() {
  local case_name="$1" input_version="$2" action_ref="$3" action_path="$4"
  local want_status="$5" want_output="$6"
  local output_file status got_output

  output_file="${work}/github_output"
  : >"${output_file}"

  set +e
  INPUT_VERSION="${input_version}" \
    ACTION_REF="${action_ref}" \
    GITHUB_ACTION_PATH="${action_path}" \
    GITHUB_OUTPUT="${output_file}" \
    "${resolver}" 2>"${work}/stderr"
  status=$?
  set -e

  if [ "${status}" -ne "${want_status}" ]; then
    echo "FAIL ${case_name}: exit status = ${status}, want ${want_status}"
    echo "  stderr: $(cat "${work}/stderr")"
    failures=$((failures + 1))
    return
  fi

  got_output="$(cat "${output_file}")"
  if [ "${got_output}" != "${want_output}" ]; then
    echo "FAIL ${case_name}: GITHUB_OUTPUT = ${got_output@Q}, want ${want_output@Q}"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

run_case "an explicit version input wins over everything else" \
  "0.2.0" "d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0" "${work}/with-version/action" \
  0 "version=0.2.0"

run_case "an explicit source input wins over everything else" \
  "source" "d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0" "${work}/with-version/action" \
  0 "version=source"

run_case "a floating tag pin resolves from the action ref alone" \
  "" "v0.1.1" "${work}/no-version/action" \
  0 "version=0.1.1"

run_case "a commit digest pin falls back to the shipped VERSION file" \
  "" "d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0" "${work}/with-version/action" \
  0 "version=0.1.1"

run_case "a branch pin with no VERSION file fails with a clear reason" \
  "" "main" "${work}/no-version/action" \
  1 ""
if ! grep -q "is not a release tag" "${work}/stderr"; then
  echo "FAIL a branch pin with no VERSION file fails with a clear reason: stderr does not name the reason"
  failures=$((failures + 1))
fi

run_case "no version input and no action ref fails with a clear reason" \
  "" "" "${work}/no-version/action" \
  1 ""
if ! grep -q "no version input and no action ref" "${work}/stderr"; then
  echo "FAIL no version input and no action ref fails with a clear reason: stderr does not name the reason"
  failures=$((failures + 1))
fi

run_case "an invalid explicit version input fails instead of falling back" \
  "not-a-version" "v0.1.1" "${work}/with-version/action" \
  1 ""
if ! grep -q "not-a-version" "${work}/stderr"; then
  echo "FAIL an invalid explicit version input fails instead of falling back: stderr does not name the value"
  failures=$((failures + 1))
fi

if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

echo "all tests passed"
