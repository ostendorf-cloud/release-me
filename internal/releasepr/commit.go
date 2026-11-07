// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package releasepr

import (
	"errors"
	"regexp"
)

// signoffPattern matches a "Name <email>" identity, the shape git's own
// Signed-off-by trailer carries.
var signoffPattern = regexp.MustCompile(`^[^<>\s][^<>\r\n]* <[^<>\s]+>$`)

// CheckSignoff refuses a signoff identity that is not "Name <email>" on one
// line.
func CheckSignoff(identity string) error {
	if !signoffPattern.MatchString(identity) {
		return errors.New(`is not "Name <email>" on one line`)
	}
	return nil
}

// CommitMessage returns the release commit's message: the release pull
// request title as its subject, followed by a "Signed-off-by: <signoff>"
// trailer when signoff is not empty. The trailer is part of the message
// itself, so it reads the same however the commit is created, by the git
// CLI or through the forge's API.
func CommitMessage(title, signoff string) string {
	if signoff == "" {
		return title
	}
	return title + "\n\nSigned-off-by: " + signoff
}
