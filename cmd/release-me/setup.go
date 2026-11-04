// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	forgegithub "github.com/ostendorf-cloud/release-me/internal/forge/github"
	"github.com/ostendorf-cloud/release-me/internal/history"
)

// setup resolves the repository root, loads release-me.yaml from it and
// builds the GitHub forge from the configured token environment variable.
func setup(ctx context.Context, stderr io.Writer, verbose bool) (root string, cfg config.Config, f forge.Forge, logger *slog.Logger, err error) {
	logger = newLogger(stderr, verbose)

	cwd, err := os.Getwd()
	if err != nil {
		return "", config.Config{}, nil, logger, fmt.Errorf("release-me: working directory: %w", err)
	}

	root, err = history.Root(ctx, cwd)
	if err != nil {
		return "", config.Config{}, nil, logger, fmt.Errorf("release-me: repository root: %w", err)
	}

	cfg, err = config.Load(filepath.Join(root, "release-me.yaml"))
	if err != nil {
		return "", config.Config{}, nil, logger, fmt.Errorf("release-me: %w", err)
	}

	token := os.Getenv(cfg.TokenEnv)
	logger.Debug("read token", "env", cfg.TokenEnv, "set", token != "")

	ghForge, err := forgegithub.New(token)
	if err != nil {
		return "", config.Config{}, nil, logger, fmt.Errorf("release-me: forge: %w", err)
	}
	return root, cfg, withChangelogHost(cfg, ghForge), logger, nil
}
