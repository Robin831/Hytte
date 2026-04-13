package stride

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// --- extractPlanJSON tests ---

func TestExtractPlanJSON_Found(t *testing.T) {
	response := "Here's the updated plan:\n```json\n[{\"date\":\"2026-04-13\",\"rest_day\":true}]\n```\nLet me know if you need changes."
	got, ok := extractPlanJSON(response)
	if !ok {
		t.Fatal("expected to find plan JSON")
	}
	if !strings.HasPrefix(got, "[") {
		t.Errorf("expected JSON array, got: %s", got)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(got), &arr); err != nil {
		t.Errorf("extracted JSON is not valid: %v", err)
	}
}

func TestExtractPlanJSON_NotFound(t *testing.T) {
	response := "I think you should rest tomorrow. No plan changes needed."
	_, ok := extractPlanJSON(response)
	if ok {
		t.Fatal("expected not to find plan JSON in a text-only response")
	}
}

func TestExtractPlanJSON_MultipleFencedBlocks(t *testing.T) {
	response := "Before:\n```json\n[{\"date\":\"old\"}]\n```\n\nAfter:\n```json\n[{\"date\":\"new\"}]\n```\n"
	got, ok := extractPlanJSON(response)
	if !ok {
		t.Fatal("expected to find plan JSON")
	}
	if !strings.Contains(got, "new") {
		t.Errorf("expected last block (containing 'new'), got: %s", got)
	}
}

func TestExtractPlanJSON_UnfencedJSON(t *testing.T) {
	response := "Here is the plan: [{\"date\":\"2026-04-13\",\"rest_day\":true}]"
	_, ok := extractPlanJSON(response)
	if ok {
		t.Fatal("expected not to extract unfenced JSON")
	}
}

// --- validatePlanUpdate tests ---

func buildValidPlanJSON(weekStart string) string {
	start, _ := time.Parse("2006-01-02", weekStart)
	var days []map[string]any
	for i := 0; i < 7; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		days = append(days, map[string]any{
			"date":     date,
			"rest_day": true,
		})
	}
	b, _ := json.Marshal(days)
	return string(b)
}

func TestValidatePlanUpdate_ValidSevenDays(t *testing.T) {
	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	planJSON := buildValidPlanJSON(weekStart)

	days, err := validatePlanUpdate(planJSON, weekStart, weekEnd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(days) != 7 {
		t.Fatalf("expected 7 days, got %d", len(days))
	}
}

func TestValidatePlanUpdate_WrongDateRange(t *testing.T) {
	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	// Build plan for a different week.
	planJSON := buildValidPlanJSON("2026-04-20")

	_, err := validatePlanUpdate(planJSON, weekStart, weekEnd)
	if err == nil {
		t.Fatal("expected error for out-of-range dates")
	}
}

func TestValidatePlanUpdate_DuplicateDates(t *testing.T) {
	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	start, _ := time.Parse("2006-01-02", weekStart)
	var days []map[string]any
	for i := 0; i < 6; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		days = append(days, map[string]any{"date": date, "rest_day": true})
	}
	// Duplicate the first date instead of adding the 7th.
	days = append(days, map[string]any{"date": weekStart, "rest_day": true})
	b, _ := json.Marshal(days)

	_, err := validatePlanUpdate(string(b), weekStart, weekEnd)
	if err == nil {
		t.Fatal("expected error for duplicate dates")
	}
}

func TestValidatePlanUpdate_NotSevenDays(t *testing.T) {
	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	start, _ := time.Parse("2006-01-02", weekStart)
	var days []map[string]any
	for i := 0; i < 5; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		days = append(days, map[string]any{"date": date, "rest_day": true})
	}
	b, _ := json.Marshal(days)

	_, err := validatePlanUpdate(string(b), weekStart, weekEnd)
	if err == nil {
		t.Fatal("expected error for non-7-day plan")
	}
}

// --- fakeExecCommand helpers for handler integration tests ---

func fakeExecCommandChat(lines []string) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		output := strings.Join(lines, "\n") + "\n"
		cmd := exec.CommandContext(ctx, "echo", "-n", output)
		return cmd
	}
}

