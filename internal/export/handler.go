// Package export implements the self-service data export: a single
// authenticated endpoint that streams a JSON archive of the signed-in user's
// own data so they can take it with them (for example before deleting their
// account).
//
// The response is written incrementally — rows are queried and encoded
// per-domain straight onto the ResponseWriter — so a large account does not
// have to be materialised in memory before the first byte is sent.
package export

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

// SchemaVersion identifies the layout of the exported document. Bump it
// whenever the shape of the payload changes in a way consumers must notice.
const SchemaVersion = 1

// redactedPreferenceKeys lists preference keys whose values are secrets and are
// therefore omitted from the export entirely. This mirrors what
// auth.PreferencesGetHandler already redacts or masks in the settings API.
var redactedPreferenceKeys = map[string]bool{
	"claude_cli_path":        true,
	"wordfeud_email":         true,
	"wordfeud_password":      true,
	"wordfeud_session_token": true,
}

// encryptedPreferenceKeys lists preference keys stored encrypted at rest that
// should be exported as plaintext.
var encryptedPreferenceKeys = map[string]bool{
	"stride_custom_prompt": true,
}

// domain is one exported section of the archive: a JSON key plus the function
// that streams its rows. New domains can be appended to allDomains without
// touching the envelope logic.
type domain struct {
	key   string
	write func(w io.Writer, enc *json.Encoder, db *sql.DB, userID int64) error
}

// allDomains is the ordered list of sections written into the archive.
var allDomains = []domain{
	{"notes", writeNotes},
	{"workouts", writeWorkouts},
	{"lactate_tests", writeLactateTests},
	{"lactate_stages", writeLactateStages},
	{"preferences", writePreferences},
	{"sessions", writeSessions},
}

// profile holds the exported account fields for the signed-in user.
type profile struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Picture   string `json:"picture"`
	CreatedAt string `json:"created_at"`
	IsAdmin   bool   `json:"is_admin"`
}

// domainError records a section that could not be exported. Errors are
// collected and reported in a trailing array rather than aborting the
// response, because the status line and headers are already committed by the
// time the first domain is written.
type domainError struct {
	Domain string `json:"domain"`
	Error  string `json:"error"`
}

// ExportHandler streams a JSON archive of the authenticated user's own data.
// Every query is scoped by user_id; encrypted fields are decrypted before
// serialization and session tokens are never included.
func ExportHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			if err := json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"}); err != nil {
				log.Printf("export: failed to write unauthorized response: %v", err)
			}
			return
		}

		now := time.Now()
		filename := fmt.Sprintf("hytte-export-%s.json", now.Format("2006-01-02"))

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)

		enc := json.NewEncoder(w)
		flush := func() {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}

		p := loadProfile(db, user)

		writeRaw(w, "{")
		writeKey(w, "schema_version")
		writeRaw(w, fmt.Sprintf("%d,", SchemaVersion))
		writeKey(w, "exported_at")
		encodeValue(enc, now.UTC().Format(time.RFC3339))
		writeRaw(w, ",")
		writeKey(w, "profile")
		encodeValue(enc, p)
		flush()

		var failures []domainError
		for _, d := range allDomains {
			writeRaw(w, ",")
			if err := d.write(w, enc, db, user.ID); err != nil {
				// streamArray always closes the array it opened, so the
				// document stays well-formed and we only record the failure
				// here — the headers are long since committed.
				log.Printf("export: failed to export %s for user %d: %v", d.key, user.ID, err)
				failures = append(failures, domainError{Domain: d.key, Error: "this section could not be exported"})
			}
			flush()
		}

		writeRaw(w, ",")
		writeKey(w, "errors")
		if failures == nil {
			failures = []domainError{}
		}
		encodeValue(enc, failures)
		writeRaw(w, "}\n")
		flush()
	}
}

// loadProfile reads the authoritative account row, falling back to the user
// attached to the request context when the lookup fails. google_id is
// deliberately not exported — it is an identifier for the identity provider,
// not user data.
func loadProfile(db *sql.DB, user *auth.User) profile {
	p := profile{
		ID:        user.ID,
		Email:     user.Email,
		Name:      user.Name,
		Picture:   user.Picture,
		CreatedAt: user.CreatedAt,
		IsAdmin:   user.IsAdmin,
	}
	var fresh profile
	var createdAt any
	err := db.QueryRow(
		`SELECT id, email, name, picture, created_at, is_admin FROM users WHERE id = ?`, user.ID,
	).Scan(&fresh.ID, &fresh.Email, &fresh.Name, &fresh.Picture, &createdAt, &fresh.IsAdmin)
	if err != nil {
		log.Printf("export: failed to load profile for user %d, using session copy: %v", user.ID, err)
		return p
	}
	fresh.CreatedAt = formatTimestamp(createdAt)
	return fresh
}

