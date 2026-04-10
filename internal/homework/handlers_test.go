package homework

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
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

func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func decode(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, body)
	}
}

var testParent = &auth.User{ID: 1, Email: "parent@test.com", Name: "Parent"}

func TestHandleGetProfileEmpty(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	handler := HandleGetProfile(d)
	r := withChiParams(withUser(newRequest(http.MethodGet, "/api/homework/children/2/profile", nil), testParent), map[string]string{"childId": "2"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	decode(t, w.Body.Bytes(), &resp)
	if resp["profile"] != nil {
		t.Errorf("expected null profile, got %v", resp["profile"])
	}
}

func TestHandleGetProfileForbidden(t *testing.T) {
	d := setupTestDB(t)
	// No family link — parent 1 does not own kid 2.

	handler := HandleGetProfile(d)
	r := withChiParams(withUser(newRequest(http.MethodGet, "/api/homework/children/2/profile", nil), testParent), map[string]string{"childId": "2"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateProfileCreateAndUpdate(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	handler := HandleUpdateProfile(d)

	// Create profile (no existing row).
	body := map[string]any{
		"age":                10,
		"grade_level":        "5th",
		"subjects":           []string{"math"},
		"preferred_language": "nb",
		"school_name":        "Test School",
		"current_topics":     []string{"fractions"},
	}
	r := withChiParams(withUser(newRequest(http.MethodPut, "/api/homework/children/2/profile", body), testParent), map[string]string{"childId": "2"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Profile HomeworkProfile `json:"profile"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if resp.Profile.Age != 10 {
		t.Errorf("expected age 10, got %d", resp.Profile.Age)
	}
	if resp.Profile.SchoolName != "Test School" {
		t.Errorf("expected school 'Test School', got %q", resp.Profile.SchoolName)
	}

	// Update existing profile.
	body["age"] = 11
	body["school_name"] = "New School"
	r = withChiParams(withUser(newRequest(http.MethodPut, "/api/homework/children/2/profile", body), testParent), map[string]string{"childId": "2"})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", w.Code, w.Body.String())
	}

	decode(t, w.Body.Bytes(), &resp)
	if resp.Profile.Age != 11 {
		t.Errorf("expected age 11, got %d", resp.Profile.Age)
	}
	if resp.Profile.SchoolName != "New School" {
		t.Errorf("expected school 'New School', got %q", resp.Profile.SchoolName)
	}
}

func TestHandleUpdateProfileInvalidJSON(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	handler := HandleUpdateProfile(d)
	r := withChiParams(withUser(httptest.NewRequest(http.MethodPut, "/api/homework/children/2/profile", bytes.NewBufferString("not json")), testParent), map[string]string{"childId": "2"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNewConversation(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	handler := HandleNewConversation(d)
	body := map[string]any{"subject": "Math homework"}
	r := withChiParams(withUser(newRequest(http.MethodPost, "/api/homework/children/2/conversations", body), testParent), map[string]string{"childId": "2"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Conversation HomeworkConversation `json:"conversation"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if resp.Conversation.Subject != "Math homework" {
		t.Errorf("expected subject 'Math homework', got %q", resp.Conversation.Subject)
	}
	if resp.Conversation.KidID != 2 {
		t.Errorf("expected kid_id 2, got %d", resp.Conversation.KidID)
	}
}

func TestHandleNewConversationNoSubject(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	handler := HandleNewConversation(d)
	r := withChiParams(withUser(newRequest(http.MethodPost, "/api/homework/children/2/conversations", map[string]any{}), testParent), map[string]string{"childId": "2"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNewConversationEmptyBody(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	handler := HandleNewConversation(d)
	r := withChiParams(withUser(httptest.NewRequest(http.MethodPost, "/api/homework/children/2/conversations", nil), testParent), map[string]string{"childId": "2"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for empty body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListConversations(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	// Create two conversations.
	if _, err := CreateConversation(d, HomeworkConversation{KidID: 2, Subject: "Math"}); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := CreateConversation(d, HomeworkConversation{KidID: 2, Subject: "Science"}); err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	handler := HandleListConversations(d)
	r := withChiParams(withUser(newRequest(http.MethodGet, "/api/homework/children/2/conversations", nil), testParent), map[string]string{"childId": "2"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Conversations []HomeworkConversation `json:"conversations"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Conversations) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(resp.Conversations))
	}
}

func TestHandleGetConversation(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	conv, err := CreateConversation(d, HomeworkConversation{KidID: 2, Subject: "Reading"})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	// Add a message.
	if _, err := AddMessage(d, HomeworkMessage{ConversationID: conv.ID, Role: "user", Content: "Help me", HelpLevel: HelpLevelHint}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	handler := HandleGetConversation(d)
	r := withChiParams(withUser(newRequest(http.MethodGet, "/api/homework/children/2/conversations/1", nil), testParent), map[string]string{"childId": "2", "id": "1"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Conversation HomeworkConversation `json:"conversation"`
		Messages     []HomeworkMessage    `json:"messages"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if resp.Conversation.Subject != "Reading" {
		t.Errorf("expected subject 'Reading', got %q", resp.Conversation.Subject)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(resp.Messages))
	}
	if resp.Messages[0].Content != "Help me" {
		t.Errorf("expected content 'Help me', got %q", resp.Messages[0].Content)
	}
}

func TestHandleGetConversationNotFound(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	handler := HandleGetConversation(d)
	r := withChiParams(withUser(newRequest(http.MethodGet, "/api/homework/children/2/conversations/999", nil), testParent), map[string]string{"childId": "2", "id": "999"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleInvalidChildID(t *testing.T) {
	d := setupTestDB(t)

	handler := HandleGetProfile(d)
	r := withChiParams(withUser(newRequest(http.MethodGet, "/api/homework/children/abc/profile", nil), testParent), map[string]string{"childId": "abc"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// setupSendMessageTest sets up the DB with a family link, profile, user prefs,
// and a conversation, returning the DB and conversation.
func setupSendMessageTest(t *testing.T) (*httptest.ResponseRecorder, http.HandlerFunc, HomeworkConversation) {
	t.Helper()
	d := setupTestDB(t)

	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}

	// Create homework profile.
	_, err = CreateProfile(d, HomeworkProfile{
		KidID:      2,
		Age:        10,
		GradeLevel: "5th",
		Subjects:   []string{"math"},
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}

	// Set up Claude config in user_preferences.
	_, err = d.Exec(`INSERT INTO user_preferences (user_id, pref_key, pref_value) VALUES (1, 'claude_enabled', 'true')`)
	if err != nil {
		t.Fatalf("insert claude_enabled pref: %v", err)
	}

	conv, err := CreateConversation(d, HomeworkConversation{KidID: 2, Subject: ""})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	handler := HandleSendMessage(d)
	rec := httptest.NewRecorder()
	return rec, handler, conv
}

// multipartBody builds a multipart form body with a message field and optional image.
func multipartBody(t *testing.T, fields map[string]string, imageData []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if imageData != nil {
		part, err := writer.CreateFormFile("image", "photo.jpg")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		part.Write(imageData)
	}
	writer.Close()
	return &buf, writer.FormDataContentType()
}

// fakeExecCommand returns a function that creates a fake exec.Cmd
// which writes the given stream-json lines to stdout.
func fakeExecCommand(lines []string) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		output := strings.Join(lines, "\n") + "\n"
		cmd := exec.CommandContext(ctx, "echo", "-n", output)
		return cmd
	}
}

func TestHandleSendMessageSuccess(t *testing.T) {
	rec, handler, conv := setupSendMessageTest(t)

	// Stub the Claude CLI execution with stream-json output.
	origExec := execCommand
	execCommand = fakeExecCommand([]string{
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"Let me help "}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"you with that."}}`,
		fmt.Sprintf(`{"type":"result","result":"Let me help you with that.","session_id":"sess-123","is_error":false}`),
	})
	t.Cleanup(func() { execCommand = origExec })

	body, contentType := multipartBody(t, map[string]string{
		"message":    "Help me solve this equation: 2x + 3 = 7",
		"help_level": "hint",
	}, nil)

	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/homework/children/2/conversations/%d/messages", conv.ID), body)
	r.Header.Set("Content-Type", contentType)
	r = withChiParams(withUser(r, testParent), map[string]string{"childId": "2", "id": fmt.Sprintf("%d", conv.ID)})

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}

	respBody := rec.Body.String()
	if !strings.Contains(respBody, "event: user_message") {
		t.Errorf("expected user_message event, got: %s", respBody)
	}
	if !strings.Contains(respBody, "event: done") {
		t.Errorf("expected done event, got: %s", respBody)
	}
	if !strings.Contains(respBody, "event: delta") {
		t.Errorf("expected delta events, got: %s", respBody)
	}
}

func TestHandleSendMessageMissingMessage(t *testing.T) {
	rec, handler, conv := setupSendMessageTest(t)

	body, contentType := multipartBody(t, map[string]string{
		"message": "",
	}, nil)

	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/homework/children/2/conversations/%d/messages", conv.ID), body)
	r.Header.Set("Content-Type", contentType)
	r = withChiParams(withUser(r, testParent), map[string]string{"childId": "2", "id": fmt.Sprintf("%d", conv.ID)})

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSendMessageInvalidHelpLevel(t *testing.T) {
	rec, handler, conv := setupSendMessageTest(t)

	body, contentType := multipartBody(t, map[string]string{
		"message":    "Help me",
		"help_level": "invalid",
	}, nil)

	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/homework/children/2/conversations/%d/messages", conv.ID), body)
	r.Header.Set("Content-Type", contentType)
	r = withChiParams(withUser(r, testParent), map[string]string{"childId": "2", "id": fmt.Sprintf("%d", conv.ID)})

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSendMessageNotFound(t *testing.T) {
	rec, handler, _ := setupSendMessageTest(t)

	body, contentType := multipartBody(t, map[string]string{
		"message": "Help me",
	}, nil)

	r := httptest.NewRequest(http.MethodPost, "/api/homework/children/2/conversations/999/messages", body)
	r.Header.Set("Content-Type", contentType)
	r = withChiParams(withUser(r, testParent), map[string]string{"childId": "2", "id": "999"})

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSendMessageForbidden(t *testing.T) {
	d := setupTestDB(t)
	// No family link for parent 1 -> child 3.

	handler := HandleSendMessage(d)
	body, contentType := multipartBody(t, map[string]string{"message": "Help"}, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/homework/children/3/conversations/1/messages", body)
	r.Header.Set("Content-Type", contentType)
	r = withChiParams(withUser(r, testParent), map[string]string{"childId": "3", "id": "1"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSendMessageWithImage(t *testing.T) {
	d := setupTestDB(t)

	_, err := d.Exec(`INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at) VALUES (1, 2, 'Kid', '📚', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("insert family link: %v", err)
	}
	_, err = d.Exec(`INSERT INTO user_preferences (user_id, pref_key, pref_value) VALUES (1, 'claude_enabled', 'true')`)
	if err != nil {
		t.Fatalf("insert pref: %v", err)
	}
	conv, err := CreateConversation(d, HomeworkConversation{KidID: 2})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	// Stub the Claude CLI.
	origExec := execCommand
	execCommand = fakeExecCommand([]string{
		`{"type":"result","result":"I can see the image.","session_id":"sess-img","is_error":false}`,
	})
	t.Cleanup(func() { execCommand = origExec })

	// Create a minimal valid JPEG (starts with FF D8 FF).
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	body, contentType := multipartBody(t, map[string]string{
		"message": "What does this show?",
	}, jpegData)

	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/homework/children/2/conversations/%d/messages", conv.ID), body)
	r.Header.Set("Content-Type", contentType)
	r = withChiParams(withUser(r, testParent), map[string]string{"childId": "2", "id": fmt.Sprintf("%d", conv.ID)})

	rec := httptest.NewRecorder()
	HandleSendMessage(d).ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify user message was persisted with image path.
	msgs, err := GetMessages(d, conv.ID, 2)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) < 1 {
		t.Fatal("expected at least 1 message")
	}
	// Clean up temp file.
	if msgs[0].ImagePath != "" {
		os.Remove(msgs[0].ImagePath)
	}
}

func TestHandleSendMessageDefaultHelpLevel(t *testing.T) {
	rec, handler, conv := setupSendMessageTest(t)

	origExec := execCommand
	execCommand = fakeExecCommand([]string{
		`{"type":"result","result":"Here is a hint.","session_id":"sess-def","is_error":false}`,
	})
	t.Cleanup(func() { execCommand = origExec })

	// No help_level specified — should default to "hint".
	body, contentType := multipartBody(t, map[string]string{
		"message": "Help me with reading comprehension",
	}, nil)

	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/homework/children/2/conversations/%d/messages", conv.ID), body)
	r.Header.Set("Content-Type", contentType)
	r = withChiParams(withUser(r, testParent), map[string]string{"childId": "2", "id": fmt.Sprintf("%d", conv.ID)})

	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	respBody := rec.Body.String()
	if !strings.Contains(respBody, "event: done") {
		t.Errorf("expected done event in response")
	}
}
