// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package legacyhtml holds the server-rendered HTML of the admin panel — the
// "legacy-html" frontend layer. Templates only: they render data structs defined
// by the handlers (html/template binds by field name via reflection, so there is
// no Go type coupling). Owned by the dev-go-html-fe figure. See docs/legacy-html-split.md.
package legacyhtml

import "xaltorka/xtkui"

var TlsTmpl = xtkui.LocParse("tls", `<section>
 <h2>{{T "admin.tls.h2"}}</h2>
 <p class="hint">{{T "admin.tls.hint"}}</p>
 <div id="le-overlay" style="display:none;position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:9999;align-items:center;justify-content:center">
  <div style="background:var(--panel,#fff);color:var(--ink,#111);padding:1.6rem 2rem;border-radius:12px;text-align:center;max-width:90%;box-shadow:0 8px 40px rgba(0,0,0,.4)">
   <p style="font-size:1.6rem;margin:0 0 .4rem">⏳</p>
   <p style="margin:.2rem 0"><b>Emissione certificato Let's Encrypt</b><br>per <code id="le-host"></code>…</p>
   <p class="hint" style="margin:.4rem 0 0">L'operazione ACME può durare qualche secondo, non chiudere la pagina.</p>
  </div>
 </div>
 <script>function xtkLE(h){var o=document.getElementById('le-overlay');document.getElementById('le-host').textContent=h;o.style.display='flex';return true;}</script>
 {{if .Site}}<p class="hint">🏠 {{T "admin.tls.site_scope"}} <b>{{.Site}}</b>. <a href="/admin/tls">{{T "admin.tls.show_all"}} →</a></p>{{end}}
 {{if .HasMsg}}<div class="{{if .MsgOK}}ok{{else}}err{{end}}">{{T (print "admin.tls." .Msg)}}</div>{{end}}
 <table><thead><tr><th>{{T "admin.col.host"}}</th><th>{{T "admin.tls.col.source"}}</th><th>{{T "admin.tls.col.expiry"}}</th><th>{{T "admin.tls.col.status"}}</th><th></th></tr></thead><tbody>
 {{range .Rows}}<tr id="h-{{.Host}}"{{if .Sub}} class="tls-sub"{{end}}>
  <td>{{if .Sub}}<span class="tls-branch" aria-hidden="true">↳</span> {{end}}<code>{{.Host}}</code></td>
  <td>{{if eq (printf "%s" .Source) "acme"}}{{T "admin.tls.src.acme"}}{{else if eq (printf "%s" .Source) "selfsigned"}}{{T "admin.tls.src.selfsigned"}}{{else}}—{{end}}</td>
  <td>{{if .Has}}{{.Expiry}}{{else}}—{{end}}</td>
  <td>{{if not .Has}}<span class="tag err stw">{{T "admin.tls.status.missing"}}</span>{{else if .Valid}}<span class="tag ok stw">{{T "admin.tls.status.valid"}}</span>{{else}}<span class="tag warn stw">{{T "admin.tls.status.expired"}}</span>{{end}}</td>
  <td><div class="actions">
   <form class="inline" method="post" action="/admin/tls/issue">
    <input type="hidden" name="host" value="{{.Host}}">
    <label class="hint" title="also serve/cert www.{{.Host}}" style="display:inline-flex;align-items:center;gap:.25rem"><input type="checkbox" name="www" value="1"{{if .WWW}} checked{{end}}> www.</label>
    <button class="btn sm" name="mode" value="acme" onclick="xtkLE('{{.Host}}')">{{T "admin.tls.issue_le"}}</button>
    <button class="btn sm" name="mode" value="selfsigned">{{T "admin.tls.issue_ss"}}</button>
   </form>
   {{if .Has}}<form class="inline" method="post" action="/admin/tls/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="host" value="{{.Host}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>{{else}}<button class="btn danger sm" disabled title="no certificate to delete">{{T "admin.act.delete"}}</button>{{end}}
  </div></td></tr>{{end}}
 {{if not .Rows}}<tr><td colspan="5" class="empty">{{T "admin.tls.none"}}</td></tr>{{end}}
 </tbody></table>
</section>
<section style="margin-top:1.4rem">
 <h2>{{T "admin.tls.ca_h"}}</h2>
 <div class="card">
  {{if .CAAvailable}}<p><a class="btn" href="/admin/tls/ca.crt">{{T "admin.tls.ca_dl"}}</a></p>
  <p class="hint">{{T "admin.tls.ca_hint"}}</p>
  {{else}}<p class="hint">{{T "admin.tls.ca_none"}}</p>{{end}}
  <p class="hint">{{T "admin.tls.email"}}: <code>{{if .Email}}{{.Email}}{{else}}—{{end}}</code> — {{T "admin.tls.email_hint"}}</p>
 </div>
</section>`)