// formatTimestamp normalises a scanned DATETIME column to RFC3339. The SQLite
// driver hands back either a time.Time or the raw text depending on how the
// value was written, so both are handled.
func formatTimestamp(v any) string {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	case string:
		return t
	case []byte:
		return string(t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

// writeRaw writes a literal JSON fragment, logging (but not propagating) write
// errors since the response is already committed.
func writeRaw(w io.Writer, s string) {
	if _, err := io.WriteString(w, s); err != nil {
		log.Printf("export: write failed: %v", err)
	}
}

// writeKey writes a quoted object key followed by its colon.
func writeKey(w io.Writer, key string) {
	writeRaw(w, `"`+key+`":`)
}

// encodeValue encodes a single JSON value. json.Encoder appends a newline,
// which is insignificant whitespace between JSON tokens.
func encodeValue(enc *json.Encoder, v any) {
	if err := enc.Encode(v); err != nil {
		log.Printf("export: encode failed: %v", err)
	}
}

// streamArray writes `"key":[...]` by scanning rows one at a time and encoding
// each straight onto the writer. The array is always closed, even when a scan
// fails partway through, so the surrounding document stays well-formed.
func streamArray(w io.Writer, enc *json.Encoder, key string, rows *sql.Rows, scan func(*sql.Rows) (any, error)) error {
	defer rows.Close()

	writeKey(w, key)
	writeRaw(w, "[")
	defer writeRaw(w, "]")

	first := true
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return err
		}
		if !first {
			writeRaw(w, ",")
		}
		first = false
		encodeValue(enc, v)
	}
	return rows.Err()
}

// emptyArray writes `"key":[]`, used when a domain query fails before any rows
// could be read so the envelope still contains the key.
func emptyArray(w io.Writer, key string) {
	writeKey(w, key)
	writeRaw(w, "[]")
}

// decryptOrKeep decrypts an encrypted field, falling back to the stored value
// and logging a warning when decryption fails. A single corrupt row must never
// abort the whole export.
func decryptOrKeep(domainKey, field string, id int64, value string) string {
	plain, err := encryption.DecryptField(value)
	if err != nil {
		log.Printf("Warning: export: failed to decrypt %s.%s for row %d, exporting stored value: %v", domainKey, field, id, err)
		return value
	}
	return plain
}

// splitTags turns a GROUP_CONCAT result into a sorted tag slice.
func splitTags(raw sql.NullString) []string {
	if !raw.Valid || raw.String == "" {
		return []string{}
	}
	tags := strings.Split(raw.String, "\x1f")
	sort.Strings(tags)
	return tags
}

