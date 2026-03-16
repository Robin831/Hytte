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
	encryptionKeyErr  error
	encryptionKeyOnce sync.Once
	encryptionKeyMu   sync.Mutex
)

// getEncryptionKey returns the 32-byte AES-256 key derived from the
// ENCRYPTION_KEY environment variable. Returns an error if ENCRYPTION_KEY is
// not set so that token operations fail closed rather than falling back to a
// shared default key that would allow offline decryption of any leaked DB.
func getEncryptionKey() ([]byte, error) {
	encryptionKeyMu.Lock()
	defer encryptionKeyMu.Unlock()
	encryptionKeyOnce.Do(func() {
		raw := os.Getenv("ENCRYPTION_KEY")
		if raw == "" {
			encryptionKeyErr = errors.New("ENCRYPTION_KEY environment variable is not set; configure it to protect stored tokens")
			return
		}
		h := sha256.Sum256([]byte(raw))
		encryptionKey = h[:]
	})
	return encryptionKey, encryptionKeyErr
}

// ResetEncryptionKey resets the encryption key singleton so it will be
// re-derived on the next call to getEncryptionKey. This is intended for
// tests that need to set different ENCRYPTION_KEY env vars.
func ResetEncryptionKey() {
	encryptionKeyMu.Lock()
	defer encryptionKeyMu.Unlock()
	encryptionKey = nil
	encryptionKeyErr = nil
	encryptionKeyOnce = sync.Once{}
}

// EncryptToken encrypts plaintext using AES-256-GCM and returns a
// base64-encoded ciphertext string suitable for database storage.
func EncryptToken(plaintext string) (string, error) {
	key, err := getEncryptionKey()
	if err != nil {
		return "", err
	}
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
	key, err := getEncryptionKey()
	if err != nil {
		return "", err
	}
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
