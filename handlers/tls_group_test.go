// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import "testing"

func TestRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"segnalapa.it":      "segnalapa.it",
		"app.segnalapa.it":  "segnalapa.it",
		"auth.sfs.it":       "sfs.it",
		"myrules.localhost": "myrules.localhost",
		"a.b.example.com":   "example.com",
	}
	for in, want := range cases {
		if got := registrableDomain(in); got != want {
			t.Errorf("registrableDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

// The parent domain comes first, its subdomains follow (sorted, marked Sub),
// and unrelated groups keep first-appearance order.
func TestGroupTLSRows(t *testing.T) {
	in := []tlsRow{
		{Host: "app.segnalapa.it"},
		{Host: "sfs.it"},
		{Host: "segnalapa.it"},
		{Host: "auth.sfs.it"},
		{Host: "api.segnalapa.it"},
		{Host: "pizzanostop.it"},
	}
	got := groupTLSRows(in)
	// segnalapa.it group first (first seen via app.segnalapa.it): head segnalapa.it, then api., app. (sorted)
	exp := []struct {
		host string
		sub  bool
	}{
		{"segnalapa.it", false},
		{"api.segnalapa.it", true},
		{"app.segnalapa.it", true},
		{"sfs.it", false},
		{"auth.sfs.it", true},
		{"pizzanostop.it", false},
	}
	if len(got) != len(exp) {
		t.Fatalf("len = %d, want %d (%+v)", len(got), len(exp), got)
	}
	for i, e := range exp {
		if got[i].Host != e.host || got[i].Sub != e.sub {
			t.Errorf("row %d = {%q sub=%v}, want {%q sub=%v}", i, got[i].Host, got[i].Sub, e.host, e.sub)
		}
	}
}
