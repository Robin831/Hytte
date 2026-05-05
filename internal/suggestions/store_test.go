package suggestions

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

// setupTestDB returns a fresh in-memory database with a single admin user and
// pins the encryption key so EncryptField/DecryptField produce stable, hermetic
// output. Without t.Setenv("ENCRYPTION_KEY", ...) tests would either share the
// developer's auto-generated key file (non-hermetic, fails on a fresh checkout)
// or fail outright in CI where the user config dir may not be writable.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-for-suggestions-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	t.Cleanup(func() { d.Close() })

	if _, err := d.Exec(
		`INSERT INTO users (id, google_id, email, name, picture, is_admin) VALUES (1, 'g1', 'admin@example.com', 'Admin', '', 1)`,
	); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return d
}

func TestInsertEncryptsBodyAndRoundTrips(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	body := "This is the suggestion body that must be stored encrypted at rest."
	feedback := "I disagree with the framing here."
	plan := "Step 1: refactor X. Step 2: add tests."

	id, err := Insert(ctx, d, Suggestion{
		UserID:   1,
		PageSlug: "weather",
		Source:   SourceClaude,
		Type:     TypeImprovement,
		Size:     SizeS,
		Title:    "Tighten forecast cache",
		Body:     body,
		Feedback: feedback,
		Plan:     plan,
		Status:   StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	// Verify the raw column does NOT contain plaintext.
	var rawBody, rawFeedback, rawPlan sql.NullString
	if err := d.QueryRow(`SELECT body_enc, feedback_enc, plan_enc FROM suggestions WHERE id = ?`, id).
		Scan(&rawBody, &rawFeedback, &rawPlan); err != nil {
		t.Fatalf("read raw row: %v", err)
	}
	if !rawBody.Valid || rawBody.String == body {
		t.Fatalf("expected encrypted body, got raw plaintext: %q", rawBody.String)
	}
	if !strings.HasPrefix(rawBody.String, "enc:") {
		t.Fatalf("expected body to be encrypted with enc: prefix, got %q", rawBody.String)
	}
	if !rawFeedback.Valid || rawFeedback.String == feedback {
		t.Fatalf("expected encrypted feedback, got raw plaintext: %q", rawFeedback.String)
	}
	if !rawPlan.Valid || rawPlan.String == plan {
		t.Fatalf("expected encrypted plan, got raw plaintext: %q", rawPlan.String)
	}

	// GetByID should round-trip the plaintext.
	got, err := GetByID(ctx, d, id)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Body != body {
		t.Fatalf("body round-trip failed: got %q want %q", got.Body, body)
	}
	if got.Feedback != feedback {
		t.Fatalf("feedback round-trip failed: got %q want %q", got.Feedback, feedback)
	}
	if got.Plan != plan {
		t.Fatalf("plan round-trip failed: got %q want %q", got.Plan, plan)
	}
	if got.Title != "Tighten forecast cache" {
		t.Fatalf("title mismatch: %q", got.Title)
	}
	if got.Status != StatusPending {
		t.Fatalf("status mismatch: %q", got.Status)
	}
}

func TestListByStatusOnlyReturnsMatchingRows(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	// Insert a mix of statuses.
	if _, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "A", Body: "b", Status: StatusPending,
	}); err != nil {
		t.Fatalf("insert A: %v", err)
	}
	if _, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeBugfix, Size: SizeM, Title: "B", Body: "b", Status: StatusRejected,
	}); err != nil {
		t.Fatalf("insert B: %v", err)
	}
	if _, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "notes", Source: SourceClaude,
		Type: TypeAddition, Size: SizeL, Title: "C", Body: "b", Status: StatusPending,
	}); err != nil {
		t.Fatalf("insert C: %v", err)
	}

	pending, err := ListByStatus(ctx, d, 1, StatusPending)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	rejected, err := ListByStatus(ctx, d, 1, StatusRejected)
	if err != nil {
		t.Fatalf("list rejected: %v", err)
	}
	if len(rejected) != 1 || rejected[0].Title != "B" {
		t.Fatalf("expected single rejected B, got %+v", rejected)
	}
}

func TestMarkRejectedUpdatesStatusAndTimestamp(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	id, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := MarkRejected(ctx, d, id); err != nil {
		t.Fatalf("mark rejected: %v", err)
	}
	got, err := GetByID(ctx, d, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusRejected {
		t.Fatalf("status: got %q want %q", got.Status, StatusRejected)
	}
	if got.RejectedAt == nil || got.RejectedAt.IsZero() {
		t.Fatalf("rejected_at not set")
	}
}

func TestMarkPlannedStoresEncryptedPlan(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	id, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	plan := "Detailed plan: do A, then B, then C."
	if err := MarkPlanned(ctx, d, id, plan); err != nil {
		t.Fatalf("mark planned: %v", err)
	}

	var rawPlan sql.NullString
	if err := d.QueryRow(`SELECT plan_enc FROM suggestions WHERE id = ?`, id).Scan(&rawPlan); err != nil {
		t.Fatalf("read raw plan: %v", err)
	}
	if !rawPlan.Valid || rawPlan.String == plan || !strings.HasPrefix(rawPlan.String, "enc:") {
		t.Fatalf("plan not encrypted at rest: %q", rawPlan.String)
	}

	got, err := GetByID(ctx, d, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Plan != plan {
		t.Fatalf("plan round-trip: got %q want %q", got.Plan, plan)
	}
	if got.Status != StatusPlanned {
		t.Fatalf("status: %q", got.Status)
	}
	if got.PlannedAt == nil {
		t.Fatalf("planned_at not set")
	}
}

func TestMarkBeadCreatedRecordsBeadID(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	id, err := Insert(ctx, d, Suggestion{
		UserID: 1, PageSlug: "weather", Source: SourceClaude,
		Type: TypeImprovement, Size: SizeS, Title: "X", Body: "b", Status: StatusPlanned,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := MarkBeadCreated(ctx, d, id, "Hytte-abcd"); err != nil {
		t.Fatalf("mark bead created: %v", err)
	}
	got, err := GetByID(ctx, d, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusBeadCreated {
		t.Fatalf("status: %q", got.Status)
	}
	if got.BeadID != "Hytte-abcd" {
		t.Fatalf("bead id: %q", got.BeadID)
	}
	if got.BeadCreatedAt == nil {
		t.Fatalf("bead_created_at not set")
	}
}

func TestRecentForPageIncludesPendingExcludesRejected(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	insertWithGenerated := func(slug, status string, when time.Time) {
		t.Helper()
		if _, err := Insert(ctx, d, Suggestion{
			UserID: 1, GeneratedAt: when, PageSlug: slug, Source: SourceClaude,
			Type: TypeImprovement, Size: SizeS, Title: "T-" + status, Body: "b", Status: status,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	insertWithGenerated("weather", StatusPending, now)
	insertWithGenerated("weather", StatusRejected, now)
	insertWithGenerated("weather", StatusPlanned, now)
	insertWithGenerated("weather", StatusBeadCreated, now)
	// Out of window — should be excluded.
	insertWithGenerated("weather", StatusPending, now.AddDate(0, 0, -30))
	// Different page — excluded.
	insertWithGenerated("notes", StatusPending, now)

	got, err := recentForPage(ctx, d, 1, "weather", 14)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 recent (pending/planned/bead_created within 14d), got %d", len(got))
	}
	for _, s := range got {
		if s.Status == StatusRejected {
			t.Fatalf("rejected should be excluded, got %+v", s)
		}
	}
}
