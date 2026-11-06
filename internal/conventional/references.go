// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package conventional

import (
	"regexp"
	"slices"
	"strconv"
)

// closingReference matches GitHub's closing keywords followed by a same
// repository issue reference, "closes #12", "Fixes: #7", "resolved #3",
// case-insensitive, the keyword and the reference as whole words. A
// reference into another repository, "owner/repo#12", carries no "#"
// directly after the keyword and so never matches.
var closingReference = regexp.MustCompile(
	`(?i)\b(?:close|closes|closed|fix|fixes|fixed|resolve|resolves|resolved):?\s+#(\d+)\b`,
)

// IssueReferences returns the issue numbers text closes with one of
// GitHub's closing keywords, "closes #12", in the order text first names
// them, each once, nil when it names none. text is a commit message or a
// pull request's title and body.
func IssueReferences(text string) []int {
	var numbers []int
	for _, m := range closingReference.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n == 0 || slices.Contains(numbers, n) {
			continue
		}
		numbers = append(numbers, n)
	}
	return numbers
}