func fakeExecCommandChatCapture(lines []string, captured *[][]string) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		*captured = append(*captured, args)
		output := strings.Join(lines, "\n") + "\n"
		cmd := exec.CommandContext(ctx, "echo", "-n", output)
		return cmd
	}
}

// TestStrideChatListHandler_Empty tests listing messages for a plan with no messages.
func TestStrideChatListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	planJSON := buildValidPlanJSON(weekStart)
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(`INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, created_at) VALUES (1, ?, ?, ?, ?)`,
		weekStart, weekEnd, planJSON, now)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	planID, _ := res.LastInsertId()

	handler := StrideChatListHandler(db)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/stride/plans/%d/chat", planID), nil)
	r = withUser(r, 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("planId", fmt.Sprintf("%d", planID))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Messages []ChatMessage `json:"messages"`
		PlanID   int64         `json:"plan_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(resp.Messages))
	}
	if resp.PlanID != planID {
		t.Errorf("expected plan_id %d, got %d", planID, resp.PlanID)
	}
}

// TestStrideChatListHandler_WrongUser tests that a plan belonging to another user returns 404.
func TestStrideChatListHandler_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'c@d.com', 'B', 'g-2')`)
	if err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	weekStart := "2026-04-13"
	planJSON := buildValidPlanJSON(weekStart)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, created_at) VALUES (1, ?, ?, ?, ?)`,
		weekStart, "2026-04-19", planJSON, now)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	planID, _ := res.LastInsertId()

	handler := StrideChatListHandler(db)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/stride/plans/%d/chat", planID), nil)
	// Request as user 2 — should not have access to user 1's plan.
	r = withUser(r, 2)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("planId", fmt.Sprintf("%d", planID))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestStrideChatSendHandler_Success tests streaming a message and getting a response.
func TestStrideChatSendHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	for _, kv := range [][2]string{
		{"claude_enabled", "true"},
	} {
		if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`, kv[0], kv[1]); err != nil {
			t.Fatalf("insert pref %s: %v", kv[0], err)
		}
	}

	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	planJSON := buildValidPlanJSON(weekStart)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, created_at) VALUES (1, ?, ?, ?, ?)`,
		weekStart, weekEnd, planJSON, now)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	planID, _ := res.LastInsertId()

	// Stub Claude CLI.
	origExec := execCommand
	execCommand = fakeExecCommandChat([]string{
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"Sure, I can "}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"help with that."}}`,
		`{"type":"result","result":"Sure, I can help with that.","session_id":"sess-abc","is_error":false}`,
	})
	t.Cleanup(func() { execCommand = origExec })

	handler := StrideChatSendHandler(db)
	rec := httptest.NewRecorder()
	reqBody := strings.NewReader(`{"content":"Can I move Thursday's run to Friday?"}`)
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/stride/plans/%d/chat", planID), reqBody)
	r.Header.Set("Content-Type", "application/json")
	r = withUser(r, 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("planId", fmt.Sprintf("%d", planID))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: user_message") {
		t.Errorf("expected user_message event, got: %s", body)
	}
	if !strings.Contains(body, "event: delta") {
		t.Errorf("expected delta events, got: %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("expected done event, got: %s", body)
	}

	// Verify messages persisted.
	msgs, err := ListChatMessages(db, planID, 1)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("first message should be user, got %s", msgs[0].Role)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("second message should be assistant, got %s", msgs[1].Role)
	}
	if msgs[1].PlanModified {
		t.Error("assistant message should not be plan_modified for a plain response")
	}

	// Verify session ID saved.
	sid, err := GetChatSessionID(db, planID, 1)
	if err != nil {
		t.Fatalf("get session id: %v", err)
	}
	if sid != "sess-abc" {
		t.Errorf("expected session_id 'sess-abc', got %q", sid)
	}
}

