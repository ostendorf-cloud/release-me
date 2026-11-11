// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	stdpath "path"
	"path/filepath"
	"slices"

	"github.com/ostendorf-cloud/release-me/internal/changelog"
	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/history"
	"github.com/ostendorf-cloud/release-me/internal/version"
	"github.com/ostendorf-cloud/release-me/internal/workspace"
)

// workspaceManifest is one component's manifest of one of the package
// managers "workspaces" names: the member internal/workspace links, and
// the manifest's path relative to the repository root.
type workspaceManifest struct {
	member workspace.Member
	path   string
}

// readWorkspace reads the manifest of every component in all for every
// package manager cfg.Workspaces names, in release-me.yaml's order. A
// component without such a manifest is no member of that manager's
// workspace; a manifest that does not parse is an error naming it. Without
// a root component, the manifest at the repository root, the workspace
// root such as a virtual Cargo workspace or a Maven aggregator pom, is
// read too, as the member of no component: its dependencies on a released
// component move, it releases nothing.
func readWorkspace(dir string, cfg config.Config, all []config.ResolvedComponent) ([]workspaceManifest, error) {
	var out []workspaceManifest
	read := func(component, path, manager string) error {
		//nolint:gosec // path is a manifest path release-me.yaml's workspaces configure, joined under the resolved repository root
		content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		who := "workspace root"
		if component != "" {
			who = "component " + component
		}
		if err != nil {
			return fmt.Errorf("release-me: %s: workspace: read %s: %w", who, path, err)
		}
		pkg, err := workspace.Parse(manager, content)
		if err != nil {
			return fmt.Errorf("release-me: %s: workspace: %s: %w", who, path, err)
		}
		out = append(out, workspaceManifest{
			member: workspace.Member{Component: component, Manager: manager, Package: pkg},
			path:   path,
		})
		return nil
	}
	hasRoot := slices.ContainsFunc(all, func(c config.ResolvedComponent) bool { return c.Path == "." })
	for _, manager := range cfg.Workspaces {
		for _, comp := range all {
			if err := read(comp.Name, stdpath.Join(comp.Path, workspace.ManifestFile(manager)), manager); err != nil {
				return nil, err
			}
		}
		if !hasRoot {
			if err := read("", workspace.ManifestFile(manager), manager); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// workspaceDependents returns every component that must release because
// one in released does, through the manifests' local dependencies, in
// release-me.yaml's order.
func workspaceDependents(cfg config.Config, manifests []workspaceManifest, released []string) []string {
	members := make([]workspace.Member, 0, len(manifests))
	for _, m := range manifests {
		members = append(members, m.member)
	}
	return workspace.Dependents(members, released, workspaceOptions(cfg))
}

// workspaceOptions returns the link options cfg's "always-link-local"
// selects.
func workspaceOptions(cfg config.Config) workspace.Options {
	return workspace.Options{LocalRangesOnly: !cfg.AlwaysLinkLocal}
}

// dependencyRelease is the release of a component that has nothing of its
// own to release but depends on one that has: a patch bump on its last
// tag, or its initial version without one, with no entries of its own;
// workspaceUpdates adds the dependency entries once every version is
// final. The component's own "versioning-strategy" does not apply here: a
// dependent carried along by a workspace relationship has no changes of
// its own, so it takes a plain patch rather than the strategy's bump.
// Without this, a "service-pack" strategy would hand it back its own last
// version instead of moving it.
func dependencyRelease(ctx context.Context, dir string, comp config.ResolvedComponent) (release, error) {
	tag, last, hasTag, err := history.LastTag(ctx, dir, comp.TagPrefix)
	if err != nil {
		return release{}, fmt.Errorf("release-me: component %s: last tag: %w", comp.Name, err)
	}
	since := ""
	if hasTag {
		since = tag
	}
	rules := comp.Rules()
	rules.Strategy = version.StrategyDefault
	result, _, _ := version.Next(last, hasTag, []version.Change{{Type: "fix"}}, rules)
	result.Reason = "workspace dependency"
	return release{version: result, options: changelogOptions(comp), fromTag: since}, nil
}

// workspaceUpdates returns the manifest updates and changelog entries that
// move every dependent releasing this run to the new version of each
// local dependency releasing with it. versions maps every component
// releasing this run to its final version. Each dependent gets one entry
// per dependency, "update <package> to <version>", typed fix and scoped
// deps, the patch a dependency bump releases it with.
func workspaceUpdates(
	cfg config.Config, manifests []workspaceManifest, versions map[string]string, logger *slog.Logger,
) ([]fileUpdate, map[string][]changelog.Entry) {
	members := make([]workspace.Member, 0, len(manifests))
	for _, m := range manifests {
		members = append(members, m.member)
	}
	var files []fileUpdate
	entries := map[string][]changelog.Entry{}
	var done []workspace.Link
	for _, link := range workspace.Links(members, workspaceOptions(cfg)) {
		to, released := versions[link.Dependency]
		// The workspace root manifest belongs to no component: it never
		// releases, but its dependencies move.
		if _, releasing := versions[link.Dependent]; !released || !releasing && link.Dependent != "" {
			continue
		}
		key := workspace.Link{Dependent: link.Dependent, Manager: link.Manager, Name: link.Name}
		if slices.Contains(done, key) {
			continue
		}
		done = append(done, key)
		for _, m := range manifests {
			if m.member.Component != link.Dependent || m.member.Manager != link.Manager {
				continue
			}
			manager, name := link.Manager, link.Name
			localVersion := ""
			for _, target := range members {
				if target.Component == link.Dependency && target.Manager == manager {
					localVersion = target.Package.Version
					break
				}
			}
			files = append(files, fileUpdate{
				file: config.File{Path: m.path},
				apply: func(content string) (string, error) {
					return workspace.SetLocalDependency(manager, content, name, to, localVersion, workspaceOptions(cfg))
				},
				what: "workspace dependency " + name,
			})
		}
		logger.Debug("workspace dependency", "component", link.Dependent, "dependency", link.Name, "version", to)
		if link.Dependent == "" {
			continue
		}
		entries[link.Dependent] = append(entries[link.Dependent], changelog.Entry{
			Type: "fix", Scope: "deps", Description: fmt.Sprintf("update %s to %s", link.Name, to),
		})
	}
	return files, entries
}
