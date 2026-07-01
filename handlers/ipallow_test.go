// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"xaltorka/matrix"
	"xaltorka/models"
)

func TestNormalizeCIDRs(t *testing.T) {
	got, err := normalizeCIDRs("203.0.113.0/24, 10.0.0.5  192.168.1.1/32\n2001:db8::1")
	if err != nil {
		t.Fatalf("normalizeCIDRs: %v", err)
	}
	want := []string{"203.0.113.0/24", "10.0.0.5/32", "192.168.1.1/32", "2001:db8::1/128"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if empty, err := normalizeCIDRs("   "); err != nil || len(empty) != 0 {
		t.Errorf("empty input → %v, %v; want [], nil", empty, err)
	}
	if _, err := normalizeCIDRs("not-an-ip"); err == nil {
		t.Error("invalid token should error")
	}
}

// TestValidateIPAllow checks the per-vhost IP allow-list is enforced in /validate
// before the rule (so even a public route is IP-restricted), fail-closed.
func TestValidateIPAllow(t *testing.T) {
	cfg := &models.Config{AuthMode: true}
	res := matrix.NewResolver(cfg)
	res.Set([]models.Backend{{
		ID: "b", Host: "app.local", IPAllow: []string{"10.0.0.0/24"},
		Routes: []models.Route{{Path: "/", Rule: "public", Upstream: "http://x:1"}},
	}})
	s := &Server{Cfg: cfg, Resolver: res}

	call := func(remote string) int {
		r := httptest.NewRequest(http.MethodGet, "/validate", nil)
		r.Header.Set("X-Original-Host", "app.local")
		r.Header.Set("X-Original-URI", "/")
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		s.handleValidate(w, r)
		return w.Code
	}

	if code := call("10.0.0.5:5000"); code != http.StatusOK {
		t.Errorf("allowed IP → %d, want 200", code)
	}
	if code := call("192.168.1.1:5000"); code != http.StatusForbidden {
		t.Errorf("disallowed IP → %d, want 403", code)
	}
}

func TestEffectiveAdminIPsFallback(t *testing.T) {
	s := &Server{Cfg: &models.Config{Admin: models.AdminCfg{IPWhitelist: []string{"127.0.0.1/32"}}}}
	// No override → falls back to config.
	if got := s.effectiveAdminIPs(); len(got) != 1 || got[0] != "127.0.0.1/32" {
		t.Errorf("fallback = %v, want [127.0.0.1/32]", got)
	}
	// Override set → used instead.
	s.adminIPs = []string{"10.0.0.0/8"}
	if got := s.effectiveAdminIPs(); len(got) != 1 || got[0] != "10.0.0.0/8" {
		t.Errorf("override = %v, want [10.0.0.0/8]", got)
	}
}
