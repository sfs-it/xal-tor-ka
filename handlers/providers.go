// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"xaltorka/config"
	"xaltorka/i18n"
	"xaltorka/legacyhtml"
	"xaltorka/models"
	"xaltorka/providers"
)

// BuildOIDC constructs the enabled OIDC clients from a provider set + secrets. The
// redirect URL is derived from external_url and must match what is registered with
// each IdP: <external_url>/auth/<id>/callback. Discovery is lazy, so an unreachable
// issuer here does not block startup. Incomplete providers (no issuer/client_id)
// are skipped — fail-closed.
func BuildOIDC(provs []models.ProviderCfg, sec models.Secrets, externalURL string) map[string]*providers.OIDC {
	out := map[string]*providers.OIDC{}
	base := strings.TrimRight(externalURL, "/")
	for _, p := range provs {
		if p.Type != "oidc" || !p.Enabled || p.Issuer == "" || p.ClientID == "" {
			continue
		}
		redirect := base + "/auth/" + p.ID + "/callback"
		out[p.ID] = providers.NewOIDC(
			p.ID, p.Name, p.Issuer,
			p.ClientID, sec.Providers[p.ID].ClientSecret,
			redirect, nil,
		)
		slog.Info("oidc provider enabled", "id", p.ID, "issuer", p.Issuer, "redirect", redirect)
	}
	return out
}

// BuildLDAP constructs the enabled LDAP providers from a provider set. LDAP is
// credential-based (bind), tried by the login handler after Local. Incomplete
// providers (no url / bind template) are skipped — fail-closed.
func BuildLDAP(provs []models.ProviderCfg) []*providers.LDAP {
	var out []*providers.LDAP
	for _, p := range provs {
		if p.Type != "ldap" || !p.Enabled || p.LDAPURL == "" || p.LDAPBindDNTemplate == "" {
			continue
		}
		out = append(out, providers.NewLDAP(p.ID, p.LDAPURL, p.LDAPBindDNTemplate, p.LDAPStartTLS, p.LDAPInsecureSkipVerify))
		slog.Info("ldap provider enabled", "id", p.ID, "url", p.LDAPURL, "start_tls", p.LDAPStartTLS)
	}
	return out
}

// mergeProviders overlays the runtime (services.json) providers on top of the base
// (config.json) ones, matched by id: a runtime entry with an existing id replaces
// the base one, a new id is appended.
func mergeProviders(base, runtime []models.ProviderCfg) []models.ProviderCfg {
	out := make([]models.ProviderCfg, 0, len(base)+len(runtime))
	out = append(out, base...)
	for _, rp := range runtime {
		replaced := false
		for i := range out {
			if out[i].ID == rp.ID {
				out[i] = rp
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, rp)
		}
	}
	return out
}

// providerRedirect returns the OIDC redirect URI to register with the IdP.
func (s *Server) providerRedirect(id string) string {
	return strings.TrimRight(s.Cfg.Server.ExternalURL, "/") + "/auth/" + id + "/callback"
}

// isConfigProvider reports whether id belongs to a static config.json provider
// (read-only in the UI). Runtime providers live in services.json and are editable.
func (s *Server) isConfigProvider(id string) bool {
	for _, p := range s.BaseProviders {
		if p.ID == id {
			return true
		}
	}
	return false
}

type provRow struct {
	ID, Name, Type, Issuer string
	Enabled                bool
	Editable               bool // in services.json → editable/deletable from the UI
	SecretSet              bool
	Redirect               string
}

type provPageData struct {
	Rows     []provRow
	Tested   bool
	TestedID string
	TestedOK bool
}

// handleAdminProviders renders the provider list + add form.
func (s *Server) handleAdminProviders(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	svcIDs := map[string]bool{}
	for _, p := range svc.Providers {
		svcIDs[p.ID] = true
	}
	sec, _ := config.LoadSecretsRaw(s.SecretsPath)
	rows := make([]provRow, 0)
	for _, p := range s.currentProviders() {
		rows = append(rows, provRow{
			ID: p.ID, Name: p.Name, Type: p.Type, Issuer: p.Issuer, Enabled: p.Enabled,
			Editable:  svcIDs[p.ID],
			SecretSet: sec.Providers[p.ID].ClientSecret != "",
			Redirect:  s.providerRedirect(p.ID),
		})
	}
	tested := r.URL.Query().Get("tested")
	s.renderAdminPage(w, r, "providers", legacyhtml.ProvidersTmpl, provPageData{
		Rows: rows, Tested: tested != "", TestedID: tested, TestedOK: r.URL.Query().Get("ok") == "1",
	})
}

type provEditData struct {
	ID, Name, Issuer, ClientID, Redirect string
	Enabled, SecretSet                   bool
}

// handleProviderEditForm shows the edit page for a runtime (services.json) provider.
func (s *Server) handleProviderEditForm(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	svc, _ := config.LoadServices(s.ServicesPath)
	for _, p := range svc.Providers {
		if p.ID != id {
			continue
		}
		sec, _ := config.LoadSecretsRaw(s.SecretsPath)
		s.renderAdminPage(w, r, "providers", legacyhtml.ProviderEditTmpl, provEditData{
			ID: p.ID, Name: p.Name, Issuer: p.Issuer, ClientID: p.ClientID,
			Enabled: p.Enabled, SecretSet: sec.Providers[p.ID].ClientSecret != "",
			Redirect: s.providerRedirect(p.ID),
		})
		return
	}
	http.Error(w, i18n.T(s.lang(r), "err.backend_not_found"), http.StatusNotFound)
}

