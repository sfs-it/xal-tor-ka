// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"

	qrcode "github.com/skip2/go-qrcode"

	"xaltorka/auth"
	"xaltorka/models"
)

var profileTmpl = template.Must(template.New("profile").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · Profilo</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<header class="topbar">
 <div class="brand">⛬ Xal-Tor-Ka<span class="sub">Profilo</span></div>
 <nav class="topnav"><a href="/listing">← Servizi</a>{{if .IsAdmin}}<a href="/admin">Amministrazione</a>{{end}}
  <form class="inline" method="post" action="/logout"><button class="btn sm">Esci</button></form></nav>
</header>
<main class="container">
 <h1>Il mio profilo</h1>
 {{if .Notice}}<div class="ok">{{.Notice}}</div>{{end}}
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <div class="card">
  <div class="meta">Email: <b>{{.Email}}</b></div>
  <div class="meta">Accesso: <b>{{.Provider}}</b>{{if .IsAdmin}} · <span class="tag">amministratore</span>{{end}}</div>
 </div>

 <div class="card" style="margin-top:1rem"><h3>Servizi a cui accedo</h3>
  {{if .IsAdmin}}<p class="hint">Come amministratore accedi a tutti i servizi.</p>{{end}}
  <ul class="hostlist">
  {{range .Services}}<li><a href="{{.URL}}"{{if .External}} target="_blank" rel="noopener"{{end}}>{{.Name}}</a></li>
  {{else}}<li class="hint">Nessun servizio disponibile per il tuo profilo.</li>{{end}}
  </ul>
 </div>

 {{if .Local}}<div class="card" style="margin-top:1rem"><h3>Cambia password</h3>
  <form method="post" action="/profilo/password"><div class="formgrid">
   <div class="field"><label>Password attuale</label><input type="password" name="current" autocomplete="current-password" required></div>
   <div class="field"><label>Nuova password</label><input type="password" name="password" autocomplete="new-password" required></div>
   <div><button class="btn primary">aggiorna</button></div>
  </div></form>
 </div>{{end}}

 {{if .TOTP}}<div class="card" style="margin-top:1rem"><h3>Autenticazione a due fattori (2FA)</h3>
  <p class="hint">Rigenera il segreto TOTP se hai cambiato dispositivo o perso l'app. Dovrai scansionare il nuovo QR: il prossimo accesso userà il nuovo codice.</p>
  <form method="post" action="/profilo/totp" onsubmit="return confirm('Rigenerare il 2FA? Il vecchio codice smetterà di funzionare.')">
   <button class="btn">rigenera 2FA</button></form>
 </div>{{end}}
</main></body></html>`))

var profileQRTmpl = template.Must(template.New("profileqr").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · 2FA</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card qr">
 <h1>Nuovo 2FA</h1>
 <p class="hint">Scansiona il QR con l'app authenticator (o inserisci la chiave a mano). Il prossimo accesso userà questo codice.</p>
 <p><img src="{{.QR}}" alt="QR otpauth" width="240" height="240"></p>
 <p>Chiave: <code>{{.Secret}}</code></p>
 <p style="margin-top:1.2rem"><a href="/profilo">← torna al profilo</a></p>
</div></div></body></html>`))

type profileData struct {
	Email, Provider      string
	IsAdmin, Local, TOTP bool
	Services             []tile
	Notice, Error        string
}

// currentUser returns the fully-authenticated user of the request (session +
// 2FA done), if any.
func (s *Server) currentUser(r *http.Request) (models.User, bool) {
	sess, ok := s.session(r)
	if !ok || !sess.TwoFADone {
		return models.User{}, false
	}
	return s.Users.Get(sess.Email)
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login?next=/profilo", http.StatusSeeOther)
		return
	}
	notice := map[string]string{"pw": "Password aggiornata."}[r.URL.Query().Get("ok")]
	errMsg := map[string]string{"pw": "Password attuale errata.", "local": "Operazione disponibile solo per gli account locali."}[r.URL.Query().Get("err")]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = profileTmpl.Execute(w, profileData{
		Email:    u.Email,
		Provider: u.Provider,
		IsAdmin:  u.Admin,
		Local:    u.Provider == "local",
		TOTP:     !s.Cfg.DisableTOTP,
		Services: s.tilesFor(u),
		Notice:   notice,
		Error:    errMsg,
	})
}

func (s *Server) handleProfilePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login?next=/profilo", http.StatusSeeOther)
		return
	}
	if u.Provider != "local" {
		http.Redirect(w, r, "/profilo?err=local", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/profilo?err=pw", http.StatusSeeOther)
		return
	}
	// Require the current password to change it (self-service safety).
	if auth.VerifyPassword(u.PasswordHash, r.PostFormValue("current")) != nil {
		s.auditFail(r, "profile_pw", "email="+u.Email)
		http.Redirect(w, r, "/profilo?err=pw", http.StatusSeeOther)
		return
	}
	newpw := r.PostFormValue("password")
	if newpw == "" {
		http.Redirect(w, r, "/profilo?err=pw", http.StatusSeeOther)
		return
	}
	hash, err := auth.HashPassword(newpw)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	err = s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			if (*users)[i].Email == u.Email {
				(*users)[i].PasswordHash = hash
				return nil
			}
		}
		return fmt.Errorf("user not found")
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/profilo?ok=pw", http.StatusSeeOther)
}

func (s *Server) handleProfileTOTP(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login?next=/profilo", http.StatusSeeOther)
		return
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	err = s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			if (*users)[i].Email == u.Email {
				(*users)[i].TOTPSecret = secret
				return nil
			}
		}
		return fmt.Errorf("user not found")
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	png, err := qrcode.Encode(otpauthURI(u.Email, secret), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "QR error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = profileQRTmpl.Execute(w, struct {
		Secret string
		QR     template.URL
	}{Secret: secret, QR: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))})
}
