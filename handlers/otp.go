// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"xaltorka/xtkui"
)

// One-time-code login (passwordless, opt-in via config.one_time_code). The code is
// a FIRST factor: after it, a user with TOTP still completes 2FA (mirrors the password
// path). Delivery is out-of-band by the configured channel — "spool" writes the code to
// a queue file with the requester IP (for manual/audited retrieval when there is no SMTP),
// "email" uses the notify transport, "sms" is reserved for a later API integration.
// Security posture: generic responses (never reveal whether an email exists), codes only
// for real local users, per-email cooldown, hashed + single-use + time-limited codes.

var codeRequestTmpl = template.Must(template.New("codereq").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · Accesso con codice</title><link rel="stylesheet" href="/_xtk/assets/admin.css"></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>⛬ Accesso con codice</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <p class="hint">Inserisci la tua email: ti invieremo un codice monouso per accedere.</p>
 <form method="post" action="/login/code">
  <input type="hidden" name="next" value="{{.Next}}">
  <div class="field"><label>Email</label><input type="email" name="email" autocomplete="username" required></div>
  <button class="btn primary">Invia il codice</button>
 </form>
 <p class="hint" style="margin-top:1rem"><a href="/login">← Torna al login con password</a></p>
 {{corner .Lang}}
</div></div></body></html>`))

var codeVerifyTmpl = template.Must(template.New("codever").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · Accesso con codice</title><link rel="stylesheet" href="/_xtk/assets/admin.css"></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>⛬ Inserisci il codice</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{else}}<div class="ok">Se l'indirizzo corrisponde a un account, un codice è stato inviato.</div>{{end}}
 <form method="post" action="/login/code/verify">
  <input type="hidden" name="next" value="{{.Next}}">
  <input type="hidden" name="email" value="{{.Email}}">
  <div class="field"><label>Codice</label><input name="code" inputmode="numeric" autocomplete="one-time-code" required autofocus></div>
  <button class="btn primary">Accedi</button>
 </form>
 <p class="hint" style="margin-top:1rem"><a href="/login/code">Richiedi un nuovo codice</a></p>
 {{corner .Lang}}
</div></div></body></html>`))

type codeData struct {
	Next  string
	Email string
	Error string
	Lang  string
}

func (s *Server) otpEnabled() bool { return s.OTP != nil && s.Cfg.OneTimeCode.Enabled }

func (s *Server) handleCodeRequestForm(w http.ResponseWriter, r *http.Request) {
	if !s.otpEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	renderHTML(w, codeRequestTmpl, codeData{Next: s.sanitizeNext(r.URL.Query().Get("next")), Lang: s.lang(r)}, http.StatusOK)
}

// handleCodeRequestSubmit issues + delivers a code, then always shows the verify
// form with a generic message — regardless of whether the email is a real user.
func (s *Server) handleCodeRequestSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.otpEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	next := s.sanitizeNext(r.PostFormValue("next"))
	email := r.PostFormValue("email")
	lang := s.lang(r)

	// Only issue a usable code for a real local user; always answer the same.
	if _, found := s.Users.Get(email); found {
		if code, ok := s.OTP.Issue(email); ok {
			s.deliverCode(r, email, code)
		}
	} else {
		slog.Info("otp: request for unknown email", "ip", clientIP(r, s.Cfg.Server.TrustedProxies))
	}
	renderHTML(w, codeVerifyTmpl, codeData{Next: next, Email: email, Lang: lang}, http.StatusOK)
}

// handleCodeVerifySubmit consumes the code and, on success for a real user,
// starts the session (first factor) — TOTP still follows unless disabled.
func (s *Server) handleCodeVerifySubmit(w http.ResponseWriter, r *http.Request) {
	if !s.otpEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	next := s.sanitizeNext(r.PostFormValue("next"))
	email := r.PostFormValue("email")
	code := r.PostFormValue("code")

	_, found := s.Users.Get(email)
	if !found || !s.OTP.Verify(email, code) {
		s.auditFail(r, "otp", "email="+email)
		renderHTML(w, codeVerifyTmpl, codeData{Next: next, Email: email, Error: "Codice non valido o scaduto.", Lang: s.lang(r)}, http.StatusUnauthorized)
		return
	}
	sess, err := s.Sessions.Create(email, "otp")
	if err != nil {
		renderHTML(w, codeVerifyTmpl, codeData{Next: next, Email: email, Error: "Errore interno.", Lang: s.lang(r)}, http.StatusInternalServerError)
		return
	}
	s.setSession(w, sess.ID)
	if s.Cfg.DisableTOTP {
		s.Sessions.Complete2FA(sess.ID)
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login/totp?next="+url.QueryEscape(next), http.StatusSeeOther)
}

// deliverCode routes the plaintext code to the configured channel. spool (default):
// append a JSON line {ts,email,ip,code,expires} to the queue file — the "share queue"
// the operator reads when there is no SMTP. email: notify transport. sms: reserved.
func (s *Server) deliverCode(r *http.Request, email, code string) {
	ip := ""
	if c := clientIP(r, s.Cfg.Server.TrustedProxies); c != nil {
		ip = c.String()
	}
	ttl := s.Cfg.OneTimeCode.TTLMinutes
	if ttl <= 0 {
		ttl = 10
	}
	switch s.Cfg.OneTimeCode.Channel {
	case "email":
		// Phase 2: send via the notify transport once an SMTP account is provided
		// (a customer of the service supplies it). Until then, spool so the code is
		// never silently lost.
		slog.Warn("otp: email channel not yet configured (no SMTP), spooling instead", "email", email)
		s.spoolCode(email, code, ip, ttl)
	case "sms":
		// Phase 2: delivery via an SMS API (Twilio/gateway). Not yet wired — spool.
		slog.Warn("otp: sms channel not yet implemented, spooling instead", "email", email)
		s.spoolCode(email, code, ip, ttl)
	default: // "spool" (and "" when enabled)
		s.spoolCode(email, code, ip, ttl)
	}
}

func (s *Server) spoolCode(email, code, ip string, ttlMin int) {
	if s.OTPQueuePath == "" {
		slog.Error("otp: spool channel but no queue path configured")
		return
	}
	rec := map[string]string{
		"ts":      time.Now().Format(time.RFC3339),
		"email":   email,
		"ip":      ip,
		"code":    code,
		"expires": time.Now().Add(time.Duration(ttlMin) * time.Minute).Format(time.RFC3339),
	}
	line, _ := json.Marshal(rec)
	f, err := os.OpenFile(s.OTPQueuePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		slog.Error("otp: cannot open spool queue", "err", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		slog.Error("otp: cannot write spool queue", "err", err)
	}
}
