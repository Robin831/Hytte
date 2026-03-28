package family

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// ── Reward handler tests ────────────────────────────────────────────────────

func TestListRewardsHandlerEmpty(t *testing.T) {
	db := setupRewardsTestDB(t)

	handler := ListRewardsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/family/rewards", nil), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Rewards []Reward `json:"rewards"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Rewards) != 0 {
		t.Errorf("expected 0 rewards, got %d", len(resp.Rewards))
	}
}

func TestCreateRewardHandlerSuccess(t *testing.T) {
	db := setupRewardsTestDB(t)

	handler := CreateRewardHandler(db)
	r := withUser(newRequest(http.MethodPost, "/api/family/rewards", map[string]any{
		"title":     "Movie Night",
		"star_cost": 10,
	}), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Reward Reward `json:"reward"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if resp.Reward.Title != "Movie Night" {
		t.Errorf("expected title 'Movie Night', got %q", resp.Reward.Title)
	}
	if resp.Reward.ID == 0 {
		t.Error("expected non-zero reward ID")
	}
}

func TestCreateRewardHandlerValidation(t *testing.T) {
	db := setupRewardsTestDB(t)
	handler := CreateRewardHandler(db)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing title", map[string]any{"star_cost": 5}},
		{"negative star_cost", map[string]any{"title": "Prize", "star_cost": -1}},
		{"max_claims zero", map[string]any{"title": "Prize", "star_cost": 5, "max_claims": 0}},
		{"max_claims negative", map[string]any{"title": "Prize", "star_cost": 5, "max_claims": -3}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := withUser(newRequest(http.MethodPost, "/api/family/rewards", tc.body), testParent)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestUpdateRewardHandlerSuccess(t *testing.T) {
	db := setupRewardsTestDB(t)

	reward, err := CreateReward(db, 1, "Old Title", "", "🎁", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	handler := UpdateRewardHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPut, "/api/family/rewards/1", map[string]any{
		"title":     "New Title",
		"star_cost": 15,
	}), "id", "1"), testParent)
	w := httptest.NewRecorder()
	_ = reward
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Reward Reward `json:"reward"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if resp.Reward.Title != "New Title" {
		t.Errorf("expected title 'New Title', got %q", resp.Reward.Title)
	}
}

func TestUpdateRewardHandlerNotFound(t *testing.T) {
	db := setupRewardsTestDB(t)

	handler := UpdateRewardHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPut, "/api/family/rewards/999", map[string]any{
		"title":     "X",
		"star_cost": 5,
	}), "id", "999"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestUpdateRewardHandlerInvalidMaxClaims(t *testing.T) {
	db := setupRewardsTestDB(t)

	if _, err := CreateReward(db, 1, "Prize", "", "🎁", "", 5, true, nil); err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	handler := UpdateRewardHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPut, "/api/family/rewards/1", map[string]any{
		"title":      "Prize",
		"star_cost":  5,
		"max_claims": 0,
	}), "id", "1"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for max_claims=0, got %d", w.Code)
	}
}

func TestDeleteRewardHandlerSuccess(t *testing.T) {
	db := setupRewardsTestDB(t)

	if _, err := CreateReward(db, 1, "Prize", "", "🎁", "", 5, true, nil); err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	handler := DeleteRewardHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodDelete, "/api/family/rewards/1", nil), "id", "1"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteRewardHandlerNotFound(t *testing.T) {
	db := setupRewardsTestDB(t)

	handler := DeleteRewardHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodDelete, "/api/family/rewards/999", nil), "id", "999"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestListClaimsHandlerEmpty(t *testing.T) {
	db := setupRewardsTestDB(t)

	handler := ListClaimsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/family/claims", nil), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Claims []ClaimWithDetails `json:"claims"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Claims) != 0 {
		t.Errorf("expected 0 claims, got %d", len(resp.Claims))
	}
}

