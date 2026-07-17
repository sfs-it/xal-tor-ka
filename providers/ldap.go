// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package providers

import (
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// LDAP authenticates users by binding to an LDAP / Active Directory server with
// the credentials supplied at login. A successful bind means the credentials are
// valid. It implements Provider (credential-based, like Local). See
// docs/next-gen-auth-sources.md.
type LDAP struct {
	id             string
	url            string // ldaps://host:636 (recommended) or ldap://host:389 (+ StartTLS)
	bindDNTemplate string // %s is replaced by the (DN-escaped) username
	startTLS       bool
	insecureSkip   bool // skip TLS cert verification — labs only
}

// NewLDAP builds an LDAP provider. bindDNTemplate must contain a single %s where
// the username goes, e.g. "%s@corp.example.com" (AD UPN) or
// "uid=%s,ou=people,dc=example,dc=com" (classic LDAP DN).
func NewLDAP(id, url, bindDNTemplate string, startTLS, insecureSkip bool) *LDAP {
	return &LDAP{id: id, url: url, bindDNTemplate: bindDNTemplate, startTLS: startTLS, insecureSkip: insecureSkip}
}

// ID implements Provider.
func (l *LDAP) ID() string { return l.id }

// Type implements Provider.
func (l *LDAP) Type() string { return "ldap" }

// Authenticate binds to the directory as the user. Returns ErrInvalidCredentials
// on a rejected bind (so the caller can't distinguish "no such user" from "wrong
// password"), and a wrapped connection error if the server is unreachable /
// misconfigured (so the caller can fail-closed without leaking to the user).
func (l *LDAP) Authenticate(username, password string) (Identity, error) {
	// An empty password would be an unauthenticated ("anonymous") bind on many
	// servers — always treat it as a failure.
	if strings.TrimSpace(username) == "" || password == "" {
		return Identity{}, ErrInvalidCredentials
	}
	tlsCfg := &tls.Config{InsecureSkipVerify: l.insecureSkip} //nolint:gosec // opt-in, labs only
	conn, err := ldap.DialURL(l.url, ldap.DialWithTLSConfig(tlsCfg))
	if err != nil {
		return Identity{}, fmt.Errorf("ldap %q: dial: %w", l.id, err)
	}
	defer conn.Close()
	if l.startTLS {
		if err := conn.StartTLS(tlsCfg); err != nil {
			return Identity{}, fmt.Errorf("ldap %q: starttls: %w", l.id, err)
		}
	}
	dn := fmt.Sprintf(l.bindDNTemplate, escapeDNValue(username))
	if err := conn.Bind(dn, password); err != nil {
		// Rejected bind (bad credentials) or user not found: generic error.
		return Identity{}, ErrInvalidCredentials
	}
	return Identity{Email: username, Provider: l.id}, nil
}

// escapeDNValue escapes the RFC 4514 special characters in a value interpolated
// into a bind DN template, so a crafted username can't inject extra DN
// components. Harmless for UPN-style templates (normal usernames are unchanged).
func escapeDNValue(s string) string {
	return strings.NewReplacer(
		`\`, `\\`, `,`, `\,`, `+`, `\+`, `"`, `\"`,
		`<`, `\<`, `>`, `\>`, `;`, `\;`, `=`, `\=`,
	).Replace(s)
}
