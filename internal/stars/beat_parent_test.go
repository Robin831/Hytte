package stars

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// anchorWeek is a fixed Monday in a known ISO week used to avoid flakiness at
// week-rollover boundaries. ISO week 2 of 2025, starts 2025-01-06.
var anchorWeek = time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)

// --- GetBeatMyParentStatus tests ---

func TestGetBeatMyParentStatus_NoWorkouts(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchorWeek)
	if err != nil {
		t.Fatalf("GetBeatMyParentStatus: %v", err)
	}
	if status.ChildDistanceRaw != 0 {
		t.Errorf("expected child raw=0, got %v", status.ChildDistanceRaw)
	}
	if status.ParentDistance != 0 {
		t.Errorf("expected parent=0, got %v", status.ParentDistance)
	}
	if status.IsBeatingParent {
		t.Error("expected IsBeatingParent=false when both have 0 distance")
	}
}

// TestAgeScalingMath_TableDriven verifies that ChildDistanceScaled equals
// ChildDistanceRaw * (parent_age / child_age) for a range of age combinations.
// Birthdays are set as Jan 1 so the full age is reached by the January anchor date.
func TestAgeScalingMath_TableDriven(t *testing.T) {
	anchor := anchorWeek

	cases := []struct {
		name       string
		childAge   int
		parentAge  int
		rawMeters  float64
		wantScaled float64
	}{
		{"child10_parent40_scale4x", 10, 40, 3000, 12000},
		{"child8_parent32_scale4x", 8, 32, 2500, 10000},
		{"child12_parent36_scale3x", 12, 36, 5000, 15000},
		{"same_age_scale1x", 35, 35, 7000, 7000},
		{"child_older_parent_younger_scale0_5x", 40, 20, 10000, 5000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			parentID := insertUser(t, db, "parent@scale.com")
			childID := insertUser(t, db, "child@scale.com")
			linkChild(t, db, parentID, childID)

			childBD := fmt.Sprintf("%d-01-01", anchor.Year()-tc.childAge)
			parentBD := fmt.Sprintf("%d-01-01", anchor.Year()-tc.parentAge)
			if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, childID, childBD); err != nil {
				t.Fatalf("set child birthday: %v", err)
			}
			if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, parentID, parentBD); err != nil {
				t.Fatalf("set parent birthday: %v", err)
			}

			insertWorkoutAt(t, db, childID, 3600, tc.rawMeters, anchor.Format(time.RFC3339))
			// Parent needs a nominal workout so distance comparison doesn't interfere.
			insertWorkoutAt(t, db, parentID, 3600, 1, anchor.Format(time.RFC3339))

			status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchor)
			if err != nil {
				t.Fatalf("GetBeatMyParentStatus: %v", err)
			}
			if status.ChildDistanceRaw != tc.rawMeters {
				t.Errorf("raw: got %.0f, want %.0f", status.ChildDistanceRaw, tc.rawMeters)
			}
			if status.ChildDistanceScaled != tc.wantScaled {
				t.Errorf("scaled: got %.0f, want %.0f (raw=%.0f, childAge=%d, parentAge=%d)",
					status.ChildDistanceScaled, tc.wantScaled, tc.rawMeters, tc.childAge, tc.parentAge)
			}
		})
	}
}

// TestBeatParentComparison_TableDriven verifies IsBeatingParent true/false for a
// range of (scaled child distance, parent distance) combinations, including cases
// where age scaling changes the outcome.
func TestBeatParentComparison_TableDriven(t *testing.T) {
	anchor := anchorWeek

	cases := []struct {
		name         string
		childMeters  float64
		parentMeters float64
		// 0 = no birthday set → ageScalingFactor defaults to 1.0
		childAge    int
		parentAge   int
		wantBeating bool
	}{
		{"child_ahead_no_scaling", 15000, 10000, 0, 0, true},
		{"parent_ahead_no_scaling", 5000, 20000, 0, 0, false},
		{"tied_no_scaling", 10000, 10000, 0, 0, false}, // strict greater-than required
		{"child_wins_via_age_scaling", 3000, 10000, 10, 40, true},  // 3000 * 4 = 12000 > 10000
		{"child_loses_despite_scaling", 2000, 10000, 8, 32, false}, // 2000 * 4 = 8000 < 10000
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			parentID := insertUser(t, db, "parent@cmp.com")
			childID := insertUser(t, db, "child@cmp.com")
			linkChild(t, db, parentID, childID)

			if tc.childAge > 0 {
				childBD := fmt.Sprintf("%d-01-01", anchor.Year()-tc.childAge)
				if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, childID, childBD); err != nil {
					t.Fatalf("set child birthday: %v", err)
				}
			}
			if tc.parentAge > 0 {
				parentBD := fmt.Sprintf("%d-01-01", anchor.Year()-tc.parentAge)
				if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, parentID, parentBD); err != nil {
					t.Fatalf("set parent birthday: %v", err)
				}
			}

			if tc.childMeters > 0 {
				insertWorkoutAt(t, db, childID, 3600, tc.childMeters, anchor.Format(time.RFC3339))
			}
			if tc.parentMeters > 0 {
				insertWorkoutAt(t, db, parentID, 3600, tc.parentMeters, anchor.Format(time.RFC3339))
			}

			status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchor)
			if err != nil {
				t.Fatalf("GetBeatMyParentStatus: %v", err)
			}
			if status.IsBeatingParent != tc.wantBeating {
				t.Errorf("IsBeatingParent: got %v, want %v (child=%.0fm, parent=%.0fm, childAge=%d, parentAge=%d, scaled=%.0fm)",
					status.IsBeatingParent, tc.wantBeating,
					tc.childMeters, tc.parentMeters, tc.childAge, tc.parentAge,
					status.ChildDistanceScaled)
			}
		})
	}
}

