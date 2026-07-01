// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistentStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	s1 := NewPersistentStore(time.Hour, time.Hour, path)
	sess, err := s1.Create("u@x", "local")
	if err != nil {
		t.Fatal(err)
	}
	s1.Complete2FA(sess.ID)

	// new store from the same path → must reload the session
	s2 := NewPersistentStore(time.Hour, time.Hour, path)
	got, ok := s2.Get(sess.ID)
	if !ok || got.Email != "u@x" || !got.TwoFADone {
		t.Errorf("session not persisted/reloaded: %+v ok=%v", got, ok)
	}
	// after Delete + reload it must no longer be there
	s2.Delete(sess.ID)
	s3 := NewPersistentStore(time.Hour, time.Hour, path)
	if _, ok := s3.Get(sess.ID); ok {
		t.Error("deleted session still present after reload")
	}
}

func TestPasswordRoundtrip(t *testing.T) {
	h, err := HashPassword("secret-pw")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword(h, "secret-pw"); err != nil {
		t.Errorf("correct password rejected: %v", err)
	}
	if err := VerifyPassword(h, "wrong"); err == nil {
		t.Error("wrong password accepted")
	}
}

func TestVerifyTOTP(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Now()
	code := refTOTP(t, secret, now)
	if !VerifyTOTP(secret, code, now) {
		t.Error("valid TOTP code rejected")
	}
	bad := "999999"
	if code == bad {
		bad = "111111"
	}
	if VerifyTOTP(secret, bad, now) {
		t.Error("wrong TOTP code accepted")
	}
}

func TestNewTOTPSecret(t *testing.T) {
	s, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s); err != nil {
		t.Errorf("secret not decodable as base32: %v", err)
	}
}

// refTOTP computes a reference RFC 6238 code independently from the package code.
func refTOTP(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		t.Fatal(err)
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(at.Unix())/30)
	m := hmac.New(sha1.New, key)
	m.Write(buf[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (uint32(sum[off]&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3]))
	return fmt.Sprintf("%06d", v%1000000)
}
