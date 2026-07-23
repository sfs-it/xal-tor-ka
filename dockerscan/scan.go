// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package dockerscan

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// MaxScanPorts bounds a single host scan (intentional, user-driven scans only).
// A wider request is CLAMPED, never silently truncated: the caller must report the
// effective range, otherwise the page claims to have scanned ports it never touched
// and the user concludes their service is missing when it was simply never probed.
const MaxScanPorts = 2000

// ClampRange returns the range that will actually be scanned, plus whether the
// request had to be narrowed.
func ClampRange(from, to int) (int, int, bool) {
	if from < 1 {
		from = 1
	}
	if to > 65535 {
		to = 65535
	}
	if to < from {
		return from, to, false
	}
	if to-from+1 > MaxScanPorts {
		return from, from + MaxScanPorts - 1, true
	}
	return from, to, false
}

// ScanPorts TCP-connect-scans host on [from,to] and returns the open ports.
// The range is clamped to [1,65535] and capped at maxScanPorts. Used to discover
// services listening on the host (e.g. PuTTY/SSH tunnels) via host.docker.internal.
func ScanPorts(ctx context.Context, host string, from, to int) []int {
	from, to, _ = ClampRange(from, to)
	if to < from {
		return nil
	}

	sem := make(chan struct{}, 100)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		open []int
	)
	d := net.Dialer{Timeout: 300 * time.Millisecond}
	for p := from; p <= to; p++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(port int) {
			defer wg.Done()
			defer func() { <-sem }()
			conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
			if err != nil {
				return
			}
			_ = conn.Close()
			mu.Lock()
			open = append(open, port)
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	sort.Ints(open)
	return open
}
