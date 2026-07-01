// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"html/template"
	"net/http"
	"net/url"
)

// forbiddenAdminTmpl is shown to a logged-in NON-admin user hitting /admin: it
// offers a server-side logout (the session cookie is HttpOnly, so JS can't clear
// it) to switch account, plus a link back to the dashboard.
var forbiddenAdminTmpl = template.Must(template.New("forbidden").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · Area riservata</title><link rel="stylesheet" href="/assets/admin.css"></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>Area riservata</h1>
 <div class="err">Questo account non è amministratore.</div>
 <p class="hint">Sei connesso come <strong>{{.Email}}</strong>. Per l'area di amministrazione serve un account con flag admin.</p>
 <form method="post" action="/logout"><button class="btn primary">Esci e cambia account</button></form>
 <p style="margin-top:.9rem"><a href="/listing">← vai alla dashboard</a></p>
</div></div></body></html>`))

// Admin authorization (BLUEPRINT §9 — unified model): the administrator is a
// normal user (users.json) with the admin=true flag. Access to /admin requires
// three conditions: IP whitelist (network level) + a valid user session (2FA
// completed) + the admin flag. There is no longer a separate admin password.
func (s *Server) adminGuard(w http.ResponseWriter, r *http.Request) bool {
	if !s.adminAllowed(r) {
		s.auditFail(r, "admin_ip", "")
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	sess, ok := s.session(r)
	if !ok || !sess.TwoFADone {
		http.Redirect(w, r, "/login?next="+url.QueryEscape("/admin"), http.StatusSeeOther)
		return false
	}
	u, found := s.Users.Get(sess.Email)
	if !found || !u.Admin {
		s.auditFail(r, "admin_denied", "email="+sess.Email)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = forbiddenAdminTmpl.Execute(w, struct{ Email string }{sess.Email})
		return false
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return false
	}
	return true
}
