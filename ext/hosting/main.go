// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Command xtk-hosting-ui is the hosting extension's web UI: an internal service that
// renders a site-management panel (shared xtkui chrome) and drives the privileged
// xtk-agent over its unix socket. It has NO host powers of its own — every mutating
// action is a vetted agent command. The gateway reverse-proxies it under the admin
// host (/admin/hosting), auth-gated; it is never exposed directly.
package main

import (
	"context"
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

// site is one row of the site_list agent command's JSON output.
type site struct {
	Name    string `json:"name"`
	UID     int    `json:"uid"`
	Running int    `json:"running"`
}

// server holds the socket path; handlers call the agent through it.
type server struct {
	socket string
	log    *slog.Logger
}

// callAgent runs one vetted command over the unix socket and returns its response.
// One request per connection (matches the agent protocol).
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
		_ = uc.CloseWrite() // signal end-of-request; the agent then replies
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return resp, fmt.Errorf("read %s: %w", cmd, err)
	}
	return resp, nil
}

func (s *server) listSites(ctx context.Context) ([]site, error) {
	resp, err := s.callAgent(ctx, "site_list", nil)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("site_list: %s", agentMsg(resp, nil))
	}
	var sites []site
	if err := json.Unmarshal([]byte(resp.Stdout), &sites); err != nil {
		return nil, fmt.Errorf("parse site_list: %w", err)
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Name < sites[j].Name })
	return sites, nil
}

// agentMsg extracts a short, human-readable message from an agent result: the dial
// error, else the command's error/stderr first line.
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

// redirectMsg sends the browser back to the index with a one-shot ok/err notice.
func redirectMsg(w http.ResponseWriter, r *http.Request, ok, errMsg string) {
	q := url.Values{}
	if errMsg != "" {
		q.Set("err", errMsg)
	} else if ok != "" {
		q.Set("ok", ok)
	}
	http.Redirect(w, r, "/admin/hosting?"+q.Encode(), http.StatusSeeOther)
}

var hostingNav = []xtkui.NavItem{
	{Key: "hosting", Href: "/admin/hosting", LabelKey: "nav.services"},
}

func (s *server) chrome(title, active string) xtkui.Chrome {
	return xtkui.Chrome{
		Title: "Xal-Tor-Ka · " + title, BrandText: "⛬ Xal-Tor-Ka · Hosting", BrandHref: "/admin/hosting",
		Version: version.Version, Nav: hostingNav, Active: active,
		DashboardHref: "/admin", DashboardKey: "nav.admin", LoggedIn: true,
	}
}

// NOTE: page copy is English-first for now; hosting i18n keys are a follow-up (the
// shared chrome — brand, language cluster, RTL — is already localized).
var indexTmpl = xtkui.LocParse("hosting", `<h1>Hosting sites</h1>
{{if .Notice}}<div class="ok">{{.Notice}}</div>{{end}}
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<section>
  <table>
    <thead><tr><th>Site</th><th>Upstream</th><th>Owner</th><th>Status</th><th></th></tr></thead>
    <tbody>
    {{range .Sites}}
      <tr{{if not .Running}} class="off"{{end}}>
        <td><b>{{.Name}}</b></td>
        <td><code>{{.Name}}.site:8080</code></td>
        <td><code>{{.UID}}</code></td>
        <td>{{if gt .Running 0}}<span class="tag ext">running · {{.Running}}</span>{{else}}<span class="tag ro">stopped</span>{{end}}</td>
        <td class="rowact">
          <form class="inline" method="post" action="/admin/hosting/up"><input type="hidden" name="name" value="{{.Name}}"><button class="btn sm">Up</button></form>
          <form class="inline" method="post" action="/admin/hosting/down"><input type="hidden" name="name" value="{{.Name}}"><button class="btn sm">Down</button></form>
          <a class="btn sm" href="/admin/hosting/edit?name={{.Name}}">Edit compose</a>
          <form class="inline" method="post" action="/admin/hosting/destroy" onsubmit="return confirm('Destroy {{.Name}}? This removes its data and OS user.')"><input type="hidden" name="name" value="{{.Name}}"><button class="btn danger sm">Destroy</button></form>
        </td>
      </tr>
    {{else}}
      <tr><td colspan="5" class="hint">No sites yet.</td></tr>
    {{end}}
    </tbody>
  </table>
</section>
<section>
  <div class="card addcard" style="margin-top:1rem">
    <h3>New site</h3>
    <form method="post" action="/admin/hosting/create"><div class="formgrid">
      <div><label>Name</label><input name="name" placeholder="a-z0-9-" pattern="[a-z][a-z0-9-]{1,30}" required></div>
      <div><label>Template</label><select name="template"><option value="php-fpm">php-fpm</option></select></div>
      <div><button class="btn primary">Create &amp; start</button></div>
    </div></form>
    <p class="hint">Provisions an isolated site (own OS user in <code>docker-hosting</code>), starts it on the
    <code>xtk-hosting</code> network, reachable at <code>&lt;name&gt;.site:8080</code>. Add a backend in
    Services to publish it.</p>
  </div>
</section>`)

