// Package livestream lets a user broadcast live video from a phone browser
// (WebRTC WHIP) and lets other users with the "livestream" feature watch it
// (WebRTC WHEP, with an HLS fallback). Media is handled by a MediaMTX server
// bound to localhost; Hytte owns auth and reverse-proxies the signalling and
// HLS traffic to it. See docs/livestream.md for the server setup.
package livestream

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Session statuses.
const (
	StatusLive  = "live"
	StatusEnded = "ended"
)

// maxTitleLen bounds the free-text broadcast title (in runes).
const maxTitleLen = 120

// ErrNotFound is returned when a session id does not exist.
var ErrNotFound = errors.New("live session not found")

// Session is one broadcast. StreamKey is server-only: it names the MediaMTX
// path and must never be serialised to clients.
type Session struct {
	ID         int64
	UserID     int64
	OwnerName  string
	StreamKey  string
	Title      string
	Status     string
	StartedAt  time.Time
	EndedAt    time.Time
	LastSeenAt time.Time

	// Options chosen on the Go live screen.
	Notify   bool
	Record   bool
	Location bool

	// ShareTokenHash is the SHA-256 of the public share-link token, or "".
	ShareTokenHash string

	// Broadcaster status from the latest heartbeat. BatteryLevel is nil when
	// the phone's browser does not expose the Battery Status API.
	BatteryLevel    *float64
	BatteryCharging bool
	PublisherState  string
}

// Options are the per-broadcast choices made when going live.
type Options struct {
	Notify   bool
	Record   bool
	Location bool
}

// MediaPath is the MediaMTX path name for the session.
func (s *Session) MediaPath() string {
	return "live/" + s.StreamKey
}

// newStreamKey returns 128 random bits as 32 hex chars — the shape the
// MediaMTX path regex (~^live/[0-9a-f]{32}$) accepts.
func newStreamKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// CreateSession starts a new live session for userID. Any session the user
// still has marked live is ended first, so a user never has two broadcasts
// running. The ended sessions are returned so the caller can kick their
// MediaMTX connections.
func CreateSession(db *sql.DB, userID int64, title string, opts Options, now time.Time) (*Session, []*Session, error) {
	key, err := newStreamKey()
	if err != nil {
		return nil, nil, fmt.Errorf("generate stream key: %w", err)
	}
	encTitle, err := encryption.EncryptField(title)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt title: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	previous, err := querySessions(tx, `WHERE s.user_id = ? AND s.status = 'live'`, userID)
	if err != nil {
		return nil, nil, err
	}
	ts := formatTime(now)
	if _, err := tx.Exec(`UPDATE live_sessions SET status = 'ended', ended_at = ? WHERE user_id = ? AND status = 'live'`, ts, userID); err != nil {
		return nil, nil, err
	}
	res, err := tx.Exec(`INSERT INTO live_sessions (user_id, stream_key, title, status, started_at, last_seen_at, notify, record, location)
		VALUES (?, ?, ?, 'live', ?, ?, ?, ?, ?)`,
		userID, key, encTitle, ts, ts, opts.Notify, opts.Record, opts.Location)
	if err != nil {
		return nil, nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	s, err := GetSession(db, id)
	if err != nil {
		return nil, nil, err
	}
	return s, previous, nil
}

// GetSession loads one session by id.
func GetSession(db *sql.DB, id int64) (*Session, error) {
	list, err := querySessions(db, `WHERE s.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// ListLive returns every session currently marked live, newest first.
func ListLive(db *sql.DB) ([]*Session, error) {
	return querySessions(db, `WHERE s.status = 'live' ORDER BY s.started_at DESC, s.id DESC`)
}

// EndSession marks a session ended. Ending an already-ended session is a no-op.
func EndSession(db *sql.DB, id int64, now time.Time) error {
	_, err := db.Exec(`UPDATE live_sessions SET status = 'ended', ended_at = ? WHERE id = ? AND status = 'live'`, formatTime(now), id)
	return err
}

// Heartbeat is the broadcaster status reported with each keep-alive.
type Heartbeat struct {
	BatteryLevel    *float64 `json:"battery_level"`
	BatteryCharging bool     `json:"battery_charging"`
	State           string   `json:"state"`
}

// validPublisherStates are the broadcaster connection states viewers are told
// about ("reconnecting" drives the "back in a moment" overlay).
var validPublisherStates = map[string]bool{"": true, "connecting": true, "live": true, "reconnecting": true}

// Touch records a publisher heartbeat (and its status) on a live session.
func Touch(db *sql.DB, id int64, hb Heartbeat, now time.Time) error {
	if !validPublisherStates[hb.State] {
		hb.State = ""
	}
	var battery any
	if hb.BatteryLevel != nil && *hb.BatteryLevel >= 0 && *hb.BatteryLevel <= 1 {
		battery = *hb.BatteryLevel
	}
	_, err := db.Exec(`UPDATE live_sessions SET last_seen_at = ?, battery_level = ?, battery_charging = ?, publisher_state = ?
		WHERE id = ? AND status = 'live'`, formatTime(now), battery, hb.BatteryCharging, hb.State, id)
	return err
}

// SetShareTokenHash stores (or clears, with "") a session's share-link hash.
func SetShareTokenHash(db *sql.DB, id int64, hash string) error {
	_, err := db.Exec(`UPDATE live_sessions SET share_token_hash = ? WHERE id = ?`, hash, id)
	return err
}

// GetSessionByShareHash finds the live session a share link points at.
func GetSessionByShareHash(db *sql.DB, hash string) (*Session, error) {
	if hash == "" {
		return nil, ErrNotFound
	}
	list, err := querySessions(db, `WHERE s.share_token_hash = ?`, hash)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// EndStale ends every live session whose last heartbeat is older than cutoff
// and returns them so the caller can kick their MediaMTX connections.
func EndStale(db *sql.DB, cutoff, now time.Time) ([]*Session, error) {
	stale, err := querySessions(db, `WHERE s.status = 'live' AND s.last_seen_at < ?`, formatTime(cutoff))
	if err != nil {
		return nil, err
	}
	for _, s := range stale {
		if err := EndSession(db, s.ID, now); err != nil {
			return nil, err
		}
		s.Status = StatusEnded
		s.EndedAt = now
	}
	return stale, nil
}

type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func querySessions(q queryer, where string, args ...any) ([]*Session, error) {
	rows, err := q.Query(`
		SELECT s.id, s.user_id, COALESCE(u.name, ''), s.stream_key, s.title, s.status,
		       s.started_at, s.ended_at, s.last_seen_at, s.notify, s.record, s.location,
		       s.share_token_hash, s.battery_level, s.battery_charging, s.publisher_state
		FROM live_sessions s
		LEFT JOIN users u ON u.id = s.user_id
		`+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Session
	for rows.Next() {
		var s Session
		var title, started, ended, seen string
		var battery sql.NullFloat64
		if err := rows.Scan(&s.ID, &s.UserID, &s.OwnerName, &s.StreamKey, &title, &s.Status, &started, &ended, &seen,
			&s.Notify, &s.Record, &s.Location, &s.ShareTokenHash, &battery, &s.BatteryCharging, &s.PublisherState); err != nil {
			return nil, err
		}
		s.Title, err = encryption.DecryptField(title)
		if err != nil {
			log.Printf("livestream: decrypt title for session %d: %v", s.ID, err)
			s.Title = ""
		}
		s.StartedAt = parseTime(started)
		s.EndedAt = parseTime(ended)
		s.LastSeenAt = parseTime(seen)
		if battery.Valid {
			b := battery.Float64
			s.BatteryLevel = &b
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}