func TestResolveClaimHandlerInvalidStatus(t *testing.T) {
	db := setupRewardsTestDB(t)

	handler := ResolveClaimHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPut, "/api/family/claims/1", map[string]any{
		"status": "maybe",
	}), "id", "1"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid status, got %d", w.Code)
	}
}

func TestResolveClaimHandlerNotFound(t *testing.T) {
	db := setupRewardsTestDB(t)

	handler := ResolveClaimHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPut, "/api/family/claims/999", map[string]any{
		"status": "approved",
	}), "id", "999"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestResolveClaimHandlerConflict(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	reward, err := CreateReward(db, 1, "Prize", "", "🎁", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}
	claim, err := ClaimReward(db, 2, reward.ID)
	if err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}
	// Resolve once.
	if _, err := ResolveClaim(db, claim.ID, 1, "approved", ""); err != nil {
		t.Fatalf("ResolveClaim: %v", err)
	}

	// Resolve again → conflict.
	handler := ResolveClaimHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPut, "/api/family/claims/1", map[string]any{
		"status": "denied",
	}), "id", "1"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListChallengeParticipantsHandler(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	c, err := CreateChallenge(db, 1, "Run 5K", "", "distance", 5.0, 3, "", "", true)
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	if err := AddParticipant(db, c.ID, 1, 2); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	handler := ListChallengeParticipantsHandler(db)
	idStr := strconv.FormatInt(c.ID, 10)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/challenges/"+idStr+"/participants", nil), "id", idStr), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Participants []ChallengeParticipant `json:"participants"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if len(resp.Participants) != 1 {
		t.Fatalf("expected 1 participant, got %d", len(resp.Participants))
	}
	if resp.Participants[0].ChildID != 2 {
		t.Errorf("expected child_id 2, got %d", resp.Participants[0].ChildID)
	}
	// completed_at must be empty string (not null) for in-progress participant.
	if resp.Participants[0].CompletedAt != "" {
		t.Errorf("expected empty completed_at, got %q", resp.Participants[0].CompletedAt)
	}
}

func TestListChallengeParticipantsHandlerNotFound(t *testing.T) {
	db := setupTestDB(t)

	handler := ListChallengeParticipantsHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/challenges/9999/participants", nil), "id", "9999"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListChallengeParticipantsHandlerInvalidID(t *testing.T) {
	db := setupTestDB(t)

	handler := ListChallengeParticipantsHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/challenges/bad/participants", nil), "id", "bad"), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAllChallengeParticipantsHandlerEmpty(t *testing.T) {
	db := setupTestDB(t)

	handler := ListAllChallengeParticipantsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/family/challenges/participants", nil), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Participants map[string][]ChallengeParticipant `json:"participants"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Participants) != 0 {
		t.Errorf("expected empty participants map, got %d entries", len(resp.Participants))
	}
}

func TestListAllChallengeParticipantsHandlerWithData(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	c, err := CreateChallenge(db, 1, "Group Run", "", "distance", 5.0, 3, "", "", true)
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}
	if err := AddParticipant(db, c.ID, 1, 2); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	handler := ListAllChallengeParticipantsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/family/challenges/participants", nil), testParent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Participants map[string][]ChallengeParticipant `json:"participants"`
	}
	decode(t, w.Body.Bytes(), &resp)

	idStr := strconv.FormatInt(c.ID, 10)
	ps, ok := resp.Participants[idStr]
	if !ok {
		t.Fatalf("expected key %q in participants map", idStr)
	}
	if len(ps) != 1 {
		t.Fatalf("expected 1 participant, got %d", len(ps))
	}
	if ps[0].ChildID != 2 {
		t.Errorf("expected child_id 2, got %d", ps[0].ChildID)
	}
	// Other parent's challenges must not appear.
	otherC, err := CreateChallenge(db, 2, "Other", "", "custom", 0, 0, "", "", true)
	if err != nil {
		t.Fatalf("CreateChallenge other: %v", err)
	}
	otherIDStr := strconv.FormatInt(otherC.ID, 10)
	if _, found := resp.Participants[otherIDStr]; found {
		t.Errorf("other parent's challenge must not appear in response")
	}
}

