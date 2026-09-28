package livestream

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// maxPointsPerPost bounds one GPS upload (the phone batches ~5 s of fixes).
const maxPointsPerPost = 200

// maxTrackPoints caps a session's stored track (~11 h at one fix per second).
const maxTrackPoints = 40000

// TrackPoint is one GPS fix. Time is when the phone took the fix.
type TrackPoint struct {
	ID       int64    `json:"id,omitempty"`
	Time     string   `json:"t"`
	Lat      float64  `json:"lat"`
	Lon      float64  `json:"lon"`
	Alt      *float64 `json:"alt,omitempty"`
	Accuracy float64  `json:"acc,omitempty"`
}

// storedPoint is what gets encrypted: the coordinates only.
type storedPoint struct {
	Lat float64  `json:"lat"`
	Lon float64  `json:"lon"`
	Alt *float64 `json:"alt,omitempty"`
	Acc float64  `json:"acc,omitempty"`
}

func validPoint(p TrackPoint) bool {
	if math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
		return false
	}
	if p.Accuracy < 0 || p.Accuracy > 10000 {
		return false
	}
	if p.Alt != nil && (math.IsNaN(*p.Alt) || math.Abs(*p.Alt) > 20000) {
		return false
	}
	return !parseTime(p.Time).IsZero()
}

// AddTrackPoints stores a batch of GPS fixes for a live session. Invalid
// points are skipped; returns how many were stored.
func AddTrackPoints(db *sql.DB, sessionID int64, points []TrackPoint) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM live_track_points WHERE session_id = ?`, sessionID).Scan(&count); err != nil {
		return 0, err
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	stored := 0
	for _, p := range points {
		if count+stored >= maxTrackPoints {
			break
		}
		if !validPoint(p) {
			continue
		}
		raw, err := json.Marshal(storedPoint{Lat: p.Lat, Lon: p.Lon, Alt: p.Alt, Acc: p.Accuracy})
		if err != nil {
			return 0, err
		}
		enc, err := encryption.EncryptField(string(raw))
		if err != nil {
			return 0, fmt.Errorf("encrypt track point: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO live_track_points (session_id, recorded_at, point) VALUES (?, ?, ?)`,
			sessionID, formatTime(parseTime(p.Time)), enc); err != nil {
			return 0, err
		}
		stored++
	}
	return stored, tx.Commit()
}

// ListTrackPoints returns a session's fixes with id > afterID, oldest first.
func ListTrackPoints(db *sql.DB, sessionID, afterID int64) ([]TrackPoint, error) {
	rows, err := db.Query(`SELECT id, recorded_at, point FROM live_track_points WHERE session_id = ? AND id > ? ORDER BY id`,
		sessionID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackPoint{}
	for rows.Next() {
		var id int64
		var t, enc string
		if err := rows.Scan(&id, &t, &enc); err != nil {
			return nil, err
		}
		raw, err := encryption.DecryptField(enc)
		if err != nil {
			log.Printf("livestream: decrypt track point %d: %v", id, err)
			continue
		}
		var sp storedPoint
		if err := json.Unmarshal([]byte(raw), &sp); err != nil {
			continue
		}
		out = append(out, TrackPoint{ID: id, Time: t, Lat: sp.Lat, Lon: sp.Lon, Alt: sp.Alt, Accuracy: sp.Acc})
	}
	return out, rows.Err()
}

// PurgeTrack deletes a session's GPS track.
func PurgeTrack(db *sql.DB, sessionID int64) error {
	_, err := db.Exec(`DELETE FROM live_track_points WHERE session_id = ?`, sessionID)
	return err
}

// trackPurgeDelay keeps an unrecorded session's track briefly after it ends so
// viewers who are still on the page see the finished route.
const trackPurgeDelay = 30 * time.Minute

// PurgeExpiredTracks removes tracks of ended, unrecorded sessions once
// trackPurgeDelay has passed.
func PurgeExpiredTracks(db *sql.DB, now time.Time) error {
	_, err := db.Exec(`DELETE FROM live_track_points WHERE session_id IN (
		SELECT id FROM live_sessions WHERE status = 'ended' AND record = 0 AND ended_at != '' AND ended_at < ?)`,
		formatTime(now.Add(-trackPurgeDelay)))
	return err
}