// setProviderSecret writes (or clears) the client_secret for a provider in
// secrets.json, keyed by id, preserving the other secrets.
func (s *Server) setProviderSecret(id, secret string) error {
	sec, err := config.LoadSecretsRaw(s.SecretsPath)
	if err != nil {
		return err
	}
	if sec.Providers == nil {
		sec.Providers = map[string]models.ProviderSecret{}
	}
	ps := sec.Providers[id]
	ps.ClientSecret = secret
	sec.Providers[id] = ps
	return config.SaveSecrets(s.SecretsPath, s.BackupsDir, sec)
}

// handleProviderAdd creates a new runtime OIDC provider (services.json + secret).
func (s *Server) handleProviderAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	lang := s.lang(r)
	id := strings.TrimSpace(r.PostFormValue("id"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	issuer := strings.TrimSpace(r.PostFormValue("issuer"))
	clientID := strings.TrimSpace(r.PostFormValue("client_id"))
	secret := r.PostFormValue("client_secret")
	enabled := r.PostFormValue("enabled") != ""
	if id == "" {
		http.Error(w, i18n.T(lang, "err.prov_id_required"), http.StatusBadRequest)
		return
	}
	if s.isConfigProvider(id) {
		http.Error(w, i18n.T(lang, "err.prov_exists"), http.StatusBadRequest)
		return
	}
	if enabled && (issuer == "" || clientID == "") {
		http.Error(w, i18n.T(lang, "err.prov_incomplete"), http.StatusBadRequest)
		return
	}
	// Write the secret first so the Reload inside mutateServices rebuilds the OIDC
	// client with it available.
	if secret != "" {
		if err := s.setProviderSecret(id, secret); err != nil {
			http.Error(w, i18n.T(lang, "err.internal"), http.StatusInternalServerError)
			return
		}
	}
	err := s.mutateServices(func(svc *models.Services) error {
		for _, p := range svc.Providers {
			if p.ID == id {
				return fmt.Errorf("%s", i18n.T(lang, "err.prov_exists"))
			}
		}
		svc.Providers = append(svc.Providers, models.ProviderCfg{
			ID: id, Type: "oidc", Name: name, Enabled: enabled, Issuer: issuer, ClientID: clientID,
		})
		return nil
	})
	s.providerRedirectAfter(w, r, err)
}

// handleProviderEdit updates a runtime provider; an empty client_secret keeps the
// existing one (write-only field).
func (s *Server) handleProviderEdit(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	lang := s.lang(r)
	id := r.PostFormValue("id")
	name := strings.TrimSpace(r.PostFormValue("name"))
	issuer := strings.TrimSpace(r.PostFormValue("issuer"))
	clientID := strings.TrimSpace(r.PostFormValue("client_id"))
	secret := r.PostFormValue("client_secret")
	enabled := r.PostFormValue("enabled") != ""
	if enabled && (issuer == "" || clientID == "") {
		http.Error(w, i18n.T(lang, "err.prov_incomplete"), http.StatusBadRequest)
		return
	}
	if secret != "" {
		if err := s.setProviderSecret(id, secret); err != nil {
			http.Error(w, i18n.T(lang, "err.internal"), http.StatusInternalServerError)
			return
		}
	}
	err := s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Providers {
			if svc.Providers[i].ID == id {
				svc.Providers[i].Name = name
				svc.Providers[i].Issuer = issuer
				svc.Providers[i].ClientID = clientID
				svc.Providers[i].Enabled = enabled
				return nil
			}
		}
		return fmt.Errorf("%s", i18n.T(lang, "err.backend_not_found"))
	})
	s.providerRedirectAfter(w, r, err)
}

// handleProviderDel removes a runtime provider and its secret.
func (s *Server) handleProviderDel(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		out := svc.Providers[:0]
		for _, p := range svc.Providers {
			if p.ID != id {
				out = append(out, p)
			}
		}
		svc.Providers = out
		return nil
	})
	if err == nil {
		_ = s.setProviderSecret(id, "") // best-effort secret cleanup
	}
	s.providerRedirectAfter(w, r, err)
}

// handleProviderToggle enables/disables a runtime provider.
func (s *Server) handleProviderToggle(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Providers {
			if svc.Providers[i].ID == id {
				svc.Providers[i].Enabled = !svc.Providers[i].Enabled
				return nil
			}
		}
		return fmt.Errorf("%s", i18n.T(s.lang(r), "err.backend_not_found"))
	})
	s.providerRedirectAfter(w, r, err)
}

// handleProviderTest runs an OIDC discovery against a saved provider and redirects
// back with a ✓/✗ banner. Never persists anything.
func (s *Server) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	var cfg *models.ProviderCfg
	for _, p := range s.currentProviders() {
		if p.ID == id {
			pc := p
			cfg = &pc
			break
		}
	}
	ok := false
	if cfg != nil && cfg.Type == "oidc" && cfg.Issuer != "" && cfg.ClientID != "" {
		sec, _ := config.LoadSecretsRaw(s.SecretsPath)
		p := providers.NewOIDC(cfg.ID, cfg.Name, cfg.Issuer, cfg.ClientID,
			sec.Providers[cfg.ID].ClientSecret, s.providerRedirect(cfg.ID), nil)
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if _, err := p.AuthURL(ctx, "probe", "probe"); err == nil {
			ok = true
		}
	}
	okv := "0"
	if ok {
		okv = "1"
	}
	http.Redirect(w, r, "/admin/providers?tested="+id+"&ok="+okv, http.StatusSeeOther)
}

// providerRedirectAfter mirrors afterMutation but returns to the providers page.
func (s *Server) providerRedirectAfter(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin/providers", http.StatusSeeOther)
}
