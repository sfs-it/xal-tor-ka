// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package audit writes a fail2ban-friendly log of authentication failures
// (one line per event, with the real client IP), so the host's fail2ban can
// monitor it and ban brute-forcers. Decoupled: the app only writes the file.
package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger appends auth-failure lines to a file. A nil *Logger is a safe no-op.
type Logger struct {
	mu sync.Mutex
	f  *os.File
}

// New opens (creating dirs as needed) the auth log at path in append mode.
// An empty path disables logging (returns nil, nil).
func New(path string) (*Logger, error) {
	if path == "" {
		return nil, nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Logger{f: f}, nil
}

// Fail records an authentication failure. Format (stable, for fail2ban):
//
//	2026-06-24T10:00:00Z xaltorka auth-fail ip=<IP> event=<event> <detail>
func (l *Logger) Fail(ip, event, detail string) {
	if l == nil || l.f == nil {
		return
	}
	if ip == "" {
		ip = "-"
	}
	line := fmt.Sprintf("%s xaltorka auth-fail ip=%s event=%s %s\n",
		time.Now().UTC().Format(time.RFC3339), ip, event, detail)
	l.mu.Lock()
	_, _ = l.f.WriteString(line)
	l.mu.Unlock()
}

// Close closes the underlying file.
func (l *Logger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}
