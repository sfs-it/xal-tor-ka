// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package legacyhtml

import (
	"html/template"

	"xaltorka/xtkui"
)

var CodeRequestTmpl = template.Must(template.New("codereq").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
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

var CodeVerifyTmpl = template.Must(template.New("codever").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
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