// TestStrideChatSendHandler_PlanModification tests that a response containing
// a valid fenced plan JSON block updates the plan and emits plan_updated.
func TestStrideChatSendHandler_PlanModification(t *testing.T) {
	db := setupTestDB(t)

	for _, kv := range [][2]string{
		{"claude_enabled", "true"},
	} {
		if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`, kv[0], kv[1]); err != nil {
			t.Fatalf("insert pref %s: %v", kv[0], err)
		}
	}

	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	planJSON := buildValidPlanJSON(weekStart)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, created_at) VALUES (1, ?, ?, ?, ?)`,
		weekStart, weekEnd, planJSON, now)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	planID, _ := res.LastInsertId()

	// Build a valid updated plan with a session on Wednesday.
	start, _ := time.Parse("2006-01-02", weekStart)
	var updatedDays []map[string]any
	for i := 0; i < 7; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		if i == 2 { // Wednesday — add a session.
			updatedDays = append(updatedDays, map[string]any{
				"date":     date,
				"rest_day": false,
				"session": map[string]any{
					"warmup":        "10 min easy",
					"main_set":      "5x1000m at threshold",
					"cooldown":      "10 min easy",
					"strides":       "",
					"target_hr_cap": 170,
					"description":   "Threshold intervals",
				},
			})
		} else {
			updatedDays = append(updatedDays, map[string]any{
				"date":     date,
				"rest_day": true,
			})
		}
	}
	updatedPlanBytes, _ := json.Marshal(updatedDays)
	updatedPlanStr := string(updatedPlanBytes)

	// Claude response with fenced plan JSON.
	fullResponse := fmt.Sprintf("I've moved the session to Wednesday. Here's the updated plan:\n```json\n%s\n```\nLet me know if that works.", updatedPlanStr)

	origExec := execCommand
	execCommand = fakeExecCommandChat([]string{
		fmt.Sprintf(`{"type":"result","result":%s,"session_id":"sess-mod","is_error":false}`, mustJSON(t, fullResponse)),
	})
	t.Cleanup(func() { execCommand = origExec })

	handler := StrideChatSendHandler(db)
	rec := httptest.NewRecorder()
	reqBody := strings.NewReader(`{"content":"Move the run to Wednesday"}`)
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/stride/plans/%d/chat", planID), reqBody)
	r.Header.Set("Content-Type", "application/json")
	r = withUser(r, 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("planId", fmt.Sprintf("%d", planID))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: plan_updated") {
		t.Errorf("expected plan_updated event, got: %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("expected done event, got: %s", body)
	}

	// Verify plan_json was updated in DB.
	plan, err := GetPlanByID(db, planID, 1)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	var days []DayPlan
	if err := json.Unmarshal(plan.Plan, &days); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	// Wednesday (index 2) should have a session.
	if days[2].RestDay {
		t.Error("expected Wednesday to not be a rest day after plan modification")
	}
	if days[2].Session == nil {
		t.Error("expected Wednesday to have a session after plan modification")
	}

	// Verify assistant message has plan_modified=true.
	msgs, err := ListChatMessages(db, planID, 1)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	var assistantMsg *ChatMessage
	for i, m := range msgs {
		if m.Role == "assistant" {
			assistantMsg = &msgs[i]
			break
		}
	}
	if assistantMsg == nil {
		t.Fatal("expected assistant message")
	}
	if !assistantMsg.PlanModified {
		t.Error("expected assistant message to have plan_modified=true")
	}
}

