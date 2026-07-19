// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"xaltorka/auth"
	"xaltorka/config"
	"xaltorka/dockerscan"
	"xaltorka/health"
	"xaltorka/i18n"
	"xaltorka/models"
	"xaltorka/version"
	"xaltorka/xtkui"
)

// Admin panel (BLUEPRINT §9). IP-whitelisted. Manages the runtime services
// (services.json: extra backends + links) and the users (users.json), with
// atomic persistence + snapshot + reload. The config.json backends are
// read-only (infrastructure, env-templated).

// renderAdminPage writes the shared chrome (head + topbar + container) around a
// page-specific content template, via the shared UI kit. The top nav (incl. the
// optional Hosting entry) is the shared xtkui.AdminNav, so the core and the hosting
// extension render an identical main menu.
func (s *Server) renderAdminPage(w http.ResponseWriter, r *http.Request, active string, t *template.Template, data any) {
	nav := xtkui.AdminNav(s.HostingUpstream != "")
	c := xtkui.Chrome{
		Title: "Xal-Tor-Ka · Admin", BrandText: "⛬ Xal-Tor-Ka", BrandHref: "/admin",
		SubtitleKey: "admin.subtitle", Version: version.Version,
		Nav: nav, Active: active,
		DashboardHref: "/listing", DashboardKey: "nav.dashboard", LoggedIn: true,
	}
	c.Render(w, s.lang(r), t, data)
}

