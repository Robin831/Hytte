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

func TestVAPIDPublicKeyHandler_NotConfigured(t *testing.T) {
	t.Setenv("VAPID_PUBLIC_KEY", "")

	req := httptest.NewRequest("GET", "/api/push/vapid-public-key", nil)
	rec := httptest.NewRecorder()
	VAPIDPublicKeyHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestVAPIDPublicKeyHandler_Configured(t *testing.T) {
	t.Setenv("VAPID_PUBLIC_KEY", "BTestKey123")

	req := httptest.NewRequest("GET", "/api/push/vapid-public-key", nil)
	rec := httptest.NewRecorder()
	VAPIDPublicKeyHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["public_key"] != "BTestKey123" {
		t.Errorf("public_key = %q, want BTestKey123", body["public_key"])
	}
}

func TestSubscribeHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"endpoint":"https://push.example.com/sub1","keys":{"p256dh":"pk","auth":"ak"}}`
	req := withUser(httptest.NewRequest("POST", "/api/push/subscribe", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	SubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Subscription Subscription `json:"subscription"`
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if body.Subscription.Endpoint != "https://push.example.com/sub1" {
		t.Errorf("endpoint = %q", body.Subscription.Endpoint)
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
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestSubscribeHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/push/subscribe", strings.NewReader("not json")), 1)
	rec := httptest.NewRecorder()
	SubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestUnsubscribeHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	_, _ = SaveSubscription(db, 1, "https://push.example.com/sub1", "pk", "ak", "Browser")

	payload := `{"endpoint":"https://push.example.com/sub1"}`
	req := withUser(httptest.NewRequest("DELETE", "/api/push/subscribe", strings.NewReader(payload)), 1)
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
	rec := httptest.NewRecorder()
	UnsubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestUnsubscribeHandler_MissingEndpoint(t *testing.T) {
	db := setupTestDB(t)

	payload := `{}`
	req := withUser(httptest.NewRequest("DELETE", "/api/push/subscribe", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	UnsubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestListSubscriptionsHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/push/subscriptions", nil), 1)
	rec := httptest.NewRecorder()
	ListSubscriptionsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Subscriptions []Subscription `json:"subscriptions"`
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if len(body.Subscriptions) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(body.Subscriptions))
	}
}

func TestSubscribeHandler_ConflictOtherUser(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// User 1 subscribes first
	_, _ = SaveSubscription(db, 1, "https://push.example.com/shared", "pk", "ak", "Browser")

	// User 2 tries to subscribe with same endpoint
	payload := `{"endpoint":"https://push.example.com/shared","keys":{"p256dh":"pk2","auth":"ak2"}}`
	req := withUser(httptest.NewRequest("POST", "/api/push/subscribe", strings.NewReader(payload)), 2)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	SubscribeHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListSubscriptionsHandler_WithData(t *testing.T) {
	db := setupTestDB(t)

	_, _ = SaveSubscription(db, 1, "https://push.example.com/1", "k1", "a1", "Browser")
	_, _ = SaveSubscription(db, 1, "https://push.example.com/2", "k2", "a2", "Browser")

	req := withUser(httptest.NewRequest("GET", "/api/push/subscriptions", nil), 1)
	rec := httptest.NewRecorder()
	ListSubscriptionsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Subscriptions []Subscription `json:"subscriptions"`
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if len(body.Subscriptions) != 2 {
		t.Errorf("expected 2 subscriptions, got %d", len(body.Subscriptions))
	}
}
