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
	"os"
	"sort"
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

// server holds the socket path and dial timeout; handlers call the agent through it.
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
		return nil, fmt.Errorf("site_list: %s%s", resp.Error, resp.Stderr)
	}
	var sites []site
	if err := json.Unmarshal([]byte(resp.Stdout), &sites); err != nil {
		return nil, fmt.Errorf("parse site_list: %w", err)
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Name < sites[j].Name })
	return sites, nil
}

var hostingNav = []xtkui.NavItem{
	{Key: "hosting", Href: "/admin/hosting", LabelKey: "nav.services"},
}

func (s *server) chrome(active string) xtkui.Chrome {
	return xtkui.Chrome{
		Title: "Xal-Tor-Ka · Hosting", BrandText: "⛬ Xal-Tor-Ka · Hosting", BrandHref: "/admin/hosting",
		Version: version.Version, Nav: hostingNav, Active: active,
		DashboardHref: "/admin", DashboardKey: "nav.admin", LoggedIn: true,
	}
}

// NOTE: the page copy is English-first for now; the hosting i18n keys are a follow-up
// (the shared chrome — brand, language cluster, RTL — is already localized).
var indexTmpl = xtkui.LocParse("hosting", `<h1>Hosting sites</h1>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<section>
  <table class="grid">
    <thead><tr><th>Site</th><th>Owner uid</th><th>Containers</th><th>Actions</th></tr></thead>
    <tbody>
    {{range .Sites}}
      <tr>
        <td><code>{{.Name}}</code> <span class="sub">{{.Name}}.site:8080</span></td>
        <td>{{.UID}}</td>
        <td>{{if gt .Running 0}}<span class="ok">running ({{.Running}})</span>{{else}}<span class="off">stopped</span>{{end}}</td>
        <td class="actions">
          <form method="post" action="/admin/hosting/up"><input type="hidden" name="name" value="{{.Name}}"><button>Up</button></form>
          <form method="post" action="/admin/hosting/down"><input type="hidden" name="name" value="{{.Name}}"><button>Down</button></form>
          <form method="post" action="/admin/hosting/destroy" onsubmit="return confirm('Destroy {{.Name}}? This removes its data and OS user.')"><input type="hidden" name="name" value="{{.Name}}"><button class="danger">Destroy</button></form>
        </td>
      </tr>
    {{else}}
      <tr><td colspan="4" class="sub">No sites yet.</td></tr>
    {{end}}
    </tbody>
  </table>
</section>
<section>
  <h2>New site</h2>
  <form method="post" action="/admin/hosting/create" class="row">
    <input name="name" placeholder="site name (a-z0-9-)" pattern="[a-z][a-z0-9-]{1,30}" required>
    <select name="template"><option value="php-fpm">php-fpm</option></select>
    <button>Create &amp; start</button>
  </form>
  <p class="sub">Provisions an isolated site (own OS user in <code>docker-hosting</code>), starts it on the
  <code>xtk-hosting</code> network, and reaches it at <code>&lt;name&gt;.site:8080</code>. Add a backend in
  Services to publish it.</p>
</section>`)

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Sites []site
		Error string
	}{}
	sites, err := s.listSites(r.Context())
	if err != nil {
		data.Error = err.Error()
		s.log.Warn("list sites failed", "err", err)
	}
	data.Sites = sites
	s.chrome("hosting").Render(w, xtkui.LangFromRequest(r), indexTmpl, data)
}

// action runs a single-name agent command then redirects back to the index.
func (s *server) action(cmd string, extra ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.FormValue("name")
		if _, err := s.callAgent(r.Context(), cmd, map[string]string{"name": name}); err != nil {
			s.log.Warn("action failed", "cmd", cmd, "err", err)
		}
		for _, next := range extra { // e.g. site_create then site_up
			if _, err := s.callAgent(r.Context(), next, map[string]string{"name": name}); err != nil {
				s.log.Warn("action failed", "cmd", next, "err", err)
			}
		}
		http.Redirect(w, r, "/admin/hosting", http.StatusSeeOther)
	}
}

func (s *server) handleCreate(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	tmpl := r.FormValue("template")
	if tmpl == "" {
		tmpl = "php-fpm"
	}
	if _, err := s.callAgent(r.Context(), "site_create", map[string]string{"name": name, "template": tmpl}); err != nil {
		s.log.Warn("site_create failed", "err", err)
		http.Redirect(w, r, "/admin/hosting", http.StatusSeeOther)
		return
	}
	if _, err := s.callAgent(r.Context(), "site_up", map[string]string{"name": name}); err != nil {
		s.log.Warn("site_up failed", "err", err)
	}
	http.Redirect(w, r, "/admin/hosting", http.StatusSeeOther)
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
	mux.HandleFunc("POST /admin/hosting/up", s.action("site_up"))
	mux.HandleFunc("POST /admin/hosting/down", s.action("site_down"))
	mux.HandleFunc("POST /admin/hosting/destroy", s.action("site_destroy"))
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
