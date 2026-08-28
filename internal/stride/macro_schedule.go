package stride

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// MacroExtensionLeadWeeks is how close the active block's last week has to be
// before the next one is generated. Eight weeks is a full mesocycle of runway:
// the athlete always has a planned horizon longer than the longest single
// training phase, and a generation that fails on one Monday has seven more
// Mondays to succeed before the block actually runs out.
const MacroExtensionLeadWeeks = 8

// userLocks serialises the long-running per-athlete jobs — the weekly cron run
// and the HTTP endpoints that trigger the same work by hand. Both spend a
// multi-minute Claude call and write the same rows, so two of them overlapping
// for one athlete means two bills for one plan and a race on which of the two
// answers wins.
//
// Entries are never deleted: the map is bounded by the number of distinct user
// IDs this process ever serves (including users deleted since), which is small
// in practice. Deleting on unlock would reintroduce the load/lock race the map
// exists to prevent — a second caller can load the mutex an instant before the
// first deletes it, and the two then lock different mutexes.
var userLocks sync.Map // userID int64 -> *sync.Mutex

// LockUser blocks until it holds the athlete's lock and returns the function
// that releases it. Callers use it as:
//
//	release := LockUser(userID)
//	defer release()
//
// It is exported because the cron and the HTTP handlers have to take the *same*
// lock — a guard only the cron respects is no guard at all.
//
// The lock is a plain, NON-REENTRANT sync.Mutex, and it belongs to the
// top-level entry points only: RunWeekly and the handlers that trigger the same
// work by hand. Nothing they call — GeneratePlan, EnsureMacroPlan,
// GenerateMacroPlan or any helper below them — may take it, because RunWeekly
// holds it for the whole run and a second acquisition underneath would
// self-deadlock (or, via TryLockUser, make the run 409 itself).
func LockUser(userID int64) func() {
	mu := userMutex(userID)
	mu.Lock()
	return mu.Unlock
}

