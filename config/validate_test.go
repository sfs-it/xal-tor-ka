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
		t.Fatalf("bundle valido rifiutato: %v", err)
	}
}

func TestValidateBadRule(t *testing.T) {
	b := validBundle()
	b.Config.Backends[0].Routes[0].Rule = "nope"
	if Validate(b) == nil {
		t.Error("regola invalida doveva fallire")
	}
}

func TestValidateBadPort(t *testing.T) {
	b := validBundle()
	b.Config.Backends[0].Routes[0].Upstream = "http://10.0.0.1:0"
	if Validate(b) == nil {
		t.Error("porta 0 doveva fallire")
	}
}

func TestValidateUnknownUserBackend(t *testing.T) {
	b := validBundle()
	b.Users.Users[0].Backends = []string{"ghost"}
	if Validate(b) == nil {
		t.Error("riferimento a backend inesistente doveva fallire")
	}
}

func TestValidateAuthModeNeedsProvider(t *testing.T) {
	b := validBundle()
	b.Config.Providers[0].Enabled = false
	if Validate(b) == nil {
		t.Error("auth_mode senza provider abilitati doveva fallire")
	}
}

func TestValidateOIDCProviderOK(t *testing.T) {
	b := validBundle()
	b.Config.Providers = append(b.Config.Providers, models.ProviderCfg{
		ID: "google", Type: "oidc", Enabled: true,
		Issuer: "https://accounts.google.com", ClientID: "cid",
	})
	if err := Validate(b); err != nil {
		t.Fatalf("provider oidc valido rifiutato: %v", err)
	}
}

func TestValidateOIDCEnabledNeedsIssuer(t *testing.T) {
	b := validBundle()
	b.Config.Providers = append(b.Config.Providers, models.ProviderCfg{
		ID: "google", Type: "oidc", Enabled: true, ClientID: "cid", // issuer mancante
	})
	if Validate(b) == nil {
		t.Error("oidc abilitato senza issuer doveva fallire")
	}
}

func TestValidateOIDCEnabledNeedsClientID(t *testing.T) {
	b := validBundle()
	b.Config.Providers = append(b.Config.Providers, models.ProviderCfg{
		ID: "google", Type: "oidc", Enabled: true, Issuer: "https://accounts.google.com", // client_id mancante
	})
	if Validate(b) == nil {
		t.Error("oidc abilitato senza client_id doveva fallire")
	}
}

func TestValidateOIDCDisabledNoRequirements(t *testing.T) {
	// Un provider oidc disabilitato (esempio in config.json) non deve richiedere issuer/client_id.
	b := validBundle()
	b.Config.Providers = append(b.Config.Providers, models.ProviderCfg{
		ID: "google", Type: "oidc", Enabled: false,
	})
	if err := Validate(b); err != nil {
		t.Fatalf("provider oidc disabilitato rifiutato: %v", err)
	}
}
