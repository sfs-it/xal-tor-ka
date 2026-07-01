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
	"xaltorka/i18n"
	"xaltorka/models"
)

var profileTmpl = template.Must(template.New("profile").Funcs(tmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "profile.subtitle"}}</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<header class="topbar">
 <div class="brand">⛬ Xal-Tor-Ka<span class="sub">{{T .Lang "profile.subtitle"}}</span></div>
 <nav class="topnav"><a href="/listing">{{T .Lang "btn.back_services"}}</a>{{if .IsAdmin}}<a href="/admin">{{T .Lang "nav.admin"}}</a>{{end}}
  {{cluster .Lang}}</nav>
</header>
<main class="container">
 <h1>{{T .Lang "profile.title"}}</h1>
 {{if .Notice}}<div class="ok">{{.Notice}}</div>{{end}}
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <div class="card">
  <div class="meta">{{T .Lang "profile.email"}}: <b>{{.Email}}</b></div>
  <div class="meta">{{T .Lang "profile.access"}}: <b>{{.Provider}}</b>{{if .IsAdmin}} · <span class="tag">{{T .Lang "profile.admin_badge"}}</span>{{end}}</div>
 </div>

 <div class="card" style="margin-top:1rem"><h3>{{T .Lang "profile.services"}}</h3>
  {{if .IsAdmin}}<p class="hint">{{T .Lang "profile.admin_all"}}</p>{{end}}
  <ul class="hostlist">
  {{range .Services}}<li><a href="{{.URL}}"{{if .External}} target="_blank" rel="noopener"{{end}}>{{.Name}}</a></li>
  {{else}}<li class="hint">{{T $.Lang "listing.empty"}}</li>{{end}}
  </ul>
 </div>

 {{if .Local}}<div class="card" style="margin-top:1rem"><h3>{{T .Lang "profile.change_pw"}}</h3>
  <form method="post" action="/profilo/password"><div class="formgrid">
   <div class="field"><label>{{T .Lang "profile.current_pw"}}</label><input type="password" name="current" autocomplete="current-password" required></div>
   <div class="field"><label>{{T .Lang "profile.new_pw"}}</label><input type="password" name="password" autocomplete="new-password" required></div>
   <div><button class="btn primary">{{T .Lang "btn.update"}}</button></div>
  </div></form>
 </div>{{end}}

 {{if .TOTP}}<div class="card" style="margin-top:1rem"><h3>{{T .Lang "profile.2fa"}}</h3>
  <p class="hint">{{T .Lang "profile.2fa_hint"}}</p>
  <form method="post" action="/profilo/totp" onsubmit="return confirm('{{T .Lang "profile.2fa_confirm"}}')">
   <button class="btn">{{T .Lang "profile.2fa_regen"}}</button></form>
 </div>{{end}}
</main></body></html>`))

var profileQRTmpl = template.Must(template.New("profileqr").Funcs(tmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "qr.new2fa"}}</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card qr">
 <h1>{{T .Lang "qr.new2fa"}}</h1>
 <p class="hint">{{T .Lang "qr.scan"}}</p>
 <p><img src="{{.QR}}" alt="QR otpauth" width="240" height="240"></p>
 <p>{{T .Lang "qr.key"}}: <code>{{.Secret}}</code></p>
 <p style="margin-top:1.2rem"><a href="/profilo">{{T .Lang "qr.back_profile"}}</a></p>
</div></div>{{corner .Lang}}</body></html>`))

type profileData struct {
	Email, Provider      string
	IsAdmin, Local, TOTP bool
	Lang                 string
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
	lang := s.lang(r)
	notice := map[string]string{"pw": i18n.T(lang, "profile.pw_updated")}[r.URL.Query().Get("ok")]
	errMsg := map[string]string{"pw": i18n.T(lang, "profile.pw_wrong"), "local": i18n.T(lang, "profile.local_only")}[r.URL.Query().Get("err")]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = profileTmpl.Execute(w, profileData{
		Email:    u.Email,
		Provider: u.Provider,
		IsAdmin:  u.Admin,
		Local:    u.Provider == "local",
		TOTP:     !s.Cfg.DisableTOTP,
		Lang:     lang,
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
		Lang   string
		QR     template.URL
	}{Secret: secret, Lang: s.lang(r), QR: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))})
}
