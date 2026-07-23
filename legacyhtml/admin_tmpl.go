// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package legacyhtml holds the server-rendered HTML of the admin panel — the
// "legacy-html" frontend layer. Templates only: they render data structs defined
// by the handlers (html/template binds by field name via reflection, so there is
// no Go type coupling). Owned by the dev-go-html-fe figure. See docs/legacy-html-split.md.
package legacyhtml

import "xaltorka/xtkui"

var OverviewTmpl = xtkui.LocParse("ov", `<h1>{{T "admin.title"}}</h1>
<div class="grid">
 <a class="card" href="/admin/servizi"><div class="row"><h3>{{T "admin.services"}}</h3><span class="tag">{{.Services}}</span></div><div class="meta">{{.ConfigBackends}} {{T "admin.from_config"}} · {{.Links}} {{T "admin.links"}}</div></a>
 <a class="card" href="/admin/docker"><div class="row"><h3>{{T "admin.docker"}}</h3><span class="tag">{{T "admin.discover"}}</span></div><div class="meta">{{T "admin.ov.docker_meta"}}</div></a>
 <a class="card" href="/admin/utenti"><div class="row"><h3>{{T "admin.users"}}</h3><span class="tag">{{.Users}}</span></div><div class="meta">{{T "admin.ov.users_meta"}}</div></a>
 <a class="card" href="/admin/monitoring"><div class="row"><h3>{{T "admin.monitoring"}}</h3><span class="badge up">{{.Up}}</span> <span class="badge down">{{.Down}}</span></div><div class="meta">{{T "admin.ov.mon_meta"}}</div></a>
</div>
<section style="margin-top:1.4rem">
 <h2>{{T "admin.sec.title"}}</h2>
 <p class="hint">{{T "admin.sec.hint"}} <b>{{.AdminIPsSource}}</b>. {{T "admin.sec.your_ip"}} <code>{{.ClientIP}}</code>.</p>
 <div class="card">
  <form method="post" action="/admin/adminips">
   <div><label>{{T "admin.sec.field"}}</label><input name="ip_whitelist" value="{{.AdminIPsRaw}}" placeholder="203.0.113.7/32 10.0.0.0/24"></div>
   <p class="hint">⚠️ {{T "admin.sec.warn"}}</p>
   <div class="actions"><button class="btn primary">{{T "btn.save"}}</button></div>
  </form>
 </div>
</section>`)

