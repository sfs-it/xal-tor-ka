// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Command xtk-hosting-ui is the hosting extension's web UI: an internal service that
// renders a site-management panel (shared xtkui chrome, tabbed: Hosts, Users, MySQL,
// PgSQL) and drives the privileged xtk-agent over its unix socket. It has NO host
// powers of its own — every mutating action is a vetted agent command. The gateway
// reverse-proxies it under the admin host (/admin/hosting), auth-gated; it is never
// exposed directly.
package main

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"xaltorka/agent"
	"xaltorka/version"
	"xaltorka/xtkui"
)

// randToken returns prefix + 10 random hex chars — a valid, opaque db/user
// identifier ([a-z][a-z0-9_]…). Used so attached db names/users are random, not
// derived from (guessable) site names.
func randToken(prefix string) string {
	b := make([]byte, 5)
	_, _ = crand.Read(b)
	return prefix + hex.EncodeToString(b)
}

type site struct {
	Name       string `json:"name"`
	UID        int    `json:"uid"`
	Running    int    `json:"running"`
	Template   string `json:"template"`
	PhpVersion string `json:"php_version"`
	Db         string `json:"db"`
	AutoUpdate bool   `json:"auto_update"`
}

type hostingUser struct {
	User   string `json:"user"`
	UID    int    `json:"uid"`
	Site   string `json:"site"`
	Home   string `json:"home"`
	Orphan bool   `json:"orphan"`
	Scp    string `json:"scp"` // on | off | none
}

type sshdStatus struct {
	Installed bool `json:"installed"`
	Running   bool `json:"running"`
	Port      int  `json:"port"`
}

