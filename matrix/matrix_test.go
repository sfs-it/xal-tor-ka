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
			{Path: "/admin", Rule: "whitelist"},
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
		{"/apixyz", "public"}, // NON deve cadere su /api
		{"/admin", "whitelist"},
		{"/admin/", "whitelist"},
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
		t.Error("host sconosciuto deve dare ok=false")
	}
}

func TestAuthorized(t *testing.T) {
	r := newR()
	u := models.User{Email: "a@b", Backends: []string{"site", "other"}}
	if !r.Authorized(u, "site") {
		t.Error("dovrebbe essere autorizzato a site")
	}
	if r.Authorized(u, "missing") {
		t.Error("non dovrebbe essere autorizzato a missing")
	}
}
