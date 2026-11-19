package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// HashToken is how a store keeps only a digest of a bearer token (SEC-05:
// the token itself never lands on disk). Two packages carried copies with
// different failure stories; this is the one.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// RandomHex returns n random bytes as hex. Callers decide what a failure
// means (the phone store panics — a token without entropy cannot be safe —
// while the MCP store reports it).
func RandomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("token entropy: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
