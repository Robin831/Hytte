package livestream

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Recording statuses.
const (
	RecStatusRecording  = "recording"
	RecStatusProcessing = "processing"
	RecStatusReady      = "ready"
	RecStatusFailed     = "failed"
)

// defaultMinFreeBytes is the free space the recordings volume must have
// before a new recording may start (~3 hours of normal-quality video).
const defaultMinFreeBytes = 2 << 30

// flushDelay gives MediaMTX time to close the last segment after the
// publisher is gone and recording is switched off.
const flushDelay = 3 * time.Second

// ErrRecordingUnavailable means recording is not configured on this server.
var ErrRecordingUnavailable = errors.New("recording is not available on this server")

// ErrLowDiskSpace means the recordings volume is too full to start recording.
var ErrLowDiskSpace = errors.New("not enough free disk space to record")

// Recording is a replay of a recorded broadcast.
type Recording struct {
	ID              int64
	SessionID       int64
	UserID          int64
	Status          string
	FileName        string
	SizeBytes       int64
	DurationSeconds float64
	WorkoutID       *int64
	Error           string
	CreatedAt       time.Time
}

// Recorder turns MediaMTX recording on for opted-in broadcasts and turns the
// resulting fMP4 segments into a single seekable MP4 when the broadcast ends.
type Recorder struct {
	db    *sql.DB
	media *MediaServer
	// SegmentsDir is MediaMTX's record root (recordPath without %path/…).
	SegmentsDir string
	// OutDir holds finished replays; served by Hytte.
	OutDir       string
	FFmpeg       string
	FFprobe      string
	MinFreeBytes uint64
	now          func() time.Time
	// flushDelay is overridable in tests.
	flushDelay time.Duration

	// mu serialises remux jobs: one ffmpeg at a time keeps the small VPS responsive.
	mu sync.Mutex
}

// RecorderFromEnv builds a Recorder from LIVE_RECORDING_SEGMENTS_DIR and
// LIVE_RECORDINGS_DIR. Recording is disabled when either is unset or ffmpeg
// is not installed.
func RecorderFromEnv(db *sql.DB, media *MediaServer) *Recorder {
	r := &Recorder{
		db:           db,
		media:        media,
		SegmentsDir:  strings.TrimSpace(os.Getenv("LIVE_RECORDING_SEGMENTS_DIR")),
		OutDir:       strings.TrimSpace(os.Getenv("LIVE_RECORDINGS_DIR")),
		MinFreeBytes: defaultMinFreeBytes,
		now:          time.Now,
		flushDelay:   flushDelay,
	}
	if v, err := strconv.ParseUint(strings.TrimSpace(os.Getenv("LIVE_RECORDING_MIN_FREE_MB")), 10, 64); err == nil && v > 0 {
		r.MinFreeBytes = v << 20
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		r.FFmpeg = p
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		r.FFprobe = p
	}
	return r
}

// Enabled reports whether recordings can be made on this server.
func (r *Recorder) Enabled() bool {
	return r != nil && r.SegmentsDir != "" && r.OutDir != "" && r.FFmpeg != "" && r.media != nil && r.media.cfg.APIURL != ""
}

