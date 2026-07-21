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
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
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

// vhost is one docker (web[+php]) of a site: its own docroot, logs, stack and gateway
// upstream. Emitted nested inside each site by site_list.
type vhost struct {
	Vhost      string `json:"vhost"`
	Domain     string `json:"domain"` // public domain (display + publish); empty = show the vhost slug
	Template   string `json:"template"`
	PhpVersion string `json:"php_version"`
	Db         string `json:"db"`
	AutoUpdate bool   `json:"auto_update"`
	Running    int    `json:"running"`
	Upstream   string `json:"upstream"` // http://<alias>:8080
	Published  string `json:"-"`        // ext-computed: up | down | none (gateway backend state)
	SSL        string `json:"-"`        // ext-computed: up | down | none (TLS cert state)
	PubHost    string `json:"-"`        // the backend's public host (for the SSL link + cert lookup)
	PubRule    string `json:"-"`        // current backend rule (to pre-fill the Publish dialog)
	PubWWW     bool   `json:"-"`        // current backend www flag (pre-checks the www box)
}

type site struct {
	Name       string  `json:"name"`
	Domain     string  `json:"domain"` // primary domain (= httpdocs vhost's); empty = show the slug
	UID        int     `json:"uid"`
	Running    int     `json:"running"`     // total across vhosts
	Template   string  `json:"template"`    // flat = httpdocs (backward-compat)
	PhpVersion string  `json:"php_version"` // flat = httpdocs
	Db         string  `json:"db"`          // flat = httpdocs
	AutoUpdate bool    `json:"auto_update"` // flat = httpdocs
	Legacy     bool    `json:"legacy"`      // true = single-docker, not yet migrated
	Vhosts     []vhost `json:"vhosts"`
	Published  string  `json:"-"` // ext-computed: state of the httpdocs vhost's backend
	SSL        string  `json:"-"` // ext-computed: TLS cert state of the httpdocs vhost
	PubHost    string  `json:"-"` // the httpdocs vhost's published host
}

type hostingUser struct {
	User   string `json:"user"`
	UID    int    `json:"uid"`
	Site   string `json:"site"`
	Home   string `json:"home"`
	Orphan bool   `json:"orphan"`
	Scp    string `json:"scp"`  // on | off | none
	Keys   int    `json:"keys"` // count of authorized_keys entries
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
	socket       string
	servicesPath string // read-only path to the core's services.json (backend state)
	certsPath    string // read-only path to the certs dir (SSL button state)
	log          *slog.Logger
	mu           sync.Mutex
	adminer      map[string]*adminerSession // token → session
}

// bkInfo is what the Hosts tab needs about a gateway backend: its state, the public
// host it serves (for the cert + SSL link), and its current rule/www (to pre-fill the
// Publish dialog so re-publishing preserves them).
type bkInfo struct {
	State, Host, Rule string
	WWW               bool
}

// backendStates reads the core's services.json (mounted read-only) and returns a map
// upstream -> bkInfo for every backend route. A vhost is matched by its upstream
// (http://<alias>:8080), so this works for backends published from Hosting AND legacy
// backends published before the Hosting marker existed. Degrades to an empty map if the
// file is absent/unreadable.
func (s *server) backendStates() (byUp, byHost map[string]bkInfo) {
	byUp, byHost = map[string]bkInfo{}, map[string]bkInfo{}
	if s.servicesPath == "" {
		return
	}
	data, err := os.ReadFile(s.servicesPath)
	if err != nil {
		return
	}
	var f struct {
		Backends []struct {
			Disabled bool   `json:"disabled"`
			Host     string `json:"host"`
			WWW      bool   `json:"www"`
			Routes   []struct {
				Upstream string `json:"upstream"`
				Rule     string `json:"rule"`
			} `json:"routes"`
		} `json:"backends"`
	}
	if json.Unmarshal(data, &f) != nil {
		return
	}
	for _, b := range f.Backends {
		st := "up"
		if b.Disabled {
			st = "down"
		}
		rule := ""
		if len(b.Routes) > 0 {
			rule = b.Routes[0].Rule
		}
		for _, rt := range b.Routes {
			if rt.Upstream != "" {
				byUp[rt.Upstream] = bkInfo{State: st, Host: b.Host, Rule: rt.Rule, WWW: b.WWW}
			}
		}
		// Also index by public host, so a vhost whose upstream diverges from the
		// published backend (e.g. a domain served by a standalone/legacy container)
		// still resolves its Publish/SSL state by its domain instead of losing it.
		if b.Host != "" {
			byHost[b.Host] = bkInfo{State: st, Host: b.Host, Rule: rule, WWW: b.WWW}
		}
	}
	return
}

