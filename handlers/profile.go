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
	"xaltorka/legacyhtml"
	"xaltorka/models"
)

type profileData struct {
	Email, Provider      string
	IsAdmin, Local, TOTP bool
	Lang                 string
	Services             []tile
	Notice, Error        string
	ClientIP             string // IP as seen by the gatekeeper (same as the admin whitelist uses)
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
	ip := ""
	if c := clientIP(r, s.Cfg.Server.TrustedProxies); c != nil {
		ip = c.String()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = legacyhtml.ProfileTmpl.Execute(w, profileData{
		Email:    u.Email,
		Provider: u.Provider,
		IsAdmin:  u.Admin,
		Local:    u.Provider == "local",
		TOTP:     !s.Cfg.DisableTOTP,
		Lang:     lang,
		Services: s.tilesFor(u),
		Notice:   notice,
		Error:    errMsg,
		ClientIP: ip,
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
		http.Error(w, i18n.T(s.lang(r), "err.internal"), http.StatusInternalServerError)
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
		http.Error(w, i18n.T(s.lang(r), "err.internal"), http.StatusInternalServerError)
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
		http.Error(w, i18n.T(s.lang(r), "err.internal"), http.StatusInternalServerError)
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
		http.Error(w, i18n.T(s.lang(r), "err.internal"), http.StatusInternalServerError)
		return
	}
	png, err := qrcode.Encode(otpauthURI(u.Email, secret), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, i18n.T(s.lang(r), "err.qr"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = legacyhtml.ProfileQRTmpl.Execute(w, struct {
		Secret string
		Lang   string
		QR     template.URL
	}{Secret: secret, Lang: s.lang(r), QR: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))})
}
