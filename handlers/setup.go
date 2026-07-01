// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"xaltorka/auth"
	"xaltorka/config"
	"xaltorka/i18n"
	"xaltorka/models"
)

// Hybrid onboarding (BLUEPRINT §13): the `setup` CLI subcommand writes
// data/setup.json with a one-time token + email; this wizard completes it in the
// browser (password + TOTP enrollment with QR), then writes users.json and
// reloads the directory.

const setupHead = `<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · Setup</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card`

var setupCredTmpl = template.Must(template.New("cred").Funcs(tmplFuncs).Parse(setupHead + `">
 <h1>⛬ {{T .Lang "setup.cred.title"}}</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <p class="hint">{{T .Lang "setup.profile"}} <strong>{{.Email}}</strong></p>
 <form method="post" action="/setup">
  <input type="hidden" name="step" value="cred">
  <input type="hidden" name="token" value="{{.Token}}">
  <div class="field"><label>{{T .Lang "field.password"}}</label><input type="password" name="password" autocomplete="new-password" required></div>
  <div class="field"><label>{{T .Lang "setup.confirm_pw"}}</label><input type="password" name="password2" autocomplete="new-password" required></div>
  <button class="btn primary">{{T .Lang "btn.continue"}}</button>
 </form>{{corner .Lang}}
</div></div></body></html>`))

var setupTOTPTmpl = template.Must(template.New("totp").Funcs(tmplFuncs).Parse(setupHead + ` qr">
 <h1>{{T .Lang "setup.totp.title"}}</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <p class="hint">{{T .Lang "qr.scan"}}</p>
 <p><img src="{{.QR}}" alt="QR otpauth" width="240" height="240"></p>
 <p>{{T .Lang "qr.key"}}: <code>{{.Secret}}</code></p>
 <form method="post" action="/setup">
  <input type="hidden" name="step" value="confirm">
  <input type="hidden" name="token" value="{{.Token}}">
  <div class="field"><label>{{T .Lang "totp.code"}}</label><input name="code" inputmode="numeric" autocomplete="one-time-code" required></div>
  <button class="btn primary">{{T .Lang "setup.totp.activate"}}</button>
 </form>{{corner .Lang}}
</div></div></body></html>`))

var setupDoneTmpl = template.Must(template.New("done").Funcs(tmplFuncs).Parse(setupHead + `">
 <h1>✓ {{T .Lang "setup.done.title"}}</h1>
 <p class="hint">{{T .Lang "setup.done.profile"}} <strong>{{.Email}}</strong> {{T .Lang "setup.done.activated"}}</p>
 <p style="margin-top:1rem"><a class="btn primary" href="/login">{{T .Lang "setup.done.go_login"}}</a></p>{{corner .Lang}}
</div></div></body></html>`))

type setupCredData struct {
	Email string
	Token string
	Error string
	Lang  string
}

type setupTOTPData struct {
	Token  string
	Secret string
	QR     template.URL
	Error  string
	Lang   string
}

func (s *Server) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	st, ok := s.validSetup(r.URL.Query().Get("token"))
	if !ok {
		s.setupError(w)
		return
	}
	renderHTML(w, setupCredTmpl, setupCredData{Email: st.Email, Token: st.Token, Lang: s.lang(r)}, http.StatusOK)
}

func (s *Server) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.setupError(w)
		return
	}
	st, ok := s.validSetup(r.PostFormValue("token"))
	if !ok {
		s.setupError(w)
		return
	}

	switch r.PostFormValue("step") {
	case "cred":
		s.setupStepCred(w, r, st)
	case "confirm":
		s.setupStepConfirm(w, r, st)
	default:
		s.setupError(w)
	}
}

