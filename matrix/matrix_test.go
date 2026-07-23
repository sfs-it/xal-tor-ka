// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package matrix

import (
	"testing"

	"xaltorka/models"
)

func newR() *Resolver {
	r := &Resolver{}
	r.Set([]models.Backend{
		{ID: "site", Host: "site.example.com", Routes: []models.Route{
			{Path: "/", Rule: "public"},
			{Path: "/api", Rule: "authenticated"},
			{Path: "/admin", Rule: models.RuleAuthorized},
		}},
	})
	return r
}

func TestResolveLongestSegment(t *testing.T) {
	r := newR()
	cases := []struct {
		path, wantRule string
	}{
		{"/", "public"},
		{"/index.html", "public"},
		{"/api", "authenticated"},
		{"/api/v1/x", "authenticated"},
		{"/apixyz", "public"}, // must NOT fall back to /api
		{"/admin", models.RuleAuthorized},
		{"/admin/", models.RuleAuthorized},
	}
	for _, c := range cases {
		_, rt, ok := r.Resolve("site.example.com", c.path)
		if !ok || rt.Rule != c.wantRule {
			t.Errorf("Resolve(%q) = %q ok=%v, want %q", c.path, rt.Rule, ok, c.wantRule)
		}
	}
}

func TestResolveUnknownHost(t *testing.T) {
	if _, _, ok := newR().Resolve("nope.example.com", "/"); ok {
		t.Error("unknown host must give ok=false")
	}
}

func TestAuthorized(t *testing.T) {
	r := newR()
	u := models.User{Email: "a@b", Backends: []string{"site", "other"}}
	if !r.Authorized(u, "site") {
		t.Error("should be authorized for site")
	}
	if r.Authorized(u, "missing") {
		t.Error("should not be authorized for missing")
	}
}

// When several services share a hostname, Resolve must scan them ALL: stopping at
// the first backend for that host denied every sub-path service whose sibling owned
// "/" (the site answered, the sub-path got a default-deny 403).
func TestResolveAcrossBackendsSharingAHost(t *testing.T) {
	r := &Resolver{}
	r.Set([]models.Backend{
		{ID: "sfsit", Host: "sfs.it", Routes: []models.Route{{Path: "/", Rule: "public", Upstream: "http://site:8080"}}},
		{ID: "sfsit-bottiglia-tunnel", Host: "sfs.it", Routes: []models.Route{{Path: "/bottiglia2", Rule: models.RuleAuthorized, Upstream: "http://tunnel:8770"}}},
	})

	be, rt, ok := r.Resolve("sfs.it", "/bottiglia2/messaggi")
	if !ok {
		t.Fatal("sub-path service must resolve even though a sibling owns /")
	}
	if be.ID != "sfsit-bottiglia-tunnel" || rt.Rule != models.RuleAuthorized {
		t.Errorf("wrong match: backend=%q rule=%q", be.ID, rt.Rule)
	}

	be, rt, ok = r.Resolve("sfs.it", "/chi-siamo")
	if !ok || be.ID != "sfsit" || rt.Rule != "public" {
		t.Errorf("the root service must still win for other paths: backend=%q ok=%v", be.ID, ok)
	}

	if _, _, ok := r.Resolve("altro.example.com", "/"); ok {
		t.Error("an unknown host must not resolve")
	}
}
