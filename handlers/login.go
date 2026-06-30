// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"xaltorka/auth"
	"xaltorka/providers"
)

var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · Accesso</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>⛬ Accesso</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <form method="post" action="/login">
  <input type="hidden" name="next" value="{{.Next}}">
  <div class="field"><label>Email</label><input type="email" name="email" autocomplete="username" required></div>
  <div class="field"><label>Password</label><input type="password" name="password" autocomplete="current-password" required></div>
  <button class="btn primary">Continua</button>
 </form>
 {{if .OIDC}}<div class="oidc"><div class="sep"><span>oppure</span></div>
  {{range .OIDC}}<a class="btn oauth" href="/auth/{{.ID}}/start?next={{$.Next}}">Accedi con {{.Name}}</a>{{end}}
 </div>{{end}}
 {{if .Version}}<div class="ver-foot">⛬ Xal-Tor-Ka · {{.Version}}</div>{{end}}
</div></div></body></html>`))

var totpTmpl = template.Must(template.New("totp").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · 2FA</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>Verifica a due fattori</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <form method="post" action="/auth/totp">
  <input type="hidden" name="next" value="{{.Next}}">
  <div class="field"><label>Codice TOTP</label><input name="code" inputmode="numeric" autocomplete="one-time-code" required></div>
  <button class="btn primary">Verifica</button>
 </form>
</div></div></body></html>`))

type formData struct {
	Next    string
	Error   string
	OIDC    []oidcButton // provider OIDC abilitati (bottoni "Accedi con …")
	Version string
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	renderHTML(w, loginTmpl, s.loginData(r.URL.Query().Get("next"), ""), http.StatusOK)
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderHTML(w, loginTmpl, s.loginData("/listing", "richiesta non valida"), http.StatusBadRequest)
		return
	}
	next := s.sanitizeNext(r.PostFormValue("next"))
	email := r.PostFormValue("email")
	password := r.PostFormValue("password")

	if _, err := s.Local.Authenticate(email, password); err != nil {
		if !errors.Is(err, providers.ErrInvalidCredentials) {
			// unexpected internal error: fail-closed, log nothing sensitive
			renderHTML(w, loginTmpl, s.loginData(next, "errore interno"), http.StatusInternalServerError)
			return
		}
		s.auditFail(r, "login", "email="+email)
		renderHTML(w, loginTmpl, s.loginData(next, "credenziali non valide"), http.StatusUnauthorized)
		return
	}

	sess, err := s.Sessions.Create(email, "local")
	if err != nil {
		renderHTML(w, loginTmpl, s.loginData(next, "errore interno"), http.StatusInternalServerError)
		return
	}
	s.setSession(w, sess.ID)
	if s.Cfg.DisableTOTP {
		// 2FA disattivato: la password basta, sessione subito completa.
		s.Sessions.Complete2FA(sess.ID)
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login/totp?next="+url.QueryEscape(next), http.StatusSeeOther)
}

func (s *Server) handleTOTPForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.session(r); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	renderHTML(w, totpTmpl, formData{Next: s.sanitizeNext(r.URL.Query().Get("next"))}, http.StatusOK)
}

func (s *Server) handleTOTPSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		renderHTML(w, totpTmpl, formData{Next: "/listing", Error: "richiesta non valida"}, http.StatusBadRequest)
		return
	}
	next := s.sanitizeNext(r.PostFormValue("next"))
	user, found := s.Users.Get(sess.Email)
	if !found || !auth.VerifyTOTP(user.TOTPSecret, r.PostFormValue("code"), time.Now()) {
		s.auditFail(r, "totp", "email="+sess.Email)
		renderHTML(w, totpTmpl, formData{Next: next, Error: "codice non valido"}, http.StatusUnauthorized)
		return
	}
	s.Sessions.Complete2FA(sess.ID)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess, ok := s.session(r); ok {
		s.Sessions.Delete(sess.ID)
	}
	s.clearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
