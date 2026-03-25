package family

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func newRequest(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			panic(err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func withUser(r *http.Request, user *auth.User) *http.Request {
	return r.WithContext(auth.ContextWithUser(r.Context(), user))
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func decode(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, body)
	}
}

var testParent = &auth.User{ID: 1, Email: "parent@test.com", Name: "Parent"}
var testChild = &auth.User{ID: 2, Email: "child@test.com", Name: "Child"}

func TestStatusHandlerNoLinks(t *testing.T) {
	db := setupTestDB(t)

	handler := StatusHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/family/status", nil), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		IsParent bool `json:"is_parent"`
		IsChild  bool `json:"is_child"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if resp.IsParent || resp.IsChild {
		t.Errorf("expected false/false for new user, got is_parent=%v is_child=%v", resp.IsParent, resp.IsChild)
	}
}

func TestListChildrenHandlerEmpty(t *testing.T) {
	db := setupTestDB(t)

	handler := ListChildrenHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/family/children", nil), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Children []FamilyLink `json:"children"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Children) != 0 {
		t.Errorf("expected 0 children, got %d", len(resp.Children))
	}
}

func TestGenerateAndAcceptInviteHandlers(t *testing.T) {
	db := setupTestDB(t)

	// Parent generates invite.
	genHandler := GenerateInviteHandler(db)
	r := withUser(newRequest(http.MethodPost, "/api/family/invite", nil), testParent)
	w := httptest.NewRecorder()
	genHandler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var genResp struct {
		Invite InviteCode `json:"invite"`
	}
	decode(t, w.Body.Bytes(), &genResp)

	if len(genResp.Invite.Code) != inviteCodeLen {
		t.Errorf("expected code length %d, got %d", inviteCodeLen, len(genResp.Invite.Code))
	}

	// Child accepts invite.
	acceptHandler := AcceptInviteHandler(db)
	r2 := withUser(newRequest(http.MethodPost, "/api/family/invite/accept", map[string]string{
		"code": genResp.Invite.Code,
	}), testChild)
	w2 := httptest.NewRecorder()
	acceptHandler.ServeHTTP(w2, r2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("expected 201 from accept, got %d: %s", w2.Code, w2.Body.String())
	}

	// Now list children from parent's perspective.
	listHandler := ListChildrenHandler(db)
	r3 := withUser(newRequest(http.MethodGet, "/api/family/children", nil), testParent)
	w3 := httptest.NewRecorder()
	listHandler.ServeHTTP(w3, r3)

	var listResp struct {
		Children []FamilyLink `json:"children"`
	}
	decode(t, w3.Body.Bytes(), &listResp)
	if len(listResp.Children) != 1 {
		t.Errorf("expected 1 child after linking, got %d", len(listResp.Children))
	}
}

