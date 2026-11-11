// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package history reads release-relevant data from a git repository through
// the git CLI: the repository root, the last version tag, the origin
// remote, the first parent commit history since that tag, and the paths a
// commit touches, so a caller can tell which component it belongs to.
package history

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// run executes git in dir and returns its stdout.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	//nolint:gosec // the binary is the fixed literal "git", args are built by this package, not passed through a shell
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("history: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
