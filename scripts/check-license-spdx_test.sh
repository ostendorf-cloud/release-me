#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Tests for check-license-spdx.sh and license-files.sh. Every case builds a
# scratch tree in a temporary directory: copies of both scripts, a LICENSE
# fixture and a few source files. The scripts find the tree through their
# own location, so a case that breaks a header or swaps the LICENSE only
# ever touches the scratch copy. One case runs the script against this
# repository itself, read only.
#
# SCRIPTS_DIR names the directory holding the scripts under test, this
# directory by default. A mutation run points it at a deliberately broken
# copy to prove that a test notices.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scripts="${SCRIPTS_DIR:-${script_dir}}"
# An explicit template, because macOS mktemp ignores TMPDIR without one
# and always lands in the system temporary directory.
work="$(mktemp -d "${TMPDIR:-/tmp}/check-license-spdx.XXXXXXXX")"
trap 'rm -rf "${work}"' EXIT

failures=0
counter=0
mit_permission='Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction.'

# new_tree <license> echoes the root of a fresh scratch tree. <license> is
# apache, mit, elastic or gpl, the opening lines of that text.
new_tree() {
  counter=$((counter + 1))
  local root="${work}/tree-${counter}"
  mkdir -p "${root}/scripts" "${root}/cmd"
  cp "${scripts}/license-files.sh" "${scripts}/check-license-spdx.sh" "${root}/scripts/"
  case "$1" in
  apache)
    printf '                                 Apache License\n                           Version 2.0, January 2004\n' >"${root}/LICENSE"
    ;;
  mit)
    printf 'MIT License\n\nCopyright (c) 2026 Simon Ostendorf\n\n%s\n' "${mit_permission}" >"${root}/LICENSE"
    ;;
  elastic)
    printf 'Elastic License 2.0\n\nURL: https://www.elastic.co/licensing/elastic-license\n' >"${root}/LICENSE"
    ;;
  gpl)
    printf '                    GNU GENERAL PUBLIC LICENSE\n                       Version 3, 29 June 2007\n' >"${root}/LICENSE"
    ;;
  esac
  printf '// Copyright 2026 Simon Ostendorf\n// SPDX-License-Identifier: Apache-2.0\n\npackage main\n' >"${root}/cmd/main.go"
  printf '#!/usr/bin/env bash\n# Copyright 2026 Simon Ostendorf\n# SPDX-License-Identifier: Apache-2.0\nset -euo pipefail\n' >"${root}/scripts/tool.sh"
  echo "${root}"
}

# run_check <root> sets code and output from the check inside that tree.
run_check() {
  code=0
  output="$("$1/scripts/check-license-spdx.sh" 2>&1)" || code=$?
}

# expect <name> <want code> [<text the output must contain>]
expect() {
  local name="$1" want="$2" text="${3:-}"
  if [ "${code}" != "${want}" ]; then
    echo "FAIL ${name}: exit code = ${code}, want ${want}: ${output}"
    failures=$((failures + 1))
  elif [ -n "${text}" ] && ! grep -qF -- "${text}" <<<"${output}"; then
    echo "FAIL ${name}: output lacks '${text}': ${output}"
    failures=$((failures + 1))
  else
    echo "PASS ${name}"
  fi
}

root="$(new_tree apache)"
run_check "${root}"
expect "matching headers pass" 0 "SPDX-License-Identifier: Apache-2.0"

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\n// SPDX-License-Identifier: Elastic-2.0\n\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "a Go file naming another license fails" 1 "./cmd/main.go: names 'Elastic-2.0', LICENSE is Apache-2.0"

root="$(new_tree apache)"
printf '#!/usr/bin/env bash\n# Copyright 2026 Simon Ostendorf\n# SPDX-License-Identifier: MIT\n' >"${root}/scripts/tool.sh"
run_check "${root}"
expect "a shell script naming another license fails" 1 "./scripts/tool.sh: names 'MIT'"

root="$(new_tree elastic)"
run_check "${root}"
expect "a LICENSE swapped for Elastic 2.0 fails the Apache headers" 1 "LICENSE is Elastic-2.0"

root="$(new_tree mit)"
run_check "${root}"
expect "a LICENSE swapped for MIT fails the Apache headers" 1 "LICENSE is MIT"