// certState reports the TLS certificate state for host: "up" (a valid cert exists),
// "down" (a cert exists but has expired), "none" (no cert / not published). Reads only
// the world-readable <host>.crt from the certs mount; the 0600 private keys stay
// unreadable to this non-root container. Degrades to "none" if unreadable.
func (s *server) certState(host string) string {
	if host == "" || s.certsPath == "" {
		return "none"
	}
	data, err := os.ReadFile(filepath.Join(s.certsPath, host+".crt"))
	if err != nil {
		return "none"
	}
	blk, _ := pem.Decode(data)
	if blk == nil {
		return "none"
	}
	c, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return "none"
	}
	if time.Now().Before(c.NotAfter) {
		return "up"
	}
	return "down"
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
<a href="/admin/hosting/system"{{if eq .Tab "system"}} class="active"{{end}}>System</a>
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
	byUp, byHost := s.backendStates()
	for i := range sites {
		// The primary vhost "httpdocs" is ALWAYS shown first (it's the site's main
		// domain); the additional vhosts follow, alphabetically. Not plain a-z, which
		// would bury httpdocs among app/api/staging.
		sort.SliceStable(sites[i].Vhosts, func(a, b int) bool {
			va, vb := sites[i].Vhosts[a].Vhost, sites[i].Vhosts[b].Vhost
			if va == "httpdocs" {
				return true
			}
			if vb == "httpdocs" {
				return false
			}
			return va < vb
		})
		sites[i].Published, sites[i].SSL = "none", "none"
		for j := range sites[i].Vhosts {
			v := &sites[i].Vhosts[j]
			// The host this vhost serves: its own domain, or (for the primary
			// httpdocs vhost) the site's domain. Empty when no domain is recorded.
			servedHost := v.Domain
			if servedHost == "" && v.Vhost == "httpdocs" {
				servedHost = sites[i].Domain
			}
			// Match the gateway backend by upstream (hosting-published) first, then
			// fall back to the served host — so a domain published with a divergent
			// upstream (legacy/standalone container) keeps its state instead of "none".
			info, ok := byUp[v.Upstream]
			if !ok && servedHost != "" {
				info = byHost[servedHost]
			}
			v.Published = info.State
			if v.Published == "" {
				v.Published = "none"
			}
			v.PubRule = info.Rule
			v.PubWWW = info.WWW
			// Public host for the SSL link + Publish dialog: the matched backend's
			// host, else the vhost's served domain.
			v.PubHost = info.Host
			if v.PubHost == "" {
				v.PubHost = servedHost
			}
			// The cert is keyed by the served DOMAIN (independent of which upstream
			// serves it); fall back to the matched backend host.
			certHost := servedHost
			if certHost == "" {
				certHost = info.Host
			}
			v.SSL = s.certState(certHost)
			if v.Vhost == "httpdocs" {
				sites[i].Published, sites[i].SSL, sites[i].PubHost = v.Published, v.SSL, v.PubHost
			}
		}
	}
	return sites, nil
}