// TestStrideChatSendHandler_SessionResume tests that the session ID is passed
// via --resume on subsequent messages.
func TestStrideChatSendHandler_SessionResume(t *testing.T) {
	db := setupTestDB(t)

	for _, kv := range [][2]string{{"claude_enabled", "true"}} {
		if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`, kv[0], kv[1]); err != nil {
			t.Fatalf("insert pref %s: %v", kv[0], err)
		}
	}

	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	planJSON := buildValidPlanJSON(weekStart)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, chat_session_id, created_at) VALUES (1, ?, ?, ?, 'existing-sess', ?)`,
		weekStart, weekEnd, planJSON, now)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	planID, _ := res.LastInsertId()

	var captured [][]string
	origExec := execCommand
	execCommand = fakeExecCommandChatCapture([]string{
		`{"type":"result","result":"Got it.","session_id":"new-sess","is_error":false}`,
	}, &captured)
	t.Cleanup(func() { execCommand = origExec })

	handler := StrideChatSendHandler(db)
	rec := httptest.NewRecorder()
	reqBody := strings.NewReader(`{"content":"How am I doing?"}`)
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/stride/plans/%d/chat", planID), reqBody)
	r.Header.Set("Content-Type", "application/json")
	r = withUser(r, 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("planId", fmt.Sprintf("%d", planID))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify --resume was passed.
	if len(captured) == 0 {
		t.Fatal("expected at least one exec call")
	}
	args := captured[0]
	foundResume := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "existing-sess" {
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Errorf("expected --resume existing-sess in args: %v", args)
	}

	// Verify session updated.
	sid, err := GetChatSessionID(db, planID, 1)
	if err != nil {
		t.Fatalf("get session id: %v", err)
	}
	if sid != "new-sess" {
		t.Errorf("expected session_id 'new-sess', got %q", sid)
	}
}

// TestStrideChatSendHandler_ClaudeDisabled tests 400 when Claude is not enabled.
func TestStrideChatSendHandler_ClaudeDisabled(t *testing.T) {
	db := setupTestDB(t)

	// No claude_enabled preference — default is disabled.

	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	planJSON := buildValidPlanJSON(weekStart)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, created_at) VALUES (1, ?, ?, ?, ?)`,
		weekStart, weekEnd, planJSON, now)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	handler := StrideChatSendHandler(db)
	rec := httptest.NewRecorder()
	reqBody := strings.NewReader(`{"content":"Hello"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/stride/plans/1/chat", reqBody)
	r.Header.Set("Content-Type", "application/json")
	r = withUser(r, 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("planId", "1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestStrideChatSendHandler_PlanNotFound tests 404 for a non-existent plan.
func TestStrideChatSendHandler_PlanNotFound(t *testing.T) {
	db := setupTestDB(t)

	handler := StrideChatSendHandler(db)
	rec := httptest.NewRecorder()
	reqBody := strings.NewReader(`{"content":"Hello"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/stride/plans/999/chat", reqBody)
	r.Header.Set("Content-Type", "application/json")
	r = withUser(r, 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("planId", "999")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestStrideChatSendHandler_InvalidPlanJSON tests soft failure when Claude
// returns an invalid plan JSON (wrong dates). The message is still saved but
// no plan_updated event is emitted.
func TestStrideChatSendHandler_InvalidPlanJSON(t *testing.T) {
	db := setupTestDB(t)

	for _, kv := range [][2]string{{"claude_enabled", "true"}} {
		if _, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`, kv[0], kv[1]); err != nil {
			t.Fatalf("insert pref: %v", err)
		}
	}

	weekStart := "2026-04-13"
	weekEnd := "2026-04-19"
	planJSON := buildValidPlanJSON(weekStart)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO stride_plans (user_id, week_start, week_end, plan_json, created_at) VALUES (1, ?, ?, ?, ?)`,
		weekStart, weekEnd, planJSON, now)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	planID, _ := res.LastInsertId()

	// Claude returns plan JSON with wrong dates.
	wrongPlan := buildValidPlanJSON("2026-05-01") // Different week.
	fullResponse := fmt.Sprintf("Updated:\n```json\n%s\n```\n", wrongPlan)

	origExec := execCommand
	execCommand = fakeExecCommandChat([]string{
		fmt.Sprintf(`{"type":"result","result":%s,"session_id":"sess-bad","is_error":false}`, mustJSON(t, fullResponse)),
	})
	t.Cleanup(func() { execCommand = origExec })

	handler := StrideChatSendHandler(db)
	rec := httptest.NewRecorder()
	reqBody := strings.NewReader(`{"content":"Change all days"}`)
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/stride/plans/%d/chat", planID), reqBody)
	r.Header.Set("Content-Type", "application/json")
	r = withUser(r, 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("planId", fmt.Sprintf("%d", planID))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	// Should NOT have plan_updated event.
	if strings.Contains(body, "event: plan_updated") {
		t.Error("expected no plan_updated event for invalid plan JSON")
	}
	// Should still have done event.
	if !strings.Contains(body, "event: done") {
		t.Error("expected done event")
	}

	// Verify assistant message does not have plan_modified.
	msgs, err := ListChatMessages(db, planID, 1)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	for _, m := range msgs {
		if m.Role == "assistant" && m.PlanModified {
			t.Error("expected assistant message NOT to have plan_modified for invalid plan JSON")
		}
	}
}

// mustJSON marshals v to a JSON string, failing the test on error.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(b)
}