root="$(new_tree elastic)"
printf '// Copyright 2026 Simon Ostendorf\n// SPDX-License-Identifier: Elastic-2.0\n\npackage main\n' >"${root}/cmd/main.go"
printf '#!/usr/bin/env bash\n# Copyright 2026 Simon Ostendorf\n# SPDX-License-Identifier: Elastic-2.0\n' >"${root}/scripts/tool.sh"
# The two copied scripts carry this repository's header, relabel them too.
for copied in license-files.sh check-license-spdx.sh; do
  sed 's/SPDX-License-Identifier: Apache-2.0/SPDX-License-Identifier: Elastic-2.0/' \
    "${root}/scripts/${copied}" >"${root}/scripts/${copied}.new"
  chmod +x "${root}/scripts/${copied}.new"
  mv "${root}/scripts/${copied}.new" "${root}/scripts/${copied}"
done
run_check "${root}"
expect "Elastic headers pass under an Elastic LICENSE" 0 "Elastic-2.0"

root="$(new_tree gpl)"
run_check "${root}"
expect "an unrecognised LICENSE fails closed" 2 "LICENSE is not recognised"

root="$(new_tree apache)"
rm "${root}/LICENSE"
run_check "${root}"
expect "a missing LICENSE fails closed" 2 "LICENSE not found"

# Every MIT form in use is recognised, and each one fails Apache headers.
mit_case() {
  root="$(new_tree apache)"
  printf '%s' "$2" >"${root}/LICENSE"
  run_check "${root}"
  expect "$1" 1 "LICENSE is MIT"
}
mit_case "the GitHub MIT text is recognised" "$(printf 'MIT License\n\nCopyright (c) 2026 Simon Ostendorf\n\n%s\n' "${mit_permission}")"
mit_case "the classic MIT text is recognised" "$(printf 'The MIT License (MIT)\n\nCopyright (c) 2026 Simon Ostendorf\n\n%s\n' "${mit_permission}")"
mit_case "the SPDX MIT text without a title is recognised" "$(printf 'Copyright (c) 2026 Simon Ostendorf\n\n%s\n' "${mit_permission}")"

root="$(new_tree apache)"
printf 'Copyright (c) 2026 Simon Ostendorf\n\nAll rights reserved.\n' >"${root}/LICENSE"
run_check "${root}"
expect "a copyright notice alone is not MIT" 2 "LICENSE is not recognised"

root="$(new_tree apache)"
printf 'MIT License\n\nCopyright (c) 2026 Simon Ostendorf\n\nSee the website for the terms.\n' >"${root}/LICENSE"
run_check "${root}"
expect "an MIT title without the permission sentence is not recognised" 2 "LICENSE is not recognised"

root="$(new_tree apache)"
printf '\n\n\n\n\n\n\n\n\n\nPermission is hereby granted, free of charge, to any person obtaining a copy\n' >"${root}/LICENSE"
run_check "${root}"
expect "the MIT permission sentence below line 10 is not recognised" 2 "LICENSE is not recognised"

root="$(new_tree apache)"
printf 'Permission is granted to Simon Ostendorf only.\nPermission is hereby revoked.\n' >"${root}/LICENSE"
run_check "${root}"
expect "another sentence that starts with Permission is not MIT" 2 "LICENSE is not recognised"

root="$(new_tree apache)"
printf '\n\n\n\n\n\n\n\n\nPermission is hereby granted, free of charge, to any person obtaining a copy\n' >"${root}/LICENSE"
run_check "${root}"
expect "the MIT permission sentence on line 10 is recognised" 1 "LICENSE is MIT"

root="$(new_tree apache)"
printf 'Apache License\nVersion 3.0, January 2099\n' >"${root}/LICENSE"
run_check "${root}"
expect "an Apache title with another version is not recognised" 2 "LICENSE is not recognised"

root="$(new_tree apache)"
printf '\n\n   \n                                 Apache License\n\n                           Version 2.0, January 2004\n' >"${root}/LICENSE"
run_check "${root}"
expect "blank lines before and inside the opening are skipped" 0

