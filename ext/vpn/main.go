// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Command xtk-vpn-ui is the VPN extension's web UI: an internal service that renders
// the VPN admin panel (shared xtkui chrome) and, from F2 on, drives the privileged
// xtk-vpn-agent over its unix socket to manage the WireGuard data-plane. It has NO
// host powers of its own — every mutating action is a vetted agent command. The gateway
// reverse-proxies it under the admin host (/admin/vpn), auth-gated (adminSessionOK); it
// is never exposed directly.
//
// F1 is a SCAFFOLD: this UI is read-only (a status page) and the module is inert unless
// explicitly enabled in vpn.json. It is the twin of ext/hosting (see DRAFT-ext-module.md).
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
	"sync"
	"time"

	"xaltorka/agent"
	"xaltorka/ext/vpn/vpnmgr"
	"xaltorka/version"
	"xaltorka/xtkui"
)

// server holds the module's runtime deps. It implements vpnmgr.AgentCaller (Call), so
// the drivers can run vetted privileged commands without knowing the transport.
type server struct {
	socket     string
	configPath string
	store      *vpnmgr.Store
	log        *slog.Logger
	mu         sync.Mutex
}

// Call runs one vetted command over the xtk-vpn-agent unix socket (one request per
// connection). It satisfies vpnmgr.AgentCaller. In F1 nothing calls it yet (the driver
// is a scaffold); F2 uses it for wg_up/down, wg_peer_add, etc.
func (s *server) Call(ctx context.Context, cmd string, params map[string]string) (agent.Response, error) {
	var resp agent.Response
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "unix", s.socket)
	if err != nil {
		return resp, fmt.Errorf("dial vpn-agent: %w", err)
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

// chrome renders the SAME top menu as the core admin (via xtkui.AdminNav) with the "VPN"
// entry active — so the extension looks like a native admin section. The module's own
// tabs are a secondary bar inside the page (subtabsSrc).
func (s *server) chrome(title string) xtkui.Chrome {
	return xtkui.Chrome{
		Title: "Xal-Tor-Ka · " + title, BrandText: "⛬ Xal-Tor-Ka", BrandHref: "/admin",
		SubtitleKey: "admin.subtitle", Version: version.Version,
		Nav:           xtkui.AdminNav(xtkui.NavItem{Key: "vpn", Href: "/admin/vpn", LabelKey: "admin.vpn"}),
		Active:        "vpn",
		DashboardHref: "/listing", DashboardKey: "nav.dashboard", LoggedIn: true,
	}
}

// subtabsSrc is the in-page secondary tab bar; .Tab marks the active one. In F1 only
// "Stato" is live; Macchine/Utenti/Matrice arrive with F2/F3/F6.
const subtabsSrc = `<nav class="subtabs">
<a href="/admin/vpn"{{if eq .Tab "status"}} class="active"{{end}}>Stato</a>
<a href="/admin/vpn/machines"{{if eq .Tab "machines"}} class="active"{{end}}>Macchine</a>
<a href="/admin/vpn/users"{{if eq .Tab "users"}} class="active"{{end}}>Utenti</a>
<a href="/admin/vpn/matrix"{{if eq .Tab "matrix"}} class="active"{{end}}>Matrice</a>
</nav>
`

var statusTmpl = xtkui.LocParse("vpnstatus", subtabsSrc+`<h1>VPN</h1>
{{if .Error}}<div class="notice err">{{.Error}}</div>{{end}}
{{if not .Cfg.Enabled}}<div class="notice">Modulo VPN presente ma <b>disattivo</b> (<code>enabled=false</code> in <code>vpn.json</code>). Nessun tunnel, nessun effetto di rete. È lo stato di scaffold (fase F1).</div>{{end}}
<table class="tbl">
  <tr><th>Stato</th><td>{{if .Status.Ready}}<b>attivo</b>{{else}}<b>non pronto</b>{{end}}{{if .Status.Reason}} — {{.Status.Reason}}{{end}}</td></tr>
  <tr><th>Driver</th><td><code>{{.Cfg.Driver}}</code></td></tr>
  <tr><th>Ruolo</th><td><code>{{.Cfg.Role}}</code></td></tr>
  <tr><th>Porta (hub)</th><td><code>{{.Cfg.ListenPort}}/udp</code></td></tr>
  <tr><th>Subnet VPN</th><td><code>{{.Cfg.VPNSubnet}}</code></td></tr>
  <tr><th>Rete docker</th><td><code>{{.Cfg.DockerNet}}</code></td></tr>
  <tr><th>Macchine</th><td>{{len .Cfg.Machines}}</td></tr>
  <tr><th>Utenti</th><td>{{len .Cfg.Users}}</td></tr>
</table>
<p class="muted">Il pannello completo (macchine, utenti, matrice, onboarding, rientro) arriva con le fasi successive. Config: <code>{{.ConfigPath}}</code>.</p>
`)

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Tab           string
		Cfg           vpnmgr.Config
		Status        vpnmgr.Status
		ConfigPath    string
		Notice, Error string
	}{Tab: "status", ConfigPath: s.configPath}

	cfg, err := s.store.Load()
	if err != nil {
		data.Error = err.Error()
		s.chrome("VPN").Render(w, xtkui.LangFromRequest(r), statusTmpl, data)
		return
	}
	data.Cfg = cfg

	if drv, derr := vpnmgr.New(cfg, s); derr != nil {
		data.Error = derr.Error()
	} else if st, serr := drv.Status(r.Context()); serr != nil {
		data.Error = serr.Error()
	} else {
		data.Status = st
	}
	s.chrome("VPN").Render(w, xtkui.LangFromRequest(r), statusTmpl, data)
}

func main() {
	socket := flag.String("socket", "/run/xtk-vpn-agent/agent.sock", "path to the xtk-vpn-agent unix socket (in a bind-mounted dir)")
	listen := flag.String("listen", ":8091", "internal HTTP listen address")
	config := flag.String("config", "/etc/xaltorka/vpn/vpn.json", "path to the module's vpn.json (in a bind-mounted dir, read-write)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	s := &server{socket: *socket, configPath: *config, store: vpnmgr.NewStore(*config), log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/vpn", s.handleIndex)
	mux.HandleFunc("GET /admin/vpn/", s.handleIndex)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	srv := &http.Server{
		Addr: *listen, Handler: mux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second,
	}
	log.Info("vpn UI listening", "listen", *listen, "socket", *socket, "config", *config)
	if err := srv.ListenAndServe(); err != nil {
		log.Error("vpn UI failed", "err", err)
		os.Exit(1)
	}
}
