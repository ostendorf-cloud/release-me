// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
	tagrelease "github.com/ostendorf-cloud/release-me/internal/release"
	"github.com/ostendorf-cloud/release-me/internal/version"
)

// outputsEnv names the environment variable holding the path of a GitHub
// Actions output file, the one $GITHUB_OUTPUT names, that pr, release and
// run append their machine-readable outputs to. The composite action sets
// it; it is opt-in rather than $GITHUB_OUTPUT itself, so a plain workflow
// step or a test running under GitHub Actions never gets outputs it did
// not ask for, and a release-me binary older than this file simply
// ignores it instead of failing on an unknown flag.
const outputsEnv = "RELEASE_ME_GITHUB_OUTPUT"

// runOutputs collects what one pr, release or run invocation did for the
// action's outputs: every release pull request it opened, updated or found
// up to date, and every release it tagged. Nothing is recorded in dry-run.
type runOutputs struct {
	prs      []prOutput
	releases []tagrelease.Result
}

// prOutput is one release pull request in the action's "pr" and "prs"
// outputs and in a component's "pr" field: the components it releases
// are more than one for a locked family or a shared release pull request.
type prOutput struct {
	Number     int      `json:"number"`
	URL        string   `json:"url"`
	Branch     string   `json:"branch"`
	Components []string `json:"components"`
}

