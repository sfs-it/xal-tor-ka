package auth

import (
	"crypto/rand"
	"encoding/base32"
)

// NewTOTPSecret generates a random 160-bit Base32 (no padding) TOTP secret,
// suitable for otpauth:// enrollment.
func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}
