package suggestions

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// RotationDefaultN is the default number of pages selected per nightly
// rotation. With ~35 eligible pages in the registry, picking 20 cycles
// through the full set every two nights. Note: 20 × PerPageTimeout (240s)
// exceeds OverallRunTimeout (30 min) in the absolute worst case; the
// overall timeout is a safety bound, not a per-page completion guarantee.
// Tune downward if cost-per-run becomes a problem after observing real spend.
const RotationDefaultN = 20

// MaxPendingPerPage caps the number of pending (un-triaged) suggestions any
// one page may have. The generator counts only status='pending' rows and asks
// Claude for exactly the deficit, so a page already at the cap is skipped
// entirely without spending API budget. Planned, bead_created, and rejected
// rows do not count — those have been acted on, freeing the slot.
const MaxPendingPerPage = 3

// FilterUnderCap drops any page from pages whose pending-suggestion count for
// userID is at or above cap. The relative order of the remaining pages is
// preserved so the caller can compose this with PickRotation without losing
// staleness ordering. cap <= 0 disables filtering and returns the input as-is.
//
// A page-level error from PendingCountForPage aborts the whole call so the
// caller can decide whether to retry; partially filtering on a transient DB
// error would silently shrink the rotation.
func FilterUnderCap(ctx context.Context, db *sql.DB, userID int64, pages []Page, cap int) ([]Page, error) {
	if cap <= 0 || len(pages) == 0 {
		return pages, nil
	}
	out := make([]Page, 0, len(pages))
	for _, p := range pages {
		n, err := PendingCountForPage(ctx, db, userID, p.Slug)
		if err != nil {
			return nil, fmt.Errorf("pending count for %q: %w", p.Slug, err)
		}
		if n >= cap {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// PickRotation selects up to n pages from eligible, ordered so that the most
// stale pages run first. The ordering is:
//  1. Pages with no prior suggestions, alphabetical by slug.
//  2. Remaining pages ascending by their most recent generated_at (oldest
//     first), tie-broken by slug.
//
// If n <= 0 or n >= len(eligible), the full sorted slice is returned. The
// returned slice is a fresh copy; the caller may mutate it freely.
func PickRotation(ctx context.Context, db *sql.DB, eligible []Page, n int) ([]Page, error) {
	if len(eligible) == 0 {
		return []Page{}, nil
	}

	rows, err := db.QueryContext(ctx, `SELECT page_slug, MAX(generated_at) FROM suggestions GROUP BY page_slug`)
	if err != nil {
		return nil, fmt.Errorf("query suggestions max generated_at: %w", err)
	}
	defer rows.Close()

	lastBySlug := make(map[string]time.Time)
	for rows.Next() {
		var slug string
		var maxGen sql.NullString
		if err := rows.Scan(&slug, &maxGen); err != nil {
			return nil, fmt.Errorf("scan suggestions max generated_at: %w", err)
		}
		if !maxGen.Valid {
			continue
		}
		t, err := time.Parse(time.RFC3339, maxGen.String)
		if err != nil {
			// Fall through with zero time so the slug is still treated as
			// "no prior suggestion" rather than dropping it silently.
			continue
		}
		lastBySlug[slug] = t
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate suggestions max generated_at: %w", err)
	}

	type entry struct {
		page    Page
		last    time.Time
		hasPast bool
	}
	entries := make([]entry, len(eligible))
	for i, p := range eligible {
		t, ok := lastBySlug[p.Slug]
		entries[i] = entry{page: p, last: t, hasPast: ok}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.hasPast != b.hasPast {
			return !a.hasPast
		}
		if !a.hasPast {
			return a.page.Slug < b.page.Slug
		}
		if !a.last.Equal(b.last) {
			return a.last.Before(b.last)
		}
		return a.page.Slug < b.page.Slug
	})

	out := make([]Page, len(entries))
	for i, e := range entries {
		out[i] = e.page
		if e.page.SourceFiles != nil {
			out[i].SourceFiles = append([]string(nil), e.page.SourceFiles...)
		}
	}

	if n <= 0 || n >= len(out) {
		return out, nil
	}
	return out[:n], nil
}
