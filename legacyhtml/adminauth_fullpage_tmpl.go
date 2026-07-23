// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package legacyhtml

import (
	"html/template"

	"xaltorka/xtkui"
)

var ForbiddenAdminTmpl = template.Must(template.New("forbidden").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "blocked.title"}}</title><link rel="stylesheet" href="/_xtk/assets/admin.css"></head><body>
<div class="auth-wrap"><div class="auth-card">
 <h1>{{T .Lang "blocked.title"}}</h1>
 <div class="err">{{T .Lang "blocked.not_admin"}}</div>
 <p class="hint">{{T .Lang "blocked.you_are"}} <strong>{{.Email}}</strong>. {{T .Lang "blocked.need_admin"}}</p>
 <form method="post" action="/logout"><button class="btn primary">{{T .Lang "blocked.logout_switch"}}</button></form>
 <p style="margin-top:.9rem"><a href="/listing">{{T .Lang "blocked.to_dashboard"}}</a></p>
</div></div>{{corner .Lang}}</body></html>`))