type dbStatus struct {
	Engine    string `json:"engine"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Localhost string `json:"localhost"`
	Version   string `json:"version"`
}

// dbTab maps a URL segment (mysql|pgsql) to the agent engine name (mysql|pg), label,
// and the Adminer login-URL driver param (server= for MySQL, pgsql= for Postgres).
type dbTab struct{ Engine, Seg, Label, Driver string }

var dbTabs = map[string]dbTab{
	"mysql": {Engine: "mysql", Seg: "mysql", Label: "MySQL", Driver: "server"},
	"pgsql": {Engine: "pg", Seg: "pgsql", Label: "PgSQL", Driver: "pgsql"},
}

// adminerSession is a live ephemeral Adminer container; reaped after idle.
type adminerSession struct {
	engine, alias string
	last          time.Time
}

type server struct {
	socket  string
	log     *slog.Logger
	mu      sync.Mutex
	adminer map[string]*adminerSession // token → session
}

// callAgent runs one vetted command over the unix socket (one request per connection).
func (s *server) callAgent(ctx context.Context, cmd string, params map[string]string) (agent.Response, error) {
	var resp agent.Response
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "unix", s.socket)
	if err != nil {
		return resp, fmt.Errorf("dial agent: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Minute))
	if err := json.NewEncoder(conn).Encode(agent.Request{Cmd: cmd, Params: params}); err != nil {
		return resp, fmt.Errorf("send %s: %w", cmd, err)
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return resp, fmt.Errorf("read %s: %w", cmd, err)
	}
	return resp, nil
}

// callJSON runs a read-only command and unmarshals its stdout into v.
func (s *server) callJSON(ctx context.Context, cmd string, params map[string]string, v any) error {
	resp, err := s.callAgent(ctx, cmd, params)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s: %s", cmd, agentMsg(resp, nil))
	}
	return json.Unmarshal([]byte(resp.Stdout), v)
}

func agentMsg(resp agent.Response, err error) string {
	if err != nil {
		return err.Error()
	}
	if resp.Error != "" {
		return resp.Error
	}
	if s := strings.TrimSpace(resp.Stderr); s != "" {
		return firstLine(s)
	}
	return firstLine(strings.TrimSpace(resp.Stdout))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// redirectMsg returns to a tab with a one-shot ok/err notice.
func redirectMsg(w http.ResponseWriter, r *http.Request, path, ok, errMsg string) {
	q := url.Values{}
	if errMsg != "" {
		q.Set("err", errMsg)
	} else if ok != "" {
		q.Set("ok", ok)
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	http.Redirect(w, r, path+sep+q.Encode(), http.StatusSeeOther)
}

// chrome renders the SAME top menu as the core admin (via xtkui.AdminNav) with the
// "Hosting" entry active — so the extension looks like a native admin section. The
// four hosting tabs are a secondary bar inside the page (subtabsSrc), not the topbar.
func (s *server) chrome(title string) xtkui.Chrome {
	return xtkui.Chrome{
		Title: "Xal-Tor-Ka · " + title, BrandText: "⛬ Xal-Tor-Ka", BrandHref: "/admin",
		SubtitleKey: "admin.subtitle", Version: version.Version,
		Nav: xtkui.AdminNav(true), Active: "hosting",
		DashboardHref: "/listing", DashboardKey: "nav.dashboard", LoggedIn: true,
	}
}

// subtabsSrc is the in-page secondary tab bar; .Tab (on each page's data) marks the
// active one. Prepended to every hosting template.
const subtabsSrc = `<nav class="subtabs">
<a href="/admin/hosting"{{if eq .Tab "hosts"}} class="active"{{end}}>Hosts</a>
<a href="/admin/hosting/users"{{if eq .Tab "users"}} class="active"{{end}}>Users</a>
<a href="/admin/hosting/mysql"{{if eq .Tab "mysql"}} class="active"{{end}}>MySQL</a>
<a href="/admin/hosting/pgsql"{{if eq .Tab "pgsql"}} class="active"{{end}}>PgSQL</a>
</nav>
`

func notices(r *http.Request) (string, string) {
	return r.URL.Query().Get("ok"), r.URL.Query().Get("err")
}

// ---------------------------------------------------------------- Hosts tab

func (s *server) listSites(ctx context.Context) ([]site, error) {
	var sites []site
	if err := s.callJSON(ctx, "site_list", nil, &sites); err != nil {
		return nil, err
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Name < sites[j].Name })
	return sites, nil
}

var indexTmpl = xtkui.LocParse("hosting", subtabsSrc+`<h1>Hosts</h1>
{{if .Notice}}<div class="ok">{{.Notice}}</div>{{end}}
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<section>
  <table>
    <thead><tr><th>Site</th><th>Stack</th><th>Upstream</th><th>Owner</th><th>Status</th><th></th></tr></thead>
    <tbody>
    {{range .Sites}}
      <tr{{if not .Running}} class="off"{{end}}>
        <td><b>{{.Name}}</b></td>
        <td>{{if .Template}}<code>{{.Template}}{{if .PhpVersion}} · {{.PhpVersion}}{{end}}</code>{{else}}<span class="hint">—</span>{{end}}</td>
        <td><code>{{.Name}}.site:8080</code></td>
        <td><code>site-{{.Name}}</code> <span class="hint">uid {{.UID}}</span></td>
        <td>{{if gt .Running 0}}<span class="tag ext">running · {{.Running}}</span>{{else}}<span class="tag ro">stopped</span>{{end}}</td>
        <td class="rowact">
          <button class="btn sm" type="button" onclick="this.nextElementSibling.showModal()">Edit</button>
          <dialog class="dlg">
            <form method="dialog" class="dlg-x"><button class="btn sm" aria-label="Close">✕</button></form>
            <h3>{{.Name}}</h3>
            <div class="meta">Stack: {{if .Template}}<code>{{.Template}}{{if .PhpVersion}} · {{.PhpVersion}}{{end}}</code>{{else}}<span class="hint">unknown</span>{{end}}</div>
            <div class="meta">Owner: <code>site-{{.Name}}</code> (uid {{.UID}})</div>
            <div class="meta">Upstream: <code>{{.Name}}.site:8080</code></div>
            <div class="meta">Status: {{if gt .Running 0}}<span class="tag ext">running · {{.Running}}</span>{{else}}<span class="tag ro">stopped</span>{{end}}</div>
            <div class="meta">Database: {{if .Db}}<code>{{.Db}}</code> (shared) — <a href="/admin/hosting/dbinfo?name={{.Name}}">connection &amp; password →</a>{{else}}none{{end}}</div>
            <div class="actions" style="justify-content:flex-start">
              <form class="inline" method="post" action="/admin/hosting/up"><input type="hidden" name="name" value="{{.Name}}"><button class="btn sm">Start</button></form>
              <form class="inline" method="post" action="/admin/hosting/down"><input type="hidden" name="name" value="{{.Name}}"><button class="btn sm">Stop</button></form>
              <form class="inline" method="post" action="/admin/hosting/autoupdate">
                <input type="hidden" name="name" value="{{.Name}}">
                <input type="hidden" name="enabled" value="{{if .AutoUpdate}}false{{else}}true{{end}}">
                <button class="btn sm">{{if .AutoUpdate}}Disable auto-update{{else}}Enable auto-update{{end}}</button>
              </form>
            </div>
            <p class="hint">Auto-update is {{if .AutoUpdate}}<b>on</b> — the site follows template updates while its compose stays pristine.{{else}}<b>off</b>.{{end}}</p>
            <h4 style="margin:1rem 0 .3rem;border-top:1px solid var(--line);padding-top:.9rem">Publish</h4>
            <form method="post" action="/admin/backend/add">
              <input type="hidden" name="id" value="{{.Name}}">
              <input type="hidden" name="name" value="{{.Name}} (hosting)">
              <input type="hidden" name="upstream" value="http://{{.Name}}.site:8080">
              <input type="hidden" name="path" value="/">
              <div class="formgrid">
                <div><label>Public host</label><input name="host" placeholder="mysite.example.com" required></div>
                <div><label>Rule</label><select name="rule"><option>public</option><option>authenticated</option><option>whitelist</option></select></div>
                <div><label>www</label><label class="hint" style="display:inline-flex;align-items:center;gap:.35rem;height:2.2rem"><input type="checkbox" name="www" value="1"> also www.host</label></div>
                <div><button class="btn primary">Publish backend</button></div>
              </div>
            </form>
            <p class="hint">Creates a gateway backend for <code>{{.Name}}.site:8080</code>. Then point the host's DNS at the gateway and issue a cert in <b>TLS</b>. (Opens the Services page.)</p>
          </dialog>
          <a class="btn sm" href="/admin/hosting/edit?name={{.Name}}" title="Edit docker-compose.yml"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" style="vertical-align:-2px"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><path d="M14 2v6h6"/></svg> Compose</a>
          <form class="inline" method="post" action="/admin/hosting/destroy" onsubmit="return confirm('Destroy {{.Name}}? This removes its data and OS user.')"><input type="hidden" name="name" value="{{.Name}}"><button class="btn danger sm">Destroy</button></form>
        </td>
      </tr>
    {{else}}
      <tr><td colspan="6" class="hint">No sites yet.</td></tr>
    {{end}}
    </tbody>
  </table>
</section>
<section>
  <div class="card addcard" style="margin-top:1rem">
    <h3>New site</h3>
    <form method="post" action="/admin/hosting/create"><div class="formgrid">
      <div><label>Name</label><input name="name" placeholder="a-z0-9-" pattern="[a-z][a-z0-9-]{1,30}" required></div>
      <div style="grid-column:span 2"><label>Stack</label><select name="stack">
        <option value="php-fpm:8.3">NGINX + PHP-FPM 8.3</option>
        <option value="php-fpm:8.3:mysql">NGINX + PHP-FPM 8.3 + MySQL (shared)</option>
        <option value="php-fpm:8.3:pg">NGINX + PHP-FPM 8.3 + PgSQL (shared)</option>
        <option value="php-fpm:8.2">NGINX + PHP-FPM 8.2</option>
        <option value="php-fpm:8.2:mysql">NGINX + PHP-FPM 8.2 + MySQL (shared)</option>
        <option value="php-fpm:8.2:pg">NGINX + PHP-FPM 8.2 + PgSQL (shared)</option>
        <option value="php-fpm:8.1">NGINX + PHP-FPM 8.1</option>
        <option value="php-fpm:7.4">NGINX + PHP-FPM 7.4 (legacy)</option>
        <option value="static">NGINX (static)</option>
        <option value="custom">Custom — write your own compose.yml</option>
      </select></div>
      <div><button class="btn primary">Create &amp; start</button></div>
    </div></form>
    <p class="hint">Provisions an isolated site (own OS user in <code>docker-hosting</code>), starts it on the
    <code>xtk-hosting</code> network, reachable at <code>&lt;name&gt;.site:8080</code>. Add a backend in
    Services to publish it.</p>
  </div>
</section>`)

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	ok, errMsg := notices(r)
	data := struct {
		Tab           string
		Sites         []site
		Notice, Error string
	}{Tab: "hosts", Notice: ok, Error: errMsg}
	sites, err := s.listSites(r.Context())
	if err != nil && data.Error == "" {
		data.Error = err.Error()
	}
	data.Sites = sites
	s.chrome("Hosting").Render(w, xtkui.LangFromRequest(r), indexTmpl, data)
}

// ---------------------------------------------------------------- Users tab

var usersTmpl = xtkui.LocParse("hostingusers", subtabsSrc+`<h1>Users</h1>
<p class="hint">OS accounts that own sites (<code>docker-hosting</code>, nologin). File access is
SCP/SFTP, chrooted to the site dir — upload into <code>www/</code>.</p>
{{if .Notice}}<div class="ok">{{.Notice}}</div>{{end}}
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<section><div class="card">
  <div class="row"><h3>SCP / SFTP gateway</h3>
  {{if .Sshd.Running}}<span class="tag ext">running · port {{.Sshd.Port}}</span>{{else if .Sshd.Installed}}<span class="tag ro">stopped</span>{{else}}<span class="tag ro">not installed</span>{{end}}</div>
  {{if .Sshd.Installed}}<div class="meta">Connect: <code>sftp -P {{.Sshd.Port}} site-&lt;name&gt;@&lt;host&gt;</code> (SFTP-only, chroot, no shell). Set a password per user below to enable access.</div>
  {{else}}<p class="hint">Not installed. Installing brings up a hardened OpenSSH container (SFTP-only, chroot, no shell) on port 2222.</p>
  <form method="post" action="/admin/hosting/users/sshd-install"><button class="btn primary">Install SCP gateway</button></form>{{end}}
</div></section>
<section>
  <table>
    <thead><tr><th>Site</th><th>OS user</th><th>uid</th><th>SCP</th><th></th></tr></thead>
    <tbody>
    {{range .Users}}
      <tr{{if .Orphan}} class="off"{{end}}>
        <td><b>{{.Site}}</b>{{if .Orphan}} <span class="tag ro">orphan</span>{{end}}</td>
        <td><code>{{.User}}</code></td><td><code>{{.UID}}</code></td>
        <td>{{if eq .Scp "on"}}<span class="tag ext">enabled</span>{{else if eq .Scp "off"}}<span class="tag ro">disabled</span>{{else}}<span class="hint">no password</span>{{end}}</td>
        <td class="rowact">
          <button class="btn sm" type="button" onclick="this.nextElementSibling.showModal()">Edit</button>
          <dialog class="dlg">
            <form method="dialog" class="dlg-x"><button class="btn sm" aria-label="Close">✕</button></form>
            <h3>{{.User}}</h3>
            <div class="meta">Site: <code>{{.Site}}</code>{{if .Orphan}} <span class="tag ro">orphan</span>{{end}} · uid <code>{{.UID}}</code></div>
            <div class="meta">SCP access: {{if eq .Scp "on"}}<b>enabled</b>{{else if eq .Scp "off"}}<b>disabled</b>{{else}}no password set{{end}}</div>
            <form method="post" action="/admin/hosting/users/passwd" style="margin:.9rem 0 .3rem">
              <input type="hidden" name="name" value="{{.Site}}">
              <div class="formgrid"><div><label>Set SCP password (min 8)</label><input type="password" name="password" minlength="8" required></div>
              <div><button class="btn primary">Set password</button></div></div>
            </form>
            <div class="actions" style="justify-content:flex-start">
              <a class="btn sm" href="/admin/hosting/users/keys?name={{.Site}}">SSH keys</a>
              {{if ne .Scp "none"}}<form class="inline" method="post" action="/admin/hosting/users/lock"><input type="hidden" name="name" value="{{.Site}}"><input type="hidden" name="locked" value="{{if eq .Scp "on"}}true{{else}}false{{end}}"><button class="btn sm">{{if eq .Scp "on"}}Disable SCP{{else}}Enable SCP{{end}}</button></form>{{end}}
              {{if .Orphan}}<form class="inline" method="post" action="/admin/hosting/users/delete" onsubmit="return confirm('Delete orphan user {{.User}}?')"><input type="hidden" name="name" value="{{.Site}}"><button class="btn danger sm">Delete user</button></form>{{end}}
            </div>
          </dialog>
        </td>
      </tr>
    {{else}}
      <tr><td colspan="5" class="hint">No site users yet.</td></tr>
    {{end}}
    </tbody>
  </table>
</section>`)

func (s *server) handleUsers(w http.ResponseWriter, r *http.Request) {
	var users []hostingUser
	err := s.callJSON(r.Context(), "hosting_users", nil, &users)
	sort.Slice(users, func(i, j int) bool { return users[i].Site < users[j].Site })
	var sshd sshdStatus
	_ = s.callJSON(r.Context(), "sshd_status", nil, &sshd)
	ok, errMsg := notices(r)
	data := struct {
		Tab           string
		Users         []hostingUser
		Sshd          sshdStatus
		Notice, Error string
	}{Tab: "users", Users: users, Sshd: sshd, Notice: ok, Error: errMsg}
	if err != nil && data.Error == "" {
		data.Error = err.Error()
	}
	s.chrome("Users").Render(w, xtkui.LangFromRequest(r), usersTmpl, data)
}

// ---------------------------------------------------------------- MySQL / PgSQL tabs

var dbTmpl = xtkui.LocParse("hostingdb", subtabsSrc+`<h1>{{.Label}}</h1>
{{if .Notice}}<div class="ok">{{.Notice}}</div>{{end}}
{{if .Created}}<div class="ok"><b>Database created — copy the connection now:</b><br><code>{{.Created}}</code></div>{{end}}
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
{{if not .Status.Installed}}
  <section><div class="card">
    <h3>{{.Label}} shared instance — not installed</h3>
    <p class="hint">A single shared {{.Label}} instance serves all hosting sites. It is not
    installed yet. Installing brings up a container with a persistent volume, reachable by
    sites as <code>{{.Status.Host}}</code> and host-locally at <code>{{.Status.Localhost}}</code>.</p>
    <form method="post" action="/admin/hosting/{{.Seg}}/install"><button class="btn primary">Install shared {{.Label}}</button></form>
  </div></section>
{{else}}
  <section><div class="card">
    <div class="row"><h3>{{.Label}} shared instance</h3>
      {{if .Status.Running}}<span class="tag ext">running · {{.Status.Version}}</span>{{else}}<span class="tag ro">stopped</span>{{end}}</div>
    <div class="meta">From sites: <code>{{.Status.Host}}:{{.Status.Port}}</code></div>
    <div class="meta">Host-local: <code>{{.Status.Localhost}}</code> (admin tools / clients)</div>
    {{if .Status.Running}}<form method="post" action="/admin/hosting/{{.Seg}}/adminer/open" style="margin-top:.7rem"><button class="btn">Open database admin (Adminer)</button></form>{{end}}
  </div></section>
  <section>
    <table><thead><tr><th>Database</th></tr></thead><tbody>
    {{range .Databases}}<tr><td><code>{{.}}</code></td></tr>{{else}}<tr><td class="hint">No databases yet.</td></tr>{{end}}
    </tbody></table>
    <div class="card addcard" style="margin-top:1rem"><h3>New database</h3>
      <form method="post" action="/admin/hosting/{{.Seg}}/dbcreate"><div class="formgrid">
        <div><label>Name (= db and user)</label><input name="name" pattern="[a-z][a-z0-9_]{1,30}" placeholder="a-z0-9_" required></div>
        <div><button class="btn primary">Create database</button></div>
      </div></form>
      <p class="hint">Creates a database and a dedicated user with a generated password (shown once).</p>
    </div>
  </section>
{{end}}`)

// dbView renders a DB tab; created is a one-time connection line to surface after a create.
func (s *server) dbView(w http.ResponseWriter, r *http.Request, t dbTab, created string) {
	ok, errMsg := notices(r)
	var st dbStatus
	if err := s.callJSON(r.Context(), "db_instance_status", map[string]string{"engine": t.Engine}, &st); err != nil && errMsg == "" {
		errMsg = err.Error()
	}
	var dbs []string
	if st.Running {
		_ = s.callJSON(r.Context(), "db_list", map[string]string{"engine": t.Engine}, &dbs)
	}
	data := struct {
		Tab, Label, Seg, Notice, Error, Created string
		Status                                  dbStatus
		Databases                               []string
	}{Tab: t.Seg, Label: t.Label, Seg: t.Seg, Notice: ok, Error: errMsg, Created: created, Status: st, Databases: dbs}
	s.chrome(t.Label).Render(w, xtkui.LangFromRequest(r), dbTmpl, data)
}

func (s *server) handleDB(seg string) http.HandlerFunc {
	t := dbTabs[seg]
	return func(w http.ResponseWriter, r *http.Request) { s.dbView(w, r, t, "") }
}

func (s *server) handleDBInstall(seg string) http.HandlerFunc {
	t := dbTabs[seg]
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := s.callAgent(r.Context(), "db_instance_up", map[string]string{"engine": t.Engine})
		if err != nil || !resp.OK {
			redirectMsg(w, r, "/admin/hosting/"+t.Seg, "", "install: "+agentMsg(resp, err))
			return
		}
		redirectMsg(w, r, "/admin/hosting/"+t.Seg, t.Label+" shared instance installed.", "")
	}
}

func (s *server) handleDBCreate(seg string) http.HandlerFunc {
	t := dbTabs[seg]
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.FormValue("name")
		resp, err := s.callAgent(r.Context(), "db_create", map[string]string{"engine": t.Engine, "name": name})
		if err != nil || !resp.OK {
			redirectMsg(w, r, "/admin/hosting/"+t.Seg, "", "create db: "+agentMsg(resp, err))
			return
		}
		// db_create prints the connection (incl. the one-time password) on stdout;
		// render it once in the page (never in the URL / logs).
		s.dbView(w, r, t, strings.TrimSpace(resp.Stdout))
	}
}

// ---------------------------------------------------------------- Adminer (ephemeral)

var adminerConnectTmpl = xtkui.LocParse("hostingadminer", subtabsSrc+`<h1>{{.Label}} · Adminer</h1>
<div class="card">
  <p class="hint">A throwaway Adminer session is running — it auto-stops after 5 minutes idle. Log in with:</p>
  <div class="meta">Server: <code>{{.Server}}</code></div>
  <div class="meta">Username: <code>{{.User}}</code></div>
  <div class="meta">Password: <code>{{.Password}}</code></div>
  <p style="margin-top:1rem">
    <a class="btn primary" href="/admin/hosting/{{.Seg}}/adminer/{{.Token}}/?{{.Driver}}={{.Server}}&username={{.User}}" target="_blank" rel="noopener">Open Adminer ↗</a>
    <a class="btn" href="/admin/hosting/{{.Seg}}">Back</a>
  </p>
</div>`)

func (s *server) handleAdminerOpen(seg string) http.HandlerFunc {
	t := dbTabs[seg]
	return func(w http.ResponseWriter, r *http.Request) {
		token := randToken("") // 10 hex chars
		resp, err := s.callAgent(r.Context(), "adminer_up", map[string]string{"engine": t.Engine, "token": token})
		if err != nil || !resp.OK {
			redirectMsg(w, r, "/admin/hosting/"+t.Seg, "", "adminer: "+agentMsg(resp, err))
			return
		}
		c := kvFields(resp.Stdout)
		s.mu.Lock()
		s.adminer[token] = &adminerSession{engine: t.Engine, alias: c["alias"], last: time.Now()}
		s.mu.Unlock()
		data := struct{ Tab, Label, Seg, Driver, Token, Server, User, Password string }{
			Tab: t.Seg, Label: t.Label, Seg: t.Seg, Driver: t.Driver, Token: token,
			Server: c["server"], User: c["user"], Password: c["password"],
		}
		s.chrome(t.Label).Render(w, xtkui.LangFromRequest(r), adminerConnectTmpl, data)
	}
}

// handleAdminerProxy reverse-proxies /admin/hosting/<seg>/adminer/<token>/… to the
// ephemeral Adminer container, refreshing the idle timer on each hit.
func (s *server) handleAdminerProxy(seg string) http.HandlerFunc {
	prefix := "/admin/hosting/" + seg + "/adminer/"
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, prefix)
		token, sub, _ := strings.Cut(rest, "/")
		s.mu.Lock()
		sess := s.adminer[token]
		if sess != nil {
			sess.last = time.Now()
		}
		s.mu.Unlock()
		if sess == nil {
			http.Error(w, "Adminer session expired — reopen it from the "+dbTabs[seg].Label+" tab.", http.StatusGone)
			return
		}
		target, _ := url.Parse("http://" + sess.alias + ":8080")
		proxy := httputil.NewSingleHostReverseProxy(target)
		r.URL.Path = "/" + sub // strip the prefix+token
		proxy.ServeHTTP(w, r)
	}
}

// reapAdminer stops ephemeral Adminer containers idle for more than 5 minutes.
func (s *server) reapAdminer() {
	for range time.Tick(60 * time.Second) {
		now := time.Now()
		s.mu.Lock()
		stale := []string{}
		for tok, sess := range s.adminer {
			if now.Sub(sess.last) > 5*time.Minute {
				stale = append(stale, tok)
				delete(s.adminer, tok)
			}
		}
		s.mu.Unlock()
		for _, tok := range stale {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			_, _ = s.callAgent(ctx, "adminer_down", map[string]string{"token": tok})
			cancel()
		}
	}
}

// ---------------------------------------------------------------- Hosts actions

func (s *server) action(cmd, okMsg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.FormValue("name")
		resp, err := s.callAgent(r.Context(), cmd, map[string]string{"name": name})
		if err != nil || !resp.OK {
			redirectMsg(w, r, "/admin/hosting", "", cmd+": "+agentMsg(resp, err))
			return
		}
		redirectMsg(w, r, "/admin/hosting", fmt.Sprintf(okMsg, name), "")
	}
}

// kvFields parses "k=v k2=v2 …" (db_create's output) into a map.
func kvFields(s string) map[string]string {
	m := map[string]string{}
	for _, tok := range strings.Fields(s) {
		if i := strings.IndexByte(tok, '='); i >= 0 {
			m[tok[:i]] = tok[i+1:]
		}
	}
	return m
}

func (s *server) handleCreate(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	// "stack" encodes template[:php_version[:db]] — e.g. "php-fpm:8.2:mysql". Colons
	// only (never '+', which form-encoding turns into a space).
	parts := strings.Split(r.FormValue("stack"), ":")
	tmpl, pv, db := parts[0], "", ""
	if len(parts) > 1 {
		pv = parts[1]
	}
	if len(parts) > 2 {
		db = parts[2] // agent engine name: "mysql" | "pg"
	}
	if tmpl == "" {
		tmpl = "php-fpm"
	}
	params := map[string]string{"name": name, "template": tmpl}
	if pv != "" {
		params["php_version"] = pv
	}
	if resp, err := s.callAgent(r.Context(), "site_create", params); err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "create: "+agentMsg(resp, err))
		return
	}
	// Optional shared DB: create it and inject the connection into the site's db.env.
	if db != "" {
		dbname, dbuser := randToken("h"), randToken("u") // random, not derived from the site name
		resp, err := s.callAgent(r.Context(), "db_create", map[string]string{"engine": db, "name": dbname, "user": dbuser})
		if err != nil || !resp.OK {
			_, _ = s.callAgent(r.Context(), "site_up", map[string]string{"name": name})
			redirectMsg(w, r, "/admin/hosting", "", "site created and started, but DB failed: "+agentMsg(resp, err))
			return
		}
		c := kvFields(resp.Stdout)
		env := fmt.Sprintf("DB_HOST=%s\nDB_PORT=%s\nDB_NAME=%s\nDB_USER=%s\nDB_PASSWORD=%s",
			c["host"], c["port"], c["db"], c["user"], c["password"])
		if resp, err := s.callAgent(r.Context(), "site_env_set", map[string]string{"name": name, "content": env}); err != nil || !resp.OK {
			s.log.Warn("site_env_set failed", "name", name, "err", agentMsg(resp, err))
		}
	}
	if resp, err := s.callAgent(r.Context(), "site_up", map[string]string{"name": name}); err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "site created but start failed: "+agentMsg(resp, err))
		return
	}
	if tmpl == "custom" {
		http.Redirect(w, r, "/admin/hosting/edit?name="+url.QueryEscape(name), http.StatusSeeOther)
		return
	}
	msg := "Site " + name + " created and started."
	if db != "" {
		msg += " Shared " + db + " database attached."
	}
	redirectMsg(w, r, "/admin/hosting", msg, "")
}

func (s *server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	resp, err := s.callAgent(r.Context(), "hosting_user_delete", map[string]string{"name": name})
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting/users", "", "delete user: "+agentMsg(resp, err))
		return
	}
	redirectMsg(w, r, "/admin/hosting/users", "Orphan user for "+name+" deleted.", "")
}

func (s *server) handleSshdInstall(w http.ResponseWriter, r *http.Request) {
	resp, err := s.callAgent(r.Context(), "sshd_up", nil)
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting/users", "", "install gateway: "+agentMsg(resp, err))
		return
	}
	redirectMsg(w, r, "/admin/hosting/users", "SCP gateway installed and running.", "")
}

func (s *server) handleUserPasswd(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	resp, err := s.callAgent(r.Context(), "hosting_user_passwd", map[string]string{"name": name, "password": r.FormValue("password")})
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting/users", "", "set password: "+agentMsg(resp, err))
		return
	}
	redirectMsg(w, r, "/admin/hosting/users", "SCP password set for site-"+name+".", "")
}

func (s *server) handleUserLock(w http.ResponseWriter, r *http.Request) {
	name, locked := r.FormValue("name"), r.FormValue("locked")
	resp, err := s.callAgent(r.Context(), "hosting_user_lock", map[string]string{"name": name, "locked": locked})
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting/users", "", "lock: "+agentMsg(resp, err))
		return
	}
	state := "enabled"
	if locked == "true" {
		state = "disabled"
	}
	redirectMsg(w, r, "/admin/hosting/users", "SCP for site-"+name+" "+state+".", "")
}

var sshkeyTmpl = xtkui.LocParse("hostingsshkey", subtabsSrc+`<h1>New SSH key · <code>site-{{.Name}}</code></h1>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
{{if .Private}}
<div class="ok"><b>Save the private key now — it is shown only once and never stored on the server.</b></div>
<div class="card">
  <h3>Private key</h3>
  <textarea id="privkey" readonly spellcheck="false" onclick="this.select()" style="width:100%;min-height:12rem;font-family:var(--font-mono);font-size:.76rem;padding:.6rem;border:1px solid var(--line);border-radius:9px;background:var(--panel);color:var(--text);white-space:pre">{{.Private}}</textarea>
  <h3 style="margin-top:1rem">Public key (appended to the user's authorized_keys)</h3>
  <textarea id="pubkey" readonly spellcheck="false" onclick="this.select()" style="width:100%;min-height:3.5rem;font-family:var(--font-mono);font-size:.76rem;padding:.6rem;border:1px solid var(--line);border-radius:9px;background:var(--panel);color:var(--text);white-space:pre-wrap">{{.Public}}</textarea>
  <div class="actions" style="justify-content:flex-start;flex-wrap:wrap">
    <form method="post" action="/admin/hosting/users/keydownload" style="display:inline"><input type="hidden" name="filename" value="site-{{.Name}}-ed25519"><textarea name="content" style="display:none">{{.Private}}</textarea><button class="btn primary">↓ Private key (OpenSSH)</button></form>
    <form method="post" action="/admin/hosting/users/keydownload" style="display:inline"><input type="hidden" name="filename" value="site-{{.Name}}-ed25519.pub"><textarea name="content" style="display:none">{{.Public}}</textarea><button class="btn">↓ Public key</button></form>
    {{if .PPK}}<form method="post" action="/admin/hosting/users/keydownload" style="display:inline"><input type="hidden" name="filename" value="site-{{.Name}}.ppk"><textarea name="content" style="display:none">{{.PPK}}</textarea><button class="btn">↓ PuTTY/WinSCP (.ppk)</button></form>{{end}}
    <a class="btn" href="/admin/hosting/users/keys?name={{.Name}}">Back to SSH keys</a>
  </div>
  <p class="hint" style="margin-top:.8rem"><b>OpenSSH (WSL/Linux/Mac):</b> save the private key, <code>chmod 600 &lt;file&gt;</code> (OpenSSH ignores world-readable keys), then <code>sftp -i &lt;file&gt; -P 2222 site-{{.Name}}@&lt;host&gt;</code>. <b>WinSCP/PuTTY/FileZilla (Windows):</b> use the <code>.ppk</code>. Key auth needs no password; the SCP/SFTP gateway must be running.</p>
</div>
{{end}}`)

func (s *server) handleUserSshKey(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	params := map[string]string{"name": name}
	if p := r.FormValue("passphrase"); p != "" {
		params["passphrase"] = p
	}
	if c := r.FormValue("comment"); c != "" {
		params["comment"] = c
	}
	resp, err := s.callAgent(r.Context(), "hosting_user_sshkey", params)
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting/users/keys?name="+url.QueryEscape(name), "", "ssh key: "+agentMsg(resp, err))
		return
	}
	out := resp.Stdout
	data := struct{ Tab, Name, Private, Public, PPK, Error string }{
		Tab:     "users",
		Name:    name,
		Private: sshSection(out, "===XTK-PRIV===", "===XTK-PUB==="),
		Public:  sshSection(out, "===XTK-PUB===", "===XTK-PPK==="),
		PPK:     sshSection(out, "===XTK-PPK===", ""),
	}
	s.chrome("New SSH key").Render(w, xtkui.LangFromRequest(r), sshkeyTmpl, data)
}

// sshSection extracts the text between two markers (end="" = to end), trimmed.
func sshSection(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	if end != "" {
		if j := strings.Index(rest, end); j >= 0 {
			rest = rest[:j]
		}
	}
	return strings.TrimSpace(rest)
}

// handleKeyDownload echoes the posted key back as a file attachment, normalized to
// LF (browsers submit textarea content with CRLF, which corrupts OpenSSH keys). The
// key is not stored server-side — it round-trips through this download only.
func (s *server) handleKeyDownload(w http.ResponseWriter, r *http.Request) {
	fn := strings.Map(func(c rune) rune {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			return c
		}
		return -1
	}, r.FormValue("filename"))
	if fn == "" {
		fn = "keyfile.txt"
	}
	content := strings.ReplaceAll(r.FormValue("content"), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fn+`"`)
	_, _ = w.Write([]byte(content))
}

// ---- DB connection info for a site ----

var dbInfoTmpl = xtkui.LocParse("hostingdbinfo", subtabsSrc+`<h1>Database · <code>site-{{.Name}}</code></h1>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
{{if .Has}}
<div class="card">
  <h3>Connection <span class="hint">(already injected into the site as env)</span></h3>
  <div class="meta">Database: <code>{{.DbName}}</code></div>
  <div class="meta">User: <code>{{.User}}</code> &nbsp; Password: <code>{{.Password}}</code></div>
  <div class="meta">From the site (app reads db.env): host <code>{{.DbHost}}</code> port <code>{{.DbPort}}</code></div>
  <div class="meta">From the host (admin tools / Adminer): <code>127.0.0.1:{{.LocalPort}}</code></div>
  <p class="hint">The container already has these as <code>DB_HOST / DB_PORT / DB_NAME / DB_USER / DB_PASSWORD</code>. The <code>%</code> you may see next to the user in Adminer is its <em>allowed-host</em> grant (any host) — <b>not</b> a server address; connect to <code>{{.DbHost}}</code> (from a site) or <code>127.0.0.1:{{.LocalPort}}</code> (from the host).</p>
  <p><a class="btn" href="/admin/hosting">Back to Hosts</a></p>
</div>
{{else}}
<div class="card"><p class="hint">No database attached to this site.</p><p><a class="btn" href="/admin/hosting">Back to Hosts</a></p></div>
{{end}}`)

func (s *server) handleDbInfo(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	resp, err := s.callAgent(r.Context(), "site_db_info", map[string]string{"name": name})
	env := map[string]string{}
	if err == nil && resp.OK {
		for _, line := range strings.Split(resp.Stdout, "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
				env[k] = v
			}
		}
	}
	localPort := "3306"
	if strings.Contains(env["DB_HOST"], "pg") {
		localPort = "5432"
	}
	data := struct {
		Tab, Name, DbHost, DbPort, DbName, User, Password, LocalPort, Error string
		Has                                                                 bool
	}{
		Tab: "hosts", Name: name, DbHost: env["DB_HOST"], DbPort: env["DB_PORT"], DbName: env["DB_NAME"],
		User: env["DB_USER"], Password: env["DB_PASSWORD"], LocalPort: localPort, Has: env["DB_HOST"] != "",
	}
	if err != nil {
		data.Error = err.Error()
	}
	s.chrome("Database").Render(w, xtkui.LangFromRequest(r), dbInfoTmpl, data)
}

