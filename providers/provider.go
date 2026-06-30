// Package providers defines the authentication provider interface and its
// implementations (Local now; Google/Microsoft OIDC later). See BLUEPRINT.md §6.
package providers

import "errors"

// ErrInvalidCredentials is returned for any failed authentication. It is
// deliberately generic to avoid leaking whether the account exists.
var ErrInvalidCredentials = errors.New("invalid credentials")

// Identity is the verified result of an authentication.
type Identity struct {
	Email    string
	Provider string
}

// Provider is the common interface implemented by all auth providers.
type Provider interface {
	ID() string
	Type() string // "oidc" | "local"
}
