// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package xtkui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChromeRender(t *testing.T) {
	c := Chrome{
		Title: "Xal-Tor-Ka · Admin", BrandText: "⛬ Xal-Tor-Ka", BrandHref: "/admin",
		SubtitleKey: "admin.subtitle", Version: "beta0.4",
		Nav: []NavItem{
			{Key: "servizi", Href: "/admin/servizi", LabelKey: "admin.services"},
			{Key: "tls", Href: "/admin/tls", LabelKey: "admin.tls"},
		},
		Active:        "tls",
		DashboardHref: "/listing", DashboardKey: "nav.dashboard", LoggedIn: true,
	}
	tmpl := LocParse("t", `<h1>{{T "admin.title"}}</h1>{{if rtl}}rtl{{end}}`)
	rec := httptest.NewRecorder()
	c.Render(rec, "en", tmpl, nil)
	out := rec.Body.String()

	for _, want := range []string{
		"<!doctype html>",
		`<link rel="stylesheet" href="/_xtk/assets/admin.css">`,
		`class="topbar"`,
		`href="/admin/servizi"`,
		`href="/admin/tls" class="active"`, // active page highlighted
		`beta0.4`,
		`href="/listing"`,   // dashboard link
		`class="cluster"`,   // language + profile + logout cluster
		`action="/logout"`,  // logged-in cluster
		`<main class="container">`,
		"</main></body></html>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered chrome missing %q", want)
		}
	}
}

func TestLangCornerAndCluster(t *testing.T) {
	if !strings.Contains(string(LangCorner("en")), "langpop") {
		t.Error("LangCorner should contain the language popup")
	}
	if strings.Contains(string(IconCluster("en", false)), "/logout") {
		t.Error("IconCluster(loggedIn=false) must not show logout")
	}
	if !strings.Contains(string(IconCluster("en", true)), "/logout") {
		t.Error("IconCluster(loggedIn=true) must show logout")
	}
}

func TestRuleOptionsOrderAndDefault(t *testing.T) {
	// The order is a UX+safety invariant: least→most restrictive, matching the
	// on-screen explanation. If someone reorders it, this test must fail.
	got := string(ruleOptions("authorized"))
	pub := strings.Index(got, ">public<")
	aut := strings.Index(got, ">authenticated<")
	azd := strings.Index(got, ">authorized<")
	if !(pub >= 0 && pub < aut && aut < azd) {
		t.Fatalf("wrong order: public=%d authenticated=%d authorized=%d in %q", pub, aut, azd, got)
	}
	// The passed value is the one pre-selected — a new service must never default
	// to public by accident.
	if !strings.Contains(got, "<option selected>authorized</option>") {
		t.Errorf("selected default not applied: %q", got)
	}
	if strings.Count(got, "selected") != 1 {
		t.Errorf("exactly one option must be selected: %q", got)
	}
}
