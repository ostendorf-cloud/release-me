// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package forgetest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/forge"
)

// FakeAuthor and FakeCommitter are the identities commitInto writes every
// commit as, the ones GitHub gives a commit a GitHub App installation
// token creates through its API: the app's bot account as the author,
// GitHub itself as the committer that signs it. They model that one
// outcome only. A personal access token or an OAuth token gets the user
// as author and committer and no signature at all; Fake.Unsigned models
// the missing signature, nothing here models that identity.
const (
	FakeAuthor    = "release-me-app[bot] <123456+release-me-app[bot]@users.noreply.github.com>"
	FakeCommitter = "GitHub <noreply@github.com>"
)

// commitInto creates c in the git directory gitDir through plumbing alone,
// never touching a worktree or publishing a branch.
func commitInto(ctx context.Context, gitDir string, c forge.BranchCommit) (string, error) {
	tmp, err := os.MkdirTemp("", "forgetest-index-")
	if err != nil {
		return "", fmt.Errorf("forgetest: branch %s: temporary index: %w", c.Branch, err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	g := plumbing{gitDir: gitDir, index: filepath.Join(tmp, "index")}

	if _, err := g.run(ctx, nil, "read-tree", c.Parent); err != nil {
		return "", fmt.Errorf("forgetest: branch %s: read parent %s: %w", c.Branch, c.Parent, err)
	}
	for _, file := range c.Files {
		blob, err := g.run(ctx, file.Content, "hash-object", "-w", "--stdin")
		if err != nil {
			return "", fmt.Errorf("forgetest: branch %s: write %s: %w", c.Branch, file.Path, err)
		}
		mode := "100644"
		if file.Executable {
			mode = "100755"
		}
		if _, err := g.run(ctx, nil, "update-index", "--add", "--cacheinfo", mode+","+blob+","+file.Path); err != nil {
			return "", fmt.Errorf("forgetest: branch %s: stage %s: %w", c.Branch, file.Path, err)
		}
	}
	tree, err := g.run(ctx, nil, "write-tree")
	if err != nil {
		return "", fmt.Errorf("forgetest: branch %s: write tree: %w", c.Branch, err)
	}
	sha, err := g.run(ctx, nil, "commit-tree", tree, "-p", c.Parent, "-m", c.Message)
	if err != nil {
		return "", fmt.Errorf("forgetest: branch %s: commit: %w", c.Branch, err)
	}

	return sha, nil
}

// plumbing runs git against one git directory and one index file.
type plumbing struct {
	gitDir, index string
}

// run runs git with stdin, returning its trimmed stdout.
func (g plumbing) run(ctx context.Context, stdin []byte, args ...string) (string, error) {
	//nolint:gosec // the binary is the fixed literal "git", args come from this test fake's own callers, not external input
	cmd := exec.CommandContext(ctx, "git", args...)
	authorName, authorEmail := splitIdentity(FakeAuthor)
	committerName, committerEmail := splitIdentity(FakeCommitter)
	cmd.Env = append(os.Environ(),
		"GIT_DIR="+g.gitDir, "GIT_INDEX_FILE="+g.index,
		"GIT_AUTHOR_NAME="+authorName, "GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME="+committerName, "GIT_COMMITTER_EMAIL="+committerEmail,
	)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// splitIdentity splits "name <email>" into its two parts.
func splitIdentity(identity string) (name, email string) {
	name, email, _ = strings.Cut(identity, " <")
	return name, strings.TrimSuffix(email, ">")
}