// setupStepCred validates the password, generates a TOTP secret and persists
// both into the setup state, then shows the QR enrollment step.
func (s *Server) setupStepCred(w http.ResponseWriter, r *http.Request, st models.SetupState) {
	lang := s.lang(r)
	pw := r.PostFormValue("password")
	if pw == "" || pw != r.PostFormValue("password2") {
		renderHTML(w, setupCredTmpl, setupCredData{Email: st.Email, Token: st.Token, Error: i18n.T(lang, "setup.err.pw_mismatch"), Lang: lang}, http.StatusBadRequest)
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		s.setupError(w)
		return
	}
	st.PasswordHash = hash
	if s.Cfg.DisableTOTP {
		// 2FA disabled: no enrollment, finalize right away.
		st.TOTPSecret = ""
		s.finalizeSetup(w, lang, st)
		return
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		s.setupError(w)
		return
	}
	st.TOTPSecret = secret
	if err := config.SaveSetup(s.SetupPath, st); err != nil {
		s.setupError(w)
		return
	}
	s.renderTOTPStep(w, lang, st, "")
}

// setupStepConfirm verifies the TOTP code, then finalizes the user.
func (s *Server) setupStepConfirm(w http.ResponseWriter, r *http.Request, st models.SetupState) {
	if st.TOTPSecret == "" || st.PasswordHash == "" {
		s.setupError(w)
		return
	}
	lang := s.lang(r)
	if !auth.VerifyTOTP(st.TOTPSecret, r.PostFormValue("code"), time.Now()) {
		s.renderTOTPStep(w, lang, st, i18n.T(lang, "err.bad_code"))
		return
	}
	s.finalizeSetup(w, lang, st)
}

// finalizeSetup writes/updates the admin user from the setup state, reloads the
// directory, removes the setup token and shows the done page.
func (s *Server) finalizeSetup(w http.ResponseWriter, lang string, st models.SetupState) {
	newUser := models.User{
		Email:        st.Email,
		Provider:     "local",
		PasswordHash: st.PasswordHash,
		TOTPSecret:   st.TOTPSecret,
		Admin:        true, // the profile created by setup is the administrator
		Backends:     []string{},
	}
	users := s.Users.All()
	replaced := false
	for i := range users {
		if users[i].Email == newUser.Email {
			users[i] = newUser
			replaced = true
			break
		}
	}
	if !replaced {
		users = append(users, newUser)
	}
	if err := config.SaveUsers(s.UsersPath, s.BackupsDir, models.Users{Users: users}); err != nil {
		s.setupError(w)
		return
	}
	s.Users.Replace(users)
	_ = os.Remove(s.SetupPath)
	renderHTML(w, setupDoneTmpl, struct{ Email, Lang string }{Email: st.Email, Lang: lang}, http.StatusOK)
}

func (s *Server) renderTOTPStep(w http.ResponseWriter, lang string, st models.SetupState, errMsg string) {
	uri := otpauthURI(st.Email, st.TOTPSecret)
	png, err := qrcode.Encode(uri, qrcode.Medium, 256)
	if err != nil {
		s.setupError(w)
		return
	}
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	renderHTML(w, setupTOTPTmpl, setupTOTPData{
		Token:  st.Token,
		Secret: st.TOTPSecret,
		QR:     template.URL(dataURI),
		Error:  errMsg,
		Lang:   lang,
	}, http.StatusOK)
}

// validSetup loads the setup state and checks the token (constant-time) and
// expiry. Returns ok=false when missing/invalid/expired (fail-closed).
func (s *Server) validSetup(token string) (models.SetupState, bool) {
	st, err := config.LoadSetup(s.SetupPath)
	if err != nil || token == "" || st.Token == "" {
		return models.SetupState{}, false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(st.Token)) != 1 {
		return models.SetupState{}, false
	}
	if time.Now().After(st.ExpiresAt) {
		return models.SetupState{}, false
	}
	return st, true
}

func (s *Server) setupError(w http.ResponseWriter) {
	http.Error(w, "setup unavailable: token missing, invalid or expired", http.StatusForbidden)
}

func otpauthURI(email, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", "Xal-Tor-Ka")
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape("Xal-Tor-Ka:"+email) + "?" + v.Encode()
}

// renderHTML writes an HTML template response with the given status.
func renderHTML(w http.ResponseWriter, t *template.Template, data any, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = t.Execute(w, data)
}
