// Package auth provides session storage, password hashing and TOTP verification
// for Xal-Tor-Ka. The session store is the authoritative in-RAM cache described
// in BLUEPRINT.md §8.1; an optional file persists sessions across restarts
// (write-behind: only on create/destroy, never on the request hot path).
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"xaltorka/models"
)

// SessionStore is the abstraction over session persistence.
type SessionStore interface {
	Create(email, provider string) (models.Session, error)
	Get(id string) (models.Session, bool)
	Complete2FA(id string) bool
	Delete(id string)
}

// MemoryStore keeps sessions in a mutex-guarded map. Reads on the hot path never
// touch disk. ttl is the absolute lifetime, idle the inactivity timeout; a zero
// duration disables that check. When path != "", sessions are persisted to disk
// on create/complete/delete (not on every read) and reloaded at startup.
type MemoryStore struct {
	mu   sync.RWMutex
	m    map[string]*models.Session
	ttl  time.Duration
	idle time.Duration
	path string
}

// NewMemoryStore builds a pure in-memory session store (no persistence).
func NewMemoryStore(ttl, idle time.Duration) *MemoryStore {
	return &MemoryStore{m: make(map[string]*models.Session), ttl: ttl, idle: idle}
}

// NewPersistentStore builds a session store backed by a JSON file (loaded now,
// written write-behind on mutations).
func NewPersistentStore(ttl, idle time.Duration, path string) *MemoryStore {
	s := &MemoryStore{m: make(map[string]*models.Session), ttl: ttl, idle: idle, path: path}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	s.load()
	return s
}

// Create allocates a new pre-2FA session with a random 256-bit opaque id.
func (s *MemoryStore) Create(email, provider string) (models.Session, error) {
	id, err := newID()
	if err != nil {
		return models.Session{}, err
	}
	now := time.Now()
	sess := &models.Session{ID: id, Email: email, Provider: provider, CreatedAt: now, LastSeen: now}
	s.mu.Lock()
	s.m[id] = sess
	s.persistLocked()
	s.mu.Unlock()
	return *sess, nil
}

// Get returns a copy of the session if present and not expired, refreshing
// LastSeen. Expired sessions are evicted. LastSeen is NOT persisted (hot path).
func (s *MemoryStore) Get(id string) (models.Session, bool) {
	if id == "" {
		return models.Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.m[id]
	if !ok {
		return models.Session{}, false
	}
	now := time.Now()
	if (s.ttl > 0 && now.Sub(sess.CreatedAt) > s.ttl) || (s.idle > 0 && now.Sub(sess.LastSeen) > s.idle) {
		delete(s.m, id)
		s.persistLocked()
		return models.Session{}, false
	}
	sess.LastSeen = now
	return *sess, true
}

// Complete2FA marks the session as having passed two-factor verification.
func (s *MemoryStore) Complete2FA(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.m[id]; ok {
		sess.TwoFADone = true
		s.persistLocked()
		return true
	}
	return false
}

// Delete removes a session (logout).
func (s *MemoryStore) Delete(id string) {
	s.mu.Lock()
	delete(s.m, id)
	s.persistLocked()
	s.mu.Unlock()
}

// persistLocked writes the session map to disk atomically. Caller holds s.mu.
func (s *MemoryStore) persistLocked() {
	if s.path == "" {
		return
	}
	b, err := json.Marshal(s.m)
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

func (s *MemoryStore) load() {
	if s.path == "" {
		return
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	m := map[string]*models.Session{}
	if json.Unmarshal(b, &m) == nil {
		s.m = m
	}
}

// newID returns a URL-safe random 256-bit identifier.
func newID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
