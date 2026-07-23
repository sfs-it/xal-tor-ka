// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"xaltorka/auth"
	"xaltorka/i18n"
	"xaltorka/legacyhtml"
	"xaltorka/providers"
)

type formData struct {
	Next    string
	Error   string
	OIDC    []oidcButton // enabled OIDC providers ("Sign in with …" buttons)
	Code    bool         // one-time-code login enabled → show the "accedi con un codice" link
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
	renderHTML(w, legacyhtml.LoginTmpl, s.loginData(r, r.URL.Query().Get("next"), ""), http.StatusOK)
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderHTML(w, legacyhtml.LoginTmpl, s.loginData(r, "/listing", "err.bad_request"), http.StatusBadRequest)
		return
	}
	next := s.sanitizeNext(r.PostFormValue("next"))
	email := r.PostFormValue("email")
	password := r.PostFormValue("password")

	if _, err := s.Local.Authenticate(email, password); err != nil {
		if !errors.Is(err, providers.ErrInvalidCredentials) {
			// unexpected internal error: fail-closed, log nothing sensitive
			renderHTML(w, legacyhtml.LoginTmpl, s.loginData(r, next, "err.internal"), http.StatusInternalServerError)
			return
		}
		// Local rejected → try the enabled LDAP/AD providers (bind). LDAP is
		// single-factor, so a successful bind completes the session immediately.
		if s.ldapLogin(w, r, email, password, next) {
			return
		}
		s.auditFail(r, "login", "email="+email)
		renderHTML(w, legacyhtml.LoginTmpl, s.loginData(r, next, "err.bad_credentials"), http.StatusUnauthorized)
		return
	}

	sess, err := s.Sessions.Create(email, "local")
	if err != nil {
		renderHTML(w, legacyhtml.LoginTmpl, s.loginData(r, next, "err.internal"), http.StatusInternalServerError)
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
			renderHTML(w, legacyhtml.LoginTmpl, s.loginData(r, next, "err.internal"), http.StatusInternalServerError)
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
	renderHTML(w, legacyhtml.TotpTmpl, s.totpData(r, r.URL.Query().Get("next"), ""), http.StatusOK)
}

func (s *Server) handleTOTPSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		renderHTML(w, legacyhtml.TotpTmpl, s.totpData(r, "/listing", "err.bad_request"), http.StatusBadRequest)
		return
	}
	next := s.sanitizeNext(r.PostFormValue("next"))
	user, found := s.Users.Get(sess.Email)
	if !found || !auth.VerifyTOTP(user.TOTPSecret, r.PostFormValue("code"), time.Now()) {
		s.auditFail(r, "totp", "email="+sess.Email)
		renderHTML(w, legacyhtml.TotpTmpl, s.totpData(r, next, "err.bad_code"), http.StatusUnauthorized)
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
