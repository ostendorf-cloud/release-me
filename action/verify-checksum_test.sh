#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
# Unit tests for verify-checksum.sh. A fixture's checksums line always
# carries the real digest of its archive's real content, computed by
# sha256_of below, never a hand typed one. One case also exercises the
# "." escaping the script applies to the archive name before it reaches
# grep: a decoy line that differs from the real name only by underscores
# where the real name has dots must never be picked over the correct line.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${script_dir}/verify-checksum.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

# sha256_of hashes a file the same way verify-checksum.sh picks its tool,
# so a fixture's checksums line is always the real digest of its archive.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d ' ' -f 1
  else
    shasum -a 256 "$1" | cut -d ' ' -f 1
  fi
}

failures=0

# run_case name runner_temp os arch version want_status [want_stdout_substring]
run_case() {
  local case_name="$1" runner_temp="$2" os="$3" arch="$4" version="$5" want_status="$6"
  local want_stdout_substring="${7:-}"
  local status

  set +e
  OS="${os}" ARCH="${arch}" VERSION="${version}" RUNNER_TEMP="${runner_temp}" \
    "${script}" >"${work}/stdout" 2>"${work}/stderr"
  status=$?
  set -e

  if { [ "${want_status}" = "nonzero" ] && [ "${status}" -eq 0 ]; } \
    || { [ "${want_status}" != "nonzero" ] && [ "${status}" -ne "${want_status}" ]; }; then
    echo "FAIL ${case_name}: exit status = ${status}, want ${want_status}"
    echo "  stdout: $(cat "${work}/stdout")"
    echo "  stderr: $(cat "${work}/stderr")"
    failures=$((failures + 1))
    return
  fi

  if [ -n "${want_stdout_substring}" ] && ! grep -qF "${want_stdout_substring}" "${work}/stdout"; then
    echo "FAIL ${case_name}: stdout = $(cat "${work}/stdout"), want it to contain ${want_stdout_substring@Q}"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

# good is a runner temp directory holding a real archive and a checksums
# file whose line carries that archive's real digest.
good="${work}/good"
mkdir -p "${good}"
echo "fixture archive content for linux amd64" >"${good}/release-me_linux_amd64.tar.gz"
printf '%s  release-me_linux_amd64.tar.gz\n' \
  "$(sha256_of "${good}/release-me_linux_amd64.tar.gz")" \
  >"${good}/release-me_1.2.3_checksums.txt"

run_case "a matching checksum verifies and exits zero" \
  "${good}" "linux" "amd64" "1.2.3" 0 "OK"

# bad_hash carries a real archive but a checksums line whose digest
# belongs to different content, the shape a corrupted download takes.
bad_hash="${work}/bad-hash"
mkdir -p "${bad_hash}"
echo "fixture archive content for linux amd64" >"${bad_hash}/release-me_linux_amd64.tar.gz"
echo "a different payload entirely" >"${bad_hash}/decoy-content"
printf '%s  release-me_linux_amd64.tar.gz\n' \
  "$(sha256_of "${bad_hash}/decoy-content")" \
  >"${bad_hash}/release-me_1.2.3_checksums.txt"

run_case "a mismatched checksum fails with a non-zero exit" \
  "${bad_hash}" "linux" "amd64" "1.2.3" nonzero "FAILED"

# archive_missing_from_checksums has the real archive on disk, but the
# checksums file only lists other platforms' archives, never this one:
# the shape of a checksums file built for a release that never published
# this OS/ARCH combination.
missing_entry="${work}/archive-missing-from-checksums"
mkdir -p "${missing_entry}"
echo "fixture archive content for linux amd64" >"${missing_entry}/release-me_linux_amd64.tar.gz"
{
  echo "0000000000000000000000000000000000000000000000000000000000000000  release-me_windows_amd64.zip"
  echo "1111111111111111111111111111111111111111111111111111111111111111  release-me_darwin_arm64.tar.gz"
} >"${missing_entry}/release-me_1.2.3_checksums.txt"

run_case "the archive missing from the checksums file fails with a non-zero exit" \
  "${missing_entry}" "linux" "amd64" "1.2.3" nonzero

# checksums_missing has the archive on disk but no checksums file at all,
# the shape a partial or failed download leaves behind.
checksums_missing="${work}/checksums-file-missing"
mkdir -p "${checksums_missing}"
echo "fixture archive content for linux amd64" >"${checksums_missing}/release-me_linux_amd64.tar.gz"

run_case "a missing checksums file fails with a non-zero exit" \
  "${checksums_missing}" "linux" "amd64" "1.2.3" nonzero

# archive_missing_on_disk lists the archive in the checksums file, with
# its real digest, but the archive itself was never downloaded.
archive_missing="${work}/archive-missing-on-disk"
mkdir -p "${archive_missing}"
echo "fixture archive content for linux amd64" >"${work}/archive-missing-on-disk-reference"
printf '%s  release-me_linux_amd64.tar.gz\n' \
  "$(sha256_of "${work}/archive-missing-on-disk-reference")" \
  >"${archive_missing}/release-me_1.2.3_checksums.txt"

run_case "the archive missing from disk fails with a non-zero exit" \
  "${archive_missing}" "linux" "amd64" "1.2.3" nonzero

# multi_line lists several archives, with a decoy line right before the
# real one: the decoy's name differs from the real archive only by
# underscores standing where the real name has dots, and it carries a
# wrong digest. An unescaped pattern would treat those dots as "match any
# character", let the decoy line through too and fail the verification
# sha256sum/shasum then does across both lines.
multi_line="${work}/multi-line-checksums"
mkdir -p "${multi_line}"
echo "fixture archive content for linux amd64" >"${multi_line}/release-me_linux_amd64.tar.gz"
{
  echo "2222222222222222222222222222222222222222222222222222222222222222  release-me_linux_amd64_tar_gz"
  printf '%s  release-me_linux_amd64.tar.gz\n' "$(sha256_of "${multi_line}/release-me_linux_amd64.tar.gz")"
  echo "3333333333333333333333333333333333333333333333333333333333333333  release-me_darwin_arm64.tar.gz"
} >"${multi_line}/release-me_1.2.3_checksums.txt"

run_case "a checksums file listing several archives still uses only the matching line" \
  "${multi_line}" "linux" "amd64" "1.2.3" 0 "OK"

if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

echo "all tests passed"
