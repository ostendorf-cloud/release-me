// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"slices"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
)

// changelogOptions translates comp's changelog settings into the options
// its entries render with: the changelog style keys as they are, and,
// without "changelog-sections", hidden-types over the default sections.
// With it, the listed sections replace the default ones, in their order,
// types sharing a section heading rendered together where that heading
// first appears, and every type the list names decides its own
// visibility: one listed with a section renders even when hidden-types
// names it, one listed hidden never does.
func changelogOptions(comp config.ResolvedComponent) changelog.Options {
	opts := changelog.Options{
		HiddenTypes:  comp.HiddenTypes,
		SentenceCase: comp.ChangelogStyle.SentenceCase,
	}
	if comp.ChangelogSections == nil {
		return opts
	}

	hidden := slices.Clone(comp.HiddenTypes)
	sections := []changelog.Section{}
	for _, s := range comp.ChangelogSections {
		if s.Hidden {
			if !slices.Contains(hidden, s.Type) {
				hidden = append(hidden, s.Type)
			}
			continue
		}
		hidden = slices.DeleteFunc(hidden, func(t string) bool { return t == s.Type })
		i := slices.IndexFunc(sections, func(c changelog.Section) bool { return c.Heading == s.Section })
		if i < 0 {
			sections = append(sections, changelog.Section{Heading: s.Section})
			i = len(sections) - 1
		}
		sections[i].Types = append(sections[i].Types, s.Type)
	}
	opts.HiddenTypes = hidden
	opts.Sections = sections
	return opts
}
