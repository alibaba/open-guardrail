// Package security provides credential primitives, never transport or authorization policy.
package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"golang.org/x/crypto/argon2"
	"strings"
)

// The encoded prefix fixes bounded parameters: Argon2id v19, 19 MiB, t=2, p=1.
// Reject unknown parameter sets rather than accepting unbounded work from storage.
const prefix = "$argon2id$v=19$m=19456,t=2,p=1$"

type Passwords struct{}

func (Passwords) Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	return prefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}
func (Passwords) Verify(encoded, password string) bool {
	if !strings.HasPrefix(encoded, prefix) || len(password) > 128 {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(encoded, prefix), "$")
	if len(parts) != 2 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil || len(salt) != 16 {
		return false
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil || len(hash) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	return subtle.ConstantTimeCompare(hash, got) == 1
}
