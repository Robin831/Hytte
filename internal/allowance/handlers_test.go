package allowance

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-allowance-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id        INTEGER PRIMARY KEY,
		email     TEXT UNIQUE NOT NULL,
		name      TEXT NOT NULL,
		picture   TEXT NOT NULL DEFAULT '',
		google_id TEXT UNIQUE NOT NULL,
		is_admin  INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS family_links (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		nickname     TEXT NOT NULL DEFAULT '',
		avatar_emoji TEXT NOT NULL DEFAULT '⭐',
		created_at   TEXT NOT NULL DEFAULT '',
		UNIQUE(parent_id, child_id),
		UNIQUE(child_id)
	);

	CREATE INDEX IF NOT EXISTS idx_family_links_parent ON family_links(parent_id);
	CREATE INDEX IF NOT EXISTS idx_family_links_child ON family_links(child_id);

	CREATE TABLE IF NOT EXISTS allowance_chores (
		id                INTEGER PRIMARY KEY,
		parent_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id          INTEGER REFERENCES users(id) ON DELETE CASCADE,
		name              TEXT NOT NULL DEFAULT '',
		description       TEXT NOT NULL DEFAULT '',
		amount            REAL NOT NULL DEFAULT 0,
		currency          TEXT NOT NULL DEFAULT 'NOK',
		frequency         TEXT NOT NULL DEFAULT 'daily',
		icon              TEXT NOT NULL DEFAULT '🧹',
		requires_approval INTEGER NOT NULL DEFAULT 1,
		active            INTEGER NOT NULL DEFAULT 1,
		created_at        TEXT NOT NULL DEFAULT '',
		completion_mode   TEXT NOT NULL DEFAULT 'solo',
		min_team_size     INTEGER NOT NULL DEFAULT 2,
		team_bonus_pct    REAL NOT NULL DEFAULT 10.0
	);

	CREATE TABLE IF NOT EXISTS allowance_completions (
		id            INTEGER PRIMARY KEY,
		chore_id      INTEGER NOT NULL REFERENCES allowance_chores(id) ON DELETE CASCADE,
		child_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		date          TEXT NOT NULL,
		status        TEXT NOT NULL DEFAULT 'pending',
		approved_by   INTEGER REFERENCES users(id),
		approved_at   TEXT,
		notes         TEXT NOT NULL DEFAULT '',
		quality_bonus REAL NOT NULL DEFAULT 0,
		photo_path    TEXT NOT NULL DEFAULT '',
		created_at    TEXT NOT NULL DEFAULT '',
		UNIQUE(chore_id, child_id, date)
	);

	CREATE TABLE IF NOT EXISTS allowance_extras (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,
		name         TEXT NOT NULL DEFAULT '',
		amount       REAL NOT NULL DEFAULT 0,
		currency     TEXT NOT NULL DEFAULT 'NOK',
		status       TEXT NOT NULL DEFAULT 'open',
		claimed_by   INTEGER REFERENCES users(id),
		completed_at TEXT,
		approved_at  TEXT,
		expires_at   TEXT,
		created_at   TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS allowance_bonus_rules (
		id          INTEGER PRIMARY KEY,
		parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		type        TEXT NOT NULL,
		multiplier  REAL NOT NULL DEFAULT 1.0,
		flat_amount REAL NOT NULL DEFAULT 0,
		active      INTEGER NOT NULL DEFAULT 1,
		UNIQUE(parent_id, type)
	);

	CREATE TABLE IF NOT EXISTS allowance_payouts (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		week_start   TEXT NOT NULL,
		base_amount  REAL NOT NULL DEFAULT 0,
		bonus_amount REAL NOT NULL DEFAULT 0,
		total_amount REAL NOT NULL DEFAULT 0,
		currency     TEXT NOT NULL DEFAULT 'NOK',
		paid_out     INTEGER NOT NULL DEFAULT 0,
		paid_at      TEXT,
		created_at   TEXT NOT NULL DEFAULT '',
		UNIQUE(parent_id, child_id, week_start)
	);

	CREATE TABLE IF NOT EXISTS allowance_settings (
		id                 INTEGER PRIMARY KEY,
		parent_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		base_weekly_amount REAL NOT NULL DEFAULT 0,
		currency           TEXT NOT NULL DEFAULT 'NOK',
		auto_approve_hours INTEGER NOT NULL DEFAULT 24,
		updated_at         TEXT NOT NULL DEFAULT '',
		UNIQUE(parent_id, child_id)
	);

	CREATE TABLE IF NOT EXISTS allowance_team_completions (
		id            INTEGER PRIMARY KEY,
		completion_id INTEGER NOT NULL REFERENCES allowance_completions(id) ON DELETE CASCADE,
		child_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		joined_at     TEXT NOT NULL DEFAULT '',
		UNIQUE(completion_id, child_id)
	);

	CREATE TABLE IF NOT EXISTS allowance_savings_goals (
		id             INTEGER PRIMARY KEY,
		parent_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name           TEXT NOT NULL DEFAULT '',
		target_amount  REAL NOT NULL DEFAULT 0,
		current_amount REAL NOT NULL DEFAULT 0,
		currency       TEXT NOT NULL DEFAULT 'NOK',
		deadline       TEXT,
		created_at     TEXT NOT NULL DEFAULT '',
		updated_at     TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS user_features (
		user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		feature_key TEXT NOT NULL,
		enabled     INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (user_id, feature_key)
	);

	CREATE TABLE IF NOT EXISTS allowance_bingo_cards (
		id              INTEGER PRIMARY KEY,
		child_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		parent_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		week_start      TEXT NOT NULL,
		cells           TEXT NOT NULL DEFAULT '[]',
		completed_lines INTEGER NOT NULL DEFAULT 0,
		full_card       INTEGER NOT NULL DEFAULT 0,
		bonus_earned    REAL NOT NULL DEFAULT 0,
		created_at      TEXT NOT NULL DEFAULT '',
		updated_at      TEXT NOT NULL DEFAULT '',
		UNIQUE(child_id, week_start)
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	// Seed test users.
	if _, err := db.Exec(`
		INSERT INTO users (id, email, name, google_id) VALUES
		(1, 'parent@test.com', 'Parent', 'gp1'),
		(2, 'child@test.com',  'Child',  'gc2')
	`); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	return db
}

// linkParentChild creates a family_links row linking parent 1 to child 2.
func linkParentChild(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 2, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link parent-child: %v", err)
	}
}

var testParent = &auth.User{ID: 1, Email: "parent@test.com", Name: "Parent"}
var testChild = &auth.User{ID: 2, Email: "child@test.com", Name: "Child"}

func newRequest(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			panic(err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func withUser(r *http.Request, user *auth.User) *http.Request {
	return r.WithContext(auth.ContextWithUser(r.Context(), user))
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func decode(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, body)
	}
}

// ---- Storage tests ----

func TestCreateAndGetChore(t *testing.T) {
	db := setupTestDB(t)

	chore, err := CreateChore(db, 1, nil, "Clean room", "Tidy everything", 20, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	if chore.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if chore.Name != "Clean room" {
		t.Errorf("expected name 'Clean room', got %q", chore.Name)
	}
	if chore.Amount != 20 {
		t.Errorf("expected amount 20, got %v", chore.Amount)
	}
	if chore.Currency != "NOK" {
		t.Errorf("expected currency NOK, got %q", chore.Currency)
	}

	// Fetch back.
	fetched, err := GetChoreByID(db, chore.ID, 1)
	if err != nil {
		t.Fatalf("GetChoreByID: %v", err)
	}
	if fetched.Name != "Clean room" {
		t.Errorf("round-trip name mismatch: got %q", fetched.Name)
	}
}

func TestGetChoreNotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := GetChoreByID(db, 999, 1)
	if err != ErrChoreNotFound {
		t.Errorf("expected ErrChoreNotFound, got %v", err)
	}
}

func TestUpdateChore(t *testing.T) {
	db := setupTestDB(t)

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}

	updated, err := UpdateChore(db, chore.ID, 1, nil, "Clean room updated", "", 25, "weekly", "🏠", false, true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("UpdateChore: %v", err)
	}
	if updated.Name != "Clean room updated" {
		t.Errorf("expected updated name, got %q", updated.Name)
	}
	if updated.Amount != 25 {
		t.Errorf("expected amount 25, got %v", updated.Amount)
	}
	if updated.RequiresApproval {
		t.Error("expected requires_approval=false after update")
	}
}

func TestDeactivateChore(t *testing.T) {
	db := setupTestDB(t)

	chore, err := CreateChore(db, 1, nil, "Dishes", "", 15, "daily", "🍽️", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}

	if err := DeactivateChore(db, chore.ID, 1); err != nil {
		t.Fatalf("DeactivateChore: %v", err)
	}

	fetched, err := GetChoreByID(db, chore.ID, 1)
	if err != nil {
		t.Fatalf("GetChoreByID after deactivate: %v", err)
	}
	if fetched.Active {
		t.Error("expected chore to be inactive after deactivation")
	}
}

func TestCreateCompletion(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}

	comp, err := CreateCompletion(db, chore.ID, 2, "2026-03-28", "Done!")
	if err != nil {
		t.Fatalf("CreateCompletion: %v", err)
	}
	if comp.Status != "pending" {
		t.Errorf("expected status pending, got %q", comp.Status)
	}
	if comp.Notes != "Done!" {
		t.Errorf("expected notes 'Done!', got %q", comp.Notes)
	}
}

