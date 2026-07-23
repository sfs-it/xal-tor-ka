// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package handlers wires the HTTP endpoints of Xal-Tor-Ka: the auth_request
// validation endpoint, the local login + TOTP flow, the first-run setup wizard,
// the services dashboard (/listing) and a minimal admin reload. See BLUEPRINT.md
// §4, §9, §13.
package handlers

import (
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"xaltorka/audit"
	"xaltorka/auth"
	"xaltorka/certmgr"
	"xaltorka/config"
	"xaltorka/health"
	"xaltorka/i18n"
	"xaltorka/matrix"
	"xaltorka/models"
	"xaltorka/providers"
	"xaltorka/proxy"
	"xaltorka/version"
	"xaltorka/xtkui"
)

// Server holds the dependencies shared by the HTTP handlers.
type Server struct {
	Cfg      *models.Config
	Users    *auth.UserDirectory // authoritative RAM directory (BLUEPRINT §8.1)
	Sessions auth.SessionStore
	Resolver *matrix.Resolver
	Local    *providers.Local
	// LDAP holds the enabled LDAP/AD providers, tried at login after Local (may be empty).
	LDAP []*providers.LDAP
	// OIDC holds the enabled OpenID Connect providers, keyed by id (may be empty).
	OIDC    map[string]*providers.OIDC
	Proxy   *proxy.Manager   // generates the NGINX backends config (may be nil)
	Health  *health.Checker  // backend health monitoring (may be nil)
	Audit   *audit.Logger    // fail2ban-friendly auth-failure log (may be nil)
	CertMgr *certmgr.Manager // TLS certificate manager (self-signed/ACME; may be nil)

	// OTP is the one-time-code store for passwordless login (nil = disabled).
	OTP *auth.OTPStore

	// Filesystem paths for persistence (set by main).
	UsersPath    string
	BackupsDir   string
	SetupPath    string
	ServicesPath string
	SecretsPath  string
	OTPQueuePath string // spool queue for one-time codes (data/otp-queue.jsonl)

	// Docker discovery (via read-only socket-proxy). Empty URL disables it.
	DockerProxyURL string
	DockerExclude  []string // container name substrings to hide (own stack)

	// HostingUpstream is the hosting extension's internal base URL (e.g.
	// http://xtk-hosting-ui:8090). Empty disables the extension: /admin/hosting
	// 404s and its admin-nav entry is hidden. The core reverse-proxies
	// /admin/hosting/* there, gated by the admin session (adminSessionOK).
	HostingUpstream string
	hostingProxy    *httputil.ReverseProxy

	// UpstreamLocalhost is the host that user-entered "localhost"/"127.0.0.1"
	// upstreams are rewritten to. In Docker that is "host.docker.internal" (the
	// host seen from inside a container); on a host/LXD deploy set "127.0.0.1"
	// (or "" to disable the rewrite entirely).
	UpstreamLocalhost string

	// BaseBackends are the static config.json backends; services.json backends
	// are merged on top at Reload time.
	BaseBackends []models.Backend
	// BaseProviders are the static config.json auth providers; services.json
	// providers are merged on top (by id) at Reload time.
	BaseProviders []models.ProviderCfg

	mu        sync.RWMutex
	links     []models.Link        // dashboard link tiles (from services.json), reloadable
	adminIPs  []string             // effective admin IP whitelist (services.json override, else empty→config)
	monitors  []models.Monitor     // standalone health monitors (from services.json), reloadable
	providers []models.ProviderCfg // effective auth providers (config + services), reloadable
}

// HealthTargets returns the set the health checker probes: the proxied backends
// plus the standalone monitors (as synthetic, non-proxied backends). Follows reloads.
func (s *Server) HealthTargets() []models.Backend {
	out := s.Resolver.Backends()
	s.mu.RLock()
	mons := s.monitors
	s.mu.RUnlock()
	for _, m := range mons {
		iv, to := m.IntervalSeconds, m.TimeoutSeconds
		if iv == 0 {
			iv = 30
		}
		if to == 0 {
			to = 5
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, models.Backend{
			ID: m.ID, Host: name,
			Health: models.Health{URL: m.URL, IntervalSeconds: iv, TimeoutSeconds: to},
		})
	}
	return out
}

