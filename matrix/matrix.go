// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package matrix resolves (host, path) requests to backend access rules and
// checks user→backend authorization. See BLUEPRINT.md §5. The backend set is
// swappable at runtime (config backends + services.json) under an RWMutex.
package matrix

import (
	"strings"
	"sync"

	"xaltorka/models"
)

// Resolver evaluates the authorization matrix over a swappable backend set.
type Resolver struct {
	mu       sync.RWMutex
	backends []models.Backend
}

// NewResolver builds a resolver over the config's backends.
func NewResolver(c *models.Config) *Resolver {
	r := &Resolver{}
	r.Set(c.Backends)
	return r
}

// Set atomically replaces the backend set (e.g. on reload after merging
// config backends with services.json).
func (r *Resolver) Set(backends []models.Backend) {
	cp := make([]models.Backend, len(backends))
	copy(cp, backends)
	r.mu.Lock()
	r.backends = cp
	r.mu.Unlock()
}

// Backends returns a snapshot of the current backend set.
func (r *Resolver) Backends() []models.Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make([]models.Backend, len(r.backends))
	copy(cp, r.backends)
	return cp
}

// Resolve matches host exactly, then selects the route with the longest path
// prefix of path (a "/" route is the catch-all). ok=false when no backend host
// matches or the matched backend has no applicable route (default-deny).
func (r *Resolver) Resolve(host, path string) (models.Backend, models.Route, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, be := range r.backends {
		if be.Host != host {
			continue
		}
		best := -1
		var bestRoute models.Route
		for _, rt := range be.Routes {
			if pathMatches(path, rt.Path) && len(rt.Path) > best {
				best = len(rt.Path)
				bestRoute = rt
			}
		}
		if best >= 0 {
			return be, bestRoute, true
		}
		return be, models.Route{}, false
	}
	return models.Backend{}, models.Route{}, false
}

// pathMatches reports whether reqPath falls under routePath on a path-segment
// boundary, so "/api" matches "/api" and "/api/x" but NOT "/apixyz". "/" (or "")
// is the catch-all.
func pathMatches(reqPath, routePath string) bool {
	if routePath == "" || routePath == "/" {
		return true
	}
	rp := strings.TrimRight(routePath, "/")
	return reqPath == rp || strings.HasPrefix(reqPath, rp+"/")
}

// Authorized reports whether the user is whitelisted for the backend id.
func (r *Resolver) Authorized(u models.User, backendID string) bool {
	for _, b := range u.Backends {
		if b == backendID {
			return true
		}
	}
	return false
}
