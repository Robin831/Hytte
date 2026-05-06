package suggestions

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// RotationDefaultN is the default number of pages selected per nightly
// rotation. Picked to keep one nightly run well under the per-admin time
// budget while still cycling through the full registry within a couple of
// weeks.
const RotationDefaultN = 10

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
