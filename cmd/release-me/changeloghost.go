// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/url"
	"strings"

	"github.com/ostendorf-cloud/release-me/internal/config"
	"github.com/ostendorf-cloud/release-me/internal/forge"
)

// withChangelogHost returns f with every link a changelog renders moved to
// cfg's "changelog-host": the compare view and a pull request's. It
// returns f unchanged without the key. Everything else f does, the
// requests it makes included, stays on the host f talks to.
func withChangelogHost(cfg config.Config, f forge.Forge) forge.Forge {
	if cfg.ChangelogStyle.Host == "" {
		return f
	}
	host, err := url.Parse(cfg.ChangelogStyle.Host)
	if err != nil {
		// config.Load already refused a host that does not parse.
		return f
	}
	return hostedForge{Forge: f, host: host}
}

// hostedForge is a forge.Forge whose changelog links point at host.
type hostedForge struct {
	forge.Forge
	host *url.URL
}

// rehost returns link on h.host: its scheme and host replaced by the
// configured ones, its path kept and appended to the configured path. A
// link that is "" or does not parse comes back unchanged.
func (h hostedForge) rehost(link string) string {
	if link == "" {
		return ""
	}
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return link
	}
	u.Scheme, u.Host, u.User = h.host.Scheme, h.host.Host, nil
	u.Path = strings.TrimSuffix(h.host.Path, "/") + u.Path
	u.RawPath = ""
	return u.String()
}

// PullRequest implements forge.Forge: the pull request as f reads it, its
// URL on h.host. The changelog links an entry's pull request through it.
func (h hostedForge) PullRequest(ctx context.Context, owner, repo string, number int) (forge.PullRequest, error) {
	pr, err := h.Forge.PullRequest(ctx, owner, repo, number)
	pr.URL = h.rehost(pr.URL)
	return pr, err
}

// CompareURL implements forge.Forge on h.host.
func (h hostedForge) CompareURL(owner, repo, base, head string) string {
	return h.rehost(h.Forge.CompareURL(owner, repo, base, head))
}
