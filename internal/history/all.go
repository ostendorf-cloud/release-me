// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"context"
	"strconv"
	"strings"
)

// AllEntries lists every commit reachable since, exclusive, to HEAD, merge
// commits excluded, each as its own Entry with Subject and Body from its
// own commit message. A commit introduced by a first parent merge commit,
// a genuine "Merge pull request #N from ..." on the main line, carries
// that merge's pull request number; a commit reachable only through the
// first parent chain itself carries 0. Order matches Walk: newest first,
// with --topo-order so a commit always sorts after every one of its
// descendants, regardless of how closely their timestamps tie.
func AllEntries(ctx context.Context, dir, since string) ([]Entry, error) {
	rng := rangeArg(since)

	out, err := run(ctx, dir, "log", "--no-merges", "--topo-order", logFormat, rng)
	if err != nil {
		return nil, err
	}
	records := parseRecords(out)

	attribution, err := mergeAttribution(ctx, dir, rng)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(records))
	for _, r := range records {
		entries = append(entries, Entry{
			SHA: r.SHA, AuthorName: r.Author, Subject: r.Subject, Body: r.Body,
			PullRequestNumber: attribution[r.SHA],
		})
	}
	return entries, nil
}

// mergeAttribution maps every commit a first parent merge commit introduced
// in rng to that merge's pull request number, parsed from its subject the
// same way Walk parses it. A merge whose subject does not match that
// pattern contributes no mapping: the commits it introduced stay
// unattributed, pull request number 0.
func mergeAttribution(ctx context.Context, dir, rng string) (map[string]int, error) {
	out, err := run(ctx, dir, "log", "--first-parent", "--merges", "--format="+recordSep+"%H"+fieldSep+"%s", rng)
	if err != nil {
		return nil, err
	}

	attribution := map[string]int{}
	for _, chunk := range strings.Split(out, recordSep) {
		if chunk == "" {
			continue
		}
		sha, subject, _ := strings.Cut(chunk, fieldSep)

		m := mergeSubject.FindStringSubmatch(subject)
		if m == nil {
			continue
		}
		number, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}

		rng, isMerge, err := secondParentRange(ctx, dir, sha)
		if err != nil {
			return nil, err
		}
		if !isMerge {
			continue
		}

		out, err := run(ctx, dir, "rev-list", rng)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(out, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				attribution[line] = number
			}
		}
	}
	return attribution, nil
}

// secondParentRange resolves the git range "sha^1..sha^2" for a merge
// commit sha: every commit reachable from its second parent but not its
// first. isMerge is false, with no error, for a commit with a single
// parent, not a merge.
func secondParentRange(ctx context.Context, dir, sha string) (rng string, isMerge bool, err error) {
	parent2, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", sha+"^2")
	if err != nil {
		return "", false, nil
	}
	parent1, err := run(ctx, dir, "rev-parse", sha+"^1")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(parent1) + ".." + strings.TrimSpace(parent2), true, nil
}
