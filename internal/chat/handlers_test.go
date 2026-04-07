package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

func withUser(r *http.Request) *http.Request {
	user := &auth.User{ID: 1, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func withURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/chat/conversations", nil)
	req = withUser(req)
	rec := httptest.NewRecorder()

	ListHandler(db)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string][]Conversation
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp["conversations"]) != 0 {
		t.Fatalf("expected empty list, got %d", len(resp["conversations"]))
	}
}

func TestCreateHandler(t *testing.T) {
	db := setupTestDB(t)

	body := `{"model": "claude-sonnet-4-6"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat/conversations", bytes.NewBufferString(body))
	req = withUser(req)
	rec := httptest.NewRecorder()

	CreateHandler(db)(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]*Conversation
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	convo := resp["conversation"]
	if convo == nil {
		t.Fatal("expected conversation in response")
	}
	if convo.Model != "claude-sonnet-4-6" {
		t.Fatalf("expected model claude-sonnet-4-6, got %q", convo.Model)
	}
}

func TestGetHandler(t *testing.T) {
	db := setupTestDB(t)

	convo, err := CreateConversation(db, 1, "Test", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/chat/conversations/"+strconv.FormatInt(convo.ID, 10), nil)
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	GetHandler(db)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Conversation *Conversation `json:"conversation"`
		Messages     []Message     `json:"messages"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Conversation.ID != convo.ID {
		t.Fatalf("wrong conversation ID")
	}
	if len(resp.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(resp.Messages))
	}
}

func TestGetHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/chat/conversations/9999", nil)
	req = withUser(req)
	req = withURLParam(req, "id", "9999")
	rec := httptest.NewRecorder()

	GetHandler(db)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteHandler(t *testing.T) {
	db := setupTestDB(t)

	convo, _ := CreateConversation(db, 1, "To Delete", "claude-sonnet-4-6")

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	DeleteHandler(db)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req = withUser(req)
	req = withURLParam(req, "id", "9999")
	rec := httptest.NewRecorder()

	DeleteHandler(db)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRenameHandler(t *testing.T) {
	db := setupTestDB(t)

	convo, _ := CreateConversation(db, 1, "", "claude-sonnet-4-6")

	body := `{"title": "Renamed Chat"}`
	req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	RenameHandler(db)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]*Conversation
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["conversation"].Title != "Renamed Chat" {
		t.Fatalf("expected 'Renamed Chat', got %q", resp["conversation"].Title)
	}
}

func TestRenameHandler_EmptyTitle(t *testing.T) {
	db := setupTestDB(t)

	convo, _ := CreateConversation(db, 1, "Original", "claude-sonnet-4-6")

	body := `{"title": ""}`
	req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	RenameHandler(db)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRenameHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	body := `{"title": "Renamed"}`
	req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", "9999")
	rec := httptest.NewRecorder()

	RenameHandler(db)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSendMessageHandler_Success(t *testing.T) {
	d := setupTestDB(t)

	// Insert Claude preferences so LoadClaudeConfig returns Enabled=true.
	_, err := d.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true'), (1, 'claude_model', 'claude-sonnet-4-6')`,
	)
	if err != nil {
		t.Fatalf("set prefs: %v", err)
	}

	convo, err := CreateConversation(d, 1, "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Replace runPromptWithSessionFn with a stub that returns a known response.
	orig := runPromptWithSessionFn
	runPromptWithSessionFn = func(_ context.Context, _ *training.ClaudeConfig, _, _ string) (*training.SessionResult, error) {
		return &training.SessionResult{Response: "Hello from Claude!", SessionID: "test-session-123"}, nil
	}
	t.Cleanup(func() { runPromptWithSessionFn = orig })

	body := `{"content": "Hello!"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	SendMessageHandler(d)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		UserMsg      *Message `json:"user_message"`
		AssistantMsg *Message `json:"assistant_message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.UserMsg == nil || resp.UserMsg.Content != "Hello!" {
		t.Fatalf("unexpected user message: %+v", resp.UserMsg)
	}
	if resp.AssistantMsg == nil || resp.AssistantMsg.Content != "Hello from Claude!" {
		t.Fatalf("unexpected assistant message: %+v", resp.AssistantMsg)
	}

	// Verify session ID was stored on the conversation.
	convoAfter, err := GetConversation(d, convo.ID, 1)
	if err != nil {
		t.Fatalf("get conversation after send: %v", err)
	}
	if convoAfter.SessionID != "test-session-123" {
		t.Fatalf("expected session_id 'test-session-123', got %q", convoAfter.SessionID)
	}
}

