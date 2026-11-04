// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"context"
	"regexp"
	"strconv"
)

// squashSubject matches a commit subject ending in a trailing "(#N)", the
// shape a squash-merged pull request's commit carries on GitHub.
var squashSubject = regexp.MustCompile(`^(.*) \(#(\d+)\)$`)

// WalkSquash reads the same first parent commits Walk does, since,
// exclusive, to HEAD, but resolves the pull request number from a trailing
// "(#N)" on a commit's own subject instead of a merge commit's subject: the
// shape a squash merge leaves behind, since it creates no merge commit of
// its own. Subject is the commit subject with that suffix removed, and
// Squashed is set, marking it as the real title a caller must never
// replace, forge reachable or not; Body is unchanged. A genuine merge
// commit, title "Merge pull request #N from ...", is still resolved the
// way Walk resolves it, Squashed false, and remains one entry.
func WalkSquash(ctx context.Context, dir, since string) ([]Entry, error) {
	out, err := run(ctx, dir, "log", "--first-parent", logFormat, rangeArg(since))
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, r := range parseRecords(out) {
		entries = append(entries, squashEntry(r))
	}
	return entries, nil
}

func squashEntry(r record) Entry {
	if m := mergeSubject.FindStringSubmatch(r.Subject); m != nil {
		if number, err := strconv.Atoi(m[1]); err == nil {
			return Entry{SHA: r.SHA, AuthorName: r.Author, PullRequestNumber: number, Subject: r.Subject, Body: r.Body}
		}
	}
	if m := squashSubject.FindStringSubmatch(r.Subject); m != nil {
		if number, err := strconv.Atoi(m[2]); err == nil {
			return Entry{SHA: r.SHA, AuthorName: r.Author, PullRequestNumber: number, Subject: m[1], Body: r.Body, Squashed: true}
		}
	}
	return Entry{SHA: r.SHA, AuthorName: r.Author, Subject: r.Subject, Body: r.Body}
}
