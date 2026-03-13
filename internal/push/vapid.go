package push

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math/big"
)

// VAPIDKeys holds the ECDSA P-256 key pair used for VAPID authentication.
type VAPIDKeys struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"-"`
}

// GetOrCreateVAPIDKeys retrieves existing VAPID keys from the database or
// generates and stores a new key pair. The keys are stored as base64url-encoded
// raw bytes (no padding). Uses INSERT OR IGNORE to avoid race conditions when
// multiple goroutines attempt to create keys concurrently.
func GetOrCreateVAPIDKeys(db *sql.DB) (*VAPIDKeys, error) {
	keys := &VAPIDKeys{}
	err := db.QueryRow("SELECT public_key, private_key FROM vapid_keys WHERE id = 1").
		Scan(&keys.PublicKey, &keys.PrivateKey)
	if err == nil {
		return keys, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("query vapid keys: %w", err)
	}

	// Generate new ECDH P-256 key pair.
	curve := ecdh.P256()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	pubKey := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes())
	privKey := base64.RawURLEncoding.EncodeToString(privateKey.Bytes())

	// INSERT OR IGNORE: if another goroutine inserted first, this is a no-op.
	_, err = db.Exec("INSERT OR IGNORE INTO vapid_keys (id, public_key, private_key) VALUES (1, ?, ?)",
		pubKey, privKey)
	if err != nil {
		return nil, fmt.Errorf("store vapid keys: %w", err)
	}

	// Always re-read to get the winning row (ours or the other goroutine's).
	err = db.QueryRow("SELECT public_key, private_key FROM vapid_keys WHERE id = 1").
		Scan(&keys.PublicKey, &keys.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("re-read vapid keys: %w", err)
	}

	return keys, nil
}

// GetVAPIDPublicKey returns only the public VAPID key, without loading the
// private key from the database. Falls back to GetOrCreateVAPIDKeys if no
// keys exist yet.
func GetVAPIDPublicKey(db *sql.DB) (string, error) {
	var publicKey string
	err := db.QueryRow("SELECT public_key FROM vapid_keys WHERE id = 1").Scan(&publicKey)
	if err == sql.ErrNoRows {
		// Keys don't exist yet — create them.
		keys, createErr := GetOrCreateVAPIDKeys(db)
		if createErr != nil {
			return "", createErr
		}
		return keys.PublicKey, nil
	}
	if err != nil {
		return "", fmt.Errorf("query vapid public key: %w", err)
	}
	return publicKey, nil
}

// DecodeVAPIDKeys decodes base64url-encoded VAPID keys into an ECDSA private key
// suitable for JWT signing.
func DecodeVAPIDKeys(keys *VAPIDKeys) (*ecdsa.PrivateKey, error) {
	privBytes, err := base64.RawURLEncoding.DecodeString(keys.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	pubBytes, err := base64.RawURLEncoding.DecodeString(keys.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}

	// Parse via crypto/ecdh to get validated key, then convert to ecdsa for JWT signing.
	curve := ecdh.P256()
	ecdhPriv, err := curve.NewPrivateKey(privBytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	ecdhPub, err := curve.NewPublicKey(pubBytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	// Convert to ecdsa.PrivateKey for JWT signing compatibility.
	// The uncompressed public key bytes are: 0x04 || X (32 bytes) || Y (32 bytes)
	pubRaw := ecdhPub.Bytes()
	if len(pubRaw) != 65 || pubRaw[0] != 0x04 {
		return nil, fmt.Errorf("unexpected public key format")
	}

	x := new(big.Int).SetBytes(pubRaw[1:33])
	y := new(big.Int).SetBytes(pubRaw[33:65])
	d := new(big.Int).SetBytes(ecdhPriv.Bytes())

	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     x,
			Y:     y,
		},
		D: d,
	}, nil
}