func TestCreateCompletionDuplicate(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	_, err = CreateCompletion(db, chore.ID, 2, "2026-03-28", "")
	if err != nil {
		t.Fatalf("first completion: %v", err)
	}

	_, err = CreateCompletion(db, chore.ID, 2, "2026-03-28", "")
	if err != ErrCompletionExists {
		t.Errorf("expected ErrCompletionExists on duplicate, got %v", err)
	}
}

func TestApproveCompletion(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	comp, err := CreateCompletion(db, chore.ID, 2, "2026-03-28", "")
	if err != nil {
		t.Fatalf("CreateCompletion: %v", err)
	}

	approved, err := ApproveCompletion(db, comp.ID, 1)
	if err != nil {
		t.Fatalf("ApproveCompletion: %v", err)
	}
	if approved.Status != "approved" {
		t.Errorf("expected status approved, got %q", approved.Status)
	}

	// Cannot approve again.
	_, err = ApproveCompletion(db, comp.ID, 1)
	if err != ErrCompletionNotPending {
		t.Errorf("expected ErrCompletionNotPending on double-approve, got %v", err)
	}
}

func TestRejectCompletion(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	comp, err := CreateCompletion(db, chore.ID, 2, "2026-03-28", "")
	if err != nil {
		t.Fatalf("CreateCompletion: %v", err)
	}

	rejected, err := RejectCompletion(db, comp.ID, 1, "Not done properly")
	if err != nil {
		t.Fatalf("RejectCompletion: %v", err)
	}
	if rejected.Status != "rejected" {
		t.Errorf("expected status rejected, got %q", rejected.Status)
	}
	if rejected.Notes != "Not done properly" {
		t.Errorf("expected rejection reason in notes, got %q", rejected.Notes)
	}
}

func TestUpsertSettings(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	settings, err := UpsertSettings(db, 1, 2, 100, 48)
	if err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}
	if settings.BaseWeeklyAmount != 100 {
		t.Errorf("expected base_weekly_amount 100, got %v", settings.BaseWeeklyAmount)
	}
	if settings.AutoApproveHours != 48 {
		t.Errorf("expected auto_approve_hours 48, got %v", settings.AutoApproveHours)
	}

	// Get settings back.
	fetched, err := GetSettings(db, 1, 2)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if fetched.BaseWeeklyAmount != 100 {
		t.Errorf("expected base_weekly_amount 100 after fetch, got %v", fetched.BaseWeeklyAmount)
	}
}

func TestGetSettingsDefaults(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// No settings row — should return defaults.
	settings, err := GetSettings(db, 1, 2)
	if err != nil {
		t.Fatalf("GetSettings (defaults): %v", err)
	}
	if settings.AutoApproveHours != 24 {
		t.Errorf("expected default auto_approve_hours 24, got %v", settings.AutoApproveHours)
	}
}

// ---- Handler tests ----

func TestListChoresHandlerEmpty(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := ListChoresHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/chores", nil), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Chores []Chore `json:"chores"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if resp.Chores == nil {
		t.Error("expected non-nil chores slice")
	}
	if len(resp.Chores) != 0 {
		t.Errorf("expected 0 chores, got %d", len(resp.Chores))
	}
}

func TestListChoresHandlerForbiddenForChild(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := ListChoresHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/chores", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestCreateChoreHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := CreateChoreHandler(db)
	body := map[string]any{
		"name":      "Clean room",
		"amount":    20.0,
		"frequency": "daily",
		"icon":      "🧹",
	}
	r := withUser(newRequest(http.MethodPost, "/api/allowance/chores", body), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var chore Chore
	decode(t, w.Body.Bytes(), &chore)
	if chore.ID == 0 {
		t.Error("expected non-zero ID in response")
	}
	if chore.Name != "Clean room" {
		t.Errorf("expected name 'Clean room', got %q", chore.Name)
	}
}

func TestCreateChoreHandlerValidation(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := CreateChoreHandler(db)

	// Missing name.
	r := withUser(newRequest(http.MethodPost, "/api/allowance/chores", map[string]any{"amount": 10}), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d", w.Code)
	}

	// Invalid frequency.
	r = withUser(newRequest(http.MethodPost, "/api/allowance/chores", map[string]any{
		"name": "Test", "frequency": "monthly",
	}), testParent)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid frequency, got %d", w.Code)
	}
}

func TestMyChoresHandlerNoLink(t *testing.T) {
	db := setupTestDB(t)
	// No family link created — child is not linked.

	handler := MyChoresHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/chores", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unlinked child, got %d", w.Code)
	}
}

func TestMyChoresHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Create a chore for any child.
	if _, err := CreateChore(db, 1, nil, "Dishes", "", 15, "daily", "🍽️", true, "solo", 2, 10.0); err != nil {
		t.Fatalf("CreateChore: %v", err)
	}

	handler := MyChoresHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/chores", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Chores []ChoreWithStatus `json:"chores"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Chores) != 1 {
		t.Errorf("expected 1 chore, got %d", len(resp.Chores))
	}
}

func TestCompleteChoreHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}

	handler := CompleteChoreHandler(db)
	body := map[string]any{"date": "2026-03-28", "notes": "All done!"}
	r := withUser(newRequest(http.MethodPost, "/api/allowance/my/complete/1", body), testChild)
	r = withChiParam(r, "id", "1")
	_ = chore

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var comp Completion
	decode(t, w.Body.Bytes(), &comp)
	if comp.Status != "pending" {
		t.Errorf("expected status pending, got %q", comp.Status)
	}
}

func TestApproveCompletionHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	comp, err := CreateCompletion(db, chore.ID, 2, "2026-03-28", "")
	if err != nil {
		t.Fatalf("CreateCompletion: %v", err)
	}

	handler := ApproveCompletionHandler(db)
	r := withUser(newRequest(http.MethodPost, "/api/allowance/approve/1", nil), testParent)
	r = withChiParam(r, "id", strconv.FormatInt(comp.ID, 10))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result Completion
	decode(t, w.Body.Bytes(), &result)
	if result.Status != "approved" {
		t.Errorf("expected status approved, got %q", result.Status)
	}
}

func TestUpsertBonusRule(t *testing.T) {
	db := setupTestDB(t)

	rule, err := UpsertBonusRule(db, 1, "full_week", 1.2, 0, true)
	if err != nil {
		t.Fatalf("UpsertBonusRule: %v", err)
	}
	if rule.Multiplier != 1.2 {
		t.Errorf("expected multiplier 1.2, got %v", rule.Multiplier)
	}

	// Upsert again — should update.
	rule2, err := UpsertBonusRule(db, 1, "full_week", 1.3, 10, true)
	if err != nil {
		t.Fatalf("UpsertBonusRule (update): %v", err)
	}
	if rule2.Multiplier != 1.3 {
		t.Errorf("expected multiplier 1.3 after update, got %v", rule2.Multiplier)
	}
	if rule2.FlatAmount != 10 {
		t.Errorf("expected flat_amount 10 after update, got %v", rule2.FlatAmount)
	}
}

func TestUpsertPayout(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	payout, err := UpsertPayout(db, 1, 2, "2026-03-24", 50, 10, 60)
	if err != nil {
		t.Fatalf("UpsertPayout: %v", err)
	}
	if payout.TotalAmount != 60 {
		t.Errorf("expected total 60, got %v", payout.TotalAmount)
	}
	if payout.PaidOut {
		t.Error("expected paid_out=false on creation")
	}

	// Update existing.
	payout2, err := UpsertPayout(db, 1, 2, "2026-03-24", 55, 15, 70)
	if err != nil {
		t.Fatalf("UpsertPayout (update): %v", err)
	}
	if payout2.TotalAmount != 70 {
		t.Errorf("expected updated total 70, got %v", payout2.TotalAmount)
	}
}

func TestMarkPayoutPaid(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	payout, err := UpsertPayout(db, 1, 2, "2026-03-24", 50, 10, 60)
	if err != nil {
		t.Fatalf("UpsertPayout: %v", err)
	}

	paid, err := MarkPayoutPaid(db, payout.ID, 1)
	if err != nil {
		t.Fatalf("MarkPayoutPaid: %v", err)
	}
	if !paid.PaidOut {
		t.Error("expected paid_out=true after marking paid")
	}
	if paid.PaidAt == nil {
		t.Error("expected paid_at to be set")
	}
}

// ---- Calculator tests ----

func TestCalculateWeeklyEarningsNoCompletions(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// No completions — only base allowance.
	if _, err := UpsertSettings(db, 1, 2, 100, 24); err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}

	earnings, err := CalculateWeeklyEarnings(db, 1, 2, "2026-03-23")
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings: %v", err)
	}
	if earnings.BaseAllowance != 100 {
		t.Errorf("expected base allowance 100, got %v", earnings.BaseAllowance)
	}
	if earnings.ChoreEarnings != 0 {
		t.Errorf("expected 0 chore earnings, got %v", earnings.ChoreEarnings)
	}
	if earnings.TotalAmount != 100 {
		t.Errorf("expected total 100, got %v", earnings.TotalAmount)
	}
}

func TestCalculateWeeklyEarningsApprovedCompletion(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := UpsertSettings(db, 1, 2, 50, 24); err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}
	chore, err := CreateChore(db, 1, nil, "Dishes", "", 20, "daily", "🍽️", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	comp, err := CreateCompletion(db, chore.ID, 2, "2026-03-23", "")
	if err != nil {
		t.Fatalf("CreateCompletion: %v", err)
	}
	if _, err := ApproveCompletion(db, comp.ID, 1); err != nil {
		t.Fatalf("ApproveCompletion: %v", err)
	}

	earnings, err := CalculateWeeklyEarnings(db, 1, 2, "2026-03-23")
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings: %v", err)
	}
	if earnings.ChoreEarnings != 20 {
		t.Errorf("expected chore earnings 20, got %v", earnings.ChoreEarnings)
	}
	if earnings.ApprovedCount != 1 {
		t.Errorf("expected approved count 1, got %d", earnings.ApprovedCount)
	}
	if earnings.TotalAmount != 70 {
		t.Errorf("expected total 70 (50 base + 20 chore), got %v", earnings.TotalAmount)
	}
}

func TestCalculateWeeklyEarningsPendingBeyondCutoff(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Set auto-approve to 0 effective hours so stale completions auto-approve.
	if _, err := UpsertSettings(db, 1, 2, 0, 1); err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}
	chore, err := CreateChore(db, 1, nil, "Vacuum", "", 15, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	// Insert completion with old created_at to simulate stale pending.
	if _, err := db.Exec(`
		INSERT INTO allowance_completions (chore_id, child_id, date, status, notes, created_at)
		VALUES (?, 2, '2026-03-23', 'pending', '', '2026-03-01T00:00:00Z')
	`, chore.ID); err != nil {
		t.Fatalf("insert stale completion: %v", err)
	}

	earnings, err := CalculateWeeklyEarnings(db, 1, 2, "2026-03-23")
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings: %v", err)
	}
	// Stale completion should have been auto-approved and counted.
	if earnings.ApprovedCount != 1 {
		t.Errorf("expected auto-approved count 1, got %d", earnings.ApprovedCount)
	}
	if earnings.ChoreEarnings != 15 {
		t.Errorf("expected chore earnings 15 after auto-approve, got %v", earnings.ChoreEarnings)
	}
}