var indexTmpl = xtkui.LocParse("hosting", subtabsSrc+`<h1>Hosts</h1>
{{if .Notice}}<div class="ok">{{.Notice}}</div>{{end}}
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<section class="hostlist">
{{range .Sites}}{{$site := .}}
  <details class="hostcard"{{if gt .Running 0}} open{{end}}>
    <summary>
      <span class="hc-name">{{if .Domain}}{{.Domain}}{{else}}{{.Name}}{{end}}</span>
      <span class="hc-stack">{{if .Template}}{{.Template}}{{if .PhpVersion}} · {{.PhpVersion}}{{end}}{{else}}—{{end}}</span>
      <span class="hc-owner"><code>site-{{.Name}}</code> · uid {{.UID}}</span>
      <span class="hc-badges">{{if gt .Running 0}}<span class="tag ext">running · {{.Running}}</span>{{else}}<span class="tag ro">stopped</span>{{end}}{{if .Legacy}} <span class="tag ro">legacy</span>{{end}}</span>
      <span class="hc-sumact">
        <button class="btn sm st-{{if .Published}}{{.Published}}{{else}}none{{end}}" type="button" onclick="event.stopPropagation();document.getElementById('pub-{{.Name}}-httpdocs').showModal()" title="gateway backend: {{if eq .Published "up"}}published (active){{else if eq .Published "down"}}disabled{{else}}not published{{end}}">Publish</button>
        <a class="btn sm st-{{if .SSL}}{{.SSL}}{{else}}none{{end}}" href="/admin/tls{{if .Domain}}?site={{.Name}}{{end}}" onclick="event.stopPropagation()" title="TLS certificate: {{if eq .SSL "up"}}valid{{else if eq .SSL "down"}}expired{{else}}none{{end}} — for {{if .Domain}}{{.Domain}}{{else}}{{.Name}}{{end}} and its vhosts">SSL</a>
        <button class="btn sm" type="submit" form="su-{{.Name}}" onclick="event.stopPropagation()">Start all</button>
        <button class="btn sm" type="submit" form="sd-{{.Name}}" onclick="event.stopPropagation()">Stop all</button>
        <button class="btn danger sm" type="submit" form="sx-{{.Name}}" onclick="event.stopPropagation()">Destroy</button>
      </span>
    </summary>
    <div class="hc-body">
      <form id="su-{{.Name}}" method="post" action="/admin/hosting/up"><input type="hidden" name="name" value="{{.Name}}"></form>
      <form id="sd-{{.Name}}" method="post" action="/admin/hosting/down"><input type="hidden" name="name" value="{{.Name}}"></form>
      <form id="sx-{{.Name}}" method="post" action="/admin/hosting/destroy" onsubmit="return confirm('Destroy {{.Name}} and ALL its vhosts + OS user?')"><input type="hidden" name="name" value="{{.Name}}"></form>
      {{if .Legacy}}
      <div class="hc-legacy">⚠ Single-docker (legacy) layout.
        <form class="inline" method="post" action="/admin/hosting/migrate"><input type="hidden" name="name" value="{{.Name}}"><button class="btn sm">Migrate to multi-vhost</button></form>
        <span class="hint">— unlocks per-vhost logs and adding more vhosts.</span>
      </div>
      {{end}}
      <div class="vhosts">
      {{range .Vhosts}}
        <div class="vhost">
          <div class="vh-head">
            <b>{{if .Domain}}{{.Domain}}{{else}}{{.Vhost}}{{end}}</b>{{if .Domain}} <span class="hint">{{.Vhost}}</span>{{end}}
            <code>{{.Template}}{{if .PhpVersion}} · {{.PhpVersion}}{{end}}</code>
            {{if gt .Running 0}}<span class="tag ext">running · {{.Running}}</span>{{else}}<span class="tag ro">stopped</span>{{end}}
            <span class="vh-act-h">
              <button class="btn sm st-{{if .Published}}{{.Published}}{{else}}none{{end}}" type="button" onclick="document.getElementById('pub-{{$site.Name}}-{{.Vhost}}').showModal()" title="gateway backend: {{if eq .Published "up"}}published (active){{else if eq .Published "down"}}disabled{{else}}not published{{end}}">Publish</button>
              <a class="btn sm st-{{if .SSL}}{{.SSL}}{{else}}none{{end}}" href="/admin/tls{{if .PubHost}}#h-{{.PubHost}}{{end}}" title="TLS certificate: {{if eq .SSL "up"}}valid{{else if eq .SSL "down"}}expired{{else}}none{{end}}">SSL</a>
              <form class="inline" method="post" action="/admin/hosting/vhost/up"><input type="hidden" name="name" value="{{$site.Name}}"><input type="hidden" name="vhost" value="{{.Vhost}}"><button class="btn sm">Start</button></form>
              <form class="inline" method="post" action="/admin/hosting/vhost/down"><input type="hidden" name="name" value="{{$site.Name}}"><input type="hidden" name="vhost" value="{{.Vhost}}"><button class="btn sm">Stop</button></form>
              <form class="inline" method="post" action="/admin/hosting/vhost/destroy" onsubmit="return confirm('Destroy vhost {{.Vhost}}?')"><input type="hidden" name="name" value="{{$site.Name}}"><input type="hidden" name="vhost" value="{{.Vhost}}"><button class="btn danger sm"{{if eq .Vhost "httpdocs"}} disabled title="the first vhost can't be destroyed — use Destroy site"{{end}}>Destroy</button></form>
            </span>
          </div>
          <div class="vh-info">
            <span>upstream <code>{{.Upstream}}</code></span>
            <span>DB {{if .Db}}<code>{{.Db}}</code> · <a href="/admin/hosting/dbinfo?name={{$site.Name}}&amp;vhost={{.Vhost}}">connection →</a>{{else}}<span class="hint">none</span>{{end}}</span>
            <span>logs <code>logs/{{.Vhost}}/</code></span>
            <span class="vh-act-i">
              <a class="btn sm" href="/admin/hosting/edit?name={{$site.Name}}{{if not $site.Legacy}}&amp;vhost={{.Vhost}}{{end}}">Compose</a>
              <form class="inline" method="post" action="/admin/hosting/vhost/autoupdate"><input type="hidden" name="name" value="{{$site.Name}}"><input type="hidden" name="vhost" value="{{.Vhost}}"><input type="hidden" name="enabled" value="{{if .AutoUpdate}}false{{else}}true{{end}}"><button class="btn sm">{{if .AutoUpdate}}Auto-update off{{else}}Auto-update on{{end}}</button></form>
              <a class="btn sm" href="/admin/hosting/nginx?name={{$site.Name}}{{if not $site.Legacy}}&amp;vhost={{.Vhost}}{{end}}">Nginx</a>
            </span>
          </div>
          <dialog class="dlg" id="pub-{{$site.Name}}-{{.Vhost}}">
            <form method="dialog" class="dlg-x"><button class="btn sm" aria-label="Close">✕</button></form>
            <h3>Publish {{if .Domain}}{{.Domain}}{{else if .PubHost}}{{.PubHost}}{{else}}{{$site.Name}}/{{.Vhost}}{{end}}</h3>
            <div class="meta">Upstream <code>{{.Upstream}}</code> <span class="hint">(managed by hosting — fixed)</span></div>
            <form method="post" action="/admin/backend/add">
              <input type="hidden" name="id" value="{{if eq .Vhost "httpdocs"}}{{$site.Name}}{{else}}{{$site.Name}}-{{.Vhost}}{{end}}">
              <input type="hidden" name="name" value="{{$site.Name}}/{{.Vhost}} (hosting)">
              <input type="hidden" name="upstream" value="{{.Upstream}}">
              <input type="hidden" name="path" value="/">
              <input type="hidden" name="hosting_site" value="{{$site.Name}}">
              <input type="hidden" name="hosting_vhost" value="{{.Vhost}}">
              <div class="formgrid">
                <div><label>Public host (this vhost's domain)</label><input name="host" value="{{if .Domain}}{{.Domain}}{{else}}{{.PubHost}}{{end}}" placeholder="mysite.example.com" required autofocus></div>
                <div><label>Rule</label><select name="rule"><option{{if eq .PubRule "public"}} selected{{end}}>public</option><option{{if eq .PubRule "authenticated"}} selected{{end}}>authenticated</option><option{{if eq .PubRule "whitelist"}} selected{{end}}>whitelist</option></select></div>
                <div><label class="hint" style="display:inline-flex;align-items:center;gap:.4rem"><input type="checkbox" name="www" value="1"{{if .PubWWW}} checked{{end}}> also serve &amp; cert <b>www.</b></label></div>
                <div><button class="btn primary">Publish backend</button></div>
              </div>
            </form>
            <p class="hint">Creates the gateway backend for this vhost's domain (plus <code>www.</code> if ticked — that's where www is managed). Then issue the certificate with the <b>SSL</b> button.</p>
          </dialog>
        </div>
      {{end}}
      </div>
      {{if not .Legacy}}
      <form class="hc-addvhost" method="post" action="/admin/hosting/vhost/create">
        <input type="hidden" name="name" value="{{.Name}}">
        <input name="vhost" placeholder="name (e.g. app)" pattern="[a-z][a-z0-9-]{1,30}" required>
        <input name="domain" placeholder="domain (optional)" pattern="[a-z0-9]([a-z0-9.-]*[a-z0-9])?">
        <select name="stack">
          <option value="php-fpm:8.3">PHP-FPM 8.3</option>
          <option value="php-fpm:8.2">PHP-FPM 8.2</option>
          <option value="php-fpm:8.1">PHP-FPM 8.1</option>
          <option value="php-fpm:7.4">PHP-FPM 7.4</option>
          <option value="static">Static</option>
          <option value="custom">Custom</option>
        </select>
        <button class="btn sm primary">+ Add vhost</button>
      </form>
      {{end}}
      <div class="hc-foot">
        <span>Owner <a href="/admin/hosting/users?open={{.Name}}"><code>site-{{.Name}}</code></a> · <a href="/admin/hosting/users/keys?name={{.Name}}">SSH keys</a></span>
      </div>
    </div>
  </details>
{{else}}
  <p class="hint">No sites yet.</p>
{{end}}
</section>
<section>
  <div class="card addcard" style="margin-top:1rem">
    <h3>New site</h3>
    <form method="post" action="/admin/hosting/create"><div class="formgrid">
      <div><label>Domain</label><input name="domain" placeholder="example.com" pattern="[a-z0-9]([a-z0-9.-]*[a-z0-9])?" required></div>
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
    <p class="hint">Enter the site's <b>domain</b> — the internal names (OS user, docker, DB) are derived from it
    automatically. Provisions an isolated site (own OS user in <code>docker-hosting</code>) on the
    <code>xtk-hosting</code> network, started and ready to publish.</p>
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
SCP/SFTP, chrooted to the site dir — upload into <code>httpdocs/</code>.</p>
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
      <tr id="u-{{.Site}}"{{if .Orphan}} class="off"{{end}}>
        <td><b>{{.Site}}</b>{{if .Orphan}} <span class="tag ro">orphan</span>{{end}}</td>
        <td><code>{{.User}}</code></td><td><code>{{.UID}}</code></td>
        <td>{{if eq .Scp "on"}}<span class="tag ext">enabled</span>{{else if eq .Scp "off"}}<span class="tag ro">disabled</span>{{else}}<span class="hint">no password</span>{{end}}</td>
        <td class="rowact">
          <button class="btn sm" type="button" onclick="document.getElementById('ud-{{.Site}}').showModal()">Edit</button>
          <dialog class="dlg" id="ud-{{.Site}}">
            <form method="dialog" class="dlg-x"><button class="btn sm" aria-label="Close">✕</button></form>
            <h3>{{.User}}</h3>
            <div class="meta">Site: <code>{{.Site}}</code>{{if .Orphan}} <span class="tag ro">orphan</span>{{end}} · uid <code>{{.UID}}</code></div>
            <div class="meta">SCP access: {{if eq .Scp "on"}}<b>enabled</b>{{else if eq .Scp "off"}}<b>disabled</b>{{else}}no password set{{end}} · <b>{{.Keys}}</b> SSH key{{if ne .Keys 1}}s{{end}}</div>
            <div class="meta hint">An SSH key works instead of a password for SCP/SFTP.</div>
            <form method="post" action="/admin/hosting/users/passwd" style="margin:.9rem 0 .3rem">
              <input type="hidden" name="name" value="{{.Site}}">
              <div class="formgrid"><div><label>Set SCP password (min 8)</label><input type="password" name="password" minlength="8" required autofocus></div>
              <div><button class="btn primary">Set password</button></div></div>
            </form>
            <div class="actions" style="justify-content:flex-start">
              <a class="btn sm" href="/admin/hosting/users/keys?name={{.Site}}">SSH keys{{if .Keys}} ({{.Keys}}){{end}}</a>
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
</section>
<script>(function(){var o=new URLSearchParams(location.search).get("open");if(!o)return;var d=document.getElementById("ud-"+o);if(d&&d.showModal)d.showModal();})();</script>`)

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

