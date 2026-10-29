// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os/exec"
	"slices"
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
// resolves to. With "delete-release-branch" on, it then deletes every
// release branch a pull request was released from, even when a later
// component failed: released only holds a branch once every tag and
// release of its pull requests succeeded.
func doRelease(ctx context.Context, dir string, cfg config.Config, component string, f forge.Forge, logger *slog.Logger, dryRun bool, stdout io.Writer) error {
	owner, repoName, err := history.Remote(ctx, dir)
	if err != nil {
		return fmt.Errorf("release-me: origin remote: %w", err)
	}

	components, err := resolveComponents(cfg, component)
	if err != nil {
		return err
	}
	labels := tagrelease.Labels{
		Pending: cfg.Labels.Pending, Tagged: cfg.Labels.Tagged,
		Alpha: cfg.Labels.Alpha, Beta: cfg.Labels.Beta, Rc: cfg.Labels.Rc,
	}

	released := releasedBranches{}
	switch {
	case cfg.Versioning == config.VersioningLocked:
		err = doReleaseLocked(ctx, cfg, owner, repoName, f, labels, dryRun, logger, stdout, released)
	case sharedOutsideLocked(cfg):
		err = doReleaseShared(ctx, dir, cfg, owner, repoName, f, labels, dryRun, logger, stdout, released)
	default:
		err = doReleasePerComponent(ctx, cfg, components, owner, repoName, f, labels, dryRun, logger, stdout, released)
	}
	if cfg.DeleteReleaseBranch {
		deleteReleaseBranches(ctx, dir, f, owner, repoName, released, dryRun, logger, stdout)
	}
	return err
}

