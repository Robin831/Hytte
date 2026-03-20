package encryption

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
	"runtime"
	"strings"
	"sync"
)

var (
	encryptionKey      []byte
	encryptionKeyErr   error
	encryptionKeyReady bool
	encryptionKeyMu    sync.Mutex
)

// keyFilePath is the path to the auto-generated key file, overridable for tests.
// Empty string means use defaultKeyFilePath().
var keyFilePath = ""

// defaultKeyFilePath returns the path for the auto-generated encryption key,
// stored in the user config directory. Returns an error if the config directory
// cannot be determined or created, rather than falling back to the working
// directory which may be world-readable or unpredictable.
func defaultKeyFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determine user config directory: %w", err)
	}
	dir := filepath.Join(configDir, "hytte")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create config directory %s: %w", dir, err)
	}
	return filepath.Join(dir, ".encryption_key"), nil
}

// getEncryptionKey returns the 32-byte AES-256 key derived from:
//  1. The ENCRYPTION_KEY environment variable (if set), or
//  2. An auto-generated key file stored in the user config directory
//     (os.UserConfigDir()/hytte/.encryption_key).
//
// When auto-generating, a cryptographically random 32-byte key is written
// to disk so it persists across restarts. The ENCRYPTION_KEY env var takes
// precedence and is recommended for production deployments.
func getEncryptionKey() ([]byte, error) {
	encryptionKeyMu.Lock()
	defer encryptionKeyMu.Unlock()
	if encryptionKeyReady {
		return encryptionKey, encryptionKeyErr
	}
	encryptionKeyReady = true

	raw := os.Getenv("ENCRYPTION_KEY")
	if raw != "" {
		h := sha256.Sum256([]byte(raw))
		encryptionKey = h[:]
		return encryptionKey, nil
	}

	// Auto-generate a persistent key file.
	kf := keyFilePath
	if kf == "" {
		var kfErr error
		kf, kfErr = defaultKeyFilePath()
		if kfErr != nil {
			encryptionKeyErr = fmt.Errorf("encryption key file path: %w", kfErr)
			return nil, encryptionKeyErr
		}
	}
	// Reject symlinks before any file operations: a malicious symlink in the
	// config directory could cause chmod/read/write to affect an unintended
	// target. Use os.Lstat (does not follow symlinks) to inspect the entry.
	if linfo, lstatErr := os.Lstat(kf); lstatErr == nil {
		mode := linfo.Mode()
		if mode&os.ModeSymlink != 0 {
			encryptionKeyErr = fmt.Errorf("key file %s is a symlink; refusing to use it for security reasons", kf)
			return nil, encryptionKeyErr
		}
		if !mode.IsRegular() {
			encryptionKeyErr = fmt.Errorf("key path %s exists but is not a regular file; refusing to use it as an encryption key", kf)
			return nil, encryptionKeyErr
		}
		// File exists and is a regular file; check permissions without
		// following any symlinks (skip on Windows where Unix bits are not
		// meaningful).
		if runtime.GOOS != "windows" {
			perm := mode.Perm()
			if perm&0077 != 0 {
				log.Printf("Warning: key file %s has permissions %04o (expected 0600), tightening", kf, perm)
				if chmodErr := os.Chmod(kf, 0600); chmodErr != nil {
					encryptionKeyErr = fmt.Errorf("key file %s has insecure permissions %04o and chmod failed: %w", kf, perm, chmodErr)
					return nil, encryptionKeyErr
				}
			}
		}
	}

	data, err := os.ReadFile(kf)
	if err == nil {
		// Trim only trailing newlines/carriage returns that editors or tools
		// may append. Do not use TrimSpace — accepting arbitrary whitespace
		// could mask corruption or tampering.
		content := strings.TrimRight(string(data), "\r\n")
		if len(content) == 64 { // 32 bytes hex-encoded
			decoded, decErr := hex.DecodeString(content)
			if decErr == nil && len(decoded) == 32 {
				encryptionKey = decoded
				return encryptionKey, nil
			}
			log.Printf("Warning: key file %s contains invalid hex data, regenerating", kf)
		} else {
			log.Printf("Warning: key file %s has unexpected length %d (expected 64), regenerating", kf, len(content))
		}
	}

	// Generate a new random key.
	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		encryptionKeyErr = fmt.Errorf("generate encryption key: %w", err)
		return nil, encryptionKeyErr
	}
	if err := os.WriteFile(kf, []byte(hex.EncodeToString(newKey)), 0600); err != nil {
		encryptionKeyErr = fmt.Errorf("save encryption key to %s: %w", kf, err)
		return nil, encryptionKeyErr
	}
	log.Printf("Auto-generated encryption key at %s (set ENCRYPTION_KEY env var to override)", kf)
	encryptionKey = newKey
	return encryptionKey, nil
}

// ResetEncryptionKey resets the encryption key singleton so it will be
// re-derived on the next call to getEncryptionKey. This is intended for
// tests that need to set different ENCRYPTION_KEY env vars.
func ResetEncryptionKey() {
	encryptionKeyMu.Lock()
	defer encryptionKeyMu.Unlock()
	encryptionKey = nil
	encryptionKeyErr = nil
	encryptionKeyReady = false
}

// Encrypt encrypts plaintext using AES-256-GCM and returns a
// base64-encoded ciphertext string suitable for database storage.
func Encrypt(plaintext string) (string, error) {
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

// ciphertextPrefix is prepended by EncryptField to all encrypted values.
// DecryptField uses its presence to unambiguously identify ciphertext, avoiding
// false positives on legacy plaintext that happens to be valid base64.
const ciphertextPrefix = "enc:"

// EncryptField encrypts a string field for database storage.
// Empty strings are returned as-is to distinguish "no data" from "encrypted data".
// The returned ciphertext is prefixed with ciphertextPrefix so DecryptField can
// reliably distinguish it from legacy unencrypted data.
func EncryptField(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	encrypted, err := Encrypt(value)
	if err != nil {
		return "", err
	}
	return ciphertextPrefix + encrypted, nil
}

// DecryptField decrypts a database field back to plaintext.
// Empty strings are returned as-is. Values prefixed with ciphertextPrefix are
// decrypted; any other value is treated as legacy unencrypted data and returned
// unchanged, so reads of pre-encryption rows remain transparent regardless of
// whether the legacy value happens to be valid base64.
func DecryptField(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, ciphertextPrefix) {
		// No prefix — legacy plaintext, return as-is.
		return value, nil
	}
	result, err := Decrypt(value[len(ciphertextPrefix):])
	if err != nil {
		return "", fmt.Errorf("decrypt field: %w", err)
	}
	return result, nil
}

// Decrypt decrypts a base64-encoded AES-256-GCM ciphertext back to
// the original plaintext.
func Decrypt(encoded string) (string, error) {
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
