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
	"strconv"
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
	write func(s *jsonStream, db *sql.DB, userID int64) error
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

		s := &jsonStream{w: w}

		s.raw("{")
		s.field("schema_version", SchemaVersion)
		s.raw(",")
		s.field("exported_at", now.UTC().Format(time.RFC3339))
		s.raw(",")
		s.field("profile", loadProfile(db, user))
		s.flush()

		failures := []domainError{}
		for _, d := range allDomains {
			s.raw(",")
			if err := d.write(s, db, user.ID); err != nil {
				// Each domain writer closes the array it opened before
				// returning, so the document stays well-formed and the failure
				// is only recorded here — the headers are long since committed.
				log.Printf("export: failed to export %s for user %d: %v", d.key, user.ID, err)
				failures = append(failures, domainError{Domain: d.key, Error: "this section could not be exported"})
			}
			s.flush()
		}

		s.raw(",")
		s.field("errors", failures)
		s.raw("}")
		s.flush()
	}
}

// jsonStream writes a JSON document incrementally onto the response writer.
// Values are marshalled one at a time — a single row is the largest thing ever
// held in memory — and written without the trailing newline json.Encoder would
// append, so the archive is compact JSON rather than a ragged mix of raw
// fragments and newline-terminated values.
//
// The response status is committed before the first byte is written, so write
// and marshal errors can only be logged, never turned into an error status.
type jsonStream struct {
	w io.Writer
}

// raw writes a literal JSON fragment such as a brace, bracket or comma.
func (s *jsonStream) raw(fragment string) {
	if _, err := io.WriteString(s.w, fragment); err != nil {
		log.Printf("export: write failed: %v", err)
	}
}

// value writes a single JSON value. A value that cannot be marshalled is
// written as null so the surrounding document stays parseable.
func (s *jsonStream) value(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("export: marshal failed: %v", err)
		s.raw("null")
		return
	}
	if _, err := s.w.Write(b); err != nil {
		log.Printf("export: write failed: %v", err)
	}
}

// field writes a `"key":value` pair without any surrounding separators.
func (s *jsonStream) field(key string, v any) {
	s.raw(strconv.Quote(key) + ":")
	s.value(v)
}

// emptyArray writes `"key":[]`, used when a domain query fails before any rows
// could be read so the envelope still contains the key.
func (s *jsonStream) emptyArray(key string) {
	s.field(key, []any{})
}

// array writes `"key":[...]` by scanning rows one at a time and encoding each
// straight onto the writer. The bracket is closed on every path — including a
// scan error partway through — so the surrounding document stays well-formed.
func (s *jsonStream) array(key string, rows *sql.Rows, scan func(*sql.Rows) (any, error)) error {
	defer rows.Close()

	s.raw(strconv.Quote(key) + ":[")
	err := s.arrayBody(rows, scan)
	s.raw("]")
	return err
}

func (s *jsonStream) arrayBody(rows *sql.Rows, scan func(*sql.Rows) (any, error)) error {
	first := true
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return err
		}
		if !first {
			s.raw(",")
		}
		first = false
		s.value(v)
	}
	return rows.Err()
}

// flush pushes what has been written so far to the client, so a large export
// starts arriving before every domain has been queried.
func (s *jsonStream) flush() {
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
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

// decryptOptional is decryptOrKeep for columns that are legitimately empty,
// such as session metadata recorded before the columns existed.
func decryptOptional(domainKey, field string, id int64, value string) string {
	if value == "" {
		return ""
	}
	return decryptOrKeep(domainKey, field, id, value)
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

func writeNotes(s *jsonStream, db *sql.DB, userID int64) error {
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
		s.emptyArray("notes")
		return err
	}
	return s.array("notes", rows, func(rows *sql.Rows) (any, error) {
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

func writeWorkouts(s *jsonStream, db *sql.DB, userID int64) error {
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
		s.emptyArray("workouts")
		return err
	}
	return s.array("workouts", rows, func(rows *sql.Rows) (any, error) {
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

func writeLactateTests(s *jsonStream, db *sql.DB, userID int64) error {
	rows, err := db.Query(`
		SELECT id, workout_id, date, comment, protocol_type,
		       warmup_duration_min, stage_duration_min,
		       start_speed_kmh, speed_increment_kmh, created_at, updated_at
		FROM lactate_tests
		WHERE user_id = ?
		ORDER BY id`, userID)
	if err != nil {
		s.emptyArray("lactate_tests")
		return err
	}
	return s.array("lactate_tests", rows, func(rows *sql.Rows) (any, error) {
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
func writeLactateStages(s *jsonStream, db *sql.DB, userID int64) error {
	rows, err := db.Query(`
		SELECT s.id, s.test_id, s.stage_number, s.speed_kmh, s.lactate_mmol,
		       s.heart_rate_bpm, s.rpe, s.notes
		FROM lactate_test_stages s
		JOIN lactate_tests t ON t.id = s.test_id
		WHERE t.user_id = ?
		ORDER BY s.test_id, s.stage_number`, userID)
	if err != nil {
		s.emptyArray("lactate_stages")
		return err
	}
	return s.array("lactate_stages", rows, func(rows *sql.Rows) (any, error) {
		var stage exportedLactateStage
		var rpe sql.NullInt64
		if err := rows.Scan(
			&stage.ID, &stage.TestID, &stage.StageNumber, &stage.SpeedKmh, &stage.LactateMmol,
			&stage.HeartRateBpm, &rpe, &stage.Notes,
		); err != nil {
			return nil, err
		}
		if rpe.Valid {
			v := int(rpe.Int64)
			stage.RPE = &v
		}
		stage.Notes = decryptOrKeep("lactate_stages", "notes", stage.ID, stage.Notes)
		return stage, nil
	})
}

type exportedPreference struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func writePreferences(s *jsonStream, db *sql.DB, userID int64) error {
	rows, err := db.Query(`
		SELECT key, value FROM user_preferences WHERE user_id = ? ORDER BY key`, userID)
	if err != nil {
		s.emptyArray("preferences")
		return err
	}
	return s.array("preferences", rows, func(rows *sql.Rows) (any, error) {
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
	UserAgent string `json:"user_agent"`
	IPAddress string `json:"ip_address"`
}

// writeSessions exports session metadata only: the timestamps plus the user
// agent and client IP recorded at sign-in (both stored encrypted, so they are
// decrypted here). Sessions created before those columns existed export them as
// empty strings. The token column holds a SHA-256 hash and is never selected.
func writeSessions(s *jsonStream, db *sql.DB, userID int64) error {
	rows, err := db.Query(`
		SELECT created_at, expires_at, user_agent, ip_address
		FROM sessions WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		s.emptyArray("sessions")
		return err
	}
	return s.array("sessions", rows, func(rows *sql.Rows) (any, error) {
		var createdAt, expiresAt any
		var userAgent, ipAddress string
		if err := rows.Scan(&createdAt, &expiresAt, &userAgent, &ipAddress); err != nil {
			return nil, err
		}
		return exportedSession{
			CreatedAt: formatTimestamp(createdAt),
			ExpiresAt: formatTimestamp(expiresAt),
			UserAgent: decryptOptional("sessions", "user_agent", userID, userAgent),
			IPAddress: decryptOptional("sessions", "ip_address", userID, ipAddress),
		}, nil
	})
}
