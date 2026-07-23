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
