// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"context"
	"slices"
	"strings"
)

// emptyTree is git's well known hash of the empty tree, the diff base used
// for a commit that has no parent.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Touches returns the paths, relative to the repository root, that the
// commit sha changed. A merge commit is compared directly against its
// first parent's tree, the same diff a caller would see for the squashed
// equivalent of the branch it merged, instead of the combined diff git log
// shows for a merge by default. A commit with no parent, the repository's
// root commit, is compared against the empty tree.
func Touches(ctx context.Context, dir, sha string) ([]string, error) {
	parent := emptyTree
	if out, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", sha+"^1"); err == nil {
		parent = strings.TrimSpace(out)
	}

	out, err := run(ctx, dir, "diff-tree", "--no-commit-id", "--name-only", "-r", parent, sha)
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

// Belongs reports whether any of paths, the shape Touches returns, falls
// under componentPath, equal to it or nested inside it, and outside every
// path in exclude: the paths whose changes do not count for the component,
// such as the other components' paths for a root component beside them, or
// the component's own "exclude-paths". componentPath "." is the sentinel
// for the component at the repository root and matches every path,
// including an empty list, unless exclude names one: then it matches every
// path outside exclude.
func Belongs(paths []string, componentPath string, exclude ...string) bool {
	if componentPath == "." && len(exclude) == 0 {
		return true
	}
	for _, p := range paths {
		if within(p, componentPath) && !slices.ContainsFunc(exclude, func(x string) bool { return within(p, x) }) {
			return true
		}
	}
	return false
}

// within reports whether path is dir itself or nested inside it. dir "."
// holds every path.
func within(path, dir string) bool {
	return dir == "." || path == dir || strings.HasPrefix(path, dir+"/")
}

// Counts reports whether a commit counts for the component name at
// componentPath, minus the paths in exclude, as in Belongs: the other
// components' paths for a root component beside them, the component's own
// "exclude-paths", or both. paths are the files the commit changed, the
// shape Touches returns, and named every component name its
// "Release-Component" footers name, as written: a name no component
// carries stays in named and matches none.
//
// A commit named for the component counts for it, whatever files it
// changed. Otherwise a commit that changed files counts through Belongs,
// so one that changed only excluded files counts for none. A commit that
// changed none counts for every component when it carries no footer, the
// way release-please treats an empty commit, and only for the components
// its footers name when it does, so an empty commit whose footers name only
// unknown components counts for none. componentPath "." without exclude
// matches every commit, as in Belongs.
func Counts(paths, named []string, name, componentPath string, exclude []string) bool {
	if slices.Contains(named, name) || componentPath == "." && len(exclude) == 0 {
		return true
	}
	if len(paths) == 0 {
		return len(named) == 0
	}
	return Belongs(paths, componentPath, exclude...)
}
