// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
)

// changelogOptions translates comp's changelog settings into the options
// its entries render with: hidden-types, and the changelog style keys as
// they are: sentence case and crediting each entry's author.
func changelogOptions(comp config.ResolvedComponent) changelog.Options {
	return changelog.Options{
		HiddenTypes:  comp.HiddenTypes,
		SentenceCase: comp.ChangelogStyle.SentenceCase,
		Authors:      comp.ChangelogStyle.Authors,
	}
}