func TestUnlinkChildHandler(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	handler := UnlinkChildHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodDelete, "/api/family/children/2", nil), "id", "2"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUnlinkChildHandlerNotFound(t *testing.T) {
	db := setupTestDB(t)

	handler := UnlinkChildHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodDelete, "/api/family/children/2", nil), "id", "2"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateChildHandler(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	handler := UpdateChildHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPut, "/api/family/children/2", map[string]string{
		"nickname":     "Champion",
		"avatar_emoji": "🏆",
	}), "id", "2"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Link FamilyLink `json:"link"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if resp.Link.Nickname != "Champion" {
		t.Errorf("expected nickname 'Champion', got %q", resp.Link.Nickname)
	}
}

func TestAcceptInviteHandlerInvalidCode(t *testing.T) {
	db := setupTestDB(t)

	handler := AcceptInviteHandler(db)
	r := withUser(newRequest(http.MethodPost, "/api/family/invite/accept", map[string]string{
		"code": "XXXXXX",
	}), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid code, got %d", w.Code)
	}
}

func TestAcceptInviteHandlerMissingCode(t *testing.T) {
	db := setupTestDB(t)

	handler := AcceptInviteHandler(db)
	r := withUser(newRequest(http.MethodPost, "/api/family/invite/accept", map[string]string{}), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing code, got %d", w.Code)
	}
}

// TestChildStatsHandlerForbidden verifies that a user who is NOT the parent of child 2 gets 403.
func TestChildStatsHandlerForbidden(t *testing.T) {
	db := setupTestDB(t)

	// No family_link exists yet — testParent is not linked to testChild.
	handler := ChildStatsHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/children/2/stats", nil), "id", "2"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unlinked parent, got %d: %s", w.Code, w.Body.String())
	}
}

// TestChildStatsHandlerOK verifies that a linked parent gets correct stats for their child.
func TestChildStatsHandlerOK(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Insert a star balance and level for the child.
	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned, total_spent) VALUES (2, 50, 10)`); err != nil {
		t.Fatalf("insert balance: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_levels (user_id, xp, level, title) VALUES (2, 200, 3, 'Rising Star')`); err != nil {
		t.Fatalf("insert level: %v", err)
	}

	// Insert a recent transaction for the child.
	if _, err := db.Exec(`INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
		VALUES (2, 5, 'showed_up', 'Completed a workout', 1, '2026-03-25T08:00:00Z')`); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	handler := ChildStatsHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/children/2/stats", nil), "id", "2"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		CurrentBalance     int `json:"current_balance"`
		TotalEarned        int `json:"total_earned"`
		Level              int `json:"level"`
		XP                 int `json:"xp"`
		CurrentStreak      int `json:"current_streak"`
		LongestStreak      int `json:"longest_streak"`
		RecentTransactions []struct {
			Amount int    `json:"amount"`
			Reason string `json:"reason"`
		} `json:"recent_transactions"`
		ActiveChallenges []any `json:"active_challenges"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if resp.CurrentBalance != 40 {
		t.Errorf("expected current_balance 40, got %d", resp.CurrentBalance)
	}
	if resp.TotalEarned != 50 {
		t.Errorf("expected total_earned 50, got %d", resp.TotalEarned)
	}
	if resp.Level != 3 {
		t.Errorf("expected level 3, got %d", resp.Level)
	}
	if resp.XP != 200 {
		t.Errorf("expected xp 200, got %d", resp.XP)
	}
	if len(resp.RecentTransactions) != 1 {
		t.Errorf("expected 1 recent transaction, got %d", len(resp.RecentTransactions))
	}
	if resp.RecentTransactions[0].Reason != "showed_up" {
		t.Errorf("expected reason 'showed_up', got %q", resp.RecentTransactions[0].Reason)
	}
	if resp.ActiveChallenges == nil {
		t.Error("expected active_challenges to be an empty array, got nil")
	}
}

// TestChildWorkoutsHandlerForbidden verifies that a user who is NOT the parent gets 403.
func TestChildWorkoutsHandlerForbidden(t *testing.T) {
	db := setupTestDB(t)

	// testParent is not linked to testChild.
	handler := ChildWorkoutsHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/children/2/workouts", nil), "id", "2"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unlinked parent, got %d: %s", w.Code, w.Body.String())
	}
}

// TestChildWorkoutsHandlerOK verifies that a linked parent gets workouts with correct fields and no GPS.
func TestChildWorkoutsHandlerOK(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Insert a workout for the child.
	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, duration_seconds, distance_meters,
		                      avg_heart_rate, calories, ascent_meters, title, fit_file_hash, created_at)
		VALUES (10, 2, 'running', '2026-03-25T08:00:00Z', 3600, 10000.0, 145, 600, 50.0, 'Morning Run', 'hash1', '2026-03-25T09:00:00Z')
	`); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	// Insert star transactions referencing the workout.
	if _, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
		VALUES (2, 5, 'showed_up', 'Completed workout', 10, '2026-03-25T09:00:00Z')
	`); err != nil {
		t.Fatalf("insert star tx: %v", err)
	}

	handler := ChildWorkoutsHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/children/2/workouts", nil), "id", "2"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Workouts []struct {
			ID              int64   `json:"id"`
			StartedAt       string  `json:"started_at"`
			Sport           string  `json:"sport"`
			DurationSeconds int     `json:"duration_seconds"`
			DistanceMeters  float64 `json:"distance_meters"`
			AvgHeartRate    int     `json:"avg_heart_rate"`
			Calories        int     `json:"calories"`
			AscentMeters    float64 `json:"ascent_meters"`
			Stars           int     `json:"stars"`
		} `json:"workouts"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if resp.Total != 1 {
		t.Errorf("expected total 1, got %d", resp.Total)
	}
	if len(resp.Workouts) != 1 {
		t.Fatalf("expected 1 workout, got %d", len(resp.Workouts))
	}

	wo := resp.Workouts[0]
	if wo.ID != 10 {
		t.Errorf("expected id 10, got %d", wo.ID)
	}
	if wo.Sport != "running" {
		t.Errorf("expected sport 'running', got %q", wo.Sport)
	}
	if wo.DurationSeconds != 3600 {
		t.Errorf("expected duration_seconds 3600, got %d", wo.DurationSeconds)
	}
	if wo.DistanceMeters != 10000.0 {
		t.Errorf("expected distance_meters 10000, got %f", wo.DistanceMeters)
	}
	if wo.Stars != 5 {
		t.Errorf("expected stars 5, got %d", wo.Stars)
	}

	// GPS/sample data must not be present in the response.
	var raw map[string]any
	decode(t, w.Body.Bytes(), &raw)
	workoutsRaw, _ := raw["workouts"].([]any)
	if len(workoutsRaw) > 0 {
		wo0 := workoutsRaw[0].(map[string]any)
		for _, gpsKey := range []string{"samples", "laps", "gps", "lat", "lon", "coordinates"} {
			if _, ok := wo0[gpsKey]; ok {
				t.Errorf("workout response must not contain GPS field %q", gpsKey)
			}
		}
	}
}

