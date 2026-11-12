#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Unit tests for check-dco.sh. Every case but the two GitHub only ones is
# a real commit in a throwaway git repository, rendered into the shape of
# GitHub's "list commits on a pull request" response from the same fields
# the endpoint exposes: the raw message, the author and committer
# identities and the parent list. Real commits rather than hand written
# JSON, so the trailer under test is the one git itself writes for
# "git commit -s", and the identities are the ones git itself records.
# The two cases git cannot express, a commit authored by a GitHub bot
# account and a commit with no usable identity, are appended as JSON.
#
# These run offline and touch nothing outside the temporary directory.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${script_dir}/check-dco.sh"
# An explicit template, because macOS mktemp ignores TMPDIR without one
# and always lands in the system temporary directory.
work="$(mktemp -d "${TMPDIR:-/tmp}/check-dco.XXXXXXXX")"
trap 'rm -rf "${work}"' EXIT

failures=0
repo="${work}/repo"
mkdir -p "${repo}"

git -C "${repo}" init --quiet --initial-branch=main
git -C "${repo}" config user.name "Ada Lovelace"
git -C "${repo}" config user.email "ada@example.com"
git -C "${repo}" config commit.gpgsign false

# commit <message> writes a file, commits it with that message verbatim
# and echoes the new SHA. Verbatim because two cases below turn on
# whitespace git's default message cleanup would strip: an indented
# trailer and one with trailing spaces. A caller that needs a committer
# other than the author prefixes the call with GIT_COMMITTER_NAME and
# GIT_COMMITTER_EMAIL.
#
# Each commit touches a file of its own, named after a checksum of its
# message: every message here is unique, so no two commits write the same
# path, and the merge case below merges two branches that changed
# different files. A shared counter would not do, because a command
# substitution runs the function in a subshell and its increment never
# reaches the parent.
change_file() {
  printf '%s\n' "$1" >"${repo}/file-$(printf '%s' "$1" | cksum | tr -dc '0-9')"
  git -C "${repo}" add -A
}

commit() {
  change_file "$1"
  printf '%s\n' "$1" | git -C "${repo}" commit --quiet --cleanup=verbatim -F -
  git -C "${repo}" rev-parse HEAD
}

# The one case that must read exactly what git produces for a sign-off
# goes through "git commit -s" instead of a hand written trailer.
signed_commit() {
  change_file "$1"
  git -C "${repo}" commit --quiet --signoff -m "$1"
  git -C "${repo}" rev-parse HEAD
}

sha_signed="$(signed_commit "feat: a commit signed off with git commit -s")"
sha_missing="$(commit "feat: a commit with no trailer at all")"
sha_other_person="$(commit "feat: a commit signed off by somebody else

Signed-off-by: Grace Hopper <grace@example.com>")"
sha_mixed_case="$(commit "feat: a commit signed off in a different case

signed-off-by: ADA LOVELACE <ADA@EXAMPLE.COM>")"
sha_several="$(commit "feat: a commit carrying several trailers

Signed-off-by: Grace Hopper <grace@example.com>
Signed-off-by: Ada Lovelace <ada@example.com>")"
sha_indented="$(commit "feat: a commit whose trailer is indented

  Signed-off-by: Ada Lovelace <ada@example.com>")"
sha_trailing_space="$(commit "feat: a commit whose trailer has trailing whitespace

Signed-off-by: Ada Lovelace <ada@example.com>   ")"
sha_name_only="$(commit "feat: a commit signed off without an address

Signed-off-by: Ada Lovelace")"

# A commit whose committer differs from its author, the shape a rebase or
# a commit through the GitHub web interface produces. The trailer matches
# the author, which is whose certificate a sign-off is, so it passes.
sha_rebased="$(GIT_COMMITTER_NAME="GitHub" GIT_COMMITTER_EMAIL="noreply@github.com" \
  commit "feat: a commit committed by somebody else

Signed-off-by: Ada Lovelace <ada@example.com>")"

# The same shape, but the trailer pairs the author's name with the
# committer's address. The GitHub DCO app accepts that combination,
# because it compares the two fields against two separate sets;
# check-dco.sh requires both to come from one identity, so it fails.
sha_crossed="$(GIT_COMMITTER_NAME="GitHub" GIT_COMMITTER_EMAIL="noreply@github.com" \
  commit "feat: a commit whose trailer crosses two identities

Signed-off-by: Ada Lovelace <noreply@github.com>")"

# A merge commit, unsigned: this repository's history is full of them,
# one per merged pull request, and none can carry a trailer.
git -C "${repo}" checkout --quiet -b side "${sha_signed}"
sha_side="$(signed_commit "feat: a commit on a side branch")"
git -C "${repo}" checkout --quiet main
GIT_COMMITTER_NAME="GitHub" GIT_COMMITTER_EMAIL="noreply@github.com" \
  git -C "${repo}" merge --quiet --no-ff -m "Merge pull request #1" "${sha_side}"
