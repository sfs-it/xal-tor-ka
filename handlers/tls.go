// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"xaltorka/certmgr"
	"xaltorka/i18n"
	"xaltorka/legacyhtml"
	"xaltorka/models"
)

// servedHosts returns the unique, non-empty hosts currently proxied (resolver
// backends), which are the candidates for a TLS certificate.
func (s *Server) servedHosts() []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range s.Resolver.Backends() {
		if b.Host == "" || seen[b.Host] {
			continue
		}
		seen[b.Host] = true
		out = append(out, b.Host)
	}
	return out
}

// servedHostsForSite returns the served hosts of the backends managed by the hosting
// site `site` (its domain + child vhosts) — used to scope the TLS page from Hosting.
func (s *Server) servedHostsForSite(site string) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range s.Resolver.Backends() {
		if b.Host == "" || seen[b.Host] || !backendBelongsToSite(b, site) {
			continue
		}
		seen[b.Host] = true
		out = append(out, b.Host)
	}
	return out
}

// backendBelongsToSite reports whether a gateway backend is served by hosting site
// `site`. Marked backends carry the Hosting reference; legacy ones (published before
// the marker existed) are matched by their upstream alias (<site>.site or
// <site>-<vhost>.site) — so the TLS filter finds them too.
func backendBelongsToSite(b models.Backend, site string) bool {
	if b.Hosting != nil {
		return b.Hosting.Site == site
	}
	for _, rt := range b.Routes {
		h := upstreamHost(rt.Upstream)
		if h == site+".site" || (strings.HasPrefix(h, site+"-") && strings.HasSuffix(h, ".site")) {
			return true
		}
	}
	return false
}

// upstreamHost extracts the host from an upstream URL like http://alias.site:8080.
func upstreamHost(u string) string {
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")
	if i := strings.IndexAny(u, ":/"); i >= 0 {
		u = u[:i]
	}
	return u
}

type tlsRow struct {
	Host   string
	Source certmgr.Source
	Expiry string
	Valid  bool
	Has    bool
	WWW    bool // backend also serves/certs www.<host>
	Sub    bool // rendered as a sub-row nested under its parent domain (e.g. app.segnalapa.it under segnalapa.it)
}

// registrableDomain returns the last two labels of host (eTLD+1 for the
// single-label TLDs we serve: .it/.com/.eu/.localhost), used to group
// subdomain cert rows under their parent domain.
func registrableDomain(host string) string {
	p := strings.Split(host, ".")
	if len(p) <= 2 {
		return host
	}
	return strings.Join(p[len(p)-2:], ".")
}

// groupTLSRows reorders rows so each parent domain is immediately followed by its
// subdomains (marked Sub for indented rendering). Group order follows first
// appearance; subdomains are sorted alphabetically within a group.
func groupTLSRows(rows []tlsRow) []tlsRow {
	var order []string
	groups := map[string][]tlsRow{}
	for _, r := range rows {
		p := registrableDomain(r.Host)
		if _, ok := groups[p]; !ok {
			order = append(order, p)
		}
		groups[p] = append(groups[p], r)
	}
	out := make([]tlsRow, 0, len(rows))
	for _, p := range order {
		var head, subs []tlsRow
		for _, r := range groups[p] {
			if r.Host == p {
				head = append(head, r)
			} else {
				r.Sub = true
				subs = append(subs, r)
			}
		}
		sort.Slice(subs, func(i, j int) bool { return subs[i].Host < subs[j].Host })
		out = append(out, head...)
		out = append(out, subs...)
	}
	return out
}

// hostWWW reports whether the backend for host wants the www.<host> alias.
func (s *Server) hostWWW(host string) bool {
	for _, b := range s.Resolver.Backends() {
		if b.Host == host {
			return b.WWW
		}
	}
	return false
}

// setBackendWWW persists the WWW flag on the services.json backend(s) for host and
// reloads (so the vhost server_name is regenerated before an ACME challenge).
func (s *Server) setBackendWWW(host string, www bool) error {
	return s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Backends {
			if svc.Backends[i].Host == host {
				svc.Backends[i].WWW = www
			}
		}
		return nil
	})
}