// componentOutput is one component's entry in the action's "components"
// output. Version, Tag, SHA and Body are empty and Major, Minor and Patch
// nil unless Released. PR is nil unless a release pull request for the
// component was opened, updated or found up to date this run. The keys
// after "tag" follow release-please-action's output names, so a migrated
// workflow reads the same name off a component.
type componentOutput struct {
	Released  bool      `json:"released"`
	Version   string    `json:"version"`
	Tag       string    `json:"tag"`
	Major     *int      `json:"major"`
	Minor     *int      `json:"minor"`
	Patch     *int      `json:"patch"`
	SHA       string    `json:"sha"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	UploadURL string    `json:"upload_url"`
	Draft     bool      `json:"draft"`
	PR        *prOutput `json:"pr"`
}

// runOutputsKey is the unexported context key a *runOutputs is stored
// under, so only withOutputs, recordPR and reportReleased reach it.
type runOutputsKey struct{}

// withOutputs returns ctx carrying out, filled by every recordPR and
// reportReleased call doPR and doRelease make. A run without one, such as
// the comment command's, records nothing.
func withOutputs(ctx context.Context, out *runOutputs) context.Context {
	return context.WithValue(ctx, runOutputsKey{}, out)
}

// recordPR records pr, opened or updated on branch for components, when
// ctx carries a *runOutputs and this is no dry run. A pull request already
// recorded under the same number is replaced.
func recordPR(ctx context.Context, dryRun bool, pr forge.PullRequest, branch string, components []string) {
	out, ok := ctx.Value(runOutputsKey{}).(*runOutputs)
	if !ok || dryRun {
		return
	}
	rec := prOutput{Number: pr.Number, URL: pr.URL, Branch: branch, Components: components}
	for i, existing := range out.prs {
		if existing.Number == pr.Number {
			out.prs[i] = rec
			return
		}
	}
	out.prs = append(out.prs, rec)
}

// reportReleased prints r's tag in the stable, unprefixed "released-tag:
// <tag>" shape run-release-me.sh greps for, to fill the composite action's
// "tags" output, and records r for the other outputs when ctx carries a
// *runOutputs: every release this run handled, including one already
// tagged or left a draft by an earlier, interrupted run. A later step can
// attach assets and publish a draft, or create the release when
// skip-github-release is set. With force-tag-creation false, the reported
// name has no git ref until the draft is published. Does nothing in
// dry-run: no tag or release was created.
func reportReleased(ctx context.Context, w io.Writer, dryRun bool, r tagrelease.Result) {
	if dryRun {
		return
	}
	fmt.Fprintf(w, "released-tag: %s\n", r.Tag)
	if out, ok := ctx.Value(runOutputsKey{}).(*runOutputs); ok {
		out.releases = append(out.releases, r)
	}
}

// render returns out as GitHub Actions output lines, one "name=value" per
// output, every value single-line JSON except released: "released", true
// when any release was tagged; "components", an object keyed by every
// name in components, the components this run handled; "pr", the one
// release pull request recorded, null when there were none or several;
// "prs", every one recorded, in the order they were.
//
// A pull request or a release recorded for a name outside components is
// skipped rather than added as a key components does not list; released,
// pr and prs still count it, the same as tags does. Neither
// can happen today: doPR and doRelease only record the components
// resolveComponents returns, a shared title naming an unconfigured
// component is refused before anything is tagged, and --component is
// rejected in shared and locked mode.
func (out *runOutputs) render(components []string) (string, error) {
	byName := make(map[string]componentOutput, len(components))
	for _, name := range components {
		byName[name] = componentOutput{}
	}
	for i := range out.prs {
		pr := &out.prs[i]
		for _, name := range pr.Components {
			if c, ok := byName[name]; ok {
				c.PR = pr
				byName[name] = c
			}
		}
	}
	for _, r := range out.releases {
		if c, ok := byName[r.Name]; ok {
			c.Released, c.Version, c.Tag = true, r.Version, r.Tag
			c.SHA, c.Body, c.HTMLURL, c.UploadURL, c.Draft = r.SHA, r.Body, r.HTMLURL, r.UploadURL, r.Draft
			v, valid := version.ParseTag("v" + r.Version)
			if !valid {
				return "", fmt.Errorf("component %s: release version %q is invalid", r.Name, r.Version)
			}
			c.Major, c.Minor, c.Patch = &v.Major, &v.Minor, &v.Patch
			byName[r.Name] = c
		}
	}

	componentsJSON, err := json.Marshal(byName)
	if err != nil {
		return "", fmt.Errorf("components: %w", err)
	}
	prs := out.prs
	if prs == nil {
		prs = []prOutput{}
	}
	prsJSON, err := json.Marshal(prs)
	if err != nil {
		return "", fmt.Errorf("prs: %w", err)
	}
	prJSON := []byte("null")
	if len(prs) == 1 {
		if prJSON, err = json.Marshal(prs[0]); err != nil {
			return "", fmt.Errorf("pr: %w", err)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "released=%t\n", len(out.releases) > 0)
	fmt.Fprintf(&b, "components=%s\n", componentsJSON)
	fmt.Fprintf(&b, "pr=%s\n", prJSON)
	fmt.Fprintf(&b, "prs=%s\n", prsJSON)
	return b.String(), nil
}

// finishWrite ends pr, release and run once their work stopped on err, nil
// for success: it prints err, then emits out for components, the ones
// the command resolved, whether or not err is set, since every pull
// request and release out records is one the forge already confirmed. A
// failed step's outputs still say how far it got. It returns the exit
// code, 1 when err is set or the outputs could not be written.
func finishWrite(stderr io.Writer, components []config.ResolvedComponent, out *runOutputs, err error) int {
	code := 0
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		code = 1
	}
	if err := emitOutputs(components, out); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		code = 1
	}
	return code
}

// emitOutputs appends out, for components, to the file outputsEnv names,
// and does nothing when it is unset.
func emitOutputs(components []config.ResolvedComponent, out *runOutputs) error {
	path := os.Getenv(outputsEnv)
	if path == "" {
		return nil
	}
	names := make([]string, 0, len(components))
	for _, c := range components {
		names = append(names, c.Name)
	}
	content, err := out.render(names)
	if err != nil {
		return fmt.Errorf("release-me: render outputs: %w", err)
	}
	//nolint:gosec // path is outputsEnv, the CI runner's own output file path, not externally supplied
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("release-me: open %s: %w", outputsEnv, err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("release-me: write %s: %w", outputsEnv, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("release-me: write %s: %w", outputsEnv, err)
	}
	return nil
}
