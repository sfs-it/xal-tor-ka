// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package vpnmgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultConfig is the inert F1 default: disabled, WireGuard hub, standard subnets.
// A missing vpn.json degrades to this, so an installed-but-unconfigured module does
// nothing (the outward-safe default).
func DefaultConfig() Config {
	return Config{
		Enabled:    false,
		Driver:     "wireguard",
		Role:       "hub",
		ListenPort: 51820,
		VPNSubnet:  "10.8.0.0/24",
		DockerNet:  "172.31.7.0/24",
	}
}

// Store loads and persists vpn.json. The extension is the SINGLE writer of that file,
// so a plain in-process mutex serializes writes. Save writes via a same-dir temp +
// rename: this is atomic AND keeps the file reachable from a container that bind-mounts
// the CONTAINING DIRECTORY (mounting the single file would go stale on the inode swap —
// the same reason the agent socket is mounted by its dir, not the file).
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore returns a Store bound to the given vpn.json path.
func NewStore(path string) *Store { return &Store{path: path} }

// Path is the vpn.json path this store manages.
func (s *Store) Path() string { return s.path }

// Load reads vpn.json. A missing file yields the inert DefaultConfig (F1: nothing to do).
// The module owns this file, so decoding is lenient (unknown/future keys are ignored)
// for forward-compatibility across phases.
func (s *Store) Load() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read vpn.json: %w", err)
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse vpn.json: %w", err)
	}
	return cfg, nil
}

// Save persists cfg to vpn.json atomically (same-dir temp + rename), inode-stable for a
// dir bind-mount. Used from F3+ (matrix edits); inert in F1 (no mutating handler calls it).
func (s *Store) Save(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode vpn.json: %w", err)
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".vpn-*.json.tmp")
	if err != nil {
		return fmt.Errorf("temp vpn.json: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write vpn.json: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close vpn.json: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("rename vpn.json: %w", err)
	}
	return nil
}
