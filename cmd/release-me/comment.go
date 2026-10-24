// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/ostendorf-cloud/release-me/internal/commentcmd"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/history"
	"github.com/ostendorf-cloud/release-me/internal/releasepr"
)

// runComment implements the comment command: one GitHub issue_comment
// event, read as flags from the workflow that invokes it. A comment naming
// the configured mention, "@release-me" by default, on the release pull
// request, from a commenter whose author association is OWNER, MEMBER or
// COLLABORATOR, applies a "release-as" or "release-notes" command to it;
// every other matching comment gets a reply naming why nothing happened. A
// comment that never names the mention at all is left alone entirely: it
// was not directed at release-me.
//
// No reply this command ever posts contains the literal configured
// mention, so the workflow's own comment never matches Parse again on the
// next issue_comment event: that, together with the workflow job's own
// "github.event.comment.user.type != 'Bot'" guard, is what keeps a reply
// from re-triggering itself. See TestReplyTemplatesNeverReTriggerTheMention.
func runComment(args []string, stdout, stderr io.Writer) int {
	flags, ok := parseCommentFlags(args, stderr)
	if !ok {
		return 2
	}

	ctx := context.Background()
	dir, cfg, f, logger, err := setup(ctx, stderr, flags.verbose)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	owner, repoName, err := history.Remote(ctx, dir)
	if err != nil {
		fmt.Fprintf(stderr, "release-me: origin remote: %v\n", err)
		return 1
	}

	if err := doComment(ctx, cfg, f, logger, owner, repoName, flags, stdout); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	return 0
}

// doComment implements runComment against an already set up configuration
// and forge, so tests exercise it with a fake forge instead of the
// network.
func doComment(
	ctx context.Context, cfg config.Config, f forge.Forge, logger *slog.Logger, owner, repoName string, flags commentFlags, stdout io.Writer,
) error {
	outcome := commentcmd.Parse(flags.body, cfg.CommentMention, flags.association)
	if !outcome.Matched {
		logger.Debug("comment does not name the configured mention, ignoring", "pr", flags.prNumber)
		return nil
	}

	// Authorization is checked before anything else, including which pull
	// request this is: an unauthorized commenter gets the same one reply
	// regardless of where they posted, never a free-standing comment on an
	// arbitrary pull request confirming whether it is the release one.
	if !outcome.Allowed {
		return reply(ctx, f, owner, repoName, flags.prNumber, stdout,
			"this command was refused: the commenter's GitHub association with this repository is "+
				"none of OWNER, MEMBER or COLLABORATOR.")
	}

	pr, err := f.PullRequest(ctx, owner, repoName, flags.prNumber)
	if err != nil {
		return fmt.Errorf("release-me: comment: pull request #%d: %w", flags.prNumber, err)
	}

	if !isReleasePullRequest(pr.Labels, cfg.Labels) {
		return reply(ctx, f, owner, repoName, flags.prNumber, stdout,
			"this command was refused: pull request #%d is not a release pull request. "+
				"release-as and release-notes only work there.", flags.prNumber)
	}

	switch {
	case outcome.Command == "":
		return reply(ctx, f, owner, repoName, flags.prNumber, stdout,
			"no recognized command followed the mention. "+
				"Available commands: `release-as X.Y.Z` and `release-notes <text>`.")

	case !outcome.Valid:
		return reply(ctx, f, owner, repoName, flags.prNumber, stdout, "the %q command was refused: %s.", outcome.Command, outcome.Reason)

	case outcome.Command == commentcmd.KindReleaseAs:
		newBody := releasepr.SetVersionOverride(pr.Body, outcome.Version)
		if _, err := f.UpdatePullRequest(ctx, owner, repoName, flags.prNumber, pr.Title, newBody); err != nil {
			return fmt.Errorf("release-me: comment: update pull request #%d: %w", flags.prNumber, err)
		}
		return reply(ctx, f, owner, repoName, flags.prNumber, stdout,
			"forced the next version to %s. This pull request's title and changelog still show the "+
				"previously computed version until the next scheduled run updates them; merging before "+
				"then releases that old version, not %s.", outcome.Version, outcome.Version)

	default: // commentcmd.KindReleaseNotes
		newBody := releasepr.SetNotes(pr.Body, outcome.Notes)
		if _, err := f.UpdatePullRequest(ctx, owner, repoName, flags.prNumber, pr.Title, newBody); err != nil {
			return fmt.Errorf("release-me: comment: update pull request #%d: %w", flags.prNumber, err)
		}
		return reply(ctx, f, owner, repoName, flags.prNumber, stdout, "set the release notes.")
	}
}

// isReleasePullRequest reports whether prLabels carries one of labels'
// configured release labels: pending, tagged, or one of the enabled
// pre-release types. A comment command only ever acts on a release pull
// request, never an ordinary one; this is the same signal the labeled and
// unlabeled workflow trigger already uses to tell the two apart.
func isReleasePullRequest(prLabels []string, labels config.Labels) bool {
	for _, l := range []string{labels.Pending, labels.Tagged, labels.Alpha, labels.Beta, labels.Rc} {
		if l != "" && slices.Contains(prLabels, l) {
			return true
		}
	}
	return false
}

// reply posts msg, formatted with args, as a new comment on pull request
// number, and prints the same text on stdout, so a workflow run's own log
// shows what happened without opening the pull request.
func reply(ctx context.Context, f forge.Forge, owner, repoName string, number int, stdout io.Writer, msg string, args ...any) error {
	text := fmt.Sprintf(msg, args...)
	if err := f.CreateComment(ctx, owner, repoName, number, text); err != nil {
		return fmt.Errorf("release-me: comment: reply on pull request #%d: %w", number, err)
	}
	fmt.Fprintln(stdout, text)
	return nil
}
