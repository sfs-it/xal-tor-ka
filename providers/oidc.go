// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package providers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDC is an OpenID Connect provider: Google, Microsoft (Azure AD / Entra ID) or
// any spec-compliant IdP that exposes a discovery document — Keycloak, Authentik,
// Auth0, Okta, GitLab, … See AUTH-PROVIDERS.md for activation.
//
// Discovery is lazy: the issuer is contacted on first use (AuthURL/Exchange) and
// cached, so a temporarily unreachable IdP does not block server startup. The
// service stays fail-closed: any error in the flow yields a denied login, never a
// pass.
type OIDC struct {
	id          string
	name        string
	issuer      string
	clientID    string
	clientSec   string
	redirectURL string
	scopes      []string

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
	ready    bool
}

// NewOIDC builds an OIDC provider. No network call happens here: discovery runs
// lazily on the first AuthURL/Exchange. Empty name falls back to id; empty scopes
// default to openid+email+profile.
func NewOIDC(id, name, issuer, clientID, clientSecret, redirectURL string, scopes []string) *OIDC {
	if name == "" {
		name = id
	}
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}
	return &OIDC{
		id: id, name: name, issuer: issuer,
		clientID: clientID, clientSec: clientSecret,
		redirectURL: redirectURL, scopes: scopes,
	}
}

// ID implements Provider.
func (o *OIDC) ID() string { return o.id }

// Name is the human label shown on the login button.
func (o *OIDC) Name() string { return o.name }

// Type implements Provider.
func (o *OIDC) Type() string { return "oidc" }

// ensure performs the one-time OIDC discovery against the issuer (well-known/
// openid-configuration) and builds the oauth2 config + id_token verifier.
func (o *OIDC) ensure(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ready {
		return nil
	}
	prov, err := oidc.NewProvider(ctx, o.issuer)
	if err != nil {
		return fmt.Errorf("oidc discovery %s: %w", o.id, err)
	}
	o.oauth = &oauth2.Config{
		ClientID:     o.clientID,
		ClientSecret: o.clientSec,
		Endpoint:     prov.Endpoint(),
		RedirectURL:  o.redirectURL,
		Scopes:       o.scopes,
	}
	o.verifier = prov.Verifier(&oidc.Config{ClientID: o.clientID})
	o.ready = true
	return nil
}

// AuthURL returns the provider authorization URL for the given anti-CSRF state
// and replay-protection nonce. Triggers lazy discovery.
func (o *OIDC) AuthURL(ctx context.Context, state, nonce string) (string, error) {
	if err := o.ensure(ctx); err != nil {
		return "", err
	}
	return o.oauth.AuthCodeURL(state, oidc.Nonce(nonce)), nil
}

// Exchange swaps the authorization code for tokens, verifies the ID token
// signature against the IdP JWKS and checks the nonce, then returns the verified
// identity. Fail-closed: any error denies the login.
func (o *OIDC) Exchange(ctx context.Context, code, nonce string) (Identity, error) {
	if err := o.ensure(ctx); err != nil {
		return Identity{}, err
	}
	tok, err := o.oauth.Exchange(ctx, code)
	if err != nil {
		return Identity{}, fmt.Errorf("oidc token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return Identity{}, errors.New("oidc: no id_token in token response")
	}
	idt, err := o.verifier.Verify(ctx, rawID)
	if err != nil {
		return Identity{}, fmt.Errorf("oidc verify id_token: %w", err)
	}
	if idt.Nonce != nonce {
		return Identity{}, errors.New("oidc: nonce mismatch")
	}
	var claims struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idt.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("oidc claims: %w", err)
	}
	email := claims.Email
	if email == "" {
		email = claims.PreferredUsername // Microsoft often puts the email here
	}
	if email == "" {
		return Identity{}, errors.New("oidc: id_token has no email/preferred_username claim")
	}
	return Identity{Email: email, Provider: o.id}, nil
}
