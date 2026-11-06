// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package updater_test

import "strings"

// replaceLine returns content with every line equal to from replaced by to.
func replaceLine(content, from, to string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if line == from {
			lines[i] = to
		}
	}
	return strings.Join(lines, "\n")
}
