// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package migrate_test

import (
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/migrate"
)

func TestFromReleasePleaseKeepsReleasePleasesCommitAndIssueLinks(t *testing.T) {
	t.Parallel()
	got, err := migrate.FromReleasePlease([]byte(`{"packages": {".": {"release-type": "go"}}}`), nil, files(nil))
	if err != nil {
		t.Fatalf("FromReleasePlease() error = %v", err)
	}
	style := load(t, got.YAML).ChangelogStyle
	if !style.CommitLinks || !style.IssueReferences {
		t.Fatalf("ChangelogStyle = %+v, want commit links and issue references on, "+
			"the shape release-please's default changelog has:\n%s", style, got.YAML)
	}
}