root="$(new_tree apache)"
printf '                                 Apache License\r\n                           Version 2.0, January 2004\r\n' >"${root}/LICENSE"
run_check "${root}"
expect "a CRLF LICENSE is recognised" 0

root="$(new_tree apache)"
printf '\r\n\r\n  \r\nElastic License 2.0\r\n\r\nURL: https://www.elastic.co/licensing/elastic-license\r\n' >"${root}/LICENSE"
run_check "${root}"
expect "a CRLF LICENSE with leading blank lines is recognised" 1 "LICENSE is Elastic-2.0"

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\n\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "a file with no SPDX line fails" 1 "no SPDX-License-Identifier line"

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\n\n\n\n\n\n// SPDX-License-Identifier: Apache-2.0\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "an SPDX line below the header fails" 1 "no SPDX-License-Identifier line"

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\n\n\n// SPDX-License-Identifier: Apache-2.0\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "an SPDX line at line 4 passes" 0

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\n\n\n\n// SPDX-License-Identifier: Apache-2.0\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "an SPDX line at line 5 passes" 0

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\n\n\n\n\n// SPDX-License-Identifier: Apache-2.0\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "an SPDX line at line 6 fails" 1 "no SPDX-License-Identifier line"

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\nx := 1 // SPDX-License-Identifier: Apache-2.0\n\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "an SPDX string not at the start of a line fails" 1 "no SPDX-License-Identifier line"

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\n// SPDX-License-Identifier: Apache-2.0 OR MIT\n\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "a compound expression is not the same identifier" 1 "names 'Apache-2.0 OR MIT'"

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\n// SPDX-License-Identifier: apache-2.0\n\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "the identifier compares case sensitively" 1 "names 'apache-2.0'"

root="$(new_tree apache)"
printf '// Copyright 2026 Simon Ostendorf\n// SPDX-License-Identifier: Apache-2.0  \r\n\npackage main\n' >"${root}/cmd/main.go"
run_check "${root}"
expect "trailing whitespace and a carriage return are ignored" 0

root="$(new_tree apache)"
rm "${root}/cmd/main.go" "${root}/scripts/tool.sh" "${root}/scripts/license-files.sh"
printf '#!/usr/bin/env bash\nexit 0\n' >"${root}/scripts/license-files.sh"
chmod +x "${root}/scripts/license-files.sh"
run_check "${root}"
expect "an empty file set fails" 1 "nothing was checked"

root="$(new_tree apache)"
mkdir -p "${root}/.worktrees/x" "${root}/.claude/worktrees/y"
printf 'package x\n' >"${root}/.worktrees/x/a.go"
printf 'package y\n' >"${root}/.claude/worktrees/y/b.go"
printf '# no header, a type that carries none\n' >"${root}/README.md"
run_check "${root}"
expect "worktree copies and other file types are not covered" 0

root="$(new_tree apache)"
mkdir -p "${root}/dir.go" "${root}/dir.sh"
ln -s "${root}/cmd/main.go" "${root}/link.go"
list="$("${root}/scripts/license-files.sh")"
case "${list}" in
*dir.go* | *dir.sh* | *link.go*)
  echo "FAIL license-files.sh lists a directory or a symlink: ${list}"
  failures=$((failures + 1))
  ;;
*)
  echo "PASS license-files.sh lists regular files only"
  ;;
esac
rm -r "${root}/dir.go" "${root}/dir.sh" "${root}/link.go"
list="$("${root}/scripts/license-files.sh")"
if [ "${list}" != "$(printf './cmd/main.go\n./scripts/license-files.sh\n./scripts/tool.sh\n./scripts/check-license-spdx.sh' | LC_ALL=C sort)" ]; then
  echo "FAIL license-files.sh lists Go and shell files: ${list}"
  failures=$((failures + 1))
else
  echo "PASS license-files.sh lists Go and shell files"
fi

# This repository itself, read only: the real LICENSE against the real files.
# Skipped for a mutation run, whose scripts do not sit in this repository.
if [ "${scripts}" = "${script_dir}" ]; then
  code=0
  output="$("${scripts}/check-license-spdx.sh" 2>&1)" || code=$?
  expect "this repository passes" 0 "SPDX-License-Identifier: Apache-2.0"
fi

if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

echo "all tests passed"
