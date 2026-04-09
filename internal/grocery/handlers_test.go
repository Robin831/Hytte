package grocery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func withUser(r *http.Request, user *auth.User) *http.Request {
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func TestHandleAddAndList(t *testing.T) {
	db := setupTestDB(t)
	user := &auth.User{ID: 1, Email: "test@example.com", Name: "Test"}

	// Add an item.
	body := `{"content":"Milk","source_language":"en"}`
	req := httptest.NewRequest(http.MethodPost, "/api/grocery/items", bytes.NewBufferString(body))
	req = withUser(req, user)
	w := httptest.NewRecorder()
	HandleAdd(db)(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("HandleAdd status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var addResp struct {
		Item GroceryItem `json:"item"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &addResp); err != nil {
		t.Fatalf("unmarshal add response: %v", err)
	}
	if addResp.Item.Content != "Milk" {
		t.Errorf("got content %q, want %q", addResp.Item.Content, "Milk")
	}

	// List items.
	req = httptest.NewRequest(http.MethodGet, "/api/grocery/items", nil)
	req = withUser(req, user)
	w = httptest.NewRecorder()
	HandleList(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HandleList status = %d, want %d", w.Code, http.StatusOK)
	}

	var listResp struct {
		Items []GroceryItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if len(listResp.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(listResp.Items))
	}
}

func TestHandleAddEmptyContent(t *testing.T) {
	db := setupTestDB(t)
	user := &auth.User{ID: 1, Email: "test@example.com", Name: "Test"}

	body := `{"content":"  "}`
	req := httptest.NewRequest(http.MethodPost, "/api/grocery/items", bytes.NewBufferString(body))
	req = withUser(req, user)
	w := httptest.NewRecorder()
	HandleAdd(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("HandleAdd with empty content status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCheck(t *testing.T) {
	db := setupTestDB(t)
	user := &auth.User{ID: 1, Email: "test@example.com", Name: "Test"}

	created, err := Add(db, GroceryItem{HouseholdID: user.ID, Content: "Eggs", OriginalText: "Eggs", AddedBy: user.ID})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	body := `{"checked":true}`
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/grocery/items/%d/check", created.ID), bytes.NewBufferString(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fmt.Sprintf("%d", created.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withUser(req, user)
	w := httptest.NewRecorder()
	HandleCheck(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HandleCheck status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	items, err := ListByHousehold(db, user.ID)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	if !items[0].Checked {
		t.Error("item should be checked")
	}
}

func TestHandleClearCompleted(t *testing.T) {
	db := setupTestDB(t)
	user := &auth.User{ID: 1, Email: "test@example.com", Name: "Test"}

	item, err := Add(db, GroceryItem{HouseholdID: user.ID, Content: "Milk", OriginalText: "Milk", AddedBy: user.ID})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := UpdateChecked(db, item.ID, user.ID, true); err != nil {
		t.Fatalf("UpdateChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/grocery/completed", nil)
	req = withUser(req, user)
	w := httptest.NewRecorder()
	HandleClearCompleted(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HandleClearCompleted status = %d, want %d", w.Code, http.StatusOK)
	}

	items, err := ListByHousehold(db, user.ID)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("got %d items after clear, want 0", len(items))
	}
}
