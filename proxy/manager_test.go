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
