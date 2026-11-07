// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Command release-me releases repositories from their first parent history.
package main

import (
	"fmt"
	"io"
	"os"
)

// Version is set at build time from the release tag.
var Version = "dev"

const usage = `release-me releases repositories from their first parent history.

Usage:
  release-me <command> [flags]

Commands:
  next        next version per component, nothing written
  changelog   changelog entries for the next version, nothing written
  pr          create or update the release pull requests
  release     tag, release, labels, major tag and branch deletion for merged release pull requests
  run         pr then release, the CI entry point
  check       config, labels and token valid
  comment     apply a release-as or release-notes comment command
  migrate     translate a release-please configuration, nothing written
  version     print the version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, Version)
		return 0
	case "next":
		return runNext(args[1:], stdout, stderr)
	case "changelog":
		return runChangelog(args[1:], stdout, stderr)
	case "pr":
		return runPR(args[1:], stdout, stderr)
	case "release":
		return runRelease(args[1:], stdout, stderr)
	case "run":
		return runRun(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "comment":
		return runComment(args[1:], stdout, stderr)
	case "migrate":
		return runMigrate(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "release-me: unknown command %q\n%s", args[0], usage)
		return 2
	}
}
