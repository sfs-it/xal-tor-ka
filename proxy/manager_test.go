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
	m := newManager(dir, "") // niente reload (default Docker)
	if err := m.Apply([]models.Backend{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(m.OutPath); err != nil {
		t.Errorf("backends.conf non scritto: %v", err)
	}
}

func TestApplyReloadOK(t *testing.T) {
	dir := t.TempDir()
	m := newManager(dir, "true") // comando di reload che riesce
	if err := m.Apply([]models.Backend{}); err != nil {
		t.Fatalf("Apply con reload ok: %v", err)
	}
}

func TestApplyReloadFails(t *testing.T) {
	dir := t.TempDir()
	m := newManager(dir, "false") // comando di reload che fallisce
	err := m.Apply([]models.Backend{})
	if err == nil {
		t.Fatal("Apply doveva propagare il fallimento del reload")
	}
	// Il file viene comunque scritto prima del reload (il reload è l'ultimo passo).
	if _, statErr := os.Stat(m.OutPath); statErr != nil {
		t.Errorf("backends.conf doveva essere scritto comunque: %v", statErr)
	}
}

func TestApplyNilManager(t *testing.T) {
	var m *Manager
	if err := m.Apply([]models.Backend{}); err != nil {
		t.Errorf("Apply su manager nil deve essere no-op, err=%v", err)
	}
}
