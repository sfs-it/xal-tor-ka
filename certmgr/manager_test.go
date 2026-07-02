// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package certmgr

import (
	"testing"
	"time"
)

func TestSelfSignedIssuance(t *testing.T) {
	dir := t.TempDir()
	reloaded := false
	m := &Manager{Dir: dir, NginxDir: "/etc/nginx/certs", Reload: func() error { reloaded = true; return nil }}

	if m.HasCert("app.test") {
		t.Fatal("HasCert should be false before issuance")
	}
	if got := m.Info("missing.test").Source; got != SourceNone {
		t.Fatalf("missing host source = %q, want none", got)
	}

	if err := m.IssueSelfSigned("app.test"); err != nil {
		t.Fatalf("IssueSelfSigned: %v", err)
	}
	if !reloaded {
		t.Error("reload hook was not invoked after issuance")
	}
	if !m.HasCert("app.test") {
		t.Fatal("HasCert should be true after issuance")
	}
	if !m.CAExists() {
		t.Fatal("internal CA should exist after the first self-signed issuance")
	}

	in := m.Info("app.test")
	if in.Source != SourceSelfSigned {
		t.Errorf("source = %q, want selfsigned", in.Source)
	}
	if !in.Valid {
		t.Error("freshly issued cert should be valid")
	}
	if time.Until(in.NotAfter) < 24*time.Hour {
		t.Errorf("NotAfter too soon: %v", in.NotAfter)
	}

	if pem, err := m.CACertPEM(); err != nil || len(pem) == 0 {
		t.Errorf("CACertPEM = %d bytes, err %v", len(pem), err)
	}

	// A second host reuses the same CA (no regeneration error).
	if err := m.IssueSelfSigned("two.test"); err != nil {
		t.Fatalf("second IssueSelfSigned: %v", err)
	}

	if err := m.Delete("app.test"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if m.HasCert("app.test") {
		t.Error("HasCert should be false after Delete")
	}
}

func TestNginxCertPaths(t *testing.T) {
	m := &Manager{Dir: "/data/certs", NginxDir: "/etc/nginx/certs"}
	if got := m.NginxCertPath("h.test"); got != "/etc/nginx/certs/h.test.crt" {
		t.Errorf("NginxCertPath = %q", got)
	}
	if got := m.NginxKeyPath("h.test"); got != "/etc/nginx/certs/h.test.key" {
		t.Errorf("NginxKeyPath = %q", got)
	}
}
