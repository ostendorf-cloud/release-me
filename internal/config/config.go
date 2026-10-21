// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package config loads release-me.yaml: initial-version,
// bump-minor-pre-major, hidden-types, fallback-type, token-env, forge,
// base-url, files, changelog and labels. major-tag stays unknown until
// milestone 5 reads it.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ostendorf-cloud/release-me/internal/conventional"
	"github.com/ostendorf-cloud/release-me/internal/updater"
	"gopkg.in/yaml.v3"
)

// Defaults applied for a key left out of release-me.yaml, or for a missing
// file entirely.
const (
	DefaultInitialVersion = "0.1.0"
	DefaultTokenEnv       = "GITHUB_TOKEN"
	// DefaultFallbackType is conventional.FallbackType, named here too so
	// callers of this package never need to import internal/conventional
	// just to read the default.
	DefaultFallbackType = conventional.FallbackType
	// DefaultChangelogPath is the changelog file path applied when the
	// "changelog" key is left out of release-me.yaml.
	DefaultChangelogPath = "CHANGELOG.md"
	// DefaultPendingLabel and DefaultTaggedLabel are the label names
	// applied when the "labels" block, or one of its keys, is left out.
	DefaultPendingLabel = "release: pending"
	DefaultTaggedLabel  = "release: tagged"
)

// DefaultHiddenTypes is the hidden-types list applied when the key is left
// out of release-me.yaml. DefaultFallbackType is hidden by default.
var DefaultHiddenTypes = []string{"chore", "docs", "ci", "style", "refactor", "test", "build", DefaultFallbackType}

// bumpingTypes are refused as fallback-type: text that does not parse must
// never move the version.
var bumpingTypes = map[string]bool{"feat": true, "fix": true, "perf": true, "revert": true}

// validForges are the values accepted for the "forge" key, besides the
// empty string, which leaves the forge to be guessed from the origin
// remote.
var validForges = map[string]bool{"github": true, "gitlab": true}

// File is one entry of the "files" key: a file release-me bumps the version
// in, through the named updater.
type File struct {
	Path    string
	Updater string
}

// Labels is the "labels" key: the two label names release-me expects on the
// repository.
type Labels struct {
	Pending string
	Tagged  string
}

// Config is the release-me.yaml configuration.
type Config struct {
	InitialVersion    string
	BumpMinorPreMajor bool
	HiddenTypes       []string
	FallbackType      string
	TokenEnv          string
	Forge             string
	BaseURL           string
	Files             []File
	ChangelogPath     string
	Labels            Labels
}

// rawFile mirrors File, the yaml shape of one "files" entry.
type rawFile struct {
	Path    string  `yaml:"path"`
	Updater *string `yaml:"updater"`
}

// rawLabels mirrors Labels with pointer fields, so Load can tell a key left
// out of the "labels" block apart from one set to its zero value.
type rawLabels struct {
	Pending *string `yaml:"pending"`
	Tagged  *string `yaml:"tagged"`
}

// raw mirrors Config with pointer fields, so Load can tell a key left out of
// the file apart from one set to its zero value.
type raw struct {
	InitialVersion    *string    `yaml:"initial-version"`
	BumpMinorPreMajor *bool      `yaml:"bump-minor-pre-major"`
	HiddenTypes       *[]string  `yaml:"hidden-types"`
	FallbackType      *string    `yaml:"fallback-type"`
	TokenEnv          *string    `yaml:"token-env"`
	Forge             *string    `yaml:"forge"`
	BaseURL           *string    `yaml:"base-url"`
	Files             *[]rawFile `yaml:"files"`
	Changelog         *string    `yaml:"changelog"`
	Labels            *rawLabels `yaml:"labels"`
}

// Default is the configuration applied when release-me.yaml is absent.
func Default() Config {
	return Config{
		InitialVersion: DefaultInitialVersion,
		HiddenTypes:    DefaultHiddenTypes,
		FallbackType:   DefaultFallbackType,
		TokenEnv:       DefaultTokenEnv,
		ChangelogPath:  DefaultChangelogPath,
		Labels:         Labels{Pending: DefaultPendingLabel, Tagged: DefaultTaggedLabel},
	}
}

// Load reads release-me.yaml at path. A missing file yields Default(). An
// unknown key is an error.
func Load(path string) (cfg Config, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("config: close %s: %w", path, cerr)
		}
	}()

	var r raw
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg = Default()
	if r.InitialVersion != nil {
		cfg.InitialVersion = *r.InitialVersion
	}
	if r.BumpMinorPreMajor != nil {
		cfg.BumpMinorPreMajor = *r.BumpMinorPreMajor
	}
	if r.HiddenTypes != nil {
		cfg.HiddenTypes = *r.HiddenTypes
	}
	if r.FallbackType != nil {
		cfg.FallbackType = *r.FallbackType
	}
	if bumpingTypes[cfg.FallbackType] {
		return Config{}, fmt.Errorf(
			"config: parse %s: fallback-type %q bumps the version, unparseable text must not",
			path, cfg.FallbackType,
		)
	}
	if r.TokenEnv != nil {
		cfg.TokenEnv = *r.TokenEnv
	}
	if r.Forge != nil {
		cfg.Forge = *r.Forge
	}
	if cfg.Forge != "" && !validForges[cfg.Forge] {
		return Config{}, fmt.Errorf("config: parse %s: forge %q is neither %q nor %q", path, cfg.Forge, "github", "gitlab")
	}
	if r.BaseURL != nil {
		cfg.BaseURL = *r.BaseURL
	}
	if r.Changelog != nil {
		cfg.ChangelogPath = *r.Changelog
	}
	if r.Labels != nil {
		if r.Labels.Pending != nil {
			cfg.Labels.Pending = *r.Labels.Pending
		}
		if r.Labels.Tagged != nil {
			cfg.Labels.Tagged = *r.Labels.Tagged
		}
	}
	if r.Files != nil {
		files := make([]File, 0, len(*r.Files))
		for _, rf := range *r.Files {
			name := updater.Marker
			if rf.Updater != nil {
				name = *rf.Updater
			}
			if _, ok := updater.Updaters[name]; !ok {
				return Config{}, fmt.Errorf("config: parse %s: file %s: unknown updater %q", path, rf.Path, name)
			}
			files = append(files, File{Path: rf.Path, Updater: name})
		}
		cfg.Files = files
	}
	return cfg, nil
}
