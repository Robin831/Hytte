package export

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-export-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id         INTEGER PRIMARY KEY,
			email      TEXT UNIQUE NOT NULL,
			name       TEXT NOT NULL,
			picture    TEXT NOT NULL DEFAULT '',
			google_id  TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_admin   INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE sessions (
			token      TEXT PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL
		);
		CREATE TABLE user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE notes (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title      TEXT NOT NULL DEFAULT '',
			content    TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE note_tags (
			note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
			tag     TEXT NOT NULL,
			PRIMARY KEY (note_id, tag)
		);
		CREATE TABLE workouts (
			id                  INTEGER PRIMARY KEY,
			user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			sport               TEXT NOT NULL DEFAULT 'other',
			sub_sport           TEXT NOT NULL DEFAULT '',
			is_indoor           INTEGER NOT NULL DEFAULT 0,
			title               TEXT NOT NULL DEFAULT '',
			title_source        TEXT NOT NULL DEFAULT '',
			started_at          TEXT NOT NULL DEFAULT '',
			duration_seconds    INTEGER NOT NULL DEFAULT 0,
			distance_meters     REAL NOT NULL DEFAULT 0,
			avg_heart_rate      INTEGER NOT NULL DEFAULT 0,
			max_heart_rate      INTEGER NOT NULL DEFAULT 0,
			avg_pace_sec_per_km REAL NOT NULL DEFAULT 0,
			avg_cadence         INTEGER NOT NULL DEFAULT 0,
			calories            INTEGER NOT NULL DEFAULT 0,
			ascent_meters       REAL NOT NULL DEFAULT 0,
			descent_meters      REAL NOT NULL DEFAULT 0,
			fit_file_hash       TEXT NOT NULL DEFAULT '',
			created_at          TEXT NOT NULL DEFAULT '',
			training_load       REAL,
			hr_drift_pct        REAL,
			pace_cv_pct         REAL
		);
		CREATE TABLE workout_tags (
			workout_id INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
			tag        TEXT NOT NULL,
			PRIMARY KEY (workout_id, tag)
		);
		CREATE TABLE lactate_tests (
			id                  INTEGER PRIMARY KEY,
			user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			workout_id          INTEGER REFERENCES workouts(id) ON DELETE SET NULL,
			date                TEXT NOT NULL DEFAULT '',
			comment             TEXT NOT NULL DEFAULT '',
			protocol_type       TEXT NOT NULL DEFAULT 'standard',
			warmup_duration_min INTEGER NOT NULL DEFAULT 10,
			stage_duration_min  INTEGER NOT NULL DEFAULT 5,
			start_speed_kmh     REAL NOT NULL DEFAULT 11.5,
			speed_increment_kmh REAL NOT NULL DEFAULT 0.5,
			created_at          TEXT NOT NULL DEFAULT '',
			updated_at          TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE lactate_test_stages (
			id             INTEGER PRIMARY KEY,
			test_id        INTEGER NOT NULL REFERENCES lactate_tests(id) ON DELETE CASCADE,
			stage_number   INTEGER NOT NULL,
			speed_kmh      REAL NOT NULL,
			lactate_mmol   REAL NOT NULL,
			heart_rate_bpm INTEGER NOT NULL DEFAULT 0,
			rpe            INTEGER,
			notes          TEXT NOT NULL DEFAULT '',
			UNIQUE(test_id, stage_number)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// archive mirrors the exported document so tests can assert on its shape.
type archive struct {
	SchemaVersion int                    `json:"schema_version"`
	ExportedAt    string                 `json:"exported_at"`
	Profile       profile                `json:"profile"`
	Notes         []exportedNote         `json:"notes"`
	Workouts      []exportedWorkout      `json:"workouts"`
	LactateTests  []exportedLactateTest  `json:"lactate_tests"`
	LactateStages []exportedLactateStage `json:"lactate_stages"`
	Preferences   []exportedPreference   `json:"preferences"`
	Sessions      []exportedSession      `json:"sessions"`
	Errors        []domainError          `json:"errors"`
}

func enc(t *testing.T, plaintext string) string {
	t.Helper()
	v, err := encryption.EncryptField(plaintext)
	if err != nil {
		t.Fatalf("encrypt %q: %v", plaintext, err)
	}
	return v
}

func createUser(t *testing.T, db *sql.DB, id int64, email string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO users (id, email, name, picture, google_id, created_at, is_admin) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		id, email, "User "+email, "https://example.com/pic.png", "google-"+email, time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("insert user %d: %v", id, err)
	}
}

// seedUser inserts one row in every exported domain for the given user. The
// suffix keeps values distinguishable between users.
func seedUser(t *testing.T, db *sql.DB, userID int64, suffix string) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO notes (user_id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		userID, enc(t, "Note "+suffix), enc(t, "Content "+suffix), "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z",
	); err != nil {
		t.Fatalf("insert note: %v", err)
	}
	var noteID int64
	if err := db.QueryRow(`SELECT id FROM notes WHERE user_id = ?`, userID).Scan(&noteID); err != nil {
		t.Fatalf("select note id: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO note_tags (note_id, tag) VALUES (?, ?), (?, ?)`, noteID, "zeta", noteID, "alpha"); err != nil {
		t.Fatalf("insert note tags: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash, created_at, training_load)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, "running", enc(t, "Workout "+suffix), "2026-01-03T06:00:00Z", 3600, 10000.0, "hash-"+suffix, "2026-01-03T07:00:00Z", 42.5,
	); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO lactate_tests (user_id, date, comment, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		userID, "2026-01-04", enc(t, "Lactate comment "+suffix), "2026-01-04T00:00:00Z", "2026-01-04T00:00:00Z",
	); err != nil {
		t.Fatalf("insert lactate test: %v", err)
	}
	var testID int64
	if err := db.QueryRow(`SELECT id FROM lactate_tests WHERE user_id = ?`, userID).Scan(&testID); err != nil {
		t.Fatalf("select lactate test id: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO lactate_test_stages (test_id, stage_number, speed_kmh, lactate_mmol, heart_rate_bpm, rpe, notes)
		 VALUES (?, 1, 12.0, 2.1, 150, 5, ?)`,
		testID, enc(t, "Stage note "+suffix),
	); err != nil {
		t.Fatalf("insert lactate stage: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (?, 'theme', ?), (?, 'claude_cli_path', ?), (?, 'stride_custom_prompt', ?)`,
		userID, "dark-"+suffix,
		userID, enc(t, "/usr/local/bin/claude-"+suffix),
		userID, enc(t, "Custom prompt "+suffix),
	); err != nil {
		t.Fatalf("insert preferences: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		"hashed-token-"+suffix, userID, time.Now().UTC(), time.Now().UTC().Add(24*time.Hour),
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func doExport(t *testing.T, db *sql.DB, userID int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/settings/export", nil)
	if userID > 0 {
		user := &auth.User{ID: userID, Email: "user@example.com", Name: "User"}
		req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	}
	rec := httptest.NewRecorder()
	ExportHandler(db).ServeHTTP(rec, req)
	return rec
}

func decodeArchive(t *testing.T, rec *httptest.ResponseRecorder) archive {
	t.Helper()
	var a archive
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode archive: %v\nbody: %s", err, rec.Body.String())
	}
	return a
}

func TestExportHandler_Unauthenticated(t *testing.T) {
	db := setupTestDB(t)

	rec := doExport(t, db, 0)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("expected JSON content type, got %q", got)
	}
	if rec.Header().Get("Content-Disposition") != "" {
		t.Error("unauthenticated response must not set a download header")
	}
}

func TestExportHandler_Headers(t *testing.T) {
	db := setupTestDB(t)
	createUser(t, db, 1, "a@example.com")

	rec := doExport(t, db, 1)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("expected application/json, got %q", got)
	}
	want := `attachment; filename="hytte-export-` + time.Now().Format("2006-01-02") + `.json"`
	if got := rec.Header().Get("Content-Disposition"); got != want {
		t.Errorf("expected Content-Disposition %q, got %q", want, got)
	}
}

func TestExportHandler_EmptyAccount(t *testing.T) {
	db := setupTestDB(t)
	createUser(t, db, 1, "a@example.com")

	a := decodeArchive(t, doExport(t, db, 1))

	if a.SchemaVersion != SchemaVersion {
		t.Errorf("expected schema version %d, got %d", SchemaVersion, a.SchemaVersion)
	}
	if _, err := time.Parse(time.RFC3339, a.ExportedAt); err != nil {
		t.Errorf("exported_at %q is not RFC3339: %v", a.ExportedAt, err)
	}
	if a.Profile.ID != 1 || a.Profile.Email != "a@example.com" {
		t.Errorf("unexpected profile: %+v", a.Profile)
	}
	counts := map[string]int{
		"notes":          len(a.Notes),
		"workouts":       len(a.Workouts),
		"lactate_tests":  len(a.LactateTests),
		"lactate_stages": len(a.LactateStages),
		"preferences":    len(a.Preferences),
		"sessions":       len(a.Sessions),
		"errors":         len(a.Errors),
	}
	for key, n := range counts {
		if n != 0 {
			t.Errorf("expected %s to be empty, got %d entries", key, n)
		}
	}
}

func TestExportHandler_DecryptsFieldsAndRedactsSecrets(t *testing.T) {
	db := setupTestDB(t)
	createUser(t, db, 1, "a@example.com")
	seedUser(t, db, 1, "one")

	rec := doExport(t, db, 1)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	a := decodeArchive(t, rec)

	if len(a.Notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(a.Notes))
	}
	if a.Notes[0].Title != "Note one" || a.Notes[0].Content != "Content one" {
		t.Errorf("note not decrypted: %+v", a.Notes[0])
	}
	if got := a.Notes[0].Tags; len(got) != 2 || got[0] != "alpha" || got[1] != "zeta" {
		t.Errorf("expected sorted tags [alpha zeta], got %v", got)
	}

	if len(a.Workouts) != 1 {
		t.Fatalf("expected 1 workout, got %d", len(a.Workouts))
	}
	if a.Workouts[0].Title != "Workout one" {
		t.Errorf("workout title not decrypted: %q", a.Workouts[0].Title)
	}
	if a.Workouts[0].TrainingLoad == nil || *a.Workouts[0].TrainingLoad != 42.5 {
		t.Errorf("expected training_load 42.5, got %v", a.Workouts[0].TrainingLoad)
	}

	if len(a.LactateTests) != 1 || a.LactateTests[0].Comment != "Lactate comment one" {
		t.Errorf("lactate comment not decrypted: %+v", a.LactateTests)
	}
	if len(a.LactateStages) != 1 || a.LactateStages[0].Notes != "Stage note one" {
		t.Errorf("lactate stage notes not decrypted: %+v", a.LactateStages)
	}
	if a.LactateStages[0].RPE == nil || *a.LactateStages[0].RPE != 5 {
		t.Errorf("expected rpe 5, got %v", a.LactateStages[0].RPE)
	}

	prefs := map[string]string{}
	for _, p := range a.Preferences {
		prefs[p.Key] = p.Value
	}
	if prefs["theme"] != "dark-one" {
		t.Errorf("expected theme dark-one, got %q", prefs["theme"])
	}
	if prefs["stride_custom_prompt"] != "Custom prompt one" {
		t.Errorf("expected decrypted stride_custom_prompt, got %q", prefs["stride_custom_prompt"])
	}
	if v, ok := prefs["claude_cli_path"]; !ok || v != "" {
		t.Errorf("expected claude_cli_path value to be redacted, got %q (present=%v)", v, ok)
	}
	if strings.Contains(rec.Body.String(), "/usr/local/bin/claude") {
		t.Error("redacted preference value leaked into the export body")
	}

	if len(a.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(a.Sessions))
	}
	if _, err := time.Parse(time.RFC3339, a.Sessions[0].CreatedAt); err != nil {
		t.Errorf("session created_at %q is not RFC3339: %v", a.Sessions[0].CreatedAt, err)
	}
	for _, forbidden := range []string{"hashed-token-one", "token"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Errorf("export body must not contain %q", forbidden)
		}
	}
}

func TestExportHandler_CorruptCiphertextFallsBack(t *testing.T) {
	db := setupTestDB(t)
	createUser(t, db, 1, "a@example.com")

	const corrupt = "enc:not-valid-base64!!"
	if _, err := db.Exec(
		`INSERT INTO notes (user_id, title, content, created_at, updated_at) VALUES (1, ?, ?, '', '')`,
		corrupt, corrupt,
	); err != nil {
		t.Fatalf("insert corrupt note: %v", err)
	}

	rec := doExport(t, db, 1)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 despite corrupt ciphertext, got %d", rec.Code)
	}
	a := decodeArchive(t, rec)
	if len(a.Notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(a.Notes))
	}
	if a.Notes[0].Title != corrupt || a.Notes[0].Content != corrupt {
		t.Errorf("expected stored value fallback, got %+v", a.Notes[0])
	}
	if len(a.Errors) != 0 {
		t.Errorf("a decrypt failure must not be reported as a domain error, got %+v", a.Errors)
	}
}