sha_merge="$(git -C "${repo}" rev-parse HEAD)"

sha_bot="bot0000000000000000000000000000000000000"
sha_no_identity="empty000000000000000000000000000000000000"

# Every commit on main, rendered one JSON object per commit the way the
# pull request commits endpoint returns them. "author" is GitHub's own
# resolution of the commit author to an account; nothing in a local
# repository can produce it, so it stays null here, which is also what
# the endpoint returns for an address no account owns.
commits_json="${work}/commits.json"
{
  while IFS= read -r sha; do
    jq -n \
      --arg sha "${sha}" \
      --arg message "$(git -C "${repo}" show -s --format=%B "${sha}")" \
      --arg author_name "$(git -C "${repo}" show -s --format=%an "${sha}")" \
      --arg author_email "$(git -C "${repo}" show -s --format=%ae "${sha}")" \
      --arg committer_name "$(git -C "${repo}" show -s --format=%cn "${sha}")" \
      --arg committer_email "$(git -C "${repo}" show -s --format=%ce "${sha}")" \
      --argjson parents "$(git -C "${repo}" show -s --format=%P "${sha}" |
        tr ' ' '\n' | jq -R 'select(. != "") | {sha: .}' | jq -s '.')" \
      '{
        sha: $sha,
        author: null,
        parents: $parents,
        commit: {
          message: $message,
          author: {name: $author_name, email: $author_email},
          committer: {name: $committer_name, email: $committer_email},
        },
      }'
  done < <(git -C "${repo}" log --format=%H main)

  # A commit authored by a bot account, which cannot sign a certificate
  # of origin and is skipped the way the GitHub DCO app skips it.
  jq -n --arg sha "${sha_bot}" '{
    sha: $sha,
    author: {login: "renovate[bot]", type: "Bot"},
    parents: [{sha: "0"}],
    commit: {
      message: "chore(deps): update actions/checkout",
      author: {name: "renovate[bot]", email: "renovate[bot]@users.noreply.github.com"},
      committer: {name: "GitHub", email: "noreply@github.com"},
    },
  }'

  # A commit with no author or committer address at all: nothing for a
  # trailer to match, so an empty trailer address must not pass it.
  jq -n --arg sha "${sha_no_identity}" '{
    sha: $sha,
    author: null,
    parents: [{sha: "0"}],
    commit: {
      message: "feat: a commit with no identity\n\nSigned-off-by:  <>",
      author: {name: "", email: ""},
      committer: {name: "", email: ""},
    },
  }'
} | jq -s '.' >"${commits_json}"

