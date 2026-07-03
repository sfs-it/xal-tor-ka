// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package proxy

import (
	"os"
	"strings"
	"testing"

	"xaltorka/models"
)

// TestGenerateNginxOpts checks the per-vhost NGINX options render the expected
// directives. Set XTK_DUMP=<path> to also write the generated config to a file
// (used to run `nginx -t` against it).
func TestGenerateNginxOpts(t *testing.T) {
	be := models.Backend{
		ID: "svc", Host: "app.test",
		Nginx: models.NginxOpts{
			ProxyTimeout:      3600,
			MaxBodyMB:         50,
			WebSocket:         true,
			NoBuffering:       true,
			BackendSelfSigned: true,
			CustomServer:      "add_header X-Env test always;",
			CustomLocation:    "proxy_set_header X-Foo bar;\nproxy_redirect off;",
		},
		Routes: []models.Route{{Path: "/", Rule: "public", Upstream: "https://10.0.0.9:8443"}},
	}
	out := Generate(GenConfig{Upstream: "xaltorka:8080", Resolver: "127.0.0.11", CertDir: "/etc/nginx/certs"}, []models.Backend{be})

	for _, want := range []string{
		"map $http_upgrade $connection_upgrade",
		"client_max_body_size 50m;",
		"add_header X-Env test always;",
		"proxy_http_version 1.1;",
		"proxy_set_header Connection $connection_upgrade;",
		"proxy_ssl_verify off;",
		"proxy_ssl_server_name on;",
		"proxy_buffering off;",
		"proxy_read_timeout 3600s;",
		"proxy_send_timeout 3600s;",
		"proxy_set_header X-Foo bar;",
		"proxy_redirect off;",
		"proxy_set_header X-Forwarded-Host $host;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated config missing %q", want)
		}
	}
	if p := os.Getenv("XTK_DUMP"); p != "" {
		_ = os.WriteFile(p, []byte(out), 0o644)
	}
}
