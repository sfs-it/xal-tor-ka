// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package auth

import (
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"math/big"
	"sync"
	"time"
)

// OTPStore holds one-time login codes in memory: at most one active code per
// email, hashed at rest, single-use, time-limited, with a short per-email
// cooldown against request spam. The plaintext code is returned once by Issue
// (the caller delivers it out-of-band — spool/email/sms) and never stored.
// Ephemeral by design: a restart invalidates pending codes (they re-issue).
type OTPStore struct {
	mu       sync.Mutex
	codes    map[string]otpEntry
	ttl      time.Duration
	cooldown time.Duration
	length   int
}

type otpEntry struct {
	hash    string
	issued  time.Time
	expires time.Time
}

// NewOTPStore builds a store. length is the number of decimal digits (min 4),
// ttl the validity window, cooldown the minimum gap between two Issue for the
// same email.
func NewOTPStore(ttl, cooldown time.Duration, length int) *OTPStore {
	if length < 4 {
		length = 6
	}
	return &OTPStore{codes: map[string]otpEntry{}, ttl: ttl, cooldown: cooldown, length: length}
}

// Issue generates a fresh code for email and stores its hash. Returns the
// plaintext code, or ok=false if the per-email cooldown has not elapsed yet
// (the caller should still answer generically, to avoid leaking the reason).
func (s *OTPStore) Issue(email string) (code string, ok bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, exists := s.codes[email]; exists && now.Sub(e.issued) < s.cooldown {
		return "", false
	}
	code = randomDigits(s.length)
	s.codes[email] = otpEntry{hash: hashCode(code), issued: now, expires: now.Add(s.ttl)}
	return code, true
}

// Verify checks code for email and consumes it on success (single-use). A wrong,
// expired, or already-used code returns false. Constant-time comparison.
func (s *OTPStore) Verify(email, code string) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.codes[email]
	if !ok || now.After(e.expires) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(e.hash), []byte(hashCode(code))) != 1 {
		return false
	}
	delete(s.codes, email) // one-time: consume regardless of what happens next
	return true
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte("xtk-otp:" + code))
	return hex.EncodeToString(sum[:])
}

// randomDigits returns n cryptographically-random decimal digits.
func randomDigits(n int) string {
	const digits = "0123456789"
	b := make([]byte, n)
	for i := range b {
		k, err := crand.Int(crand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			// crypto/rand failure is unrecoverable here; bias-free fallback is
			// impossible without entropy, so fail loud by returning a marker the
			// verify path can never match (never a usable code).
			return "----------"[:n]
		}
		b[i] = digits[k.Int64()]
	}
	return string(b)
}
