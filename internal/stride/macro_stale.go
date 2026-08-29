package stride

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"slices"
)

// This file implements one rule from the macro-plan design: a race added,
// edited or removed inside an active block's horizon marks that block stale.
// It never regenerates anything — the plan keeps its active role and the UI
// gets a banner with a Regenerate button the athlete presses when they want a
// new block. Auto-regenerating here would burn a Claude call on every race
// edit and silently rewrite a plan the athlete is already training against.

// raceStaleWeeks maps the dates a race write touched to the distinct week keys
// whose blocks it could invalidate, oldest first. Empty dates are skipped, so
// a create or delete can pass the one date it has and an update can pass both
// the old and the new one without either caller special-casing anything.
//
// The dates are snapped back to their Monday because a block's horizon is
// stored as two week keys (start_week, end_week, both Mondays): a race on a
// Saturday belongs to the block covering the Monday of that week.
func raceStaleWeeks(dates []string) ([]string, error) {
	weeks := make([]string, 0, len(dates))
	for _, date := range dates {
		if date == "" {
			continue
		}
		week, err := NormaliseMacroStartWeek(date)
		if err != nil {
			return nil, fmt.Errorf("race date %q: %w", date, err)
		}
		if !slices.Contains(weeks, week) {
			weeks = append(weeks, week)
		}
	}
	slices.Sort(weeks)
	return weeks, nil
}

// MarkMacroPlansStaleForRaces marks every active macro block whose horizon
// covers one of dates with MacroStaleRacesChanged.
//
// Passing both the old and the new date of an edited race is what makes a race
// moving *into* or *out of* a horizon count: either week resolves a block, and
// both of them are marked when the move crossed a block boundary. A date no
// active block covers marks nothing, and an athlete with no active block at all
// is not an error — there is simply nothing to invalidate.
//
// Marking is idempotent: an already-stale block is written the same reason
// again rather than being treated as a conflict.
func MarkMacroPlansStaleForRaces(ctx context.Context, db *sql.DB, userID int64, dates ...string) error {
	weeks, err := raceStaleWeeks(dates)
	if err != nil {
		return err
	}

	marked := map[int64]struct{}{}
	for _, week := range weeks {
		// Coverage of a single week, expressed through the same predicate
		// GetActiveMacroPlan uses, so "which block owns this week" has one
		// answer across the package.
		spans, err := listActiveMacroPlanSpans(ctx, db, userID, week, week)
		if err != nil {
			return fmt.Errorf("find active macro plans covering %s: %w", week, err)
		}
		for _, span := range spans {
			if _, done := marked[span.ID]; done {
				continue
			}
			if err := SetMacroPlanStale(ctx, db, span.ID, userID, MacroStaleRacesChanged); err != nil {
				return fmt.Errorf("mark macro plan %d stale: %w", span.ID, err)
			}
			marked[span.ID] = struct{}{}
		}
	}
	return nil
}

// noteRaceCalendarChanged is the handler-side wrapper: the race write it
// follows has already committed, so a failure to mark the block stale is
// logged rather than turned into an error that would tell the athlete their
// race was not saved. The worst case is a block that stays unflagged until the
// next race edit or the Monday job.
func noteRaceCalendarChanged(ctx context.Context, db *sql.DB, userID int64, dates ...string) {
	if err := MarkMacroPlansStaleForRaces(ctx, db, userID, dates...); err != nil {
		log.Printf("stride: mark macro plans stale after race change for user %d: %v", userID, err)
	}
}
