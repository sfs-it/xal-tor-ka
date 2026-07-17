// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"testing"

	"xaltorka/models"
)

func TestBuildLDAP(t *testing.T) {
	provs := []models.ProviderCfg{
		{ID: "corp", Type: "ldap", Enabled: true, LDAPURL: "ldaps://dc:636", LDAPBindDNTemplate: "%s@corp.example.com"},
		{ID: "off", Type: "ldap", Enabled: false, LDAPURL: "ldaps://x:636", LDAPBindDNTemplate: "%s"}, // disabled
		{ID: "incomplete", Type: "ldap", Enabled: true, LDAPURL: ""},                                  // no url → skipped
		{ID: "notmpl", Type: "ldap", Enabled: true, LDAPURL: "ldaps://x:636"},                         // no bind template → skipped
		{ID: "google", Type: "oidc", Enabled: true},                                                   // not ldap
	}
	got := BuildLDAP(provs)
	if len(got) != 1 {
		t.Fatalf("BuildLDAP returned %d providers, want 1", len(got))
	}
	if got[0].ID() != "corp" || got[0].Type() != "ldap" {
		t.Errorf("provider = %q/%q, want corp/ldap", got[0].ID(), got[0].Type())
	}
}
