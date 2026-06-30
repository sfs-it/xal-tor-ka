package proxy

import (
	"os"
	"path/filepath"
	"time"

	"xaltorka/config"
	"xaltorka/models"
)

// Manager renders and writes the generated NGINX config atomically, keeping a
// timestamped snapshot of the previous version (stop&revert primitive, MYRULES
// NGINX §4). Validation (`nginx -t`) and reload happen in the NGINX container.
type Manager struct {
	OutPath    string // e.g. <configdir>/nginx/conf.d/backends.conf
	BackupsDir string
	Gen        GenConfig
}

// Apply regenerates the config for the given backends and writes it atomically.
// A nil manager or empty OutPath is a no-op (e.g. local dev without NGINX).
func (m *Manager) Apply(backends []models.Backend) error {
	if m == nil || m.OutPath == "" {
		return nil
	}
	conf := Generate(m.Gen, backends)
	if err := os.MkdirAll(filepath.Dir(m.OutPath), 0o755); err != nil {
		return err
	}
	if old, err := os.ReadFile(m.OutPath); err == nil && m.BackupsDir != "" {
		if os.MkdirAll(m.BackupsDir, 0o700) == nil {
			stamp := time.Now().Format("20060102-150405")
			_ = os.WriteFile(filepath.Join(m.BackupsDir, "backends-"+stamp+".conf"), old, 0o644)
			config.PruneBackups(m.BackupsDir, "backends", "conf", 10)
		}
	}
	tmp := m.OutPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(conf), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.OutPath)
}
