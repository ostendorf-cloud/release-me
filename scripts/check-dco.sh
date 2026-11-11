#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Checks that every commit of a pull request carries a Developer
# Certificate of Origin sign-off, and writes a markdown report naming the
# commits that do not. Exits 0 when every commit passes, 1 when at least
# one fails, 2 when the input is unusable.
#
# The input is the JSON of GitHub's "list commits on a pull request"
# endpoint, read from stdin or from the files named as arguments:
#
#   gh api --paginate "repos/${REPO}/pulls/${PR}/commits" | scripts/check-dco.sh
#
# That endpoint, rather than git log against the merge base, because it is
# the only source that carries all four facts the rule below needs for
# every commit in one place: the raw commit message, the author and
# committer identities as they were recorded, the parent count, and
# whether the commit's author resolves to a GitHub bot account. A git log
# in the job's checkout would need fetch-depth: 0 and a merge-base
# computation, and still could not answer the bot question. The endpoint
# returns at most 250 commits for a pull request; a larger one has to be
# split, which is the same limit the GitHub DCO app works under. A pull
# request past that cap makes "gh api --paginate" stop silently instead
# of failing, so a caller that knows the pull request's real commit count
# can pass it as DCO_EXPECTED_COMMITS: a mismatch between that count and
# the number of commits this run actually read then fails closed with
# exit 2, rather than this script reporting success over a truncated
# prefix of the pull request.
#
# "gh api --paginate" emits one JSON array per page, so the jq program
# below is slurped and flattened one level: that reads a single page, a
# multi page response and a plain stream of commit objects alike.
#
# What counts as a valid sign-off follows what the GitHub DCO app
# (dcoapp/app, lib/dco.js) treats as a pass, with one rule tightened:
#
#   - A merge commit (more than one parent) is skipped. This repository
#     merges every pull request as a merge commit, authored by the person
#     who clicked merge and committed by GitHub itself, and no sign-off
#     can be added to it. The DCO app skips them for the same reason.
#   - A commit whose author resolves to a GitHub bot account is skipped,
#     as the DCO app does: a bot cannot sign a certificate of origin.
#     This trusts GitHub's own resolution of the commit's author email to
#     an account of type Bot, so a human who commits under a bot's email
#     address is skipped too. The DCO app has the same hole. Sign-off is
#     an attestation, not an authentication: someone willing to commit
#     under another identity can equally type a correct trailer.
#   - Every other commit needs at least one "Signed-off-by: Name <email>"
#     trailer line whose name and email together match either the
#     commit's author or its committer, compared case insensitively.
#   - The tightened rule: name and email must match the same identity.
#     The DCO app compares the trailer's name against the set of both
#     names and its email against the set of both emails, so a trailer
#     pairing the author's name with the committer's email passes there.
#     That combination identifies nobody. It matters in practice in this
#     repository, where a commit made through the GitHub web interface is
#     committed by "GitHub <noreply@github.com>": the looser rule accepts
#     a sign-off by "<the author's name> <noreply@github.com>", which
#     certifies nothing about a reachable person.
#
# An identity with an empty name or an empty email is ignored, so a
# commit missing an author email cannot be matched by a trailer with an
# empty email, and a trailer address without an "@" never matches.
#
# Remediation commits, the "I, Name <email>, hereby add my Signed-off-by
# to this commit: <sha>" form the DCO app can be configured to accept,
# are not implemented: they are off by default there as well, and nothing
# in this repository's CONTRIBUTING.md offers them.
set -euo pipefail

report_path="${DCO_REPORT:-}"
expected_commits="${DCO_EXPECTED_COMMITS:-}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required but not installed" >&2
  exit 2
fi

