package chat

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

func TestBuildPrompt(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "user", Content: "How are you?"},
	}

	prompt := buildPrompt(msgs)
	expected := "Human: Hello\n\nAssistant: Hi there!\n\nHuman: How are you?\n\n"
	if prompt != expected {
		t.Fatalf("unexpected prompt:\n%q\nexpected:\n%q", prompt, expected)
	}
}