func TestCalculateWeeklyEarningsAutoApproveChildSpecific(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add a second child linked to same parent.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'child2@test.com', 'Child2', 'gc3')`); err != nil {
		t.Fatalf("insert child2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link child2: %v", err)
	}

	// Settings: child 2 gets 1h (stale), child 3 gets 999h (not stale).
	if _, err := UpsertSettings(db, 1, 2, 0, 1); err != nil {
		t.Fatalf("UpsertSettings child2: %v", err)
	}
	if _, err := UpsertSettings(db, 1, 3, 0, 999); err != nil {
		t.Fatalf("UpsertSettings child3: %v", err)
	}

	chore, err := CreateChore(db, 1, nil, "Mop", "", 10, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	// Insert old pending completions for both children.
	for _, childID := range []int{2, 3} {
		if _, err := db.Exec(`
			INSERT INTO allowance_completions (chore_id, child_id, date, status, notes, created_at)
			VALUES (?, ?, '2026-03-23', 'pending', '', '2026-03-01T00:00:00Z')
		`, chore.ID, childID); err != nil {
			t.Fatalf("insert completion for child %d: %v", childID, err)
		}
	}

	// Calculate for child 2 — should auto-approve only child 2's completion.
	earnings, err := CalculateWeeklyEarnings(db, 1, 2, "2026-03-23")
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings child2: %v", err)
	}
	if earnings.ApprovedCount != 1 {
		t.Errorf("expected child2 approved count 1, got %d", earnings.ApprovedCount)
	}

	// Child 3's completion should still be pending (not auto-approved by child 2's calculation).
	var status string
	if err := db.QueryRow(`SELECT status FROM allowance_completions WHERE child_id = 3`).Scan(&status); err != nil {
		t.Fatalf("query child3 status: %v", err)
	}
	if status != "pending" {
		t.Errorf("expected child3 completion to remain pending, got %q", status)
	}
}

func TestCalculateWeeklyEarningsFullWeekBonus(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := UpsertSettings(db, 1, 2, 0, 24); err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}
	chore, err := CreateChore(db, 1, nil, "Exercise", "", 10, "daily", "🏃", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	// Approve one completion per day for the full week (Mon-Sun).
	weekStart := "2026-03-23"
	baseDate, _ := time.Parse("2006-01-02", weekStart)
	for i := range 7 {
		date := baseDate.AddDate(0, 0, i).Format("2006-01-02")
		comp, err := CreateCompletion(db, chore.ID, 2, date, "")
		if err != nil {
			t.Fatalf("CreateCompletion day %d: %v", i, err)
		}
		if _, err := ApproveCompletion(db, comp.ID, 1); err != nil {
			t.Fatalf("ApproveCompletion day %d: %v", i, err)
		}
	}

	// Add a full_week bonus rule: 1.5x multiplier.
	if _, err := UpsertBonusRule(db, 1, "full_week", 1.5, 0, true); err != nil {
		t.Fatalf("UpsertBonusRule: %v", err)
	}

	earnings, err := CalculateWeeklyEarnings(db, 1, 2, weekStart)
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings: %v", err)
	}
	if earnings.ApprovedCount != 7 {
		t.Errorf("expected 7 approved, got %d", earnings.ApprovedCount)
	}
	// 7 completions × 10 NOK = 70 chore earnings. Bonus = 70 * 0.5 = 35.
	if earnings.ChoreEarnings != 70 {
		t.Errorf("expected chore earnings 70, got %v", earnings.ChoreEarnings)
	}
	if earnings.BonusAmount != 35 {
		t.Errorf("expected bonus 35, got %v", earnings.BonusAmount)
	}
}

func TestQualityBonusHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, err := CreateChore(db, 1, nil, "Tidy room", "", 10, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	comp, err := CreateCompletion(db, chore.ID, 2, time.Now().Format("2006-01-02"), "")
	if err != nil {
		t.Fatalf("CreateCompletion: %v", err)
	}

	handler := QualityBonusHandler(db)

	// Success: add 5 NOK quality bonus.
	body := map[string]float64{"amount": 5}
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/quality-bonus/"+strconv.FormatInt(comp.ID, 10), body), "id", strconv.FormatInt(comp.ID, 10)), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result Completion
	decode(t, w.Body.Bytes(), &result)
	if result.QualityBonus != 5 {
		t.Errorf("expected quality_bonus 5, got %v", result.QualityBonus)
	}

	// Not found: non-existent completion.
	r2 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/quality-bonus/9999", body), "id", "9999"), testParent)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown completion, got %d", w2.Code)
	}

	// Forbidden: child cannot call this endpoint.
	r3 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/quality-bonus/"+strconv.FormatInt(comp.ID, 10), body), "id", strconv.FormatInt(comp.ID, 10)), testChild)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Code != http.StatusForbidden {
		t.Errorf("expected 403 for child, got %d", w3.Code)
	}

	// Invalid ID: non-numeric {id} param.
	r4 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/quality-bonus/abc", body), "id", "abc"), testParent)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, r4)
	if w4.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-numeric id, got %d", w4.Code)
	}

	// Invalid JSON body.
	req5, _ := http.NewRequest(http.MethodPost, "/api/allowance/quality-bonus/"+strconv.FormatInt(comp.ID, 10), strings.NewReader("not-json"))
	req5.Header.Set("Content-Type", "application/json")
	r5 := withUser(withChiParam(req5, "id", strconv.FormatInt(comp.ID, 10)), testParent)
	w5 := httptest.NewRecorder()
	handler.ServeHTTP(w5, r5)
	if w5.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w5.Code)
	}

	// Negative amount.
	negBody := map[string]float64{"amount": -1}
	r6 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/quality-bonus/"+strconv.FormatInt(comp.ID, 10), negBody), "id", strconv.FormatInt(comp.ID, 10)), testParent)
	w6 := httptest.NewRecorder()
	handler.ServeHTTP(w6, r6)
	if w6.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative amount, got %d", w6.Code)
	}
}

func TestListExtrasHandlerEmpty(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	handler := ListExtrasHandler(db)

	r := withUser(newRequest(http.MethodGet, "/api/allowance/extras", nil), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result map[string][]Extra
	decode(t, w.Body.Bytes(), &result)
	if len(result["extras"]) != 0 {
		t.Errorf("expected empty extras list, got %d", len(result["extras"]))
	}
}

func TestListExtrasHandlerForbiddenForChild(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	handler := ListExtrasHandler(db)

	r := withUser(newRequest(http.MethodGet, "/api/allowance/extras", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for child, got %d", w.Code)
	}
}

func TestCreateExtraHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	handler := CreateExtraHandler(db)

	body := map[string]any{"name": "Clean garage", "amount": 50.0}
	r := withUser(newRequest(http.MethodPost, "/api/allowance/extras", body), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var result Extra
	decode(t, w.Body.Bytes(), &result)
	if result.Name != "Clean garage" {
		t.Errorf("expected name 'Clean garage', got %q", result.Name)
	}
	if result.Amount != 50.0 {
		t.Errorf("expected amount 50, got %v", result.Amount)
	}
	if result.Status != "open" {
		t.Errorf("expected status 'open', got %q", result.Status)
	}
}

func TestCreateExtraHandlerValidation(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	handler := CreateExtraHandler(db)

	// Missing name.
	r := withUser(newRequest(http.MethodPost, "/api/allowance/extras", map[string]any{"amount": 10.0}), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d", w.Code)
	}

	// Negative amount.
	r2 := withUser(newRequest(http.MethodPost, "/api/allowance/extras", map[string]any{"name": "Task", "amount": -5.0}), testParent)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative amount, got %d", w2.Code)
	}

	// Forbidden for child.
	r3 := withUser(newRequest(http.MethodPost, "/api/allowance/extras", map[string]any{"name": "Task", "amount": 10.0}), testChild)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Code != http.StatusForbidden {
		t.Errorf("expected 403 for child, got %d", w3.Code)
	}
}

func TestClaimExtraHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Parent creates an extra.
	extra, err := CreateExtra(db, 1, nil, "Mow lawn", 30, nil)
	if err != nil {
		t.Fatalf("CreateExtra: %v", err)
	}

	handler := ClaimExtraHandler(db)

	// Child claims it.
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/claim-extra/"+strconv.FormatInt(extra.ID, 10), nil), "id", strconv.FormatInt(extra.ID, 10)), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result Extra
	decode(t, w.Body.Bytes(), &result)
	if result.Status != "claimed" {
		t.Errorf("expected status 'claimed', got %q", result.Status)
	}

	// Claim again — should be conflict.
	r2 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/claim-extra/"+strconv.FormatInt(extra.ID, 10), nil), "id", strconv.FormatInt(extra.ID, 10)), testChild)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusConflict {
		t.Errorf("expected 409 for already-claimed extra, got %d", w2.Code)
	}

	// Not found.
	r3 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/claim-extra/9999", nil), "id", "9999"), testChild)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown extra, got %d", w3.Code)
	}

	// Invalid ID.
	r4 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/claim-extra/abc", nil), "id", "abc"), testChild)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, r4)
	if w4.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid ID, got %d", w4.Code)
	}
}

func TestClaimExtraHandlerForbiddenForParent(t *testing.T) {
	db := setupTestDB(t)
	handler := ClaimExtraHandler(db)

	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/claim-extra/1", nil), "id", "1"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	// Parent is not linked as a child, so should get 403.
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for parent calling kid endpoint, got %d", w.Code)
	}
}

func TestMyExtrasHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Create an open extra for child 2.
	childID := int64(2)
	if _, err := CreateExtra(db, 1, &childID, "Water plants", 10, nil); err != nil {
		t.Fatalf("CreateExtra: %v", err)
	}

	handler := MyExtrasHandler(db)

	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/extras", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result map[string][]Extra
	decode(t, w.Body.Bytes(), &result)
	if len(result["extras"]) != 1 {
		t.Errorf("expected 1 extra, got %d", len(result["extras"]))
	}
}

func TestListBonusRulesHandlerEmpty(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	handler := ListBonusRulesHandler(db)

	r := withUser(newRequest(http.MethodGet, "/api/allowance/bonuses", nil), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result map[string][]BonusRule
	decode(t, w.Body.Bytes(), &result)
	if len(result["bonus_rules"]) != 0 {
		t.Errorf("expected empty bonus_rules, got %d", len(result["bonus_rules"]))
	}
}

func TestListBonusRulesHandlerForbiddenForChild(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	handler := ListBonusRulesHandler(db)

	r := withUser(newRequest(http.MethodGet, "/api/allowance/bonuses", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for child, got %d", w.Code)
	}
}

func TestUpdateBonusRulesHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	handler := UpdateBonusRulesHandler(db)

	active := true
	body := map[string]any{"type": "full_week", "multiplier": 1.2, "flat_amount": 0.0, "active": active}
	r := withUser(newRequest(http.MethodPut, "/api/allowance/bonuses", body), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rule BonusRule
	decode(t, w.Body.Bytes(), &rule)
	if rule.Type != "full_week" {
		t.Errorf("expected type 'full_week', got %q", rule.Type)
	}
	if rule.Multiplier != 1.2 {
		t.Errorf("expected multiplier 1.2, got %v", rule.Multiplier)
	}
	if !rule.Active {
		t.Errorf("expected active=true")
	}
}

func TestUpdateBonusRulesHandlerValidation(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	handler := UpdateBonusRulesHandler(db)

	// Invalid type.
	r := withUser(newRequest(http.MethodPut, "/api/allowance/bonuses", map[string]any{"type": "bad_type", "multiplier": 1.0}), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid type, got %d", w.Code)
	}

	// Multiplier below 1.0.
	r2 := withUser(newRequest(http.MethodPut, "/api/allowance/bonuses", map[string]any{"type": "streak", "multiplier": 0.5}), testParent)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for multiplier < 1.0, got %d", w2.Code)
	}

	// Negative flat_amount.
	r3 := withUser(newRequest(http.MethodPut, "/api/allowance/bonuses", map[string]any{"type": "early_bird", "multiplier": 1.0, "flat_amount": -1.0}), testParent)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative flat_amount, got %d", w3.Code)
	}

	// Forbidden for child.
	r4 := withUser(newRequest(http.MethodPut, "/api/allowance/bonuses", map[string]any{"type": "full_week", "multiplier": 1.2}), testChild)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, r4)
	if w4.Code != http.StatusForbidden {
		t.Errorf("expected 403 for child, got %d", w4.Code)
	}
}

func TestApproveExtraHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Parent creates an extra; child claims it.
	extra, err := CreateExtra(db, 1, nil, "Vacuum", 20, nil)
	if err != nil {
		t.Fatalf("CreateExtra: %v", err)
	}
	if _, err := ClaimExtra(db, extra.ID, 2); err != nil {
		t.Fatalf("ClaimExtra: %v", err)
	}

	handler := ApproveExtraHandler(db)

	// Parent approves the claimed extra.
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/extras/"+strconv.FormatInt(extra.ID, 10)+"/approve", nil), "id", strconv.FormatInt(extra.ID, 10)), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result Extra
	decode(t, w.Body.Bytes(), &result)
	if result.Status != "approved" {
		t.Errorf("expected status 'approved', got %q", result.Status)
	}

	// Approve again — extra is no longer in claimable state, should 404.
	r2 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/extras/"+strconv.FormatInt(extra.ID, 10)+"/approve", nil), "id", strconv.FormatInt(extra.ID, 10)), testParent)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 for already-approved extra, got %d", w2.Code)
	}

	// Not found.
	r3 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/extras/9999/approve", nil), "id", "9999"), testParent)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown extra, got %d", w3.Code)
	}

	// Invalid ID.
	r4 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/extras/abc/approve", nil), "id", "abc"), testParent)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, r4)
	if w4.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid ID, got %d", w4.Code)
	}

	// Forbidden for child.
	extra2, err := CreateExtra(db, 1, nil, "Dishes", 10, nil)
	if err != nil {
		t.Fatalf("CreateExtra: %v", err)
	}
	if _, err := ClaimExtra(db, extra2.ID, 2); err != nil {
		t.Fatalf("ClaimExtra: %v", err)
	}
	r5 := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/extras/"+strconv.FormatInt(extra2.ID, 10)+"/approve", nil), "id", strconv.FormatInt(extra2.ID, 10)), testChild)
	w5 := httptest.NewRecorder()
	handler.ServeHTTP(w5, r5)
	if w5.Code != http.StatusForbidden {
		t.Errorf("expected 403 for child calling approve, got %d", w5.Code)
	}
}

func TestCalculateWeeklyEarningsQualityBonus(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := UpsertSettings(db, 1, 2, 0, 24); err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}
	chore, err := CreateChore(db, 1, nil, "Dishes", "", 10, "daily", "🍽️", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	comp, err := CreateCompletion(db, chore.ID, 2, "2026-03-23", "")
	if err != nil {
		t.Fatalf("CreateCompletion: %v", err)
	}
	if _, err := ApproveCompletion(db, comp.ID, 1); err != nil {
		t.Fatalf("ApproveCompletion: %v", err)
	}
	// Add a quality bonus on the approved completion.
	if _, err := AddQualityBonus(db, comp.ID, 1, 5); err != nil {
		t.Fatalf("AddQualityBonus: %v", err)
	}

	earnings, err := CalculateWeeklyEarnings(db, 1, 2, "2026-03-23")
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings: %v", err)
	}
	// chore_earnings = 10 (base) + 5 (quality bonus) = 15
	if earnings.ChoreEarnings != 15 {
		t.Errorf("expected chore earnings 15 (10 base + 5 quality bonus), got %v", earnings.ChoreEarnings)
	}
	if earnings.TotalAmount != 15 {
		t.Errorf("expected total 15, got %v", earnings.TotalAmount)
	}
}

// ---- Team completion storage tests ----

// insertTeamChore inserts a team chore directly so we can set completion_mode, min_team_size,
// and team_bonus_pct without going through CreateChore (which doesn't expose those fields).
func insertTeamChore(t *testing.T, db *sql.DB, parentID int64, childID *int64, amount float64, minTeamSize int, teamBonusPct float64) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO allowance_chores
		  (parent_id, child_id, name, description, amount, currency, frequency, icon,
		   requires_approval, active, created_at, completion_mode, min_team_size, team_bonus_pct)
		VALUES (?, ?, 'Team Chore', '', ?, 'NOK', 'daily', '🤝', 1, 1, '2026-01-01T00:00:00Z',
		        'team', ?, ?)
	`, parentID, childID, amount, minTeamSize, teamBonusPct)
	if err != nil {
		t.Fatalf("insertTeamChore: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insertTeamChore LastInsertId: %v", err)
	}
	return id
}

