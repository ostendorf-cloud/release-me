#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
# Unit tests for publish-release-assets.sh. Fake git, goreleaser and gh
# binaries stand in for the real tools, logging every call they receive, so
# these run offline and never touch a real repository or release.
# shellcheck disable=SC2016  # the fake binaries below are written with $1,
# $3 and ${CALL_LOG} deliberately unexpanded, so the written script expands
# them itself when it runs, not this test when it writes it.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${script_dir}/publish-release-assets.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

fake_bin="${work}/fake-bin"
mkdir -p "${fake_bin}"

# fake-bin/git only logs the call: publish-release-assets.sh only ever
# calls "git fetch", never anything that needs a real repository.
{
  echo '#!/usr/bin/env bash'
  echo 'echo "git $*" >>"${CALL_LOG}"'
} >"${fake_bin}/git"
chmod +x "${fake_bin}/git"

# fake-bin/goreleaser logs the call and drops the archive and checksum
# files publish-release-assets.sh globs for, named like the real
# .goreleaser.yaml would for a build of "fixture" at version 1.0.0.
{
  echo '#!/usr/bin/env bash'
  echo 'echo "goreleaser $*" >>"${CALL_LOG}"'
  echo 'mkdir -p dist'
  echo 'echo archive >dist/fixture_linux_amd64.tar.gz'
  echo 'echo checksum >dist/fixture_1.0.0_checksums.txt'
} >"${fake_bin}/goreleaser"
chmod +x "${fake_bin}/goreleaser"

# fake-bin/gh logs every call. "release view" answers isDraft from the tag
# name itself: "-draft" or "-published" need no separate state file,
# "-view-fails" simulates a release GitHub cannot read back, a deleted
# release, an expired token, a 5xx or a rate limit alike. Anything else it
# does not recognize fails loudly, to catch a call the script was not
# supposed to make.
{
  echo '#!/usr/bin/env bash'
  echo 'echo "gh $*" >>"${CALL_LOG}"'
  echo 'if [ "$1 $2" = "release view" ]; then'
  echo '  case "$3" in'
  echo '  *-draft) echo true ;;'
  echo '  *-published) echo false ;;'
  echo '  *-view-fails) echo "fake gh: release view failed" >&2; exit 1 ;;'
  echo '  *) echo "fake gh: unknown tag $3" >&2; exit 1 ;;'
  echo '  esac'
  echo '  exit 0'
  echo 'fi'
  echo 'case "$1 $2" in'
  echo '"release upload"|"release edit") exit 0 ;;'
  echo '*) echo "fake gh: unexpected call: $*" >&2; exit 1 ;;'
  echo 'esac'
} >"${fake_bin}/gh"
chmod +x "${fake_bin}/gh"

export CALL_LOG
export PATH="${fake_bin}:${PATH}"

failures=0

# run_case name tags want_calls
run_case() {
  local case_name="$1" tags="$2" want_calls="$3"
  CALL_LOG="${work}/calls.log"
  : >"${CALL_LOG}"

  # A fresh, empty build directory each time: the real script runs from
  # the repository checkout, drops "dist" into its current directory, and
  # must never see a previous case's leftovers.
  local build_dir="${work}/build"
  rm -rf "${build_dir}"
  mkdir -p "${build_dir}"

  if ! (cd "${build_dir}" && TAGS="${tags}" "${script}") >"${work}/stdout" 2>"${work}/stderr"; then
    echo "FAIL ${case_name}: exit status = $?, want 0"
    echo "stderr: $(cat "${work}/stderr")"
    failures=$((failures + 1))
    return
  fi

  local got_calls
  got_calls="$(cat "${CALL_LOG}")"
  if [ "${got_calls}" != "${want_calls}" ]; then
    echo "FAIL ${case_name}: calls = ${got_calls@Q}, want ${want_calls@Q}"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

# run_failure_case name tags want_stderr_substring
run_failure_case() {
  local case_name="$1" tags="$2" want_stderr_substring="$3"
  CALL_LOG="${work}/calls.log"
  : >"${CALL_LOG}"

  local build_dir="${work}/build"
  rm -rf "${build_dir}"
  mkdir -p "${build_dir}"

  local status
  if (cd "${build_dir}" && TAGS="${tags}" "${script}") >"${work}/stdout" 2>"${work}/stderr"; then
    status=0
  else
    status=$?
  fi
  if [ "${status}" -eq 0 ]; then
    echo "FAIL ${case_name}: exit status = 0, want non-zero"
    failures=$((failures + 1))
    return
  fi

  if ! grep -qF "${want_stderr_substring}" "${work}/stderr"; then
    echo "FAIL ${case_name}: stderr = $(cat "${work}/stderr"), want it to contain ${want_stderr_substring@Q}"
    failures=$((failures + 1))
    return
  fi

  # The failure must stop the script before it ever builds, uploads or
  # publishes anything for the tag it could not read: treating an unread
  # release as "nothing to do" would otherwise leave a draft with no
  # assets and a workflow run reporting a false success.
  local got_calls
  got_calls="$(cat "${CALL_LOG}")"
  if printf '%s' "${got_calls}" | grep -qE "goreleaser|release upload|release edit"; then
    echo "FAIL ${case_name}: calls = ${got_calls@Q}, want it to stop before building, uploading or publishing"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

run_case "nothing released makes no call at all" "" ""

run_case "a draft release is built, attached and published" "v1.0.0-draft" \
  "git fetch origin --tags --force
gh release view v1.0.0-draft --json isDraft --jq .isDraft
goreleaser release --clean --skip=announce,validate,publish
gh release upload v1.0.0-draft dist/fixture_linux_amd64.tar.gz dist/fixture_1.0.0_checksums.txt --clobber
gh release edit v1.0.0-draft --draft=false"

run_case "an already published release is left alone" "v1.0.0-published" \
  "git fetch origin --tags --force
gh release view v1.0.0-published --json isDraft --jq .isDraft"

run_case "every tag in a multi-line TAGS is handled" "v1.0.0-draft
v2.0.0-published" \
  "git fetch origin --tags --force
gh release view v1.0.0-draft --json isDraft --jq .isDraft
goreleaser release --clean --skip=announce,validate,publish
gh release upload v1.0.0-draft dist/fixture_linux_amd64.tar.gz dist/fixture_1.0.0_checksums.txt --clobber
gh release edit v1.0.0-draft --draft=false
gh release view v2.0.0-published --json isDraft --jq .isDraft"

run_failure_case \
  "a gh release view failure stops the script instead of being treated as already published" \
  "v1.0.0-view-fails" "cannot read the release for v1.0.0-view-fails"

if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

echo "all tests passed"
