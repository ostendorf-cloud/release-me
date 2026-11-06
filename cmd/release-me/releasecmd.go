// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/history"
	tagrelease "github.com/ostendorf-cloud/release-me/internal/release"
)

// runRelease implements the release command: it tags and releases every
// merged release pull request carrying the pending label, for every
// configured component or the one named with --component.
func runRelease(args []string, stdout, stderr io.Writer) int {
	verbose, dryRun, component, ok := parseWriteFlags("release", args, stderr)
	if !ok {
		return 2
	}

	out := &runOutputs{}
	ctx := withOutputs(context.Background(), out)
	dir, cfg, f, logger, err := setup(ctx, stderr, verbose)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if err := rejectComponentForLocked(cfg, component); err != nil {
		fmt.Fprintf(stderr, "release-me: %v\n", err)
		return 2
	}
	if err := rejectComponentForShared(cfg, component); err != nil {
		fmt.Fprintf(stderr, "release-me: %v\n", err)
		return 2
	}

	components, err := resolveComponents(cfg, component)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	return finishWrite(stderr, components, out, doRelease(ctx, dir, cfg, component, f, logger, dryRun, stdout))
}

// doRelease tags, releases and swaps the label of every merged release pull
// request carrying the pending label, for every component component
// resolves to.
func doRelease(ctx context.Context, dir string, cfg config.Config, component string, f forge.Forge, logger *slog.Logger, dryRun bool, stdout io.Writer) error {
	owner, repoName, err := history.Remote(ctx, dir)
	if err != nil {
		return fmt.Errorf("release-me: origin remote: %w", err)
	}

	components, err := resolveComponents(cfg, component)
	if err != nil {
		return err
	}
	multi := len(components) > 1
	labels := tagrelease.Labels{
		Pending: cfg.Labels.Pending, Tagged: cfg.Labels.Tagged,
		Alpha: cfg.Labels.Alpha, Beta: cfg.Labels.Beta, Rc: cfg.Labels.Rc,
	}

	if cfg.Versioning == config.VersioningLocked {
		return doReleaseLocked(ctx, cfg, owner, repoName, f, labels, dryRun, logger, stdout)
	}
	if sharedOutsideLocked(cfg) {
		return doReleaseShared(ctx, dir, cfg, owner, repoName, f, labels, dryRun, logger, stdout)
	}

	any := false
	for _, comp := range components {
		results, err := tagrelease.Run(
			ctx, f, owner, repoName, releaseComponent(comp), labels, cfg.DraftUntilAssetsAttach, dryRun, logger,
		)
		if err != nil {
			return fmt.Errorf("release-me: %w", err)
		}
		for _, r := range results {
			any = true
			printLine(stdout, comp.Name, multi, r.Message)
			if r.MajorTagMessage != "" {
				printLine(stdout, comp.Name, multi, r.MajorTagMessage)
			}
			reportReleased(ctx, stdout, dryRun, r)
		}
	}
	if !any {
		fmt.Fprintln(stdout, "nothing to release")
	}
	return nil
}

// releaseComponent carries the settings of comp the release package reads
// over to its own Component.
func releaseComponent(comp config.ResolvedComponent) tagrelease.Component {
	return tagrelease.Component{
		Name: comp.Name, Branch: comp.Branch, TagPrefix: comp.TagPrefix, Multi: comp.Multi, MajorTag: comp.MajorTag,
		SkipGitHubRelease: comp.SkipGitHubRelease,
	}
}

