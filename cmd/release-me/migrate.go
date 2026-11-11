// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/history"
	"github.com/ostendorf-cloud/release-me/internal/migrate"
)

// migrateUsage is the migrate command's own help, printed for a missing or
// unknown source tool.
const migrateUsage = `Usage:
  release-me migrate release-please [--verbose]

Reads release-please-config.json and .release-please-manifest.json from the
repository root and prints the equivalent release-me.yaml. Settings without a
release-me equivalent are reported on stderr. Nothing is written.
`

// runMigrate implements the migrate command: it translates another release
// tool's configuration into release-me.yaml on stdout, reports what it could
// not carry over on stderr, and checks that the tag each package was last
// released under exists, since release-me continues from it.
func runMigrate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "release-please" {
		fmt.Fprint(stderr, migrateUsage)
		return 2
	}
	verbose, component, ok := parseFlags("migrate", args[1:], stderr)
	if !ok {
		return 2
	}
	if component != "" {
		fmt.Fprintln(stderr, "release-me: migrate: --component does not apply, the whole configuration is translated")
		return 2
	}
	logger := newLogger(stderr, verbose)

	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "release-me: working directory: %v\n", err)
		return 1
	}
	dir, err := history.Root(ctx, cwd)
	if err != nil {
		fmt.Fprintf(stderr, "release-me: repository root: %v\n", err)
		return 1
	}
	logger.Debug("resolved repository root", "dir", dir)

	//nolint:gosec // dir is the local repository root history.Root resolved, the filename is a fixed literal
	configJSON, err := os.ReadFile(filepath.Join(dir, "release-please-config.json"))
	if err != nil {
		fmt.Fprintf(stderr, "release-me: migrate: %v\n", err)
		return 1
	}
	//nolint:gosec // dir is the local repository root history.Root resolved, the filename is a fixed literal
	manifestJSON, err := os.ReadFile(filepath.Join(dir, ".release-please-manifest.json"))
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "release-me: migrate: %v\n", err)
		return 1
	}

	// An extra-files entry comes out of release-please-config.json, so it
	// names a path the repository controls, not one release-me chose. The
	// lexical check rejects a literal "..": it names the offending entry in
	// the warning, which the generic open error cannot. os.OpenInRoot then
	// resolves the entry with every path element and every symlink checked
	// against dir, so a symlink committed inside the repository and pointing
	// out of it is refused too. filepath.Rel alone would follow that symlink,
	// since it only compares path text.
	readFile := func(path string) ([]byte, error) {
		rel, err := filepath.Rel(dir, filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("release-me: migrate: extra-files path %q escapes the repository root", path)
		}
		f, err := os.OpenInRoot(dir, rel)
		if err != nil {
			return nil, fmt.Errorf("release-me: migrate: extra-files path %q does not open inside the repository root: %w", path, err)
		}
		defer func() { _ = f.Close() }()
		return io.ReadAll(f)
	}
	result, err := migrate.FromReleasePlease(configJSON, manifestJSON, readFile)
	if err != nil {
		fmt.Fprintf(stderr, "release-me: %v\n", err)
		return 1
	}
	// The warnings come first: a translation release-me refuses is often
	// explained by one of them.
	for _, w := range result.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if err := validateMigrated(result.YAML); err != nil {
		fmt.Fprintf(stderr, "release-me: migrate: the translated release-me.yaml does not load: %v\n", err)
		fmt.Fprintf(stderr, "release-me: migrate: the refused translation, to correct by hand:\n%s", result.YAML)
		return 1
	}
	for _, t := range result.Tags {
		exists, err := history.TagExists(ctx, dir, t.Tag)
		if err != nil {
			fmt.Fprintf(stderr, "release-me: migrate: tag %s: %v\n", t.Tag, err)
			return 1
		}
		logger.Debug("looked up the last release tag", "component", t.Component, "tag", t.Tag, "exists", exists)
		if !exists {
			fmt.Fprintf(stderr, "warning: %s: tag %s does not exist, release-me would start over at the initial version\n",
				t.Component, t.Tag)
		}
	}

	if _, err := stdout.Write(result.YAML); err != nil {
		fmt.Fprintf(stderr, "release-me: migrate: %v\n", err)
		return 1
	}
	return 0
}

// validateMigrated loads the translated configuration the way every other
// command will, so a translation release-me itself would refuse is reported
// here instead of on the first release run. The error names release-me.yaml,
// not the temporary file it was loaded from, which is gone by the time the
// user reads it.
func validateMigrated(content []byte) error {
	tmp, err := os.MkdirTemp("", "release-me-migrate-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	path := filepath.Join(tmp, "release-me.yaml")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	if _, err := config.Load(path); err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), path, "release-me.yaml"))
	}
	return nil
}
