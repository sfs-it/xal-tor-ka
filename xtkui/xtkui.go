// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package xtkui is the shared Xal-Tor-Ka UI kit: the embedded design system
// (CSS/JS), the localization template helpers, and the page chrome (head +
// top bar + container). Both the core gateway and installable extensions import
// it, so every panel gets the same look, dark mode, 10 languages and navigation
// without duplicating markup. See DRAFT-hosting-extension.md.
package xtkui

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"

	"xaltorka/i18n"
)

// AssetsFS embeds the static design system (CSS/JS) into the binary, so the UI
// stays self-contained in the minimal container (no external mounts, no CDN).
//
//go:embed assets/admin.css assets/admin.js
var AssetsFS embed.FS

// LangCookie stores the user's chosen UI language.
const LangCookie = "xtk_lang"

// LangFromRequest resolves the UI language: the xtk_lang cookie wins, else the
// Accept-Language header, else English.
func LangFromRequest(r *http.Request) string {
	cookie := ""
	if c, err := r.Cookie(LangCookie); err == nil {
		cookie = c.Value
	}
	return i18n.Match(cookie, r.Header.Get("Accept-Language"))
}

// Inline line-icons (currentColor → adapt to light/dark).
const (
	globeSVG  = `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3c2.6 2.6 2.6 15.4 0 18M12 3c-2.6 2.6-2.6 15.4 0 18"/></svg>`
	userSVG   = `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="8" r="3.3"/><path d="M5.5 20c1-3.7 12-3.7 13 0"/></svg>`
	logoutSVG = `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 4h3a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-3"/><path d="M10 8l-4 4 4 4"/><path d="M6 12h9"/></svg>`
)

// langPopup renders the globe button + a click popup (native <details>) listing
// the languages. Each entry links to /lang/<code>; the handler returns to the
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