var ServicesTmpl = xtkui.LocParse("services", `<section>
 <h2>{{T "admin.svc.h2"}}</h2>
 <p class="hint">{{T "admin.svc.hint"}}</p>
 <table><thead><tr><th>{{T "admin.col.service"}}</th><th>{{T "admin.col.host"}} / {{T "admin.col.upstream"}}</th><th>{{T "admin.col.rule"}}</th><th></th></tr></thead><tbody>
 {{range .ConfigBackends}}<tr><td>{{.ID}} <span class="tag ro">config</span></td>
   <td><a href="//{{.Host}}" target="_blank" rel="noopener"><code>{{.Host}}</code></a>{{range .Routes}}<div class="hint"><code>{{.Upstream}}</code></div>{{end}}</td>
   <td>{{range .Routes}}<span class="tag">{{.Rule}}</span> {{end}}</td>
   <td></td><td class="rowact"><a class="btn sm" href="/admin/tls#h-{{.Host}}">{{T "admin.tls.manage"}}</a></td></tr>{{end}}
 {{range .Groups}}{{$g := .}}{{$grp := and $g.Site (gt (len $g.Backends) 1)}}{{if $grp}}<tr class="svc-ghdr"><td colspan="4"><b>{{$g.Site}}/</b> <span class="hint">{{len $g.Backends}} {{T "admin.services"}}</span></td></tr>{{end}}{{range $g.Backends}}<tr{{if .Disabled}} class="off"{{end}}>
   <td>{{if $grp}}<span class="tls-branch" aria-hidden="true">↳</span> {{end}}<b>{{.Label}}</b>{{if .Disabled}} <span class="tag ro">off</span>{{end}}{{if .Description}}<div class="hint">{{.Description}}</div>{{end}}</td>
   <td><a href="//{{.Host}}" target="_blank" rel="noopener"><code>{{.Host}}</code></a>{{range .Routes}}<div class="hint"><code>{{.Upstream}}</code></div>{{end}}</td>
   <td>{{range .Routes}}<span class="tag">{{.Rule}}</span> {{end}}{{if .IPAllow}}<span class="tag ro ipbadge" title="{{range .IPAllow}}{{.}}&#10;{{end}}">🔒 {{len .IPAllow}} IP</span>{{end}}</td>
   <td class="rowact">
    <a class="btn sm" href="/admin/backend/edit?id={{.ID}}">{{T "admin.act.edit"}}</a>
    <a class="btn sm" href="/admin/tls#h-{{.Host}}">{{T "admin.tls.manage"}}</a>
    <form class="inline" method="post" action="/admin/backend/toggle"><input type="hidden" name="id" value="{{.ID}}"><button class="btn sm">{{if .Disabled}}{{T "admin.act.enable"}}{{else}}{{T "admin.act.disable"}}{{end}}</button></form>
    <form class="inline" method="post" action="/admin/backend/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="id" value="{{.ID}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>
   </td></tr>
 {{end}}{{end}}
 </tbody></table>
 <div class="card addcard" style="margin-top:1rem"><h3>{{T "admin.svc.add"}}</h3>
  <form method="post" action="/admin/backend/add"><div class="formgrid">
   <div><label>{{T "admin.f.id"}}</label><input name="id" required></div>
   <div><label>{{T "admin.f.name"}}</label><input name="name"></div>
   <div><label>{{T "admin.f.host"}}</label><input name="host" placeholder="app.example.com" required></div>
   <div><label>{{T "admin.f.path"}}</label><input name="path" value="/"></div>
   <div><label>{{T "admin.f.rule"}}</label><select name="rule">{{ruleOptions "authorized"}}</select></div>
   <div><label>{{T "admin.f.upstream"}}</label><input name="upstream" placeholder="http://10.0.0.5:8080"></div>
   <div><label>{{T "admin.f.url"}}</label><input name="url" placeholder="https://app.example.com"></div>
   <div><label>www</label><label class="hint" style="display:inline-flex;align-items:center;gap:.35rem;height:2.2rem"><input type="checkbox" name="www" value="1"> also www.&lt;host&gt;</label></div>
   <div><label>{{T "admin.f.ipallow"}}</label><input name="ip_allow" placeholder="203.0.113.0/24"></div>
   <div><button class="btn primary">{{T "btn.add"}}</button></div>
  </div><p class="hint">{{T "admin.rule.help"}}</p></form></div>
</section>
{{if not .HostingEnabled}}
<section>
 <div class="card addcard" style="border-color:var(--accent)">
  <h3>{{T "admin.hosting_off.title"}}</h3>
  <p class="hint">{{T "admin.hosting_off.body"}}</p>
  <pre style="background:var(--accent-weak);padding:.5rem .7rem;border-radius:.5rem;overflow-x:auto;margin:.5rem 0"><code>sudo deploy/agent/install.sh --dev</code></pre>
  <p class="hint"><code>deploy/agent/install.sh --help</code> · <code>make hosting-install</code></p>
 </div>
</section>
{{end}}
<section>
 <h2>{{T "admin.links.h2"}}</h2><p class="hint">{{T "admin.links.hint"}}</p>
 <table><thead><tr><th>{{T "admin.col.name"}}</th><th>{{T "admin.col.url"}}</th><th>{{T "admin.col.visibility"}}</th><th></th></tr></thead><tbody>
 {{range .Links}}<tr{{if .Disabled}} class="off"{{end}}>
   <td><b>{{.Name}}</b>{{if .Disabled}} <span class="tag ro">off</span>{{end}}{{if .Description}}<div class="hint">{{.Description}}</div>{{end}}</td>
   <td><a href="{{.URL}}" target="_blank" rel="noopener"><code>{{.URL}}</code></a></td>
   <td><span class="tag ext">{{if .Public}}{{T "admin.vis.public"}}{{else}}{{T "admin.vis.private"}}{{end}}</span></td>
   <td class="rowact">
    <form class="inline" method="post" action="/admin/link/toggle"><input type="hidden" name="id" value="{{.ID}}"><button class="btn sm">{{if .Disabled}}{{T "admin.act.enable"}}{{else}}{{T "admin.act.disable"}}{{end}}</button></form>
    <form class="inline" method="post" action="/admin/link/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="id" value="{{.ID}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>
   </td></tr>
 {{else}}{{end}}
 </tbody></table>
 <div class="card addcard" style="margin-top:1rem"><h3>{{T "admin.links.add"}}</h3>
  <form method="post" action="/admin/link/add"><div class="formgrid">
   <div><label>{{T "admin.f.id"}}</label><input name="id" required></div>
   <div><label>{{T "admin.f.name"}}</label><input name="name" required></div>
   <div><label>{{T "admin.f.url"}}</label><input name="url" placeholder="https://..." required></div>
   <div><label>{{T "admin.f.desc"}}</label><input name="desc"></div>
   <div><label class="check"><input type="checkbox" name="public"> {{T "admin.f.public"}}</label></div>
   <div><button class="btn primary">{{T "btn.add"}}</button></div>
  </div></form></div>
</section>`)

