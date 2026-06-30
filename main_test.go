package main

import (
	"testing"

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

	got := buildOIDC(cfg, sec)
	if len(got) != 1 {
		t.Fatalf("buildOIDC = %d provider, want 1 (solo oidc abilitati)", len(got))
	}
	p, ok := got["google"]
	if !ok {
		t.Fatal("google non presente nel registry")
	}
	if p.ID() != "google" || p.Type() != "oidc" {
		t.Errorf("provider = (%s,%s), want (google,oidc)", p.ID(), p.Type())
	}
	if _, isMS := got["microsoft"]; isMS {
		t.Error("microsoft è disabilitato e non deve comparire")
	}
	if _, isLocal := got["local"]; isLocal {
		t.Error("local non è oidc e non deve comparire nel registry OIDC")
	}
}
