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
	"xaltorka/legacyhtml"
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
	// One nav entry per enabled extension (see DRAFT-ext-module.md): the core builds
	// the slice from the <MOD>_UPSTREAM values it has wired on.
	var mods []xtkui.NavItem
	if s.HostingUpstream != "" {
		mods = append(mods, xtkui.NavItem{Key: "hosting", Href: "/admin/hosting", LabelKey: "admin.hosting"})
	}
	if s.VpnUpstream != "" {
		mods = append(mods, xtkui.NavItem{Key: "vpn", Href: "/admin/vpn", LabelKey: "admin.vpn"})
	}
	nav := xtkui.AdminNav(mods...)
	lang := s.lang(r)
	// TLS and Providers are second-level tabs of Servizi / Utenti: highlight the parent
	// in the top nav and render a sub-tab bar to switch between the grouped pages.
	topActive, subtabs := active, ""
	switch active {
	case "servizi", "tls":
		topActive = "servizi"
		subtabs = xtkui.SubtabBar(active, []xtkui.NavItem{
			{Key: "servizi", Href: "/admin/servizi", LabelKey: "admin.services"},
			{Key: "tls", Href: "/admin/tls", LabelKey: "admin.tls"},
		}, lang)
	case "utenti", "providers":
		topActive = "utenti"
		subtabs = xtkui.SubtabBar(active, []xtkui.NavItem{
			{Key: "utenti", Href: "/admin/utenti", LabelKey: "admin.users"},
			{Key: "providers", Href: "/admin/providers", LabelKey: "admin.providers"},
		}, lang)
	}
	c := xtkui.Chrome{
		Title: "Xal-Tor-Ka · Admin", BrandText: "⛬ Xal-Tor-Ka", BrandHref: "/admin",
		SubtitleKey: "admin.subtitle", Version: version.Version,
		Nav: nav, Active: topActive, Subtabs: subtabs,
		DashboardHref: "/listing", DashboardKey: "nav.dashboard", LoggedIn: true,
	}
	// Persistent security reminder for administrators: shown whenever the admin
	// area is IP-open (ADMIN_CIDR=0.0.0.0/0 or empty) or 2FA is disabled.
	if adminIPOpen(s.Cfg.Admin.IPWhitelist) || s.Cfg.DisableTOTP {
		c.Notice = i18n.T(lang, "admin.secalert")
	}
	c.Render(w, lang, t, data)
}

// adminIPOpen reports whether the admin area is reachable from any IP (empty
// whitelist or a catch-all CIDR) — used to surface the security banner.
func adminIPOpen(wl []string) bool {
	if len(wl) == 0 {
		return true
	}
	for _, cidr := range wl {
		switch strings.TrimSpace(cidr) {
		case "0.0.0.0/0", "::/0":
			return true
		}
	}
	return false
}

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
	s.renderAdminPage(w, r, "", legacyhtml.OverviewTmpl, struct {
		Services, Links, Users, ConfigBackends, Up, Down int
		AdminIPsSource, ClientIP, AdminIPsRaw            string
	}{
		len(svc.Backends), len(svc.Links), s.Users.Count(), len(s.BaseBackends), up, down,
		source, clientIPStr, strings.Join(eff, " "),
	})
}

// svcItem is a backend as shown in the Services table: the backend itself (its fields
// stay directly reachable from the template) plus the label to display.
type svcItem struct {
	models.Backend
	Label string
}

// svcGroup collects the services shown under one header. A group is either a hosting
// SITE — whose vhosts may span several hostnames (segnalapa.it, api.segnalapa.it, …) —
// or a DOMAIN shared by more than one service: the site on "/" plus the services mounted
// on its paths. A lone service stays ungrouped (Site == "").
type svcGroup struct {
	Site     string
	Backends []svcItem
}

