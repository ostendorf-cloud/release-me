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

// runCheck implements the check command: it validates release-me.yaml, the
// configured component or --component name, the configured labels and the
// token environment variable, printing every missing or invalid item.
func runCheck(args []string, stdout, stderr io.Writer) int {
	verbose, component, ok := parseFlags("check", args, stderr)
	if !ok {
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

	cfg, cfgErr := config.Load(filepath.Join(dir, "release-me.yaml"))
	tokenEnv := config.DefaultTokenEnv
	if cfgErr == nil {
		tokenEnv = cfg.TokenEnv
	}
	token := os.Getenv(tokenEnv)
	logger.Debug("read token", "env", tokenEnv, "set", token != "")

	var f forge.Forge
	owner, repoName := "", ""
	if cfgErr == nil {
		var remoteErr error
		owner, repoName, remoteErr = history.Remote(ctx, dir)
		if remoteErr != nil {
			fmt.Fprintf(stderr, "release-me: origin remote: %v\n", remoteErr)
			return 1
		}
		ghForge, forgeErr := forgegithub.New(token)
		if forgeErr != nil {
			fmt.Fprintf(stderr, "release-me: forge: %v\n", forgeErr)
			return 1
		}
		f = ghForge
	}

	var componentErr error
	if cfgErr == nil {
		_, componentErr = resolveComponents(cfg, component)
		warnIgnoredAuthor(cfg, logger)
	}

	missing := doCheck(ctx, cfg, cfgErr, componentErr, tokenEnv, token, owner, repoName, f)
	for _, m := range missing {
		fmt.Fprintln(stdout, m)
	}
	if len(missing) > 0 {
		return 1
	}
	fmt.Fprintln(stdout, "ok")
	return 0
}

// warnIgnoredAuthor logs a warning for every key of the "author" block
// release-me.yaml sets. pr no longer applies it: the release commit is
// created through the forge under the token's own identity, the only way
// GitHub signs it. The key still loads, since Load rejects unknown keys
// and an existing configuration must keep working, so this warning is
// the one signal a repository gets that its configured identity is gone.
// A warning, never a missing item: check still passes.
func warnIgnoredAuthor(cfg config.Config, logger *slog.Logger) {
	for _, key := range cfg.Author.Set {
		logger.Warn(key+" is ignored: pr creates the release commit through the forge under the token's own identity",
			"key", key)
	}
}

// doCheck validates config, the --component name, the configured labels
// and the token, returning every missing or invalid item. An empty result
// means every check passed. f is nil when cfgErr is set: the labels cannot
// be checked against an unparsed configuration.
func doCheck(ctx context.Context, cfg config.Config, cfgErr, componentErr error, tokenEnv, token, owner, repoName string, f forge.Forge) []string {
	var missing []string

	if cfgErr != nil {
		missing = append(missing, fmt.Sprintf("config: %v", cfgErr))
	}
	if componentErr != nil {
		missing = append(missing, fmt.Sprintf("config: %v", componentErr))
	}
	if token == "" {
		missing = append(missing, fmt.Sprintf("token: environment variable %s is not set", tokenEnv))
	}

	if cfgErr == nil && f != nil {
		labels, unauthorized, err := missingLabels(ctx, f, owner, repoName, cfg)
		switch {
		case unauthorized:
			missing = append(missing, fmt.Sprintf("token: environment variable %s is rejected by the forge: %v", tokenEnv, err))
		case err != nil:
			missing = append(missing, fmt.Sprintf("labels: %v", err))
		default:
			for _, name := range labels {
				missing = append(missing, fmt.Sprintf("label %q does not exist on the repository", name))
			}
		}
	}
	return missing
}
