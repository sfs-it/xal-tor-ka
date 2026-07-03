// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"xaltorka/certmgr"
	"xaltorka/i18n"
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

type tlsRow struct {
	Host   string
	Source certmgr.Source
	Expiry string
	Valid  bool
	Has    bool
}

type tlsPageData struct {
	Rows        []tlsRow
	CAAvailable bool
	Email       string
	HasMsg      bool
	Msg         string // i18n key suffix under admin.tls.*
	MsgOK       bool
}

var tlsTmpl = xtkui.LocParse("tls", `<section>
 <h2>{{T "admin.tls.h2"}}</h2>
 <p class="hint">{{T "admin.tls.hint"}}</p>
 {{if .HasMsg}}<div class="{{if .MsgOK}}ok{{else}}err{{end}}">{{T (print "admin.tls." .Msg)}}</div>{{end}}
 <table><thead><tr><th>{{T "admin.col.host"}}</th><th>{{T "admin.tls.col.source"}}</th><th>{{T "admin.tls.col.expiry"}}</th><th>{{T "admin.tls.col.status"}}</th><th></th></tr></thead><tbody>
 {{range .Rows}}<tr id="h-{{.Host}}">
  <td><code>{{.Host}}</code></td>
  <td>{{if eq (printf "%s" .Source) "acme"}}{{T "admin.tls.src.acme"}}{{else if eq (printf "%s" .Source) "selfsigned"}}{{T "admin.tls.src.selfsigned"}}{{else}}—{{end}}</td>
  <td>{{if .Has}}{{.Expiry}}{{else}}—{{end}}</td>
  <td>{{if not .Has}}<span class="tag ro">{{T "admin.tls.status.missing"}}</span>{{else if .Valid}}<span class="tag">{{T "admin.tls.status.valid"}}</span>{{else}}<span class="tag ro">{{T "admin.tls.status.expired"}}</span>{{end}}</td>
  <td><div class="actions">
   <form class="inline" method="post" action="/admin/tls/issue"><input type="hidden" name="host" value="{{.Host}}"><input type="hidden" name="mode" value="acme"><button class="btn sm">{{T "admin.tls.issue_le"}}</button></form>
   <form class="inline" method="post" action="/admin/tls/issue"><input type="hidden" name="host" value="{{.Host}}"><input type="hidden" name="mode" value="selfsigned"><button class="btn sm">{{T "admin.tls.issue_ss"}}</button></form>
   {{if .Has}}<form class="inline" method="post" action="/admin/tls/del" onsubmit="return confirm('{{T "admin.confirm_del"}}')"><input type="hidden" name="host" value="{{.Host}}"><button class="btn danger sm">{{T "admin.act.delete"}}</button></form>{{end}}
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
	if s.CertMgr != nil {
		for _, in := range s.CertMgr.List(s.servedHosts()) {
			row := tlsRow{Host: in.Host, Source: in.Source, Valid: in.Valid, Has: in.Source != certmgr.SourceNone}
			if row.Has {
				row.Expiry = in.NotAfter.Format("2006-01-02")
			}
			data.Rows = append(data.Rows, row)
		}
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
	var err error
	if mode == "acme" {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		err = s.CertMgr.IssueACME(ctx, host)
	} else {
		err = s.CertMgr.IssueSelfSigned(host)
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
	if err := s.CertMgr.IssueACME(ctx, host); err != nil {
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