var DockerTmpl = xtkui.LocParse("docker", `<section>
 <h2>{{T "admin.dk.h2"}}</h2>
 {{if .DockerEnabled}}
  <p class="hint">{{T "admin.dk.hint"}}</p>
  <table><thead><tr><th>{{T "admin.dk.container"}}</th><th>{{T "admin.dk.port"}}</th><th>{{T "admin.dk.vhost"}}</th><th></th></tr></thead><tbody>
  {{range .Discovered}}<tr><td>{{.Name}}</td><td>{{.Port}}</td><td><code>{{.Host}}</code></td>
   <td class="rowact">{{if .Added}}<span class="tag ro">{{T "admin.dk.added"}}</span>{{else}}<form class="inline" method="post" action="/admin/discover/add">
    <input type="hidden" name="name" value="{{.Name}}"><input type="hidden" name="port" value="{{.Port}}">
    <select name="rule" style="width:auto;vertical-align:middle">{{ruleOptions "authorized"}}</select>
    <button class="btn primary sm">{{T "btn.add"}}</button></form>{{end}}</td></tr>
  {{else}}<tr><td colspan="4" class="empty">{{T "admin.dk.none"}}</td></tr>{{end}}
  </tbody></table>
 {{else}}<p class="hint">{{T "admin.dk.disabled"}}</p>{{end}}
 <h3 style="margin-top:1.4rem">{{T "admin.dk.portmap"}}</h3>
 <p class="hint">{{T "admin.dk.portmap_hint"}}</p>
 <table><thead><tr><th>{{T "admin.dk.vhost"}}</th><th>{{T "admin.dk.hostport"}}</th><th>{{T "admin.col.rule"}}</th></tr></thead><tbody>
 {{range .Ports}}<tr{{if .Disabled}} class="off"{{end}}>
   <td><a href="//{{.Host}}" target="_blank" rel="noopener"><code>{{.Host}}</code></a></td>
   <td><code>host.docker.internal:{{.Port}}</code></td>
   <td><span class="tag">{{.Rule}}</span></td></tr>
 {{else}}<tr><td colspan="3" class="empty">{{T "admin.dk.noport"}}</td></tr>{{end}}
 </tbody></table>
 <h3 style="margin-top:1.4rem">{{T "admin.dk.hostports"}}</h3>
 <p class="hint">{{T "admin.dk.hostports_hint"}}</p>
 <form method="get" action="/admin/hostscan">
  <label class="check">{{T "admin.dk.from"}} <input name="from" value="3000" style="width:5.5rem"></label>
  <label class="check">{{T "admin.dk.to"}} <input name="to" value="3100" style="width:5.5rem"></label>
  <button class="btn sm">{{T "admin.dk.scan"}}</button>
 </form>
</section>`)

var UsersTmpl = xtkui.LocParse("users", `<section>
 <h2>{{T "admin.users"}}</h2>
 <div style="margin:.5rem 0"><input id="ufilter" type="search" placeholder="{{T "admin.usr.filter"}}" oninput="ufilter()" style="width:18rem;padding:.38rem .65rem;border:1px solid var(--line);border-radius:8px;background:var(--panel);color:var(--text)"></div>
 <table class="utbl" id="utbl"><thead><tr><th>{{T "admin.usr.email"}}</th><th class="usort" onclick="usortAdmin()" style="cursor:pointer;user-select:none" title="{{T "admin.usr.sort_admin"}}">Admin&nbsp;⇅</th><th>{{T "admin.usr.enabled_hosts"}}</th><th></th></tr></thead><tbody>
 {{range .Users}}<tr data-admin="{{if .Admin}}1{{else}}0{{end}}" data-email="{{.Email}}">
  <td><a href="/admin/utenti/{{.Email}}">{{.Email}}</a></td>
  <td>{{if .Admin}}<span class="tag">admin</span>{{end}}</td>
  <td>{{if .Admin}}<span class="hint">{{T "admin.usr.all_admin"}}</span>{{else if .Hosts}}<details><summary>{{len .Hosts}} host</summary><ul class="hostlist">{{range .Hosts}}<li>{{.}}</li>{{end}}</ul></details>{{else}}<span class="hint">{{T "admin.usr.none"}}</span>{{end}}</td>
  <td><div class="actions">
   <a class="btn sm" href="/admin/utenti/{{.Email}}">{{T "admin.usr.properties"}}</a>
   <form class="inline" method="post" action="/admin/user/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="email" value="{{.Email}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>
  </div></td></tr>{{end}}
 </tbody></table>
 <script>
 function ufilter(){var q=(document.getElementById('ufilter').value||'').toLowerCase();var rows=document.querySelectorAll('#utbl tbody tr');for(var i=0;i<rows.length;i++){var e=(rows[i].getAttribute('data-email')||'').toLowerCase();rows[i].style.display=e.indexOf(q)>=0?'':'none';}}
 var uAdminAsc=false;
 function usortAdmin(){uAdminAsc=!uAdminAsc;var tb=document.querySelector('#utbl tbody');var rows=Array.prototype.slice.call(tb.querySelectorAll('tr'));rows.sort(function(a,b){var d=(+b.getAttribute('data-admin'))-(+a.getAttribute('data-admin'));if(uAdminAsc)d=-d;return d||(a.getAttribute('data-email')||'').localeCompare(b.getAttribute('data-email')||'');});for(var i=0;i<rows.length;i++)tb.appendChild(rows[i]);}
 </script>
 <div class="card addcard" style="margin-top:1rem"><h3>{{T "admin.usr.create"}}</h3>
  <form method="post" action="/admin/user/add">
   <div class="formgrid">
    <div><label>{{T "admin.usr.email"}}</label><input type="email" name="email" required></div>
    <div><label>{{T "field.password"}}</label><input type="password" name="password" required></div>
   </div>
   <div class="authztree"><label class="authz-lbl">{{T "admin.usr.authz"}}</label>{{range .AllTree}}{{if .Site}}<div class="authz-group"><div class="authz-ghdr">{{.Site}}/</div>{{range .Items}}<label class="check"><input type="checkbox" name="authz" value="{{.ID}}">{{.Label}}</label>{{end}}</div>{{else}}{{range .Items}}<label class="check"><input type="checkbox" name="authz" value="{{.ID}}">{{.Label}}</label>{{end}}{{end}}{{end}}</div>
   <div class="actions"><button class="btn primary">{{T "admin.usr.create_btn"}}</button></div>
  </form></div>
</section>`)

