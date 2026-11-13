#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
# Unit tests for resolve-platform.sh.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${script_dir}/resolve-platform.sh"
# An explicit template, because macOS mktemp ignores TMPDIR without one
# and always lands in the system temporary directory.
work="$(mktemp -d "${TMPDIR:-/tmp}/resolve-platform.XXXXXXXX")"
trap 'rm -rf "${work}"' EXIT

failures=0

# run_case name runner_os runner_arch want_status want_output [want_stderr_substring]
run_case() {
  local case_name="$1" runner_os="$2" runner_arch="$3" want_status="$4" want_output="$5"
  local want_stderr_substring="${6:-}"
  local output_file status got_output

  output_file="${work}/github_output"
  : >"${output_file}"

  set +e
  RUNNER_OS="${runner_os}" RUNNER_ARCH="${runner_arch}" GITHUB_OUTPUT="${output_file}" \
    "${script}" 2>"${work}/stderr"
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

  if [ -n "${want_stderr_substring}" ] && ! grep -qF "${want_stderr_substring}" "${work}/stderr"; then
    echo "FAIL ${case_name}: stderr = $(cat "${work}/stderr"), want it to contain ${want_stderr_substring@Q}"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

run_case "a Linux X64 runner maps to linux amd64" \
  "Linux" "X64" 0 "os=linux
arch=amd64"

run_case "a macOS ARM64 runner maps to darwin arm64" \
  "macOS" "ARM64" 0 "os=darwin
arch=arm64"

run_case "a macOS X64 runner maps to darwin amd64" \
  "macOS" "X64" 0 "os=darwin
arch=amd64"

run_case "a Linux ARM64 runner maps to linux arm64" \
  "Linux" "ARM64" 0 "os=linux
arch=arm64"

run_case "an unsupported runner os fails with a clear reason" \
  "Windows" "X64" 1 "" "unsupported runner os Windows"

run_case "an unsupported runner arch fails with a clear reason" \
  "Linux" "X86" 1 "" "unsupported runner arch X86"

if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

echo "all tests passed"
