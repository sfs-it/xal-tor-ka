package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// totpStep is the TOTP time step (RFC 6238, 30s).
const totpStep = 30

// VerifyTOTP checks a 6-digit TOTP code against a Base32 secret at time t,
// tolerating ±1 step of clock skew. Comparison is constant-time.
func VerifyTOTP(secret, code string, t time.Time) bool {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 {
		return false
	}
	code = strings.TrimSpace(code)
	counter := uint64(t.Unix()) / totpStep
	for _, d := range []int64{-1, 0, 1} {
		if equalConst(hotp(key, uint64(int64(counter)+d)), code) {
			return true
		}
	}
	return false
}

// hotp computes the 6-digit HOTP value for key and counter (RFC 4226).
func hotp(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (uint32(sum[off]&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3]))
	return fmt.Sprintf("%06d", v%1000000)
}

func equalConst(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