func TestStartTeamCompletion(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}
	if comp.Status != "waiting_for_team" {
		t.Errorf("expected status waiting_for_team, got %s", comp.Status)
	}
	if comp.ChoreID != choreID {
		t.Errorf("expected chore ID %d, got %d", choreID, comp.ChoreID)
	}

	// Participant row must exist.
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM allowance_team_completions WHERE completion_id = ? AND child_id = ?`, comp.ID, 2).Scan(&cnt); err != nil {
		t.Fatalf("count participants: %v", err)
	}
	if cnt != 1 {
		t.Errorf("expected 1 team participant row, got %d", cnt)
	}
}

func TestStartTeamCompletion_Duplicate(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	if _, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28"); err != nil {
		t.Fatalf("first StartTeamCompletion: %v", err)
	}
	_, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != ErrCompletionExists {
		t.Errorf("expected ErrCompletionExists on duplicate, got %v", err)
	}
}

func TestStartTeamCompletion_NotTeamMode(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	chore, err := CreateChore(db, 1, nil, "Solo", "", 10, "daily", "🧹", true, "solo", 2, 10.0)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}
	_, err = StartTeamCompletion(db, 1, chore.ID, 2, "2026-03-28")
	if err != ErrChoreNotTeamMode {
		t.Errorf("expected ErrChoreNotTeamMode, got %v", err)
	}
}

func TestJoinTeamCompletion_PromotesToPending(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add a third user (sibling) so we have two joiners.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}
	if comp.Status != "waiting_for_team" {
		t.Errorf("expected waiting_for_team after start, got %s", comp.Status)
	}

	// Sibling joins — this should push count to 2 (>= min_team_size=2) and promote to pending.
	updated, err := JoinTeamCompletion(db, 1, comp.ID, 3)
	if err != nil {
		t.Fatalf("JoinTeamCompletion: %v", err)
	}
	if updated.Status != "pending" {
		t.Errorf("expected status pending after join, got %s", updated.Status)
	}

	// Verify DB status.
	var dbStatus string
	if err := db.QueryRow(`SELECT status FROM allowance_completions WHERE id = ?`, comp.ID).Scan(&dbStatus); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if dbStatus != "pending" {
		t.Errorf("expected DB status pending, got %s", dbStatus)
	}
}

func TestJoinTeamCompletion_AlreadyJoined(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 3, 10) // min_team_size=3 so it stays waiting

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}

	if _, err := JoinTeamCompletion(db, 1, comp.ID, 3); err != nil {
		t.Fatalf("first join: %v", err)
	}
	_, err = JoinTeamCompletion(db, 1, comp.ID, 3)
	if err != ErrAlreadyJoined {
		t.Errorf("expected ErrAlreadyJoined, got %v", err)
	}
}

func TestJoinTeamCompletion_NotFound(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	_, err := JoinTeamCompletion(db, 1, 99999, 2)
	if err != ErrCompletionNotFound {
		t.Errorf("expected ErrCompletionNotFound, got %v", err)
	}
}

func TestGetActiveTeamSessions(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 3, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}

	sessions, err := GetActiveTeamSessions(db, 2, []int64{choreID}, "2026-03-28")
	if err != nil {
		t.Fatalf("GetActiveTeamSessions: %v", err)
	}
	sess, ok := sessions[choreID]
	if !ok {
		t.Fatalf("expected session for chore %d", choreID)
	}
	if sess.CompletionID != comp.ID {
		t.Errorf("expected completion ID %d, got %d", comp.ID, sess.CompletionID)
	}
	if sess.ParticipantCount != 1 {
		t.Errorf("expected 1 participant, got %d", sess.ParticipantCount)
	}
	if !sess.CurrentChildJoined {
		t.Errorf("expected CurrentChildJoined=true for initiator")
	}

	// Sibling (ID=3) should not see CurrentChildJoined.
	sessions3, err := GetActiveTeamSessions(db, 3, []int64{choreID}, "2026-03-28")
	if err != nil {
		t.Fatalf("GetActiveTeamSessions sibling: %v", err)
	}
	if sessions3[choreID].CurrentChildJoined {
		t.Errorf("expected CurrentChildJoined=false for non-participant sibling")
	}
}

func TestGetTeamParticipantCounts(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 3, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}

	counts, err := GetTeamParticipantCounts(db, []int64{comp.ID})
	if err != nil {
		t.Fatalf("GetTeamParticipantCounts: %v", err)
	}
	if counts[comp.ID] != 1 {
		t.Errorf("expected count 1, got %d", counts[comp.ID])
	}

	// Sibling joins → count should be 2.
	if _, err := JoinTeamCompletion(db, 1, comp.ID, 3); err != nil {
		t.Fatalf("JoinTeamCompletion: %v", err)
	}
	counts2, err := GetTeamParticipantCounts(db, []int64{comp.ID})
	if err != nil {
		t.Fatalf("GetTeamParticipantCounts after join: %v", err)
	}
	if counts2[comp.ID] != 2 {
		t.Errorf("expected count 2 after join, got %d", counts2[comp.ID])
	}

	// Empty slice should return nil without error.
	empty, err := GetTeamParticipantCounts(db, nil)
	if err != nil {
		t.Fatalf("GetTeamParticipantCounts nil: %v", err)
	}
	if empty != nil {
		t.Errorf("expected nil for empty input, got %v", empty)
	}
}

func TestGetTeamParticipationsForChildInWeek(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add sibling (ID=3).
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	// Sibling (3) starts a session; child (2) joins, pushing it to pending.
	comp, err := StartTeamCompletion(db, 1, choreID, 3, "2026-03-28")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}
	if _, err := JoinTeamCompletion(db, 1, comp.ID, 2); err != nil {
		t.Fatalf("JoinTeamCompletion: %v", err)
	}

	// Approve the completion so it shows up in earnings.
	if _, err := ApproveCompletion(db, comp.ID, 1); err != nil {
		t.Fatalf("ApproveCompletion: %v", err)
	}

	// Child (2) is a joiner (not initiator) — should appear in participations.
	participations, err := GetTeamParticipationsForChildInWeek(db, 2, "2026-03-24", "2026-03-30")
	if err != nil {
		t.Fatalf("GetTeamParticipationsForChildInWeek: %v", err)
	}
	if len(participations) != 1 {
		t.Fatalf("expected 1 participation, got %d", len(participations))
	}
	if participations[0].ChoreID != choreID {
		t.Errorf("expected chore ID %d, got %d", choreID, participations[0].ChoreID)
	}

	// Sibling (3) is the initiator — should NOT appear as a joiner.
	siblingParts, err := GetTeamParticipationsForChildInWeek(db, 3, "2026-03-24", "2026-03-30")
	if err != nil {
		t.Fatalf("GetTeamParticipationsForChildInWeek sibling: %v", err)
	}
	if len(siblingParts) != 0 {
		t.Errorf("expected 0 participations for initiator, got %d", len(siblingParts))
	}
}

// ---- Team handler tests ----

func TestTeamStartHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	handler := TeamStartHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/team-start/"+strconv.FormatInt(choreID, 10), map[string]string{"date": "2026-03-28"}), "chore_id", strconv.FormatInt(choreID, 10)), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var comp Completion
	decode(t, w.Body.Bytes(), &comp)
	if comp.Status != "waiting_for_team" {
		t.Errorf("expected status waiting_for_team, got %s", comp.Status)
	}
}

func TestTeamStartHandler_Duplicate(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	if _, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28"); err != nil {
		t.Fatalf("pre-create: %v", err)
	}

	handler := TeamStartHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/team-start/"+strconv.FormatInt(choreID, 10), map[string]string{"date": "2026-03-28"}), "chore_id", strconv.FormatInt(choreID, 10)), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTeamStartHandler_InvalidChoreID(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := TeamStartHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/team-start/notanid", nil), "chore_id", "notanid"), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestTeamJoinHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add sibling.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}
	sibling := &auth.User{ID: 3, Email: "sibling@test.com", Name: "Sibling"}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}

	handler := TeamJoinHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/team-join/"+strconv.FormatInt(comp.ID, 10), nil), "completion_id", strconv.FormatInt(comp.ID, 10)), sibling)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated Completion
	decode(t, w.Body.Bytes(), &updated)
	// min_team_size=2 and initiator already counted → promoted to pending.
	if updated.Status != "pending" {
		t.Errorf("expected status pending after join, got %s", updated.Status)
	}
}

func TestTeamJoinHandler_AlreadyJoined(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}
	sibling := &auth.User{ID: 3, Email: "sibling@test.com", Name: "Sibling"}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 3, 10) // min_team_size=3 keeps it waiting

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}

	handler := TeamJoinHandler(db)
	makeReq := func() *http.Request {
		return withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/team-join/"+strconv.FormatInt(comp.ID, 10), nil), "completion_id", strconv.FormatInt(comp.ID, 10)), sibling)
	}

	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, makeReq())
	if w1.Code != http.StatusOK {
		t.Fatalf("first join expected 200, got %d", w1.Code)
	}

	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, makeReq())
	if w2.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate join, got %d", w2.Code)
	}
}

func TestTeamJoinHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := TeamJoinHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/team-join/99999", nil), "completion_id", "99999"), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestCancelTeamCompletion(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("start team: %v", err)
	}

	if err := CancelTeamCompletion(db, 1, comp.ID, 2); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// Verify completion is deleted.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM allowance_completions WHERE id = ?`, comp.ID).Scan(&count); err != nil {
		t.Fatalf("count completions: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 completions, got %d", count)
	}

	// Verify team entries are deleted.
	if err := db.QueryRow(`SELECT COUNT(*) FROM allowance_team_completions WHERE completion_id = ?`, comp.ID).Scan(&count); err != nil {
		t.Fatalf("count team: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 team entries, got %d", count)
	}
}