func TestMyFamilyHandlerNotLinked(t *testing.T) {
	db := setupTestDB(t)

	handler := MyFamilyHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/family/my-family", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unlinked child, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMyFamilyHandlerOnlyChild(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, testParent.ID, testChild.ID, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	handler := MyFamilyHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/family/my-family", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Parent struct {
			Name    string `json:"name"`
			Picture string `json:"picture"`
		} `json:"parent"`
		Siblings   []any `json:"siblings"`
		FamilySize int   `json:"family_size"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if resp.Parent.Name != testParent.Name {
		t.Errorf("expected parent name %q, got %q", testParent.Name, resp.Parent.Name)
	}
	if len(resp.Siblings) != 0 {
		t.Errorf("expected 0 siblings, got %d", len(resp.Siblings))
	}
	if resp.FamilySize != 1 {
		t.Errorf("expected family_size 1, got %d", resp.FamilySize)
	}
}

func TestMyFamilyHandlerWithSiblings(t *testing.T) {
	db := setupTestDB(t)

	// Add a third user as a second child.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sibling@test.com', 'Sibling', 'g3')`); err != nil {
		t.Fatalf("insert sibling user: %v", err)
	}
	const siblingID int64 = 3

	if _, err := CreateLink(db, testParent.ID, testChild.ID, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink child: %v", err)
	}
	if _, err := CreateLink(db, testParent.ID, siblingID, "Sis", "🌟"); err != nil {
		t.Fatalf("CreateLink sibling: %v", err)
	}

	// Give sibling some stars and a level.
	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned, total_spent) VALUES (?, 50, 10)`, siblingID); err != nil {
		t.Fatalf("insert star_balances: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_levels (user_id, xp, level, title) VALUES (?, 200, 3, 'Trail Blazer')`, siblingID); err != nil {
		t.Fatalf("insert user_levels: %v", err)
	}

	handler := MyFamilyHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/family/my-family", nil), testChild)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Parent struct {
			Name string `json:"name"`
		} `json:"parent"`
		Siblings []struct {
			ChildID        int64  `json:"child_id"`
			Nickname       string `json:"nickname"`
			AvatarEmoji    string `json:"avatar_emoji"`
			CurrentBalance int    `json:"current_balance"`
			Level          int    `json:"level"`
			Title          string `json:"title"`
		} `json:"siblings"`
		FamilySize int `json:"family_size"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if resp.Parent.Name != testParent.Name {
		t.Errorf("expected parent name %q, got %q", testParent.Name, resp.Parent.Name)
	}
	if len(resp.Siblings) != 1 {
		t.Fatalf("expected 1 sibling, got %d", len(resp.Siblings))
	}
	s := resp.Siblings[0]
	if s.ChildID != siblingID {
		t.Errorf("expected sibling child_id %d, got %d", siblingID, s.ChildID)
	}
	if s.Nickname != "Sis" {
		t.Errorf("expected nickname %q, got %q", "Sis", s.Nickname)
	}
	if s.AvatarEmoji != "🌟" {
		t.Errorf("expected avatar_emoji %q, got %q", "🌟", s.AvatarEmoji)
	}
	if s.CurrentBalance != 40 {
		t.Errorf("expected balance 40, got %d", s.CurrentBalance)
	}
	if s.Level != 3 {
		t.Errorf("expected level 3, got %d", s.Level)
	}
	if s.Title != "Trail Blazer" {
		t.Errorf("expected title %q, got %q", "Trail Blazer", s.Title)
	}
	if resp.FamilySize != 2 {
		t.Errorf("expected family_size 2, got %d", resp.FamilySize)
	}
}
