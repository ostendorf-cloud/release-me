// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	"github.com/ostendorf-cloud/release-me/internal/forge/forgetest"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// newModeRepo creates a repository one changelog source mode test exercises:
// a merge (pull request #10, two commits on its branch), a commit shaped
// like a squash merge (a trailing "(#11)"), and a plain direct commit, all
// since the v0.1.0 tag.
func newModeRepo(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	commitEntry(t, dir, "chore: initial commit")
	tagRepo(t, dir)

	runGit(t, dir, "checkout", "-q", "-b", "feat-branch")
	commitEntry(t, dir, "feat(widget): add the widget")
	commitEntry(t, dir, "fix(widget): correct a bug")
	runGit(t, dir, "checkout", "-q", "main")
	runGit(t, dir, "merge", "--no-ff", "-q", "-m", "Merge pull request #10 from owner/feat-branch", "feat-branch")

	commitEntry(t, dir, "feat(gadget): add the gadget (#11)")
	commitEntry(t, dir, "fix: direct hotfix")
	return dir
}

// resolvedComponent builds the single implicit "main" component for source,
// the same way a real release-me.yaml without "components" resolves.
func resolvedComponent(source string) config.ResolvedComponent {
	cfg := config.Default()
	cfg.ChangelogSource = source
	return cfg.Resolve()[0]
}

func findEntry(entries []changelog.Entry, description string) (changelog.Entry, bool) {
	for _, e := range entries {
		if e.Description == description {
			return e, true
		}
	}
	return changelog.Entry{}, false
}

// countingForge wraps a forgetest.Fake, counting how many times
// PullRequest is called for each number, so a test can assert the forge
// was read once per merge commit, not once per commit it introduced.
type countingForge struct {
	*forgetest.Fake
	calls map[int]int
}

func newCountingForge(f *forgetest.Fake) *countingForge {
	return &countingForge{Fake: f, calls: map[int]int{}}
}

func (c *countingForge) PullRequest(ctx context.Context, owner, repo string, number int) (forge.PullRequest, error) {
	c.calls[number]++
	return c.Fake.PullRequest(ctx, owner, repo, number)
}