func TestCancelTeamCompletion_NotInitiator(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	// Add sibling.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-03-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("start team: %v", err)
	}

	// Sibling (child_id=3) tries to cancel — should be rejected.
	err = CancelTeamCompletion(db, 1, comp.ID, 3)
	if !errors.Is(err, ErrNotSessionInitiator) {
		t.Errorf("expected ErrNotSessionInitiator, got %v", err)
	}
}

func TestCancelTeamCompletion_NotWaiting(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-03-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("start team: %v", err)
	}
	// Join to promote to pending.
	if _, err := JoinTeamCompletion(db, 1, comp.ID, 3); err != nil {
		t.Fatalf("join: %v", err)
	}

	err = CancelTeamCompletion(db, 1, comp.ID, 2)
	if !errors.Is(err, ErrSessionNotWaiting) {
		t.Errorf("expected ErrSessionNotWaiting, got %v", err)
	}
}

func TestTeamCancelHandler(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("start team: %v", err)
	}

	handler := TeamCancelHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/team-cancel/"+strconv.FormatInt(comp.ID, 10), nil), "completion_id", strconv.FormatInt(comp.ID, 10)), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTeamCancelHandler_Forbidden(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-03-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-28")
	if err != nil {
		t.Fatalf("start team: %v", err)
	}

	// Sibling tries to cancel via handler.
	siblingUser := &auth.User{ID: 3, Email: "sibling@test.com", Name: "Sibling"}
	handler := TeamCancelHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/team-cancel/"+strconv.FormatInt(comp.ID, 10), nil), "completion_id", strconv.FormatInt(comp.ID, 10)), siblingUser)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTeamCancelHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := TeamCancelHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/allowance/my/team-cancel/99999", nil), "completion_id", "99999"), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ---- Calculator tests for team bonus and joiner earnings ----

