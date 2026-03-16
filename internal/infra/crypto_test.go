package infra

import (
	"testing"
)

func resetCryptoForTest(t *testing.T) {
	t.Helper()
	ResetEncryptionKey()
	t.Cleanup(func() { ResetEncryptionKey() })
}

func TestEncryptDecryptToken_RoundTrip(t *testing.T) {
	resetCryptoForTest(t)
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

func TestEncryptToken_DifferentCiphertexts(t *testing.T) {
	resetCryptoForTest(t)
	token := "same-token"
	enc1, err := EncryptToken(token)
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := EncryptToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if enc1 == enc2 {
		t.Error("encrypting the same token twice should produce different ciphertexts (random nonce)")
	}
}

func TestDecryptToken_InvalidData(t *testing.T) {
	resetCryptoForTest(t)
	_, err := DecryptToken("not-valid-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}

	_, err = DecryptToken("aGVsbG8=") // valid base64 but not valid ciphertext
	if err == nil {
		t.Error("expected error for invalid ciphertext")
	}
}
