package providers

import (
	"xaltorka/auth"
)

// Local authenticates users against the in-RAM user directory (argon2id
// password hashes). It implements Provider.
type Local struct {
	dir *auth.UserDirectory
}

// NewLocal builds the local provider over the shared user directory.
func NewLocal(dir *auth.UserDirectory) *Local {
	return &Local{dir: dir}
}

// ID implements Provider.
func (l *Local) ID() string { return "local" }

// Type implements Provider.
func (l *Local) Type() string { return "local" }

// Authenticate verifies email+password. The returned error is always generic
// (ErrInvalidCredentials) to avoid user enumeration.
func (l *Local) Authenticate(email, password string) (Identity, error) {
	u, ok := l.dir.Get(email)
	if !ok || u.Provider != "local" || u.PasswordHash == "" {
		return Identity{}, ErrInvalidCredentials
	}
	if err := auth.VerifyPassword(u.PasswordHash, password); err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	return Identity{Email: email, Provider: "local"}, nil
}