// TestCalculateWeeklyEarnings_TeamBonus verifies that when an initiator's completion has
// team participants, team_bonus_pct is applied to the base chore amount.
func TestCalculateWeeklyEarnings_TeamBonus(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add sibling so we can join and reach min_team_size.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-03-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	if _, err := UpsertSettings(db, 1, 2, 0, 24); err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}

	// Team chore: amount=20, min_team_size=2, team_bonus_pct=10 → bonus amount = 20 * 1.10 = 22.
	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	// Child 2 starts; sibling 3 joins → promoted to pending.
	comp, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-24")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}
	if _, err := JoinTeamCompletion(db, 1, comp.ID, 3); err != nil {
		t.Fatalf("JoinTeamCompletion: %v", err)
	}

	// Approve so it counts for earnings.
	if _, err := ApproveCompletion(db, comp.ID, 1); err != nil {
		t.Fatalf("ApproveCompletion: %v", err)
	}

	earnings, err := CalculateWeeklyEarnings(db, 1, 2, "2026-03-24")
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings: %v", err)
	}

	// Initiator should earn base * (1 + 10/100) = 22.
	want := 20.0 * 1.10
	if earnings.ChoreEarnings != want {
		t.Errorf("expected team-bonus chore earnings %.2f, got %.2f", want, earnings.ChoreEarnings)
	}
	if earnings.ApprovedCount != 1 {
		t.Errorf("expected approved count 1, got %d", earnings.ApprovedCount)
	}
}

// TestCalculateWeeklyEarnings_JoinerEarnings verifies that a child who joined (but did not
// initiate) a team completion earns the team-bonus amount without double-counting.
func TestCalculateWeeklyEarnings_JoinerEarnings(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add sibling (ID=3) who will be the initiator; child 2 will be the joiner.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-03-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	if _, err := UpsertSettings(db, 1, 2, 0, 24); err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}

	// Team chore: amount=20, min_team_size=2, team_bonus_pct=10 → joiner earns 20 * 1.10 = 22.
	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	// Sibling 3 starts; child 2 joins → promoted to pending.
	comp, err := StartTeamCompletion(db, 1, choreID, 3, "2026-03-24")
	if err != nil {
		t.Fatalf("StartTeamCompletion: %v", err)
	}
	if _, err := JoinTeamCompletion(db, 1, comp.ID, 2); err != nil {
		t.Fatalf("JoinTeamCompletion: %v", err)
	}

	// Approve so it counts for earnings.
	if _, err := ApproveCompletion(db, comp.ID, 1); err != nil {
		t.Fatalf("ApproveCompletion: %v", err)
	}

	// Child 2 is the joiner — earnings come from GetTeamParticipationsForChildInWeek.
	earnings, err := CalculateWeeklyEarnings(db, 1, 2, "2026-03-24")
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings: %v", err)
	}

	// Joiner earns base * (1 + team_bonus_pct/100) = 20 * 1.10 = 22.
	want := 20.0 * 1.10
	if earnings.ChoreEarnings != want {
		t.Errorf("expected joiner chore earnings %.2f, got %.2f", want, earnings.ChoreEarnings)
	}
	// ApprovedCount should be 1 (not double-counted as own completion AND joiner).
	if earnings.ApprovedCount != 1 {
		t.Errorf("expected approved count 1 (no double-count), got %d", earnings.ApprovedCount)
	}
}

