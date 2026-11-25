package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/oekit/pkg/stringx"
)

// HashToken is how a store keeps only a digest of a bearer token (SEC-05:
// the token itself never lands on disk). Two packages carried copies with
// different failure stories; this is the one.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// RandomHex returns n random bytes as hex, sourced from stringx.RandomToken.
// Callers decide what a failure means (the phone store panics — a token
// without entropy cannot be safe — while the MCP store reports it).
func RandomHex(n int) (string, error) {
	token, err := stringx.RandomToken(n, stringx.TokenHex)
	if err != nil {
		return "", fmt.Errorf("token entropy: %w", err)
	}
	return token, nil
}
