// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package legacyhtml holds the server-rendered HTML of the admin panel — the
// "legacy-html" frontend layer. Templates only: they render data structs defined
// by the handlers (html/template binds by field name via reflection, so there is
// no Go type coupling). Owned by the dev-go-html-fe figure. See docs/legacy-html-split.md.
package legacyhtml

import "xaltorka/xtkui"

var ProvidersTmpl = xtkui.LocParse("providers", `<section>
 <h2>{{T "admin.prov.h2"}}</h2>
 <p class="hint">{{T "admin.prov.hint"}}</p>
 {{if .Tested}}<div class="{{if .TestedOK}}ok{{else}}err{{end}}">{{if .TestedOK}}✓ {{T "admin.prov.test_ok"}}{{else}}✗ {{T "admin.prov.test_fail"}}{{end}} — <code>{{.TestedID}}</code></div>{{end}}
 <table><thead><tr><th>{{T "admin.prov.col.id"}}</th><th>{{T "admin.f.name"}}</th><th>{{T "admin.prov.col.type"}}</th><th>{{T "admin.prov.col.enabled"}}</th><th>{{T "admin.prov.f.issuer"}}</th><th>{{T "admin.prov.col.secret"}}</th><th></th></tr></thead><tbody>
 {{range .Rows}}<tr{{if not .Enabled}} class="off"{{end}}>
  <td><b>{{.ID}}</b>{{if not .Editable}} <span class="tag ro">{{T "admin.prov.src.config"}}</span>{{end}}</td>
  <td>{{.Name}}</td>
  <td><span class="tag">{{.Type}}</span></td>
  <td>{{if .Enabled}}<span class="tag">on</span>{{else}}<span class="tag ro">off</span>{{end}}</td>
  <td>{{if .Issuer}}<code>{{.Issuer}}</code>{{else}}—{{end}}</td>
  <td>{{if eq .Type "oidc"}}{{if .SecretSet}}<span class="tag">set</span>{{else}}<span class="tag ro">—</span>{{end}}{{else}}—{{end}}</td>
  <td><div class="actions">
   {{if .Editable}}
    <a class="btn sm" href="/admin/provider/edit?id={{.ID}}">{{T "admin.act.edit"}}</a>
    {{if eq .Type "oidc"}}<form class="inline" method="post" action="/admin/provider/test"><input type="hidden" name="id" value="{{.ID}}"><button class="btn sm">{{T "admin.prov.test"}}</button></form>{{end}}
    <form class="inline" method="post" action="/admin/provider/toggle"><input type="hidden" name="id" value="{{.ID}}"><button class="btn sm">{{if .Enabled}}{{T "admin.act.disable"}}{{else}}{{T "admin.act.enable"}}{{end}}</button></form>
    <form class="inline" method="post" action="/admin/provider/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="id" value="{{.ID}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>
   {{else}}<span class="hint">{{T "admin.prov.readonly"}}</span>{{end}}
  </div></td></tr>{{end}}
 {{if not .Rows}}<tr><td colspan="7" class="empty">{{T "admin.prov.none"}}</td></tr>{{end}}
 </tbody></table>
</section>
<section style="margin-top:1.4rem">
 <h2>{{T "admin.prov.add_h"}}</h2>
 <div class="card">
  <form method="post" action="/admin/provider/add">
   <input type="hidden" name="type" value="oidc">
   <table class="ftable"><tbody>
    <tr><th>{{T "admin.prov.f.preset"}}</th><td><select onchange="var v=this.value;if(v){var f=this.form;f.issuer.value=v;}">
      <option value="">{{T "admin.prov.preset.custom"}}</option>
      <option value="https://accounts.google.com">Google</option>
      <option value="https://login.microsoftonline.com/common/v2.0">Microsoft</option>
     </select></td><td class="fhelp">{{T "admin.prov.help.preset"}}</td></tr>
    <tr><th>{{T "admin.prov.col.id"}}</th><td><input name="id" placeholder="google" required></td><td class="fhelp">{{T "admin.prov.help.id"}}</td></tr>
    <tr><th>{{T "admin.f.name"}}</th><td><input name="name" placeholder="Google"></td><td class="fhelp">{{T "admin.prov.help.name"}}</td></tr>
    <tr><th>{{T "admin.prov.f.issuer"}}</th><td><input name="issuer" placeholder="https://accounts.google.com"></td><td class="fhelp">{{T "admin.prov.help.issuer"}}</td></tr>
    <tr><th>{{T "admin.prov.f.client_id"}}</th><td><input name="client_id" placeholder="1234….apps.googleusercontent.com"></td><td class="fhelp">{{T "admin.prov.help.client_id"}}</td></tr>
    <tr><th>{{T "admin.prov.f.client_secret"}}</th><td><input name="client_secret" type="password" autocomplete="new-password"></td><td class="fhelp">{{T "admin.prov.help.client_secret"}}</td></tr>
    <tr><th>{{T "admin.prov.col.enabled"}}</th><td><input type="checkbox" name="enabled"></td><td class="fhelp">{{T "admin.prov.help.enabled"}}</td></tr>
   </tbody></table>
   <div class="actions"><button class="btn primary">{{T "btn.add"}}</button></div>
  </form>
 </div>
</section>`)

var ProviderEditTmpl = xtkui.LocParse("provedit", `<h1>{{T "admin.prov.edit_h1"}} «{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}}»</h1>
 <div class="card">
  <form method="post" action="/admin/provider/edit">
   <input type="hidden" name="id" value="{{.ID}}">
   <table class="ftable"><tbody>
    <tr><th>{{T "admin.prov.col.id"}}</th><td><input value="{{.ID}}" disabled></td><td class="fhelp">{{T "admin.prov.help.id_ro"}}</td></tr>
    <tr><th>{{T "admin.f.name"}}</th><td><input name="name" value="{{.Name}}"></td><td class="fhelp">{{T "admin.prov.help.name"}}</td></tr>
    <tr><th>{{T "admin.prov.f.issuer"}}</th><td><input name="issuer" value="{{.Issuer}}" required></td><td class="fhelp">{{T "admin.prov.help.issuer"}}</td></tr>
    <tr><th>{{T "admin.prov.f.client_id"}}</th><td><input name="client_id" value="{{.ClientID}}" required></td><td class="fhelp">{{T "admin.prov.help.client_id"}}</td></tr>
    <tr><th>{{T "admin.prov.f.client_secret"}}</th><td><input name="client_secret" type="password" autocomplete="new-password" placeholder="{{if .SecretSet}}{{T "admin.prov.secret_keep"}}{{else}}{{T "admin.prov.secret_unset"}}{{end}}"></td><td class="fhelp">{{T "admin.prov.help.client_secret"}}</td></tr>
    <tr><th>{{T "admin.prov.col.enabled"}}</th><td><input type="checkbox" name="enabled"{{if .Enabled}} checked{{end}}></td><td class="fhelp">{{T "admin.prov.help.enabled"}}</td></tr>
    <tr><th>{{T "admin.prov.redirect"}}</th><td colspan="2"><code>{{.Redirect}}</code></td></tr>
   </tbody></table>
   <div class="actions" style="margin-top:1rem">
    <button class="btn primary">{{T "btn.save"}}</button><a class="btn" href="/admin/providers">{{T "admin.cancel"}}</a></div>
  </form>
 </div>`)