// ---------------------------------------------------------------- System (OS updates) tab

type osUpdate struct {
	Name      string `json:"name"`
	Current   string `json:"current"`
	Candidate string `json:"candidate"`
	Security  bool   `json:"security"`
	Held      bool   `json:"held"`
}

type osUpdatesResult struct {
	Total          int        `json:"total"`
	Security       int        `json:"security"`
	Held           int        `json:"held"`
	RebootRequired bool       `json:"reboot_required"`
	Packages       []osUpdate `json:"packages"`
	Note           string     `json:"note,omitempty"`
}

// fail2banStatus mirrors the fail2ban_status agent command output.
type fail2banStatus struct {
	Enabled         bool     `json:"enabled"`
	Jail            string   `json:"jail"`
	CurrentlyBanned int      `json:"currently_banned"`
	TotalBanned     int      `json:"total_banned"`
	CurrentlyFailed int      `json:"currently_failed"`
	Banned          []string `json:"banned"`
	Note            string   `json:"note"`
}

var systemTmpl = xtkui.LocParse("hostingsystem", subtabsSrc+`<h1>System updates</h1>
<p class="hint">Available OS package updates on the host, checked via the vetted agent — the check is <b>read-only</b> (<code>apt</code>).</p>
{{if .Notice}}<div class="ok">{{.Notice}}</div>{{end}}
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<section><div class="card">
  <div class="row">
    <h3>{{if .Res.Total}}{{.Res.Total}} update{{if ne .Res.Total 1}}s{{end}} available{{else}}System up to date{{end}}</h3>
    {{if .Res.Security}}<span class="tag ro">{{.Res.Security}} security</span>{{end}}
    {{if .Res.RebootRequired}}<span class="tag ro">reboot required</span>{{end}}
  </div>
  {{if .Res.Note}}<div class="meta hint">{{.Res.Note}}</div>{{else}}<div class="meta hint">Applying updates is admin-gated and never reboots on its own.</div>{{end}}
</div></section>
<section><div class="card">
  <div class="row"><h3>Firewall — fail2ban</h3>
    {{if .F2b.Enabled}}<span class="tag ext">active</span>{{else}}<span class="tag ro">not configured</span>{{end}}
  </div>
  {{if .F2b.Enabled}}
    <div class="meta hint">Jail <code>{{.F2b.Jail}}</code> · currently banned {{.F2b.CurrentlyBanned}} · total {{.F2b.TotalBanned}} · failed {{.F2b.CurrentlyFailed}}. Bans hit tcp 80/443 only (never SSH); admin IPs + LAN are whitelisted.</div>
    {{if .F2b.Banned}}
    <table style="margin-top:.6rem"><thead><tr><th>Banned IP</th><th></th></tr></thead><tbody>
      {{range .F2b.Banned}}<tr><td><code>{{.}}</code></td>
        <td class="rowact"><form class="inline" method="post" action="/admin/hosting/system/unban"><input type="hidden" name="ip" value="{{.}}"><button class="btn sm">Unban</button></form></td></tr>{{end}}
    </tbody></table>
    {{else}}<div class="meta hint" style="margin-top:.4rem">No IPs currently banned.</div>{{end}}
  {{else}}<div class="meta hint">{{if .F2b.Note}}{{.F2b.Note}}{{else}}fail2ban jail not configured on this host.{{end}}</div>{{end}}
</div></section>
{{if .Res.Packages}}
<section>
  <div class="actions" style="justify-content:flex-start;margin-bottom:.7rem">
    <button class="btn primary sm" type="button" onclick="xtkApply()">Apply selected…</button>
    <button class="btn sm" type="button" onclick="xtkSel('all')">Select all</button>
    <button class="btn sm" type="button" onclick="xtkSel('sec')">Select security</button>
    <button class="btn sm" type="button" onclick="xtkSel('none')">Clear</button>
  </div>
  <table>
    <thead><tr><th style="width:2.2rem"></th><th>Package</th><th>Current</th><th>Candidate</th><th></th><th></th></tr></thead>
    <tbody>
    {{range .Res.Packages}}
      <tr{{if .Held}} class="off"{{end}}>
        <td>{{if not .Held}}<input type="checkbox" name="pkg" value="{{.Name}}"{{if .Security}} data-sec="1"{{end}} aria-label="select {{.Name}}">{{end}}</td>
        <td><code>{{.Name}}</code></td><td><code>{{.Current}}</code></td><td><code>{{.Candidate}}</code></td>
        <td>{{if .Security}}<span class="tag ro">security</span>{{end}}{{if .Held}}<span class="tag ro">held</span>{{end}}</td>
        <td class="rowact"><form class="inline" method="post" action="/admin/hosting/system/hold"><input type="hidden" name="pkg" value="{{.Name}}"><input type="hidden" name="action" value="{{if .Held}}unhold{{else}}hold{{end}}"><button class="btn sm" title="{{if .Held}}release the no-update hold{{else}}pin this version (no update){{end}}">{{if .Held}}Release{{else}}Hold{{end}}</button></form></td>
      </tr>
    {{end}}
    </tbody>
  </table>
</section>
<form id="applyForm" method="post" action="/admin/hosting/system/preview" style="display:none"></form>
<script>
function xtkSel(mode){document.querySelectorAll('input[name=pkg]').forEach(function(c){
 if(mode==='all')c.checked=true;else if(mode==='none')c.checked=false;else if(mode==='sec')c.checked=c.dataset.sec==='1';});}
function xtkApply(){var ch=Array.prototype.slice.call(document.querySelectorAll('input[name=pkg]:checked'));
 if(!ch.length){alert('Select at least one package.');return;}
 var f=document.getElementById('applyForm');f.innerHTML='';
 ch.forEach(function(c){var i=document.createElement('input');i.type='hidden';i.name='pkg';i.value=c.value;f.appendChild(i);});
 f.submit();}
</script>
{{end}}`)

