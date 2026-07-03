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
	"xaltorka/models"
	"xaltorka/providers"
	"xaltorka/xtkui"
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

var providersTmpl = xtkui.LocParse("providers", `<section>
 <h2>{{T "admin.prov.h2"}}</h2>
 <p class="hint">{{T "admin.prov.hint"}}</p>
 {{if .Tested}}<div class="{{if .TestedOK}}ok{{else}}err{{end}}">{{if .TestedOK}}✓ {{T "admin.prov.test_ok"}}{{else}}✗ {{T "admin.prov.test_fail"}}{{end}} — <code>{{.TestedID}}</code></div>{{end}}
 <table><thead><tr><th>{{T "admin.prov.col.id"}}</th><th>{{T "admin.f.name"}}</th><th>{{T "admin.prov.col.type"}}</th><th>{{T "admin.prov.col.enabled"}}</th><th>{{T "admin.prov.f.issuer"}}</th><th>{{T "admin.prov.col.secret"}}</th><th></th></tr></thead><tbody>
 {{range .Rows}}<tr{{if not .Enabled}} class="off"{{end}}>
  <td><b>{{.ID}}</b>{{if not .Editable}} <span class="tag ro">{{T "admin.prov.src.config"}}</span>{{end}}</td>
  <td>{{.Name}}</td>
  <td><span class="tag">{{.Type}}</span></td>
  <td>{{if .Enabled}}<span class="tag">on</span>{{else}}<span class="tag ro">off</span>{{end}}</td>
  <td>{{if .Issuer}}<code>{{.Issuer}}</code>{{else}}—{{end}}</td>
  <td>{{if eq .Type "oidc"}}{{if .SecretSet}}<span class="tag">set</span>{{else}}<span class="tag ro">—</span>{{end}}{{else}}—{{end}}</td>
  <td><div class="actions">
   {{if .Editable}}
    <a class="btn sm" href="/admin/provider/edit?id={{.ID}}">{{T "admin.act.edit"}}</a>
    {{if eq .Type "oidc"}}<form class="inline" method="post" action="/admin/provider/test"><input type="hidden" name="id" value="{{.ID}}"><button class="btn sm">{{T "admin.prov.test"}}</button></form>{{end}}
    <form class="inline" method="post" action="/admin/provider/toggle"><input type="hidden" name="id" value="{{.ID}}"><button class="btn sm">{{if .Enabled}}{{T "admin.act.disable"}}{{else}}{{T "admin.act.enable"}}{{end}}</button></form>
    <form class="inline" method="post" action="/admin/provider/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="id" value="{{.ID}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>
   {{else}}<span class="hint">{{T "admin.prov.readonly"}}</span>{{end}}
  </div></td></tr>{{end}}
 {{if not .Rows}}<tr><td colspan="7" class="empty">{{T "admin.prov.none"}}</td></tr>{{end}}
 </tbody></table>
</section>
<section style="margin-top:1.4rem">
 <h2>{{T "admin.prov.add_h"}}</h2>
 <div class="card">
  <form method="post" action="/admin/provider/add">
   <input type="hidden" name="type" value="oidc">
   <table class="ftable"><tbody>
    <tr><th>{{T "admin.prov.f.preset"}}</th><td><select onchange="var v=this.value;if(v){var f=this.form;f.issuer.value=v;}">
      <option value="">{{T "admin.prov.preset.custom"}}</option>
      <option value="https://accounts.google.com">Google</option>
      <option value="https://login.microsoftonline.com/common/v2.0">Microsoft</option>
     </select></td><td class="fhelp">{{T "admin.prov.help.preset"}}</td></tr>
    <tr><th>{{T "admin.prov.col.id"}}</th><td><input name="id" placeholder="google" required></td><td class="fhelp">{{T "admin.prov.help.id"}}</td></tr>
    <tr><th>{{T "admin.f.name"}}</th><td><input name="name" placeholder="Google"></td><td class="fhelp">{{T "admin.prov.help.name"}}</td></tr>
    <tr><th>{{T "admin.prov.f.issuer"}}</th><td><input name="issuer" placeholder="https://accounts.google.com"></td><td class="fhelp">{{T "admin.prov.help.issuer"}}</td></tr>
    <tr><th>{{T "admin.prov.f.client_id"}}</th><td><input name="client_id" placeholder="1234….apps.googleusercontent.com"></td><td class="fhelp">{{T "admin.prov.help.client_id"}}</td></tr>
    <tr><th>{{T "admin.prov.f.client_secret"}}</th><td><input name="client_secret" type="password" autocomplete="new-password"></td><td class="fhelp">{{T "admin.prov.help.client_secret"}}</td></tr>
    <tr><th>{{T "admin.prov.col.enabled"}}</th><td><input type="checkbox" name="enabled"></td><td class="fhelp">{{T "admin.prov.help.enabled"}}</td></tr>
   </tbody></table>
   <div class="actions"><button class="btn primary">{{T "btn.add"}}</button></div>
  </form>
 </div>
</section>`)

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
	s.renderAdminPage(w, r, "providers", providersTmpl, provPageData{
		Rows: rows, Tested: tested != "", TestedID: tested, TestedOK: r.URL.Query().Get("ok") == "1",
	})
}

type provEditData struct {
	ID, Name, Issuer, ClientID, Redirect string
	Enabled, SecretSet                   bool
}

var providerEditTmpl = xtkui.LocParse("provedit", `<h1>{{T "admin.prov.edit_h1"}} «{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}}»</h1>
 <div class="card">
  <form method="post" action="/admin/provider/edit">
   <input type="hidden" name="id" value="{{.ID}}">
   <table class="ftable"><tbody>
    <tr><th>{{T "admin.prov.col.id"}}</th><td><input value="{{.ID}}" disabled></td><td class="fhelp">{{T "admin.prov.help.id_ro"}}</td></tr>
    <tr><th>{{T "admin.f.name"}}</th><td><input name="name" value="{{.Name}}"></td><td class="fhelp">{{T "admin.prov.help.name"}}</td></tr>
    <tr><th>{{T "admin.prov.f.issuer"}}</th><td><input name="issuer" value="{{.Issuer}}" required></td><td class="fhelp">{{T "admin.prov.help.issuer"}}</td></tr>
    <tr><th>{{T "admin.prov.f.client_id"}}</th><td><input name="client_id" value="{{.ClientID}}" required></td><td class="fhelp">{{T "admin.prov.help.client_id"}}</td></tr>
    <tr><th>{{T "admin.prov.f.client_secret"}}</th><td><input name="client_secret" type="password" autocomplete="new-password" placeholder="{{if .SecretSet}}{{T "admin.prov.secret_keep"}}{{else}}{{T "admin.prov.secret_unset"}}{{end}}"></td><td class="fhelp">{{T "admin.prov.help.client_secret"}}</td></tr>
    <tr><th>{{T "admin.prov.col.enabled"}}</th><td><input type="checkbox" name="enabled"{{if .Enabled}} checked{{end}}></td><td class="fhelp">{{T "admin.prov.help.enabled"}}</td></tr>
    <tr><th>{{T "admin.prov.redirect"}}</th><td colspan="2"><code>{{.Redirect}}</code></td></tr>
   </tbody></table>
   <div class="actions" style="margin-top:1rem">
    <button class="btn primary">{{T "btn.save"}}</button><a class="btn" href="/admin/providers">{{T "admin.cancel"}}</a></div>
  </form>
 </div>`)

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
		s.renderAdminPage(w, r, "providers", providerEditTmpl, provEditData{
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
