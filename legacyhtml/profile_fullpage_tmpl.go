// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package legacyhtml

import (
	"html/template"

	"xaltorka/xtkui"
)

var ProfileTmpl = template.Must(template.New("profile").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "profile.subtitle"}}</title><link rel="stylesheet" href="/_xtk/assets/admin.css"><script src="/_xtk/assets/admin.js" defer></script></head><body>
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
 {{if .ClientIP}}<p class="hint" style="margin-top:1.5rem">{{T .Lang "profile.client_ip"}}: <code>{{.ClientIP}}</code></p>{{end}}
</main></body></html>`))

var ProfileQRTmpl = template.Must(template.New("profileqr").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "qr.new2fa"}}</title><link rel="stylesheet" href="/_xtk/assets/admin.css"><script src="/_xtk/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card qr">
 <h1>{{T .Lang "qr.new2fa"}}</h1>
 <p class="hint">{{T .Lang "qr.scan"}}</p>
 <p><img src="{{.QR}}" alt="QR otpauth" width="240" height="240"></p>
 <p>{{T .Lang "qr.key"}}: <code>{{.Secret}}</code></p>
 <p style="margin-top:1.2rem"><a href="/profilo">{{T .Lang "qr.back_profile"}}</a></p>
</div></div>{{corner .Lang}}</body></html>`))
