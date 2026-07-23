// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package legacyhtml

import (
	"html/template"

	"xaltorka/xtkui"
)

var LoginTmpl = template.Must(template.New("login").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "login.title"}}</title><link rel="stylesheet" href="/_xtk/assets/admin.css"><script src="/_xtk/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>⛬ {{T .Lang "login.title"}}</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <form method="post" action="/login">
  <input type="hidden" name="next" value="{{.Next}}">
  <div class="field"><label>{{T .Lang "field.email"}}</label><input type="email" name="email" autocomplete="username" required></div>
  <div class="field"><label>{{T .Lang "field.password"}}</label><input type="password" name="password" autocomplete="current-password" required></div>
  <button class="btn primary">{{T .Lang "btn.continue"}}</button>
 </form>
 {{if .Code}}<p class="hint" style="margin-top:.8rem;text-align:center"><a href="/login/code?next={{.Next}}">Accedi con un codice monouso</a></p>{{end}}
 {{if .OIDC}}<div class="oidc"><div class="sep"><span>{{T .Lang "login.or"}}</span></div>
  {{range .OIDC}}<a class="btn oauth" href="/auth/{{.ID}}/start?next={{$.Next}}">{{T $.Lang "login.with"}} {{.Name}}</a>{{end}}
 </div>{{end}}
 {{if .Version}}<div class="ver-foot">⛬ Xal-Tor-Ka · {{.Version}}</div>{{end}}
 {{corner .Lang}}
</div></div></body></html>`))

var TotpTmpl = template.Must(template.New("totp").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "totp.title"}}</title><link rel="stylesheet" href="/_xtk/assets/admin.css"><script src="/_xtk/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>{{T .Lang "totp.title"}}</h1>
 {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
 <form method="post" action="/auth/totp">
  <input type="hidden" name="next" value="{{.Next}}">
  <div class="field"><label>{{T .Lang "totp.code"}}</label><input name="code" inputmode="numeric" autocomplete="one-time-code" required></div>
  <button class="btn primary">{{T .Lang "btn.verify"}}</button>
 </form>
 {{corner .Lang}}
</div></div></body></html>`))