// TestBeatParent_ParentZeroWorkouts verifies that when a parent has no workouts
// for the week but the child does, GetBeatMyParentStatus does not panic or error
// (no divide-by-zero in the age scaling factor) and returns IsBeatingParent=false
// — a child cannot "beat" a parent who has not participated that week.
func TestBeatParent_ParentZeroWorkouts(t *testing.T) {
	anchor := anchorWeek
	db := setupTestDB(t)

	parentID := insertUser(t, db, "parent@zero.com")
	childID := insertUser(t, db, "child@zero.com")
	linkChild(t, db, parentID, childID)

	// Child has a solid workout; parent has zero workouts.
	// No birthdays configured — ageScalingFactor must default to 1.0 without panicking.
	insertWorkoutAt(t, db, childID, 3600, 10000, anchor.Format(time.RFC3339))

	status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchor)
	if err != nil {
		t.Fatalf("GetBeatMyParentStatus with parent zero workouts: %v", err)
	}
	if status.ChildDistanceRaw != 10000 {
		t.Errorf("expected child raw=10000, got %v", status.ChildDistanceRaw)
	}
	if status.ChildDistanceScaled != 10000 {
		t.Errorf("expected child scaled=10000 (factor=1.0), got %v", status.ChildDistanceScaled)
	}
	if status.ParentDistance != 0 {
		t.Errorf("expected parent=0, got %v", status.ParentDistance)
	}
	if status.IsBeatingParent {
		t.Error("expected IsBeatingParent=false when parent has zero workouts")
	}
}

// --- AwardBeatParentBonus tests ---

func TestAwardBeatParentBonus_ChildWins(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	insertWorkoutAt(t, db, childID, 3600, 20000, anchorWeek.Format(time.RFC3339)) // 20 km
	insertWorkoutAt(t, db, parentID, 1800, 5000, anchorWeek.Format(time.RFC3339)) // 5 km

	award, err := AwardBeatParentBonus(context.Background(), db, childID, parentID, anchorWeek)
	if err != nil {
		t.Fatalf("AwardBeatParentBonus: %v", err)
	}
	if award == nil {
		t.Fatal("expected a StarAward when child beats parent, got nil")
	}
	if award.Amount != 25 {
		t.Errorf("expected 25 stars, got %d", award.Amount)
	}
}

func TestAwardBeatParentBonus_ChildLoses(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	insertWorkoutAt(t, db, childID, 900, 2000, anchorWeek.Format(time.RFC3339))    // 2 km
	insertWorkoutAt(t, db, parentID, 3600, 15000, anchorWeek.Format(time.RFC3339)) // 15 km

	award, err := AwardBeatParentBonus(context.Background(), db, childID, parentID, anchorWeek)
	if err != nil {
		t.Fatalf("AwardBeatParentBonus: %v", err)
	}
	if award != nil {
		t.Errorf("expected nil award when child does not beat parent, got %+v", award)
	}
}

func TestAwardBeatParentBonus_Tied(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Exact tie — child must strictly exceed parent (>), so tie → no award.
	insertWorkoutAt(t, db, childID, 3600, 10000, anchorWeek.Format(time.RFC3339))
	insertWorkoutAt(t, db, parentID, 3600, 10000, anchorWeek.Format(time.RFC3339))

	award, err := AwardBeatParentBonus(context.Background(), db, childID, parentID, anchorWeek)
	if err != nil {
		t.Fatalf("AwardBeatParentBonus: %v", err)
	}
	if award != nil {
		t.Error("expected nil award on exact tie")
	}
}

func TestAwardBeatParentBonus_AwardReason(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	insertWorkoutAt(t, db, childID, 3600, 20000, anchorWeek.Format(time.RFC3339))
	insertWorkoutAt(t, db, parentID, 1800, 5000, anchorWeek.Format(time.RFC3339))

	award, err := AwardBeatParentBonus(context.Background(), db, childID, parentID, anchorWeek)
	if err != nil {
		t.Fatalf("AwardBeatParentBonus: %v", err)
	}
	if award == nil {
		t.Fatal("expected award, got nil")
	}
	expectedReason := fmt.Sprintf("beat_parent_%s", weekKey(anchorWeek))
	if award.Reason != expectedReason {
		t.Errorf("expected reason %q, got %q", expectedReason, award.Reason)
	}
}

