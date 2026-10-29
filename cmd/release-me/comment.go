// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

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

	if err := doComment(ctx, dir, cfg, f, logger, owner, repoName, flags, stdout); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	return 0
}

// doComment implements runComment against an already set up configuration
// and forge, so tests exercise it with a fake forge instead of the
// network. dir is the local git checkout doPR needs to recompute a release
// pull request's title and changelog immediately after a "release-as"
// command: it is unused by every other command and every rejection path.
func doComment(
	ctx context.Context, dir string, cfg config.Config, f forge.Forge, logger *slog.Logger, owner, repoName string, flags commentFlags, stdout io.Writer,
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
		target, refusal := releaseAsTarget(cfg, pr, outcome.Component)
		if refusal != "" {
			return reply(ctx, f, owner, repoName, flags.prNumber, stdout, "the %q command was refused: %s.", outcome.Command, refusal)
		}
		var newBody string
		if target != "" {
			newBody = releasepr.SetComponentVersionOverride(pr.Body, target, outcome.Version)
		} else {
			newBody = releasepr.SetVersionOverride(pr.Body, outcome.Version)
		}
		if _, err := f.UpdatePullRequest(ctx, owner, repoName, flags.prNumber, pr.Title, newBody); err != nil {
			return fmt.Errorf("release-me: comment: update pull request #%d: %w", flags.prNumber, err)
		}

		// The override is durably written at this point; everything from
		// here on is best-effort. Recompute every configured component's
		// release pull request now, the same logic and the same "pr" code
		// path the next scheduled run would use, so this one does not have
		// to wait for it. doPR itself returning no error only means the
		// recompute ran; it says nothing about whether applyVersionOverride
		// actually let this exact version win against the last released
		// one, so an overrideRecorder rides along on ctx to answer that
		// specific question for this pull request.
		rec := &overrideRecorder{}
		recomputeErr := doPR(withOverrideRecorder(ctx, rec), dir, cfg, "", f, logger, false, stdout)
		applied, known := rec.results[overrideKey{number: flags.prNumber, component: target}]

		switch {
		case recomputeErr != nil || !known:
			// A failure here, or doPR never reporting back on this exact
			// pull request at all, never undoes the write above and never
			// fails the command: the next scheduled run still applies the
			// override exactly as it always has, so the fallback this
			// reply names is real, not a false promise.
			if recomputeErr != nil {
				logger.Error("release-as: could not recompute the title and changelog immediately, "+
					"falling back to the next scheduled run", "pr", flags.prNumber, "error", recomputeErr)
			} else {
				logger.Warn("release-as: could not confirm whether the immediate recompute reached this "+
					"pull request, falling back to the next scheduled run", "pr", flags.prNumber)
			}
			return reply(ctx, f, owner, repoName, flags.prNumber, stdout,
				"forced the next version%s to %s. The override is saved, but updating this pull request's title "+
					"and changelog right now did not succeed; the next scheduled run will update them instead.",
				ofComponent(target), outcome.Version)

		case !applied:
			// Written for visibility, same as a hand-edited override
			// always was, but applyVersionOverride already decided, same
			// as it always has, that it does not win: it does not parse,
			// which commentcmd.Parse already ruled out above, or it is not
			// strictly greater than the last released version.
			return reply(ctx, f, owner, repoName, flags.prNumber, stdout,
				"the release-as version %q was refused: it is not greater than the last released version%s, "+
					"so this pull request's title and changelog were not updated.", outcome.Version, ofComponent(target))

		default:
			return reply(ctx, f, owner, repoName, flags.prNumber, stdout,
				"forced the next version%s to %s and updated this pull request's title and changelog to match.",
				ofComponent(target), outcome.Version)
		}

	default: // commentcmd.KindReleaseNotes
		newBody := releasepr.SetNotes(pr.Body, outcome.Notes)
		if _, err := f.UpdatePullRequest(ctx, owner, repoName, flags.prNumber, pr.Title, newBody); err != nil {
			return fmt.Errorf("release-me: comment: update pull request #%d: %w", flags.prNumber, err)
		}
		return reply(ctx, f, owner, repoName, flags.prNumber, stdout, "set the release notes.")
	}
}

// releaseAsTarget decides which component a "release-as" command on pr
// forces the version of. target is "" for the one version every release
// pull request but a shared one with independent versions carries: a
// component's own, a locked family's or a linked family's, where naming a
// component is refused, since there is nothing to pick between. In a
// shared release pull request with independent versions, target is the
// component named, which must be one the pull request releases, or the one
// component it releases when none is named; naming none while it releases
// several is refused as ambiguous. refusal, when not "", is the reason the
// command is refused, and nothing is written. Which components the pull
// request releases is read from its body's component headings or details
// summaries, in whichever layout the body carries, not the one
// release-me.yaml sets now: the comment path's source of truth, so a
// heading added or deleted by hand changes what the command accepts, and
// the recompute that follows still applies an override only to a
// component it actually releases.
func releaseAsTarget(cfg config.Config, pr forge.PullRequest, component string) (target, refusal string) {
	if !sharedOutsideLocked(cfg) || cfg.Versioning != config.VersioningIndependent {
		if component == "" {
			return "", ""
		}
		why := "this pull request releases one component"
		if cfg.SharedPullRequest() {
			why = "every component in this pull request shares one version"
		}
		return "", fmt.Sprintf("%s, so release-as takes no component, only `release-as X.Y.Z`", why)
	}

	names := releasepr.ComponentNames(releasepr.ExtractChangelog(pr.Body))
	switch {
	case len(names) == 0:
		return "", "this pull request carries no component sections: with `release-pull-request: shared`, " +
			"only the shared release pull request takes release-as"
	case component == "" && len(names) == 1:
		return names[0], ""
	case component == "":
		return "", fmt.Sprintf("this pull request releases %s, each at its own version: name the component, "+
			"`release-as <component> X.Y.Z`", strings.Join(names, ", "))
	case !slices.Contains(names, component):
		return "", fmt.Sprintf("component %q is not part of this pull request, which releases %s", component, strings.Join(names, ", "))
	default:
		return component, ""
	}
}

// ofComponent renders " of <name>" for a reply naming the component a
// release-as command forced, "" when it names none.
func ofComponent(name string) string {
	if name == "" {
		return ""
	}
	return " of " + name
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
