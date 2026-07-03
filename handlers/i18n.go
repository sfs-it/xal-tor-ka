// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"net/http"
	"net/url"

	"xaltorka/i18n"
	"xaltorka/xtkui"
)

// lang resolves the request's UI language (delegates to the shared UI kit).
func (s *Server) lang(r *http.Request) string {
	return xtkui.LangFromRequest(r)
}

// handleSetLang sets the language cookie and returns to the page the user was on
// (explicit ?next, else the Referer path), same-site only.
func (s *Server) handleSetLang(w http.ResponseWriter, r *http.Request) {
	if code := r.PathValue("code"); i18n.IsSupported(code) {
		http.SetCookie(w, &http.Cookie{
			Name: xtkui.LangCookie, Value: code, Path: "/",
			MaxAge: 31536000, SameSite: http.SameSiteLaxMode,
		})
	}
	next := r.URL.Query().Get("next")
	if next == "" {
		if ref, err := url.Parse(r.Referer()); err == nil && ref.Path != "" {
			next = ref.Path
			if ref.RawQuery != "" {
				next += "?" + ref.RawQuery
			}
		}
	}
	http.Redirect(w, r, s.sanitizeNext(next), http.StatusSeeOther)
}