// groupServiceBackends nests the services under their site or domain, preserving first
// appearance order. A service with no hosting marker still belongs with the others on
// its domain: a reverse-proxy mounted on a path of an existing site must appear under
// that site, not float away as a lone row (the proxy already treats the host as the
// unit — the panel has to tell the same story).
func groupServiceBackends(bs []models.Backend) []svcGroup {
	hostSite := map[string]string{} // host → hosting site owning it, when there is one
	hostCount := map[string]int{}   // services per host: >1 means the domain is shared
	for _, b := range bs {
		hostCount[b.Host]++
		if b.Hosting != nil && b.Hosting.Site != "" && hostSite[b.Host] == "" {
			hostSite[b.Host] = b.Hosting.Site
		}
	}

	var out []svcGroup
	idx := map[string]int{}
	for _, b := range bs {
		site := ""
		if b.Hosting != nil {
			site = b.Hosting.Site
		}
		if site == "" {
			if s := hostSite[b.Host]; s != "" {
				site = s // joins the site that owns this domain
			} else if b.Host != "" && hostCount[b.Host] > 1 {
				site = b.Host // no hosting site: the domain itself is the group
			}
		}
		it := svcItem{Backend: b, Label: serviceLabel(b, site)}
		if site == "" {
			out = append(out, svcGroup{Backends: []svcItem{it}})
			continue
		}
		if i, ok := idx[site]; ok {
			out[i].Backends = append(out[i].Backends, it)
		} else {
			idx[site] = len(out)
			out = append(out, svcGroup{Site: site, Backends: []svcItem{it}})
		}
	}

	// The short label only makes sense under a header that names the site, and the
	// table draws that header only for groups with more than one service. A lone
	// service keeps its full name — otherwise "centrosub-com/httpdocs (hosting)"
	// would show as a bare "httpdocs" with nothing saying which site it belongs to.
	for i := range out {
		if len(out[i].Backends) == 1 {
			out[i].Backends[0].Label = serviceLabel(out[i].Backends[0].Backend, "")
		}
	}
	return out
}

// serviceLabel is the name shown in the Services table. Inside a group the header
// already states the site, so the redundant "<site>/" prefix is dropped
// ("segnalapa/api (hosting)" → "api"); outside a group the full name is kept.
func serviceLabel(b models.Backend, site string) string {
	name := b.Name
	if name == "" {
		name = b.ID
	}
	if site == "" {
		return name
	}
	if b.Hosting != nil && b.Hosting.Vhost != "" {
		return b.Hosting.Vhost
	}
	return strings.TrimPrefix(name, site+"/")
}

