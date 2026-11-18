#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Checks that the SPDX-License-Identifier of every covered file names the
# license this repository's own LICENSE file holds. addlicense -check only
# proves a header exists, it accepts a header that names another license.
# This script closes that gap. Exits 0 when every file matches, 1 when a
# file does not or the set is empty, 2 when LICENSE is not recognised.
#
# The covered files come from scripts/license-files.sh, the same list the
# addlicense check uses.
#
# The identifier is derived from the opening lines of LICENSE, through a
# pinned table below. A text that is not in the table is an error, never a
# guess, so adding a license to the repository means adding a row here.
#
# What it cannot catch: a LICENSE that is wrong itself, a license text
# edited below its opening lines, and files outside the covered set.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${script_dir}/.." && pwd)"
license="${root}/LICENSE"

if [ ! -f "${license}" ]; then
  echo "LICENSE not found at ${license}" >&2
  exit 2
fi

# The first two non empty lines of LICENSE, trimmed.
opening="$(awk 'NF { gsub(/^[ \t]+|[ \t\r]+$/, ""); print; if (++n == 2) exit }' "${license}")"
first="$(printf '%s\n' "${opening}" | sed -n 1p)"
second="$(printf '%s\n' "${opening}" | sed -n 2p)"

expected=""
case "${first}" in
"Apache License")
  if [ "${second}" = "Version 2.0, January 2004" ]; then
    expected="Apache-2.0"
  fi
  ;;
"MIT License")
  expected="MIT"
  ;;
"Elastic License 2.0")
  expected="Elastic-2.0"
  ;;
esac

if [ -z "${expected}" ]; then
  echo "LICENSE is not recognised: opens with '${first}' / '${second}'." >&2
  echo "Add its SPDX identifier to the table in scripts/check-license-spdx.sh." >&2
  exit 2
fi

files="$("${script_dir}/license-files.sh")"
if [ -z "${files}" ]; then
  echo "no file matched scripts/license-files.sh, nothing was checked" >&2
  exit 1
fi

failures=0
count=0
while IFS= read -r file; do
  count=$((count + 1))
  # The identifier sits in the header, so only the top of the file counts.
  # A line further down, in a comment or a string, is not a header.
  found="$(head -n 5 "${root}/${file}" |
    sed -n -E 's,^(//|#) SPDX-License-Identifier: *(.*[^[:space:]])[[:space:]]*$,\2,p')"
  if [ -z "${found}" ]; then
    echo "${file}: no SPDX-License-Identifier line in the first 5 lines" >&2
    failures=$((failures + 1))
  elif [ "${found}" != "${expected}" ]; then
    echo "${file}: names '${found}', LICENSE is ${expected}" >&2
    failures=$((failures + 1))
  fi
done <<<"${files}"

if [ "${failures}" -ne 0 ]; then
  echo "${failures} of ${count} file(s) do not carry SPDX-License-Identifier: ${expected}" >&2
  exit 1
fi

echo "${count} file(s) carry SPDX-License-Identifier: ${expected}"
