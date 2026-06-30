// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xaltorka/models"
	"xaltorka/providers"
)

func TestRandB64(t *testing.T) {
	a, b := randB64(), randB64()
	if a == "" || b == "" {
		t.Fatal("randB64 ha prodotto stringa vuota")
	}
	if a == b {
		t.Error("randB64 dovrebbe produrre valori diversi")
	}
	if _, err := base64.RawURLEncoding.DecodeString(a); err != nil {
		t.Errorf("randB64 non è base64url valido: %v", err)
	}
}

func TestCookieSecure(t *testing.T) {
	https := &Server{Cfg: &models.Config{Server: models.ServerCfg{ExternalURL: "https://gate.x"}}}
	if !https.cookieSecure() {
		t.Error("external_url https → cookieSecure deve essere true")
	}
	httpOnly := &Server{Cfg: &models.Config{Server: models.ServerCfg{ExternalURL: "http://localhost"}}}
	if httpOnly.cookieSecure() {
		t.Error("external_url http → cookieSecure deve essere false")
	}
}

func TestOIDCStateRoundTrip(t *testing.T) {
	s := &Server{}
	st := oidcState{State: "st-1", Nonce: "no-1", Next: "/listing", Provider: "google"}
	raw, _ := json.Marshal(st)

	r := httptest.NewRequest(http.MethodGet, "/auth/google/callback", nil)
	r.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: base64.RawURLEncoding.EncodeToString(raw)})

	got, ok := s.readOIDCState(r)
	if !ok {
		t.Fatal("readOIDCState: atteso ok=true")
	}
	if got != st {
		t.Errorf("readOIDCState = %+v, want %+v", got, st)
	}

	// clearOIDCState deve emettere un cookie scaduto (MaxAge<0).
	w := httptest.NewRecorder()
	s.clearOIDCState(w)
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == oidcStateCookie {
			found = true
			if c.MaxAge >= 0 {
				t.Errorf("clearOIDCState MaxAge = %d, deve essere <0", c.MaxAge)
			}
		}
	}
	if !found {
		t.Error("clearOIDCState non ha emesso il cookie di stato")
	}
}

func TestReadOIDCStateInvalid(t *testing.T) {
	s := &Server{}
	cases := map[string]string{
		"no cookie":   "",
		"non-base64":  "!!!not base64!!!",
		"non-json":    base64.RawURLEncoding.EncodeToString([]byte("not json")),
		"campi vuoti": base64.RawURLEncoding.EncodeToString([]byte(`{"s":"","n":""}`)),
	}
	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/auth/x/callback", nil)
			if val != "" {
				r.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: val})
			}
			if _, ok := s.readOIDCState(r); ok {
				t.Error("readOIDCState doveva fallire (ok=false)")
			}
		})
	}
}

func TestOIDCButtons(t *testing.T) {
	s := &Server{
		Cfg: &models.Config{Providers: []models.ProviderCfg{
			{ID: "local", Type: "local", Enabled: true},
			{ID: "google", Type: "oidc", Name: "Google", Enabled: true},
			{ID: "microsoft", Type: "oidc", Name: "Microsoft", Enabled: true},
		}},
		OIDC: map[string]*providers.OIDC{
			// Solo google è "abilitato" lato registry (microsoft assente → niente bottone).
			"google": providers.NewOIDC("google", "Google", "https://accounts.google.com", "cid", "sec", "https://gate/cb", nil),
		},
	}
	btns := s.oidcButtons()
	if len(btns) != 1 {
		t.Fatalf("oidcButtons = %d, want 1 (solo i provider nel registry)", len(btns))
	}
	if btns[0].ID != "google" || btns[0].Name != "Google" {
		t.Errorf("button = %+v, want {google Google}", btns[0])
	}
}
