package stars

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
)

// journeyTestDB creates an in-memory DB with the story_journeys table added.
func journeyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS story_journeys (
			id               INTEGER PRIMARY KEY,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			theme            TEXT NOT NULL DEFAULT 'middle_earth',
			total_distance_m REAL NOT NULL DEFAULT 0,
			created_at       TEXT NOT NULL DEFAULT '',
			updated_at       TEXT NOT NULL DEFAULT '',
			UNIQUE(user_id)
		);
	`)
	if err != nil {
		t.Fatalf("create story_journeys: %v", err)
	}
	return db
}

// insertJourneyFamily inserts a parent and child user with a family link.
func insertJourneyFamily(t *testing.T, db *sql.DB) (childID, parentID int64) {
	t.Helper()
	parentID = insertUser(t, db, "journey-parent@test.com")
	childID = insertUser(t, db, "journey-child@test.com")
	_, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, created_at) VALUES (?,?,'Kid','2024-01-01T00:00:00Z')`, parentID, childID)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}
	return
}

func TestGetJourney_CreatesDefaultJourney(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	resp, err := GetJourney(ctx, db, childID)
	if err != nil {
		t.Fatalf("GetJourney: %v", err)
	}
	if resp.Theme != "middle_earth" {
		t.Errorf("expected default theme middle_earth, got %s", resp.Theme)
	}
	if resp.TotalDistanceM != 0 {
		t.Errorf("expected 0 initial distance, got %f", resp.TotalDistanceM)
	}
	if resp.CurrentWaypoint.Name != "The Shire" {
		t.Errorf("expected current waypoint 'The Shire', got %q", resp.CurrentWaypoint.Name)
	}
	if resp.NextWaypoint == nil {
		t.Fatal("expected next waypoint to be set")
	}
	if resp.NextWaypoint.Name != "Bree" {
		t.Errorf("expected next waypoint 'Bree', got %q", resp.NextWaypoint.Name)
	}
	if len(resp.AvailableThemes) != 3 {
		t.Errorf("expected 3 available themes, got %d", len(resp.AvailableThemes))
	}
}

func TestGetJourney_IdempotentCreate(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	_, err := GetJourney(ctx, db, childID)
	if err != nil {
		t.Fatalf("first GetJourney: %v", err)
	}
	_, err = GetJourney(ctx, db, childID)
	if err != nil {
		t.Fatalf("second GetJourney: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM story_journeys WHERE user_id = ?`, childID).Scan(&count); err != nil {
		t.Fatalf("count journey rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 journey row, got %d", count)
	}
}

func TestUpdateJourneyDistance_NoWaypointCrossing(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	// Advance 50km — does not reach Bree at 100km.
	crossed, err := UpdateJourneyDistance(ctx, db, childID, 1, 50_000)
	if err != nil {
		t.Fatalf("UpdateJourneyDistance: %v", err)
	}
	if len(crossed) != 0 {
		t.Errorf("expected 0 crossed waypoints, got %d", len(crossed))
	}

	resp, err := GetJourney(ctx, db, childID)
	if err != nil {
		t.Fatalf("GetJourney after update: %v", err)
	}
	if resp.TotalDistanceM != 50_000 {
		t.Errorf("expected 50000m, got %f", resp.TotalDistanceM)
	}
}

func TestUpdateJourneyDistance_WaypointCrossing(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	// Advance 105km — crosses Bree at 100km.
	crossed, err := UpdateJourneyDistance(ctx, db, childID, 1, 105_000)
	if err != nil {
		t.Fatalf("UpdateJourneyDistance: %v", err)
	}
	if len(crossed) != 1 {
		t.Fatalf("expected 1 crossed waypoint, got %d", len(crossed))
	}
	if crossed[0].Name != "Bree" {
		t.Errorf("expected crossed waypoint 'Bree', got %q", crossed[0].Name)
	}

	// Verify the +10 star transaction was recorded.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM star_transactions WHERE user_id = ? AND reason = 'waypoint_reached'`, childID).Scan(&count); err != nil {
		t.Fatalf("query star_transactions: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 waypoint_reached transaction, got %d", count)
	}
}

func TestUpdateJourneyDistance_MultipleWaypointsCrossed(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	// Advance 260km — crosses Bree (100km) and Weathertop (250km).
	crossed, err := UpdateJourneyDistance(ctx, db, childID, 1, 260_000)
	if err != nil {
		t.Fatalf("UpdateJourneyDistance: %v", err)
	}
	if len(crossed) != 2 {
		t.Fatalf("expected 2 crossed waypoints, got %d: %v", len(crossed), crossed)
	}
}

func TestUpdateJourneyDistance_StartWaypointNotCrossed(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	// A tiny distance — the 0km start waypoint should not count as crossed.
	crossed, err := UpdateJourneyDistance(ctx, db, childID, 1, 1)
	if err != nil {
		t.Fatalf("UpdateJourneyDistance: %v", err)
	}
	for _, wp := range crossed {
		if wp.DistanceKm == 0 {
			t.Errorf("start waypoint (distance=0) should not be in crossed list")
		}
	}
}

func TestProgressCalculation_Midleg(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	// Place user at 50km — halfway between The Shire (0) and Bree (100km).
	_, err := db.Exec(`INSERT INTO story_journeys (user_id, theme, total_distance_m) VALUES (?, 'middle_earth', 50000)`, childID)
	if err != nil {
		t.Fatalf("insert journey: %v", err)
	}

	resp, err := GetJourney(ctx, db, childID)
	if err != nil {
		t.Fatalf("GetJourney: %v", err)
	}

	if resp.ProgressInLegPct < 49.9 || resp.ProgressInLegPct > 50.1 {
		t.Errorf("expected ~50%% progress, got %f", resp.ProgressInLegPct)
	}
	if resp.DistanceToNextKm < 49.9 || resp.DistanceToNextKm > 50.1 {
		t.Errorf("expected ~50km to next, got %f", resp.DistanceToNextKm)
	}
}