// ---- SSH keys management page (view/edit authorized_keys + generate) ----

var keysTmpl = xtkui.LocParse("hostingkeys", subtabsSrc+`<h1>SSH keys · <code>site-{{.Name}}</code></h1>
{{if .Notice}}<div class="ok">{{.Notice}}</div>{{end}}
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<section><div class="card">
  <h3>Authorized public keys</h3>
  <p class="hint">One key per line — each grants SSH/SFTP access (port 2222, chrooted to the site). Edit freely.</p>
  <form method="post" action="/admin/hosting/users/keys">
    <input type="hidden" name="name" value="{{.Name}}">
    <textarea name="content" spellcheck="false" style="width:100%;min-height:8rem;font-family:var(--font-mono);font-size:.78rem;padding:.6rem;border:1px solid var(--line);border-radius:9px;background:var(--panel);color:var(--text);white-space:pre">{{.AuthKeys}}</textarea>
    <div class="actions"><a class="btn" href="/admin/hosting/users">Back to Users</a><button class="btn primary">Save keys</button></div>
  </form>
</div></section>
<section><div class="card addcard">
  <h3>Generate new keypair</h3>
  <form method="post" action="/admin/hosting/users/sshkey"><div class="formgrid">
    <input type="hidden" name="name" value="{{.Name}}">
    <div><label>Passphrase (optional)</label><input type="password" name="passphrase" autocomplete="new-password"></div>
    <div><label>Comment (optional)</label><input name="comment" placeholder="site-{{.Name}}@laptop"></div>
    <div><button class="btn primary">Generate &amp; append</button></div>
  </div></form>
  <p class="hint">Creates an ed25519 pair, appends its public key above, and shows the private key once (with a download).</p>
</div></section>`)

