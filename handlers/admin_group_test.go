// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"testing"

	"xaltorka/models"
)

// The Services table must tell the same story as the proxy: a service mounted on a
// path of an existing domain belongs WITH that domain, not as a lone row — and the
// group header already names the site, so children must not repeat it.
func TestGroupServiceBackends(t *testing.T) {
	bs := []models.Backend{
		{ID: "sfsit", Name: "sfsit (hosting)", Host: "sfs.it"},
		{ID: "sfsit-bottiglia-tunnel", Name: "bottiglia2 (tunnel)", Host: "sfs.it"},
		{ID: "centrosub", Name: "centrosub (hosting)", Host: "centrosub.com"},
		{ID: "segnalapa", Name: "segnalapa/httpdocs (hosting)", Host: "segnalapa.it",
			Hosting: &models.HostingRef{Site: "segnalapa", Vhost: "httpdocs"}},
		{ID: "segnalapa-api", Name: "segnalapa/api (hosting)", Host: "api.segnalapa.it",
			Hosting: &models.HostingRef{Site: "segnalapa", Vhost: "api"}},
	}
	got := groupServiceBackends(bs)

	bySite := map[string][]svcItem{}
	for _, g := range got {
		bySite[g.Site] = append(bySite[g.Site], g.Backends...)
	}

	// A shared domain with no hosting marker groups under the domain itself.
	if items := bySite["sfs.it"]; len(items) != 2 {
		t.Fatalf("the two services on sfs.it must share a group, got %d", len(items))
	}
	// A hosting site groups its vhosts even though they live on different hostnames.
	if items := bySite["segnalapa"]; len(items) != 2 {
		t.Fatalf("segnalapa's vhosts must group by site, got %d", len(items))
	}
	// A lone service stays ungrouped.
	for _, g := range got {
		if g.Site == "centrosub.com" {
			t.Error("a single service on a domain must not get a group header")
		}
	}

	// Labels: the header carries the site, the rows must not repeat it.
	for _, it := range bySite["segnalapa"] {
		if it.Label != "httpdocs" && it.Label != "api" {
			t.Errorf("grouped vhost should show its short name, got %q", it.Label)
		}
	}
	// Outside a group the full name is kept.
	for _, g := range got {
		if g.Site == "" && g.Backends[0].ID == "centrosub" && g.Backends[0].Label != "centrosub (hosting)" {
			t.Errorf("ungrouped service must keep its full name, got %q", g.Backends[0].Label)
		}
	}
}