// doReleasePerComponent tags and releases every merged, pending-labeled
// pull request on each component's own release branch.
func doReleasePerComponent(
	ctx context.Context, cfg config.Config, components []config.ResolvedComponent, owner, repoName string, f forge.Forge,
	labels tagrelease.Labels, dryRun bool, logger *slog.Logger, stdout io.Writer, released releasedBranches,
) error {
	multi := len(components) > 1
	any := false
	for _, comp := range components {
		results, err := tagrelease.Run(
			ctx, f, owner, repoName,
			tagrelease.Component{Name: comp.Name, Branch: comp.Branch, TagPrefix: comp.TagPrefix, Multi: comp.Multi, MajorTag: comp.MajorTag},
			labels, cfg.DraftUntilAssetsAttach, dryRun, logger,
		)
		if err != nil {
			return fmt.Errorf("release-me: %w", err)
		}
		released.add(comp.Branch, results)
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

// doReleaseLocked tags and releases every configured component together,
// from the one release pull request a locked family shares: every merged,
// pending-labeled pull request on branch "release-me/main" releases every
// component at once, the same merge commit and version for each.
func doReleaseLocked(
	ctx context.Context, cfg config.Config, owner, repoName string, f forge.Forge, labels tagrelease.Labels, dryRun bool, logger *slog.Logger,
	stdout io.Writer, released releasedBranches,
) error {
	resolved := cfg.Resolve()
	comps := make([]tagrelease.Component, 0, len(resolved))
	for _, comp := range resolved {
		comps = append(comps, tagrelease.Component{Name: comp.Name, Branch: comp.Branch, TagPrefix: comp.TagPrefix, Multi: comp.Multi, MajorTag: comp.MajorTag})
	}
	branch := resolved[0].Branch

	results, err := tagrelease.RunLocked(ctx, f, owner, repoName, branch, comps, labels, cfg.DraftUntilAssetsAttach, dryRun, logger)
	if err != nil {
		return fmt.Errorf("release-me: %w", err)
	}
	released.add(branch, results)

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
//
// With "delete-release-branch" on, a swept branch is deleted once its
// leftover pull request is released, so the sweep has nothing left to
// find there, unless "draft-until-assets-attach" is on: the sweep is the
// only thing that resumes a leftover's unpublished draft after a failed
// publish, and it only looks while the branch exists, so the branch stays.
func doReleaseShared(
	ctx context.Context, dir string, cfg config.Config, owner, repoName string, f forge.Forge, labels tagrelease.Labels,
	dryRun bool, logger *slog.Logger, stdout io.Writer, released releasedBranches,
) error {
	resolved := cfg.Resolve()
	branch := resolved[0].Branch
	comps := make([]tagrelease.Component, 0, len(resolved))
	for _, comp := range resolved {
		comps = append(comps, tagrelease.Component{Name: comp.Name, Branch: comp.Branch, TagPrefix: comp.TagPrefix, Multi: comp.Multi, MajorTag: comp.MajorTag})
	}

	results, err := tagrelease.RunShared(ctx, f, owner, repoName, branch, comps, labels, cfg.DraftUntilAssetsAttach, dryRun, logger)
	if err != nil {
		return fmt.Errorf("release-me: %w", err)
	}
	released.add(branch, results)
	ownBranches, err := remoteBranches(ctx, dir, "release-me/*")
	if err != nil {
		return fmt.Errorf("release-me: %s: list the per-component release branches: %w", sharedWho, err)
	}
	for _, comp := range comps {
		own := config.PerComponentBranch(comp.Name)
		if own == branch {
			continue
		}
		if _, ok := ownBranches[own]; !ok {
			logger.Debug("no per-component release branch on the remote, skipping its leftover sweep",
				"component", comp.Name, "head", own)
			continue
		}
		comp.Branch = own
		leftover, err := tagrelease.Run(ctx, f, owner, repoName, comp, labels, cfg.DraftUntilAssetsAttach, dryRun, logger)
		if err != nil {
			return fmt.Errorf("release-me: %w", err)
		}
		if cfg.DraftUntilAssetsAttach {
			if len(leftover) > 0 {
				logger.Debug("keeping the per-component release branch, the sweep resumes an unpublished draft only while it exists",
					"component", comp.Name, "head", own)
			}
		} else {
			released.add(own, leftover)
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

// releasedBranches maps a release branch to the head commits of the pull
// requests this run tagged and released from it, each to its pull request
// number, for deleteReleaseBranches. A branch is only added once the run
// over it succeeded, every tag and release of every pull request on it
// included.
type releasedBranches map[string]map[string]int

// add records every result in results with a head commit as released from
// branch. A resumed draft carries none: an earlier run tagged it, and the
// branch was that run's to delete.
func (b releasedBranches) add(branch string, results []tagrelease.Result) {
	for _, r := range results {
		if r.HeadSHA == "" {
			continue
		}
		if b[branch] == nil {
			b[branch] = map[string]int{}
		}
		b[branch][r.HeadSHA] = r.PullRequestNumber
	}
}

// deleteReleaseBranches deletes every branch in released through the forge,
// "delete-release-branch"'s job, once release has tagged and released what
// was merged from it. A branch is kept when it is already gone, when its
// tip is no longer the head commit a released pull request merged with,
// since pr pushed the next release onto it since, and while an open pull
// request uses it. Nothing here fails the run: the tags and releases exist
// already, so a failed lookup or delete is a warning and the branch stays
// for a later run or a human. dryRun reports what it would delete and
// deletes nothing.
func deleteReleaseBranches(
	ctx context.Context, dir string, f forge.Forge, owner, repoName string, released releasedBranches, dryRun bool,
	logger *slog.Logger, stdout io.Writer,
) {
	if len(released) == 0 {
		return
	}
	names := slices.Sorted(maps.Keys(released))
	tips, err := remoteBranches(ctx, dir, names...)
	if err != nil {
		logger.Warn("release branches kept: list them on the remote", "error", err)
		return
	}
	for _, branch := range names {
		tip, ok := tips[branch]
		if !ok {
			logger.Debug("release branch already deleted", "head", branch)
			continue
		}
		number, ok := released[branch][tip]
		if !ok {
			logger.Debug("release branch kept: it moved past the released pull request", "head", branch, "tip", tip)
			continue
		}
		open, err := f.PullRequests(ctx, owner, repoName, branch, "open")
		if err != nil {
			logger.Warn("release branch kept: list its open pull requests", "head", branch, "number", number, "error", err)
			continue
		}
		if len(open) > 0 {
			logger.Debug("release branch kept: an open pull request uses it", "head", branch, "open", open[0].Number)
			continue
		}
		if dryRun {
			fmt.Fprintf(stdout, "would delete release branch %s\n", branch)
			continue
		}
		if err := f.DeleteBranch(ctx, owner, repoName, branch); err != nil {
			logger.Warn("release branch kept: delete it, the release itself succeeded", "head", branch, "number", number, "error", err)
			continue
		}
		fmt.Fprintf(stdout, "deleted release branch %s\n", branch)
	}
}

// remoteBranches returns the branches on origin matching every pattern, a
// glob or a name under refs/heads/, as a map from the name without the
// refs/heads/ prefix to the commit it points at. It asks the push URL, the
// same remote pr pushes the release branches to and compares them against.
// The URL can carry a credential, so an error names the remote "origin"
// instead of it.
func remoteBranches(ctx context.Context, dir string, patterns ...string) (map[string]string, error) {
	pushURL, err := gitOutput(ctx, dir, "remote", "get-url", "--push", "origin")
	if err != nil {
		return nil, err
	}
	refs := make([]string, 0, len(patterns))
	for _, p := range patterns {
		refs = append(refs, "refs/heads/"+p)
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"ls-remote", "--heads", pushURL}, refs...)...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-remote --heads origin %s: %w: %s",
			strings.Join(refs, " "), err, strings.ReplaceAll(strings.TrimSpace(stderr.String()), pushURL, "origin"))
	}
	branches := make(map[string]string)
	for _, line := range strings.Split(stdout.String(), "\n") {
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		branches[strings.TrimPrefix(ref, "refs/heads/")] = sha
	}
	return branches, nil
}
