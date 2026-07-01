// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xaltorka/models"
)

// backupsKeep is how many timestamped snapshots to retain per kind (auto-trash).
const backupsKeep = 10

// LoadConfigOnly decodes just config.json (with ${VAR} expansion), without
// requiring secrets.json / users.json. Used by the `setup` CLI subcommand,
// which may run before those files exist.
func LoadConfigOnly(dir string) (models.Config, error) {
	var c models.Config
	if err := decodeFile(filepath.Join(dir, "config.json"), &c); err != nil {
		return c, err
	}
	return c, nil
}

// SaveUsers writes users.json atomically, snapshotting the previous version into
// backupsDir first (BLUEPRINT §12, minimal form). Perms 0600 (contains secrets).
func SaveUsers(usersPath, backupsDir string, users models.Users) error {
	snapshotAndPrune(usersPath, backupsDir, "users", "json")
	return writeJSONAtomic(usersPath, users, 0o600)
}

// LoadUsers reads users.json (lenient). A missing file yields an empty set.
func LoadUsers(path string) (models.Users, error) {
	var u models.Users
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return u, nil
		}
		return u, fmt.Errorf("read users %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return u, fmt.Errorf("parse users %s: %w", path, err)
	}
	return u, nil
}

// LoadServices reads services.json. A missing file is not an error (returns an
// empty set): runtime-managed services are optional.
func LoadServices(path string) (models.Services, error) {
	var svc models.Services
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return svc, nil
		}
		return svc, fmt.Errorf("read services file %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &svc); err != nil {
		return svc, fmt.Errorf("parse services file %s: %w", path, err)
	}
	return svc, nil
}

// SaveServices writes services.json atomically, snapshotting the previous
// version into backupsDir first.
func SaveServices(servicesPath, backupsDir string, svc models.Services) error {
	snapshotAndPrune(servicesPath, backupsDir, "services", "json")
	return writeJSONAtomic(servicesPath, svc, 0o644)
}

// LoadSecretsRaw reads secrets.json as-is (no ${VAR} expansion, no strictness),
// for runtime read/modify/write of the admin password. A missing file yields an
// empty Secrets. Unknown env placeholders are preserved verbatim on round-trip.
func LoadSecretsRaw(path string) (models.Secrets, error) {
	var sec models.Secrets
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sec, nil
		}
		return sec, fmt.Errorf("read secrets %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &sec); err != nil {
		return sec, fmt.Errorf("parse secrets %s: %w", path, err)
	}
	return sec, nil
}

// SaveSecrets writes secrets.json atomically (0600), snapshotting the previous
// version into backupsDir first.
func SaveSecrets(secretsPath, backupsDir string, sec models.Secrets) error {
	snapshotAndPrune(secretsPath, backupsDir, "secrets", "json")
	return writeJSONAtomic(secretsPath, sec, 0o600)
}

// LoadSetup reads the setup state file (data/setup.json).
func LoadSetup(path string) (models.SetupState, error) {
	var st models.SetupState
	raw, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return st, fmt.Errorf("parse setup file %s: %w", path, err)
	}
	return st, nil
}

// SaveSetup writes the setup state file atomically (0600, in data/).
func SaveSetup(path string, st models.SetupState) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return writeJSONAtomic(path, st, 0o600)
}

func writeJSONAtomic(path string, v any, perm os.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

// snapshotAndPrune copies src into backupsDir as <prefix>-<stamp>.<ext> (if src
// exists), then auto-trashes old snapshots keeping the most recent backupsKeep.
func snapshotAndPrune(src, backupsDir, prefix, ext string) {
	if backupsDir == "" {
		return
	}
	if _, err := os.Stat(src); err != nil {
		return
	}
	if os.MkdirAll(backupsDir, 0o700) != nil {
		return
	}
	stamp := time.Now().Format("20060102-150405")
	_ = copyFile(src, filepath.Join(backupsDir, prefix+"-"+stamp+"."+ext))
	PruneBackups(backupsDir, prefix, ext, backupsKeep)
}

// PruneBackups keeps only the most recent `keep` snapshots of a given kind
// (timestamped names sort chronologically), deleting older ones (auto-trash).
func PruneBackups(backupsDir, prefix, ext string, keep int) {
	matches, _ := filepath.Glob(filepath.Join(backupsDir, prefix+"-*."+ext))
	if len(matches) <= keep {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-keep] {
		_ = os.Remove(old)
	}
}

// ListBackups returns the snapshot file names in backupsDir (sorted).
func ListBackups(backupsDir string) ([]string, error) {
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// RestoreSnapshot copies backups/<name> back to its target file, inferred from
// the name prefix. Returns the restored target path.
func RestoreSnapshot(configDir, name string) (string, error) {
	src := filepath.Join(configDir, "backups", name)
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("snapshot not found: %s", name)
	}
	cfg, _ := LoadConfigOnly(configDir)
	var target string
	switch {
	case strings.HasPrefix(name, "users-"):
		target = resolveIn(configDir, cfg.UsersFile, "users.json")
	case strings.HasPrefix(name, "services-"):
		target = resolveIn(configDir, cfg.ServicesFile, "services.json")
	case strings.HasPrefix(name, "secrets-"):
		target = resolveIn(configDir, cfg.SecretsFile, "secrets.json")
	case strings.HasPrefix(name, "backends-"):
		target = filepath.Join(configDir, "nginx", "conf.d", "backends.conf")
	default:
		return "", fmt.Errorf("unrecognized snapshot type: %s", name)
	}
	if err := copyFile(src, target); err != nil {
		return "", err
	}
	return target, nil
}

func resolveIn(dir, p, def string) string {
	if p == "" {
		p = def
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}