// TestStartTeamCompletion_OneSessionPerChoreDate verifies that a second child cannot start
// a new waiting_for_team session for the same chore+date when one already exists.
func TestStartTeamCompletion_OneSessionPerChoreDate(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add sibling.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-03-01T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	// Child 2 starts a session.
	if _, err := StartTeamCompletion(db, 1, choreID, 2, "2026-03-24"); err != nil {
		t.Fatalf("first StartTeamCompletion: %v", err)
	}

	// Sibling 3 tries to start a different session for the same chore+date.
	_, err := StartTeamCompletion(db, 1, choreID, 3, "2026-03-24")
	if err != ErrCompletionExists {
		t.Errorf("expected ErrCompletionExists when second child starts session for same chore+date, got %v", err)
	}
}

// TestCompleteChoreHandler_TeamChoreSolo verifies that a child can solo-complete a team
// chore via the regular complete endpoint. The test asserts that the request succeeds
// (201 Created) and returns a pending completion.
func TestCompleteChoreHandler_TeamChoreSolo(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 25, 2, 15)

	handler := CompleteChoreHandler(db)
	body := map[string]any{"date": "2026-03-28"}
	r := withUser(newRequest(http.MethodPost, "/api/allowance/my/complete/"+strconv.FormatInt(choreID, 10), body), testChild)
	r = withChiParam(r, "id", strconv.FormatInt(choreID, 10))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for solo completion of team chore, got %d: %s", w.Code, w.Body.String())
	}

	var comp Completion
	decode(t, w.Body.Bytes(), &comp)
	if comp.Status != "pending" {
		t.Errorf("expected status pending, got %q", comp.Status)
	}
}

// TestCalculateWeeklyEarnings_TeamChoreSoloNoBonus verifies that a child who solo-completes
// a team chore earns only the base amount (no team bonus).
func TestCalculateWeeklyEarnings_TeamChoreSoloNoBonus(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := UpsertSettings(db, 1, 2, 0, 24); err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}

	// Team chore: amount=20, min_team_size=2, team_bonus_pct=10.
	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	// Child completes solo via CreateCompletion (no team session, no team_completions rows).
	comp, err := CreateCompletion(db, choreID, 2, "2026-03-24", "")
	if err != nil {
		t.Fatalf("CreateCompletion: %v", err)
	}
	if _, err := ApproveCompletion(db, comp.ID, 1); err != nil {
		t.Fatalf("ApproveCompletion: %v", err)
	}

	earnings, err := CalculateWeeklyEarnings(db, 1, 2, "2026-03-24")
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings: %v", err)
	}

	// Solo completion of team chore: base amount only, no team bonus.
	want := 20.0
	if earnings.ChoreEarnings != want {
		t.Errorf("expected solo team-chore earnings %.2f (no bonus), got %.2f", want, earnings.ChoreEarnings)
	}
	if earnings.ApprovedCount != 1 {
		t.Errorf("expected approved count 1, got %d", earnings.ApprovedCount)
	}
}

// ---- MyBingoHandler tests ----

func TestMyBingoHandlerNoLink(t *testing.T) {
	db := setupTestDB(t)
	// Child 2 has no family_links row → requireChild should return 403.

	handler := MyBingoHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/bingo", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when no family link, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMyBingoHandlerInvalidWeek(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := MyBingoHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/bingo?week=not-a-date", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid week, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMyBingoHandlerDefaultWeek(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	now := time.Now().UTC()
	handler := MyBingoHandler(db)
	// No ?week param — should default to current UTC Monday.
	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/bingo", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var card AllowanceBingoCard
	decode(t, w.Body.Bytes(), &card)
	if card.ChildID != 2 {
		t.Errorf("expected child_id=2, got %d", card.ChildID)
	}
	if len(card.Cells) != 9 {
		t.Errorf("expected 9 cells on a 3x3 bingo card, got %d", len(card.Cells))
	}
	expectedWeek := MondayOf(now)
	if card.WeekStart != expectedWeek {
		t.Errorf("expected week_start=%q, got %q", expectedWeek, card.WeekStart)
	}
}

func TestMyBingoHandlerExplicitWeek(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := MyBingoHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/bingo?week=2026-03-23", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var card AllowanceBingoCard
	decode(t, w.Body.Bytes(), &card)
	if card.WeekStart != "2026-03-23" {
		t.Errorf("expected week_start=2026-03-23, got %q", card.WeekStart)
	}
	if len(card.Cells) != 9 {
		t.Errorf("expected 9 cells, got %d", len(card.Cells))
	}
}

// ---- MySiblingsHandler tests ----

func TestMySiblingsHandler_DecryptsNickname(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add sibling (ID=3) with an encrypted nickname.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling user: %v", err)
	}
	encNickname, err := encryption.EncryptField("Lillesøster")
	if err != nil {
		t.Fatalf("encrypt nickname: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 3, ?, '🌸', '2026-01-02T00:00:00Z')`, encNickname); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	handler := MySiblingsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/siblings", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	type siblingInfo struct {
		ChildID     int64  `json:"child_id"`
		Nickname    string `json:"nickname"`
		AvatarEmoji string `json:"avatar_emoji"`
	}
	var siblings []siblingInfo
	decode(t, w.Body.Bytes(), &siblings)

	if len(siblings) != 1 {
		t.Fatalf("expected 1 sibling, got %d", len(siblings))
	}
	if siblings[0].Nickname != "Lillesøster" {
		t.Errorf("expected decrypted nickname 'Lillesøster', got %q", siblings[0].Nickname)
	}
	if siblings[0].AvatarEmoji != "🌸" {
		t.Errorf("expected avatar_emoji '🌸', got %q", siblings[0].AvatarEmoji)
	}
	if siblings[0].ChildID != 3 {
		t.Errorf("expected child_id 3, got %d", siblings[0].ChildID)
	}
}

func TestMySiblingsHandler_LegacyPlaintextNickname(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add sibling (ID=3) with a legacy plaintext nickname (not encrypted).
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("insert sibling user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 3, 'Storebror', '⭐', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("link sibling: %v", err)
	}

	handler := MySiblingsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/siblings", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	type siblingInfo struct {
		ChildID     int64  `json:"child_id"`
		Nickname    string `json:"nickname"`
		AvatarEmoji string `json:"avatar_emoji"`
	}
	var siblings []siblingInfo
	decode(t, w.Body.Bytes(), &siblings)

	if len(siblings) != 1 {
		t.Fatalf("expected 1 sibling, got %d", len(siblings))
	}
	if siblings[0].Nickname != "Storebror" {
		t.Errorf("expected plaintext nickname 'Storebror', got %q", siblings[0].Nickname)
	}
}

func TestMySiblingsHandler_NoSiblings(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	handler := MySiblingsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/allowance/my/siblings", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	type siblingInfo struct {
		ChildID     int64  `json:"child_id"`
		Nickname    string `json:"nickname"`
		AvatarEmoji string `json:"avatar_emoji"`
	}
	var siblings []siblingInfo
	decode(t, w.Body.Bytes(), &siblings)
	if len(siblings) != 0 {
		t.Errorf("expected 0 siblings, got %d", len(siblings))
	}
}

