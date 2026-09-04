// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import "net/http"

// handleVpnProxy reverse-proxies /admin/vpn/* to the vpn extension, enforcing the
// admin session first (adminSessionOK, which — unlike adminGuard — leaves the request
// body intact for the proxied POST). The extension itself has no host powers; it drives
// the vetted xtk-vpn-agent over a unix socket, which in turn drives the data-plane.
// Twin of handleHostingProxy (see DRAFT-ext-module.md, the reusable module pattern).
func (s *Server) handleVpnProxy(w http.ResponseWriter, r *http.Request) {
	if s.VpnUpstream == "" || s.vpnProxy == nil {
		http.NotFound(w, r)
		return
	}
	if !s.adminSessionOK(w, r) {
		return
	}
	s.vpnProxy.ServeHTTP(w, r)
}
