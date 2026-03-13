package push

import (
	"sync"
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

func TestGetOrCreateVAPIDKeys_Concurrent(t *testing.T) {
	db := setupTestDB(t)

	const goroutines = 10
	results := make([]*VAPIDKeys, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range results {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = GetOrCreateVAPIDKeys(db)
		}(i)
	}
	wg.Wait()

	// All goroutines should succeed and return the same keys.
	var firstKeys *VAPIDKeys
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("goroutine %d failed: %v", i, errs[i])
		}
		if firstKeys == nil {
			firstKeys = results[i]
		} else {
			if results[i].PublicKey != firstKeys.PublicKey {
				t.Errorf("goroutine %d got different public key", i)
			}
			if results[i].PrivateKey != firstKeys.PrivateKey {
				t.Errorf("goroutine %d got different private key", i)
			}
		}
	}
}

func TestGetVAPIDPublicKey(t *testing.T) {
	db := setupTestDB(t)

	// Should create keys on first call.
	pubKey, err := GetVAPIDPublicKey(db)
	if err != nil {
		t.Fatalf("get public key: %v", err)
	}
	if pubKey == "" {
		t.Error("public key is empty")
	}

	// Should return the same key on subsequent calls.
	pubKey2, err := GetVAPIDPublicKey(db)
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if pubKey2 != pubKey {
		t.Errorf("public key changed: %q != %q", pubKey2, pubKey)
	}

	// Should match what GetOrCreateVAPIDKeys returns.
	keys, err := GetOrCreateVAPIDKeys(db)
	if err != nil {
		t.Fatalf("get full keys: %v", err)
	}
	if pubKey != keys.PublicKey {
		t.Errorf("public key mismatch: %q != %q", pubKey, keys.PublicKey)
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