// doReleaseLocked tags and releases every configured component together,
// from the one release pull request a locked family shares: every merged,
// pending-labeled pull request on branch "release-me/main" releases every
// component at once, the same merge commit and version for each.
func doReleaseLocked(
	ctx context.Context, cfg config.Config, owner, repoName string, f forge.Forge, labels tagrelease.Labels, dryRun bool, logger *slog.Logger, stdout io.Writer,
) error {
	resolved := cfg.Resolve()
	comps := make([]tagrelease.Component, 0, len(resolved))
	for _, comp := range resolved {
		comps = append(comps, releaseComponent(comp))
	}
	branch := resolved[0].Branch

	results, err := tagrelease.RunLocked(ctx, f, owner, repoName, branch, comps, labels, cfg.DraftUntilAssetsAttach, dryRun, logger)
	if err != nil {
		return fmt.Errorf("release-me: %w", err)
	}

	multi := len(resolved) > 1
	any := false
	for _, r := range results {
		any = true
		printLine(stdout, r.Name, multi, r.Message)
		if r.MajorTagMessage != "" {
			printLine(stdout, r.Name, multi, r.MajorTagMessage)
		}
		reportReleased(ctx, stdout, dryRun, r)
	}
	if !any {
		fmt.Fprintln(stdout, "nothing to release")
	}
	return nil
}

// doReleaseShared tags and releases every component a merged shared
// release pull request names, on branch "release-me/main", each at its own
// version, then every merged, still pending pull request on a component's
// own branch, "release-me/<name>": one merged right before the switch to
// "release-pull-request: shared" still gets its tag and release instead of
// staying pending forever. The sweep only looks at a component whose own
// branch still exists on the remote: a repository that never ran per
// component, or whose leftover branches are merged and deleted, pays one
// "git ls-remote" instead of listing every component's ever growing
// closed pull request history on every run.
func doReleaseShared(
	ctx context.Context, dir string, cfg config.Config, owner, repoName string, f forge.Forge, labels tagrelease.Labels,
	dryRun bool, logger *slog.Logger, stdout io.Writer,
) error {
	resolved := cfg.Resolve()
	branch := resolved[0].Branch
	comps := make([]tagrelease.Component, 0, len(resolved))
	for _, comp := range resolved {
		comps = append(comps, releaseComponent(comp))
	}

	results, err := tagrelease.RunShared(ctx, f, owner, repoName, branch, comps, labels, cfg.DraftUntilAssetsAttach, dryRun, logger)
	if err != nil {
		return fmt.Errorf("release-me: %w", err)
	}
	ownBranches, err := remoteBranches(ctx, dir, "release-me/*")
	if err != nil {
		return fmt.Errorf("release-me: %s: list the per-component release branches: %w", sharedWho, err)
	}
	for _, comp := range comps {
		own := config.PerComponentBranch(comp.Name)
		if own == branch {
			continue
		}
		if !ownBranches[own] {
			logger.Debug("no per-component release branch on the remote, skipping its leftover sweep",
				"component", comp.Name, "head", own)
			continue
		}
		comp.Branch = own
		leftover, err := tagrelease.Run(ctx, f, owner, repoName, comp, labels, cfg.DraftUntilAssetsAttach, dryRun, logger)
		if err != nil {
			return fmt.Errorf("release-me: %w", err)
		}
		results = append(results, leftover...)
	}

	if len(results) == 0 {
		fmt.Fprintln(stdout, "nothing to release")
		return nil
	}
	for _, r := range results {
		printLine(stdout, r.Name, true, r.Message)
		if r.MajorTagMessage != "" {
			printLine(stdout, r.Name, true, r.MajorTagMessage)
		}
		reportReleased(ctx, stdout, dryRun, r)
	}
	return nil
}

// remoteBranches returns the branches on origin matching pattern, a glob
// under refs/heads/, as a set of names without the refs/heads/ prefix. It
// asks the push URL, the same remote pr pushes the release branches to and
// compares them against. The URL can carry a credential, so an error names
// the remote "origin" instead of it.
func remoteBranches(ctx context.Context, dir, pattern string) (map[string]bool, error) {
	pushURL, err := gitOutput(ctx, dir, "remote", "get-url", "--push", "origin")
	if err != nil {
		return nil, err
	}
	ref := "refs/heads/" + pattern
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", pushURL, ref)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-remote --heads origin %s: %w: %s",
			ref, err, strings.ReplaceAll(strings.TrimSpace(stderr.String()), pushURL, "origin"))
	}
	branches := make(map[string]bool)
	for _, line := range strings.Split(stdout.String(), "\n") {
		_, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		branches[strings.TrimPrefix(ref, "refs/heads/")] = true
	}
	return branches, nil
}
