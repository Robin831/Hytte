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

func TestGetBeatMyParentStatus_ChildAhead(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Use a fixed anchor time within a known ISO week to avoid flakiness.
	// Child: 15 km, Parent: 10 km. No birthdays → scaling factor = 1.0.
	insertWorkoutAt(t, db, childID, 3600, 15000, anchorWeek.Format(time.RFC3339))
	insertWorkoutAt(t, db, parentID, 3600, 10000, anchorWeek.Format(time.RFC3339))

	status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchorWeek)
	if err != nil {
		t.Fatalf("GetBeatMyParentStatus: %v", err)
	}
	if status.ChildDistanceRaw != 15000 {
		t.Errorf("expected child raw=15000, got %v", status.ChildDistanceRaw)
	}
	if status.ChildDistanceScaled != 15000 {
		t.Errorf("expected child scaled=15000 (no age scaling), got %v", status.ChildDistanceScaled)
	}
	if status.ParentDistance != 10000 {
		t.Errorf("expected parent=10000, got %v", status.ParentDistance)
	}
	if !status.IsBeatingParent {
		t.Error("expected IsBeatingParent=true when child (15km) > parent (10km)")
	}
}

func TestGetBeatMyParentStatus_ParentAhead(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Child: 5 km, Parent: 20 km.
	insertWorkoutAt(t, db, childID, 1800, 5000, anchorWeek.Format(time.RFC3339))
	insertWorkoutAt(t, db, parentID, 7200, 20000, anchorWeek.Format(time.RFC3339))

	status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchorWeek)
	if err != nil {
		t.Fatalf("GetBeatMyParentStatus: %v", err)
	}
	if status.IsBeatingParent {
		t.Error("expected IsBeatingParent=false when child (5km) < parent (20km)")
	}
}

func TestGetBeatMyParentStatus_AgeScalingChildWins(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Child is 10, parent is 40. Scale = 40/10 = 4.0.
	// Child runs 3 km raw → 12 km scaled → beats parent's 10 km.
	childBD := fmt.Sprintf("%d-01-01", anchorWeek.Year()-10)
	parentBD := fmt.Sprintf("%d-01-01", anchorWeek.Year()-40)

	if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, childID, childBD); err != nil {
		t.Fatalf("set child birthday: %v", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, parentID, parentBD); err != nil {
		t.Fatalf("set parent birthday: %v", err)
	}

	insertWorkoutAt(t, db, childID, 1800, 3000, anchorWeek.Format(time.RFC3339))  // 3 km raw → 12 km scaled
	insertWorkoutAt(t, db, parentID, 3600, 10000, anchorWeek.Format(time.RFC3339)) // 10 km

	status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchorWeek)
	if err != nil {
		t.Fatalf("GetBeatMyParentStatus: %v", err)
	}
	if status.ChildDistanceRaw != 3000 {
		t.Errorf("expected child raw=3000, got %v", status.ChildDistanceRaw)
	}
	if status.ChildDistanceScaled != 12000 {
		t.Errorf("expected child scaled=12000 (3000 * 4.0), got %v", status.ChildDistanceScaled)
	}
	if !status.IsBeatingParent {
		t.Errorf("expected IsBeatingParent=true with age scaling (12km > 10km), got scaled=%.0f", status.ChildDistanceScaled)
	}
}

func TestGetBeatMyParentStatus_AgeScalingChildLoses(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Child is 8, parent is 32. Scale = 32/8 = 4.0.
	// Child runs 2 km raw → 8 km scaled. Parent runs 10 km.
	// Child does NOT beat parent even with scaling.
	childBD := fmt.Sprintf("%d-01-01", anchorWeek.Year()-8)
	parentBD := fmt.Sprintf("%d-01-01", anchorWeek.Year()-32)

	if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, childID, childBD); err != nil {
		t.Fatalf("set child birthday: %v", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, 'kids_stars_birthday', ?)`, parentID, parentBD); err != nil {
		t.Fatalf("set parent birthday: %v", err)
	}

	insertWorkoutAt(t, db, childID, 900, 2000, anchorWeek.Format(time.RFC3339))    // 2 km raw → 8 km scaled
	insertWorkoutAt(t, db, parentID, 3600, 10000, anchorWeek.Format(time.RFC3339)) // 10 km

	status, err := GetBeatMyParentStatus(context.Background(), db, childID, parentID, anchorWeek)
	if err != nil {
		t.Fatalf("GetBeatMyParentStatus: %v", err)
	}
	if status.IsBeatingParent {
		t.Errorf("expected IsBeatingParent=false (8km scaled < 10km), got true (scaled=%.0f)", status.ChildDistanceScaled)
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