var overviewTmpl = xtkui.LocParse("ov", `<h1>{{T "admin.title"}}</h1>
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

var servicesTmpl = xtkui.LocParse("services", `<section>
 <h2>{{T "admin.svc.h2"}}</h2>
 <p class="hint">{{T "admin.svc.hint"}}</p>
 <table><thead><tr><th>{{T "admin.col.service"}}</th><th>{{T "admin.col.host"}} / {{T "admin.col.upstream"}}</th><th>{{T "admin.col.rule"}}</th><th>{{T "admin.col.ipallow"}}</th><th></th></tr></thead><tbody>
 {{range .ConfigBackends}}<tr><td>{{.ID}} <span class="tag ro">config</span></td>
   <td><a href="//{{.Host}}" target="_blank" rel="noopener"><code>{{.Host}}</code></a>{{range .Routes}}<div class="hint"><code>{{.Upstream}}</code></div>{{end}}</td>
   <td>{{range .Routes}}<span class="tag">{{.Rule}}</span> {{end}}</td>
   <td></td><td class="rowact"><a class="btn sm" href="/admin/tls#h-{{.Host}}">{{T "admin.tls.manage"}}</a></td></tr>{{end}}
 {{range .ServiceBackends}}<tr{{if .Disabled}} class="off"{{end}}>
   <td><b>{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}}</b>{{if .Disabled}} <span class="tag ro">off</span>{{end}}{{if .Description}}<div class="hint">{{.Description}}</div>{{end}}</td>
   <td><a href="//{{.Host}}" target="_blank" rel="noopener"><code>{{.Host}}</code></a>{{range .Routes}}<div class="hint"><code>{{.Upstream}}</code></div>{{end}}</td>
   <td>{{range .Routes}}<span class="tag">{{.Rule}}</span> {{end}}</td>
   <td>{{if .IPAllow}}🔒 <code>{{index .IPAllow 0}}</code>{{if gt (len .IPAllow) 1}} <span class="hint" title="{{len .IPAllow}} IP">…</span>{{end}}{{end}}</td>
   <td class="rowact">
    <a class="btn sm" href="/admin/backend/edit?id={{.ID}}">{{T "admin.act.edit"}}</a>
    <a class="btn sm" href="/admin/tls#h-{{.Host}}">{{T "admin.tls.manage"}}</a>
    <form class="inline" method="post" action="/admin/backend/toggle"><input type="hidden" name="id" value="{{.ID}}"><button class="btn sm">{{if .Disabled}}{{T "admin.act.enable"}}{{else}}{{T "admin.act.disable"}}{{end}}</button></form>
    <form class="inline" method="post" action="/admin/backend/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="id" value="{{.ID}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>
   </td></tr>
 {{else}}{{end}}
 </tbody></table>
 <div class="card addcard" style="margin-top:1rem"><h3>{{T "admin.svc.add"}}</h3>
  <form method="post" action="/admin/backend/add"><div class="formgrid">
   <div><label>{{T "admin.f.id"}}</label><input name="id" required></div>
   <div><label>{{T "admin.f.name"}}</label><input name="name"></div>
   <div><label>{{T "admin.f.host"}}</label><input name="host" placeholder="app.example.com" required></div>
   <div><label>{{T "admin.f.path"}}</label><input name="path" value="/"></div>
   <div><label>{{T "admin.f.rule"}}</label><select name="rule"><option>whitelist</option><option>authenticated</option><option>public</option></select></div>
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

var dockerTmpl = xtkui.LocParse("docker", `<section>
 <h2>{{T "admin.dk.h2"}}</h2>
 {{if .DockerEnabled}}
  <p class="hint">{{T "admin.dk.hint"}}</p>
  <table><thead><tr><th>{{T "admin.dk.container"}}</th><th>{{T "admin.dk.port"}}</th><th>{{T "admin.dk.vhost"}}</th><th></th></tr></thead><tbody>
  {{range .Discovered}}<tr><td>{{.Name}}</td><td>{{.Port}}</td><td><code>{{.Host}}</code></td>
   <td>{{if .Added}}<span class="tag ro">{{T "admin.dk.added"}}</span>{{else}}<form class="inline" method="post" action="/admin/discover/add">
    <input type="hidden" name="name" value="{{.Name}}"><input type="hidden" name="port" value="{{.Port}}">
    <select name="rule"><option>whitelist</option><option>authenticated</option><option>public</option></select>
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

var usersTmpl = xtkui.LocParse("users", `<section>
 <h2>{{T "admin.users"}}</h2>
 <table><thead><tr><th>{{T "admin.usr.email"}}</th><th></th><th>{{T "admin.usr.enabled_hosts"}}</th><th></th></tr></thead><tbody>
 {{range .Users}}<tr>
  <td><a href="/admin/utenti/{{.Email}}">{{.Email}}</a></td>
  <td>{{if .Admin}}<span class="tag">admin</span>{{end}}</td>
  <td>{{if .Admin}}<span class="hint">{{T "admin.usr.all_admin"}}</span>{{else if .Hosts}}<details><summary>{{len .Hosts}} host</summary><ul class="hostlist">{{range .Hosts}}<li>{{.}}</li>{{end}}</ul></details>{{else}}<span class="hint">{{T "admin.usr.none"}}</span>{{end}}</td>
  <td><div class="actions">
   <a class="btn sm" href="/admin/utenti/{{.Email}}">{{T "admin.usr.properties"}}</a>
   <form class="inline" method="post" action="/admin/user/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="email" value="{{.Email}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>
  </div></td></tr>{{end}}
 </tbody></table>
 <div class="card addcard" style="margin-top:1rem"><h3>{{T "admin.usr.create"}}</h3>
  <form method="post" action="/admin/user/add">
   <div class="formgrid">
    <div><label>{{T "admin.usr.email"}}</label><input type="email" name="email" required></div>
    <div><label>{{T "field.password"}}</label><input type="password" name="password" required></div>
   </div>
   <div class="checks"><label>{{T "admin.usr.authz"}}</label>{{range .AllIDs}}<label class="check"><input type="checkbox" name="authz" value="{{.}}">{{.}}</label>{{end}}</div>
   <div class="actions"><button class="btn primary">{{T "admin.usr.create_btn"}}</button></div>
  </form></div>
</section>`)

var userDetailTmpl = xtkui.LocParse("userdetail", `<section>
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
  {{if .Admin}}<p class="hint">{{T "admin.usr.admin_all_note"}}</p>
  {{else}}<form method="post" action="/admin/user/authz"><input type="hidden" name="email" value="{{.Email}}">
   <div class="checks">{{range .AllIDs}}<label class="check"><input type="checkbox" name="authz" value="{{.}}" {{if index $.Checked .}}checked{{end}}>{{.}}</label>{{else}}<span class="hint">{{T "admin.usr.no_services"}}</span>{{end}}</div>
   <div class="actions" style="margin-top:.6rem"><button class="btn primary">{{T "admin.usr.save_authz"}}</button></div>
  </form>{{end}}
 </div>
</section>`)

var monitoringTmpl = xtkui.LocParse("mon", `<section>
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

var adminEditTmpl = xtkui.LocParse("adminedit", `<h1>{{T "admin.edit.h1"}} «{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}}»</h1>
{{if .Managed}}<div class="hint" style="border:1px solid var(--line);border-radius:9px;padding:.7rem .9rem;margin:.3rem 0 1rem;display:flex;gap:.8rem;align-items:center;flex-wrap:wrap">🏠 <span>{{T "admin.edit.managed"}}</span> <a class="btn sm" href="/admin/hosting">{{T "admin.edit.gotohosting"}} →</a></div>{{end}}
 <div class="card">
  <form method="post" action="/admin/backend/edit">
   <input type="hidden" name="id" value="{{.ID}}">
   <table class="ftable"><tbody>
    <tr><th>{{T "admin.f.id"}}</th><td><input value="{{.ID}}" disabled></td><td class="fhelp">{{T "admin.edit.help.id"}}</td></tr>
    <tr><th>{{T "admin.f.name"}}</th><td><input name="name" value="{{.Name}}"></td><td class="fhelp">{{T "admin.edit.help.name"}}</td></tr>
    <tr><th>{{T "admin.f.host"}}</th><td><input name="host" value="{{.Host}}" required></td><td class="fhelp">{{T "admin.edit.help.host"}}</td></tr>
    <tr><th>{{T "admin.f.url"}}</th><td><input name="url" value="{{.URL}}"></td><td class="fhelp">{{T "admin.edit.help.url"}}</td></tr>
    <tr><th>www</th><td><label class="hint" style="display:inline-flex;align-items:center;gap:.35rem"><input type="checkbox" name="www" value="1"{{if .WWW}} checked{{end}}> also serve/cert <code>www.{{.Host}}</code></label></td><td class="fhelp">Adds www.&lt;host&gt; to the vhost server_name and (on issue) the certificate SAN.</td></tr>
    <tr><th>{{T "admin.f.path"}}</th><td><input name="path" value="{{.Path}}"></td><td class="fhelp">{{T "admin.edit.help.path"}}</td></tr>
    <tr><th>{{T "admin.f.rule"}}</th><td><select name="rule">
     <option {{if eq .Rule "whitelist"}}selected{{end}}>whitelist</option>
     <option {{if eq .Rule "authenticated"}}selected{{end}}>authenticated</option>
     <option {{if eq .Rule "public"}}selected{{end}}>public</option></select></td><td class="fhelp">{{T "admin.rule.help"}}</td></tr>
    <tr><th>{{T "admin.f.upstream"}}</th><td><input name="upstream" value="{{.Upstream}}"{{if .Managed}} readonly{{else}} required{{end}}></td><td class="fhelp">{{if .Managed}}{{T "admin.edit.upstream.managed"}}{{else}}{{T "admin.edit.help.upstream"}}{{end}}</td></tr>
    <tr><th>{{T "admin.f.desc"}}</th><td colspan="2"><input name="description" value="{{.Description}}" placeholder="{{T "admin.edit.help.desc"}}"></td></tr>
    <tr><th>{{T "admin.col.ipallow"}}</th><td><input name="ip_allow" value="{{.IPAllow}}" placeholder="203.0.113.0/24 10.0.0.5"></td><td class="fhelp">{{T "admin.edit.help.ipallow"}}</td></tr>
   </tbody></table>
   <h3 style="margin-top:1.3rem">{{T "admin.routes.h"}}</h3>
   <p class="hint">{{T "admin.routes.hint"}}</p>
   <table class="ftable rtable" id="xtk-routes"><tbody>
    <tr class="rhead"><th>{{T "admin.routes.path"}}</th><th>{{T "admin.routes.match"}}</th><th>{{T "admin.f.rule"}}</th><th></th></tr>
    {{range .Overrides}}<tr class="rrow">
     <td><input name="opath" value="{{.Path}}" placeholder="/wp-login.php"></td>
     <td><select name="omatch"><option value="prefix"{{if not .Exact}} selected{{end}}>{{T "admin.routes.prefix"}}</option><option value="exact"{{if .Exact}} selected{{end}}>{{T "admin.routes.exact"}}</option></select></td>
     <td><select name="orule"><option{{if eq .Rule "authenticated"}} selected{{end}}>authenticated</option><option{{if eq .Rule "whitelist"}} selected{{end}}>whitelist</option><option{{if eq .Rule "public"}} selected{{end}}>public</option></select></td>
     <td><button type="button" class="btn sm" onclick="this.closest('tr').remove()">✕</button></td></tr>{{end}}
   </tbody></table>
   <p><button type="button" class="btn sm" onclick="xtkAddRoute()">+ {{T "admin.routes.add"}}</button></p>
   <p class="hint">{{T "admin.routes.note"}}</p>
   <template id="xtk-rtmpl"><tr class="rrow">
     <td><input name="opath" placeholder="/wp-login.php"></td>
     <td><select name="omatch"><option value="prefix">{{T "admin.routes.prefix"}}</option><option value="exact">{{T "admin.routes.exact"}}</option></select></td>
     <td><select name="orule"><option>authenticated</option><option>whitelist</option><option>public</option></select></td>
     <td><button type="button" class="btn sm" onclick="this.closest('tr').remove()">✕</button></td></tr></template>
   <script>function xtkAddRoute(){var t=document.getElementById('xtk-rtmpl');document.querySelector('#xtk-routes tbody').appendChild(t.content.cloneNode(true));}</script>
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
   <h3 style="margin-top:1.3rem">{{T "admin.waf.h"}}</h3>
   <p class="hint">{{T "admin.waf.hint"}}</p>
   <table class="ftable"><tbody>
    <tr><th>{{T "admin.waf.enable"}}</th><td><label class="hint" style="display:inline-flex;align-items:center;gap:.35rem"><input type="checkbox" name="waf_enabled" value="1"{{if .WafEnabled}} checked{{end}}> {{T "admin.waf.enable.on"}}</label></td><td class="fhelp">{{T "admin.waf.help"}}</td></tr>
    <tr><th>{{T "admin.waf.mode"}}</th><td><select name="waf_mode">
     <option value="detect"{{if ne .WafMode "block"}} selected{{end}}>{{T "admin.waf.detect"}}</option>
     <option value="block"{{if eq .WafMode "block"}} selected{{end}}>{{T "admin.waf.block"}}</option></select></td><td class="fhelp">{{T "admin.waf.mode.help"}}</td></tr>
   </tbody></table>
   <div class="actions" style="margin-top:1rem">
    <button class="btn primary">{{T "btn.save"}}</button><a class="btn" href="/admin/tls#h-{{.Host}}">{{T "admin.tls.manage"}}</a><a class="btn" href="/admin/servizi">{{T "admin.cancel"}}</a></div>
  </form>
 </div>`)

var adminQRTmpl = xtkui.LocParse("adminqr", `<h1>{{T "admin.qr.title"}} {{.Email}}</h1>
 <div class="card qr" style="text-align:center">
  <p class="hint">{{T "admin.qr.hint"}}</p>
  <p><img src="{{.QR}}" alt="QR otpauth" width="240" height="240"></p>
  <p>{{T "qr.key"}}: <code>{{.Secret}}</code></p>
 </div>
 <p style="margin-top:1rem"><a class="btn" href="/admin/utenti">← {{T "admin.qr.back"}}</a></p>`)

// handleAdmin is the overview page with summary tiles linking to the sections.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	up, down := 0, 0
	if s.Health != nil {
		for _, st := range s.Health.Snapshot() {
			if st.State == health.StateUp {
				up++
			} else {
				down++
			}
		}
	}
	eff := s.effectiveAdminIPs()
	source := "config.json / ADMIN_CIDR"
	if len(svc.AdminIPWhitelist) > 0 {
		source = "services.json (override runtime)"
	}
	clientIPStr := ""
	if ip := clientIP(r, s.Cfg.Server.TrustedProxies); ip != nil {
		clientIPStr = ip.String()
	}
	s.renderAdminPage(w, r, "", overviewTmpl, struct {
		Services, Links, Users, ConfigBackends, Up, Down int
		AdminIPsSource, ClientIP, AdminIPsRaw            string
	}{
		len(svc.Backends), len(svc.Links), s.Users.Count(), len(s.BaseBackends), up, down,
		source, clientIPStr, strings.Join(eff, " "),
	})
}

func (s *Server) handleAdminServices(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	s.renderAdminPage(w, r, "servizi", servicesTmpl, struct {
		ConfigBackends  []models.Backend
		ServiceBackends []models.Backend
		Links           []models.Link
		HostingEnabled  bool
	}{s.BaseBackends, svc.Backends, svc.Links, s.HostingUpstream != ""})
}

func (s *Server) handleAdminDocker(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	var ports []portRow
	for _, b := range svc.Backends {
		for _, rt := range b.Routes {
			if p := hostInternalPort(rt.Upstream); p > 0 {
				ports = append(ports, portRow{Host: b.Host, Port: p, Rule: rt.Rule, Disabled: b.Disabled})
			}
		}
	}
	s.renderAdminPage(w, r, "docker", dockerTmpl, struct {
		DockerEnabled bool
		Discovered    []discoveredRow
		Ports         []portRow
	}{s.DockerProxyURL != "", s.discover(r, svc), ports})
}

// portRow is one entry of the Docker "port map": a vhost routed to a host port.
type portRow struct {
	Host     string
	Port     int
	Rule     string
	Disabled bool
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	users := s.Users.All()
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	s.renderAdminPage(w, r, "utenti", usersTmpl, struct {
		Users  []adminUserRow
		AllIDs []string
	}{rowsFor(users), s.allServiceIDs(svc)})
}

// handleAdminUserDetail is the per-user properties page.
func (s *Server) handleAdminUserDetail(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PathValue("email")
	u, found := s.Users.Get(email)
	if !found {
		http.Redirect(w, r, "/admin/utenti", http.StatusSeeOther)
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	checked := map[string]bool{}
	for _, b := range u.Backends {
		checked[b] = true
	}
	s.renderAdminPage(w, r, "utenti", userDetailTmpl, struct {
		Email, Provider string
		Admin           bool
		AllIDs          []string
		Checked         map[string]bool
	}{u.Email, u.Provider, u.Admin, s.allServiceIDs(svc), checked})
}

func (s *Server) handleAdminMonitoring(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	var monitoring []health.Status
	if s.Health != nil {
		monitoring = s.Health.Snapshot()
		sort.Slice(monitoring, func(i, j int) bool { return monitoring[i].BackendID < monitoring[j].BackendID })
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	s.renderAdminPage(w, r, "monitoring", monitoringTmpl, struct {
		Monitoring []health.Status
		Monitors   []models.Monitor
	}{monitoring, svc.Monitors})
}

func (s *Server) handleMonitorAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id, url := r.PostFormValue("id"), r.PostFormValue("url")
	if id == "" || url == "" {
		http.Error(w, i18n.T(s.lang(r), "err.id_url_required"), http.StatusBadRequest)
		return
	}
	iv, _ := strconv.Atoi(r.PostFormValue("interval"))
	if iv <= 0 {
		iv = 30
	}
	to, _ := strconv.Atoi(r.PostFormValue("timeout"))
	if to <= 0 {
		to = 5
	}
	err := s.mutateServices(func(svc *models.Services) error {
		for _, m := range svc.Monitors {
			if m.ID == id {
				return fmt.Errorf("id already exists")
			}
		}
		svc.Monitors = append(svc.Monitors, models.Monitor{
			ID: id, Name: r.PostFormValue("name"), URL: url,
			IntervalSeconds: iv, TimeoutSeconds: to,
		})
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleMonitorDel(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		out := svc.Monitors[:0]
		for _, m := range svc.Monitors {
			if m.ID != id {
				out = append(out, m)
			}
		}
		svc.Monitors = out
		return nil
	})
	s.afterMutation(w, r, err)
}

type discoveredRow struct {
	Name  string
	Port  int
	Host  string
	Added bool
}

// discover queries the docker-socket-proxy and proposes vhosts for running
// containers with published TCP ports (excluding our own stack). Best-effort:
// returns nil on any error or when disabled.
func (s *Server) discover(r *http.Request, svc models.Services) []discoveredRow {
	if s.DockerProxyURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	containers, err := dockerscan.List(ctx, s.DockerProxyURL)
	if err != nil {
		return nil
	}
	existing := map[string]bool{}
	for _, b := range s.BaseBackends {
		existing[b.Host] = true
	}
	for _, b := range svc.Backends {
		existing[b.Host] = true
	}

	seen := map[string]bool{}
	var out []discoveredRow
	for _, c := range containers {
		if excluded(c.Name, s.DockerExclude) {
			continue
		}
		name := sanitizeName(c.Name)
		if name == "" {
			continue
		}
		for _, p := range c.Ports {
			if p.Type != "tcp" || p.PublicPort == 0 {
				continue
			}
			host := name + ".localhost"
			key := fmt.Sprintf("%s:%d", host, p.PublicPort)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, discoveredRow{Name: name, Port: p.PublicPort, Host: host, Added: existing[host]})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// handleDiscoverAdd creates a service backend from a discovered container,
// routed via host.docker.internal:<published-port>.
func (s *Server) handleDiscoverAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	name := sanitizeName(r.PostFormValue("name"))
	port, _ := strconv.Atoi(r.PostFormValue("port"))
	rule := r.PostFormValue("rule")
	if name == "" || port <= 0 || port > 65535 {
		http.Error(w, i18n.T(s.lang(r), "err.invalid_container_port"), http.StatusBadRequest)
		return
	}
	if rule != "public" && rule != "authenticated" && rule != "whitelist" {
		rule = "whitelist"
	}
	host := name + ".localhost"
	upstream := fmt.Sprintf("http://host.docker.internal:%d", port)
	err := s.mutateServices(func(svc *models.Services) error {
		if s.idTaken(*svc, name) {
			return fmt.Errorf("id %q already exists", name)
		}
		svc.Backends = append(svc.Backends, models.Backend{
			ID: name, Name: name, Host: host, URL: "//" + host,
			Routes: []models.Route{{Path: "/", Rule: rule, Upstream: upstream}},
			Health: models.Health{URL: upstream + "/", IntervalSeconds: 30, TimeoutSeconds: 5},
		})
		return nil
	})
	s.afterMutation(w, r, err)
}

var hostScanTmpl = xtkui.LocParse("hostscan", `<h1>{{T "admin.hs.h1"}} ({{.From}}–{{.To}})</h1>
 <p class="hint">{{T "admin.hs.hint"}}</p>
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
   <label>{{T "admin.f.rule"}} <select name="rule"><option>whitelist</option><option>authenticated</option><option>public</option></select></label>
   <button class="btn primary">{{T "admin.hs.add_selected"}}</button>
  </div>
 </form>
 <p style="margin-top:1rem"><a class="btn" href="/admin">← {{T "admin.hs.back"}}</a></p>`)

type hostPortRow struct {
	Port         int
	Added        bool
	ExistingHost string
}

// handleHostScan scans host.docker.internal on the requested port range and
// lists open ports to be turned into vhosts.
func (s *Server) handleHostScan(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	to, _ := strconv.Atoi(r.URL.Query().Get("to"))
	if from <= 0 {
		from = 3000
	}
	if to <= 0 {
		to = from + 100
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	open := dockerscan.ScanPorts(ctx, "host.docker.internal", from, to)

	svc, _ := config.LoadServices(s.ServicesPath)
	byPort := map[int]string{}
	collect := func(bs []models.Backend) {
		for _, b := range bs {
			for _, rt := range b.Routes {
				if p := hostInternalPort(rt.Upstream); p > 0 {
					byPort[p] = b.Host
				}
			}
		}
	}
	collect(s.BaseBackends)
	collect(svc.Backends)

	rows := make([]hostPortRow, 0, len(open))
	for _, p := range open {
		h, added := byPort[p]
		rows = append(rows, hostPortRow{Port: p, Added: added, ExistingHost: h})
	}
	s.renderAdminPage(w, r, "servizi", hostScanTmpl, struct {
		From, To int
		Ports    []hostPortRow
	}{From: from, To: to, Ports: rows})
}

// handleHostScanAdd bulk-creates vhosts for the selected host ports.
func (s *Server) handleHostScanAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	rule := r.PostFormValue("rule")
	if rule != "public" && rule != "authenticated" && rule != "whitelist" {
		rule = "whitelist"
	}
	ports := r.PostForm["ports"]
	err := s.mutateServices(func(svc *models.Services) error {
		for _, ps := range ports {
			port, e := strconv.Atoi(ps)
			if e != nil || port < 1 || port > 65535 {
				continue
			}
			name := sanitizeName(r.PostFormValue("name_" + ps))
			if name == "" {
				name = fmt.Sprintf("host-%d", port)
			}
			if s.idTaken(*svc, name) {
				continue // skip duplicates without failing the whole batch
			}
			host := name + ".localhost"
			upstream := fmt.Sprintf("http://host.docker.internal:%d", port)
			svc.Backends = append(svc.Backends, models.Backend{
				ID: name, Name: name, Host: host, URL: "//" + host,
				Routes: []models.Route{{Path: "/", Rule: rule, Upstream: upstream}},
				Health: models.Health{URL: upstream + "/", IntervalSeconds: 30, TimeoutSeconds: 5},
			})
		}
		return nil
	})
	s.afterMutation(w, r, err)
}

// handleUserEmail renames a user (email is the key).
func (s *Server) handleUserEmail(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	old := r.PostFormValue("old")
	neu := strings.TrimSpace(r.PostFormValue("email"))
	if neu == "" {
		http.Error(w, i18n.T(s.lang(r), "err.email_required"), http.StatusBadRequest)
		return
	}
	err := s.mutateUsers(func(users *[]models.User) error {
		for _, u := range *users {
			if u.Email == neu && neu != old {
				return fmt.Errorf("email %q already in use", neu)
			}
		}
		for i := range *users {
			if (*users)[i].Email == old {
				(*users)[i].Email = neu
				return nil
			}
		}
		return fmt.Errorf("user not found")
	})
	s.afterMutation(w, r, err)
}

// handleUserPassword sets a new password for a local user (admin-driven reset).
func (s *Server) handleUserPassword(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	pw := r.PostFormValue("password")
	if pw == "" {
		http.Error(w, i18n.T(s.lang(r), "err.password_required"), http.StatusBadRequest)
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		http.Error(w, i18n.T(s.lang(r), "err.internal"), http.StatusInternalServerError)
		return
	}
	err = s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			if (*users)[i].Email == email {
				(*users)[i].PasswordHash = hash
				return nil
			}
		}
		return fmt.Errorf("user not found")
	})
	s.afterMutation(w, r, err)
}

// hostInternalize rewrites an upstream pointing at localhost/127.0.0.1 to
// s.UpstreamLocalhost: inside a container "localhost" is the container itself,
// not the host, so host services must be reached via host.docker.internal (the
// Docker default). On a host/LXD deploy this is "127.0.0.1"; an empty value
// disables the rewrite (upstream left untouched).
func (s *Server) hostInternalize(upstream string) string {
	target := s.UpstreamLocalhost
	if target == "" {
		return upstream // rewrite disabled (host deploy without translation)
	}
	i := strings.Index(upstream, "://")
	if i < 0 {
		return upstream
	}
	rest := upstream[i+3:]
	hostport, tail := rest, ""
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		hostport, tail = rest[:j], rest[j:]
	}
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = hostport, ""
	}
	if host != "localhost" && host != "127.0.0.1" {
		return upstream
	}
	host = target
	hp := host
	if port != "" {
		hp = host + ":" + port
	}
	return upstream[:i+3] + hp + tail
}

