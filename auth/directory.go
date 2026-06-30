package auth

import (
	"sync"

	"xaltorka/models"
)

// UserDirectory is the authoritative in-RAM user cache (BLUEPRINT §8.1),
// guarded by an RWMutex. It is shared by the validation path and the local
// provider, and can be hot-reloaded after admin/setup changes.
type UserDirectory struct {
	mu      sync.RWMutex
	byEmail map[string]models.User
}

// NewUserDirectory builds a directory from a user slice.
func NewUserDirectory(users []models.User) *UserDirectory {
	d := &UserDirectory{byEmail: map[string]models.User{}}
	d.Replace(users)
	return d
}

// Get returns the user for an email, if present.
func (d *UserDirectory) Get(email string) (models.User, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	u, ok := d.byEmail[email]
	return u, ok
}

// Replace atomically swaps the whole directory contents.
func (d *UserDirectory) Replace(users []models.User) {
	m := make(map[string]models.User, len(users))
	for _, u := range users {
		m[u.Email] = u
	}
	d.mu.Lock()
	d.byEmail = m
	d.mu.Unlock()
}

// All returns a snapshot copy of all users.
func (d *UserDirectory) All() []models.User {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]models.User, 0, len(d.byEmail))
	for _, u := range d.byEmail {
		out = append(out, u)
	}
	return out
}

// Count returns the number of users.
func (d *UserDirectory) Count() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.byEmail)
}