var UserDetailTmpl = xtkui.LocParse("userdetail", `<section>
 <p><a href="/admin/utenti">← {{T "admin.users"}}</a></p>
 <h2>{{T "admin.usr.props_of"}} «{{.Email}}»</h2>
 <div class="card">
  <div class="formgrid">
   <div><label>{{T "admin.usr.email"}}</label><form class="inline" method="post" action="/admin/user/email"><input type="hidden" name="old" value="{{.Email}}"><input name="email" value="{{.Email}}"><button class="btn sm">{{T "btn.save"}}</button></form></div>
   <div><label>{{T "admin.f.provider"}}</label><div style="padding-top:.4rem">{{.Provider}}{{if .Admin}} · <span class="tag">admin</span>{{end}}</div></div>
  </div>
  <div class="actions" style="margin-top:.8rem">
   <form class="inline" method="post" action="/admin/user/admin"><input type="hidden" name="email" value="{{.Email}}"><button class="btn sm">{{if .Admin}}{{T "admin.usr.revoke_admin"}}{{else}}{{T "admin.usr.make_admin"}}{{end}}</button></form>
   <form class="inline" method="post" action="/admin/user/password"><input type="hidden" name="email" value="{{.Email}}"><input type="password" name="password" placeholder="{{T "admin.usr.new_pw"}}" style="width:11rem"><button class="btn sm">{{T "admin.usr.set_pw"}}</button></form>
   <form class="inline" method="post" action="/admin/user/totp"><input type="hidden" name="email" value="{{.Email}}"><button class="btn sm">{{T "admin.usr.reset_2fa"}}</button></form>
   <form class="inline" method="post" action="/admin/user/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="email" value="{{.Email}}"><button class="btn danger sm">{{T "admin.usr.del_user"}}</button></form>
  </div>
 </div>
 <div class="card" style="margin-top:1rem"><h3>{{T "admin.usr.authz_title"}}</h3>
  {{if .Admin}}<p class="hint">{{T "admin.usr.admin_all_note"}}</p>{{end}}
  <form method="post" action="/admin/user/authz"><input type="hidden" name="email" value="{{.Email}}">
   <div class="authztree">{{range .AllTree}}{{if .Site}}<div class="authz-group"><div class="authz-ghdr">{{.Site}}/</div>{{range .Items}}<label class="check"><input type="checkbox" name="authz" value="{{.ID}}" {{if index $.Checked .ID}}checked{{end}}>{{.Label}}</label>{{end}}</div>{{else}}{{range .Items}}<label class="check"><input type="checkbox" name="authz" value="{{.ID}}" {{if index $.Checked .ID}}checked{{end}}>{{.Label}}</label>{{end}}{{end}}{{else}}<span class="hint">{{T "admin.usr.no_services"}}</span>{{end}}</div>
   <div class="actions" style="margin-top:.6rem"><button class="btn primary">{{T "admin.usr.save_authz"}}</button></div>
  </form>
 </div>
</section>`)

var MonitoringTmpl = xtkui.LocParse("mon", `<section>
 <h2>{{T "admin.mon.status"}}</h2>
 <table><thead><tr><th>id</th><th>{{T "admin.col.host"}}</th><th>{{T "admin.mon.state"}}</th><th>{{T "admin.mon.last_error"}}</th><th>{{T "admin.mon.last_check"}}</th></tr></thead><tbody>
 {{range .Monitoring}}<tr><td>{{.BackendID}}</td><td><code>{{.Host}}</code></td><td><span class="badge {{.State}}">{{.State}}</span></td><td>{{.LastError}}</td><td>{{.LastCheck.Format "15:04:05"}}</td></tr>
 {{else}}<tr><td colspan="5" class="empty">{{T "admin.mon.none"}}</td></tr>{{end}}
 </tbody></table>
</section>
<section>
 <h2>{{T "admin.mon.custom"}}</h2>
 <p class="hint">{{T "admin.mon.custom_hint"}}</p>
 <table><thead><tr><th>id</th><th>{{T "admin.f.name"}}</th><th>{{T "admin.col.url"}}</th><th>{{T "admin.mon.interval"}}</th><th>{{T "admin.mon.timeout"}}</th><th></th></tr></thead><tbody>
 {{range .Monitors}}<tr>
   <td>{{.ID}}</td><td>{{.Name}}</td><td><code>{{.URL}}</code></td>
   <td>{{if .IntervalSeconds}}{{.IntervalSeconds}}{{else}}30{{end}}s</td>
   <td>{{if .TimeoutSeconds}}{{.TimeoutSeconds}}{{else}}5{{end}}s</td>
   <td class="rowact"><form class="inline" method="post" action="/admin/monitor/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="id" value="{{.ID}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form></td></tr>
 {{else}}<tr><td colspan="6" class="empty">{{T "admin.mon.no_custom"}}</td></tr>{{end}}
 </tbody></table>
 <div class="card addcard" style="margin-top:1rem"><h3>{{T "admin.mon.add"}}</h3>
  <form method="post" action="/admin/monitor/add"><div class="formgrid">
   <div><label>{{T "admin.f.id"}}</label><input name="id" required></div>
   <div><label>{{T "admin.f.name"}}</label><input name="name"></div>
   <div><label>{{T "admin.mon.health_url"}}</label><input name="url" placeholder="https://host/health" required></div>
   <div><label>{{T "admin.mon.interval_s"}}</label><input name="interval" value="30"></div>
   <div><label>{{T "admin.mon.timeout_s"}}</label><input name="timeout" value="5"></div>
   <div><button class="btn primary">{{T "btn.add"}}</button></div>
  </div></form></div>
</section>`)

