// Package auth generates the opaque bearer tokens issued at register/login
// time. There is no JWT/session machinery here on purpose: a token is just a
// random, unguessable string the server looks up directly against the
// users table, which is simpler to reason about for a service whose only
// clients are scheduler programs (not browsers needing expiry/refresh).
package auth

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateToken returns a new random 256-bit token, hex-encoded.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
