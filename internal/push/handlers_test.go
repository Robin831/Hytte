package push

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
)

func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func TestVAPIDKeyHandler(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest("GET", "/api/push/vapid-key", nil)
	rec := httptest.NewRecorder()
	VAPIDKeyHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["public_key"] == "" {
		t.Error("public_key is empty")
	}
	if _, hasPrivate := body["private_key"]; hasPrivate {
		t.Error("private key must not be exposed in VAPID endpoint response")
	}
}

func TestSubscribeHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"endpoint":"https://push.example.com/sub1","keys":{"p256dh":"key1","auth":"auth1"}}`
	req := withUser(httptest.NewRequest("POST", "/api/push/subscribe", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	SubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["endpoint"] != "https://push.example.com/sub1" {
		t.Errorf("endpoint = %q", body["endpoint"])
	}
	// Verify cryptographic keys are not exposed in the response.
	if _, has := body["p256dh"]; has {
		t.Error("p256dh must not be exposed in subscribe response")
	}
	if _, has := body["auth"]; has {
		t.Error("auth secret must not be exposed in subscribe response")
	}
}

func TestSubscribeHandler_MissingFields(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"endpoint":"https://push.example.com/sub1"}`
	req := withUser(httptest.NewRequest("POST", "/api/push/subscribe", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	SubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSubscribeHandler_Unauthorized(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"endpoint":"https://push.example.com/sub1","keys":{"p256dh":"key1","auth":"auth1"}}`
	req := httptest.NewRequest("POST", "/api/push/subscribe", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	SubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestUnsubscribeHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	// First subscribe.
	_, err := SaveSubscription(db, 1, "https://push.example.com/sub1", "key", "auth")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	payload := `{"endpoint":"https://push.example.com/sub1"}`
	req := withUser(httptest.NewRequest("DELETE", "/api/push/subscribe", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	UnsubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUnsubscribeHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"endpoint":"https://push.example.com/nonexistent"}`
	req := withUser(httptest.NewRequest("DELETE", "/api/push/subscribe", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	UnsubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSubscriptionsListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/push/subscriptions", nil), 1)
	rec := httptest.NewRecorder()
	SubscriptionsListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Subscriptions []Subscription `json:"subscriptions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Subscriptions) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(body.Subscriptions))
	}
}

func TestSubscriptionsListHandler_WithData(t *testing.T) {
	db := setupTestDB(t)

	_, _ = SaveSubscription(db, 1, "https://push.example.com/sub1", "key1", "auth1")
	_, _ = SaveSubscription(db, 1, "https://push.example.com/sub2", "key2", "auth2")

	req := withUser(httptest.NewRequest("GET", "/api/push/subscriptions", nil), 1)
	rec := httptest.NewRecorder()
	SubscriptionsListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Subscriptions []map[string]any `json:"subscriptions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Subscriptions) != 2 {
		t.Errorf("expected 2 subscriptions, got %d", len(body.Subscriptions))
	}
	// Verify cryptographic keys are not exposed in the list response.
	for i, sub := range body.Subscriptions {
		if _, has := sub["p256dh"]; has {
			t.Errorf("subscription[%d]: p256dh must not be exposed", i)
		}
		if _, has := sub["auth"]; has {
			t.Errorf("subscription[%d]: auth secret must not be exposed", i)
		}
	}
}
