// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package proxy

import (
	"strings"
	"testing"

	"xaltorka/models"
)

func TestGenerate(t *testing.T) {
	g := GenConfig{Upstream: "xaltorka:8080", GateLoginURL: "https://gate.example.com", Resolver: "127.0.0.11"}
	backends := []models.Backend{
		{
			ID:   "site",
			Host: "site.example.com",
			Routes: []models.Route{
				{Path: "/", Rule: "public", Upstream: "http://10.0.0.5:80"},
				{Path: "/api", Rule: "authenticated", Upstream: "http://10.0.0.5:8000"},
				{Path: "/admin", Rule: "whitelist", Upstream: "http://10.0.0.5:9000"},
			},
		},
	}
	out := Generate(g, backends)

	must := []string{
		"server_name site.example.com;",
		"resolver 127.0.0.11 valid=10s ipv6=off;",
		"location = /__auth {",
		"proxy_pass http://xaltorka:8080/validate;",
		"location / {",
		"set $up0 http://10.0.0.5:80;",
		"proxy_pass $up0$request_uri;",
		"location /api {",
		"location /admin {",
		"auth_request /__auth;",
		"location /login {",
		"location @login { return 302 /login?next=$request_uri; }",
	}
	for _, m := range must {
		if !strings.Contains(out, m) {
			t.Errorf("output does not contain %q\n---\n%s", m, out)
		}
	}

	// The public route must NOT have auth_request: isolate the "location /" block.
	pub := blockAfter(out, "location / {")
	if strings.Contains(pub, "auth_request") {
		t.Errorf("the public route must not have auth_request:\n%s", pub)
	}
}

func TestGenerateSkipsEmpty(t *testing.T) {
	out := Generate(GenConfig{Upstream: "x:8080"}, []models.Backend{
		{ID: "nohost", Routes: []models.Route{{Path: "/", Rule: "public", Upstream: "http://x:1"}}},
		{ID: "noroutes", Host: "h.example.com"},
	})
	if strings.Contains(out, "server {") {
		t.Errorf("backends without host or routes must not generate server{}:\n%s", out)
	}
}

// blockAfter returns the text from the first occurrence of marker up to the next
// closing brace at column 4 ("    }").
func blockAfter(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := s[i:]
	if j := strings.Index(rest, "\n    }"); j >= 0 {
		return rest[:j]
	}
	return rest
}
