package suggestions

import (
	"context"
	"testing"
)

// TestPendingCountForPageOnlyCountsPending verifies that PendingCountForPage
// returns only rows whose status is "pending" — planned, rejected, and
// bead_created rows must be excluded so that triaging a suggestion frees a
// rotation slot for the page.
func TestPendingCountForPageOnlyCountsPending(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	insert := func(slug, status string) {
		t.Helper()
		if _, err := Insert(ctx, d, Suggestion{
			UserID: 1, PageSlug: slug, Source: SourceClaude,
			Type: TypeImprovement, Size: SizeS, Title: "T-" + status, Body: "b", Status: status,
		}); err != nil {
			t.Fatalf("insert %s/%s: %v", slug, status, err)
		}
	}

	insert("weather", StatusPending)
	insert("weather", StatusPending)
	insert("weather", StatusRejected)
	insert("weather", StatusPlanned)
	insert("weather", StatusBeadCreated)
	// Different page should not contribute.
	insert("notes", StatusPending)

	got, err := PendingCountForPage(ctx, d, 1, "weather")
	if err != nil {
		t.Fatalf("PendingCountForPage: %v", err)
	}
	if got != 2 {
		t.Fatalf("expected 2 pending for weather (rejected/planned/bead_created excluded), got %d", got)
	}

	other, err := PendingCountForPage(ctx, d, 1, "notes")
	if err != nil {
		t.Fatalf("PendingCountForPage notes: %v", err)
	}
	if other != 1 {
		t.Fatalf("expected 1 pending for notes, got %d", other)
	}
}

// TestPendingCountForPageZeroForUnknownPage confirms a slug with no rows
// returns 0 rather than a sql.ErrNoRows error — COUNT(*) always produces a row.
func TestPendingCountForPageZeroForUnknownPage(t *testing.T) {
	d := setupTestDB(t)
	got, err := PendingCountForPage(context.Background(), d, 1, "no-such-page")
	if err != nil {
		t.Fatalf("PendingCountForPage: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected 0 for empty page, got %d", got)
	}
}

// TestPendingCountForPageScopedByUser ensures the user_id filter is honoured;
// counting page X for user A must not return user B's pending rows.
func TestPendingCountForPageScopedByUser(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	if _, err := d.Exec(
		`INSERT INTO users (id, google_id, email, name, picture, is_admin) VALUES (2, 'g2', 'b@example.com', 'B', '', 1)`,
	); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	for _, uid := range []int64{1, 2} {
		if _, err := Insert(ctx, d, Suggestion{
			UserID: uid, PageSlug: "weather", Source: SourceClaude,
			Type: TypeImprovement, Size: SizeS, Title: "T", Body: "b", Status: StatusPending,
		}); err != nil {
			t.Fatalf("insert for user %d: %v", uid, err)
		}
	}

	got, err := PendingCountForPage(ctx, d, 1, "weather")
	if err != nil {
		t.Fatalf("PendingCountForPage: %v", err)
	}
	if got != 1 {
		t.Fatalf("expected 1 for user 1 only, got %d", got)
	}
}
