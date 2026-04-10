package homework

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
