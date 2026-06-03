package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestCreateHandler_InvalidModel(t *testing.T) {
	db := setupTestDB(t)

	body := `{"model": "gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat/conversations", bytes.NewBufferString(body))
	req = withUser(req)
	rec := httptest.NewRecorder()

	CreateHandler(db)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify no conversation was created.
	convos, err := ListConversations(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(convos) != 0 {
		t.Fatalf("expected no conversations after invalid model rejection, got %d", len(convos))
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

func TestRenameHandler_UpdateModel(t *testing.T) {
	db := setupTestDB(t)

	convo, _ := CreateConversation(db, 1, "Keep Title", "claude-sonnet-4-6")

	body := `{"model": "claude-opus-4-8"}`
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
	if resp["conversation"].Model != "claude-opus-4-8" {
		t.Fatalf("expected model 'claude-opus-4-8', got %q", resp["conversation"].Model)
	}
	// Title must be left untouched when only the model is supplied.
	if resp["conversation"].Title != "Keep Title" {
		t.Fatalf("expected title preserved as 'Keep Title', got %q", resp["conversation"].Title)
	}

	// Verify persistence.
	after, err := GetConversation(db, convo.ID, 1)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if after.Model != "claude-opus-4-8" {
		t.Fatalf("expected persisted model 'claude-opus-4-8', got %q", after.Model)
	}
}

func TestRenameHandler_InvalidModel(t *testing.T) {
	db := setupTestDB(t)

	convo, _ := CreateConversation(db, 1, "Original", "claude-sonnet-4-6")

	body := `{"model": "gpt-4"}`
	req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	RenameHandler(db)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	// Model must be left untouched on rejection.
	after, err := GetConversation(db, convo.ID, 1)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if after.Model != "claude-sonnet-4-6" {
		t.Fatalf("expected model unchanged 'claude-sonnet-4-6', got %q", after.Model)
	}
}

func TestRenameHandler_TitleAndModel(t *testing.T) {
	db := setupTestDB(t)

	convo, _ := CreateConversation(db, 1, "", "claude-sonnet-4-6")

	body := `{"title": "New Title", "model": "claude-haiku-4-5"}`
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
	if resp["conversation"].Title != "New Title" {
		t.Fatalf("expected title 'New Title', got %q", resp["conversation"].Title)
	}
	if resp["conversation"].Model != "claude-haiku-4-5" {
		t.Fatalf("expected model 'claude-haiku-4-5', got %q", resp["conversation"].Model)
	}
}

func TestRenameHandler_NoFields(t *testing.T) {
	db := setupTestDB(t)

	convo, _ := CreateConversation(db, 1, "Original", "claude-sonnet-4-6")

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))
	rec := httptest.NewRecorder()

	RenameHandler(db)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when neither title nor model supplied, got %d", rec.Code)
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
	preSendUpdatedAt := convo.UpdatedAt

	// Replace runPromptWithSessionFn with a stub that returns a known response.
	orig := runPromptWithSessionFn
	runPromptWithSessionFn = func(_ context.Context, _ *training.ClaudeConfig, _, _ string) (*training.SessionResult, error) {
		return &training.SessionResult{Response: "Hello from Claude!", SessionID: "test-session-123"}, nil
	}
	t.Cleanup(func() { runPromptWithSessionFn = orig })

	// Stub the auto-title prompt so we don't shell out to a real CLI and so we
	// can assert that the returned conversation reflects the generated title.
	origPrompt := runPromptFn
	runPromptFn = func(_ context.Context, _ *training.ClaudeConfig, _ string) (string, error) {
		return "  Generated Title  ", nil
	}
	t.Cleanup(func() { runPromptFn = origPrompt })

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
		UserMsg      *Message      `json:"user_message"`
		AssistantMsg *Message      `json:"assistant_message"`
		Conversation *Conversation `json:"conversation"`
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
	if resp.Conversation == nil {
		t.Fatal("expected conversation in response, got nil")
	}
	if resp.Conversation.ID != convo.ID {
		t.Fatalf("expected conversation ID %d, got %d", convo.ID, resp.Conversation.ID)
	}
	if resp.Conversation.Title == "" {
		t.Fatal("expected non-empty auto-generated title on first send, got empty")
	}
	if resp.Conversation.Title != "Generated Title" {
		t.Fatalf("expected sanitised title 'Generated Title', got %q", resp.Conversation.Title)
	}
	if resp.Conversation.UpdatedAt == "" {
		t.Fatal("expected non-empty updated_at on returned conversation")
	}
	if resp.Conversation.UpdatedAt < preSendUpdatedAt {
		t.Fatalf("expected updated_at to advance, pre=%q post=%q", preSendUpdatedAt, resp.Conversation.UpdatedAt)
	}

	// Verify session ID was stored on the conversation.
	convoAfter, err := GetConversation(d, convo.ID, 1)
	if err != nil {
		t.Fatalf("get conversation after send: %v", err)
	}
	if convoAfter.SessionID != "test-session-123" {
		t.Fatalf("expected session_id 'test-session-123', got %q", convoAfter.SessionID)
	}
	if convoAfter.Title != "Generated Title" {
		t.Fatalf("expected stored title to be auto-generated, got %q", convoAfter.Title)
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

// flushRecorder wraps httptest.ResponseRecorder and adds the no-op Flush so
// the SSE handler's Flusher assertion succeeds in tests.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
}

// parseSSE splits a recorder body into ordered (event, data) frames.
func parseSSE(body string) [][2]string {
	var out [][2]string
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		var event, data string
		for _, line := range strings.Split(frame, "\n") {
			switch {
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				if data != "" {
					data += "\n"
				}
				data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		out = append(out, [2]string{event, data})
	}
	return out
}

func TestStreamMessageHandler_HappyPath(t *testing.T) {
	d := setupTestDB(t)
	if _, err := d.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true'), (1, 'claude_model', 'claude-sonnet-4-6')`,
	); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	convo, err := CreateConversation(d, 1, "Initial", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	orig := runPromptWithSessionStreamFn
	runPromptWithSessionStreamFn = func(_ context.Context, _ *training.ClaudeConfig, prompt, sid string, onChunk func(string), onSession func(string)) (string, error) {
		if prompt != "Hello!" {
			t.Errorf("unexpected prompt: %q", prompt)
		}
		if sid != "" {
			t.Errorf("expected empty session id on first call, got %q", sid)
		}
		onChunk("Hello ")
		onChunk("from ")
		onChunk("Claude!")
		onSession("sess-abc")
		return "Hello from Claude!", nil
	}
	t.Cleanup(func() { runPromptWithSessionStreamFn = orig })

	rec := newFlushRecorder()
	body := `{"content": "Hello!"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))

	StreamMessageHandler(d)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected SSE content type, got %q", ct)
	}

	frames := parseSSE(rec.Body.String())
	var (
		gotUserMsg, gotDone bool
		tokens              []string
	)
	for _, f := range frames {
		switch f[0] {
		case "user_message":
			gotUserMsg = true
		case "token":
			var d struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(f[1]), &d); err != nil {
				t.Fatalf("token JSON: %v", err)
			}
			tokens = append(tokens, d.Text)
		case "done":
			gotDone = true
			var d struct {
				AssistantMessage *Message `json:"assistant_message"`
			}
			if err := json.Unmarshal([]byte(f[1]), &d); err != nil {
				t.Fatalf("done JSON: %v", err)
			}
			if d.AssistantMessage == nil || d.AssistantMessage.Content != "Hello from Claude!" {
				t.Fatalf("unexpected assistant message: %+v", d.AssistantMessage)
			}
		case "error":
			t.Fatalf("unexpected error frame: %s", f[1])
		}
	}
	if !gotUserMsg {
		t.Error("expected user_message event")
	}
	if !gotDone {
		t.Error("expected done event")
	}
	if strings.Join(tokens, "") != "Hello from Claude!" {
		t.Errorf("token concatenation = %q, want %q", strings.Join(tokens, ""), "Hello from Claude!")
	}

	// Verify the assistant row was persisted and session id stored.
	msgs, err := GetMessages(d, convo.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages persisted, got %d", len(msgs))
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Hello from Claude!" {
		t.Fatalf("unexpected assistant message in DB: %+v", msgs[1])
	}
	updated, err := GetConversation(d, convo.ID, 1)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if updated.SessionID != "sess-abc" {
		t.Errorf("expected session id 'sess-abc', got %q", updated.SessionID)
	}
}

func TestStreamMessageHandler_CLIError(t *testing.T) {
	d := setupTestDB(t)
	if _, err := d.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true'), (1, 'claude_model', 'claude-sonnet-4-6')`,
	); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	convo, err := CreateConversation(d, 1, "Err", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	orig := runPromptWithSessionStreamFn
	runPromptWithSessionStreamFn = func(_ context.Context, _ *training.ClaudeConfig, _, _ string, onChunk func(string), _ func(string)) (string, error) {
		onChunk("partial...")
		return "", errors.New("claude exploded")
	}
	t.Cleanup(func() { runPromptWithSessionStreamFn = orig })

	rec := newFlushRecorder()
	body := `{"content": "Will fail"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))

	StreamMessageHandler(d)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (SSE), got %d", rec.Code)
	}
	frames := parseSSE(rec.Body.String())
	var errorFrames int
	for _, f := range frames {
		if f[0] == "error" {
			errorFrames++
		}
		if f[0] == "done" {
			t.Fatalf("unexpected done frame after CLI error")
		}
	}
	if errorFrames != 1 {
		t.Errorf("expected exactly one error frame, got %d", errorFrames)
	}

	// User message persisted; assistant row must NOT exist.
	msgs, err := GetMessages(d, convo.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	var assistantCount int
	for _, m := range msgs {
		if m.Role == "assistant" {
			assistantCount++
		}
	}
	if assistantCount != 0 {
		t.Errorf("expected no assistant row on CLI error, got %d", assistantCount)
	}
}

func TestStreamMessageHandler_ClientDisconnect(t *testing.T) {
	d := setupTestDB(t)
	if _, err := d.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true'), (1, 'claude_model', 'claude-sonnet-4-6')`,
	); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	convo, err := CreateConversation(d, 1, "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Fake stream that observes ctx cancellation: writes one chunk, then
	// blocks until the caller cancels and returns ctx.Err().
	var (
		started sync.WaitGroup
		seenCtx context.Context
	)
	started.Add(1)

	orig := runPromptWithSessionStreamFn
	runPromptWithSessionStreamFn = func(ctx context.Context, _ *training.ClaudeConfig, _, _ string, onChunk func(string), _ func(string)) (string, error) {
		seenCtx = ctx
		onChunk("partial")
		started.Done()
		<-ctx.Done()
		return "", ctx.Err()
	}
	t.Cleanup(func() { runPromptWithSessionStreamFn = orig })

	rec := newFlushRecorder()
	body := `{"content": "Hi"}`
	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = req.WithContext(reqCtx)
	req = withUser(req)
	req = withURLParam(req, "id", strconv.FormatInt(convo.ID, 10))

	done := make(chan struct{})
	go func() {
		StreamMessageHandler(d)(rec, req)
		close(done)
	}()

	started.Wait()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after context cancel")
	}

	if seenCtx == nil {
		t.Fatal("handler never invoked the stream stub")
	}

	// On client disconnect, no assistant row may be written.
	msgs, err := GetMessages(d, convo.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	for _, m := range msgs {
		if m.Role == "assistant" {
			t.Errorf("expected no assistant row on disconnect, found one")
		}
	}
}

func TestStreamMessageHandler_EmptyContent(t *testing.T) {
	d := setupTestDB(t)
	rec := newFlushRecorder()
	body := `{"content": ""}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", "1")

	StreamMessageHandler(d)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestStreamMessageHandler_NotFound(t *testing.T) {
	d := setupTestDB(t)
	if _, err := d.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'claude_enabled', 'true'), (1, 'claude_model', 'claude-sonnet-4-6')`,
	); err != nil {
		t.Fatalf("set prefs: %v", err)
	}

	rec := newFlushRecorder()
	body := `{"content": "Hi"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withUser(req)
	req = withURLParam(req, "id", "9999")

	StreamMessageHandler(d)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
