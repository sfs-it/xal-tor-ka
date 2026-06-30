// Package health periodically probes the HTTP health endpoint of each backend
// and records its status for the admin Monitoring view. Not ICMP: it does an
// application-level GET (BLUEPRINT.md §11). The checker reads the current backend
// set on every tick, so it follows reloads automatically.
package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"xaltorka/models"
)

// State is the observed health state of a backend.
type State string

const (
	StateUnknown     State = "unknown"
	StateUp          State = "up"
	StateDown        State = "down"        // reachable but non-2xx
	StateUnreachable State = "unreachable" // network error / timeout
)

// Status is the last known health of a backend.
type Status struct {
	BackendID string
	Host      string
	URL       string
	State     State
	LastError string
	LastCheck time.Time
}

// Alerter is notified on state transitions (nil disables alerting).
type Alerter interface {
	Notify(cur Status, prev State)
}

// Checker probes backends on a base tick, honoring each backend's interval.
type Checker struct {
	backends func() []models.Backend // current backend set (follows reloads)
	client   *http.Client
	alerter  Alerter
	baseTick time.Duration

	mu     sync.RWMutex
	status map[string]Status // by backend ID
}

// New builds a checker. backends returns the current backend set; alerter may be nil.
func New(backends func() []models.Backend, alerter Alerter) *Checker {
	return &Checker{
		backends: backends,
		client:   &http.Client{},
		alerter:  alerter,
		baseTick: 5 * time.Second,
		status:   map[string]Status{},
	}
}

// Start runs the probe loop until ctx is cancelled (graceful shutdown).
func (c *Checker) Start(ctx context.Context) {
	t := time.NewTicker(c.baseTick)
	defer t.Stop()
	c.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.tick(ctx)
		}
	}
}

func (c *Checker) tick(ctx context.Context) {
	now := time.Now()
	for _, be := range c.backends() {
		if be.Health.URL == "" {
			continue
		}
		interval := time.Duration(be.Health.IntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 30 * time.Second
		}
		c.mu.RLock()
		prev, seen := c.status[be.ID]
		c.mu.RUnlock()
		if seen && now.Sub(prev.LastCheck) < interval {
			continue
		}
		c.probe(ctx, be, prev.State)
	}
}

func (c *Checker) probe(ctx context.Context, be models.Backend, prevState State) {
	timeout := time.Duration(be.Health.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	st := Status{BackendID: be.ID, Host: be.Host, URL: be.Health.URL, LastCheck: time.Now()}
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, be.Health.URL, nil)
	if err != nil {
		st.State, st.LastError = StateUnreachable, err.Error()
	} else if resp, derr := c.client.Do(req); derr != nil {
		st.State, st.LastError = StateUnreachable, derr.Error()
	} else {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			st.State = StateUp
		} else {
			st.State, st.LastError = StateDown, fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
	}

	c.mu.Lock()
	c.status[be.ID] = st
	c.mu.Unlock()

	if c.alerter != nil && prevState != "" && prevState != StateUnknown && prevState != st.State {
		c.alerter.Notify(st, prevState)
	}
}

// Snapshot returns the current statuses (admin Monitoring view).
func (c *Checker) Snapshot() []Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Status, 0, len(c.status))
	for _, s := range c.status {
		out = append(out, s)
	}
	return out
}
