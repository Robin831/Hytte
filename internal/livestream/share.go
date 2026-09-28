package livestream

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
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

// UserLink is a user's permanent "My live link".
type UserLink struct {
	UserID int64
	Token  string
}

// CreateUserLink issues (or replaces) the user's personal live link. Replacing
// it invalidates the old URL.
func CreateUserLink(db *sql.DB, userID int64, now time.Time) (*UserLink, error) {
	token, hash, err := newShareToken()
	if err != nil {
		return nil, err
	}
	enc, err := encryption.EncryptField(token)
	if err != nil {
		return nil, fmt.Errorf("encrypt link token: %w", err)
	}
	_, err = db.Exec(`INSERT INTO live_user_links (user_id, token_hash, token, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET token_hash = excluded.token_hash, token = excluded.token, created_at = excluded.created_at`,
		userID, hash, enc, formatTime(now))
	if err != nil {
		return nil, err
	}
	return &UserLink{UserID: userID, Token: token}, nil
}

// GetUserLink returns the user's personal link, or ErrNotFound.
func GetUserLink(db *sql.DB, userID int64) (*UserLink, error) {
	var enc string
	err := db.QueryRow(`SELECT token FROM live_user_links WHERE user_id = ?`, userID).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	token, err := encryption.DecryptField(enc)
	if err != nil {
		return nil, fmt.Errorf("decrypt link token: %w", err)
	}
	return &UserLink{UserID: userID, Token: token}, nil
}

// DeleteUserLink revokes the user's personal link.
func DeleteUserLink(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM live_user_links WHERE user_id = ?`, userID)
	return err
}

// resolveShare maps a share token to what it currently shows. A per-session
// link resolves to that session (live or ended). A personal link resolves to
// the owner's live session, or to nil while they are offline (ownerName is
// then still set so the page can say who it is waiting for).
func resolveShare(db *sql.DB, token string) (s *Session, ownerName string, err error) {
	hash := hashShareToken(token)
	s, err = GetSessionByShareHash(db, hash)
	if err == nil {
		return s, s.OwnerName, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, "", err
	}
	var userID int64
	err = db.QueryRow(`SELECT l.user_id, COALESCE(u.name, '') FROM live_user_links l JOIN users u ON u.id = l.user_id
		WHERE l.token_hash = ?`, hash).Scan(&userID, &ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	live, err := querySessions(db, `WHERE s.user_id = ? AND s.status = 'live' ORDER BY s.started_at DESC, s.id DESC LIMIT 1`, userID)
	if err != nil {
		return nil, "", err
	}
	if len(live) == 0 {
		return nil, ownerName, nil
	}
	return live[0], ownerName, nil
}
