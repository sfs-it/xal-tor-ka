// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"xaltorka/i18n"
)

// langCookie stores the user's chosen UI language.
const langCookie = "xtk_lang"

// Inline line-icons (currentColor → adapt to light/dark). Globe = language,
// user = profile, exit = logout.
const (
	globeSVG  = `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3c2.6 2.6 2.6 15.4 0 18M12 3c-2.6 2.6-2.6 15.4 0 18"/></svg>`
	userSVG   = `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="8" r="3.3"/><path d="M5.5 20c1-3.7 12-3.7 13 0"/></svg>`
	logoutSVG = `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 4h3a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-3"/><path d="M10 8l-4 4 4 4"/><path d="M6 12h9"/></svg>`
)

// langPopup renders the globe button + a click popup (native <details>) listing
// the languages. Each entry links to /lang/<code>; handleSetLang returns to the
// referring page, so no per-page path threading is needed.
func langPopup(lang string) string {
	var b strings.Builder
	b.WriteString(`<details class="langpop"><summary class="iconbtn" title="`)
	b.WriteString(template.HTMLEscapeString(i18n.T(lang, "lang.label")))
	b.WriteString(`">` + globeSVG + `<span class="lc">` + strings.ToUpper(template.HTMLEscapeString(lang)) + `</span></summary><div class="menu">`)
	for _, l := range i18n.Supported {
		cls := ""
		if l.Code == lang {
			cls = ` class="on"`
		}
		b.WriteString(`<a href="/lang/` + template.HTMLEscapeString(l.Code) + `"` + cls + `>` + template.HTMLEscapeString(l.Name) + `</a>`)
	}
	b.WriteString(`</div></details>`)
	return b.String()
}

// iconCluster is the trailing action cluster shown at the end of every bar:
// language popup, and (when logged in) profile + logout icons.
func iconCluster(lang string, loggedIn bool) template.HTML {
	var b strings.Builder
	b.WriteString(`<div class="cluster">`)
	b.WriteString(langPopup(lang))
	if loggedIn {
		p := template.HTMLEscapeString(i18n.T(lang, "nav.profile"))
		lo := template.HTMLEscapeString(i18n.T(lang, "btn.logout"))
		b.WriteString(`<a class="iconbtn" href="/profilo" title="` + p + `" aria-label="` + p + `">` + userSVG + `</a>`)
		b.WriteString(`<form class="inline" method="post" action="/logout"><button class="iconbtn" title="` + lo + `" aria-label="` + lo + `">` + logoutSVG + `</button></form>`)
	}
	b.WriteString(`</div>`)
	return template.HTML(b.String())
}

// langCorner is the language popup fixed in the page corner (for pages without a
// bar: login/2FA/setup/QR/blocked).
func langCorner(lang string) template.HTML {
	return template.HTML(`<div class="corner">` + langPopup(lang) + `</div>`)
}

// tmplFuncs are the localization helpers for data-driven templates (they pass a
// .Lang field): {{T .Lang "key"}}, {{cluster .Lang}}, {{corner .Lang}}, {{if rtl .Lang}}.
var tmplFuncs = template.FuncMap{
	"T":       i18n.T,
	"langs":   func() []i18n.Lang { return i18n.Supported },
	"rtl":     i18n.IsRTL,
	"cluster": func(lang string) template.HTML { return iconCluster(lang, true) },
	"corner":  func(lang string) template.HTML { return langCorner(lang) },
}

// locFuncs returns helpers bound to a language, so admin/setup templates can call
// {{T "key"}} / {{cluster}} / {{corner}} / {{curlang}} without a .Lang field.
func locFuncs(lang string) template.FuncMap {
	return template.FuncMap{
		"T":       func(k string) string { return i18n.T(lang, k) },
		"rtl":     func() bool { return i18n.IsRTL(lang) },
		"curlang": func() string { return lang },
		"langs":   func() []i18n.Lang { return i18n.Supported },
		"cluster": func() template.HTML { return iconCluster(lang, true) },
		"corner":  func() template.HTML { return langCorner(lang) },
	}
}

// locParse builds a template pre-bound with default-language loc helpers; the real
// language is bound per request by cloning.
func locParse(name, body string) *template.Template {
	return template.Must(template.New(name).Funcs(locFuncs(i18n.Default)).Parse(body))
}

// renderLoc clones a locParse template, binds the request language and executes it.
func (s *Server) renderLoc(w http.ResponseWriter, r *http.Request, t *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ct, err := t.Clone()
	if err != nil {
		_ = t.Execute(w, data)
		return
	}
	ct.Funcs(locFuncs(s.lang(r)))
	_ = ct.Execute(w, data)
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

// handleSetLang sets the language cookie and returns to the page the user was on
// (explicit ?next, else the Referer path), same-site only.
func (s *Server) handleSetLang(w http.ResponseWriter, r *http.Request) {
	if code := r.PathValue("code"); i18n.IsSupported(code) {
		http.SetCookie(w, &http.Cookie{
			Name: langCookie, Value: code, Path: "/",
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
