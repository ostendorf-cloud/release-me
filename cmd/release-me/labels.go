// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"slices"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
)

// missingLabels returns the configured pending, tagged and pre-release type
// labels that do not exist on the repository, the check doCheck and doPR
// both apply before writing anything. unauthorized is true when the forge
// rejected the credentials rather than failing for any other reason.
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