// IconCluster is the trailing action cluster shown at the end of every bar:
// language popup, and (when logged in) profile + logout icons. The /profilo and
// /logout endpoints are the gateway's, so the cluster works for any panel served
// behind Xal-Tor-Ka (core or extension).
func IconCluster(lang string, loggedIn bool) template.HTML {
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

// LangCorner is the language popup fixed in the page corner (pages without a bar:
// login/2FA/setup/QR/blocked).
func LangCorner(lang string) template.HTML {
	return template.HTML(`<div class="corner">` + langPopup(lang) + `</div>`)
}

// TmplFuncs are the localization helpers for data-driven templates (they pass a
// .Lang field): {{T .Lang "key"}}, {{cluster .Lang}}, {{corner .Lang}}, {{if rtl .Lang}}.
var TmplFuncs = template.FuncMap{
	"T":       i18n.T,
	"langs":   func() []i18n.Lang { return i18n.Supported },
	"rtl":     i18n.IsRTL,
	"cluster": func(lang string) template.HTML { return IconCluster(lang, true) },
	"corner":  func(lang string) template.HTML { return LangCorner(lang) },
}

// LocFuncs returns helpers bound to a language, so templates can call
// {{T "key"}} / {{cluster}} / {{corner}} / {{curlang}} without a .Lang field.
func LocFuncs(lang string) template.FuncMap {
	return template.FuncMap{
		"T":       func(k string) string { return i18n.T(lang, k) },
		"rtl":     func() bool { return i18n.IsRTL(lang) },
		"curlang": func() string { return lang },
		"langs":   func() []i18n.Lang { return i18n.Supported },
		"cluster": func() template.HTML { return IconCluster(lang, true) },
		"corner":  func() template.HTML { return LangCorner(lang) },
	}
}

// LocParse builds a template pre-bound with default-language loc helpers; the real
// language is bound per request by cloning (see Chrome.Render).
func LocParse(name, body string) *template.Template {
	return template.Must(template.New(name).Funcs(LocFuncs(i18n.Default)).Parse(body))
}

// NavItem is one entry in the top navigation bar. LabelKey is an i18n key.
type NavItem struct {
	Key, Href, LabelKey string
}

// AdminNav is the core admin top navigation, shared by the core and by extensions
// (e.g. hosting) so the main menu is identical everywhere. withHosting appends the
// Hosting entry — the core adds it only when the hosting extension is enabled; the
// extension always passes true and marks it Active.
func AdminNav(withHosting bool) []NavItem {
	nav := []NavItem{
		{Key: "servizi", Href: "/admin/servizi", LabelKey: "admin.services"},
		{Key: "docker", Href: "/admin/docker", LabelKey: "admin.docker"},
		{Key: "providers", Href: "/admin/providers", LabelKey: "admin.providers"},
		{Key: "utenti", Href: "/admin/utenti", LabelKey: "admin.users"},
		{Key: "monitoring", Href: "/admin/monitoring", LabelKey: "admin.monitoring"},
		{Key: "tls", Href: "/admin/tls", LabelKey: "admin.tls"},
	}
	if withHosting {
		nav = append(nav, NavItem{Key: "hosting", Href: "/admin/hosting", LabelKey: "admin.hosting"})
	}
	return nav
}

// Chrome describes the shared page frame (head + top bar + container) that wraps a
// content template. The core and each extension fill it with their own brand and
// navigation; everything else (assets, language cluster, RTL, layout) is shared.
type Chrome struct {
	Title         string    // <title>
	BrandText     string    // e.g. "⛬ Xal-Tor-Ka"
	BrandHref     string    // e.g. "/admin"
	SubtitleKey   string    // i18n key for the small subtitle next to the brand
	Version       string    // shown as a tag next to the brand ("" = omit)
	Nav           []NavItem // top navigation entries
	Active        string    // NavItem.Key of the current page
	DashboardHref string    // trailing dashboard link ("" = omit)
	DashboardKey  string    // i18n key for the dashboard link label
	LoggedIn      bool      // show profile+logout in the cluster
}

// Topbar renders the shared header for the given language.
func (c Chrome) Topbar(lang string) string {
	var nav strings.Builder
	for _, it := range c.Nav {
		cls := ""
		if it.Key == c.Active {
			cls = ` class="active"`
		}
		fmt.Fprintf(&nav, `<a href="%s"%s>%s</a>`, it.Href, cls, template.HTMLEscapeString(i18n.T(lang, it.LabelKey)))
	}
	var b strings.Builder
	b.WriteString(`<header class="topbar"><div class="brand"><a href="` + template.HTMLEscapeString(c.BrandHref) + `" style="color:inherit;text-decoration:none">` + template.HTMLEscapeString(c.BrandText) + `</a>`)
	if c.SubtitleKey != "" || c.Version != "" {
		b.WriteString(`<span class="subver">`)
		if c.SubtitleKey != "" {
			b.WriteString(`<span class="sub">` + template.HTMLEscapeString(i18n.T(lang, c.SubtitleKey)) + `</span>`)
		}
		if c.Version != "" {
			b.WriteString(`<span class="ver">` + template.HTMLEscapeString(c.Version) + `</span>`)
		}
		b.WriteString(`</span>`)
	}
	b.WriteString(`</div><nav class="topnav">`)
	b.WriteString(nav.String())
	if c.DashboardHref != "" {
		b.WriteString(`<a href="` + template.HTMLEscapeString(c.DashboardHref) + `">` + template.HTMLEscapeString(i18n.T(lang, c.DashboardKey)) + `</a>`)
	}
	b.WriteString(string(IconCluster(lang, c.LoggedIn)))
	b.WriteString(`</nav></header>`)
	return b.String()
}

// Render writes the full localized page (head + top bar + container) around a
// content template, cloning it and binding the request language.
func (c Chrome) Render(w http.ResponseWriter, lang string, t *template.Template, data any) {
	dir := ""
	if i18n.IsRTL(lang) {
		dir = ` dir="rtl"`
	}
	title := c.Title
	if title == "" {
		title = "Xal-Tor-Ka"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="%s"%s><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>`,
		lang, dir, template.HTMLEscapeString(title))
	io.WriteString(w, c.Topbar(lang))
	io.WriteString(w, `<main class="container">`)
	if ct, err := t.Clone(); err == nil {
		ct.Funcs(LocFuncs(lang))
		_ = ct.Execute(w, data)
	} else {
		_ = t.Execute(w, data)
	}
	io.WriteString(w, `</main></body></html>`)
}
