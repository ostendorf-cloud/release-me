// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package workspace_test

import (
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/workspace"
)

func TestLocalRangesOnlyLinksADependencyTheLocalVersionSatisfies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rng   string
		local string
		want  bool
	}{
		{rng: "^1.2.0", local: "1.4.1", want: true},
		{rng: "^1.2.0", local: "2.0.0", want: false},
		{rng: "^1.2.0", local: "1.1.9", want: false},
		{rng: "^0.2.3", local: "0.2.9", want: true},
		{rng: "^0.2.3", local: "0.3.0", want: false},
		{rng: "^0.0.3", local: "0.0.4", want: false},
		{rng: "~1.2.0", local: "1.2.7", want: true},
		{rng: "~1.2.0", local: "1.3.0", want: false},
		{rng: "~1", local: "1.9.0", want: true},
		{rng: "1.2.0", local: "1.2.0", want: true},
		{rng: "1.2.0", local: "1.2.1", want: false},
		{rng: "1.x", local: "1.7.0", want: true},
		{rng: "1.2", local: "1.3.0", want: false},
		{rng: "*", local: "3.1.0", want: true},
		{rng: "", local: "3.1.0", want: true},
		{rng: ">=1.0.0 <2", local: "1.9.9", want: true},
		{rng: ">=1.0.0 <2", local: "2.0.0", want: false},
		{rng: ">= 1.0.0", local: "1.0.0", want: true},
		{rng: ">1.2", local: "1.2.9", want: false},
		{rng: ">1.2", local: "1.3.0", want: true},
		{rng: "<=1.2", local: "1.2.9", want: true},
		{rng: "^1.0.0 || ^2.0.0", local: "2.3.0", want: true},
		{rng: "1.0.0 - 1.4.0", local: "1.4.0", want: true},
		{rng: "1.0.0 - 1.4.0", local: "1.4.1", want: false},
		{rng: "workspace:^9.0.0", local: "1.0.0", want: true},
		{rng: "workspace:*", local: "1.0.0", want: true},
		{rng: "file:../core", local: "1.0.0", want: true},
		{rng: "latest", local: "1.0.0", want: false},
		{rng: "github:acme/core", local: "1.0.0", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.rng+" at "+tc.local, func(t *testing.T) {
			t.Parallel()
			members := []workspace.Member{
				{Component: "core", Manager: workspace.Node, Package: workspace.Package{Name: "@acme/core", Version: tc.local}},
				{Component: "web", Manager: workspace.Node, Package: workspace.Package{
					Name: "@acme/web", Dependencies: []workspace.Dependency{{Name: "@acme/core", Range: tc.rng}},
				}},
			}
			got := len(workspace.Links(members, workspace.Options{LocalRangesOnly: true})) == 1
			if got != tc.want {
				t.Fatalf("Links() linked = %v, want %v", got, tc.want)
			}
			if len(workspace.Links(members, workspace.Options{})) != 1 {
				t.Fatal("Links() without LocalRangesOnly linked nothing, want every local name linked")
			}
		})
	}
}

func TestDependentsWithLocalRangesOnlyLeavesARegistryDependentAlone(t *testing.T) {
	t.Parallel()
	members := []workspace.Member{
		{Component: "core", Manager: workspace.Node, Package: workspace.Package{Name: "@acme/core", Version: "2.0.0"}},
		{Component: "legacy", Manager: workspace.Node, Package: workspace.Package{
			Name: "@acme/legacy", Dependencies: []workspace.Dependency{{Name: "@acme/core", Range: "^1.4.0"}},
		}},
		{Component: "web", Manager: workspace.Node, Package: workspace.Package{
			Name: "@acme/web", Dependencies: []workspace.Dependency{{Name: "@acme/core", Range: "workspace:^"}},
		}},
	}
	got := workspace.Dependents(members, []string{"core"}, workspace.Options{LocalRangesOnly: true})
	if want := []string{"web"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Dependents() = %q, want %q: legacy pins core 1.x from the registry", got, want)
	}
}