type exportedNote struct {
	ID        int64    `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func writeNotes(w io.Writer, enc *json.Encoder, db *sql.DB, userID int64) error {
	// char(31) is the ASCII unit separator used as the tag delimiter elsewhere
	// in the codebase (see notes.List).
	rows, err := db.Query(`
		SELECT n.id, n.title, n.content, n.created_at, n.updated_at,
		       GROUP_CONCAT(nt.tag, char(31)) AS tags
		FROM notes n
		LEFT JOIN note_tags nt ON nt.note_id = n.id
		WHERE n.user_id = ?
		GROUP BY n.id
		ORDER BY n.id`, userID)
	if err != nil {
		emptyArray(w, "notes")
		return err
	}
	return streamArray(w, enc, "notes", rows, func(rows *sql.Rows) (any, error) {
		var n exportedNote
		var tags sql.NullString
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.CreatedAt, &n.UpdatedAt, &tags); err != nil {
			return nil, err
		}
		n.Title = decryptOrKeep("notes", "title", n.ID, n.Title)
		n.Content = decryptOrKeep("notes", "content", n.ID, n.Content)
		n.Tags = splitTags(tags)
		return n, nil
	})
}

type exportedWorkout struct {
	ID              int64    `json:"id"`
	Sport           string   `json:"sport"`
	SubSport        string   `json:"sub_sport"`
	IsIndoor        bool     `json:"is_indoor"`
	Title           string   `json:"title"`
	TitleSource     string   `json:"title_source"`
	StartedAt       string   `json:"started_at"`
	DurationSeconds int64    `json:"duration_seconds"`
	DistanceMeters  float64  `json:"distance_meters"`
	AvgHeartRate    int      `json:"avg_heart_rate"`
	MaxHeartRate    int      `json:"max_heart_rate"`
	AvgPaceSecPerKm float64  `json:"avg_pace_sec_per_km"`
	AvgCadence      int      `json:"avg_cadence"`
	Calories        int      `json:"calories"`
	AscentMeters    float64  `json:"ascent_meters"`
	DescentMeters   float64  `json:"descent_meters"`
	TrainingLoad    *float64 `json:"training_load"`
	HRDriftPct      *float64 `json:"hr_drift_pct"`
	PaceCVPct       *float64 `json:"pace_cv_pct"`
	Tags            []string `json:"tags"`
	CreatedAt       string   `json:"created_at"`
}

func writeWorkouts(w io.Writer, enc *json.Encoder, db *sql.DB, userID int64) error {
	rows, err := db.Query(`
		SELECT wo.id, wo.sport, wo.sub_sport, wo.is_indoor, wo.title, wo.title_source,
		       wo.started_at, wo.duration_seconds, wo.distance_meters,
		       wo.avg_heart_rate, wo.max_heart_rate, wo.avg_pace_sec_per_km,
		       wo.avg_cadence, wo.calories, wo.ascent_meters, wo.descent_meters,
		       wo.training_load, wo.hr_drift_pct, wo.pace_cv_pct, wo.created_at,
		       GROUP_CONCAT(wt.tag, char(31)) AS tags
		FROM workouts wo
		LEFT JOIN workout_tags wt ON wt.workout_id = wo.id
		WHERE wo.user_id = ?
		GROUP BY wo.id
		ORDER BY wo.id`, userID)
	if err != nil {
		emptyArray(w, "workouts")
		return err
	}
	return streamArray(w, enc, "workouts", rows, func(rows *sql.Rows) (any, error) {
		var wo exportedWorkout
		var isIndoor int
		var trainingLoad, hrDrift, paceCV sql.NullFloat64
		var tags sql.NullString
		if err := rows.Scan(
			&wo.ID, &wo.Sport, &wo.SubSport, &isIndoor, &wo.Title, &wo.TitleSource,
			&wo.StartedAt, &wo.DurationSeconds, &wo.DistanceMeters,
			&wo.AvgHeartRate, &wo.MaxHeartRate, &wo.AvgPaceSecPerKm,
			&wo.AvgCadence, &wo.Calories, &wo.AscentMeters, &wo.DescentMeters,
			&trainingLoad, &hrDrift, &paceCV, &wo.CreatedAt, &tags,
		); err != nil {
			return nil, err
		}
		wo.IsIndoor = isIndoor != 0
		wo.Title = decryptOrKeep("workouts", "title", wo.ID, wo.Title)
		wo.TrainingLoad = nullFloat(trainingLoad)
		wo.HRDriftPct = nullFloat(hrDrift)
		wo.PaceCVPct = nullFloat(paceCV)
		wo.Tags = splitTags(tags)
		return wo, nil
	})
}

func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

type exportedLactateTest struct {
	ID                int64   `json:"id"`
	WorkoutID         *int64  `json:"workout_id"`
	Date              string  `json:"date"`
	Comment           string  `json:"comment"`
	ProtocolType      string  `json:"protocol_type"`
	WarmupDurationMin int     `json:"warmup_duration_min"`
	StageDurationMin  int     `json:"stage_duration_min"`
	StartSpeedKmh     float64 `json:"start_speed_kmh"`
	SpeedIncrementKmh float64 `json:"speed_increment_kmh"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

func writeLactateTests(w io.Writer, enc *json.Encoder, db *sql.DB, userID int64) error {
	rows, err := db.Query(`
		SELECT id, workout_id, date, comment, protocol_type,
		       warmup_duration_min, stage_duration_min,
		       start_speed_kmh, speed_increment_kmh, created_at, updated_at
		FROM lactate_tests
		WHERE user_id = ?
		ORDER BY id`, userID)
	if err != nil {
		emptyArray(w, "lactate_tests")
		return err
	}
	return streamArray(w, enc, "lactate_tests", rows, func(rows *sql.Rows) (any, error) {
		var lt exportedLactateTest
		var workoutID sql.NullInt64
		if err := rows.Scan(
			&lt.ID, &workoutID, &lt.Date, &lt.Comment, &lt.ProtocolType,
			&lt.WarmupDurationMin, &lt.StageDurationMin,
			&lt.StartSpeedKmh, &lt.SpeedIncrementKmh, &lt.CreatedAt, &lt.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if workoutID.Valid {
			id := workoutID.Int64
			lt.WorkoutID = &id
		}
		lt.Comment = decryptOrKeep("lactate_tests", "comment", lt.ID, lt.Comment)
		return lt, nil
	})
}

type exportedLactateStage struct {
	ID           int64   `json:"id"`
	TestID       int64   `json:"test_id"`
	StageNumber  int     `json:"stage_number"`
	SpeedKmh     float64 `json:"speed_kmh"`
	LactateMmol  float64 `json:"lactate_mmol"`
	HeartRateBpm int     `json:"heart_rate_bpm"`
	RPE          *int    `json:"rpe"`
	Notes        string  `json:"notes"`
}

// writeLactateStages exports stage rows flat (each carrying its test_id) rather
// than nested inside lactate_tests, so both domains can stream row by row.
// lactate_test_stages has no user_id of its own, so ownership is enforced by
// joining through lactate_tests.
func writeLactateStages(w io.Writer, enc *json.Encoder, db *sql.DB, userID int64) error {
	rows, err := db.Query(`
		SELECT s.id, s.test_id, s.stage_number, s.speed_kmh, s.lactate_mmol,
		       s.heart_rate_bpm, s.rpe, s.notes
		FROM lactate_test_stages s
		JOIN lactate_tests t ON t.id = s.test_id
		WHERE t.user_id = ?
		ORDER BY s.test_id, s.stage_number`, userID)
	if err != nil {
		emptyArray(w, "lactate_stages")
		return err
	}
	return streamArray(w, enc, "lactate_stages", rows, func(rows *sql.Rows) (any, error) {
		var s exportedLactateStage
		var rpe sql.NullInt64
		if err := rows.Scan(
			&s.ID, &s.TestID, &s.StageNumber, &s.SpeedKmh, &s.LactateMmol,
			&s.HeartRateBpm, &rpe, &s.Notes,
		); err != nil {
			return nil, err
		}
		if rpe.Valid {
			v := int(rpe.Int64)
			s.RPE = &v
		}
		s.Notes = decryptOrKeep("lactate_stages", "notes", s.ID, s.Notes)
		return s, nil
	})
}

type exportedPreference struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func writePreferences(w io.Writer, enc *json.Encoder, db *sql.DB, userID int64) error {
	rows, err := db.Query(`
		SELECT key, value FROM user_preferences WHERE user_id = ? ORDER BY key`, userID)
	if err != nil {
		emptyArray(w, "preferences")
		return err
	}
	return streamArray(w, enc, "preferences", rows, func(rows *sql.Rows) (any, error) {
		var p exportedPreference
		if err := rows.Scan(&p.Key, &p.Value); err != nil {
			return nil, err
		}
		if redactedPreferenceKeys[p.Key] {
			// Secret values are dropped; the key is kept so the user can see
			// the setting exists.
			p.Value = ""
			return p, nil
		}
		if encryptedPreferenceKeys[p.Key] {
			p.Value = decryptOrKeep("preferences", p.Key, userID, p.Value)
		}
		return p, nil
	})
}

type exportedSession struct {
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

// writeSessions exports session metadata only. The sessions table stores just
// the hashed token plus the two timestamps — no user agent or IP is recorded —
// and neither the token nor its hash is ever selected here.
func writeSessions(w io.Writer, enc *json.Encoder, db *sql.DB, userID int64) error {
	rows, err := db.Query(`
		SELECT created_at, expires_at FROM sessions WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		emptyArray(w, "sessions")
		return err
	}
	return streamArray(w, enc, "sessions", rows, func(rows *sql.Rows) (any, error) {
		var createdAt, expiresAt any
		if err := rows.Scan(&createdAt, &expiresAt); err != nil {
			return nil, err
		}
		return exportedSession{
			CreatedAt: formatTimestamp(createdAt),
			ExpiresAt: formatTimestamp(expiresAt),
		}, nil
	})
}