// hostInternalPort returns the port of an upstream pointing at host.docker.internal,
// or 0 otherwise.
func hostInternalPort(upstream string) int {
	s := upstream
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || host != "host.docker.internal" {
		return 0
	}
	n, _ := strconv.Atoi(port)
	return n
}

// sanitizeName lowercases a container name and keeps only [a-z0-9-].
func sanitizeName(n string) string {
	n = strings.ToLower(strings.TrimPrefix(n, "/"))
	var b strings.Builder
	for _, c := range n {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func excluded(name string, deny []string) bool {
	ln := strings.ToLower(name)
	for _, d := range deny {
		if d != "" && strings.Contains(ln, strings.ToLower(d)) {
			return true
		}
	}
	return false
}

// adminUserRow precomputes per-(user,id) authorization to render checkboxes.
type adminUserRow struct {
	Email    string
	Provider string
	Admin    bool
	Hosts    []string
}

func rowsFor(users []models.User) []adminUserRow {
	rows := make([]adminUserRow, 0, len(users))
	for _, u := range users {
		rows = append(rows, adminUserRow{Email: u.Email, Provider: u.Provider, Admin: u.Admin, Hosts: u.Backends})
	}
	return rows
}

// handleUserAdmin toggles the admin flag, keeping at least one admin.
func (s *Server) handleUserAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	err := s.mutateUsers(func(users *[]models.User) error {
		target, admins := -1, 0
		for i := range *users {
			if (*users)[i].Admin {
				admins++
			}
			if (*users)[i].Email == email {
				target = i
			}
		}
		if target < 0 {
			return fmt.Errorf("user not found")
		}
		if (*users)[target].Admin && admins <= 1 {
			return fmt.Errorf("at least one administrator must remain")
		}
		(*users)[target].Admin = !(*users)[target].Admin
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) allServiceIDs(svc models.Services) []string {
	set := map[string]bool{}
	for _, b := range s.BaseBackends {
		set[b.ID] = true
	}
	for _, b := range svc.Backends {
		set[b.ID] = true
	}
	for _, l := range svc.Links {
		set[l.ID] = true
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// --- service mutations -------------------------------------------------------

func (s *Server) mutateServices(fn func(*models.Services) error) error {
	svc, err := config.LoadServices(s.ServicesPath)
	if err != nil {
		return err
	}
	if err := fn(&svc); err != nil {
		return err
	}
	if err := config.SaveServices(s.ServicesPath, s.BackupsDir, svc); err != nil {
		return err
	}
	return s.Reload()
}

func (s *Server) handleLinkAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id, name, url := r.PostFormValue("id"), r.PostFormValue("name"), r.PostFormValue("url")
	if id == "" || name == "" || url == "" {
		http.Error(w, i18n.T(s.lang(r), "err.id_name_url_required"), http.StatusBadRequest)
		return
	}
	err := s.mutateServices(func(svc *models.Services) error {
		if s.idTaken(*svc, id) {
			return fmt.Errorf("id already exists")
		}
		svc.Links = append(svc.Links, models.Link{ID: id, Name: name, URL: url,
			Description: r.PostFormValue("desc"), Public: r.PostFormValue("public") != ""})
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleLinkDel(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		out := svc.Links[:0]
		for _, l := range svc.Links {
			if l.ID != id {
				out = append(out, l)
			}
		}
		svc.Links = out
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleBackendAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id, host := r.PostFormValue("id"), r.PostFormValue("host")
	upstream := s.hostInternalize(r.PostFormValue("upstream"))
	rule := r.PostFormValue("rule")
	if id == "" || host == "" || upstream == "" {
		http.Error(w, i18n.T(s.lang(r), "err.id_host_upstream_required"), http.StatusBadRequest)
		return
	}
	if rule != "public" && rule != "authenticated" && rule != "whitelist" {
		http.Error(w, i18n.T(s.lang(r), "err.invalid_rule"), http.StatusBadRequest)
		return
	}
	path := r.PostFormValue("path")
	if path == "" {
		path = "/"
	}
	ipAllow, ierr := normalizeCIDRs(r.PostFormValue("ip_allow"))
	if ierr != nil {
		http.Error(w, ierr.Error(), http.StatusBadRequest)
		return
	}
	// Already published (e.g. re-publishing a hosting site)? Don't error — show the
	// existing backend's edit form so the admin sees/changes what's there.
	if cur, e := config.LoadServices(s.ServicesPath); e == nil && s.idTaken(cur, id) {
		http.Redirect(w, r, "/admin/backend/edit?id="+url.QueryEscape(id), http.StatusSeeOther)
		return
	}
	// When the Hosting panel publishes a vhost it tags the backend as hosting-managed
	// (its upstream <site[-vhost]>.site:8080 is owned there and locked in Services).
	var hostingRef *models.HostingRef
	if hs := r.PostFormValue("hosting_site"); hs != "" {
		hostingRef = &models.HostingRef{Site: hs, Vhost: r.PostFormValue("hosting_vhost")}
	}
	err := s.mutateServices(func(svc *models.Services) error {
		if s.idTaken(*svc, id) {
			return fmt.Errorf("id already exists")
		}
		svc.Backends = append(svc.Backends, models.Backend{
			ID: id, Name: r.PostFormValue("name"), Host: host, URL: r.PostFormValue("url"),
			WWW:     r.PostFormValue("www") != "",
			IPAllow: ipAllow,
			Routes:  []models.Route{{Path: path, Rule: rule, Upstream: upstream}},
			Hosting: hostingRef,
		})
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleBackendDel(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		out := svc.Backends[:0]
		for _, b := range svc.Backends {
			if b.ID != id {
				out = append(out, b)
			}
		}
		svc.Backends = out
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleBackendToggle(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Backends {
			if svc.Backends[i].ID == id {
				svc.Backends[i].Disabled = !svc.Backends[i].Disabled
				return nil
			}
		}
		return fmt.Errorf("backend not found")
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleLinkToggle(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Links {
			if svc.Links[i].ID == id {
				svc.Links[i].Disabled = !svc.Links[i].Disabled
				return nil
			}
		}
		return fmt.Errorf("link not found")
	})
	s.afterMutation(w, r, err)
}

// routeView is one per-path override row as shown in the backend edit form.
// The nginx match kind (exact "= /x" vs prefix "/x") is encoded in Route.Path;
// splitMatch/joinMatch translate between the stored path and the (path, exact) UI.
type routeView struct {
	Path  string
	Exact bool
	Rule  string
}

func splitMatch(p string) (string, bool) {
	if strings.HasPrefix(p, "= ") {
		return strings.TrimSpace(p[2:]), true
	}
	return p, false
}

func joinMatch(path string, exact bool) string {
	path = strings.TrimSpace(path)
	if exact {
		return "= " + path
	}
	return path
}

func (s *Server) handleBackendEditForm(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	svc, _ := config.LoadServices(s.ServicesPath)
	for _, b := range svc.Backends {
		if b.ID != id {
			continue
		}
		rt := models.Route{Path: "/", Rule: "whitelist"}
		if len(b.Routes) > 0 {
			rt = b.Routes[0]
		}
		var overrides []routeView
		if len(b.Routes) > 1 {
			for _, ro := range b.Routes[1:] {
				cp, exact := splitMatch(ro.Path)
				overrides = append(overrides, routeView{Path: cp, Exact: exact, Rule: ro.Rule})
			}
		}
		wafEnabled, wafMode := false, "detect"
		if b.Waf != nil {
			wafEnabled = b.Waf.Enabled
			if b.Waf.Mode != "" {
				wafMode = b.Waf.Mode
			}
		}
		s.renderAdminPage(w, r, "servizi", adminEditTmpl, struct {
			ID, Name, Description, Host, URL, Path, Rule, Upstream, IPAllow string
			NgxTimeout, NgxMaxBody                                          int
			NgxWS, NgxNoBuf, NgxSelfSigned, WWW, Managed                    bool
			NgxCustomLoc, NgxCustomSrv                                      string
			Overrides                                                       []routeView
			WafEnabled                                                      bool
			WafMode                                                         string
		}{
			b.ID, b.Name, b.Description, b.Host, b.URL, rt.Path, rt.Rule, rt.Upstream, strings.Join(b.IPAllow, " "),
			b.Nginx.ProxyTimeout, b.Nginx.MaxBodyMB,
			b.Nginx.WebSocket, b.Nginx.NoBuffering, b.Nginx.BackendSelfSigned, b.WWW, b.Hosting != nil,
			b.Nginx.CustomLocation, b.Nginx.CustomServer, overrides, wafEnabled, wafMode,
		})
		return
	}
	http.Error(w, i18n.T(s.lang(r), "err.backend_not_found"), http.StatusNotFound)
}

func (s *Server) handleBackendEdit(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	rule := r.PostFormValue("rule")
	if rule != "public" && rule != "authenticated" && rule != "whitelist" {
		rule = "whitelist"
	}
	path := r.PostFormValue("path")
	if path == "" {
		path = "/"
	}
	upstream := s.hostInternalize(r.PostFormValue("upstream"))
	if err := validateUpstream(upstream); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	host := r.PostFormValue("host")
	if host == "" {
		http.Error(w, i18n.T(s.lang(r), "err.host_required"), http.StatusBadRequest)
		return
	}
	ipAllow, ierr := normalizeCIDRs(r.PostFormValue("ip_allow"))
	if ierr != nil {
		http.Error(w, ierr.Error(), http.StatusBadRequest)
		return
	}
	ngx := parseNginxOpts(r)
	err := s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Backends {
			if svc.Backends[i].ID != id {
				continue
			}
			b := &svc.Backends[i]
			b.Name = r.PostFormValue("name")
			b.Description = r.PostFormValue("description")
			b.Host = host
			b.URL = r.PostFormValue("url")
			b.WWW = r.PostFormValue("www") != ""
			b.IPAllow = ipAllow
			b.Nginx = ngx
			up := upstream
			if b.Hosting != nil && len(b.Routes) > 0 {
				up = b.Routes[0].Upstream // hosting owns the upstream; ignore any posted change
			}
			routes := []models.Route{{Path: path, Rule: rule, Upstream: up}}
			opaths := r.PostForm["opath"]
			omatches := r.PostForm["omatch"]
			orules := r.PostForm["orule"]
			for i, op := range opaths {
				cp, exact := splitMatch(strings.TrimSpace(op))
				if cp == "" {
					continue
				}
				orule := "authenticated"
				if i < len(orules) {
					orule = orules[i]
				}
				if orule != "public" && orule != "authenticated" && orule != "whitelist" {
					orule = "authenticated"
				}
				if i < len(omatches) && omatches[i] == "exact" {
					exact = true
				}
				routes = append(routes, models.Route{Path: joinMatch(cp, exact), Rule: orule, Upstream: up})
			}
			b.Routes = routes
			if r.PostFormValue("waf_enabled") != "" {
				mode := r.PostFormValue("waf_mode")
				if mode != "block" {
					mode = "detect"
				}
				wc := &models.WafCfg{Enabled: true, Mode: mode}
				if b.Waf != nil { // preserve stored paranoia/threshold tuning
					wc.Paranoia, wc.Threshold = b.Waf.Paranoia, b.Waf.Threshold
				}
				b.Waf = wc
			} else {
				b.Waf = nil
			}
			return nil
		}
		return fmt.Errorf("backend not found")
	})
	s.afterMutation(w, r, err)
}

// parseNginxOpts reads the per-vhost "NGINX settings" section of the backend edit
// form into a models.NginxOpts (zero values = default behaviour).
func parseNginxOpts(r *http.Request) models.NginxOpts {
	atoi := func(s string) int {
		n, _ := strconv.Atoi(strings.TrimSpace(s))
		if n < 0 {
			n = 0
		}
		return n
	}
	return models.NginxOpts{
		ProxyTimeout:      atoi(r.PostFormValue("ngx_timeout")),
		MaxBodyMB:         atoi(r.PostFormValue("ngx_maxbody")),
		WebSocket:         r.PostFormValue("ngx_ws") != "",
		NoBuffering:       r.PostFormValue("ngx_nobuf") != "",
		BackendSelfSigned: r.PostFormValue("ngx_selfsigned") != "",
		CustomLocation:    strings.TrimSpace(r.PostFormValue("ngx_custom_loc")),
		CustomServer:      strings.TrimSpace(r.PostFormValue("ngx_custom_srv")),
	}
}

// handleAdminIPs updates the admin-area IP whitelist (persisted as a services.json
// override, applied on hot reload). Anti-lockout: the resulting effective list
// must still include the caller's IP, else the change is refused.
func (s *Server) handleAdminIPs(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	cidrs, err := normalizeCIDRs(r.PostFormValue("ip_whitelist"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	effective := cidrs
	if len(effective) == 0 {
		effective = s.Cfg.Admin.IPWhitelist // clearing the override reverts to config
	}
	ip := clientIP(r, s.Cfg.Server.TrustedProxies)
	if ip == nil || !ipInCIDRs(ip, effective) {
		http.Error(w, i18n.T(s.lang(r), "err.ip_lockout"), http.StatusBadRequest)
		return
	}
	err = s.mutateServices(func(svc *models.Services) error {
		svc.AdminIPWhitelist = cidrs // empty = revert to config on reload
		return nil
	})
	s.afterMutation(w, r, err)
}

// validateUpstream checks an upstream URL has a valid host:port.
func validateUpstream(upstream string) error {
	s := upstream
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return fmt.Errorf("invalid upstream (expected http://host:port)")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid upstream port")
	}
	return nil
}

// --- user mutations ----------------------------------------------------------

func (s *Server) mutateUsers(fn func(*[]models.User) error) error {
	users := s.Users.All()
	if err := fn(&users); err != nil {
		return err
	}
	if err := config.SaveUsers(s.UsersPath, s.BackupsDir, models.Users{Users: users}); err != nil {
		return err
	}
	s.Users.Replace(users)
	return nil
}

func (s *Server) handleUserAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email, pw := r.PostFormValue("email"), r.PostFormValue("password")
	if email == "" || pw == "" {
		http.Error(w, i18n.T(s.lang(r), "err.email_password_required"), http.StatusBadRequest)
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		http.Error(w, i18n.T(s.lang(r), "err.internal"), http.StatusInternalServerError)
		return
	}
	secret := ""
	if !s.Cfg.DisableTOTP {
		secret, err = auth.NewTOTPSecret()
		if err != nil {
			http.Error(w, i18n.T(s.lang(r), "err.internal"), http.StatusInternalServerError)
			return
		}
	}
	authzIDs := r.PostForm["authz"]
	err = s.mutateUsers(func(users *[]models.User) error {
		for _, u := range *users {
			if u.Email == email {
				return fmt.Errorf("user already exists")
			}
		}
		*users = append(*users, models.User{
			Email: email, Provider: "local", PasswordHash: hash,
			TOTPSecret: secret, Backends: authzIDs,
		})
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Cfg.DisableTOTP {
		s.afterMutation(w, r, nil) // no QR when 2FA is disabled
		return
	}
	s.renderAdminQR(w, r, email, secret)
}

func (s *Server) handleUserDel(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	err := s.mutateUsers(func(users *[]models.User) error {
		out := (*users)[:0]
		for _, u := range *users {
			if u.Email != email {
				out = append(out, u)
			}
		}
		*users = out
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleUserAuthz(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	ids := r.PostForm["authz"]
	err := s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			if (*users)[i].Email == email {
				(*users)[i].Backends = ids
				return nil
			}
		}
		return fmt.Errorf("user not found")
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleUserTOTP(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		http.Error(w, i18n.T(s.lang(r), "err.internal"), http.StatusInternalServerError)
		return
	}
	err = s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			if (*users)[i].Email == email {
				(*users)[i].TOTPSecret = secret
				return nil
			}
		}
		return fmt.Errorf("user not found")
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.renderAdminQR(w, r, email, secret)
}

// --- helpers -----------------------------------------------------------------

// afterMutation redirects back to /admin on success (PRG), else shows the error.
func (s *Server) afterMutation(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dest := "/admin/servizi"
	if ref := r.Referer(); ref != "" {
		// Return to the page the user came from, but only if it renders without a
		// query param. The backend edit form (/admin/backend/edit?id=…) needs the
		// id in the query, which we drop here — landing there would 404; send the
		// user back to the services list instead.
		if u, e := url.Parse(ref); e == nil && strings.HasPrefix(u.Path, "/admin") && u.Path != "/admin/backend/edit" {
			dest = u.Path
		}
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) idTaken(svc models.Services, id string) bool {
	for _, b := range s.BaseBackends {
		if b.ID == id {
			return true
		}
	}
	for _, b := range svc.Backends {
		if b.ID == id {
			return true
		}
	}
	for _, l := range svc.Links {
		if l.ID == id {
			return true
		}
	}
	return false
}

func (s *Server) renderAdminQR(w http.ResponseWriter, r *http.Request, email, secret string) {
	png, err := qrcode.Encode(otpauthURI(email, secret), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, i18n.T(s.lang(r), "err.qr"), http.StatusInternalServerError)
		return
	}
	s.renderAdminPage(w, r, "utenti", adminQRTmpl, struct {
		Email  string
		Secret string
		QR     template.URL
	}{Email: email, Secret: secret, QR: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))})
}