func (s *server) handleSystem(w http.ResponseWriter, r *http.Request) {
	var res osUpdatesResult
	err := s.callJSON(r.Context(), "os_updates_check", nil, &res)
	var f2b fail2banStatus
	_ = s.callJSON(r.Context(), "fail2ban_status", nil, &f2b) // best-effort: not configured -> enabled:false
	ok, errMsg := notices(r)
	data := struct {
		Tab           string
		Res           osUpdatesResult
		F2b           fail2banStatus
		Notice, Error string
	}{Tab: "system", Res: res, F2b: f2b, Notice: ok, Error: errMsg}
	if err != nil && data.Error == "" {
		data.Error = err.Error()
	}
	s.chrome("System").Render(w, xtkui.LangFromRequest(r), systemTmpl, data)
}

// handleSystemUnban lifts a fail2ban ban on one IP (vetted agent command).
func (s *server) handleSystemUnban(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.PostFormValue("ip"))
	if resp, err := s.callAgent(r.Context(), "fail2ban_unban", map[string]string{"ip": ip}); err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting/system", "", "unban: "+agentMsg(resp, err))
		return
	}
	redirectMsg(w, r, "/admin/hosting/system", "Unbanned "+ip+".", "")
}

// handleSystemApply upgrades the packages the admin ticked. The agent re-validates each
// name; upgrade-only, no reboot.
func (s *server) handleSystemApply(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectMsg(w, r, "/admin/hosting/system", "", "bad form")
		return
	}
	pkgs := r.PostForm["pkg"]
	if len(pkgs) == 0 {
		redirectMsg(w, r, "/admin/hosting/system", "", "no packages selected")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	var res struct {
		Applied        int  `json:"applied"`
		RebootRequired bool `json:"reboot_required"`
	}
	if err := s.callJSON(ctx, "os_updates_apply", map[string]string{"packages": strings.Join(pkgs, " ")}, &res); err != nil {
		redirectMsg(w, r, "/admin/hosting/system", "", "apply: "+err.Error())
		return
	}
	suffix := "s"
	if res.Applied == 1 {
		suffix = ""
	}
	msg := fmt.Sprintf("Applied %d package%s.", res.Applied, suffix)
	if res.RebootRequired {
		msg += " A reboot is now required to finish (not done automatically)."
	}
	redirectMsg(w, r, "/admin/hosting/system", msg, "")
}