func (s *server) handleUserKeys(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	resp, err := s.callAgent(r.Context(), "hosting_user_authkeys_get", map[string]string{"name": name})
	ok, errMsg := notices(r)
	data := struct{ Tab, Name, AuthKeys, Notice, Error string }{Tab: "users", Name: name, Notice: ok, Error: errMsg}
	if err != nil || !resp.OK {
		if data.Error == "" {
			data.Error = "read keys: " + agentMsg(resp, err)
		}
	} else {
		data.AuthKeys = resp.Stdout
	}
	s.chrome("SSH keys").Render(w, xtkui.LangFromRequest(r), keysTmpl, data)
}

func (s *server) handleUserAuthKeysSet(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	resp, err := s.callAgent(r.Context(), "hosting_user_authkeys_set", map[string]string{"name": name, "content": r.FormValue("content")})
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting/users/keys?name="+url.QueryEscape(name), "", "save keys: "+agentMsg(resp, err))
		return
	}
	redirectMsg(w, r, "/admin/hosting/users/keys?name="+url.QueryEscape(name), "Authorized keys updated.", "")
}

func (s *server) handleAutoUpdate(w http.ResponseWriter, r *http.Request) {
	name, enabled := r.FormValue("name"), r.FormValue("enabled")
	resp, err := s.callAgent(r.Context(), "site_autoupdate", map[string]string{"name": name, "enabled": enabled})
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "auto-update: "+agentMsg(resp, err))
		return
	}
	redirectMsg(w, r, "/admin/hosting", "Auto-update for "+name+" set to "+enabled+".", "")
}

