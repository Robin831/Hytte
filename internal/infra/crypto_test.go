package infra

import (
	"testing"
)

// TestEncryptDecryptToken_RoundTrip verifies the infra wrapper delegates correctly
// to the shared encryption package.
func TestEncryptDecryptToken_RoundTrip(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-for-tests-only")
	ResetEncryptionKey()
	t.Cleanup(func() { ResetEncryptionKey() })

	original := "hcloud-test-token-abc123xyz"
	encrypted, err := EncryptToken(original)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if encrypted == original {
		t.Error("encrypted token should differ from plaintext")
	}

	decrypted, err := DecryptToken(encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != original {
		t.Errorf("expected %q, got %q", original, decrypted)
	}
}
