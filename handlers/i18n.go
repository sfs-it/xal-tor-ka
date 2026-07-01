// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"html/template"
	"net/http"

	"xaltorka/i18n"
)

// langCookie stores the user's chosen UI language.
const langCookie = "xtk_lang"

// langSelHTML is a compact language switcher embedded into templates; it needs
// the data to expose .Lang and the `langs` func. Selecting an option navigates
// to /lang/<code> preserving the current path.
const langSelHTML = `<select class="langsel" aria-label="Language" onchange="location.href='/lang/'+this.value+'?next='+encodeURIComponent(location.pathname+location.search)">{{range langs}}<option value="{{.Code}}"{{if eq .Code $.Lang}} selected{{end}}>{{.Name}}</option>{{end}}</select>`

// tmplFuncs are the shared template helpers for localization:
//
//	{{T .Lang "key"}}   → translated string
//	{{range langs}}      → the offered languages (for the switcher)
//	{{if rtl .Lang}}     → true for right-to-left languages (Arabic)
var tmplFuncs = template.FuncMap{
	"T":     i18n.T,
	"langs": func() []i18n.Lang { return i18n.Supported },
	"rtl":   i18n.IsRTL,
}

// lang resolves the request's UI language: the xtk_lang cookie wins, else the
// Accept-Language header, else English.
func (s *Server) lang(r *http.Request) string {
	cookie := ""
	if c, err := r.Cookie(langCookie); err == nil {
		cookie = c.Value
	}
	return i18n.Match(cookie, r.Header.Get("Accept-Language"))
}

// handleSetLang sets the language cookie and redirects back (same-site).
func (s *Server) handleSetLang(w http.ResponseWriter, r *http.Request) {
	if code := r.PathValue("code"); i18n.IsSupported(code) {
		http.SetCookie(w, &http.Cookie{
			Name: langCookie, Value: code, Path: "/",
			MaxAge: 31536000, SameSite: http.SameSiteLaxMode,
		})
	}
	http.Redirect(w, r, s.sanitizeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
}