# run_case <name> <sha> <want exit code> <want output substring>
# Runs check-dco.sh over that one commit, so a case's verdict and message
# are read on their own rather than out of a combined report.
run_case() {
  local case_name="$1" sha="$2" want_code="$3" want_substring="$4"
  local one="${work}/one.json" out="${work}/out" code=0

  jq --arg sha "${sha}" '[.[] | select(.sha == $sha)]' "${commits_json}" >"${one}"
  if [ "$(jq 'length' "${one}")" != "1" ]; then
    echo "FAIL ${case_name}: no commit ${sha} in the rendered input"
    failures=$((failures + 1))
    return
  fi

  "${script}" "${one}" >"${out}" 2>&1 || code=$?

  if [ "${code}" != "${want_code}" ]; then
    echo "FAIL ${case_name}: exit code = ${code}, want ${want_code}, output = $(cat "${out}")"
    failures=$((failures + 1))
    return
  fi

  # "--", because a wanted substring starting with a dash is a report
  # bullet, not an option.
  if ! grep -qF -- "${want_substring}" "${out}"; then
    echo "FAIL ${case_name}: output = $(cat "${out}"), want it to contain: ${want_substring}"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

run_case "git commit -s passes" \
  "${sha_signed}" 0 "Every commit carries a valid DCO sign-off."
run_case "a missing trailer fails and names the commit" \
  "${sha_missing}" 1 "- \`${sha_missing}\`: no sign-off trailer, authored by Ada Lovelace <ada@example.com>"
run_case "a trailer naming an unrelated person fails" \
  "${sha_other_person}" 1 "sign-off by Grace Hopper <grace@example.com> matches neither the author"
run_case "a trailer differing only in case passes" \
  "${sha_mixed_case}" 0 "Every commit carries a valid DCO sign-off."
run_case "one matching trailer among several passes" \
  "${sha_several}" 0 "Every commit carries a valid DCO sign-off."
run_case "an indented trailer fails" \
  "${sha_indented}" 1 "no sign-off trailer"
run_case "a trailer with trailing whitespace passes" \
  "${sha_trailing_space}" 0 "Every commit carries a valid DCO sign-off."
run_case "a trailer without an address fails" \
  "${sha_name_only}" 1 "no sign-off trailer"
run_case "a trailer matching the author of a commit committed by somebody else passes" \
  "${sha_rebased}" 0 "Every commit carries a valid DCO sign-off."
run_case "a trailer pairing one identity's name with another's address fails" \
  "${sha_crossed}" 1 "sign-off by Ada Lovelace <noreply@github.com> matches neither the author"
run_case "an unsigned merge commit is skipped" \
  "${sha_merge}" 0 "1 skipped"
run_case "a commit authored by a bot account is skipped" \
  "${sha_bot}" 0 "1 skipped"
run_case "an empty trailer address does not match an empty commit identity" \
  "${sha_no_identity}" 1 "records no usable author or committer identity"

# The whole pull request at once: the report has to name every failing
# commit and keep the fix instructions, which is what the check run the
# workflow publishes shows a contributor.
report="${work}/report"
code=0
"${script}" "${commits_json}" >"${report}" 2>&1 || code=$?
if [ "${code}" != "1" ]; then
  echo "FAIL a report over every commit: exit code = ${code}, want 1"
  failures=$((failures + 1))
elif ! grep -qF "6 of 12 commit(s) lack a valid DCO sign-off." "${report}"; then
  echo "FAIL a report over every commit: counts = $(head -1 "${report}")"
  failures=$((failures + 1))
elif ! grep -qF "git commit --amend --signoff" "${report}" ||
  ! grep -qF "git rebase --signoff origin/main" "${report}"; then
  echo "FAIL a report over every commit: the fix instructions are missing"
  failures=$((failures + 1))
else
  echo "PASS a report over every commit names each failure and the fix"
fi

# DCO_REPORT writes the same text to a file, which is how the workflow
# hands the report to the check run without reparsing the log.
code=0
DCO_REPORT="${work}/dco.md" "${script}" "${commits_json}" >/dev/null 2>&1 || code=$?
if [ "${code}" != "1" ]; then
  echo "FAIL DCO_REPORT: exit code = ${code}, want 1"
  failures=$((failures + 1))
elif ! diff -q "${report}" "${work}/dco.md" >/dev/null; then
  echo "FAIL DCO_REPORT: the file differs from the report on stdout"
  failures=$((failures + 1))
else
  echo "PASS DCO_REPORT holds the same report as stdout"
fi

# An empty response is a bug in the caller, not a pull request that
# passes: exit 2 keeps it from being read as a green check.
code=0
echo '[]' | "${script}" >"${work}/empty-out" 2>&1 || code=$?
if [ "${code}" != "2" ]; then
  echo "FAIL an empty input: exit code = ${code}, want 2"
  failures=$((failures + 1))
elif ! grep -qF "no commits to check" "${work}/empty-out"; then
  echo "FAIL an empty input: output = $(cat "${work}/empty-out")"
  failures=$((failures + 1))
else
  echo "PASS an empty input is an error, not a pass"
fi

# "gh api --paginate" emits one array per page. Two pages of the same
# commits have to read the same as one array holding both.
code=0
jq -c '.[0:6], .[6:]' "${commits_json}" | "${script}" >"${work}/paged" 2>&1 || code=$?
if [ "${code}" != "1" ]; then
  echo "FAIL a paginated input: exit code = ${code}, want 1"
  failures=$((failures + 1))
elif ! diff -q "${report}" "${work}/paged" >/dev/null; then
  echo "FAIL a paginated input: the report differs from the single page one"
  failures=$((failures + 1))
else
  echo "PASS a paginated input reads the same as a single page"
fi

# DCO_EXPECTED_COMMITS set to more than this run actually read is the
# shape a pull request past the commits endpoint's 250-commit cap takes:
# "gh api --paginate" stops silently, so the script has to fail closed.
total_commits="$(jq 'length' "${commits_json}")"
code=0
DCO_EXPECTED_COMMITS="$((total_commits + 1))" \
  "${script}" "${commits_json}" >"${work}/expected-mismatch" 2>&1 || code=$?
if [ "${code}" != "2" ]; then
  echo "FAIL DCO_EXPECTED_COMMITS mismatch: exit code = ${code}, want 2"
  failures=$((failures + 1))
elif ! grep -qF "only ${total_commits} of $((total_commits + 1)) commit(s) were checked" \
  "${work}/expected-mismatch"; then
  echo "FAIL DCO_EXPECTED_COMMITS mismatch: output = $(cat "${work}/expected-mismatch")"
  failures=$((failures + 1))
else
  echo "PASS DCO_EXPECTED_COMMITS mismatch fails closed"
fi

if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

echo "all tests passed"
