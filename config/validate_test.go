// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package config

import (
	"testing"

	"xaltorka/models"
)

func validBundle() *Bundle {
	return &Bundle{
		Config: models.Config{
			AuthMode: true,
			Server:   models.ServerCfg{Listen: ":8080", TrustedProxies: []string{"127.0.0.1/32"}},
			TLS:      models.TLSCfg{Mode: "selfsigned"},
			Session:  models.SessionCfg{Store: "memory"},
			Admin:    models.AdminCfg{IPWhitelist: []string{"127.0.0.1/32"}},
			Providers: []models.ProviderCfg{
				{ID: "local", Type: "local", Enabled: true},
			},
			Backends: []models.Backend{
				{ID: "b1", Host: "h1", Routes: []models.Route{{Path: "/", Rule: "public", Upstream: "http://10.0.0.1:80"}}},
			},
		},
		Users: models.Users{Users: []models.User{
			{Email: "u@x", Provider: "local", PasswordHash: "x", Backends: []string{"b1"}},
		}},
	}
}

func TestValidateOK(t *testing.T) {
	if err := Validate(validBundle()); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
}

func TestValidateBadRule(t *testing.T) {
	b := validBundle()
	b.Config.Backends[0].Routes[0].Rule = "nope"
	if Validate(b) == nil {
		t.Error("invalid rule should have failed")
	}
}

func TestValidateBadPort(t *testing.T) {
	b := validBundle()
	b.Config.Backends[0].Routes[0].Upstream = "http://10.0.0.1:0"
	if Validate(b) == nil {
		t.Error("port 0 should have failed")
	}
}

func TestValidateUnknownUserBackend(t *testing.T) {
	b := validBundle()
	b.Users.Users[0].Backends = []string{"ghost"}
	if Validate(b) == nil {
		t.Error("reference to nonexistent backend should have failed")
	}
}

func TestValidateAuthModeNeedsProvider(t *testing.T) {
	b := validBundle()
	b.Config.Providers[0].Enabled = false
	if Validate(b) == nil {
		t.Error("auth_mode without enabled providers should have failed")
	}
}

func TestValidateOIDCProviderOK(t *testing.T) {
	b := validBundle()
	b.Config.Providers = append(b.Config.Providers, models.ProviderCfg{
		ID: "google", Type: "oidc", Enabled: true,
		Issuer: "https://accounts.google.com", ClientID: "cid",
	})
	if err := Validate(b); err != nil {
		t.Fatalf("valid oidc provider rejected: %v", err)
	}
}

func TestValidateOIDCEnabledNeedsIssuer(t *testing.T) {
	b := validBundle()
	b.Config.Providers = append(b.Config.Providers, models.ProviderCfg{
		ID: "google", Type: "oidc", Enabled: true, ClientID: "cid", // issuer missing
	})
	if Validate(b) == nil {
		t.Error("enabled oidc without issuer should have failed")
	}
}

func TestValidateOIDCEnabledNeedsClientID(t *testing.T) {
	b := validBundle()
	b.Config.Providers = append(b.Config.Providers, models.ProviderCfg{
		ID: "google", Type: "oidc", Enabled: true, Issuer: "https://accounts.google.com", // client_id missing
	})
	if Validate(b) == nil {
		t.Error("enabled oidc without client_id should have failed")
	}
}

func TestValidateOIDCDisabledNoRequirements(t *testing.T) {
	// A disabled oidc provider (example in config.json) must not require issuer/client_id.
	b := validBundle()
	b.Config.Providers = append(b.Config.Providers, models.ProviderCfg{
		ID: "google", Type: "oidc", Enabled: false,
	})
	if err := Validate(b); err != nil {
		t.Fatalf("disabled oidc provider rejected: %v", err)
	}
}

func TestValidateBadBackendIPAllow(t *testing.T) {
	b := validBundle()
	b.Config.Backends[0].IPAllow = []string{"not-a-cidr"}
	if Validate(b) == nil {
		t.Error("ip_allow with invalid CIDR should have failed")
	}
}

func TestValidateGoodBackendIPAllow(t *testing.T) {
	b := validBundle()
	b.Config.Backends[0].IPAllow = []string{"10.0.0.0/24", "203.0.113.7/32"}
	if err := Validate(b); err != nil {
		t.Fatalf("valid ip_allow rejected: %v", err)
	}
}

func TestValidateBadAdminIPWhitelist(t *testing.T) {
	b := validBundle()
	b.Services.AdminIPWhitelist = []string{"999.999.0.0/16"}
	if Validate(b) == nil {
		t.Error("admin_ip_whitelist with invalid CIDR should have failed")
	}
}
