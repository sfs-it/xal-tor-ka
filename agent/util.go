// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package agent

import (
	"os"
	"syscall"
)

// fileUID returns the owning uid of a stat result (Linux). ok=false if the
// platform-specific info is unavailable.
func fileUID(fi os.FileInfo) (uint32, bool) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Uid, true
	}
	return 0, false
}

// maxCapture bounds how much command output the agent buffers/returns, so a
// runaway command cannot exhaust memory.
const maxCapture = 256 * 1024

// limitedBuffer is a byte sink that keeps at most maxCapture bytes.
type limitedBuffer struct {
	b []byte
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxCapture - len(l.b); room > 0 {
		if len(p) > room {
			l.b = append(l.b, p[:room]...)
		} else {
			l.b = append(l.b, p...)
		}
	}
	return len(p), nil // report full length so the command isn't blocked on a full buffer
}

func (l *limitedBuffer) String() string { return string(l.b) }