func TestComputeReleaseInFirstParentMode(t *testing.T) {
	dir := newModeRepo(t)
	comp := resolvedComponent(config.ChangelogSourceFirstParent)
	f := newFake()
	f.Responses = map[int]forgetest.Response{
		10: {PullRequest: forge.PullRequest{Title: "feat(widget): add the widgets", URL: "https://example.invalid/pull/10"}},
	}

	rel, released, err := computeRelease(t.Context(), dir, config.Default(), comp, f, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease() error = %v", err)
	}
	if !released || rel.version.Bump != version.BumpMinor {
		t.Fatalf("released = %v, bump = %v, want a minor bump from the merge's feat", released, rel.version.Bump)
	}

	if e, ok := findEntry(rel.entries, "add the widgets"); !ok || e.PullRequestNumber != 10 || e.Scope != "widget" {
		t.Fatalf("entries = %+v, want the merge resolved through the forge", rel.entries)
	}
	// first-parent never special-cases a trailing "(#N)": it stays part of
	// the direct commit's own description, unresolved.
	if e, ok := findEntry(rel.entries, "add the gadget (#11)"); !ok || e.PullRequestNumber != 0 {
		t.Fatalf("entries = %+v, want the gadget commit untouched, with its number literal", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "direct hotfix"); !ok {
		t.Fatalf("entries = %+v, want the direct hotfix commit", rel.entries)
	}
}

func TestComputeReleaseInSquashMode(t *testing.T) {
	dir := newModeRepo(t)
	comp := resolvedComponent(config.ChangelogSourceSquash)
	f := newFake()
	f.Responses = map[int]forgetest.Response{
		10: {PullRequest: forge.PullRequest{Title: "feat(widget): add the widgets", URL: "https://example.invalid/pull/10"}},
		// the forge's own title is never used for a squashed commit, only
		// its link: the gadget commit's cleaned subject is the title.
		11: {PullRequest: forge.PullRequest{Title: "feat(gadget): a retitled pull request", URL: "https://example.invalid/pull/11"}},
	}

	rel, released, err := computeRelease(t.Context(), dir, config.Default(), comp, f, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease() error = %v", err)
	}
	if !released {
		t.Fatal("released = false, want true")
	}

	// the trailing "(#11)" resolves the pull request number and is
	// stripped from the description, which stays the commit's own.
	if e, ok := findEntry(rel.entries, "add the gadget"); !ok || e.PullRequestNumber != 11 || e.URL != "https://example.invalid/pull/11" {
		t.Fatalf("entries = %+v, want the gadget commit resolved as pull request #11, its own title kept", rel.entries)
	}
	// a genuine merge commit still uses the pull request's own title in
	// squash mode, and is still one entry.
	if e, ok := findEntry(rel.entries, "add the widgets"); !ok || e.PullRequestNumber != 10 {
		t.Fatalf("entries = %+v, want the merge commit resolved as pull request #10", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "direct hotfix"); !ok {
		t.Fatalf("entries = %+v, want the direct hotfix commit", rel.entries)
	}
}

func TestComputeReleaseInAllMode(t *testing.T) {
	dir := newModeRepo(t)
	comp := resolvedComponent(config.ChangelogSourceAll)
	fake := newFake()
	fake.Responses = map[int]forgetest.Response{
		10: {PullRequest: forge.PullRequest{Title: "ignored: all mode never uses a pull request title", URL: "https://example.invalid/pull/10"}},
	}
	f := newCountingForge(fake)

	rel, released, err := computeRelease(t.Context(), dir, config.Default(), comp, f, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease() error = %v", err)
	}
	if !released || rel.version.Bump != version.BumpMinor {
		t.Fatalf("released = %v, bump = %v, want a minor bump from the widget feat commit", released, rel.version.Bump)
	}

	// every commit is its own entry, parsed from its own subject, never
	// the pull request's title.
	widget, ok := findEntry(rel.entries, "add the widget")
	if !ok || widget.PullRequestNumber != 10 || widget.Type != "feat" {
		t.Fatalf("entries = %+v, want the widget commit attributed to pull request #10", rel.entries)
	}
	fix, ok := findEntry(rel.entries, "correct a bug")
	if !ok || fix.PullRequestNumber != 10 || fix.Type != "fix" {
		t.Fatalf("entries = %+v, want the bugfix commit attributed to pull request #10", rel.entries)
	}
	if e, ok := findEntry(rel.entries, "add the gadget (#11)"); !ok || e.PullRequestNumber != 0 {
		t.Fatalf("entries = %+v, want the gadget commit unattributed, it introduced no merge", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "direct hotfix"); !ok {
		t.Fatalf("entries = %+v, want the direct hotfix commit", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "ignored: all mode never uses a pull request title"); ok {
		t.Fatalf("entries = %+v, want the pull request title never used", rel.entries)
	}

	if f.calls[10] != 1 {
		t.Fatalf("PullRequest(#10) called %d times, want exactly once for the two commits it introduced", f.calls[10])
	}
}

func TestComputeReleaseInNestedMode(t *testing.T) {
	dir := newModeRepo(t)
	comp := resolvedComponent(config.ChangelogSourceNested)
	f := newFake()
	f.Responses = map[int]forgetest.Response{
		// a hidden, non-bumping type on the merge itself: the bump must
		// still come from the feat sub commit.
		10: {PullRequest: forge.PullRequest{Title: "chore: merge widget work", URL: "https://example.invalid/pull/10"}},
	}

	rel, released, err := computeRelease(t.Context(), dir, config.Default(), comp, f, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease() error = %v", err)
	}
	if !released || rel.version.Bump != version.BumpMinor {
		t.Fatalf("released = %v, bump = %v, want a minor bump from the nested feat sub commit", released, rel.version.Bump)
	}

	merge, ok := findEntry(rel.entries, "merge widget work")
	if !ok || merge.Type != "chore" {
		t.Fatalf("entries = %+v, want the merge entry with its own chore type", rel.entries)
	}
	if len(merge.SubEntries) != 2 {
		t.Fatalf("SubEntries = %+v, want the two commits the merge introduced", merge.SubEntries)
	}
	if merge.SubEntries[0].Description != "add the widget" || merge.SubEntries[0].Type != "feat" {
		t.Fatalf("SubEntries[0] = %+v, want the feat commit, oldest first", merge.SubEntries[0])
	}
	if merge.SubEntries[1].Description != "correct a bug" || merge.SubEntries[1].Type != "fix" {
		t.Fatalf("SubEntries[1] = %+v, want the fix commit", merge.SubEntries[1])
	}

	if e, ok := findEntry(rel.entries, "add the gadget (#11)"); !ok || e.PullRequestNumber != 0 {
		t.Fatalf("entries = %+v, want the gadget commit as a plain direct entry", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "direct hotfix"); !ok {
		t.Fatalf("entries = %+v, want the direct hotfix commit", rel.entries)
	}

	// the hidden chore type must not swallow the rendered changelog: the
	// merge's line still renders, as the heading of its visible sub
	// commits, under the section one of them belongs to.
	rendered := changelog.Sections(rel.entries, rel.options)
	if !strings.Contains(rendered, "merge widget work") {
		t.Fatalf("Sections() = %q, want the merge entry rendered despite its hidden own type", rendered)
	}
	if !strings.Contains(rendered, "  - **widget:** add the widget") || !strings.Contains(rendered, "  - **widget:** correct a bug") {
		t.Fatalf("Sections() = %q, want both sub entries indented under the merge", rendered)
	}
}

func TestComputeReleaseInAllModeAppliesTheOverrideBlockToTheWholeGroup(t *testing.T) {
	dir := newModeRepo(t)
	comp := resolvedComponent(config.ChangelogSourceAll)
	f := newFake()
	f.Responses = map[int]forgetest.Response{
		10: {PullRequest: forge.PullRequest{
			Body: "```release-me\nfeat: the override replaces both commits pull request #10 introduced\n```",
			URL:  "https://example.invalid/pull/10",
		}},
	}

	rel, released, err := computeRelease(t.Context(), dir, config.Default(), comp, f, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease() error = %v", err)
	}
	if !released {
		t.Fatal("released = false, want true")
	}

	if _, ok := findEntry(rel.entries, "the override replaces both commits pull request #10 introduced"); !ok {
		t.Fatalf("entries = %+v, want the override entry", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "add the widget"); ok {
		t.Fatalf("entries = %+v, want the widget commit replaced by the override", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "correct a bug"); ok {
		t.Fatalf("entries = %+v, want the bugfix commit replaced by the override too", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "direct hotfix"); !ok {
		t.Fatalf("entries = %+v, want the unrelated direct commit untouched", rel.entries)
	}
}

func TestComputeReleaseInSquashModeAppliesTheOverrideBlock(t *testing.T) {
	dir := newModeRepo(t)
	comp := resolvedComponent(config.ChangelogSourceSquash)
	f := newFake()
	f.Responses = map[int]forgetest.Response{
		11: {PullRequest: forge.PullRequest{
			Body: "```release-me\nfix: the override replaces the gadget commit\n```",
			URL:  "https://example.invalid/pull/11",
		}},
	}

	rel, released, err := computeRelease(t.Context(), dir, config.Default(), comp, f, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease() error = %v", err)
	}
	if !released {
		t.Fatal("released = false, want true")
	}

	if _, ok := findEntry(rel.entries, "the override replaces the gadget commit"); !ok {
		t.Fatalf("entries = %+v, want the override entry", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "add the gadget"); ok {
		t.Fatalf("entries = %+v, want the gadget commit's own title replaced by the override", rel.entries)
	}
}

func TestComputeReleaseInNestedModeAppliesTheOverrideBlockToTheMerge(t *testing.T) {
	dir := newModeRepo(t)
	comp := resolvedComponent(config.ChangelogSourceNested)
	f := newFake()
	f.Responses = map[int]forgetest.Response{
		10: {PullRequest: forge.PullRequest{
			Body: "```release-me\nfeat: the override replaces the merge and its sub commits\n```",
			URL:  "https://example.invalid/pull/10",
		}},
	}

	rel, released, err := computeRelease(t.Context(), dir, config.Default(), comp, f, discardLogger())
	if err != nil {
		t.Fatalf("computeRelease() error = %v", err)
	}
	if !released {
		t.Fatal("released = false, want true")
	}

	overridden, ok := findEntry(rel.entries, "the override replaces the merge and its sub commits")
	if !ok || overridden.PullRequestNumber != 10 || len(overridden.SubEntries) != 0 {
		t.Fatalf("entries = %+v, want the override entry with no sub entries of its own", rel.entries)
	}
	if _, ok := findEntry(rel.entries, "add the widget"); ok {
		t.Fatalf("entries = %+v, want the merge's own sub commits replaced, not kept alongside the override", rel.entries)
	}
}
