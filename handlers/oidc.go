// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"xaltorka/version"
)

// oidcStateCookie carries the anti-CSRF state, nonce, next-URL and provider id
// across the redirect to the IdP and back. HttpOnly + short-lived + Path=/auth/.
const oidcStateCookie = "xtk_oidc"

type oidcState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Next     string `json:"x"`
	Provider string `json:"p"`
}

// oidcButton is a login-page entry for an enabled OIDC provider.
type oidcButton struct {
	ID   string
	Name string
}

// oidcButtons lists the enabled OIDC providers (in config order) for the login page.
func (s *Server) oidcButtons() []oidcButton {
	var bs []oidcButton
	for _, p := range s.Cfg.Providers {
		if pr, ok := s.OIDC[p.ID]; ok {
			bs = append(bs, oidcButton{ID: pr.ID(), Name: pr.Name()})
		}
	}
	return bs
}

// loginData builds the login template payload with a sanitized next and the
// OIDC buttons.
func (s *Server) loginData(next, errMsg string) formData {
	return formData{Next: s.sanitizeNext(next), Error: errMsg, OIDC: s.oidcButtons(), Version: version.Version}
}

func (s *Server) cookieSecure() bool {
	return strings.HasPrefix(s.Cfg.Server.ExternalURL, "https")
}

func randB64() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// handleOIDCStart begins the OIDC authorization-code flow: it mints state+nonce,
// stores them in a short-lived cookie and redirects the browser to the IdP.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("provider")
	p, ok := s.OIDC[id]
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	next := s.sanitizeNext(r.URL.Query().Get("next"))
	st := oidcState{State: randB64(), Nonce: randB64(), Next: next, Provider: id}
	authURL, err := p.AuthURL(r.Context(), st.State, st.Nonce)
	if err != nil {
		// Discovery non riuscita (issuer irraggiungibile/errato): fail-closed.
		s.auditFail(r, "oidc", "provider="+id+" discovery")
		renderHTML(w, loginTmpl, s.loginData(next, "provider non disponibile, riprova più tardi"), http.StatusBadGateway)
		return
	}
	raw, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     "/auth/",
		HttpOnly: true,
		MaxAge:   600,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(),
	})
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// handleOIDCCallback is the IdP redirect target: it verifies state, exchanges the
// code, maps the verified email to a provisioned user and opens a session.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("provider")
	p, ok := s.OIDC[id]
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	st, ok := s.readOIDCState(r)
	s.clearOIDCState(w)
	if !ok || st.Provider != id {
		s.auditFail(r, "oidc", "provider="+id+" state")
		renderHTML(w, loginTmpl, s.loginData("/listing", "sessione di login scaduta, riprova"), http.StatusBadRequest)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		s.auditFail(r, "oidc", "provider="+id+" idp_error="+e)
		renderHTML(w, loginTmpl, s.loginData(st.Next, "accesso negato dal provider"), http.StatusUnauthorized)
		return
	}
	if r.URL.Query().Get("state") != st.State {
		s.auditFail(r, "oidc", "provider="+id+" csrf")
		renderHTML(w, loginTmpl, s.loginData(st.Next, "verifica anti-CSRF fallita"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	idn, err := p.Exchange(ctx, r.URL.Query().Get("code"), st.Nonce)
	if err != nil {
		s.auditFail(r, "oidc", "provider="+id+" exchange")
		renderHTML(w, loginTmpl, s.loginData(st.Next, "autenticazione fallita"), http.StatusUnauthorized)
		return
	}

	// Nessun auto-provisioning: l'utente deve già esistere ed essere dichiarato
	// per QUESTO provider (admin lo crea con provider=<id> e l'email dell'IdP).
	u, found := s.Users.Get(idn.Email)
	if !found || u.Provider != id {
		s.auditFail(r, "oidc", "provider="+id+" email="+idn.Email+" not_provisioned")
		renderHTML(w, loginTmpl, s.loginData(st.Next, "utente non abilitato: "+idn.Email), http.StatusForbidden)
		return
	}

	sess, err := s.Sessions.Create(idn.Email, id)
	if err != nil {
		renderHTML(w, loginTmpl, s.loginData(st.Next, "errore interno"), http.StatusInternalServerError)
		return
	}
	s.setSession(w, sess.ID)
	// L'IdP ha già autenticato l'utente (incl. eventuale MFA): sessione completa.
	s.Sessions.Complete2FA(sess.ID)
	http.Redirect(w, r, st.Next, http.StatusSeeOther)
}

func (s *Server) readOIDCState(r *http.Request) (oidcState, bool) {
	c, err := r.Cookie(oidcStateCookie)
	if err != nil {
		return oidcState{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return oidcState{}, false
	}
	var st oidcState
	if err := json.Unmarshal(raw, &st); err != nil {
		return oidcState{}, false
	}
	if st.State == "" || st.Nonce == "" {
		return oidcState{}, false
	}
	return st, true
}

func (s *Server) clearOIDCState(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: "", Path: "/auth/",
		HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteLaxMode,
	})
}
