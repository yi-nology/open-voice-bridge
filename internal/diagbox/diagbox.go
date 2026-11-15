// Package diagbox seals the diagnostics pack for export (R10): the pack is
// encrypted locally with a passphrase the exporter picks, so a shared
// attachment never carries plaintext logs. Format "OVBDIA1":
//
//	0-7   magic "OVBDIA1\n"
//	8-23  PBKDF2 salt (16 bytes)
//	24-35 AES-GCM nonce (12 bytes)
//	36-39 PBKDF2 iterations, big-endian uint32
//	40-   AES-256-GCM ciphertext (tag appended)
//
// Key = PBKDF2-HMAC-SHA256(passphrase, salt, iterations, 32). The web UI
// seals with WebCrypto and this package opens the result byte-for-byte.
package diagbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

const (
	magic      = "OVBDIA1\n"
	headerLen  = 40
	saltLen    = 16
	nonceLen   = 12
	keyLen     = 32
	iterations = 600_000
)

// ErrWrongPassphrase reports an authentication failure — a wrong passphrase
// or a tampered ciphertext, which GCM cannot tell apart.
var ErrWrongPassphrase = errors.New("wrong passphrase or corrupted diagnostics pack")

// Seal encrypts plaintext under passphrase at the default work factor.
func Seal(plaintext []byte, passphrase string) ([]byte, error) {
	return seal(plaintext, passphrase, iterations)
}

func seal(plaintext []byte, passphrase string, iters int) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("empty passphrase")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("salt: %w", err)
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	out := make([]byte, headerLen, headerLen+len(plaintext)+16)
	copy(out, magic)
	copy(out[8:], salt)
	copy(out[24:], nonce)
	binary.BigEndian.PutUint32(out[36:], uint32(iters))

	key := pbkdf2.Key([]byte(passphrase), salt, iters, keyLen, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Seal(out, nonce, plaintext, out[:headerLen]), nil
}

// Open reverses Seal. The iteration count travels in the header so packs
// stay readable after the work factor is raised.
func Open(pack []byte, passphrase string) ([]byte, error) {
	if len(pack) < headerLen+16 || string(pack[:8]) != magic {
		return nil, errors.New("not an OVBDIA1 diagnostics pack")
	}
	salt := pack[8:24]
	nonce := pack[24:36]
	iters := binary.BigEndian.Uint32(pack[36:40])
	if iters == 0 || iters > 10_000_000 {
		return nil, errors.New("implausible PBKDF2 iteration count")
	}
	key := pbkdf2.Key([]byte(passphrase), salt, int(iters), keyLen, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, pack[headerLen:], pack[:headerLen])
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	return plaintext, nil
}