var editTmpl = xtkui.LocParse("hostingedit", subtabsSrc+`<h1>Edit compose · <code>{{.Name}}</code></h1>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<p class="hint">Editing <code>/opt/sites/{{.Name}}/docker-compose.yml</code>. On save it is validated with
<code>docker&nbsp;compose&nbsp;config</code> (reverted if invalid) and re-applied with <code>up&nbsp;-d</code>.</p>
<form method="post" action="/admin/hosting/edit">
  <input type="hidden" name="name" value="{{.Name}}">
  <textarea name="content" spellcheck="false" style="width:100%;min-height:26rem;font-family:var(--font-mono);font-size:.82rem;line-height:1.45;padding:.75rem;border:1px solid var(--line);border-radius:9px;background:var(--panel);color:var(--text);white-space:pre">{{.Content}}</textarea>
  <div class="actions">
    <a class="btn" href="/admin/hosting">Cancel</a>
    <button class="btn primary">Save &amp; apply</button>
  </div>
</form>`)

func (s *server) handleEdit(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	resp, err := s.callAgent(r.Context(), "site_compose_get", map[string]string{"name": name})
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "edit: "+agentMsg(resp, err))
		return
	}
	data := struct{ Tab, Name, Content, Error string }{Tab: "hosts", Name: name, Content: resp.Stdout}
	s.chrome("Edit compose").Render(w, xtkui.LangFromRequest(r), editTmpl, data)
}