// DiskUsage returns free bytes on the recordings volume.
func (r *Recorder) DiskUsage() (free uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(r.OutDir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// CanRecord checks configuration and free space before a broadcast that
// wants recording goes live.
func (r *Recorder) CanRecord() error {
	if !r.Enabled() {
		return ErrRecordingUnavailable
	}
	free, err := r.DiskUsage()
	if err != nil {
		return fmt.Errorf("check disk space: %w", err)
	}
	if free < r.MinFreeBytes {
		return ErrLowDiskSpace
	}
	return nil
}

// Start switches recording on for a freshly created session and records the
// replay row. The session is live either way; a failure here only means no
// replay.
func (r *Recorder) Start(ctx context.Context, s *Session) error {
	ts := formatTime(r.now())
	if _, err := r.db.Exec(`INSERT INTO live_recordings (session_id, user_id, status, created_at, updated_at) VALUES (?, ?, 'recording', ?, ?)`,
		s.ID, s.UserID, ts, ts); err != nil {
		return err
	}
	if err := r.media.SetRecording(ctx, s.MediaPath(), true); err != nil {
		r.fail(s.ID, "could not start recording: "+err.Error())
		return err
	}
	return nil
}

func (r *Recorder) setStatus(sessionID int64, status, errMsg string) {
	if _, err := r.db.Exec(`UPDATE live_recordings SET status = ?, error = ?, updated_at = ? WHERE session_id = ?`,
		status, errMsg, formatTime(r.now()), sessionID); err != nil {
		log.Printf("livestream: set recording %d status %s: %v", sessionID, status, err)
	}
}

func (r *Recorder) fail(sessionID int64, msg string) {
	log.Printf("livestream: recording for session %d failed: %s", sessionID, msg)
	r.setStatus(sessionID, RecStatusFailed, msg)
}

// segmentDir is where MediaMTX writes a session's segments.
func (r *Recorder) segmentDir(s *Session) string {
	return filepath.Join(r.SegmentsDir, "live", s.StreamKey)
}

// Finish stops recording for an ended session and builds the replay MP4.
// Safe to call more than once; it only acts on recordings still marked
// recording/processing. Blocks while ffmpeg runs — call it in a goroutine.
func (r *Recorder) Finish(s *Session) {
	if r == nil || !s.Record {
		return
	}
	var status string
	err := r.db.QueryRow(`SELECT status FROM live_recordings WHERE session_id = ?`, s.ID).Scan(&status)
	if err != nil || (status != RecStatusRecording && status != RecStatusProcessing) {
		return
	}
	r.setStatus(s.ID, RecStatusProcessing, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := r.media.SetRecording(ctx, s.MediaPath(), false); err != nil {
		log.Printf("livestream: stop recording session %d: %v", s.ID, err)
	}
	cancel()
	time.Sleep(r.flushDelay)

	r.mu.Lock()
	defer r.mu.Unlock()

	segDir := r.segmentDir(s)
	segments, _ := filepath.Glob(filepath.Join(segDir, "*.mp4"))
	sort.Strings(segments) // names are timestamps, so this is chronological
	if len(segments) == 0 {
		r.fail(s.ID, "no video was recorded")
		return
	}

	name, size, dur, err := r.remux(s, segments)
	if err != nil {
		r.fail(s.ID, err.Error())
		return
	}
	if _, err := r.db.Exec(`UPDATE live_recordings SET status = 'ready', error = '', file_name = ?, size_bytes = ?, duration_seconds = ?, updated_at = ?
		WHERE session_id = ?`, name, size, dur, formatTime(r.now()), s.ID); err != nil {
		log.Printf("livestream: save recording %d: %v", s.ID, err)
		return
	}
	// Best effort: MediaMTX creates the per-stream folder without group write
	// permission, so on the server this usually fails and its own
	// recordDeleteAfter removes the segments a day later instead.
	if err := os.RemoveAll(segDir); err != nil && !errors.Is(err, os.ErrPermission) {
		log.Printf("livestream: remove segments for session %d: %v", s.ID, err)
	}
	if _, err := LinkWorkout(r.db, s.ID); err != nil {
		log.Printf("livestream: link workout for session %d: %v", s.ID, err)
	}
	log.Printf("livestream: replay for session %d ready (%s, %.0fs, %d MB)", s.ID, name, dur, size>>20)
}

// remux concatenates the fMP4 segments into one MP4 with the moov atom up
// front (instant seeking over HTTP). Video is copied; Opus audio is converted
// to AAC because Safari/iOS cannot play Opus in MP4.
func (r *Recorder) remux(s *Session, segments []string) (name string, size int64, dur float64, err error) {
	if err := os.MkdirAll(r.OutDir, 0o750); err != nil {
		return "", 0, 0, err
	}
	list, err := os.CreateTemp(r.OutDir, "concat-*.txt")
	if err != nil {
		return "", 0, 0, err
	}
	defer os.Remove(list.Name())
	for _, seg := range segments {
		fmt.Fprintf(list, "file '%s'\n", strings.ReplaceAll(seg, "'", `'\''`))
	}
	list.Close()

	suffix := make([]byte, 6)
	rand.Read(suffix) //nolint:errcheck
	name = fmt.Sprintf("session-%d-%s.mp4", s.ID, hex.EncodeToString(suffix))
	out := filepath.Join(r.OutDir, name)
	tmp := out + ".part"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "concat", "-safe", "0", "-i", list.Name(),
		"-map", "0:v:0?", "-map", "0:a:0?",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart", "-f", "mp4", "-y", tmp)
	if outb, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		msg := strings.TrimSpace(string(outb))
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		return "", 0, 0, fmt.Errorf("ffmpeg: %v: %s", err, msg)
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", 0, 0, err
	}
	info, err := os.Stat(out)
	if err != nil {
		return "", 0, 0, err
	}
	return name, info.Size(), r.probeDuration(out), nil
}

func (r *Recorder) probeDuration(file string) float64 {
	if r.FFprobe == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, r.FFprobe, "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", file).Output()
	if err != nil {
		return 0
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return d
}

// ResumePending finishes recordings interrupted by a Hytte restart: their
// session has ended but the replay was never built.
func (r *Recorder) ResumePending() {
	if !r.Enabled() {
		return
	}
	rows, err := r.db.Query(`SELECT r.session_id FROM live_recordings r JOIN live_sessions s ON s.id = r.session_id
		WHERE r.status IN ('recording', 'processing') AND s.status = 'ended'`)
	if err != nil {
		log.Printf("livestream: find pending recordings: %v", err)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s, err := GetSession(r.db, id)
		if err != nil {
			continue
		}
		r.Finish(s)
	}
}

// FilePath returns the on-disk path of a ready recording, refusing anything
// that would escape OutDir.
func (r *Recorder) FilePath(rec *Recording) (string, error) {
	if rec.FileName == "" || rec.FileName != filepath.Base(rec.FileName) {
		return "", ErrNotFound
	}
	return filepath.Join(r.OutDir, rec.FileName), nil
}

// DeleteRecording removes a replay file, its row, and the GPS track kept for it.
func (r *Recorder) DeleteRecording(rec *Recording) error {
	if rec.FileName != "" && r != nil && r.OutDir != "" {
		if p, err := r.FilePath(rec); err == nil {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	if _, err := r.db.Exec(`DELETE FROM live_track_points WHERE session_id = ?`, rec.SessionID); err != nil {
		return err
	}
	_, err := r.db.Exec(`DELETE FROM live_recordings WHERE id = ?`, rec.ID)
	return err
}

// --- queries -----------------------------------------------------------------

const recordingColumns = `r.id, r.session_id, r.user_id, r.status, r.file_name, r.size_bytes, r.duration_seconds, r.workout_id, r.error, r.created_at`

func scanRecording(sc interface{ Scan(...any) error }) (*Recording, error) {
	var rec Recording
	var workout sql.NullInt64
	var created string
	if err := sc.Scan(&rec.ID, &rec.SessionID, &rec.UserID, &rec.Status, &rec.FileName, &rec.SizeBytes,
		&rec.DurationSeconds, &workout, &rec.Error, &created); err != nil {
		return nil, err
	}
	if workout.Valid {
		w := workout.Int64
		rec.WorkoutID = &w
	}
	rec.CreatedAt = parseTime(created)
	return &rec, nil
}

// GetRecording loads a recording by id.
func GetRecording(db *sql.DB, id int64) (*Recording, error) {
	rec, err := scanRecording(db.QueryRow(`SELECT `+recordingColumns+` FROM live_recordings r WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

// GetRecordingBySession loads the recording for a session, if any.
func GetRecordingBySession(db *sql.DB, sessionID int64) (*Recording, error) {
	rec, err := scanRecording(db.QueryRow(`SELECT `+recordingColumns+` FROM live_recordings r WHERE r.session_id = ?`, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

// ListRecordings returns every recording, newest first.
func ListRecordings(db *sql.DB) ([]*Recording, error) {
	rows, err := db.Query(`SELECT ` + recordingColumns + ` FROM live_recordings r ORDER BY r.created_at DESC, r.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Recording
	for rows.Next() {
		rec, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// linkSlack widens the broadcast window when matching workouts: the watch is
// often started a little before "Go live" or stopped a little after "Stop".
const linkSlack = 15 * time.Minute

// LinkWorkout attaches the owner's workout that overlaps the broadcast most
// to a ready recording that has none yet. Returns the workout id (0 if none).
// Called when the replay is built and again lazily when it is viewed, since
// the watch usually syncs after the run.
func LinkWorkout(db *sql.DB, sessionID int64) (int64, error) {
	rec, err := GetRecordingBySession(db, sessionID)
	if err != nil {
		return 0, err
	}
	if rec.WorkoutID != nil {
		return *rec.WorkoutID, nil
	}
	s, err := GetSession(db, sessionID)
	if err != nil {
		return 0, err
	}
	end := s.EndedAt
	if end.IsZero() {
		return 0, nil
	}
	from := s.StartedAt.Add(-linkSlack)
	to := end.Add(linkSlack)

	rows, err := db.Query(`SELECT id, started_at, duration_seconds FROM workouts
		WHERE user_id = ? AND started_at >= ? AND started_at <= ?`,
		s.UserID, formatTime(from.Add(-12*time.Hour)), formatTime(to))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var best int64
	var bestOverlap time.Duration
	for rows.Next() {
		var id int64
		var started string
		var durSec int64
		if err := rows.Scan(&id, &started, &durSec); err != nil {
			return 0, err
		}
		ws := parseTime(started)
		if ws.IsZero() {
			continue
		}
		we := ws.Add(time.Duration(durSec) * time.Second)
		overlap := minTime(we, to).Sub(maxTime(ws, from))
		if overlap > bestOverlap {
			best, bestOverlap = id, overlap
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if best == 0 {
		return 0, nil
	}
	_, err = db.Exec(`UPDATE live_recordings SET workout_id = ? WHERE id = ? AND workout_id IS NULL`, best, rec.ID)
	return best, err
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
