// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package main

import (
	"testing"

	"xaltorka/handlers"
	"xaltorka/models"
)

func TestBuildOIDC(t *testing.T) {
	cfg := &models.Config{
		Server: models.ServerCfg{ExternalURL: "https://gate.example.it/"},
		Providers: []models.ProviderCfg{
			{ID: "local", Type: "local", Enabled: true},
			{ID: "google", Type: "oidc", Name: "Google", Enabled: true,
				Issuer: "https://accounts.google.com", ClientID: "cid"},
			{ID: "microsoft", Type: "oidc", Name: "Microsoft", Enabled: false,
				Issuer: "https://login.microsoftonline.com/t/v2.0", ClientID: "cid2"},
		},
	}
	sec := models.Secrets{Providers: map[string]models.ProviderSecret{
		"google": {ClientSecret: "secret"},
	}}

	got := handlers.BuildOIDC(cfg.Providers, sec, cfg.Server.ExternalURL)
	if len(got) != 1 {
		t.Fatalf("buildOIDC = %d providers, want 1 (only enabled oidc)", len(got))
	}
	p, ok := got["google"]
	if !ok {
		t.Fatal("google not present in the registry")
	}
	if p.ID() != "google" || p.Type() != "oidc" {
		t.Errorf("provider = (%s,%s), want (google,oidc)", p.ID(), p.Type())
	}
	if _, isMS := got["microsoft"]; isMS {
		t.Error("microsoft is disabled and must not appear")
	}
	if _, isLocal := got["local"]; isLocal {
		t.Error("local is not oidc and must not appear in the OIDC registry")
	}
}
