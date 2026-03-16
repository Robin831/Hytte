package infra

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	encryptionKey     []byte
	encryptionKeyOnce sync.Once
)

// getEncryptionKey returns the 32-byte AES-256 key derived from the
// ENCRYPTION_KEY environment variable. If unset, a deterministic fallback
// key is used (better than plaintext, but operators should set the env var).
func getEncryptionKey() []byte {
	encryptionKeyOnce.Do(func() {
		raw := os.Getenv("ENCRYPTION_KEY")
		if raw == "" {
			// Deterministic fallback — not ideal but still prevents casual
			// plaintext exposure in the database file.
			raw = "hytte-default-encryption-key-change-me"
		}
		h := sha256.Sum256([]byte(raw))
		encryptionKey = h[:]
	})
	return encryptionKey
}

// EncryptToken encrypts plaintext using AES-256-GCM and returns a
// base64-encoded ciphertext string suitable for database storage.
func EncryptToken(plaintext string) (string, error) {
	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptToken decrypts a base64-encoded AES-256-GCM ciphertext back to
// the original plaintext token.
func DecryptToken(encoded string) (string, error) {
	key := getEncryptionKey()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
