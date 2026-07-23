// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package models

import "testing"

// "whitelist" was renamed "authorized". Configuration written before the rename must
// keep working: the gate canonicalises it on load, so a service that used to admit
// only its enabled users must not silently widen to every authenticated account.
func TestCanonicalRuleAcceptsTheLegacyName(t *testing.T) {
	if got := CanonicalRule("whitelist"); got != RuleAuthorized {
		t.Errorf("legacy rule must map to %q, got %q", RuleAuthorized, got)
	}
	for _, r := range []string{RulePublic, RuleAuthenticated, RuleAuthorized} {
		if got := CanonicalRule(r); got != r {
			t.Errorf("current rule %q must be left alone, got %q", r, got)
		}
	}

	bs := []Backend{{ID: "x", Routes: []Route{
		{Path: "/", Rule: "whitelist"},
		{Path: "/pub", Rule: RulePublic},
	}}}
	CanonicalizeRules(bs)
	if bs[0].Routes[0].Rule != RuleAuthorized {
		t.Errorf("load-time canonicalisation missed the route, got %q", bs[0].Routes[0].Rule)
	}
	if bs[0].Routes[1].Rule != RulePublic {
		t.Error("canonicalisation must not touch the other rules")
	}
}

// Per-path grants: a path may carry its own list of users instead of the service's,
// and inheritance runs DOWNWARD — a path that inherits takes the list of the nearest
// ancestor that defines one, falling back to the service.
func TestEffectiveGrantIDInheritsDownward(t *testing.T) {
	be := Backend{ID: "svc", Routes: []Route{
		{Path: "/", Rule: RuleAuthorized},
		{Path: "/area", Rule: RuleAuthorized, OwnGrants: true},
		{Path: "/area/sub", Rule: RuleAuthorized},      // inherits → /area
		{Path: "/area/sub/deep", Rule: RuleAuthorized}, // inherits → /area (nearest ancestor)
		{Path: "/altro", Rule: RuleAuthorized},         // inherits → service
		{Path: "/area/own", Rule: RuleAuthorized, OwnGrants: true},
	}}
	cases := map[string]string{
		"/":              "svc",
		"/area":          "svc#/area",
		"/area/sub":      "svc#/area",
		"/area/sub/deep": "svc#/area",
		"/altro":         "svc",
		"/area/own":      "svc#/area/own", // its own wins over the ancestor
	}
	for _, rt := range be.Routes {
		if got, want := EffectiveGrantID(be, rt), cases[rt.Path]; got != want {
			t.Errorf("route %q: grant %q, want %q", rt.Path, got, want)
		}
	}
}

// A path must never be widened by a near-miss prefix: /areax is NOT inside /area.
func TestEffectiveGrantIDDoesNotMatchPartialSegment(t *testing.T) {
	be := Backend{ID: "svc", Routes: []Route{
		{Path: "/area", OwnGrants: true},
		{Path: "/areax"},
	}}
	if got := EffectiveGrantID(be, Route{Path: "/areax"}); got != "svc" {
		t.Errorf("/areax must not inherit from /area, got %q", got)
	}
}
