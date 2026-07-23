// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package proxy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"xaltorka/config"
	"xaltorka/models"
)

// Manager renders and writes the generated NGINX config atomically, keeping a
// timestamped snapshot of the previous version (stop&revert primitive, MYRULES
// NGINX §4).
//
// Reload: in the Docker stack the NGINX container polls conf.d and reloads
// itself, so ReloadCmd stays empty. Outside Docker (host/LXD/dedicated machine)
// set ReloadCmd to a command that reloads the local NGINX, e.g.
// "nginx -s reload" or "systemctl reload nginx"; it runs after each successful
// write.
type Manager struct {
	OutPath    string // e.g. <configdir>/nginx/conf.d/backends.conf
	BackupsDir string
	ReloadCmd  string // shell command to reload NGINX after a write ("" = no-op, Docker poller)
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
	if err := os.Rename(tmp, m.OutPath); err != nil {
		return err
	}
	return m.reload()
}

// CheckWritable verifies that the core can actually regenerate the config. The
// atomic write needs permission on the DIRECTORY holding OutPath (temp file +
// rename), not just on the file itself, so this probes the directory the same
// way Apply does.
//
// It exists because the failure it catches is otherwise SILENT: when a deploy
// leaves nginx/conf.d owned by root, services.json keeps updating (a different
// directory) and the admin UI looks perfectly healthy, but backends.conf is
// never regenerated — so no publish and no certificate ever reaches NGINX. That
// degraded a production box for a full day behind a single WARN.
//
// A nil manager or empty OutPath is a no-op (local dev without NGINX).
func (m *Manager) CheckWritable() error {
	if m == nil || m.OutPath == "" {
		return nil
	}
	dir := filepath.Dir(m.OutPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("nginx conf dir %s: %w", dir, err)
	}
	// Dotfile on purpose: it never matches the include glob (*.conf), even if we
	// were killed between the write and the cleanup.
	probe := filepath.Join(dir, ".xtk-writeprobe")
	if err := os.WriteFile(probe, []byte("xaltorka writability probe\n"), 0o644); err != nil {
		return fmt.Errorf("cannot create files in %s: %w", dir, err)
	}
	return os.Remove(probe)
}

// reload runs the configured NGINX reload command (no-op if unset). nginx itself
// validates the new config on reload and keeps the running config if it is
// invalid, so a bad generated file does not take the proxy down.
func (m *Manager) reload() error {
	if m.ReloadCmd == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sh", "-c", m.ReloadCmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx reload (%q): %w: %s", m.ReloadCmd, err, strings.TrimSpace(string(out)))
	}
	return nil
}