type previewItem struct {
	Name  string
	Extra bool // pulled in as a dependency, not explicitly selected
}

var systemPreviewTmpl = xtkui.LocParse("hostingsyspreview", subtabsSrc+`<h1>Confirm updates</h1>
<p class="hint">apt's plan for the packages you selected — <b>nothing has changed yet</b>. Dependencies are resolved and ordered by apt/dpkg.</p>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<section><div class="card">
  <div class="meta">Selected <b>{{len .Selected}}</b> · apt will upgrade/install <b>{{len .Upgrade}}</b> package{{if ne (len .Upgrade) 1}}s{{end}} (dependencies included).</div>
  {{if .Remove}}<div class="err" style="margin:.8rem 0 0"><b>⚠ These would be REMOVED:</b> {{range .Remove}}<code>{{.}}</code> {{end}}<br><span class="hint">A removal can break something — cancel if unexpected.</span></div>{{end}}
</div></section>
{{if .Upgrade}}
<section>
  <table><thead><tr><th>Will upgrade / install</th><th></th></tr></thead><tbody>
  {{range .Upgrade}}<tr><td><code>{{.Name}}</code></td><td>{{if .Extra}}<span class="tag ro">dependency</span>{{end}}</td></tr>{{end}}
  </tbody></table>
</section>
{{else}}
<p class="hint">Nothing to upgrade for the selection (already up to date).</p>
{{end}}
<div class="actions" style="justify-content:flex-start;margin-top:1rem">
  {{if .Upgrade}}<form class="inline" method="post" action="/admin/hosting/system/apply" onsubmit="return confirm('Apply now? The host will NOT reboot on its own.')">
    {{range .Selected}}<input type="hidden" name="pkg" value="{{.}}">{{end}}
    <button class="btn primary">Confirm &amp; apply</button>
  </form>{{end}}
  <a class="btn" href="/admin/hosting/system">Cancel</a>
</div>`)

// handleSystemPreview dry-runs the selected upgrade and shows apt's plan (cascade +
// any removals) before anything is applied.
func (s *server) handleSystemPreview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectMsg(w, r, "/admin/hosting/system", "", "bad form")
		return
	}
	selected := r.PostForm["pkg"]
	if len(selected) == 0 {
		redirectMsg(w, r, "/admin/hosting/system", "", "no packages selected")
		return
	}
	sel := map[string]bool{}
	for _, p := range selected {
		sel[p] = true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	var plan struct {
		Upgrade []string `json:"upgrade"`
		Remove  []string `json:"remove"`
	}
	err := s.callJSON(ctx, "os_updates_preview", map[string]string{"packages": strings.Join(selected, " ")}, &plan)
	var items []previewItem
	for _, u := range plan.Upgrade {
		items = append(items, previewItem{Name: u, Extra: !sel[u]})
	}
	data := struct {
		Tab      string
		Selected []string
		Upgrade  []previewItem
		Remove   []string
		Error    string
	}{Tab: "system", Selected: selected, Upgrade: items, Remove: plan.Remove}
	if err != nil {
		data.Error = err.Error()
	}
	s.chrome("Confirm updates").Render(w, xtkui.LangFromRequest(r), systemPreviewTmpl, data)
}

// handleSystemHold pins ("no update") or releases a package via apt-mark.
func (s *server) handleSystemHold(w http.ResponseWriter, r *http.Request) {
	pkg := r.PostFormValue("pkg")
	action := r.PostFormValue("action")
	if pkg == "" || (action != "hold" && action != "unhold") {
		redirectMsg(w, r, "/admin/hosting/system", "", "bad hold request")
		return
	}
	var res struct {
		OK bool `json:"ok"`
	}
	if err := s.callJSON(r.Context(), "os_updates_hold", map[string]string{"action": action, "packages": pkg}, &res); err != nil {
		redirectMsg(w, r, "/admin/hosting/system", "", action+": "+err.Error())
		return
	}
	verb := "held (no update)"
	if action == "unhold" {
		verb = "released"
	}
	redirectMsg(w, r, "/admin/hosting/system", pkg+" "+verb+".", "")
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

// slugify derives an internal, dot-free site slug from a public domain: strip a
// leading "www.", collapse any run of non-[a-z0-9] to a single dash, trim dashes,
// ensure it starts with a letter and fits the site-name pattern [a-z][a-z0-9-]{1,30}.
func slugify(domain string) string {
	s := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "www.")
	var b strings.Builder
	dash := false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 30 {
		slug = strings.Trim(slug[:30], "-")
	}
	if slug != "" && (slug[0] < 'a' || slug[0] > 'z') {
		slug = "s" + slug
		if len(slug) > 31 {
			slug = slug[:31]
		}
	}
	return slug
}

