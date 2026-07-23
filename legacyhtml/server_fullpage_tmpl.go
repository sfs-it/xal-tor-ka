// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package legacyhtml

import (
	"html/template"

	"xaltorka/xtkui"
)

var ListingTmpl = template.Must(template.New("listing").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "listing.subtitle"}}</title><link rel="stylesheet" href="/_xtk/assets/admin.css"><script src="/_xtk/assets/admin.js" defer></script></head><body>
<header class="topbar">
 <div class="brand">⛬ Xal-Tor-Ka<span class="sub">{{T .Lang "listing.subtitle"}}</span></div>
 <nav class="topnav"><span class="who">{{.Email}}</span>
  {{if .IsAdmin}}<a href="/admin">{{T .Lang "nav.admin"}}</a>{{end}}
  {{cluster .Lang}}</nav>
</header>
<main class="container">
 <h1>{{T .Lang "listing.title"}}</h1>
 <div class="grid">
 {{range .Groups}}{{if and .Site (gt (len .Tiles) 1)}}<section class="sitegroup"><h2>{{.Site}}/</h2><div class="grid subgrid">{{range .Tiles}}<a class="card" href="{{.URL}}"{{if .External}} target="_blank" rel="noopener"{{end}}>
   <div class="row"><h3>{{.Name}}</h3><span class="tag {{if .External}}ext{{end}}">{{if .External}}{{T $.Lang "tag.external"}}{{else}}{{T $.Lang "tag.proxy"}}{{end}}</span></div>
   {{if .Image}}<img src="{{.Image}}" alt="" loading="lazy" style="max-width:100%;border-radius:8px;margin:.4rem 0;display:block">{{end}}{{if .Description}}<div class="meta">{{.Description}}</div>{{else if .Host}}<div class="meta"><code>{{.Host}}</code></div>{{end}}</a>{{end}}</div></section>{{else}}{{range .Tiles}}<a class="card" href="{{.URL}}"{{if .External}} target="_blank" rel="noopener"{{end}}>
   <div class="row"><h3>{{.Name}}</h3><span class="tag {{if .External}}ext{{end}}">{{if .External}}{{T $.Lang "tag.external"}}{{else}}{{T $.Lang "tag.proxy"}}{{end}}</span></div>
   {{if .Image}}<img src="{{.Image}}" alt="" loading="lazy" style="max-width:100%;border-radius:8px;margin:.4rem 0;display:block">{{end}}{{if .Description}}<div class="meta">{{.Description}}</div>{{else if .Host}}<div class="meta"><code>{{.Host}}</code></div>{{end}}</a>{{end}}{{end}}
 {{else}}<p class="empty">{{T .Lang "listing.empty"}}</p>{{end}}
 </div>
</main></body></html>`))
