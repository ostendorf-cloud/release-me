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
// workspace; a manifest that does not parse is an error naming it.
func readWorkspace(dir string, cfg config.Config, all []config.ResolvedComponent) ([]workspaceManifest, error) {
	var out []workspaceManifest
	for _, manager := range cfg.Workspaces {
		for _, comp := range all {
			path := stdpath.Join(comp.Path, workspace.ManifestFile(manager))
			content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("release-me: component %s: workspace: read %s: %w", comp.Name, path, err)
			}
			pkg, err := workspace.Parse(manager, content)
			if err != nil {
				return nil, fmt.Errorf("release-me: component %s: workspace: %s: %w", comp.Name, path, err)
			}
			out = append(out, workspaceManifest{
				member: workspace.Member{Component: comp.Name, Manager: manager, Package: pkg},
				path:   path,
			})
		}
	}
	return out, nil
}

// workspaceDependents returns every component that must release because
// one in released does, through the manifests' local dependencies, in
// release-me.yaml's order.
func workspaceDependents(manifests []workspaceManifest, released []string) []string {
	members := make([]workspace.Member, 0, len(manifests))
	for _, m := range manifests {
		members = append(members, m.member)
	}
	return workspace.Dependents(members, released)
}

// dependencyRelease is the release of a component that has nothing of its
// own to release but depends on one that has: a patch bump on its last
// tag, or its initial version without one, with no entries of its own;
// workspaceUpdates adds the dependency entries once every version is
// final.
func dependencyRelease(ctx context.Context, dir string, comp config.ResolvedComponent) (release, error) {
	tag, last, hasTag, err := history.LastTag(ctx, dir, comp.TagPrefix)
	if err != nil {
		return release{}, fmt.Errorf("release-me: component %s: last tag: %w", comp.Name, err)
	}
	since := ""
	if hasTag {
		since = tag
	}
	result, _, _ := version.Next(last, hasTag, []version.Change{{Type: "fix"}}, comp.InitialVersion, comp.BumpMinorPreMajor)
	result.Reason = "workspace dependency"
	return release{version: result, hiddenTypes: comp.HiddenTypes, fromTag: since}, nil
}

// workspaceUpdates returns the manifest updates and changelog entries that
// move every dependent releasing this run to the new version of each
// local dependency releasing with it. versions maps every component
// releasing this run to its final version. Each dependent gets one entry
// per dependency, "update <package> to <version>", typed fix and scoped
// deps, the patch a dependency bump releases it with.
func workspaceUpdates(manifests []workspaceManifest, versions map[string]string, logger *slog.Logger) ([]fileUpdate, map[string][]changelog.Entry) {
	members := make([]workspace.Member, 0, len(manifests))
	for _, m := range manifests {
		members = append(members, m.member)
	}
	var files []fileUpdate
	entries := map[string][]changelog.Entry{}
	var done []workspace.Link
	for _, link := range workspace.Links(members) {
		to, released := versions[link.Dependency]
		if _, releasing := versions[link.Dependent]; !released || !releasing {
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
			files = append(files, fileUpdate{
				file: config.File{Path: m.path},
				apply: func(content string) (string, error) {
					return workspace.SetDependency(manager, content, name, to)
				},
				what: "workspace dependency " + name,
			})
		}
		logger.Debug("workspace dependency", "component", link.Dependent, "dependency", link.Name, "version", to)
		entries[link.Dependent] = append(entries[link.Dependent], changelog.Entry{
			Type: "fix", Scope: "deps", Description: fmt.Sprintf("update %s to %s", link.Name, to),
		})
	}
	return files, entries
}
