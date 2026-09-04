// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package vpnmgr

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsDefault(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "vpn.json"))
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("default must be disabled (inert), got enabled=true")
	}
	if cfg.Driver != "wireguard" || cfg.Role != "hub" || cfg.ListenPort != 51820 {
		t.Errorf("unexpected default: %+v", cfg)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vpn.json")
	s := NewStore(path)
	in := DefaultConfig()
	in.Enabled = true
	in.Machines = []Machine{{ID: "brazzale2024", Reach: "tunnel", Addr: "10.8.0.12"}}
	in.Users = []User{{User: "agostino", PeerAddr: "10.8.0.2", CanReach: []string{"brazzale2024"}}}
	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !out.Enabled || len(out.Machines) != 1 || out.Machines[0].ID != "brazzale2024" {
		t.Errorf("roundtrip lost data: %+v", out)
	}
	if len(out.Users) != 1 || out.Users[0].CanReach[0] != "brazzale2024" {
		t.Errorf("roundtrip lost matrix: %+v", out.Users)
	}
}

func TestNewDriverSelection(t *testing.T) {
	// wireguard (and empty → wireguard) succeed; planned engines fail closed; unknown errors.
	for _, tc := range []struct {
		driver string
		ok     bool
	}{{"wireguard", true}, {"", true}, {"openvpn", false}, {"ipsec", false}, {"bogus", false}} {
		cfg := DefaultConfig()
		cfg.Driver = tc.driver
		d, err := New(cfg, nil)
		if tc.ok && (err != nil || d == nil) {
			t.Errorf("driver %q: want ok, got d=%v err=%v", tc.driver, d, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("driver %q: want error (fail-closed), got nil", tc.driver)
		}
	}
}

func TestWGStatusInertInF1(t *testing.T) {
	d, err := New(DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st, err := d.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Ready {
		t.Errorf("F1 driver must report not-ready (inert), got Ready=true")
	}
	if st.Driver != "wireguard" {
		t.Errorf("driver name: %q", st.Driver)
	}
}
