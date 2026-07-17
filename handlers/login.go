// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"xaltorka/auth"
	"xaltorka/i18n"
	"xaltorka/providers"
	"xaltorka/xtkui"
)

var loginTmpl = template.Must(template.New("login").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "login.title"}}</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>⛬ {{T .Lang "login.title"}}</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <form method="post" action="/login">
  <input type="hidden" name="next" value="{{.Next}}">
  <div class="field"><label>{{T .Lang "field.email"}}</label><input type="email" name="email" autocomplete="username" required></div>
  <div class="field"><label>{{T .Lang "field.password"}}</label><input type="password" name="password" autocomplete="current-password" required></div>
  <button class="btn primary">{{T .Lang "btn.continue"}}</button>
 </form>
 {{if .OIDC}}<div class="oidc"><div class="sep"><span>{{T .Lang "login.or"}}</span></div>
  {{range .OIDC}}<a class="btn oauth" href="/auth/{{.ID}}/start?next={{$.Next}}">{{T $.Lang "login.with"}} {{.Name}}</a>{{end}}
 </div>{{end}}
 {{if .Version}}<div class="ver-foot">⛬ Xal-Tor-Ka · {{.Version}}</div>{{end}}
 {{corner .Lang}}
</div></div></body></html>`))

var totpTmpl = template.Must(template.New("totp").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "totp.title"}}</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>{{T .Lang "totp.title"}}</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <form method="post" action="/auth/totp">
  <input type="hidden" name="next" value="{{.Next}}">
  <div class="field"><label>{{T .Lang "totp.code"}}</label><input name="code" inputmode="numeric" autocomplete="one-time-code" required></div>
  <button class="btn primary">{{T .Lang "btn.verify"}}</button>
 </form>
 {{corner .Lang}}
</div></div></body></html>`))

type formData struct {
	Next    string
	Error   string
	OIDC    []oidcButton // enabled OIDC providers ("Sign in with …" buttons)
	Version string
	Lang    string
}

// totpData builds the 2FA page payload, translating an optional error key.
func (s *Server) totpData(r *http.Request, next, errKey string) formData {
	lang := s.lang(r)
	msg := ""
	if errKey != "" {
		msg = i18n.T(lang, errKey)
	}
	return formData{Next: s.sanitizeNext(next), Error: msg, Lang: lang}
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	renderHTML(w, loginTmpl, s.loginData(r, r.URL.Query().Get("next"), ""), http.StatusOK)
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderHTML(w, loginTmpl, s.loginData(r, "/listing", "err.bad_request"), http.StatusBadRequest)
		return
	}
	next := s.sanitizeNext(r.PostFormValue("next"))
	email := r.PostFormValue("email")
	password := r.PostFormValue("password")

	if _, err := s.Local.Authenticate(email, password); err != nil {
		if !errors.Is(err, providers.ErrInvalidCredentials) {
			// unexpected internal error: fail-closed, log nothing sensitive
			renderHTML(w, loginTmpl, s.loginData(r, next, "err.internal"), http.StatusInternalServerError)
			return
		}
		// Local rejected → try the enabled LDAP/AD providers (bind). LDAP is
		// single-factor, so a successful bind completes the session immediately.
		if s.ldapLogin(w, r, email, password, next) {
			return
		}
		s.auditFail(r, "login", "email="+email)
		renderHTML(w, loginTmpl, s.loginData(r, next, "err.bad_credentials"), http.StatusUnauthorized)
		return
	}

	sess, err := s.Sessions.Create(email, "local")
	if err != nil {
		renderHTML(w, loginTmpl, s.loginData(r, next, "err.internal"), http.StatusInternalServerError)
		return
	}
	s.setSession(w, sess.ID)
	if s.Cfg.DisableTOTP {
		// 2FA disabled: the password is enough, session complete right away.
		s.Sessions.Complete2FA(sess.ID)
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login/totp?next="+url.QueryEscape(next), http.StatusSeeOther)
}

// ldapLogin tries the enabled LDAP/AD providers with the given credentials. On
// the first successful bind it creates a *completed* (single-factor) session —
// LDAP users have no local TOTP secret — sets the cookie, redirects, and returns
// true. Returns false if no provider authenticates the user; an unreachable or
// misconfigured server is logged and treated as a non-match (fail-closed).
func (s *Server) ldapLogin(w http.ResponseWriter, r *http.Request, email, password, next string) bool {
	for _, lp := range s.LDAP {
		if _, err := lp.Authenticate(email, password); err != nil {
			if !errors.Is(err, providers.ErrInvalidCredentials) {
				slog.Warn("ldap login: provider unreachable/misconfigured", "id", lp.ID(), "err", err)
			}
			continue
		}
		sess, err := s.Sessions.Create(email, lp.ID())
		if err != nil {
			renderHTML(w, loginTmpl, s.loginData(r, next, "err.internal"), http.StatusInternalServerError)
			return true
		}
		s.setSession(w, sess.ID)
		s.Sessions.Complete2FA(sess.ID) // a successful bind is the single factor
		http.Redirect(w, r, next, http.StatusSeeOther)
		return true
	}
	return false
}

func (s *Server) handleTOTPForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.session(r); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	renderHTML(w, totpTmpl, s.totpData(r, r.URL.Query().Get("next"), ""), http.StatusOK)
}

func (s *Server) handleTOTPSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		renderHTML(w, totpTmpl, s.totpData(r, "/listing", "err.bad_request"), http.StatusBadRequest)
		return
	}
	next := s.sanitizeNext(r.PostFormValue("next"))
	user, found := s.Users.Get(sess.Email)
	if !found || !auth.VerifyTOTP(user.TOTPSecret, r.PostFormValue("code"), time.Now()) {
		s.auditFail(r, "totp", "email="+sess.Email)
		renderHTML(w, totpTmpl, s.totpData(r, next, "err.bad_code"), http.StatusUnauthorized)
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
