// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import "testing"

func TestHostInternalize(t *testing.T) {
	cases := []struct {
		name     string
		target   string
		in, want string
	}{
		// Docker (default): localhost/127.0.0.1 → host.docker.internal
		{"docker localhost", "host.docker.internal", "http://localhost:8765/x", "http://host.docker.internal:8765/x"},
		{"docker 127", "host.docker.internal", "http://127.0.0.1:9000", "http://host.docker.internal:9000"},
		{"docker non-local unchanged", "host.docker.internal", "http://10.0.0.5:80", "http://10.0.0.5:80"},
		// Host deploy: localhost → 127.0.0.1
		{"host localhost", "127.0.0.1", "http://localhost:8765", "http://127.0.0.1:8765"},
		// Rewrite disabled: upstream unchanged
		{"disabled", "", "http://localhost:8765", "http://localhost:8765"},
		// No scheme: unchanged
		{"no scheme", "host.docker.internal", "localhost:8765", "localhost:8765"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{UpstreamLocalhost: c.target}
			if got := s.hostInternalize(c.in); got != c.want {
				t.Errorf("hostInternalize(%q) [target=%q] = %q, want %q", c.in, c.target, got, c.want)
			}
		})
	}
}
