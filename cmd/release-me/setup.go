// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	forgegithub "github.com/ostendorf-cloud/release-me/internal/forge/github"
	forgegitlab "github.com/ostendorf-cloud/release-me/internal/forge/gitlab"
	"github.com/ostendorf-cloud/release-me/internal/history"
)

// sshRemoteHost and urlRemoteHost read the host out of an origin remote
// URL, in the same SSH shorthand, ssh:// or HTTPS forms history.Remote
// parses, to guess the forge when release-me.yaml names none.
var (
	sshRemoteHost = regexp.MustCompile(`^git@([^:]+):`)
	urlRemoteHost = regexp.MustCompile(`^(?:https?|ssh)://(?:[^@/]+@)?([^/]+)/`)
)

// setup resolves the repository root, loads release-me.yaml from it and
// builds the forge the configured "forge" key or the origin remote names.
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

	name, reason, err := resolveForgeName(ctx, root, cfg.Forge)
	if err != nil {
		return "", config.Config{}, nil, logger, fmt.Errorf("release-me: %w", err)
	}
	logger.Debug("resolved forge", "forge", name, "reason", reason)

	tokenEnv := forgeTokenEnv(name, cfg.TokenEnv)
	token := os.Getenv(tokenEnv)
	logger.Debug("read token", "env", tokenEnv, "set", token != "")

	built, err := buildForge(name, cfg.BaseURL, token)
	if err != nil {
		return "", config.Config{}, nil, logger, fmt.Errorf("release-me: forge: %w", err)
	}
	return root, cfg, built, logger, nil
}

// resolveForgeName picks "github" or "gitlab": the configured forge key
// when set, otherwise "gitlab" when the origin remote's host names gitlab,
// "github" otherwise. reason explains the pick, for --verbose logging and
// for check to report.
func resolveForgeName(ctx context.Context, dir, configured string) (name, reason string, err error) {
	if configured != "" {
		return configured, "the forge key in release-me.yaml", nil
	}
	host, err := originRemoteHost(ctx, dir)
	if err != nil {
		return "", "", err
	}
	if strings.Contains(strings.ToLower(host), "gitlab") {
		return "gitlab", fmt.Sprintf("origin remote host %q names gitlab", host), nil
	}
	return "github", fmt.Sprintf("origin remote host %q does not name gitlab", host), nil
}

// forgeTokenEnv applies the per-forge default token environment variable
// when release-me.yaml leaves token-env at config.DefaultTokenEnv, the
// GitHub oriented default: a gitlab forge then reads
// forgegitlab.DefaultTokenEnv instead.
func forgeTokenEnv(forgeName, configured string) string {
	if forgeName == "gitlab" && configured == config.DefaultTokenEnv {
		return forgegitlab.DefaultTokenEnv
	}
	return configured
}

// buildForge constructs the named forge's client.
func buildForge(name, baseURL, token string) (forge.Forge, error) {
	switch name {
	case "github":
		return forgegithub.New(token)
	case "gitlab":
		return forgegitlab.New(baseURL, token), nil
	default:
		return nil, fmt.Errorf("forge %q is neither %q nor %q", name, "github", "gitlab")
	}
}

// originRemoteHost reads the host out of the origin remote URL, in the SSH
// shorthand "git@host:owner/repo", "ssh://[user@]host/owner/repo" or
// "https://host/owner/repo" form, the same forms internal/history.Remote
// parses for the owner and repository name.
func originRemoteHost(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("origin remote: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	url := strings.TrimSpace(stdout.String())

	if m := sshRemoteHost.FindStringSubmatch(url); m != nil {
		return m[1], nil
	}
	if m := urlRemoteHost.FindStringSubmatch(url); m != nil {
		return m[1], nil
	}
	return "", fmt.Errorf("origin remote %q is neither an SSH nor an HTTPS URL", url)
}
