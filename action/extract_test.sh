#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
# Unit tests for extract.sh. Every fixture is a real tar.gz built with the
# tar command itself, never a hand crafted archive.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${script_dir}/extract.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

failures=0

# run_case name runner_temp os arch want_status
run_case() {
  local case_name="$1" runner_temp="$2" os="$3" arch="$4" want_status="$5"
  local status

  set +e
  OS="${os}" ARCH="${arch}" RUNNER_TEMP="${runner_temp}" \
    "${script}" >"${work}/stdout" 2>"${work}/stderr"
  status=$?
  set -e

  if { [ "${want_status}" = "nonzero" ] && [ "${status}" -eq 0 ]; } \
    || { [ "${want_status}" != "nonzero" ] && [ "${status}" -ne "${want_status}" ]; }; then
    echo "FAIL ${case_name}: exit status = ${status}, want ${want_status}"
    echo "  stderr: $(cat "${work}/stderr")"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

# good is a runner temp directory holding a real archive whose single
# member is named "release-me", the shape goreleaser's own archives take.
good="${work}/good"
mkdir -p "${good}/src"
printf '#!/bin/sh\necho release-me\n' >"${good}/src/release-me"
tar -czf "${good}/release-me_linux_amd64.tar.gz" -C "${good}/src" release-me

run_case "a valid archive extracts the binary and makes it executable" \
  "${good}" "linux" "amd64" 0
if [ ! -x "${good}/release-me" ]; then
  echo "FAIL a valid archive extracts the binary and makes it executable: release-me is missing or not executable"
  failures=$((failures + 1))
fi

# wrong_member is a real archive, but its one member is not named
# "release-me": the shape a build misconfiguration would leave behind.
wrong_member="${work}/wrong-member"
mkdir -p "${wrong_member}/src"
printf 'not a binary\n' >"${wrong_member}/src/other-file"
tar -czf "${wrong_member}/release-me_linux_amd64.tar.gz" -C "${wrong_member}/src" other-file

run_case "an archive without a release-me member fails with a non-zero exit" \
  "${wrong_member}" "linux" "amd64" nonzero

# archive_missing has no archive at all: the shape a failed or skipped
# download leaves behind.
archive_missing="${work}/archive-missing"
mkdir -p "${archive_missing}"

run_case "a missing archive fails with a non-zero exit" \
  "${archive_missing}" "linux" "amd64" nonzero

if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

echo "all tests passed"