type tlsPageData struct {
	Rows        []tlsRow
	CAAvailable bool
	Email       string
	Site        string // when set, the list is scoped to a hosting site (domain + vhosts)
	HasMsg      bool
	Msg         string // i18n key suffix under admin.tls.*
	MsgOK       bool
}

// handleAdminTLS renders the certificate list + CA download.
func (s *Server) handleAdminTLS(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	data := tlsPageData{}
	hosts := s.servedHosts()
	if site := r.URL.Query().Get("site"); site != "" {
		hosts, data.Site = s.servedHostsForSite(site), site
	}
	if s.CertMgr != nil {
		for _, in := range s.CertMgr.List(hosts) {
			row := tlsRow{Host: in.Host, Source: in.Source, Valid: in.Valid, Has: in.Source != certmgr.SourceNone, WWW: s.hostWWW(in.Host)}
			if row.Has {
				row.Expiry = in.NotAfter.Format("2006-01-02")
			}
			data.Rows = append(data.Rows, row)
		}
		data.Rows = groupTLSRows(data.Rows)
		data.CAAvailable = s.CertMgr.CAExists()
		data.Email = s.Cfg.TLS.ACME.Email
	}
	if msg := r.URL.Query().Get("msg"); msg != "" {
		data.HasMsg, data.Msg, data.MsgOK = true, msg, r.URL.Query().Get("ok") == "1"
	}
	s.renderAdminPage(w, r, "tls", legacyhtml.TlsTmpl, data)
}

// handleTLSIssue issues a certificate (mode = acme | selfsigned) for a host.
func (s *Server) handleTLSIssue(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	host := r.PostFormValue("host")
	mode := r.PostFormValue("mode")
	if s.CertMgr == nil || host == "" {
		s.tlsRedirect(w, r, "issue_failed", false)
		return
	}
	// Persist the www flag first (drives the vhost server_name) and reload, so the
	// :80 vhost answers www.<host> for its ACME challenge before we issue.
	www := r.PostFormValue("www") != ""
	_ = s.setBackendWWW(host, www)
	var extra []string
	if www {
		extra = []string{"www." + host}
	}
	var err error
	if mode == "acme" {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		err = s.CertMgr.IssueACME(ctx, host, extra...)
	} else {
		err = s.CertMgr.IssueSelfSigned(host, extra...)
	}
	if err != nil {
		slog.Warn("tls issue failed", "host", host, "mode", mode, "err", err)
		s.tlsRedirect(w, r, "issue_failed", false)
		return
	}
	s.tlsRedirect(w, r, "issued", true)
}

// handleTLSRenew re-runs ACME issuance for a host.
func (s *Server) handleTLSRenew(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	host := r.PostFormValue("host")
	if s.CertMgr == nil || host == "" {
		s.tlsRedirect(w, r, "issue_failed", false)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var extra []string
	if s.hostWWW(host) {
		extra = []string{"www." + host}
	}
	if err := s.CertMgr.IssueACME(ctx, host, extra...); err != nil {
		slog.Warn("tls renew failed", "host", host, "err", err)
		s.tlsRedirect(w, r, "issue_failed", false)
		return
	}
	s.tlsRedirect(w, r, "renewed", true)
}

// handleTLSDelete removes a host certificate.
func (s *Server) handleTLSDelete(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	host := r.PostFormValue("host")
	if s.CertMgr != nil && host != "" {
		_ = s.CertMgr.Delete(host)
	}
	s.tlsRedirect(w, r, "deleted", true)
}

// handleTLSCA serves the internal CA certificate for client installation.
func (s *Server) handleTLSCA(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	if s.CertMgr == nil || !s.CertMgr.CAExists() {
		http.NotFound(w, r)
		return
	}
	pem, err := s.CertMgr.CACertPEM()
	if err != nil {
		http.Error(w, i18n.T(s.lang(r), "err.internal"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="xaltorka-ca.crt"`)
	_, _ = w.Write(pem)
}

func (s *Server) tlsRedirect(w http.ResponseWriter, r *http.Request, msg string, ok bool) {
	okv := "0"
	if ok {
		okv = "1"
	}
	http.Redirect(w, r, "/admin/tls?msg="+msg+"&ok="+okv, http.StatusSeeOther)
}
