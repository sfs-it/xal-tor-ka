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

// A hostname may host several services: the site itself on "/" plus reverse-proxied
// services on sub-paths. They must collapse into ONE server{} (a duplicate
// server_name would make nginx silently shadow all but the first), each keeping its
// own custom_location so a per-service secret never leaks into its neighbours.
func TestGenerateGroupsBackendsByHost(t *testing.T) {
	g := GenConfig{Upstream: "xaltorka:8080", Resolver: "127.0.0.11"}
	backends := []models.Backend{
		{
			ID: "sfsit-bottiglia-tunnel", Host: "sfs.it",
			Routes: []models.Route{{Path: "/bottiglia2", Rule: "whitelist", Upstream: "http://tunnel:8770"}},
			Nginx:  models.NginxOpts{CustomLocation: "proxy_set_header X-Gate \"s3cr3t\";"},
		},
		{
			ID: "sfsit", Host: "sfs.it", WWW: true,
			Routes: []models.Route{{Path: "/", Rule: "public", Upstream: "http://sfsit.site:8080"}},
		},
	}
	out := Generate(g, backends)

	if n := strings.Count(out, "server {"); n != 1 {
		t.Fatalf("want a single server block for the shared host, got %d\n%s", n, out)
	}
	if n := strings.Count(out, "server_name sfs.it www.sfs.it;"); n != 1 {
		t.Errorf("the primary (owner of /) must drive server_name+www, got:\n%s", out)
	}
	for _, want := range []string{"location /bottiglia2 {", "location / {", "http://tunnel:8770", "/login"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The secret belongs to the sub-path service ONLY.
	if n := strings.Count(out, "s3cr3t"); n != 1 {
		t.Errorf("per-service custom_location must not leak to sibling locations (found %d times)\n%s", n, out)
	}
	seg := out[strings.Index(out, "location / {"):]
	if strings.Contains(seg[:strings.Index(seg, "}")], "s3cr3t") {
		t.Error("the public root location must not carry the other service's secret")
	}
}

// Two services declaring the same host+path must not emit a duplicate location{}:
// nginx would reject the entire config and every site would go down.
func TestGenerateDeduplicatesSamePath(t *testing.T) {
	g := GenConfig{Upstream: "xaltorka:8080"}
	backends := []models.Backend{
		{ID: "a", Host: "dup.example.com", Routes: []models.Route{{Path: "/", Rule: "public", Upstream: "http://a:80"}}},
		{ID: "b", Host: "dup.example.com", Routes: []models.Route{{Path: "/", Rule: "public", Upstream: "http://b:80"}}},
	}
	out := Generate(g, backends)
	if n := strings.Count(out, "location / {"); n != 1 {
		t.Fatalf("duplicate host+path must collapse to one location, got %d\n%s", n, out)
	}
}