func (s *Server) handleAdminServices(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	s.renderAdminPage(w, r, "servizi", legacyhtml.ServicesTmpl, struct {
		ConfigBackends []models.Backend
		Groups         []svcGroup
		Links          []models.Link
		HostingEnabled bool
	}{s.BaseBackends, groupServiceBackends(svc.Backends), svc.Links, s.HostingUpstream != ""})
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
	s.renderAdminPage(w, r, "docker", legacyhtml.DockerTmpl, struct {
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
	s.renderAdminPage(w, r, "utenti", legacyhtml.UsersTmpl, struct {
		Users   []adminUserRow
		AllTree []authzNode
	}{rowsFor(users), s.authzTree(svc)})
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
	s.renderAdminPage(w, r, "utenti", legacyhtml.UserDetailTmpl, struct {
		Email, Provider string
		Admin           bool
		AllTree         []authzNode
		Checked         map[string]bool
	}{u.Email, u.Provider, u.Admin, s.authzTree(svc), checked})
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
	s.renderAdminPage(w, r, "monitoring", legacyhtml.MonitoringTmpl, struct {
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
	rule := models.CanonicalRule(r.PostFormValue("rule"))
	if name == "" || port <= 0 || port > 65535 {
		http.Error(w, i18n.T(s.lang(r), "err.invalid_container_port"), http.StatusBadRequest)
		return
	}
	if rule != models.RulePublic && rule != models.RuleAuthenticated && rule != models.RuleAuthorized {
		rule = models.RuleAuthorized
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
	// Clamp BEFORE rendering: the page must state the range it really probed, not the
	// one that was asked for — a scan that quietly covers a slice of the request makes
	// a missing service look absent when it was never looked at.
	from, to, capped := dockerscan.ClampRange(from, to)
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
	s.renderAdminPage(w, r, "servizi", legacyhtml.HostScanTmpl, struct {
		From, To int
		Capped   bool
		Max      int
		Ports    []hostPortRow
	}{From: from, To: to, Capped: capped, Max: dockerscan.MaxScanPorts, Ports: rows})
}

// handleHostScanAdd bulk-creates vhosts for the selected host ports.
func (s *Server) handleHostScanAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	rule := models.CanonicalRule(r.PostFormValue("rule"))
	if rule != models.RulePublic && rule != models.RuleAuthenticated && rule != models.RuleAuthorized {
		rule = models.RuleAuthorized
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

// authGatedServiceIDs returns the IDs offer-able in the per-user authorization
// lists: only services that actually sit behind the gate's auth (any route rule
// != "public"), plus links. Public-only backends are excluded — authorizing a
// user for a public host is meaningless (it needs no login), so it must not
// appear in the "host abilitati" checklist. A backend with no explicit routes is
// kept (conservative: don't hide something whose rule we can't read from routes).
func (s *Server) authGatedServiceIDs(svc models.Services) []string {
	set := map[string]bool{}
	for _, b := range s.BaseBackends {
		if backendGated(b) {
			set[b.ID] = true
		}
	}
	for _, b := range svc.Backends {
		if backendGated(b) {
			set[b.ID] = true
		}
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

type authzItem struct{ ID, Label string }

// pathGrantees lists, space separated, the users holding the grant for one path of a
// service — i.e. the list that applies when that path does not inherit.
func (s *Server) pathGrantees(backendID, path string) string {
	want := models.GrantID(backendID, path)
	if want == backendID {
		return ""
	}
	var out []string
	for _, u := range s.Users.All() {
		for _, id := range u.Backends {
			if id == want {
				out = append(out, u.Email)
				break
			}
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// svcUserRow is a user as offered in a service's Access tab: who they are and whether
// they are enabled on THIS service. Administrators are listed but not selectable —
// they get in regardless, and hiding that would misrepresent who can actually enter.
type svcUserRow struct {
	Email   string
	Enabled bool
	Admin   bool
}

// serviceUsers lists every user with a flag telling whether backendID is among the
// services they were granted.
func (s *Server) serviceUsers(backendID string) []svcUserRow {
	var out []svcUserRow
	for _, u := range s.Users.All() {
		on := false
		for _, id := range u.Backends {
			if id == backendID {
				on = true
				break
			}
		}
		out = append(out, svcUserRow{Email: u.Email, Enabled: on, Admin: u.Admin})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out
}

// authzNode is one row-group of the per-user authorization checklist: a hosting site
// (Site != "") with its vhosts, or a standalone service (Site == "", one item).
type authzNode struct {
	Site  string
	Items []authzItem
}

// authzTree lists ALL authorizable services for the per-user checklist, grouped by hosting
// site (a multidomain site → one node with its vhosts). Unlike the gated-only list it also
// includes public backends: a route rule can change public→gated later, and you must be able
// to pre-authorize a user without re-enabling every site for everyone when a setting changes.
func (s *Server) authzTree(svc models.Services) []authzNode {
	var out []authzNode
	idx := map[string]int{}
	add := func(id, site, label string) {
		if site == "" {
			out = append(out, authzNode{Items: []authzItem{{id, label}}})
			return
		}
		if i, ok := idx[site]; ok {
			out[i].Items = append(out[i].Items, authzItem{id, label})
		} else {
			idx[site] = len(out)
			out = append(out, authzNode{Site: site, Items: []authzItem{{id, label}}})
		}
	}
	for _, b := range s.BaseBackends {
		add(b.ID, "", b.ID)
	}
	for _, b := range svc.Backends {
		site, label := "", b.ID
		if b.Hosting != nil && b.Hosting.Site != "" {
			site, label = b.Hosting.Site, b.Hosting.Vhost
			if label == "" {
				label = b.ID
			}
		}
		add(b.ID, site, label)
	}
	for _, l := range svc.Links {
		add(l.ID, "", l.ID)
	}
	return out
}

// backendGated reports whether a backend is behind the gate's auth (so a per-user
// authorization is meaningful). No explicit routes → treated as gated (conservative).
func backendGated(b models.Backend) bool {
	if len(b.Routes) == 0 {
		return true
	}
	for _, rt := range b.Routes {
		if rt.Rule != "public" {
			return true
		}
	}
	return false
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
	rule := models.CanonicalRule(r.PostFormValue("rule"))
	if id == "" || host == "" || upstream == "" {
		http.Error(w, i18n.T(s.lang(r), "err.id_host_upstream_required"), http.StatusBadRequest)
		return
	}
	if rule != models.RulePublic && rule != models.RuleAuthenticated && rule != models.RuleAuthorized {
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
	Path      string
	Exact     bool
	Rule      string
	OwnGrants bool   // false = eredita gli utenti (dall'antenato più vicino, o dal servizio)
	Users     string // emails abilitate su QUESTA directory, separate da spazio
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
		rt := models.Route{Path: "/", Rule: models.RuleAuthorized}
		if len(b.Routes) > 0 {
			rt = b.Routes[0]
		}
		var overrides []routeView
		if len(b.Routes) > 1 {
			for _, ro := range b.Routes[1:] {
				cp, exact := splitMatch(ro.Path)
				overrides = append(overrides, routeView{Path: cp, Exact: exact, Rule: ro.Rule,
					OwnGrants: ro.OwnGrants, Users: s.pathGrantees(b.ID, ro.Path)})
			}
		}
		wafEnabled, wafMode, wafRules, wafIgnore, wafCustom := false, "detect", "", "", ""
		if b.Waf != nil {
			wafEnabled = b.Waf.Enabled
			if b.Waf.Mode != "" {
				wafMode = b.Waf.Mode
			}
			ids := make([]string, len(b.Waf.DisabledRules))
			for i, id := range b.Waf.DisabledRules {
				ids[i] = strconv.Itoa(id)
			}
			wafRules = strings.Join(ids, " ")
			wafIgnore = strings.Join(b.Waf.IgnoreIPs, " ")
			wafCustom = b.Waf.CustomRules
		}
		s.renderAdminPage(w, r, "servizi", legacyhtml.AdminEditTmpl, struct {
			ID, Name, Description, Host, URL, Path, Rule, Upstream, IPAllow, Image string
			NgxTimeout, NgxMaxBody                                                 int
			NgxWS, NgxNoBuf, NgxSelfSigned, WWW, Managed, Unlisted                 bool
			NgxCustomLoc, NgxCustomSrv                                             string
			Overrides                                                              []routeView
			WafEnabled                                                             bool
			WafMode, WafDisabledRules, WafIgnoreIPs, WafCustomRules                string
			Users                                                                  []svcUserRow
		}{
			b.ID, b.Name, b.Description, b.Host, b.URL, rt.Path, rt.Rule, rt.Upstream, strings.Join(b.IPAllow, " "), b.Image,
			b.Nginx.ProxyTimeout, b.Nginx.MaxBodyMB,
			b.Nginx.WebSocket, b.Nginx.NoBuffering, b.Nginx.BackendSelfSigned, b.WWW, b.Hosting != nil, b.Unlisted,
			b.Nginx.CustomLocation, b.Nginx.CustomServer, overrides, wafEnabled, wafMode, wafRules, wafIgnore, wafCustom,
			s.serviceUsers(b.ID),
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
	rule := models.CanonicalRule(r.PostFormValue("rule"))
	if rule != models.RulePublic && rule != models.RuleAuthenticated && rule != models.RuleAuthorized {
		rule = models.RuleAuthorized
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
	imgName, imgRemove, imgErr := s.saveListingImage(r, id)
	if imgErr != nil {
		http.Error(w, imgErr.Error(), http.StatusBadRequest)
		return
	}
	pathGrants := map[string][]string{} // path che NON eredita → utenti abilitati
	err := s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Backends {
			if svc.Backends[i].ID != id {
				continue
			}
			b := &svc.Backends[i]
			b.Name = r.PostFormValue("name")
			b.Description = r.PostFormValue("description")
			b.Unlisted = r.PostFormValue("listed") == ""
			if imgRemove {
				s.removeListingImage(b.Image)
				b.Image = ""
			} else if imgName != "" {
				b.Image = imgName
			}
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
			oinherit := r.PostForm["oinherit"]
			ousers := r.PostForm["ousers"]
			for i, op := range opaths {
				cp, exact := splitMatch(strings.TrimSpace(op))
				if cp == "" {
					continue
				}
				orule := models.RuleAuthenticated
				if i < len(orules) {
					orule = orules[i]
				}
				if orule != models.RulePublic && orule != models.RuleAuthenticated && orule != models.RuleAuthorized {
					orule = "authenticated"
				}
				if i < len(omatches) && omatches[i] == "exact" {
					exact = true
				}
				own := i < len(oinherit) && oinherit[i] == "0"
				rp := joinMatch(cp, exact)
				if own {
					var em []string
					if i < len(ousers) {
						em = strings.Fields(ousers[i])
					}
					pathGrants[rp] = em
				}
				routes = append(routes, models.Route{Path: rp, Rule: orule, Upstream: up, OwnGrants: own})
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
				for _, f := range strings.Fields(r.PostFormValue("waf_disabled_rules")) {
					if id, err := strconv.Atoi(strings.TrimRight(f, ",")); err == nil && id > 0 {
						wc.DisabledRules = append(wc.DisabledRules, id)
					}
				}
				for _, ip := range strings.Fields(r.PostFormValue("waf_ignore_ips")) {
					wc.IgnoreIPs = append(wc.IgnoreIPs, strings.TrimRight(ip, ","))
				}
				wc.CustomRules = strings.TrimSpace(r.PostFormValue("waf_custom_rules"))
				b.Waf = wc
			} else {
				b.Waf = nil
			}
			return nil
		}
		return fmt.Errorf("backend not found")
	})
	if err == nil {
		err = s.applyServiceGrants(r, id)
	}
	if err == nil {
		err = s.applyPathGrants(id, pathGrants)
	}
	s.afterMutation(w, r, err)
}

// applyPathGrants writes back the per-directory grants. Every grant this service holds
// on a path is rebuilt from what the form says, so a path that goes back to inheriting
// — or is deleted — does not leave a stale permission behind that would quietly grant
// access again if the path were recreated later.
func (s *Server) applyPathGrants(backendID string, grants map[string][]string) error {
	prefix := backendID + "#"
	want := map[string]map[string]bool{} // email → set of grant ids
	for path, emails := range grants {
		gid := models.GrantID(backendID, path)
		for _, e := range emails {
			if want[e] == nil {
				want[e] = map[string]bool{}
			}
			want[e][gid] = true
		}
	}
	return s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			u := &(*users)[i]
			kept := u.Backends[:0]
			for _, id := range u.Backends {
				if !strings.HasPrefix(id, prefix) { // grant di un altro servizio o del servizio stesso
					kept = append(kept, id)
				}
			}
			u.Backends = kept
			for gid := range want[u.Email] {
				u.Backends = append(u.Backends, gid)
			}
			sort.Strings(u.Backends)
		}
		return nil
	})
}

// applyServiceGrants writes back the per-user grants edited in the Access tab. The
// hidden marker tells an unchecked-everything submit apart from a form that never
// carried the list, so saving from another tab can never silently revoke everyone.
func (s *Server) applyServiceGrants(r *http.Request, backendID string) error {
	if r.PostFormValue("svcuser_sent") == "" {
		return nil
	}
	enabled := map[string]bool{}
	for _, e := range r.PostForm["svcuser"] {
		enabled[e] = true
	}
	return s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			u := &(*users)[i]
			has := false
			for _, id := range u.Backends {
				if id == backendID {
					has = true
					break
				}
			}
			switch {
			case enabled[u.Email] && !has:
				u.Backends = append(u.Backends, backendID)
			case !enabled[u.Email] && has:
				out := u.Backends[:0]
				for _, id := range u.Backends {
					if id != backendID {
						out = append(out, id)
					}
				}
				u.Backends = out
			}
		}
		return nil
	})
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
	s.renderAdminPage(w, r, "utenti", legacyhtml.AdminQRTmpl, struct {
		Email  string
		Secret string
		QR     template.URL
	}{Email: email, Secret: secret, QR: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))})
}