// Reload re-reads services.json and rebuilds the resolver (config backends +
// services backends) and the link tiles. Safe to call at runtime.
func (s *Server) Reload() error {
	svc, err := config.LoadServices(s.ServicesPath)
	if err != nil {
		return err
	}
	// Only enabled backends enter the resolver/proxy/health.
	merged := append([]models.Backend{}, s.BaseBackends...)
	for _, b := range svc.Backends {
		if !b.Disabled {
			merged = append(merged, b)
		}
	}
	s.Resolver.Set(merged)

	// Rebuild the effective provider set (config base + services runtime, merged by
	// id) and the OIDC client map, hot-swapped under the lock — no restart needed.
	provs := mergeProviders(s.BaseProviders, svc.Providers)
	sec, _ := config.LoadSecretsRaw(s.SecretsPath)
	oidc := BuildOIDC(provs, sec, s.Cfg.Server.ExternalURL)

	s.mu.Lock()
	s.links = svc.Links
	s.adminIPs = svc.AdminIPWhitelist // empty → adminAllowed falls back to config
	s.monitors = svc.Monitors
	s.providers = provs
	s.OIDC = oidc
	s.mu.Unlock()
	if err := s.Proxy.Apply(merged); err != nil {
		return err
	}
	return nil
}

// oidcFor returns the built OIDC client for a provider id, under the read lock.
func (s *Server) oidcFor(id string) (*providers.OIDC, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.OIDC[id]
	return p, ok
}

// currentProviders returns a copy of the effective provider set. Before the first
// Reload (or if it failed) it falls back to the static config providers.
func (s *Server) currentProviders() []models.ProviderCfg {
	s.mu.RLock()
	p := s.providers
	s.mu.RUnlock()
	if p == nil {
		return s.BaseProviders
	}
	cp := make([]models.ProviderCfg, len(p))
	copy(cp, p)
	return cp
}

func (s *Server) currentLinks() []models.Link {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]models.Link, len(s.links))
	copy(cp, s.links)
	return cp
}

