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
	"net/url"
	"os"
	"sort"
	"strings"
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
}

type hostingUser struct {
	User string `json:"user"`
	UID  int    `json:"uid"`
	Site string `json:"site"`
	Home string `json:"home"`
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

// dbTab maps a URL segment (mysql|pgsql) to the agent engine name (mysql|pg) + label.
type dbTab struct{ Engine, Seg, Label string }

var dbTabs = map[string]dbTab{
	"mysql": {Engine: "mysql", Seg: "mysql", Label: "MySQL"},
	"pgsql": {Engine: "pg", Seg: "pgsql", Label: "PgSQL"},
}

type server struct {
	socket string
	log    *slog.Logger
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
	http.Redirect(w, r, path+"?"+q.Encode(), http.StatusSeeOther)
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
          <form class="inline" method="post" action="/admin/hosting/up"><input type="hidden" name="name" value="{{.Name}}"><button class="btn sm">Up</button></form>
          <form class="inline" method="post" action="/admin/hosting/down"><input type="hidden" name="name" value="{{.Name}}"><button class="btn sm">Down</button></form>
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
<p class="hint">OS accounts that own sites — each a system user in the <code>docker-hosting</code>
group (nologin). Their containers run as this uid:gid.</p>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<section>
  <table>
    <thead><tr><th>Site</th><th>OS user</th><th>uid</th><th>Home</th></tr></thead>
    <tbody>
    {{range .Users}}
      <tr><td><b>{{.Site}}</b></td><td><code>{{.User}}</code></td><td><code>{{.UID}}</code></td><td><code>{{.Home}}</code></td></tr>
    {{else}}
      <tr><td colspan="4" class="hint">No site users yet.</td></tr>
    {{end}}
    </tbody>
  </table>
</section>`)

func (s *server) handleUsers(w http.ResponseWriter, r *http.Request) {
	var users []hostingUser
	err := s.callJSON(r.Context(), "hosting_users", nil, &users)
	sort.Slice(users, func(i, j int) bool { return users[i].Site < users[j].Site })
	data := struct {
		Tab   string
		Users []hostingUser
		Error string
	}{Tab: "users", Users: users}
	if err != nil {
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
	socket := flag.String("socket", "/run/xtk-agent.sock", "path to the xtk-agent unix socket")
	listen := flag.String("listen", ":8090", "internal HTTP listen address")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	s := &server{socket: *socket, log: log}

	mux := http.NewServeMux()
	// Hosts
	mux.HandleFunc("GET /admin/hosting", s.handleIndex)
	mux.HandleFunc("GET /admin/hosting/", s.handleIndex)
	mux.HandleFunc("POST /admin/hosting/create", s.handleCreate)
	mux.HandleFunc("POST /admin/hosting/up", s.action("site_up", "Site %s started."))
	mux.HandleFunc("POST /admin/hosting/down", s.action("site_down", "Site %s stopped."))
	mux.HandleFunc("POST /admin/hosting/destroy", s.action("site_destroy", "Site %s destroyed."))
	mux.HandleFunc("GET /admin/hosting/edit", s.handleEdit)
	mux.HandleFunc("POST /admin/hosting/edit", s.handleEditSave)
	// Users
	mux.HandleFunc("GET /admin/hosting/users", s.handleUsers)
	// MySQL / PgSQL
	for _, seg := range []string{"mysql", "pgsql"} {
		mux.HandleFunc("GET /admin/hosting/"+seg, s.handleDB(seg))
		mux.HandleFunc("POST /admin/hosting/"+seg+"/install", s.handleDBInstall(seg))
		mux.HandleFunc("POST /admin/hosting/"+seg+"/dbcreate", s.handleDBCreate(seg))
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
