// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"xaltorka/models"
)

var (
	validRules    = map[string]bool{"public": true, "authenticated": true, "whitelist": true}
	validTLSModes = map[string]bool{"external": true, "acme": true, "selfsigned": true}
	validStores   = map[string]bool{"sqlite": true, "memory": true, "file": true}
)

// Validate enforces the constraints described in BLUEPRINT.md §7. It fails fast
// with a clear message identifying the offending field.
func Validate(b *Bundle) error {
	c := &b.Config

	if c.Server.Listen == "" {
		return fmt.Errorf("config: server.listen is required")
	}
	if !validTLSModes[c.TLS.Mode] {
		return fmt.Errorf("config: tls.mode %q invalid (external|acme|selfsigned)", c.TLS.Mode)
	}
	if c.TLS.Mode == "acme" && c.TLS.ACME.PDNSAPIURL == "" {
		return fmt.Errorf("config: tls.mode=acme requires tls.acme.pdns_api_url")
	}
	if !validStores[c.Session.Store] {
		return fmt.Errorf("config: session.store %q invalid (sqlite|memory)", c.Session.Store)
	}

	cidrs := append(append([]string{}, c.Admin.IPWhitelist...), c.Server.TrustedProxies...)
	for _, cidr := range cidrs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("config: invalid CIDR %q: %w", cidr, err)
		}
	}

	providerIDs := map[string]bool{}
	enabled := 0
	for i, p := range c.Providers {
		if p.ID == "" {
			return fmt.Errorf("config: providers[%d].id is required", i)
		}
		if providerIDs[p.ID] {
			return fmt.Errorf("config: duplicate provider id %q", p.ID)
		}
		providerIDs[p.ID] = true
		if p.Type != "oidc" && p.Type != "local" {
			return fmt.Errorf("config: providers[%d].type %q invalid (oidc|local)", i, p.Type)
		}
		if p.Enabled {
			enabled++
			if p.Type == "oidc" {
				if strings.TrimSpace(p.Issuer) == "" {
					return fmt.Errorf("config: providers[%d] (%s): enabled oidc provider requires issuer", i, p.ID)
				}
				if strings.TrimSpace(p.ClientID) == "" {
					return fmt.Errorf("config: providers[%d] (%s): enabled oidc provider requires client_id", i, p.ID)
				}
			}
		}
	}
	if c.AuthMode && enabled == 0 {
		return fmt.Errorf("config: auth_mode=true requires at least one enabled provider")
	}

	// Unified id namespace: config backends + services backends + links. Users
	// reference any of these ids in their authorization list.
	allowedIDs := map[string]bool{}
	checkBackend := func(scope string, i int, be models.Backend) error {
		if be.ID == "" {
			return fmt.Errorf("%s[%d].id is required", scope, i)
		}
		if allowedIDs[be.ID] {
			return fmt.Errorf("duplicate service/backend id %q", be.ID)
		}
		allowedIDs[be.ID] = true
		if be.Host == "" {
			return fmt.Errorf("%s[%d].host is required", scope, i)
		}
		for j, r := range be.Routes {
			if !validRules[r.Rule] {
				return fmt.Errorf("%s[%s].routes[%d].rule %q invalid (public|authenticated|whitelist)", scope, be.ID, j, r.Rule)
			}
			if err := validateUpstreamPort(r.Upstream); err != nil {
				return fmt.Errorf("%s[%s].routes[%d].upstream: %w", scope, be.ID, j, err)
			}
		}
		for j, cidr := range be.IPAllow {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("%s[%s].ip_allow[%d] %q: invalid CIDR: %w", scope, be.ID, j, cidr, err)
			}
		}
		return nil
	}
	for i, be := range c.Backends {
		if err := checkBackend("config.backends", i, be); err != nil {
			return err
		}
	}
	for i, be := range b.Services.Backends {
		if err := checkBackend("services.backends", i, be); err != nil {
			return err
		}
	}
	for i, l := range b.Services.Links {
		if l.ID == "" {
			return fmt.Errorf("services.links[%d].id is required", i)
		}
		if allowedIDs[l.ID] {
			return fmt.Errorf("duplicate service/backend id %q", l.ID)
		}
		allowedIDs[l.ID] = true
		if l.URL == "" {
			return fmt.Errorf("services.links[%s].url is required", l.ID)
		}
	}

	for i, cidr := range b.Services.AdminIPWhitelist {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("services.admin_ip_whitelist[%d] %q: invalid CIDR: %w", i, cidr, err)
		}
	}

	for i, m := range b.Services.Monitors {
		if m.ID == "" {
			return fmt.Errorf("services.monitors[%d].id is required", i)
		}
		if m.URL == "" {
			return fmt.Errorf("services.monitors[%s].url is required", m.ID)
		}
	}

	for i, u := range b.Users.Users {
		if u.Email == "" {
			return fmt.Errorf("users[%d].email is required", i)
		}
		if !providerIDs[u.Provider] {
			return fmt.Errorf("users[%s].provider %q not declared in config.providers", u.Email, u.Provider)
		}
		if u.Provider == "local" && u.PasswordHash == "" {
			return fmt.Errorf("users[%s]: local provider requires password_hash", u.Email)
		}
		for _, bid := range u.Backends {
			if !allowedIDs[bid] {
				return fmt.Errorf("users[%s].backends references unknown service %q", u.Email, bid)
			}
		}
	}
	return nil
}

// validateUpstreamPort checks the port of an upstream URL is in 1..65535.
// Accepts forms like "http://host:port[/path]" or "host:port".
func validateUpstreamPort(upstream string) error {
	s := upstream
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return fmt.Errorf("invalid upstream %q (want host:port)", upstream)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port in upstream %q", upstream)
	}
	return nil
}