// Routes builds the HTTP handler with all endpoints mounted.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	// Gate static assets live under /_xtk/ (namespaced) so a proxied site keeps its
	// OWN /assets/ — the reserved-path collision that broke auth-gated sites' CSS.
	mux.Handle("GET /_xtk/assets/", http.StripPrefix("/_xtk", http.FileServerFS(xtkui.AssetsFS)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/listing", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /validate", s.handleValidate)
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("GET /login/code", s.handleCodeRequestForm)
	mux.HandleFunc("POST /login/code", s.handleCodeRequestSubmit)
	mux.HandleFunc("POST /login/code/verify", s.handleCodeVerifySubmit)
	mux.HandleFunc("GET /login/totp", s.handleTOTPForm)
	mux.HandleFunc("POST /auth/totp", s.handleTOTPSubmit)
	mux.HandleFunc("GET /auth/{provider}/start", s.handleOIDCStart)
	mux.HandleFunc("GET /auth/{provider}/callback", s.handleOIDCCallback)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /listing", s.handleListing)
	mux.HandleFunc("GET /listing/img/{id}", s.handleListingImg)
	mux.HandleFunc("GET /lang/{code}", s.handleSetLang)
	mux.HandleFunc("GET /profilo", s.handleProfile)
	mux.HandleFunc("POST /profilo/password", s.handleProfilePassword)
	mux.HandleFunc("POST /profilo/totp", s.handleProfileTOTP)
	mux.HandleFunc("GET /setup", s.handleSetupForm)
	mux.HandleFunc("POST /setup", s.handleSetupSubmit)
	mux.HandleFunc("POST /admin/reload", s.handleAdminReload)
	mux.HandleFunc("GET /admin", s.handleAdmin)
	mux.HandleFunc("GET /admin/servizi", s.handleAdminServices)
	mux.HandleFunc("GET /admin/docker", s.handleAdminDocker)
	mux.HandleFunc("GET /admin/utenti", s.handleAdminUsers)
	mux.HandleFunc("GET /admin/utenti/{email}", s.handleAdminUserDetail)
	mux.HandleFunc("GET /admin/monitoring", s.handleAdminMonitoring)
	mux.HandleFunc("POST /admin/link/add", s.handleLinkAdd)
	mux.HandleFunc("POST /admin/link/del", s.handleLinkDel)
	mux.HandleFunc("POST /admin/backend/add", s.handleBackendAdd)
	mux.HandleFunc("POST /admin/backend/del", s.handleBackendDel)
	mux.HandleFunc("GET /admin/backend/edit", s.handleBackendEditForm)
	mux.HandleFunc("POST /admin/backend/edit", s.handleBackendEdit)
	mux.HandleFunc("POST /admin/backend/toggle", s.handleBackendToggle)
	mux.HandleFunc("POST /admin/link/toggle", s.handleLinkToggle)
	mux.HandleFunc("POST /admin/user/add", s.handleUserAdd)
	mux.HandleFunc("POST /admin/user/del", s.handleUserDel)
	mux.HandleFunc("POST /admin/user/authz", s.handleUserAuthz)
	mux.HandleFunc("POST /admin/user/totp", s.handleUserTOTP)
	mux.HandleFunc("POST /admin/discover/add", s.handleDiscoverAdd)
	mux.HandleFunc("GET /admin/hostscan", s.handleHostScan)
	mux.HandleFunc("POST /admin/hostscan/add", s.handleHostScanAdd)
	mux.HandleFunc("POST /admin/user/email", s.handleUserEmail)
	mux.HandleFunc("POST /admin/user/password", s.handleUserPassword)
	mux.HandleFunc("POST /admin/user/admin", s.handleUserAdmin)
	mux.HandleFunc("POST /admin/adminips", s.handleAdminIPs)
	mux.HandleFunc("POST /admin/monitor/add", s.handleMonitorAdd)
	mux.HandleFunc("POST /admin/monitor/del", s.handleMonitorDel)
	mux.HandleFunc("GET /admin/providers", s.handleAdminProviders)
	mux.HandleFunc("GET /admin/provider/edit", s.handleProviderEditForm)
	mux.HandleFunc("POST /admin/provider/add", s.handleProviderAdd)
	mux.HandleFunc("POST /admin/provider/edit", s.handleProviderEdit)
	mux.HandleFunc("POST /admin/provider/del", s.handleProviderDel)
	mux.HandleFunc("POST /admin/provider/toggle", s.handleProviderToggle)
	mux.HandleFunc("POST /admin/provider/test", s.handleProviderTest)
	mux.HandleFunc("GET /admin/tls", s.handleAdminTLS)
	mux.HandleFunc("POST /admin/tls/issue", s.handleTLSIssue)
	mux.HandleFunc("POST /admin/tls/renew", s.handleTLSRenew)
	mux.HandleFunc("POST /admin/tls/del", s.handleTLSDelete)
	mux.HandleFunc("GET /admin/tls/ca.crt", s.handleTLSCA)

	// Hosting extension (optional): the core reverse-proxies /admin/hosting/* to it,
	// gated by the admin session. No method prefix → GET and POST both route here.
	if s.HostingUpstream != "" {
		if u, err := url.Parse(s.HostingUpstream); err == nil {
			s.hostingProxy = httputil.NewSingleHostReverseProxy(u)
		}
		mux.HandleFunc("/admin/hosting", s.handleHostingProxy)
		mux.HandleFunc("/admin/hosting/", s.handleHostingProxy)
	}
	return mux
}

