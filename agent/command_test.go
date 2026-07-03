// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndValidate(t *testing.T) {
	cmds, err := LoadCommands("commands")
	if err != nil {
		t.Fatalf("LoadCommands: %v", err)
	}
	lt := cmds["logtail"]
	if lt == nil {
		t.Fatal("logtail command not loaded")
	}
	cases := []struct {
		name    string
		params  map[string]string
		wantErr bool
	}{
		{"unknown param", map[string]string{"foo": "bar"}, true},
		{"missing required", map[string]string{}, true},
		{"enum mismatch", map[string]string{"log": "passwd"}, true},
		{"pattern injection", map[string]string{"log": "syslog", "lines": "1; rm -rf /"}, true},
		{"valid", map[string]string{"log": "syslog", "lines": "50"}, false},
		{"valid no optional", map[string]string{"log": "nginx-error"}, false},
	}
	for _, c := range cases {
		err := lt.validate(c.params)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

// TestRunPassesEnvNotShell is the core security property: a hostile parameter
// value reaches the script only as a literal environment variable, never
// interpreted by a shell — so there is no injection.
func TestRunPassesEnvNotShell(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "echo.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\nprintf 'got:%s' \"$XTK_P_VAL\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Command{Name: "echo", Script: script, Manifest: Manifest{
		TimeoutSeconds: 5, Params: map[string]ParamSpec{"val": {Required: true}},
	}}
	res, err := c.run(context.Background(), map[string]string{"val": "; rm -rf / #"}, uint32(os.Getuid()))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Code != 0 {
		t.Fatalf("exit %d, stderr=%q", res.Code, res.Stderr)
	}
	if res.Stdout != "got:; rm -rf / #" {
		t.Errorf("value not passed literally as env: %q", res.Stdout)
	}
}

func TestTrustRefusesWritable(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "x.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\ntrue\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Chmod bypasses umask, so we actually get the group/world-writable bits.
	if err := os.Chmod(script, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := checkScriptTrust(script, uint32(os.Getuid())); err == nil {
		t.Error("group/world-writable script must be refused")
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkScriptTrust(script, uint32(os.Getuid())); err != nil {
		t.Errorf("own, non-writable script should be trusted: %v", err)
	}
}
