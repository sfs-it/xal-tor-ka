// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPruneBackups(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 15; i++ {
		name := filepath.Join(dir, fmt.Sprintf("users-%010d.json", i))
		if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	PruneBackups(dir, "users", "json", 10)
	list, _ := ListBackups(dir)
	if len(list) != 10 {
		t.Fatalf("atteso 10 snapshot dopo prune, ho %d", len(list))
	}
	// devono restare i 10 più recenti (indici 5..14)
	if list[0] != "users-0000000005.json" {
		t.Errorf("prune ha tenuto i file sbagliati: primo=%s", list[0])
	}
}

func TestRestoreSnapshot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	snap := "users-20260101-000000.json"
	want := `{"users":[{"email":"restored@x"}]}`
	if err := os.WriteFile(filepath.Join(backups, snap), []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}

	target, err := RestoreSnapshot(dir, snap)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("contenuto ripristinato errato: %s", got)
	}
	if filepath.Base(target) != "users.json" {
		t.Errorf("target errato: %s", target)
	}
}
