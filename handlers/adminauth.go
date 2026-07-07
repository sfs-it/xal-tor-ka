// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"html/template"
	"net/http"
	"net/url"

	"xaltorka/i18n"
	"xaltorka/xtkui"
)

// forbiddenAdminTmpl is shown to a logged-in NON-admin user hitting /admin: it
// offers a server-side logout (the session cookie is HttpOnly, so JS can't clear
// it) to switch account, plus a link back to the dashboard.
var forbiddenAdminTmpl = template.Must(template.New("forbidden").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "blocked.title"}}</title><link rel="stylesheet" href="/assets/admin.css"></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>{{T .Lang "blocked.title"}}</h1>
 <div class="err">{{T .Lang "blocked.not_admin"}}</div>
 <p class="hint">{{T .Lang "blocked.you_are"}} <strong>{{.Email}}</strong>. {{T .Lang "blocked.need_admin"}}</p>
 <form method="post" action="/logout"><button class="btn primary">{{T .Lang "blocked.logout_switch"}}</button></form>
 <p style="margin-top:.9rem"><a href="/listing">{{T .Lang "blocked.to_dashboard"}}</a></p>
</div></div>{{corner .Lang}}</body></html>`))

// Admin authorization (BLUEPRINT §9 — unified model): the administrator is a
// normal user (users.json) with the admin=true flag. Access to /admin requires
// three conditions: IP whitelist (network level) + a valid user session (2FA
// completed) + the admin flag. There is no longer a separate admin password.
func (s *Server) adminGuard(w http.ResponseWriter, r *http.Request) bool {
	if !s.adminSessionOK(w, r) {
		return false
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(s.lang(r), "err.bad_request"), http.StatusBadRequest)
		return false
	}
	return true
}

// adminSessionOK enforces the admin gate (IP whitelist + valid 2FA session + admin
// user) WITHOUT parsing the form. Use it when the body must be preserved for a
// downstream reader — e.g. reverse-proxying POSTs to an extension. adminGuard wraps
// this and additionally parses the form for the core's own handlers.
func (s *Server) adminSessionOK(w http.ResponseWriter, r *http.Request) bool {
	if !s.adminAllowed(r) {
		s.auditFail(r, "admin_ip", "")
		http.Error(w, i18n.T(s.lang(r), "err.forbidden"), http.StatusForbidden)
		return false
	}
	sess, ok := s.session(r)
	if !ok || !sess.TwoFADone {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return false
	}
	u, found := s.Users.Get(sess.Email)
	if !found || !u.Admin {
		s.auditFail(r, "admin_denied", "email="+sess.Email)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = forbiddenAdminTmpl.Execute(w, struct{ Email, Lang string }{sess.Email, s.lang(r)})
		return false
	}
	return true
}
