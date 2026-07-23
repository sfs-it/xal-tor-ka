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
	"xaltorka/models"
	"xaltorka/xtkui"
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

var tlsTmpl = xtkui.LocParse("tls", `<section>
 <h2>{{T "admin.tls.h2"}}</h2>
 <p class="hint">{{T "admin.tls.hint"}}</p>
 <div id="le-overlay" style="display:none;position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:9999;align-items:center;justify-content:center">
  <div style="background:var(--panel,#fff);color:var(--ink,#111);padding:1.6rem 2rem;border-radius:12px;text-align:center;max-width:90%;box-shadow:0 8px 40px rgba(0,0,0,.4)">
   <p style="font-size:1.6rem;margin:0 0 .4rem">⏳</p>
   <p style="margin:.2rem 0"><b>Emissione certificato Let's Encrypt</b><br>per <code id="le-host"></code>…</p>
   <p class="hint" style="margin:.4rem 0 0">L'operazione ACME può durare qualche secondo, non chiudere la pagina.</p>
  </div>
 </div>
 <script>function xtkLE(h){var o=document.getElementById('le-overlay');document.getElementById('le-host').textContent=h;o.style.display='flex';return true;}</script>
 {{if .Site}}<p class="hint">🏠 {{T "admin.tls.site_scope"}} <b>{{.Site}}</b>. <a href="/admin/tls">{{T "admin.tls.show_all"}} →</a></p>{{end}}
 {{if .HasMsg}}<div class="{{if .MsgOK}}ok{{else}}err{{end}}">{{T (print "admin.tls." .Msg)}}</div>{{end}}
 <table><thead><tr><th>{{T "admin.col.host"}}</th><th>{{T "admin.tls.col.source"}}</th><th>{{T "admin.tls.col.expiry"}}</th><th>{{T "admin.tls.col.status"}}</th><th></th></tr></thead><tbody>
 {{range .Rows}}<tr id="h-{{.Host}}"{{if .Sub}} class="tls-sub"{{end}}>
  <td>{{if .Sub}}<span class="tls-branch" aria-hidden="true">↳</span> {{end}}<code>{{.Host}}</code></td>
  <td>{{if eq (printf "%s" .Source) "acme"}}{{T "admin.tls.src.acme"}}{{else if eq (printf "%s" .Source) "selfsigned"}}{{T "admin.tls.src.selfsigned"}}{{else}}—{{end}}</td>
  <td>{{if .Has}}{{.Expiry}}{{else}}—{{end}}</td>
  <td>{{if not .Has}}<span class="tag err stw">{{T "admin.tls.status.missing"}}</span>{{else if .Valid}}<span class="tag ok stw">{{T "admin.tls.status.valid"}}</span>{{else}}<span class="tag warn stw">{{T "admin.tls.status.expired"}}</span>{{end}}</td>
  <td><div class="actions">
   <form class="inline" method="post" action="/admin/tls/issue">
    <input type="hidden" name="host" value="{{.Host}}">
    <label class="hint" title="also serve/cert www.{{.Host}}" style="display:inline-flex;align-items:center;gap:.25rem"><input type="checkbox" name="www" value="1"{{if .WWW}} checked{{end}}> www.</label>
    <button class="btn sm" name="mode" value="acme" onclick="xtkLE('{{.Host}}')">{{T "admin.tls.issue_le"}}</button>
    <button class="btn sm" name="mode" value="selfsigned">{{T "admin.tls.issue_ss"}}</button>
   </form>
   {{if .Has}}<form class="inline" method="post" action="/admin/tls/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="host" value="{{.Host}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>{{else}}<button class="btn danger sm" disabled title="no certificate to delete">{{T "admin.act.delete"}}</button>{{end}}
  </div></td></tr>{{end}}
 {{if not .Rows}}<tr><td colspan="5" class="empty">{{T "admin.tls.none"}}</td></tr>{{end}}
 </tbody></table>
</section>
<section style="margin-top:1.4rem">
 <h2>{{T "admin.tls.ca_h"}}</h2>
 <div class="card">
  {{if .CAAvailable}}<p><a class="btn" href="/admin/tls/ca.crt">{{T "admin.tls.ca_dl"}}</a></p>
  <p class="hint">{{T "admin.tls.ca_hint"}}</p>
  {{else}}<p class="hint">{{T "admin.tls.ca_none"}}</p>{{end}}
  <p class="hint">{{T "admin.tls.email"}}: <code>{{if .Email}}{{.Email}}{{else}}—{{end}}</code> — {{T "admin.tls.email_hint"}}</p>
 </div>
</section>`)

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
	s.renderAdminPage(w, r, "tls", tlsTmpl, data)
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
