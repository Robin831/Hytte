package allowance

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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
		created_at        TEXT NOT NULL DEFAULT ''
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

func decode(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, body)
	}
}

// ---- Storage tests ----

func TestCreateAndGetChore(t *testing.T) {
	db := setupTestDB(t)

	chore, err := CreateChore(db, 1, nil, "Clean room", "Tidy everything", 20, "daily", "🧹", true)
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

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true)
	if err != nil {
		t.Fatalf("CreateChore: %v", err)
	}

	updated, err := UpdateChore(db, chore.ID, 1, nil, "Clean room updated", "", 25, "weekly", "🏠", false, true)
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

	chore, err := CreateChore(db, 1, nil, "Dishes", "", 15, "daily", "🍽️", true)
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

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true)
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

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true)
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

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true)
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

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true)
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
	if _, err := CreateChore(db, 1, nil, "Dishes", "", 15, "daily", "🍽️", true); err != nil {
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

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true)
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

	chore, err := CreateChore(db, 1, nil, "Clean room", "", 20, "daily", "🧹", true)
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
	chore, err := CreateChore(db, 1, nil, "Dishes", "", 20, "daily", "🍽️", true)
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
	chore, err := CreateChore(db, 1, nil, "Vacuum", "", 15, "daily", "🧹", true)
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

	chore, err := CreateChore(db, 1, nil, "Mop", "", 10, "daily", "🧹", true)
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
	chore, err := CreateChore(db, 1, nil, "Exercise", "", 10, "daily", "🏃", true)
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

	chore, err := CreateChore(db, 1, nil, "Tidy room", "", 10, "daily", "🧹", true)
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

func TestCalculateWeeklyEarningsQualityBonus(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	if _, err := UpsertSettings(db, 1, 2, 0, 24); err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}
	chore, err := CreateChore(db, 1, nil, "Dishes", "", 10, "daily", "🍽️", true)
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