var AdminEditTmpl = xtkui.LocParse("adminedit", `<h1>{{T "admin.edit.h1"}} «{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}}»</h1>
{{if .Managed}}<div class="hint" style="border:1px solid var(--line);border-radius:9px;padding:.7rem .9rem;margin:.3rem 0 1rem;display:flex;gap:.8rem;align-items:center;flex-wrap:wrap">🏠 <span>{{T "admin.edit.managed"}}</span> <a class="btn sm" href="/admin/hosting">{{T "admin.edit.gotohosting"}} →</a></div>{{end}}
 <div class="card">
  <form method="post" action="/admin/backend/edit" enctype="multipart/form-data">
   <input type="hidden" name="id" value="{{.ID}}">
   <div class="xtk-tabs" role="tablist">
    <button type="button" class="xtk-tab on" data-p="tg">{{T "admin.tab.general"}}</button>
    <button type="button" class="xtk-tab" data-p="ta">{{T "admin.tab.access"}}</button>
    <button type="button" class="xtk-tab" data-p="tn">{{T "admin.tab.nginx"}}</button>
    <button type="button" class="xtk-tab" data-p="ts">{{T "admin.tab.security"}}</button>
   </div>
   <div class="xtk-pane on" id="tg">
   <table class="ftable"><tbody>
    <tr><th>{{T "admin.f.id"}}</th><td><input value="{{.ID}}" disabled></td><td class="fhelp">{{T "admin.edit.help.id"}}</td></tr>
    <tr><th>{{T "admin.f.name"}}</th><td><input name="name" value="{{.Name}}"></td><td class="fhelp">{{T "admin.edit.help.name"}}</td></tr>
    <tr><th>{{T "admin.f.host"}}</th><td><input name="host" value="{{.Host}}" required></td><td class="fhelp">{{T "admin.edit.help.host"}}</td></tr>
    <tr><th>{{T "admin.f.url"}}</th><td><input name="url" value="{{.URL}}"></td><td class="fhelp">{{T "admin.edit.help.url"}}</td></tr>
    <tr><th>www</th><td><label class="hint" style="display:inline-flex;align-items:center;gap:.35rem"><input type="checkbox" name="www" value="1"{{if .WWW}} checked{{end}}> also serve/cert <code>www.{{.Host}}</code></label></td><td class="fhelp">Adds www.&lt;host&gt; to the vhost server_name and (on issue) the certificate SAN.</td></tr>
    <tr><th>{{T "admin.f.path"}}</th><td><input name="path" value="{{.Path}}"></td><td class="fhelp">{{T "admin.edit.help.path"}}</td></tr>
    <tr><th>{{T "admin.f.upstream"}}</th><td><input name="upstream" value="{{.Upstream}}"{{if .Managed}} readonly{{else}} required{{end}}></td><td class="fhelp">{{if .Managed}}{{T "admin.edit.upstream.managed"}}{{else}}{{T "admin.edit.help.upstream"}}{{end}}</td></tr>
    <tr><th>{{T "admin.f.desc"}}</th><td colspan="2"><textarea name="description" rows="3" style="width:100%;font-family:inherit" placeholder="Markdown: **grassetto**, [link](https://…), elenchi — reso e sanitizzato sulla card">{{.Description}}</textarea></td></tr>
    <tr><th>Listing</th><td colspan="2"><label class="hint" style="display:inline-flex;align-items:center;gap:.35rem"><input type="checkbox" name="listed" value="1"{{if not .Unlisted}} checked{{end}}> Esponi nel listing pubblico dei servizi</label></td></tr>
    <tr><th>Immagine</th><td colspan="2">{{if .Image}}<img src="/listing/img/{{.ID}}" alt="" style="max-height:80px;border-radius:6px;display:block;margin-bottom:.4rem"><label class="hint" style="display:inline-flex;gap:.35rem;margin-bottom:.4rem"><input type="checkbox" name="img_remove" value="1"> rimuovi immagine</label><br>{{end}}<input type="file" name="image" accept="image/png,image/jpeg,image/webp,image/gif"><div class="fhelp">Anteprima sulla card del listing (PNG/JPEG/WebP/GIF, max 2 MB).</div></td></tr>
   </tbody></table></div>

   <div class="xtk-pane" id="ta">
    <table class="ftable"><tbody>
    <tr><th>{{T "admin.f.rule"}}</th><td><select name="rule">
     {{ruleOptions .Rule}}</select></td><td></td></tr>
    </tbody></table>
    <div class="rule-help">
     <p><b>{{T "admin.rule.public"}}</b> — {{T "admin.rule.public.d"}}</p>
     <p><b>{{T "admin.rule.authenticated"}}</b> — {{T "admin.rule.authenticated.d"}}</p>
     <p><b>{{T "admin.rule.authorized"}}</b> — {{T "admin.rule.authorized.d"}}</p>
     <p class="hint">{{T "admin.rule.note"}}</p>
    </div>
    <div id="svcusers" class="svcusers">
     <h3>{{T "admin.svcusers.h"}}</h3>
     <p class="hint">{{T "admin.svcusers.hint"}}</p>
     <p class="svcusers-off-note hint">{{T "admin.svcusers.inactive"}}</p>
     <input type="hidden" name="svcuser_sent" value="1">
     <div class="svcuser-list">
      {{$any := false}}{{range .Users}}{{if or .Enabled .Admin}}{{$any = true}}<label class="check svcuser" data-e="{{.Email}}"><input type="checkbox" name="svcuser" value="{{.Email}}"{{if .Enabled}} checked{{end}}{{if .Admin}} disabled{{end}}> {{.Email}}{{if .Admin}} <span class="tag ro">admin</span>{{end}}</label>{{end}}{{end}}
      {{if not $any}}<p class="hint svcuser-empty">{{T "admin.svcusers.none"}}</p>{{end}}
     </div>
     <details class="svcuser-add">
      <summary>+ {{T "admin.svcusers.add"}}</summary>
      <input type="search" class="svcuser-filter" placeholder="{{T "admin.svcusers.filter"}}" oninput="xtkFilterUsers(this)">
      <div class="svcuser-list">
       {{$more := false}}{{range .Users}}{{if and (not .Enabled) (not .Admin)}}{{$more = true}}<label class="check svcuser" data-e="{{.Email}}"><input type="checkbox" name="svcuser" value="{{.Email}}"> {{.Email}}</label>{{end}}{{end}}
       {{if not $more}}<p class="hint">{{T "admin.svcusers.allin"}}</p>{{end}}
      </div>
     </details>
    </div>

   <h3 style="margin-top:1.3rem">{{T "admin.routes.h"}}</h3>
   <p class="hint">{{T "admin.routes.hint"}}</p>
   <table class="ftable rtable" id="xtk-routes"><tbody>
    <tr class="rhead"><th>{{T "admin.routes.path"}}</th><th>{{T "admin.routes.match"}}</th><th>{{T "admin.f.rule"}}</th><th>{{T "admin.routes.users"}}</th><th></th></tr>
    {{range .Overrides}}<tr class="rrow">
     <td><input name="opath" value="{{.Path}}" placeholder="/wp-login.php"></td>
     <td><select name="omatch"><option value="prefix"{{if not .Exact}} selected{{end}}>{{T "admin.routes.prefix"}}</option><option value="exact"{{if .Exact}} selected{{end}}>{{T "admin.routes.exact"}}</option></select></td>
     <td><select name="orule">{{ruleOptions .Rule}}</select></td>
     <td class="rusers">
      <select name="oinherit" class="oinherit"><option value="1"{{if not .OwnGrants}} selected{{end}}>{{T "admin.routes.inherit"}}</option><option value="0"{{if .OwnGrants}} selected{{end}}>{{T "admin.routes.own"}}</option></select>
      <input type="hidden" name="ousers" value="{{.Users}}">
      <details class="rusers-pick"{{if not .OwnGrants}} hidden{{end}}>
       <summary>{{T "admin.routes.pickusers"}}</summary>
       <div class="svcuser-list">{{range $.Users}}<label class="check svcuser"><input type="checkbox" class="ouser"{{if .Admin}} disabled{{end}}> {{.Email}}{{if .Admin}} <span class="tag ro">admin</span>{{end}}</label>{{end}}</div>
      </details>
     </td>
     <td><button type="button" class="btn sm" onclick="this.closest('tr').remove()">✕</button></td></tr>{{end}}
   </tbody></table>
   <p><button type="button" class="btn sm" onclick="xtkAddRoute()">+ {{T "admin.routes.add"}}</button></p>
   <p class="hint">{{T "admin.routes.note"}}</p>
   <template id="xtk-rtmpl"><tr class="rrow">
     <td><input name="opath" placeholder="/wp-login.php"></td>
     <td><select name="omatch"><option value="prefix">{{T "admin.routes.prefix"}}</option><option value="exact">{{T "admin.routes.exact"}}</option></select></td>
     <td><select name="orule">{{ruleOptions "authenticated"}}</select></td>
     <td class="rusers">
      <select name="oinherit" class="oinherit"><option value="1" selected>{{T "admin.routes.inherit"}}</option><option value="0">{{T "admin.routes.own"}}</option></select>
      <input type="hidden" name="ousers" value="">
      <details class="rusers-pick" hidden>
       <summary>{{T "admin.routes.pickusers"}}</summary>
       <div class="svcuser-list">{{range $.Users}}<label class="check svcuser"><input type="checkbox" class="ouser"{{if .Admin}} disabled{{end}}> {{.Email}}{{if .Admin}} <span class="tag ro">admin</span>{{end}}</label>{{end}}</div>
      </details>
     </td>
     <td><button type="button" class="btn sm" onclick="this.closest('tr').remove()">✕</button></td></tr></template>
   <script>
function xtkAddRoute(){var t=document.getElementById('xtk-rtmpl');document.querySelector('#xtk-routes tbody').appendChild(t.content.cloneNode(true));xtkWireRoutes();}
function xtkWireRoutes(){
 document.querySelectorAll('#xtk-routes .rrow').forEach(function(tr){
  if(tr.dataset.wired) return; tr.dataset.wired='1';
  var sel=tr.querySelector('.oinherit'), pick=tr.querySelector('.rusers-pick'), hid=tr.querySelector('input[name="ousers"]');
  if(!sel||!pick||!hid) return;
  var have=(hid.value||'').split(/\s+/).filter(Boolean);
  pick.querySelectorAll('.ouser').forEach(function(cb){
   var em=cb.parentElement.textContent.trim().split(/\s+/)[0];
   cb.dataset.email=em; cb.checked=have.indexOf(em)>=0;
   cb.addEventListener('change',function(){
    var out=[]; pick.querySelectorAll('.ouser').forEach(function(x){if(x.checked)out.push(x.dataset.email);});
    hid.value=out.join(' ');
   });
  });
  function sync(){ pick.hidden = (sel.value!=='0'); }
  sel.addEventListener('change',sync); sync();
 });
}
xtkWireRoutes();
</script>
   </div>

   <div class="xtk-pane" id="tn">
   <h3 style="margin-top:1.3rem">{{T "admin.nginx.h"}}</h3>
   <p class="hint">{{T "admin.nginx.hint"}}</p>
   <table class="ftable"><tbody>
    <tr><th>{{T "admin.nginx.timeout"}}</th><td><input type="number" name="ngx_timeout" value="{{.NgxTimeout}}" min="0" placeholder="60"></td><td class="fhelp">{{T "admin.nginx.timeout.help"}}</td></tr>
    <tr><th>{{T "admin.nginx.maxbody"}}</th><td><input type="number" name="ngx_maxbody" value="{{.NgxMaxBody}}" min="0" placeholder="1"></td><td class="fhelp">{{T "admin.nginx.maxbody.help"}}</td></tr>
    <tr><th>{{T "admin.nginx.websocket"}}</th><td><input type="checkbox" name="ngx_ws"{{if .NgxWS}} checked{{end}}></td><td class="fhelp">{{T "admin.nginx.websocket.help"}}</td></tr>
    <tr><th>{{T "admin.nginx.nobuffer"}}</th><td><input type="checkbox" name="ngx_nobuf"{{if .NgxNoBuf}} checked{{end}}></td><td class="fhelp">{{T "admin.nginx.nobuffer.help"}}</td></tr>
    <tr><th>{{T "admin.nginx.selfsigned"}}</th><td><input type="checkbox" name="ngx_selfsigned"{{if .NgxSelfSigned}} checked{{end}}></td><td class="fhelp">{{T "admin.nginx.selfsigned.help"}}</td></tr>
    <tr><th>{{T "admin.nginx.custom_loc"}}</th><td colspan="2"><textarea name="ngx_custom_loc" rows="3" placeholder="proxy_set_header X-Foo bar;">{{.NgxCustomLoc}}</textarea></td></tr>
    <tr><th>{{T "admin.nginx.custom_srv"}}</th><td colspan="2"><textarea name="ngx_custom_srv" rows="2">{{.NgxCustomSrv}}</textarea></td></tr>
   </tbody></table>
   <p class="hint">{{T "admin.nginx.custom.help"}}</p>
   </div>

   <div class="xtk-pane" id="ts">
    <table class="ftable"><tbody>
    <tr><th>{{T "admin.col.ipallow"}}</th><td><input name="ip_allow" value="{{.IPAllow}}" placeholder="203.0.113.0/24 10.0.0.5"></td><td class="fhelp">{{T "admin.edit.help.ipallow"}}</td></tr>
    </tbody></table>
   <h3 style="margin-top:1.3rem">{{T "admin.waf.h"}}</h3>
   <p class="hint">{{T "admin.waf.hint"}}</p>
   <table class="ftable"><tbody>
    <tr><th>{{T "admin.waf.enable"}}</th><td><label class="hint" style="display:inline-flex;align-items:center;gap:.35rem"><input type="checkbox" name="waf_enabled" value="1"{{if .WafEnabled}} checked{{end}}> {{T "admin.waf.enable.on"}}</label></td><td class="fhelp">{{T "admin.waf.help"}}</td></tr>
    <tr><th>{{T "admin.waf.mode"}}</th><td><select name="waf_mode">
     <option value="detect"{{if ne .WafMode "block"}} selected{{end}}>{{T "admin.waf.detect"}}</option>
     <option value="block"{{if eq .WafMode "block"}} selected{{end}}>{{T "admin.waf.block"}}</option></select></td><td class="fhelp">{{T "admin.waf.mode.help"}}</td></tr>
    <tr><th>{{T "admin.waf.rules"}}</th><td><input name="waf_disabled_rules" value="{{.WafDisabledRules}}" placeholder="942100 941110"></td><td class="fhelp">{{T "admin.waf.rules.help"}}</td></tr>
    <tr><th>{{T "admin.waf.ignoreips"}}</th><td><input name="waf_ignore_ips" value="{{.WafIgnoreIPs}}" placeholder="1.2.3.4 10.0.0.0/24"></td><td class="fhelp">{{T "admin.waf.ignoreips.help"}}</td></tr>
    <tr><th>{{T "admin.waf.custom"}}</th><td colspan="2"><textarea name="waf_custom_rules" rows="3" placeholder='SecRule REQUEST_URI "@beginsWith /api/upload" "id:9009500,phase:1,pass,nolog,ctl:ruleEngine=Off"'>{{.WafCustomRules}}</textarea></td></tr>
   </tbody></table>
   <p class="hint">{{T "admin.waf.custom.help"}}</p>
   <div class="actions" style="margin-top:1rem">
    <button class="btn primary">{{T "btn.save"}}</button><a class="btn" href="/admin/tls#h-{{.Host}}">{{T "admin.tls.manage"}}</a><a class="btn" href="/admin/servizi">{{T "admin.cancel"}}</a></div>
  </div>
  </form>
<script>
(function(){
 document.querySelectorAll('.xtk-tab').forEach(function(t){
  t.addEventListener('click',function(){
   document.querySelectorAll('.xtk-tab').forEach(function(x){x.classList.remove('on')});
   document.querySelectorAll('.xtk-pane').forEach(function(x){x.classList.remove('on')});
   t.classList.add('on'); document.getElementById(t.dataset.p).classList.add('on');
  });
 });
 var sel=document.querySelector('select[name="rule"]'), box=document.getElementById('svcusers');
 // Non si nasconde: resta visibile e modificabile anche quando la regola non la usa,
 // così cambiando regola non tocca ri-abilitare tutti a mano (e si vede chi entrerebbe).
 function sync(){ if(box&&sel) box.classList.toggle('inactive', sel.value!=='authorized'); }
 if(sel){ sel.addEventListener('change',sync); sync(); }
 window.xtkFilterUsers=function(i){var q=i.value.toLowerCase();
  i.closest('.svcusers').querySelectorAll('.svcuser').forEach(function(l){
   l.style.display = l.dataset.e.toLowerCase().indexOf(q)>=0 ? '' : 'none';});};
})();
</script>

 </div>`)

