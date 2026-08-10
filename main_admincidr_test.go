// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package main

import (
	"os"
	"path/filepath"
	"testing"

	"xaltorka/config"
	"xaltorka/models"
)

func TestParseCIDRList(t *testing.T) {
	got, err := parseCIDRList([]string{"10.0.0.0/24, 1.2.3.4", "::1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.0/24", "1.2.3.4/32", "::1/128"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if _, err := parseCIDRList([]string{"not-an-ip"}); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestDedupAndOpen(t *testing.T) {
	if d := dedupCIDRs([]string{"a", "a", "b", "a"}); len(d) != 2 || d[0] != "a" || d[1] != "b" {
		t.Fatalf("dedup: %v", d)
	}
	if !adminCIDRsOpen([]string{"1.2.3.4/32", "0.0.0.0/0"}) {
		t.Fatal("0.0.0.0/0 must be open")
	}
	if !adminCIDRsOpen([]string{"::/0"}) {
		t.Fatal("::/0 must be open")
	}
	if adminCIDRsOpen([]string{"10.0.0.0/8"}) {
		t.Fatal("10.0.0.0/8 is not open")
	}
}

func TestRunAdminCIDR(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"admin":{"ip_whitelist":["127.0.0.1/32"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "services.json"),
		[]byte(`{"backends":[],"links":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	read := func() models.Services {
		svc, err := config.LoadServices(filepath.Join(dir, "services.json"))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}

	if err := runAdminCIDR([]string{"-config", dir, "set", "10.0.0.0/24"}); err != nil {
		t.Fatal(err)
	}
	if s := read(); len(s.AdminIPWhitelist) != 1 || s.AdminIPWhitelist[0] != "10.0.0.0/24" {
		t.Fatalf("set: %v", s.AdminIPWhitelist)
	}

	if err := runAdminCIDR([]string{"-config", dir, "add", "1.2.3.4"}); err != nil {
		t.Fatal(err)
	}
	if s := read(); len(s.AdminIPWhitelist) != 2 || s.AdminIPWhitelist[1] != "1.2.3.4/32" {
		t.Fatalf("add: %v", s.AdminIPWhitelist)
	}

	// add of an existing entry is a no-op (dedup)
	if err := runAdminCIDR([]string{"-config", dir, "add", "1.2.3.4/32"}); err != nil {
		t.Fatal(err)
	}
	if s := read(); len(s.AdminIPWhitelist) != 2 {
		t.Fatalf("add-dup: %v", s.AdminIPWhitelist)
	}

	if err := runAdminCIDR([]string{"-config", dir, "open"}); err != nil {
		t.Fatal(err)
	}
	if s := read(); len(s.AdminIPWhitelist) != 1 || s.AdminIPWhitelist[0] != "0.0.0.0/0" {
		t.Fatalf("open: %v", s.AdminIPWhitelist)
	}

	if err := runAdminCIDR([]string{"-config", dir, "clear"}); err != nil {
		t.Fatal(err)
	}
	if s := read(); len(s.AdminIPWhitelist) != 0 {
		t.Fatalf("clear: %v", s.AdminIPWhitelist)
	}

	if err := runAdminCIDR([]string{"-config", dir, "set"}); err == nil {
		t.Fatal("set with no CIDR must error")
	}
}