// TestBeatParentBonus_CreditedExactlyOnce verifies that the 25-star beat-parent
// bonus is recorded exactly once when EvaluateWeeklyBonuses is called twice for
// the same (child, week). The idempotency guard in weekly_bonus_evaluations must
// prevent a second award.
func TestBeatParentBonus_CreditedExactlyOnce(t *testing.T) {
	ctx := context.Background()
	db := setupWeeklyDB(t)
	anchor := anchorWeek

	parentID := insertUser(t, db, "parent@bonus.com")
	childID := insertUser(t, db, "child@bonus.com")
	linkChild(t, db, parentID, childID)

	// Child runs 20 km; parent runs 5 km. No age scaling (no birthdays set).
	insertWorkoutAt(t, db, childID, 7200, 20000, anchor.Format(time.RFC3339))
	insertWorkoutAt(t, db, parentID, 1800, 5000, anchor.Format(time.RFC3339))

	// First evaluation — beat-parent bonus must be awarded.
	awards1, err := EvaluateWeeklyBonuses(ctx, db, childID, anchor)
	if err != nil {
		t.Fatalf("first EvaluateWeeklyBonuses: %v", err)
	}

	expectedReason := fmt.Sprintf("beat_parent_%s", weekKey(anchor))
	found := false
	for _, a := range awards1 {
		if a.Reason == expectedReason {
			found = true
			if a.Amount != 25 {
				t.Errorf("beat-parent bonus: got %d stars, want 25", a.Amount)
			}
		}
	}
	if !found {
		t.Fatalf("beat-parent bonus not found in first evaluation; got awards: %v", awardReasons(awards1))
	}

	earned1, _, _ := getBalance(t, db, childID)

	// Second evaluation for the same week — idempotency guard must fire and return nil.
	awards2, err := EvaluateWeeklyBonuses(ctx, db, childID, anchor)
	if err != nil {
		t.Fatalf("second EvaluateWeeklyBonuses: %v", err)
	}
	if awards2 != nil {
		t.Errorf("expected no awards on second call (idempotent); got: %v", awardReasons(awards2))
	}

	earned2, _, _ := getBalance(t, db, childID)
	if earned2 != earned1 {
		t.Errorf("balance changed after second evaluation: was %d, now %d (double award!)", earned1, earned2)
	}

	// The beat-parent transaction must appear exactly once in the DB.
	var txCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM star_transactions WHERE user_id = ? AND reason = ?`,
		childID, expectedReason).Scan(&txCount); err != nil {
		t.Fatalf("count beat-parent transactions: %v", err)
	}
	if txCount != 1 {
		t.Errorf("expected exactly 1 beat-parent transaction in DB, got %d", txCount)
	}
}

// --- BeatMyParentHandler tests ---

func TestBeatMyParentHandler_NoParent(t *testing.T) {
	db := setupTestDB(t)
	childID := insertUser(t, db, "loner@test.com")
	user := &auth.User{ID: childID, Email: "loner@test.com", Name: "Loner"}

	handler := BeatMyParentHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/beat-parent"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when no parent linked, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBeatMyParentHandler_NotBeating(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	insertWorkout(t, db, childID, 900, 3000, 0, 0, 0)
	insertWorkout(t, db, parentID, 3600, 20000, 0, 0, 0)

	user := &auth.User{ID: childID, Email: "child@test.com", Name: "Child"}
	handler := BeatMyParentHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/beat-parent"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp BeatParentStatus
	decode(t, w.Body.Bytes(), &resp)

	if resp.IsBeatingParent {
		t.Error("expected IsBeatingParent=false")
	}
	if resp.ChildDistanceRaw != 3000 {
		t.Errorf("expected child raw=3000, got %v", resp.ChildDistanceRaw)
	}
	if resp.ParentDistance != 20000 {
		t.Errorf("expected parent=20000, got %v", resp.ParentDistance)
	}
}

func TestBeatMyParentHandler_Beating(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	insertWorkout(t, db, childID, 7200, 30000, 0, 0, 0)
	insertWorkout(t, db, parentID, 1800, 5000, 0, 0, 0)

	user := &auth.User{ID: childID, Email: "child@test.com", Name: "Child"}
	handler := BeatMyParentHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/beat-parent"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp BeatParentStatus
	decode(t, w.Body.Bytes(), &resp)

	if !resp.IsBeatingParent {
		t.Error("expected IsBeatingParent=true")
	}
	if resp.ChildDistanceRaw != 30000 {
		t.Errorf("expected child raw=30000, got %v", resp.ChildDistanceRaw)
	}
	if resp.ParentDistance != 5000 {
		t.Errorf("expected parent=5000, got %v", resp.ParentDistance)
	}
}