// TryLockUser takes the athlete's lock only if it is free, returning the
// release function and true, or nil and false when a generation for that
// athlete is already in flight. Like LockUser it is an entry-point-only guard
// on a non-reentrant mutex; see LockUser.
//
// Request-driven callers use this instead of LockUser. A cron run holds the
// lock for minutes — GenerateMacroPlan alone budgets 300s — and sync.Mutex.Lock
// cannot observe r.Context(), so a blocking handler would park a goroutine long
// after the client hung up and then answer nobody. Failing fast lets the caller
// answer 409 and let the athlete retry.
func TryLockUser(userID int64) (func(), bool) {
	mu := userMutex(userID)
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

func userMutex(userID int64) *sync.Mutex {
	v, _ := userLocks.LoadOrStore(userID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// generateMacroPlanFunc is the seam tests replace so the scheduling decisions
// in EnsureMacroPlan — and the hand-triggered generations in
// GenerateMacroPlanHandler / ExtendMacroPlanHandler — can be exercised without a
// Claude call. Production always runs GenerateMacroPlan.
var generateMacroPlanFunc = GenerateMacroPlan

// weeksRemaining returns how many whole weeks separate from and endWeek. A
// block whose last week *is* from has zero weeks remaining; a block that
// already ended returns a negative number.
//
// Sub-day skew is rounded away before the division, so the answer does not
// depend on both arguments being exact UTC midnights. Both operands come from
// parseWeekDate/parseMondayWeek and so are UTC midnights today; the rounding
// is there so a caller that ever hands this a local-time or DST-shifted
// midnight does not silently lose a whole week to truncation.
//
// Whole days are then floored rather than rounded, because day-level skew *is*
// reachable: nothing in macro_store validates that a stored end_week is a
// Monday (see NormaliseMacroStartWeek), and a block ending mid-week has that
// final week partially planned, not wholly remaining. Rounding four days up
// would overstate the horizon and skip the extension for a Monday; flooring
// understates it, which at worst buys the extension block one Monday early.
func weeksRemaining(from, endWeek time.Time) int {
	days := math.Round(endWeek.Sub(from).Hours() / 24)
	return int(math.Floor(days / 7))
}

// EnsureMacroPlan makes sure the athlete has a macro block covering nextMonday
// and enough planned horizon beyond it, generating at most one block per call:
//
//   - no active block covers nextMonday — generate a fresh MacroBlockWeeks-week
//     block starting there (generated_by 'scheduled');
//   - a block covers it but ends within MacroExtensionLeadWeeks weeks, and
//     nothing active covers the Monday after it ends — generate the extension
//     block starting there (generated_by 'extension', previous_plan_id set);
//   - otherwise do nothing.
//
// The decision is a check *before* the call rather than a catch after it,
// because the call is what costs money: a restart or a double trigger that
// re-enters here finds the block it already generated and returns without
// spending a second one. The partial unique index on
// (user_id, start_week) WHERE status='active' is the backstop for the window
// two concurrent generations could still slip through — it surfaces as
// ErrOverlappingMacroPlan, which is read here as "somebody else already did
// this" rather than as a failure.
//
// GenerateMacroPlan writes the block, its weeks and its initial goal revision
// in one transaction, so a failure anywhere in the flow leaves no partial
// block behind; there is nothing extra to roll back at this level.
func EnsureMacroPlan(ctx context.Context, db *sql.DB, userID int64, nextMonday time.Time) error {
	start, err := NormaliseMacroStartWeek(nextMonday.UTC().Format(dateLayout))
	if err != nil {
		return fmt.Errorf("normalise next Monday: %w", err)
	}
	startDate, err := parseMondayWeek(start)
	if err != nil {
		return fmt.Errorf("parse next Monday: %w", err)
	}

	active, err := GetActiveMacroPlan(ctx, db, userID, start)
	if err != nil {
		return fmt.Errorf("load active macro plan: %w", err)
	}
	if active == nil {
		// Nothing covers the week the athlete is about to train, so a full
		// block starts here. If an active block happens to start *later* — a
		// horizon gap, only reachable if a block was retired mid-flight — the
		// new one runs through it and GenerateMacroPlan retires that future
		// block. That is the wanted outcome: a plan the athlete can follow
		// from Monday beats an untouched block that starts weeks from now.
		return generateMacroBlock(ctx, db, userID, start, MacroModeScheduled)
	}

	endDate, err := parseWeekDate(active.EndWeek)
	if err != nil {
		return fmt.Errorf("parse macro plan %d end week: %w", active.ID, err)
	}
	if weeksRemaining(startDate, endDate) > MacroExtensionLeadWeeks {
		return nil
	}

	extensionStart, err := NormaliseMacroStartWeek(endDate.AddDate(0, 0, 7).Format(dateLayout))
	if err != nil {
		return fmt.Errorf("normalise extension start week: %w", err)
	}
	// Coverage, not an exact start_week match: any active block over that
	// Monday means the horizon is already extended, and generating into it
	// would burn a Claude call only for CreateMacroPlan to reject the overlap.
	successor, err := GetActiveMacroPlan(ctx, db, userID, extensionStart)
	if err != nil {
		return fmt.Errorf("load successor macro plan: %w", err)
	}
	if successor != nil {
		return nil
	}
	return generateMacroBlock(ctx, db, userID, extensionStart, MacroModeExtension)
}

// generateMacroBlock runs one generation and folds the "another writer got
// there first" outcome back into success. There is no retry: the next Monday
// re-enters EnsureMacroPlan and, with MacroExtensionLeadWeeks weeks of runway,
// a failed generation costs runway rather than the plan.
func generateMacroBlock(ctx context.Context, db *sql.DB, userID int64, startWeek string, mode MacroMode) error {
	if _, err := generateMacroPlanFunc(ctx, db, userID, startWeek, mode); err != nil {
		if errors.Is(err, ErrOverlappingMacroPlan) {
			log.Printf("stride: macro block for user %d starting %s already exists, skipping %s generation",
				userID, startWeek, mode)
			return nil
		}
		return fmt.Errorf("generate %s macro block starting %s: %w", mode, startWeek, err)
	}
	log.Printf("stride: generated %s macro block for user %d starting %s", mode, userID, startWeek)
	return nil
}
