// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package proxy

import (
	"os"
	"path/filepath"
	"testing"

	"xaltorka/models"
)

func newManager(dir, reload string) *Manager {
	return &Manager{
		OutPath:    filepath.Join(dir, "backends.conf"),
		BackupsDir: filepath.Join(dir, "backups"),
		ReloadCmd:  reload,
		Gen:        GenConfig{Upstream: "xaltorka:8080", Resolver: "127.0.0.11"},
	}
}

func TestApplyWritesConfig(t *testing.T) {
	dir := t.TempDir()
	m := newManager(dir, "") // no reload (Docker default)
	if err := m.Apply([]models.Backend{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(m.OutPath); err != nil {
		t.Errorf("backends.conf non scritto: %v", err)
	}
}

func TestApplyReloadOK(t *testing.T) {
	dir := t.TempDir()
	m := newManager(dir, "true") // reload command that succeeds
	if err := m.Apply([]models.Backend{}); err != nil {
		t.Fatalf("Apply with reload ok: %v", err)
	}
}

func TestApplyReloadFails(t *testing.T) {
	dir := t.TempDir()
	m := newManager(dir, "false") // reload command that fails
	err := m.Apply([]models.Backend{})
	if err == nil {
		t.Fatal("Apply should have propagated the reload failure")
	}
	// The file is written anyway before the reload (the reload is the last step).
	if _, statErr := os.Stat(m.OutPath); statErr != nil {
		t.Errorf("backends.conf should have been written anyway: %v", statErr)
	}
}

func TestApplyNilManager(t *testing.T) {
	var m *Manager
	if err := m.Apply([]models.Backend{}); err != nil {
		t.Errorf("Apply on a nil manager must be a no-op, err=%v", err)
	}
}

func TestCheckWritableOK(t *testing.T) {
	dir := t.TempDir()
	m := newManager(dir, "")
	if err := m.CheckWritable(); err != nil {
		t.Fatalf("CheckWritable on a writable dir: %v", err)
	}
	// The probe must not survive: a leftover file in conf.d is litter.
	if _, err := os.Stat(filepath.Join(dir, ".xtk-writeprobe")); !os.IsNotExist(err) {
		t.Errorf("the probe file was left behind (err=%v)", err)
	}
}

// The silent failure this whole check exists for: the directory is not writable
// by the core, so the atomic write of backends.conf can never land.
func TestCheckWritableFailsOnReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions would not block the write")
	}
	dir := t.TempDir()
	confDir := filepath.Join(dir, "conf.d")
	if err := os.Mkdir(confDir, 0o555); err != nil { // r-xr-xr-x: no write
		t.Fatalf("mkdir: %v", err)
	}
	m := &Manager{OutPath: filepath.Join(confDir, "backends.conf")}
	if err := m.CheckWritable(); err == nil {
		t.Fatal("CheckWritable must fail when the core cannot write in conf.d")
	}
}

func TestCheckWritableNilManager(t *testing.T) {
	var m *Manager
	if err := m.CheckWritable(); err != nil {
		t.Errorf("CheckWritable on a nil manager must be a no-op, err=%v", err)
	}
	if err := (&Manager{}).CheckWritable(); err != nil {
		t.Errorf("CheckWritable with an empty OutPath must be a no-op, err=%v", err)
	}
}
