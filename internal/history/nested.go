// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"context"
)

// SubCommits lists the commits a merge commit sha introduced, oldest
// first, the order they were made on the branch: every commit reachable
// from its second parent but not its first. PullRequestNumber is always 0;
// a sub commit is never itself a pull request. It returns no entries, and
// no error, for a commit with a single parent, not a merge.
func SubCommits(ctx context.Context, dir, sha string) ([]Entry, error) {
	rng, isMerge, err := secondParentRange(ctx, dir, sha)
	if err != nil {
		return nil, err
	}
	if !isMerge {
		return nil, nil
	}

	out, err := run(ctx, dir, "log", "--reverse", logFormat, rng)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, r := range parseRecords(out) {
		entries = append(entries, Entry{SHA: r.SHA, AuthorName: r.Author, Subject: r.Subject, Body: r.Body})
	}
	return entries, nil
}