var AdminQRTmpl = xtkui.LocParse("adminqr", `<h1>{{T "admin.qr.title"}} {{.Email}}</h1>
 <div class="card qr" style="text-align:center">
  <p class="hint">{{T "admin.qr.hint"}}</p>
  <p><img src="{{.QR}}" alt="QR otpauth" width="240" height="240"></p>
  <p>{{T "qr.key"}}: <code>{{.Secret}}</code></p>
 </div>
 <p style="margin-top:1rem"><a class="btn" href="/admin/utenti">← {{T "admin.qr.back"}}</a></p>`)

var HostScanTmpl = xtkui.LocParse("hostscan", `<h1>{{T "admin.hs.h1"}} ({{.From}}–{{.To}})</h1>
 <p class="hint">{{T "admin.hs.hint"}}</p>
 {{if .Capped}}<p class="hint" style="border-left:3px solid var(--line);padding-left:.6rem">{{T "admin.hs.capped"}}</p>{{end}}
 <form method="post" action="/admin/hostscan/add">
  <table><thead><tr>
   <th><input type="checkbox" onclick="for(const c of document.querySelectorAll('input[name=ports]'))c.checked=this.checked"></th>
   <th>{{T "admin.dk.port"}}</th><th>{{T "admin.hs.vhost_name"}}</th><th>{{T "admin.mon.state"}}</th></tr></thead><tbody>
  {{range .Ports}}<tr>
   <td>{{if not .Added}}<input type="checkbox" name="ports" value="{{.Port}}">{{end}}</td>
   <td>{{.Port}}</td>
   <td>{{if .Added}}<span class="tag ro">{{T "admin.hs.already"}} {{.ExistingHost}}</span>{{else}}<input name="name_{{.Port}}" placeholder="host-{{.Port}}">{{end}}</td>
   <td>{{if .Added}}—{{else}}{{T "admin.hs.new"}}{{end}}</td></tr>
  {{else}}<tr><td colspan="4" class="empty">{{T "admin.hs.none"}}</td></tr>{{end}}
  </tbody></table>
  <div class="actions" style="margin-top:.8rem">
   <label>{{T "admin.f.rule"}} <select name="rule">{{ruleOptions "authorized"}}</select></label>
   <button class="btn primary">{{T "admin.hs.add_selected"}}</button>
  </div>
 </form>
 <p style="margin-top:1rem"><a class="btn" href="/admin">← {{T "admin.hs.back"}}</a></p>`)
