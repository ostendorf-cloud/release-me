// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// Entry is one first parent commit since the last tag.
type Entry struct {
	// PullRequestNumber is the pull request number parsed from a GitHub
	// "Merge pull request #N from ..." subject, or a GitLab merge request
	// number read from the body of a "Merge branch '...' into '...'"
	// subject, or 0 for a direct commit.
	PullRequestNumber int
	// Subject is the commit subject: the merge subject for a merge commit,
	// or the commit's own subject for a direct commit.
	Subject string
	// Body is the commit body. For a merge commit it is the fallback text
	// used when the forge has no pull request for PullRequestNumber.
	Body string
}

// mergeSubject matches GitHub's merge commit subject, naming the pull
// request number directly.
var mergeSubject = regexp.MustCompile(`^Merge pull request #(\d+) from `)

// gitlabMergeSubject matches GitLab's merge commit subject, which names
// neither the source nor the merge request; the number is read from the
// body instead, through gitlabMergeRequest.
var gitlabMergeSubject = regexp.MustCompile(`^Merge branch '.+' into '.+'$`)

// gitlabMergeRequest matches the merge request reference GitLab appends to
// the merge commit body, "See merge request group/project!N".
var gitlabMergeRequest = regexp.MustCompile(`See merge request \S*!(\d+)`)

const (
	recordSep = "\x1e"
	fieldSep  = "\x1f"
)

// Walk reads the first parent commit history from since, exclusive, to HEAD.
// since is a tag name, or "" to read the whole history.
func Walk(ctx context.Context, dir, since string) ([]Entry, error) {
	rangeArg := "HEAD"
	if since != "" {
		rangeArg = since + "..HEAD"
	}

	out, err := run(ctx, dir, "log", "--first-parent", "--format="+recordSep+"%s"+fieldSep+"%b", rangeArg)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, record := range strings.Split(out, recordSep) {
		if record == "" {
			continue
		}
		subject, body, _ := strings.Cut(record, fieldSep)
		body = strings.TrimRight(body, "\n")

		entry := Entry{Subject: subject, Body: body}
		switch {
		case mergeSubject.MatchString(subject):
			if m := mergeSubject.FindStringSubmatch(subject); m != nil {
				if number, err := strconv.Atoi(m[1]); err == nil {
					entry.PullRequestNumber = number
				}
			}
		case gitlabMergeSubject.MatchString(subject):
			if m := gitlabMergeRequest.FindStringSubmatch(body); m != nil {
				if number, err := strconv.Atoi(m[1]); err == nil {
					entry.PullRequestNumber = number
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