func TestProgressCalculation_AtEndOfJourney(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	// Place user beyond the final waypoint (Mount Doom at 1350km).
	_, err := db.Exec(`INSERT INTO story_journeys (user_id, theme, total_distance_m) VALUES (?, 'middle_earth', 1500000)`, childID)
	if err != nil {
		t.Fatalf("insert journey: %v", err)
	}

	resp, err := GetJourney(ctx, db, childID)
	if err != nil {
		t.Fatalf("GetJourney: %v", err)
	}

	if resp.CurrentWaypoint.Name != "Mount Doom" {
		t.Errorf("expected current waypoint 'Mount Doom', got %q", resp.CurrentWaypoint.Name)
	}
	if resp.NextWaypoint != nil {
		t.Errorf("expected nil next waypoint at journey end, got %+v", resp.NextWaypoint)
	}
	if resp.ProgressInLegPct != 100 {
		t.Errorf("expected 100%% progress at journey end, got %f", resp.ProgressInLegPct)
	}
}

func TestChangeTheme_Valid(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	// Create the journey with default theme first.
	if _, err := GetJourney(ctx, db, childID); err != nil {
		t.Fatalf("GetJourney: %v", err)
	}

	resp, err := ChangeTheme(ctx, db, childID, "space")
	if err != nil {
		t.Fatalf("ChangeTheme: %v", err)
	}
	if resp.Theme != "space" {
		t.Errorf("expected theme 'space', got %s", resp.Theme)
	}
	if resp.ThemeName != "Solar System Explorer" {
		t.Errorf("expected theme name 'Solar System Explorer', got %s", resp.ThemeName)
	}
	if resp.CurrentWaypoint.Name != "Earth" {
		t.Errorf("expected current waypoint 'Earth' for space theme, got %q", resp.CurrentWaypoint.Name)
	}
}

func TestChangeTheme_Invalid(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	if _, err := GetJourney(ctx, db, childID); err != nil {
		t.Fatalf("GetJourney: %v", err)
	}

	_, err := ChangeTheme(ctx, db, childID, "narnia")
	if err == nil {
		t.Error("expected error for invalid theme, got nil")
	}
}

func TestChangeTheme_PreservesDistance(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	// Pre-insert journey with some distance traveled.
	_, err := db.Exec(`INSERT INTO story_journeys (user_id, theme, total_distance_m) VALUES (?, 'middle_earth', 150000)`, childID)
	if err != nil {
		t.Fatalf("insert journey: %v", err)
	}

	resp, err := ChangeTheme(ctx, db, childID, "pirate")
	if err != nil {
		t.Fatalf("ChangeTheme: %v", err)
	}
	if resp.TotalDistanceM != 150_000 {
		t.Errorf("expected preserved distance 150000m, got %f", resp.TotalDistanceM)
	}
}

func TestGetJourneyHandler_OK(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	user := &auth.User{ID: childID}

	r := withUser(newRequest(http.MethodGet, "/api/stars/journey"), user)
	w := httptest.NewRecorder()
	GetJourneyHandler(db).ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp JourneyResponse
	decode(t, w.Body.Bytes(), &resp)
	if resp.Theme != "middle_earth" {
		t.Errorf("expected theme middle_earth, got %s", resp.Theme)
	}
}

func TestChangeThemeHandler_Valid(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	user := &auth.User{ID: childID}

	r := httptest.NewRequest(http.MethodPut, "/api/stars/journey/theme", strings.NewReader(`{"theme":"pirate"}`))
	r = withUser(r, user)
	w := httptest.NewRecorder()
	ChangeThemeHandler(db).ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp JourneyResponse
	decode(t, w.Body.Bytes(), &resp)
	if resp.Theme != "pirate" {
		t.Errorf("expected theme pirate, got %s", resp.Theme)
	}
}

func TestChangeThemeHandler_InvalidTheme(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	user := &auth.User{ID: childID}

	r := httptest.NewRequest(http.MethodPut, "/api/stars/journey/theme", strings.NewReader(`{"theme":"narnia"}`))
	r = withUser(r, user)
	w := httptest.NewRecorder()
	ChangeThemeHandler(db).ServeHTTP(w, r)

	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWaypointsCrossedStatus(t *testing.T) {
	db := journeyTestDB(t)
	childID, _ := insertJourneyFamily(t, db)
	ctx := context.Background()

	// Place user just past Rivendell (400km).
	_, err := db.Exec(`INSERT INTO story_journeys (user_id, theme, total_distance_m) VALUES (?, 'middle_earth', 420000)`, childID)
	if err != nil {
		t.Fatalf("insert journey: %v", err)
	}

	resp, err := GetJourney(ctx, db, childID)
	if err != nil {
		t.Fatalf("GetJourney: %v", err)
	}

	// Waypoints up to and including Rivendell (≤400km) should be crossed; beyond should not.
	for _, wp := range resp.Waypoints {
		shouldBeCrossed := wp.DistanceKm <= 400
		if wp.Crossed != shouldBeCrossed {
			t.Errorf("waypoint %q at %.0fkm: crossed=%v, expected=%v", wp.Name, wp.DistanceKm, wp.Crossed, shouldBeCrossed)
		}
	}
}
