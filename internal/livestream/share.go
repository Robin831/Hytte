package livestream

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
)

// shareTokenPattern is the shape of a share-link token (32 random bytes, hex).
var shareTokenPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// newShareToken returns a fresh share-link token and the SHA-256 hash that is
// stored. Only the hash is persisted, like kiosk tokens and session cookies.
func newShareToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(b)
	return token, hashShareToken(token), nil
}

func hashShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
