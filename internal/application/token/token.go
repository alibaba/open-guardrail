// Package token provides random opaque identifiers for persisted resources.
package token

import "crypto/rand"

// Two independent random strings provide at least 256 bits of token entropy.
func RandomToken(prefix string) string { return prefix + rand.Text() + rand.Text() }
