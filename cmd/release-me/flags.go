// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"io"
)

// componentUsage is the --component flag's help text, shared by every
// command that loops over components.
const componentUsage = "limit to one configured component"

// parseFlags parses the --verbose and --component flags shared by next,
// changelog and check, which write nothing and so have no --dry-run flag.
// ok is false when flag.ContinueOnError already reported the error on
// stderr; the caller should return exit code 2.
func parseFlags(name string, args []string, stderr io.Writer) (verbose bool, component string, ok bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	v := fs.Bool("verbose", false, "enable debug logging on stderr")
	c := fs.String("component", "", componentUsage)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return false, "", false
	}
	return *v, *c, true
}

// parseWriteFlags parses the --verbose, --dry-run and --component flags
// shared by pr, release and run. ok is false when flag.ContinueOnError
// already reported the error on stderr; the caller should return exit
// code 2.
func parseWriteFlags(name string, args []string, stderr io.Writer) (verbose, dryRun bool, component string, ok bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	v := fs.Bool("verbose", false, "enable debug logging on stderr")
	d := fs.Bool("dry-run", false, "print what would change, write nothing")
	c := fs.String("component", "", componentUsage)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return false, false, "", false
	}
	return *v, *d, *c, true
}

// commentFlags are comment's parsed flags: the pull request the comment
// was posted on, the comment event's author_association and body, exactly
// as the GitHub Actions workflow reads them off github.event.
type commentFlags struct {
	verbose     bool
	prNumber    int
	association string
	body        string
}

// parseCommentFlags parses comment's flags: --verbose, --pr-number,
// --association and --body. ok is false when flag.ContinueOnError already
// reported the error on stderr, or --pr-number was left at its zero
// value; the caller should return exit code 2.
func parseCommentFlags(args []string, stderr io.Writer) (f commentFlags, ok bool) {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.BoolVar(&f.verbose, "verbose", false, "enable debug logging on stderr")
	fs.IntVar(&f.prNumber, "pr-number", 0, "the pull request the comment was posted on")
	fs.StringVar(&f.association, "association", "", "the comment event's author_association")
	fs.StringVar(&f.body, "body", "", "the comment body")
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return commentFlags{}, false
	}
	if f.prNumber == 0 {
		fmt.Fprintln(stderr, "release-me: comment: --pr-number is required")
		return commentFlags{}, false
	}
	return f, true
}
