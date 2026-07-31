package suggestions

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func insertForCleanup(t *testing.T, d *sql.DB, s Suggestion) int64 {
	t.Helper()
	if s.UserID == 0 {
		s.UserID = 1
	}
	if s.Source == "" {
		s.Source = SourceClaude
	}
	if s.Type == "" {
		s.Type = TypeImprovement
	}
	if s.Size == "" {
		s.Size = SizeS
	}
	if s.Status == "" {
		s.Status = StatusPending
	}
	if s.Body == "" {
		s.Body = "body"
	}
	id, err := Insert(context.Background(), d, s)
	if err != nil {
		t.Fatalf("insert suggestion: %v", err)
	}
	return id
}

func statusOf(t *testing.T, d *sql.DB, id int64) (string, bool) {
	t.Helper()
	var status string
	err := d.QueryRow("SELECT status FROM suggestions WHERE id = ?", id).Scan(&status)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		t.Fatalf("query status: %v", err)
	}
	return status, true
}

func TestCleanupExpiresOldClaudePendingOnly(t *testing.T) {
	d := setupTestDB(t)
	old := time.Now().UTC().AddDate(0, 0, -(PendingMaxAgeDays + 5))

	oldClaude := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "Old claude", GeneratedAt: old})
	oldUser := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "Old user", GeneratedAt: old, Source: SourceUser})
	oldPlanned := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "Old planned", GeneratedAt: old, Status: StatusPlanned})
	fresh := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "Fresh claude"})

	res, err := CleanupPending(context.Background(), d, 1)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if res.ExpiredPending != 1 {
		t.Errorf("expired pending = %d, want 1", res.ExpiredPending)
	}
	if _, ok := statusOf(t, d, oldClaude); ok {
		t.Error("old claude pending suggestion survived expiry")
	}
	for _, id := range []int64{oldUser, oldPlanned, fresh} {
		if _, ok := statusOf(t, d, id); !ok {
			t.Errorf("suggestion %d was deleted but should be kept", id)
		}
	}
}

func TestCleanupExpiresOldRejected(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	// Rejected long ago (rejected_at old).
	oldRejected := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "Old rejected"})
	if err := MarkRejected(ctx, d, oldRejected); err != nil {
		t.Fatalf("reject: %v", err)
	}
	oldStamp := time.Now().UTC().AddDate(0, 0, -(RejectedMaxAgeDays + 1)).Format(time.RFC3339)
	if _, err := d.Exec("UPDATE suggestions SET rejected_at = ? WHERE id = ?", oldStamp, oldRejected); err != nil {
		t.Fatalf("backdate rejected_at: %v", err)
	}

	// Rejected recently — kept.
	freshRejected := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "Fresh rejected"})
	if err := MarkRejected(ctx, d, freshRejected); err != nil {
		t.Fatalf("reject: %v", err)
	}

	res, err := CleanupPending(ctx, d, 1)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if res.ExpiredRejected != 1 {
		t.Errorf("expired rejected = %d, want 1", res.ExpiredRejected)
	}
	if _, ok := statusOf(t, d, oldRejected); ok {
		t.Error("old rejected suggestion survived expiry")
	}
	if _, ok := statusOf(t, d, freshRejected); !ok {
		t.Error("recently rejected suggestion was deleted")
	}
}

func TestCleanupDedupesPendingKeepingNewest(t *testing.T) {
	d := setupTestDB(t)

	first := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "Add radar view"})
	second := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "  add RADAR view "})
	// Same title on another page is not a duplicate.
	otherPage := insertForCleanup(t, d, Suggestion{PageSlug: "budget", Title: "Add radar view"})

	res, err := CleanupPending(context.Background(), d, 1)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if res.Duplicates != 1 {
		t.Errorf("duplicates = %d, want 1", res.Duplicates)
	}
	if _, ok := statusOf(t, d, first); ok {
		t.Error("older duplicate survived dedupe")
	}
	for _, id := range []int64{second, otherPage} {
		if _, ok := statusOf(t, d, id); !ok {
			t.Errorf("suggestion %d was deleted but should be kept", id)
		}
	}
}

func TestCleanupRemovesPendingSupersededByPlanned(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	planned := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "Cache forecasts", Status: StatusPlanned})
	pendingDup := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "cache forecasts"})
	unrelated := insertForCleanup(t, d, Suggestion{PageSlug: "weather", Title: "Something else"})

	res, err := CleanupPending(ctx, d, 1)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if res.Superseded != 1 {
		t.Errorf("superseded = %d, want 1", res.Superseded)
	}
	if _, ok := statusOf(t, d, pendingDup); ok {
		t.Error("pending duplicate of a planned suggestion survived")
	}
	if st, ok := statusOf(t, d, planned); !ok || st != StatusPlanned {
		t.Errorf("planned suggestion state = %q/%v, want planned/kept", st, ok)
	}
	if _, ok := statusOf(t, d, unrelated); !ok {
		t.Error("unrelated pending suggestion was deleted")
	}
}

func TestCleanupScopedToUser(t *testing.T) {
	d := setupTestDB(t)
	if _, err := d.Exec(`INSERT INTO users (id, google_id, email, name, picture, is_admin) VALUES (2, 'g2', 'b@example.com', 'B', '', 0)`); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	old := time.Now().UTC().AddDate(0, 0, -(PendingMaxAgeDays + 5))
	otherUsers := insertForCleanup(t, d, Suggestion{UserID: 2, PageSlug: "weather", Title: "Old other user", GeneratedAt: old})

	res, err := CleanupPending(context.Background(), d, 1)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if res.Total() != 0 {
		t.Errorf("cleanup removed %d rows for the wrong user", res.Total())
	}
	if _, ok := statusOf(t, d, otherUsers); !ok {
		t.Error("other user's suggestion was deleted")
	}
}