func TestExportHandler_ScopedToOwnUser(t *testing.T) {
	db := setupTestDB(t)
	createUser(t, db, 1, "a@example.com")
	createUser(t, db, 2, "b@example.com")
	seedUser(t, db, 1, "one")
	seedUser(t, db, 2, "two")

	rec := doExport(t, db, 1)
	a := decodeArchive(t, rec)

	if strings.Contains(rec.Body.String(), "two") {
		t.Errorf("export leaked the other user's data:\n%s", rec.Body.String())
	}
	if a.Profile.Email != "a@example.com" {
		t.Errorf("expected own profile, got %q", a.Profile.Email)
	}
	for name, n := range map[string]int{
		"notes":          len(a.Notes),
		"workouts":       len(a.Workouts),
		"lactate_tests":  len(a.LactateTests),
		"lactate_stages": len(a.LactateStages),
		"sessions":       len(a.Sessions),
	} {
		if n != 1 {
			t.Errorf("expected exactly 1 %s row, got %d", name, n)
		}
	}
}

func TestExportHandler_DomainErrorDoesNotBreakDocument(t *testing.T) {
	db := setupTestDB(t)
	createUser(t, db, 1, "a@example.com")
	seedUser(t, db, 1, "one")

	// Drop a table so one domain query fails while the rest still stream.
	if _, err := db.Exec(`DROP TABLE note_tags`); err != nil {
		t.Fatalf("drop note_tags: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE notes`); err != nil {
		t.Fatalf("drop notes: %v", err)
	}

	rec := doExport(t, db, 1)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	a := decodeArchive(t, rec)

	if len(a.Errors) != 1 || a.Errors[0].Domain != "notes" {
		t.Fatalf("expected a single notes error, got %+v", a.Errors)
	}
	if len(a.Workouts) != 1 {
		t.Errorf("remaining domains should still export, got %d workouts", len(a.Workouts))
	}
}
