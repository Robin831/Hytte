package push

import (
	"testing"
)

func TestGetOrCreateVAPIDKeys(t *testing.T) {
	db := setupTestDB(t)

	keys, err := GetOrCreateVAPIDKeys(db)
	if err != nil {
		t.Fatalf("get or create: %v", err)
	}
	if keys.PublicKey == "" {
		t.Error("public key is empty")
	}
	if keys.PrivateKey == "" {
		t.Error("private key is empty")
	}

	// Second call should return the same keys.
	keys2, err := GetOrCreateVAPIDKeys(db)
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if keys2.PublicKey != keys.PublicKey {
		t.Errorf("public key changed: %q != %q", keys2.PublicKey, keys.PublicKey)
	}
	if keys2.PrivateKey != keys.PrivateKey {
		t.Errorf("private key changed")
	}
}

func TestDecodeVAPIDKeys(t *testing.T) {
	db := setupTestDB(t)

	keys, err := GetOrCreateVAPIDKeys(db)
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	privKey, err := DecodeVAPIDKeys(keys)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if privKey == nil {
		t.Fatal("private key is nil")
	}
	if privKey.PublicKey.Curve == nil {
		t.Error("curve is nil")
	}
}