func TestSendMessageHandler_ClaudeNotEnabled(t *testing.T) {
	d := setupTestDB(t)

	// claude_enabled not set → disabled.
	convo, _ := CreateConversation(d, 1, "Test", "claude-sonnet-4-6")

	body := `{"content": "Hello!"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	SendMessageHandler(d)(rec, req)

	// LoadClaudeConfig may fail (no CLI path) or return Enabled=false.
	// Either way, we expect a 4xx/5xx non-200 status.
	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 when Claude is not configured, got 200")
	}
}

func TestSendMessageHandler_ClaudeError(t *testing.T) {
	d := setupTestDB(t)

	_, err := d.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true'), (1, 'claude_model', 'claude-sonnet-4-6')`,
	)
	if err != nil {
		t.Fatalf("set prefs: %v", err)
	}

	convo, _ := CreateConversation(d, 1, "", "claude-sonnet-4-6")

	orig := runPromptWithSessionFn
	runPromptWithSessionFn = func(_ context.Context, _ *training.ClaudeConfig, _, _ string) (*training.SessionResult, error) {
		return nil, errors.New("claude CLI unavailable")
	}
	t.Cleanup(func() { runPromptWithSessionFn = orig })

	body := `{"content": "Hello!"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	SendMessageHandler(d)(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on Claude error, got %d", rec.Code)
	}
}

func TestSendMessageHandler_ConversationNotFound(t *testing.T) {
	d := setupTestDB(t)

	body := `{"content": "Hello!"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", "9999")
	rec := httptest.NewRecorder()

	SendMessageHandler(d)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSendMessageHandler_EmptyContent(t *testing.T) {
	d := setupTestDB(t)

	body := `{"content": ""}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", "1")
	rec := httptest.NewRecorder()

	SendMessageHandler(d)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSendMessageHandler_SessionResumption(t *testing.T) {
	d := setupTestDB(t)

	_, err := d.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true'), (1, 'claude_model', 'claude-sonnet-4-6')`,
	)
	if err != nil {
		t.Fatalf("set prefs: %v", err)
	}

	convo, err := CreateConversation(d, 1, "Resume Test", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Set session_id on the conversation to simulate a prior exchange.
	if err := UpdateSessionID(d, convo.ID, 1, "existing-session-456"); err != nil {
		t.Fatalf("update session id: %v", err)
	}

	// Stub verifies that the session ID is passed through.
	var receivedSessionID string
	orig := runPromptWithSessionFn
	runPromptWithSessionFn = func(_ context.Context, _ *training.ClaudeConfig, _, sid string) (*training.SessionResult, error) {
		receivedSessionID = sid
		return &training.SessionResult{Response: "Resumed!", SessionID: "existing-session-456"}, nil
	}
	t.Cleanup(func() { runPromptWithSessionFn = orig })

	body := `{"content": "Follow-up question"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	SendMessageHandler(d)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if receivedSessionID != "existing-session-456" {
		t.Fatalf("expected session ID 'existing-session-456' to be passed, got %q", receivedSessionID)
	}
}

func TestSendMessageHandler_SessionExpiredFallback(t *testing.T) {
	d := setupTestDB(t)

	_, err := d.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true'), (1, 'claude_model', 'claude-sonnet-4-6')`,
	)
	if err != nil {
		t.Fatalf("set prefs: %v", err)
	}

	convo, err := CreateConversation(d, 1, "Expired Test", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := UpdateSessionID(d, convo.ID, 1, "expired-session"); err != nil {
		t.Fatalf("update session id: %v", err)
	}

	// First call with session ID fails; second call without session ID succeeds.
	callCount := 0
	orig := runPromptWithSessionFn
	runPromptWithSessionFn = func(_ context.Context, _ *training.ClaudeConfig, _, sid string) (*training.SessionResult, error) {
		callCount++
		if sid != "" {
			return nil, errors.New("session not found")
		}
		return &training.SessionResult{Response: "Fresh start!", SessionID: "new-session-789"}, nil
	}
	t.Cleanup(func() { runPromptWithSessionFn = orig })

	body := `{"content": "Hello again"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	SendMessageHandler(d)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if callCount != 2 {
		t.Fatalf("expected 2 calls (retry after session failure), got %d", callCount)
	}

	// Verify new session ID was saved.
	convoAfter, err := GetConversation(d, convo.ID, 1)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if convoAfter.SessionID != "new-session-789" {
		t.Fatalf("expected new session ID 'new-session-789', got %q", convoAfter.SessionID)
	}
}
