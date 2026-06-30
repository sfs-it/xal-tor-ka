// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package proxy generates the NGINX reverse-proxy configuration from the
// resolved backend set and applies it atomically. See BLUEPRINT.md §16.3 and
// the auth_request contract §4. The generated file is consumed by the NGINX
// container, which validates (`nginx -t`) and hot-reloads it.
package proxy

import (
	"fmt"
	"sort"
	"strings"

	"xaltorka/models"
)

// GenConfig holds the parameters needed to render the vhosts.
type GenConfig struct {
	// Upstream is the internal address of the Go auth service, e.g. "xaltorka:8080".
	Upstream string
	// GateLoginURL is the public base URL of the gateway (for the login redirect).
	GateLoginURL string
	// Resolver is the DNS server NGINX uses to resolve backend upstreams at
	// request time (docker embedded DNS = 127.0.0.11). When set, proxy_pass uses
	// a variable so NGINX won't fail to (re)load if a backend is temporarily down.
	Resolver string
}

// Generate renders one server{} block per backend host. Backends with no host
// or no routes are skipped. Output is deterministic (hosts sorted).
func Generate(g GenConfig, backends []models.Backend) string {
	var b strings.Builder
	b.WriteString("# GENERATO da Xal-Tor-Ka proxy manager — NON editare a mano.\n")
	b.WriteString("# Si rigenera a ogni reload/avvio del servizio.\n\n")

	sorted := append([]models.Backend{}, backends...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Host < sorted[j].Host })

	for _, be := range sorted {
		if be.Host == "" || len(be.Routes) == 0 {
			continue
		}
		writeServer(&b, g, be)
	}
	return b.String()
}

func writeServer(b *strings.Builder, g GenConfig, be models.Backend) {
	fmt.Fprintf(b, "server {\n")
	b.WriteString("    listen 80;\n")
	fmt.Fprintf(b, "    server_name %s;\n", be.Host)
	if g.Resolver != "" {
		fmt.Fprintf(b, "    resolver %s valid=10s ipv6=off;\n", g.Resolver)
	}
	b.WriteString("    add_header X-Frame-Options \"SAMEORIGIN\" always;\n")
	b.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
	b.WriteString("    add_header X-XSS-Protection \"0\" always;\n\n")

	fmt.Fprintf(b, "    location = /__auth {\n"+
		"        internal;\n"+
		"        proxy_pass http://%s/validate;\n"+
		"        proxy_pass_request_body off;\n"+
		"        proxy_set_header Content-Length \"\";\n"+
		"        proxy_set_header X-Original-Host $host;\n"+
		"        proxy_set_header X-Original-URI $request_uri;\n"+
		"        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n"+
		"    }\n\n", g.Upstream)

	// Endpoint dell'interfaccia di Xal-Tor-Ka serviti su QUESTO host: login e 2FA
	// avvengono sul dominio del backend → cookie host-only affidabile (no SSO
	// cross-sottodominio, inaffidabile su *.localhost). In produzione, per il SSO,
	// usa un dominio padre reale + session.cookie_domain.
	for _, p := range []string{"/login", "/auth/", "/logout", "/listing", "/assets/"} {
		fmt.Fprintf(b, "    location %s {\n", p)
		fmt.Fprintf(b, "        proxy_pass http://%s;\n", g.Upstream)
		b.WriteString("        proxy_set_header Host $host;\n")
		b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
		b.WriteString("    }\n\n")
	}

	for i, rt := range be.Routes {
		writeLocation(b, g, rt, i)
	}

	// Redirect al login SULLO STESSO host (cookie host-only), poi ritorno alla
	// pagina richiesta.
	b.WriteString("    location @login { return 302 /login?next=$request_uri; }\n")
	b.WriteString("    location @forbidden { return 403; }\n")
	b.WriteString("}\n\n")
}

func writeLocation(b *strings.Builder, g GenConfig, rt models.Route, idx int) {
	path := rt.Path
	if path == "" {
		path = "/"
	}
	fmt.Fprintf(b, "    location %s {\n", path)
	if rt.Rule != "public" {
		b.WriteString("        auth_request /__auth;\n")
		b.WriteString("        error_page 401 = @login;\n")
		b.WriteString("        error_page 403 = @forbidden;\n")
		b.WriteString("        auth_request_set $auth_user $upstream_http_x_auth_user;\n")
		b.WriteString("        proxy_set_header X-Auth-User $auth_user;\n")
	}
	// With a resolver, proxy_pass via a variable defers DNS to request time, so a
	// down/unresolvable backend doesn't break nginx reload. $request_uri preserves
	// the original path+query (variable proxy_pass doesn't append it automatically).
	if g.Resolver != "" {
		fmt.Fprintf(b, "        set $up%d %s;\n", idx, rt.Upstream)
		fmt.Fprintf(b, "        proxy_pass $up%d$request_uri;\n", idx)
	} else {
		fmt.Fprintf(b, "        proxy_pass %s;\n", rt.Upstream)
	}
	b.WriteString("        proxy_set_header Host $host;\n")
	b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
	b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
	b.WriteString("    }\n\n")
}
