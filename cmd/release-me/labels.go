// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
)

// missingLabels returns the configured pending, tagged and pre-release type
// labels that do not exist on the repository, the check ensureLabels applies
// before doCheck and doPR write anything. unauthorized is true when the
// forge rejected the credentials rather than failing for any other reason.
func missingLabels(ctx context.Context, f forge.Forge, owner, repoName string, cfg config.Config) (missing []string, unauthorized bool, err error) {
	repoLabels, err := f.RepositoryLabels(ctx, owner, repoName)
	if err != nil {
		if errors.Is(err, forge.ErrUnauthorized) {
			return nil, true, err
		}
		return nil, false, err
	}
	// Pending and tagged are always expected; an empty pre-release label
	// turns that type off, so there is no label to expect for it.
	for _, want := range append(append(slices.Clone(cfg.Labels.Pending), cfg.Labels.Tagged...), cfg.Labels.Extra...) {
		if !slices.Contains(repoLabels, want) {
			missing = append(missing, want)
		}
	}
	for _, want := range cfg.Labels.PreReleaseLabels() {
		if !slices.Contains(repoLabels, want) {
			missing = append(missing, want)
		}
	}
	return missing, false, nil
}

// ensureLabels is the check doCheck and doPR both apply before writing
// anything: it calls missingLabels, and, with "labels.create-missing" at
// its default true, creates every label missingLabels reports absent
// instead of leaving the caller to report and fail on it, so missing
// returns empty and err nil once every create succeeds. The first
// CreateLabel failure stops right there, wrapped with the label name, never
// silently skipped in favor of the next one. With "labels.create-missing:
// false", missingLabels' own result is returned unchanged, restoring
// release-me's report-and-fail behavior from before this key existed.
func ensureLabels(ctx context.Context, f forge.Forge, owner, repoName string, cfg config.Config) (missing []string, unauthorized bool, err error) {
	missing, unauthorized, err = missingLabels(ctx, f, owner, repoName, cfg)
	if err != nil || unauthorized || len(missing) == 0 || !cfg.Labels.CreateMissing {
		return missing, unauthorized, err
	}
	for _, name := range missing {
		color, description := labelDefaults(name, cfg)
		if err := f.CreateLabel(ctx, owner, repoName, name, color, description); err != nil {
			return nil, false, fmt.Errorf("release-me: create label %q: %w", name, err)
		}
	}
	return nil, false, nil
}

// neutralLabelColor and preReleaseLabelColor are the two colors
// labelDefaults assigns: a neutral gray for a label with no
// release-me-specific meaning beyond its role, and a light green for a
// pre-release type label, both matching this repository's own labels.
const (
	neutralLabelColor    = "ededed"
	preReleaseLabelColor = "c2e0c6"
)

// preReleaseDescriptions is the human-written description labelDefaults
// gives a built-in pre-release type's label, keyed by PreReleaseType.Type.
// A configured type outside this map, a repository's own addition such as
// "next", gets the generic description labelDefaults falls back to instead:
// this tool cannot know the wording a human would pick for a type it does
// not ship.
var preReleaseDescriptions = map[string]string{
	"alpha": "Next release as alpha pre-release.",
	"beta":  "Next release as beta pre-release.",
	"rc":    "Next release as release candidate.",
}

// labelDefaults classifies name against cfg.Labels' pending, tagged,
// pre-release and extra roles, and returns the color and description
// ensureLabels gives it on create. A label name is unique across roles in a
// configuration config.Load accepted, so exactly one case applies; any name
// outside every configured role, which cfg.Labels.Extra always is, falls
// back to the extra scheme: a neutral color, since nothing more specific is
// knowable about a label release-me does not assign any meaning beyond
// adding it to a release pull request.
func labelDefaults(name string, cfg config.Config) (color, description string) {
	if slices.Contains(cfg.Labels.Pending, name) {
		return neutralLabelColor, "Release pull request open."
	}
	if slices.Contains(cfg.Labels.Tagged, name) {
		return neutralLabelColor, "Release tagged."
	}
	for _, t := range cfg.Labels.PreRelease {
		if t.Label == name {
			if desc, ok := preReleaseDescriptions[t.Type]; ok {
				return preReleaseLabelColor, desc
			}
			return preReleaseLabelColor, fmt.Sprintf("Next release as %s pre-release.", t.Type)
		}
	}
	return neutralLabelColor, "Added to release-me's release pull requests."
}
