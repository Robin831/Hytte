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
