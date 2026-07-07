// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import "net/http"

// handleHostingProxy reverse-proxies /admin/hosting/* to the hosting extension,
// enforcing the admin session first (adminSessionOK, which — unlike adminGuard —
// leaves the request body intact for the proxied POST). The extension itself has no
// host powers; it drives the vetted xtk-agent over a unix socket.
func (s *Server) handleHostingProxy(w http.ResponseWriter, r *http.Request) {
	if s.HostingUpstream == "" || s.hostingProxy == nil {
		http.NotFound(w, r)
		return
	}
	if !s.adminSessionOK(w, r) {
		return
	}
	s.hostingProxy.ServeHTTP(w, r)
}