// normalizeCIDRs parses a free-form list of IPs/CIDRs (separated by spaces,
// commas or newlines), turning bare IPs into /32 (v4) or /128 (v6). It returns
// the normalized CIDRs and an error on the first invalid token.
func normalizeCIDRs(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !strings.Contains(f, "/") {
			if ip := net.ParseIP(f); ip != nil {
				if ip.To4() != nil {
					f += "/32"
				} else {
					f += "/128"
				}
			}
		}
		if _, _, err := net.ParseCIDR(f); err != nil {
			return nil, fmt.Errorf("invalid IP/CIDR %q", f)
		}
		out = append(out, f)
	}
	return out, nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "{\"status\":\"ok\",\"auth_mode\":%t,\"version\":%q}\n", s.Cfg.AuthMode, version.Version)
}

// handleValidate is the auth_request target (BLUEPRINT §4.2). Fail-closed:
// any unmatched request or evaluation gap yields 403; missing auth yields 401.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.AuthMode {
		w.WriteHeader(http.StatusOK) // solo-proxy mode: pass through
		return
	}

	host := stripPort(firstNonEmpty(r.Header.Get("X-Original-Host"), r.Host))
	path := firstNonEmpty(r.Header.Get("X-Original-URI"), "/")
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}

	be, route, ok := s.Resolver.Resolve(host, path)
	if !ok {
		w.WriteHeader(http.StatusForbidden) // default-deny
		return
	}

	// Per-vhost IP allow-list: enforced before the rule, so it also restricts
	// "public" services. Fail-closed: unknown/absent client IP is denied.
	if len(be.IPAllow) > 0 {
		ip := clientIP(r, s.Cfg.Server.TrustedProxies)
		if ip == nil || !ipInCIDRs(ip, be.IPAllow) {
			s.auditFail(r, "ip_denied", "host="+host)
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}

	switch route.Rule {
	case "public":
		w.WriteHeader(http.StatusOK)
	case "authenticated":
		if sess, ok := s.session(r); ok && sess.TwoFADone {
			w.Header().Set("X-Auth-User", sess.Email)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	case "whitelist":
		sess, ok := s.session(r)
		if !ok || !sess.TwoFADone {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if u, found := s.Users.Get(sess.Email); found && (u.Admin || s.Resolver.Authorized(u, be.ID)) {
			w.Header().Set("X-Auth-User", sess.Email)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	default:
		w.WriteHeader(http.StatusForbidden) // unknown rule => deny
	}
}

var listingTmpl = template.Must(template.New("listing").Funcs(xtkui.TmplFuncs).Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if rtl .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · {{T .Lang "listing.subtitle"}}</title><link rel="stylesheet" href="/_xtk/assets/admin.css"><script src="/_xtk/assets/admin.js" defer></script></head><body>
<header class="topbar">
 <div class="brand">⛬ Xal-Tor-Ka<span class="sub">{{T .Lang "listing.subtitle"}}</span></div>
 <nav class="topnav"><span class="who">{{.Email}}</span>
  {{if .IsAdmin}}<a href="/admin">{{T .Lang "nav.admin"}}</a>{{end}}
  {{cluster .Lang}}</nav>
</header>
<main class="container">
 <h1>{{T .Lang "listing.title"}}</h1>
 <div class="grid">
 {{range .Groups}}{{if and .Site (gt (len .Tiles) 1)}}<section class="sitegroup"><h2>{{.Site}}/</h2><div class="grid subgrid">{{range .Tiles}}<a class="card" href="{{.URL}}"{{if .External}} target="_blank" rel="noopener"{{end}}>
   <div class="row"><h3>{{.Name}}</h3><span class="tag {{if .External}}ext{{end}}">{{if .External}}{{T $.Lang "tag.external"}}{{else}}{{T $.Lang "tag.proxy"}}{{end}}</span></div>
   {{if .Image}}<img src="{{.Image}}" alt="" loading="lazy" style="max-width:100%;border-radius:8px;margin:.4rem 0;display:block">{{end}}{{if .Description}}<div class="meta">{{.Description}}</div>{{else if .Host}}<div class="meta"><code>{{.Host}}</code></div>{{end}}</a>{{end}}</div></section>{{else}}{{range .Tiles}}<a class="card" href="{{.URL}}"{{if .External}} target="_blank" rel="noopener"{{end}}>
   <div class="row"><h3>{{.Name}}</h3><span class="tag {{if .External}}ext{{end}}">{{if .External}}{{T $.Lang "tag.external"}}{{else}}{{T $.Lang "tag.proxy"}}{{end}}</span></div>
   {{if .Image}}<img src="{{.Image}}" alt="" loading="lazy" style="max-width:100%;border-radius:8px;margin:.4rem 0;display:block">{{end}}{{if .Description}}<div class="meta">{{.Description}}</div>{{else if .Host}}<div class="meta"><code>{{.Host}}</code></div>{{end}}</a>{{end}}{{end}}
 {{else}}<p class="empty">{{T .Lang "listing.empty"}}</p>{{end}}
 </div>
</main></body></html>`))

type tile struct {
	Name        string
	URL         string
	Description template.HTML // sanitized rich description (Markdown → HTML), shown on the card
	Image       string        // preview image URL (/listing/img/<id>), optional
	Host        string
	Site        string // hosting site that owns this tile ("" = standalone)
	External    bool
}

// tileGroup collects the listing tiles of one hosting site so a multidomain site shows as
// ONE block instead of scattered cards. Site == "" → a standalone tile (one item).
type tileGroup struct {
	Site  string
	Tiles []tile
}

func groupTiles(ts []tile) []tileGroup {
	var out []tileGroup
	idx := map[string]int{}
	for _, t := range ts {
		if t.Site == "" {
			out = append(out, tileGroup{Tiles: []tile{t}})
			continue
		}
		if i, ok := idx[t.Site]; ok {
			out[i].Tiles = append(out[i].Tiles, t)
		} else {
			idx[t.Site] = len(out)
			out = append(out, tileGroup{Site: t.Site, Tiles: []tile{t}})
		}
	}
	return out
}

func (s *Server) handleListing(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(r)
	if !ok || !sess.TwoFADone {
		http.Redirect(w, r, "/login?next=/listing", http.StatusSeeOther)
		return
	}
	u, _ := s.Users.Get(sess.Email)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = listingTmpl.Execute(w, struct {
		Email   string
		IsAdmin bool
		Lang    string
		Groups  []tileGroup
	}{Email: sess.Email, IsAdmin: u.Admin, Lang: s.lang(r), Groups: groupTiles(s.tilesFor(u))})
}

// tilesFor builds the dashboard tiles visible to the user: proxied backends it
// can reach plus authorized/public external links.
func (s *Server) tilesFor(u models.User) []tile {
	var ts []tile
	for _, be := range s.Resolver.Backends() {
		if !s.canSeeBackend(u, be) || be.Unlisted {
			continue
		}
		name := be.Name
		if name == "" {
			name = be.ID
		}
		url := be.URL
		if url == "" {
			url = "//" + be.Host
		}
		site := ""
		if be.Hosting != nil {
			site = be.Hosting.Site
		}
		img := ""
		if be.Image != "" {
			img = "/listing/img/" + be.ID
		}
		ts = append(ts, tile{Name: name, URL: url, Host: be.Host, Site: site, External: false,
			Description: richHTML(be.Description), Image: img})
	}
	for _, l := range s.currentLinks() {
		if l.Disabled {
			continue
		}
		if u.Admin || l.Public || s.Resolver.Authorized(u, l.ID) {
			ts = append(ts, tile{Name: l.Name, URL: l.URL, Description: template.HTML(template.HTMLEscapeString(l.Description)), External: true})
		}
	}
	return ts
}

func (s *Server) canSeeBackend(u models.User, be models.Backend) bool {
	if u.Admin {
		return true // admins see and reach everything
	}
	for _, rt := range be.Routes {
		if rt.Rule == "public" || rt.Rule == "authenticated" {
			return true
		}
	}
	return s.Resolver.Authorized(u, be.ID)
}

// handleAdminReload re-reads services.json without a restart (IP-whitelisted).
func (s *Server) handleAdminReload(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	if err := s.Reload(); err != nil {
		http.Error(w, i18n.T(s.lang(r), "err.reload_failed"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, `{"status":"reloaded"}`)
}

// session returns the current session from the cookie, if valid.
func (s *Server) session(r *http.Request) (models.Session, bool) {
	c, err := r.Cookie(s.Cfg.Session.CookieName)
	if err != nil {
		return models.Session{}, false
	}
	return s.Sessions.Get(c.Value)
}

// setSession writes the session cookie. Secure is enabled when the external URL
// is HTTPS (BLUEPRINT §18.5: behind external TLS termination).
func (s *Server) setSession(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.Cfg.Session.CookieName,
		Value:    id,
		Path:     "/",
		Domain:   s.Cfg.Session.CookieDomain, // empty = host-only; "localhost" = SSO across *.localhost
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.Cfg.Server.ExternalURL, "https"),
	})
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.Cfg.Session.CookieName, Value: "", Path: "/",
		Domain:   s.Cfg.Session.CookieDomain,
		HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteLaxMode,
	})
}

// auditFail logs an authentication failure with the real client IP (fail2ban).
func (s *Server) auditFail(r *http.Request, event, detail string) {
	if s.Audit == nil {
		return
	}
	ip := ""
	if c := clientIP(r, s.Cfg.Server.TrustedProxies); c != nil {
		ip = c.String()
	}
	s.Audit.Fail(ip, event, detail)
}

// effectiveAdminIPs returns the admin IP whitelist in force: the services.json
// runtime override if set, otherwise the config/env value (ADMIN_CIDR).
func (s *Server) effectiveAdminIPs() []string {
	s.mu.RLock()
	cidrs := s.adminIPs
	s.mu.RUnlock()
	if len(cidrs) == 0 {
		return s.Cfg.Admin.IPWhitelist
	}
	return cidrs
}

// adminAllowed reports whether the request comes from an admin-whitelisted IP.
func (s *Server) adminAllowed(r *http.Request) bool {
	ip := clientIP(r, s.Cfg.Server.TrustedProxies)
	if ip == nil {
		return false
	}
	return ipInCIDRs(ip, s.effectiveAdminIPs())
}

// clientIP returns the real client IP. Forwarded headers are honored ONLY when
// the direct peer is a trusted proxy (BLUEPRINT §18.5). It prefers X-Real-IP,
// which the edge NGINX sets to $remote_addr and thus cannot be spoofed by the
// client; X-Forwarded-For (leftmost) is a fallback for upstream LB chains.
func clientIP(r *http.Request, trusted []string) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if !ipInCIDRs(ip, trusted) {
		return ip // untrusted/direct peer: never trust forwarded headers
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		if cand := net.ParseIP(xr); cand != nil {
			return cand
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := strings.TrimSpace(strings.Split(xff, ",")[0])
		if cand := net.ParseIP(first); cand != nil {
			return cand
		}
	}
	return ip
}

func ipInCIDRs(ip net.IP, cidrs []string) bool {
	if ip == nil {
		return false
	}
	for _, cidr := range cidrs {
		if _, n, err := net.ParseCIDR(cidr); err == nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// sanitizeNext prevents open-redirects: allows same-site absolute paths, or an
// absolute http(s) URL whose host is a known backend (so login can redirect back
// to the originally requested service). Anything else falls back to /listing.
func (s *Server) sanitizeNext(n string) string {
	if n == "" {
		return "/listing"
	}
	if strings.HasPrefix(n, "/") && !strings.HasPrefix(n, "//") {
		return n
	}
	if u, err := url.Parse(n); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		host := stripPort(u.Host)
		for _, be := range s.Resolver.Backends() {
			if be.Host == host {
				return n
			}
		}
	}
	return "/listing"
}