# Each line of the jq output is one commit: its SHA, the outcome, and a
# detail for the report. Tabs and newlines are stripped from every name
# and address that reaches the output, so one commit is always one line
# whatever an identity happens to contain.
outcomes="$(jq -r -s '
  def clean: tostring | gsub("[\t\n\r]"; " ");

  def identities:
    [.commit.author, .commit.committer]
    | map(select(. != null))
    | map({name: ((.name // "") | ascii_downcase), email: ((.email // "") | ascii_downcase)})
    | map(select(.name != "" and .email != ""));

  def signoffs:
    [
      ((.commit.message // "") | split("\n")[])
      | sub("\r$"; "")
      | capture("^signed-off-by:[ \t]+(?<name>[^<>]+?)[ \t]*<(?<email>[^<>]+)>[ \t]*$"; "i")
      | {
          name: (.name | ascii_downcase),
          email: (.email | ascii_downcase),
          text: "\(.name) <\(.email)>",
        }
      | select(.email | test("@"))
    ];

  flatten(1)
  | .[]
  | . as $commit
  | (.sha // "") as $sha
  | if ((.parents // []) | length) > 1 then
      [$sha, "skip", "merge commit"]
    elif (.author.type? // "") == "Bot" then
      [$sha, "skip", "authored by \((.author.login? // "a bot") | clean), a bot account"]
    else
      (identities) as $identities
      | (signoffs) as $signoffs
      | ("\($commit.commit.author.name // "" | clean) <\($commit.commit.author.email // "" | clean)>") as $author
      | if ($identities | length) == 0 then
          [$sha, "fail", "the commit records no usable author or committer identity"]
        elif ($signoffs | length) == 0 then
          [$sha, "fail", "no sign-off trailer, authored by \($author)"]
        elif any($signoffs[]; {name, email} as $signoff | any($identities[]; . == $signoff)) then
          [$sha, "pass", "signed off by \($author)"]
        else
          [
            $sha,
            "fail",
            "sign-off by \($signoffs | map(.text) | join(", ") | clean)"
              + " matches neither the author \($author)"
              + " nor the committer \($commit.commit.committer.name // "" | clean)"
              + " <\($commit.commit.committer.email // "" | clean)>"
          ]
        end
    end
  | @tsv
' "$@")"

passed=0
skipped=0
declare -a failures=()

while IFS=$'\t' read -r sha outcome detail; do
  if [ -z "${sha}" ]; then
    continue
  fi
  case "${outcome}" in
  pass) passed=$((passed + 1)) ;;
  skip) skipped=$((skipped + 1)) ;;
  fail) failures+=("${sha} ${detail}") ;;
  *)
    echo "unexpected outcome for ${sha}: ${outcome}" >&2
    exit 2
    ;;
  esac
done <<<"${outcomes}"

total=$((passed + skipped + ${#failures[@]}))

# DCO_EXPECTED_COMMITS, when the caller sets it, is the pull request's
# real commit count from the GitHub API rather than a count derived from
# this run's own input. The two can differ only when "gh api --paginate"
# was truncated by the commits endpoint's 250-commit cap, since pagination
# itself is otherwise lossless. Checking fewer commits than the pull
# request actually has must not be read as a passing run.
if [ -n "${expected_commits}" ] && [ "${total}" != "${expected_commits}" ]; then
  echo "only ${total} of ${expected_commits} commit(s) were checked:" \
    "the pull request has more commits than this run read" >&2
  exit 2
fi

if [ "${passed}" -eq 0 ] && [ "${skipped}" -eq 0 ] && [ "${#failures[@]}" -eq 0 ]; then
  echo "no commits to check: the input holds no commit" >&2
  exit 2
fi

# The report goes to stdout so a caller can read it from a pipe, and to
# DCO_REPORT as well when that names a file, which is how the workflow
# hands the same text to the check run it publishes.
write_report() {
  if [ -n "${report_path}" ]; then
    tee "${report_path}"
  else
    cat
  fi
}

if [ "${#failures[@]}" -eq 0 ]; then
  {
    echo "Every commit carries a valid DCO sign-off."
    echo
    echo "${passed} commit(s) checked, ${skipped} skipped (merge commits and bot commits)."
  } | write_report
  exit 0
fi

{
  echo "${#failures[@]} of $((passed + ${#failures[@]})) commit(s) lack a valid DCO sign-off."
  echo
  for failure in "${failures[@]}"; do
    echo "- \`${failure%% *}\`: ${failure#* }"
  done
  echo
  echo "Every commit needs a \`Signed-off-by: Name <email>\` trailer whose name and"
  echo "email match the commit's own author or committer. See CONTRIBUTING.md."
  echo
  echo "Fix the most recent commit:"
  echo
  echo '```sh'
  echo "git commit --amend --signoff"
  echo "git push --force-with-lease"
  echo '```'
  echo
  echo "Fix several commits, replacing main with this pull request's base branch:"
  echo
  echo '```sh'
  echo "git rebase --signoff origin/main"
  echo "git push --force-with-lease"
  echo '```'
  echo
  echo "Set the sign-off for every future commit with \`git commit -s\`."
} | write_report

exit 1