// TestChildWorkoutsHandlerPagination verifies that limit and offset parameters work correctly.
func TestChildWorkoutsHandlerPagination(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Insert 3 workouts for the child.
	for i := 1; i <= 3; i++ {
		if _, err := db.Exec(`
			INSERT INTO workouts (id, user_id, sport, started_at, duration_seconds, fit_file_hash, created_at)
			VALUES (?, 2, 'cycling', ?, 1800, ?, ?)
		`, i, "2026-03-2"+string(rune('0'+i))+"T08:00:00Z", "hash"+string(rune('0'+i)), "2026-03-2"+string(rune('0'+i))+"T09:00:00Z"); err != nil {
			t.Fatalf("insert workout %d: %v", i, err)
		}
	}

	handler := ChildWorkoutsHandler(db)

	// Fetch with limit=2.
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/children/2/workouts?limit=2", nil), "id", "2"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Workouts []any `json:"workouts"`
		Total    int   `json:"total"`
		Limit    int   `json:"limit"`
		Offset   int   `json:"offset"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if resp.Total != 3 {
		t.Errorf("expected total 3, got %d", resp.Total)
	}
	if len(resp.Workouts) != 2 {
		t.Errorf("expected 2 workouts with limit=2, got %d", len(resp.Workouts))
	}
	if resp.Limit != 2 {
		t.Errorf("expected limit 2, got %d", resp.Limit)
	}

	// Fetch with offset=2 should return remaining 1 workout.
	r2 := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/children/2/workouts?limit=2&offset=2", nil), "id", "2"), testParent)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)

	var resp2 struct {
		Workouts []any `json:"workouts"`
		Total    int   `json:"total"`
		Offset   int   `json:"offset"`
	}
	decode(t, w2.Body.Bytes(), &resp2)

	if resp2.Total != 3 {
		t.Errorf("expected total 3 at offset=2, got %d", resp2.Total)
	}
	if len(resp2.Workouts) != 1 {
		t.Errorf("expected 1 workout at offset=2, got %d", len(resp2.Workouts))
	}
	if resp2.Offset != 2 {
		t.Errorf("expected offset 2, got %d", resp2.Offset)
	}
}
