package dockerscan

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// maxScanPorts bounds a single host scan (intentional, user-driven scans only).
const maxScanPorts = 2000

// ScanPorts TCP-connect-scans host on [from,to] and returns the open ports.
// The range is clamped to [1,65535] and capped at maxScanPorts. Used to discover
// services listening on the host (e.g. PuTTY/SSH tunnels) via host.docker.internal.
func ScanPorts(ctx context.Context, host string, from, to int) []int {
	if from < 1 {
		from = 1
	}
	if to > 65535 {
		to = 65535
	}
	if to < from {
		return nil
	}
	if to-from+1 > maxScanPorts {
		to = from + maxScanPorts - 1
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