func (s *server) handleEditSave(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	content := r.FormValue("content")
	resp, err := s.callAgent(r.Context(), "site_compose_set", map[string]string{"name": name, "content": content})
	if err != nil || !resp.OK {
		data := struct{ Tab, Name, Content, Error string }{Tab: "hosts", Name: name, Content: content, Error: agentMsg(resp, err)}
		s.chrome("Edit compose").Render(w, xtkui.LangFromRequest(r), editTmpl, data)
		return
	}
	redirectMsg(w, r, "/admin/hosting", "Compose for "+name+" updated and applied.", "")
}

func main() {
	socket := flag.String("socket", "/run/xtk-agent/agent.sock", "path to the xtk-agent unix socket (in a bind-mounted dir)")
	listen := flag.String("listen", ":8090", "internal HTTP listen address")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	s := &server{socket: *socket, log: log, adminer: map[string]*adminerSession{}}
	// clean up any Adminer containers orphaned by a previous run, then reap idle ones.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = s.callAgent(ctx, "adminer_gc", nil)
	}()
	go s.reapAdminer()

	mux := http.NewServeMux()
	// Hosts
	mux.HandleFunc("GET /admin/hosting", s.handleIndex)
	mux.HandleFunc("GET /admin/hosting/", s.handleIndex)
	mux.HandleFunc("POST /admin/hosting/create", s.handleCreate)
	mux.HandleFunc("POST /admin/hosting/up", s.action("site_up", "Site %s started."))
	mux.HandleFunc("POST /admin/hosting/down", s.action("site_down", "Site %s stopped."))
	mux.HandleFunc("POST /admin/hosting/destroy", s.action("site_destroy", "Site %s destroyed."))
	mux.HandleFunc("POST /admin/hosting/autoupdate", s.handleAutoUpdate)
	mux.HandleFunc("GET /admin/hosting/dbinfo", s.handleDbInfo)
	mux.HandleFunc("GET /admin/hosting/edit", s.handleEdit)
	mux.HandleFunc("POST /admin/hosting/edit", s.handleEditSave)
	// Users
	mux.HandleFunc("GET /admin/hosting/users", s.handleUsers)
	mux.HandleFunc("POST /admin/hosting/users/delete", s.handleUserDelete)
	mux.HandleFunc("POST /admin/hosting/users/sshd-install", s.handleSshdInstall)
	mux.HandleFunc("POST /admin/hosting/users/passwd", s.handleUserPasswd)
	mux.HandleFunc("POST /admin/hosting/users/lock", s.handleUserLock)
	mux.HandleFunc("POST /admin/hosting/users/sshkey", s.handleUserSshKey)
	mux.HandleFunc("GET /admin/hosting/users/keys", s.handleUserKeys)
	mux.HandleFunc("POST /admin/hosting/users/keys", s.handleUserAuthKeysSet)
	mux.HandleFunc("POST /admin/hosting/users/keydownload", s.handleKeyDownload)
	// MySQL / PgSQL
	for _, seg := range []string{"mysql", "pgsql"} {
		mux.HandleFunc("GET /admin/hosting/"+seg, s.handleDB(seg))
		mux.HandleFunc("POST /admin/hosting/"+seg+"/install", s.handleDBInstall(seg))
		mux.HandleFunc("POST /admin/hosting/"+seg+"/dbcreate", s.handleDBCreate(seg))
		mux.HandleFunc("POST /admin/hosting/"+seg+"/adminer/open", s.handleAdminerOpen(seg))
		proxy := s.handleAdminerProxy(seg) // GET+POST explicit (avoids method/path ambiguity with GET /admin/hosting/)
		mux.HandleFunc("GET /admin/hosting/"+seg+"/adminer/", proxy)
		mux.HandleFunc("POST /admin/hosting/"+seg+"/adminer/", proxy)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	srv := &http.Server{
		Addr: *listen, Handler: mux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second,
	}
	log.Info("hosting UI listening", "listen", *listen, "socket", *socket)
	if err := srv.ListenAndServe(); err != nil {
		log.Error("hosting UI failed", "err", err)
		os.Exit(1)
	}
}