var editTmpl = xtkui.LocParse("hostingedit", `<h1>Edit compose · <code>{{.Name}}</code></h1>
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

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Sites         []site
		Notice, Error string
	}{
		Notice: r.URL.Query().Get("ok"),
		Error:  r.URL.Query().Get("err"),
	}
	sites, err := s.listSites(r.Context())
	if err != nil {
		if data.Error == "" {
			data.Error = err.Error()
		}
		s.log.Warn("list sites failed", "err", err)
	}
	data.Sites = sites
	s.chrome("Hosting", "hosting").Render(w, xtkui.LangFromRequest(r), indexTmpl, data)
}

// action runs a single-name agent command then redirects back with feedback.
func (s *server) action(cmd, okMsg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.FormValue("name")
		resp, err := s.callAgent(r.Context(), cmd, map[string]string{"name": name})
		if err != nil || !resp.OK {
			s.log.Warn("action failed", "cmd", cmd, "name", name, "err", agentMsg(resp, err))
			redirectMsg(w, r, "", cmd+": "+agentMsg(resp, err))
			return
		}
		redirectMsg(w, r, fmt.Sprintf(okMsg, name), "")
	}
}

func (s *server) handleCreate(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	tmpl := r.FormValue("template")
	if tmpl == "" {
		tmpl = "php-fpm"
	}
	if resp, err := s.callAgent(r.Context(), "site_create", map[string]string{"name": name, "template": tmpl}); err != nil || !resp.OK {
		redirectMsg(w, r, "", "create: "+agentMsg(resp, err))
		return
	}
	if resp, err := s.callAgent(r.Context(), "site_up", map[string]string{"name": name}); err != nil || !resp.OK {
		redirectMsg(w, r, "", "site created but start failed: "+agentMsg(resp, err))
		return
	}
	redirectMsg(w, r, "Site "+name+" created and started.", "")
}

func (s *server) handleEdit(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	resp, err := s.callAgent(r.Context(), "site_compose_get", map[string]string{"name": name})
	if err != nil || !resp.OK {
		redirectMsg(w, r, "", "edit: "+agentMsg(resp, err))
		return
	}
	data := struct{ Name, Content, Error string }{Name: name, Content: resp.Stdout}
	s.chrome("Edit compose", "hosting").Render(w, xtkui.LangFromRequest(r), editTmpl, data)
}

func (s *server) handleEditSave(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	content := r.FormValue("content")
	resp, err := s.callAgent(r.Context(), "site_compose_set", map[string]string{"name": name, "content": content})
	if err != nil || !resp.OK {
		// Re-render the editor with the (rejected) content so the admin can fix it.
		data := struct{ Name, Content, Error string }{Name: name, Content: content, Error: agentMsg(resp, err)}
		s.chrome("Edit compose", "hosting").Render(w, xtkui.LangFromRequest(r), editTmpl, data)
		return
	}
	redirectMsg(w, r, "Compose for "+name+" updated and applied.", "")
}

func main() {
	socket := flag.String("socket", "/run/xtk-agent.sock", "path to the xtk-agent unix socket")
	listen := flag.String("listen", ":8090", "internal HTTP listen address")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	s := &server{socket: *socket, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/hosting", s.handleIndex)
	mux.HandleFunc("GET /admin/hosting/", s.handleIndex)
	mux.HandleFunc("POST /admin/hosting/create", s.handleCreate)
	mux.HandleFunc("POST /admin/hosting/up", s.action("site_up", "Site %s started."))
	mux.HandleFunc("POST /admin/hosting/down", s.action("site_down", "Site %s stopped."))
	mux.HandleFunc("POST /admin/hosting/destroy", s.action("site_destroy", "Site %s destroyed."))
	mux.HandleFunc("GET /admin/hosting/edit", s.handleEdit)
	mux.HandleFunc("POST /admin/hosting/edit", s.handleEditSave)
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
