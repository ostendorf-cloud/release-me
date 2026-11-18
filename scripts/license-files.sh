#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Prints, one per line, every file that has to carry the license header:
# all Go files and all shell scripts of the repository. This is the one
# place that names the set. "task lint:license", the "license" CI job and
# scripts/check-license-spdx.sh all read it from here, so the presence
# check and the identifier check cannot cover different files.
#
# Not covered, on purpose: Markdown, YAML, JSON, TOML, the Dockerfile, go.mod,
# go.sum, LICENSE and VERSION. These file types carry no header in this
# repository, and AGENTS.md records the decision. Worktrees are skipped
# because they hold copies of this repository's own files.
#
# Prints nothing when no file matches. The callers treat an empty set as a
# failure, this script only selects.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${script_dir}/.."

find . -type f \( -name "*.go" -o -name "*.sh" \) \
  -not -path "./.worktrees/*" \
  -not -path "./.claude/worktrees/*" |
  LC_ALL=C sort
