// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package providers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// --- mock OpenID Provider ----------------------------------------------------

const testKID = "test-key-1"

// mockIdP stands up a minimal but real OIDC provider: discovery + JWKS + token
// endpoint that returns an id_token signed with an RSA key. It lets the tests
// exercise the full Exchange path (token swap + signature verification + nonce)
// that cannot be tried against a live provider without credentials.
type mockIdP struct {
	srv      *httptest.Server
	priv     *rsa.PrivateKey
	clientID string
	// claims baked into the next issued id_token
	nonce string
	email string
	extra map[string]any
}

func newMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}
	m := &mockIdP{priv: priv, clientID: "client-123", nonce: "good-nonce", email: "user@example.com"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                m.issuer(),
			"authorization_endpoint":                m.issuer() + "/authorize",
			"token_endpoint":                        m.issuer() + "/token",
			"jwks_uri":                              m.issuer() + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		jwk := jose.JSONWebKey{Key: priv.Public(), KeyID: testKID, Algorithm: "RS256", Use: "sig"}
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"access_token": "at",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     m.signIDToken(t),
		})
	})
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockIdP) issuer() string { return m.srv.URL }

func (m *mockIdP) signIDToken(t *testing.T) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss":   m.issuer(),
		"sub":   "subject-1",
		"aud":   m.clientID,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
		"nonce": m.nonce,
		"email": m.email,
	}
	for k, v := range m.extra {
		claims[k] = v
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: m.priv},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", testKID),
	)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	obj, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw, err := obj.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return raw
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- tests -------------------------------------------------------------------

func TestNewOIDCDefaults(t *testing.T) {
	// Empty name falls back to id; empty scopes default to openid+email+profile.
	p := NewOIDC("google", "", "https://issuer", "cid", "secret", "https://gate/cb", nil)
	if p.ID() != "google" {
		t.Errorf("ID = %q, want google", p.ID())
	}
	if p.Name() != "google" {
		t.Errorf("Name fallback = %q, want google", p.Name())
	}
	if p.Type() != "oidc" {
		t.Errorf("Type = %q, want oidc", p.Type())
	}
	p2 := NewOIDC("ms", "Microsoft", "https://issuer", "cid", "secret", "https://gate/cb", []string{"openid"})
	if p2.Name() != "Microsoft" {
		t.Errorf("Name = %q, want Microsoft", p2.Name())
	}
}

func TestOIDC_AuthURL(t *testing.T) {
	idp := newMockIdP(t)
	p := NewOIDC("mock", "Mock", idp.issuer(), idp.clientID, "secret", "https://gate/auth/mock/callback", nil)

	got, err := p.AuthURL(context.Background(), "the-state", "the-nonce")
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse authURL: %v", err)
	}
	if want := idp.issuer() + "/authorize"; !strings.HasPrefix(got, want) {
		t.Errorf("authURL %q does not start with %q", got, want)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"client_id":     idp.clientID,
		"state":         "the-state",
		"nonce":         "the-nonce",
		"response_type": "code",
		"redirect_uri":  "https://gate/auth/mock/callback",
	} {
		if q.Get(k) != want {
			t.Errorf("authURL %s = %q, want %q", k, q.Get(k), want)
		}
	}
	if !strings.Contains(q.Get("scope"), "openid") {
		t.Errorf("scope %q missing openid", q.Get("scope"))
	}
}

func TestOIDC_Exchange(t *testing.T) {
	idp := newMockIdP(t)
	p := NewOIDC("mock", "Mock", idp.issuer(), idp.clientID, "secret", "https://gate/cb", nil)

	id, err := p.Exchange(context.Background(), "any-code", "good-nonce")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if id.Email != "user@example.com" {
		t.Errorf("email = %q, want user@example.com", id.Email)
	}
	if id.Provider != "mock" {
		t.Errorf("provider = %q, want mock", id.Provider)
	}
}

func TestOIDC_Exchange_NonceMismatch(t *testing.T) {
	idp := newMockIdP(t)
	p := NewOIDC("mock", "Mock", idp.issuer(), idp.clientID, "secret", "https://gate/cb", nil)

	if _, err := p.Exchange(context.Background(), "any-code", "WRONG-nonce"); err == nil {
		t.Fatal("Exchange con nonce errato doveva fallire (replay protection)")
	}
}

func TestOIDC_Exchange_EmailFallbackPreferredUsername(t *testing.T) {
	idp := newMockIdP(t)
	idp.email = "" // niente claim email
	idp.extra = map[string]any{"preferred_username": "upn@example.com"}
	p := NewOIDC("mock", "Mock", idp.issuer(), idp.clientID, "secret", "https://gate/cb", nil)

	id, err := p.Exchange(context.Background(), "any-code", "good-nonce")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if id.Email != "upn@example.com" {
		t.Errorf("email fallback = %q, want upn@example.com", id.Email)
	}
}

func TestOIDC_Discovery_BadIssuer(t *testing.T) {
	// Issuer irraggiungibile → fail-closed (errore, non panico).
	p := NewOIDC("mock", "Mock", "http://127.0.0.1:1/nope", "cid", "secret", "https://gate/cb", nil)
	if _, err := p.AuthURL(context.Background(), "s", "n"); err == nil {
		t.Fatal("discovery su issuer irraggiungibile doveva fallire")
	}
}
