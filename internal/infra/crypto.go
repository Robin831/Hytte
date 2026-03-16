package infra

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	encryptionKey     []byte
	encryptionKeyErr  error
	encryptionKeyOnce sync.Once
	encryptionKeyMu   sync.Mutex
)

// keyFilePath is the path to the auto-generated key file, overridable for tests.
var keyFilePath = ""

// defaultKeyFilePath returns the path for the auto-generated encryption key,
// stored next to the application executable (or working directory as fallback).
func defaultKeyFilePath() string {
	if keyFilePath != "" {
		return keyFilePath
	}
	exe, err := os.Executable()
	if err != nil {
		return ".encryption_key"
	}
	return filepath.Join(filepath.Dir(exe), ".encryption_key")
}

// getEncryptionKey returns the 32-byte AES-256 key derived from:
//  1. The ENCRYPTION_KEY environment variable (if set), or
//  2. An auto-generated key file stored alongside the executable.
//
// When auto-generating, a cryptographically random 32-byte key is written
// to disk so it persists across restarts. The ENCRYPTION_KEY env var takes
// precedence and is recommended for production deployments.
func getEncryptionKey() ([]byte, error) {
	encryptionKeyMu.Lock()
	defer encryptionKeyMu.Unlock()
	encryptionKeyOnce.Do(func() {
		raw := os.Getenv("ENCRYPTION_KEY")
		if raw != "" {
			h := sha256.Sum256([]byte(raw))
			encryptionKey = h[:]
			return
		}

		// Auto-generate a persistent key file.
		kf := defaultKeyFilePath()
		data, err := os.ReadFile(kf)
		if err == nil && len(data) == 64 { // 32 bytes hex-encoded
			decoded, decErr := hex.DecodeString(string(data))
			if decErr == nil && len(decoded) == 32 {
				encryptionKey = decoded
				return
			}
		}

		// Generate a new random key.
		newKey := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
			encryptionKeyErr = fmt.Errorf("generate encryption key: %w", err)
			return
		}
		if err := os.WriteFile(kf, []byte(hex.EncodeToString(newKey)), 0600); err != nil {
			encryptionKeyErr = fmt.Errorf("save encryption key to %s: %w", kf, err)
			return
		}
		log.Printf("Auto-generated encryption key at %s (set ENCRYPTION_KEY env var to override)", kf)
		encryptionKey = newKey
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
