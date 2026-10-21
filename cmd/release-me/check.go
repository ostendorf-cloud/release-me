// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/history"
)

// runCheck implements the check command: it validates release-me.yaml, the
// configured labels and the token environment variable, printing every
// missing or invalid item.
func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	verbose := fs.Bool("verbose", false, "enable debug logging on stderr")
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	logger := newLogger(stderr, *verbose)

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
	owner, repoName, forgeName := "", "", ""

	if cfgErr == nil {
		var remoteErr error
		owner, repoName, remoteErr = history.Remote(ctx, dir)
		if remoteErr != nil {
			fmt.Fprintf(stderr, "release-me: origin remote: %v\n", remoteErr)
			return 1
		}

		var forgeReason string
		var resolveErr error
		forgeName, forgeReason, resolveErr = resolveForgeName(ctx, dir, cfg.Forge)
		if resolveErr != nil {
			fmt.Fprintf(stderr, "release-me: forge: %v\n", resolveErr)
			return 1
		}
		fmt.Fprintf(stdout, "forge: %s, resolved from %s\n", forgeName, forgeReason)
		tokenEnv = forgeTokenEnv(forgeName, cfg.TokenEnv)
	}

	token := os.Getenv(tokenEnv)
	logger.Debug("read token", "env", tokenEnv, "set", token != "")

	var f forge.Forge
	if cfgErr == nil {
		built, forgeErr := buildForge(forgeName, cfg.BaseURL, token)
		if forgeErr != nil {
			fmt.Fprintf(stderr, "release-me: forge: %v\n", forgeErr)
			return 1
		}
		f = built
	}

	missing := doCheck(ctx, cfg, cfgErr, tokenEnv, token, owner, repoName, f)
	for _, m := range missing {
		fmt.Fprintln(stdout, m)
	}
	if len(missing) > 0 {
		return 1
	}
	fmt.Fprintln(stdout, "ok")
	return 0
}

// doCheck validates config, the configured labels and the token, returning
// every missing or invalid item. An empty result means every check passed.
// f is nil when cfgErr is set: the labels cannot be checked against an
// unparsed configuration.
func doCheck(ctx context.Context, cfg config.Config, cfgErr error, tokenEnv, token, owner, repoName string, f forge.Forge) []string {
	var missing []string

	if cfgErr != nil {
		missing = append(missing, fmt.Sprintf("config: %v", cfgErr))
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
