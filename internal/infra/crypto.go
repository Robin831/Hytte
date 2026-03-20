package infra

import "github.com/Robin831/Hytte/internal/encryption"

// EncryptToken encrypts plaintext using AES-256-GCM via the shared encryption package.
// Delegates to encryption.Encrypt.
func EncryptToken(plaintext string) (string, error) {
	return encryption.Encrypt(plaintext)
}

// DecryptToken decrypts a base64-encoded AES-256-GCM ciphertext via the shared encryption package.
// Delegates to encryption.Decrypt.
func DecryptToken(encoded string) (string, error) {
	return encryption.Decrypt(encoded)
}

// ResetEncryptionKey resets the encryption key singleton so it will be
// re-derived on the next call. Delegates to encryption.ResetEncryptionKey.
func ResetEncryptionKey() {
	encryption.ResetEncryptionKey()
}