func (s *server) handleCreate(w http.ResponseWriter, r *http.Request) {
	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
	name := r.FormValue("name") // optional explicit slug; otherwise derived from the domain
	if name == "" {
		name = slugify(domain)
	}
	if name == "" {
		redirectMsg(w, r, "/admin/hosting", "", "create: a domain is required")
		return
	}
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
	if domain != "" {
		params["domain"] = domain
	}
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

var dbInfoTmpl = xtkui.LocParse("hostingdbinfo", subtabsSrc+`<h1>Database · <code>site-{{.Name}}{{if .Vhost}}/{{.Vhost}}{{end}}</code></h1>
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
	name, vh := r.URL.Query().Get("name"), r.URL.Query().Get("vhost")
	cmd, params := "site_db_info", map[string]string{"name": name}
	if vh != "" {
		cmd, params = "vhost_db_info", map[string]string{"name": name, "vhost": vh}
	}
	resp, err := s.callAgent(r.Context(), cmd, params)
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
		Tab, Name, Vhost, DbHost, DbPort, DbName, User, Password, LocalPort, Error string
		Has                                                                        bool
	}{
		Tab: "hosts", Name: name, Vhost: vh, DbHost: env["DB_HOST"], DbPort: env["DB_PORT"], DbName: env["DB_NAME"],
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

func (s *server) handleVhostAutoUpdate(w http.ResponseWriter, r *http.Request) {
	name, vh, enabled := r.FormValue("name"), r.FormValue("vhost"), r.FormValue("enabled")
	resp, err := s.callAgent(r.Context(), "vhost_autoupdate", map[string]string{"name": name, "vhost": vh, "enabled": enabled})
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "auto-update: "+agentMsg(resp, err))
		return
	}
	redirectMsg(w, r, "/admin/hosting", "Auto-update for "+vhostLabel(name, vh)+" set to "+enabled+".", "")
}

// editData backs both the compose and nginx editors; Vhost is empty for a legacy
// (un-migrated) site's top-level file, set for a per-vhost file.
type editData struct{ Tab, Name, Vhost, Content, Error string }

// vhostLabel renders "name" or "name/vhost" for headings and flash messages.
func vhostLabel(name, vh string) string {
	if vh == "" {
		return name
	}
	return name + "/" + vh
}

// vhostAction runs a name+vhost agent command (up/down/destroy) and returns to Hosts.
func (s *server) vhostAction(cmd, okMsg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, vh := r.FormValue("name"), r.FormValue("vhost")
		resp, err := s.callAgent(r.Context(), cmd, map[string]string{"name": name, "vhost": vh})
		if err != nil || !resp.OK {
			redirectMsg(w, r, "/admin/hosting", "", cmd+": "+agentMsg(resp, err))
			return
		}
		redirectMsg(w, r, "/admin/hosting", fmt.Sprintf(okMsg, vhostLabel(name, vh)), "")
	}
}

func (s *server) handleVhostCreate(w http.ResponseWriter, r *http.Request) {
	name, vh := r.FormValue("name"), r.FormValue("vhost")
	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
	parts := strings.Split(r.FormValue("stack"), ":")
	tmpl, pv := parts[0], ""
	if len(parts) > 1 {
		pv = parts[1]
	}
	if tmpl == "" {
		tmpl = "php-fpm"
	}
	params := map[string]string{"name": name, "vhost": vh, "template": tmpl}
	if domain != "" {
		params["domain"] = domain
	}
	if pv != "" {
		params["php_version"] = pv
	}
	if resp, err := s.callAgent(r.Context(), "vhost_create", params); err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "add vhost: "+agentMsg(resp, err))
		return
	}
	if resp, err := s.callAgent(r.Context(), "vhost_up", map[string]string{"name": name, "vhost": vh}); err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "vhost created but start failed: "+agentMsg(resp, err))
		return
	}
	if tmpl == "custom" {
		http.Redirect(w, r, "/admin/hosting/edit?name="+url.QueryEscape(name)+"&vhost="+url.QueryEscape(vh), http.StatusSeeOther)
		return
	}
	redirectMsg(w, r, "/admin/hosting", "Vhost "+vhostLabel(name, vh)+" added and started.", "")
}

func (s *server) handleMigrate(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	resp, err := s.callAgent(r.Context(), "vhost_migrate", map[string]string{"name": name})
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "migrate: "+agentMsg(resp, err))
		return
	}
	redirectMsg(w, r, "/admin/hosting", "Site "+name+" migrated to the multi-vhost layout.", "")
}

var editTmpl = xtkui.LocParse("hostingedit", subtabsSrc+`<h1>Compose · <code>{{.Name}}{{if .Vhost}}/{{.Vhost}}{{end}}</code></h1>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<p class="hint">On save it is validated with <code>docker&nbsp;compose&nbsp;config</code> (reverted if invalid) and re-applied with <code>up&nbsp;-d</code>.</p>
<form method="post" action="/admin/hosting/edit">
  <input type="hidden" name="name" value="{{.Name}}">
  <input type="hidden" name="vhost" value="{{.Vhost}}">
  <textarea name="content" spellcheck="false" style="width:100%;min-height:26rem;font-family:var(--font-mono);font-size:.82rem;line-height:1.45;padding:.75rem;border:1px solid var(--line);border-radius:9px;background:var(--panel);color:var(--text);white-space:pre">{{.Content}}</textarea>
  <div class="actions">
    <a class="btn" href="/admin/hosting">Cancel</a>
    <button class="btn primary">Save &amp; apply</button>
  </div>
</form>`)

func (s *server) handleEdit(w http.ResponseWriter, r *http.Request) {
	name, vh := r.URL.Query().Get("name"), r.URL.Query().Get("vhost")
	cmd, params := "site_compose_get", map[string]string{"name": name}
	if vh != "" {
		cmd, params = "vhost_compose_get", map[string]string{"name": name, "vhost": vh}
	}
	resp, err := s.callAgent(r.Context(), cmd, params)
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "compose: "+agentMsg(resp, err))
		return
	}
	s.chrome("Compose").Render(w, xtkui.LangFromRequest(r), editTmpl, editData{Tab: "hosts", Name: name, Vhost: vh, Content: resp.Stdout})
}

func (s *server) handleEditSave(w http.ResponseWriter, r *http.Request) {
	name, vh, content := r.FormValue("name"), r.FormValue("vhost"), r.FormValue("content")
	cmd, params := "site_compose_set", map[string]string{"name": name, "content": content}
	if vh != "" {
		cmd, params = "vhost_compose_set", map[string]string{"name": name, "vhost": vh, "content": content}
	}
	resp, err := s.callAgent(r.Context(), cmd, params)
	if err != nil || !resp.OK {
		s.chrome("Compose").Render(w, xtkui.LangFromRequest(r), editTmpl, editData{Tab: "hosts", Name: name, Vhost: vh, Content: content, Error: agentMsg(resp, err)})
		return
	}
	redirectMsg(w, r, "/admin/hosting", "Compose for "+vhostLabel(name, vh)+" updated and applied.", "")
}

var nginxTmpl = xtkui.LocParse("hostingnginx", subtabsSrc+`<h1>Nginx · <code>{{.Name}}{{if .Vhost}}/{{.Vhost}}{{end}}</code></h1>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<p class="hint">The vhost's <code>nginx.conf</code>. On save it is validated with <code>nginx&nbsp;-t</code> in the
container (reverted if invalid) and reloaded. Tip: a canonical www→apex redirect inside the <code>server</code> block:
<br><code>if ($host ~* ^www\.(.*)$) { return 301 $scheme://$1$request_uri; }</code></p>
<form method="post" action="/admin/hosting/nginx">
  <input type="hidden" name="name" value="{{.Name}}">
  <input type="hidden" name="vhost" value="{{.Vhost}}">
  <textarea name="content" spellcheck="false" style="width:100%;min-height:26rem;font-family:var(--font-mono);font-size:.82rem;line-height:1.45;padding:.75rem;border:1px solid var(--line);border-radius:9px;background:var(--panel);color:var(--text);white-space:pre">{{.Content}}</textarea>
  <div class="actions">
    <a class="btn" href="/admin/hosting">Cancel</a>
    <button class="btn primary">Save &amp; reload</button>
  </div>
</form>`)

func (s *server) handleNginxEdit(w http.ResponseWriter, r *http.Request) {
	name, vh := r.URL.Query().Get("name"), r.URL.Query().Get("vhost")
	cmd, params := "site_nginx_get", map[string]string{"name": name}
	if vh != "" {
		cmd, params = "vhost_nginx_get", map[string]string{"name": name, "vhost": vh}
	}
	resp, err := s.callAgent(r.Context(), cmd, params)
	if err != nil || !resp.OK {
		redirectMsg(w, r, "/admin/hosting", "", "nginx: "+agentMsg(resp, err))
		return
	}
	s.chrome("Nginx").Render(w, xtkui.LangFromRequest(r), nginxTmpl, editData{Tab: "hosts", Name: name, Vhost: vh, Content: resp.Stdout})
}

func (s *server) handleNginxSave(w http.ResponseWriter, r *http.Request) {
	name, vh, content := r.FormValue("name"), r.FormValue("vhost"), r.FormValue("content")
	cmd, params := "site_nginx_set", map[string]string{"name": name, "content": content}
	if vh != "" {
		cmd, params = "vhost_nginx_set", map[string]string{"name": name, "vhost": vh, "content": content}
	}
	resp, err := s.callAgent(r.Context(), cmd, params)
	if err != nil || !resp.OK {
		s.chrome("Nginx").Render(w, xtkui.LangFromRequest(r), nginxTmpl, editData{Tab: "hosts", Name: name, Vhost: vh, Content: content, Error: agentMsg(resp, err)})
		return
	}
	redirectMsg(w, r, "/admin/hosting", "nginx.conf for "+vhostLabel(name, vh)+" updated.", "")
}

func main() {
	socket := flag.String("socket", "/run/xtk-agent/agent.sock", "path to the xtk-agent unix socket (in a bind-mounted dir)")
	listen := flag.String("listen", ":8090", "internal HTTP listen address")
	services := flag.String("services", "/etc/xaltorka/services.json", "read-only path to the core's services.json (backend state)")
	certs := flag.String("certs", "/etc/xaltorka/certs", "read-only path to the certs dir (SSL button state)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	s := &server{socket: *socket, servicesPath: *services, certsPath: *certs, log: log, adminer: map[string]*adminerSession{}}
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
	mux.HandleFunc("GET /admin/hosting/nginx", s.handleNginxEdit)
	mux.HandleFunc("POST /admin/hosting/nginx", s.handleNginxSave)
	mux.HandleFunc("POST /admin/hosting/migrate", s.handleMigrate)
	mux.HandleFunc("POST /admin/hosting/vhost/create", s.handleVhostCreate)
	mux.HandleFunc("POST /admin/hosting/vhost/up", s.vhostAction("vhost_up", "Vhost %s started."))
	mux.HandleFunc("POST /admin/hosting/vhost/down", s.vhostAction("vhost_down", "Vhost %s stopped."))
	mux.HandleFunc("POST /admin/hosting/vhost/destroy", s.vhostAction("vhost_destroy", "Vhost %s destroyed."))
	mux.HandleFunc("POST /admin/hosting/vhost/autoupdate", s.handleVhostAutoUpdate)
	// Users
	mux.HandleFunc("GET /admin/hosting/system", s.handleSystem)
	mux.HandleFunc("POST /admin/hosting/system/preview", s.handleSystemPreview)
	mux.HandleFunc("POST /admin/hosting/system/apply", s.handleSystemApply)
	mux.HandleFunc("POST /admin/hosting/system/hold", s.handleSystemHold)
	mux.HandleFunc("POST /admin/hosting/system/unban", s.handleSystemUnban)
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
