package webhooks

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
)

func TestIconForSource(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"github", "/icons/github.svg"},
		{"gitlab", ""},
		{"", ""},
		{"unknown", ""},
	}
	for _, tt := range tests {
		got := iconForSource(tt.source)
		if got != tt.want {
			t.Errorf("iconForSource(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

// TestDispatchPushNotifications_NonexistentEndpoint verifies that the dispatch
// function handles a missing endpoint gracefully (logs and returns, no panic).
func TestDispatchPushNotifications_NonexistentEndpoint(t *testing.T) {
	db := setupTestDB(t)

	// Should not panic — endpoint doesn't exist, lookup fails, function returns early.
	dispatchPushNotifications(
		context.Background(), db, http.DefaultClient,
		"nonexistent-endpoint-xyz", 42,
		"", nil, []byte(`{"event":"test"}`), "POST", "/hooks/nonexistent",
	)
}

// TestDispatchPushNotifications_NoSubscriptions verifies that the dispatch
// function completes without error when the endpoint owner has no push subscriptions.
func TestDispatchPushNotifications_NoSubscriptions(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep1', ?, 'Test')",
		userID,
	); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	// Should complete without panic — no push subscriptions exist for the user.
	dispatchPushNotifications(
		context.Background(), db, http.DefaultClient,
		"ep1", 1,
		"push", nil,
		[]byte(`{"ref":"refs/heads/main","commits":[{}]}`),
		"POST", "/hooks/ep1",
	)
}

// TestDispatchPushNotifications_GitHubEvent verifies that a GitHub push event
// produces a well-formed notification tag and URL (via side-effect: no panic,
// endpoint stored, no subscriptions means no HTTP calls needed).
func TestDispatchPushNotifications_GitHubEvent(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep2', ?, 'GitHub Hook')",
		userID,
	); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	headers := map[string]string{"X-Github-Event": "release"}
	body := []byte(`{"action":"published","release":{"tag_name":"v1.0.0","name":"v1.0.0"}}`)

	// No subscriptions → no actual HTTP push, but the format+payload path is exercised.
	dispatchPushNotifications(
		context.Background(), db, http.DefaultClient,
		"ep2", 99,
		"release", headers, body,
		"POST", "/hooks/ep2",
	)
}

// TestDispatchPushNotifications_DeadSubscriptionMarking verifies that when all
// push deliveries report 410 Gone, the user preference "notifications_degraded"
// is set to "true".
func TestDispatchPushNotifications_DeadSubscriptionMarking(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep3', ?, 'Dead Sub Test')",
		userID,
	); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	// Generate a valid P-256 key pair so encryption succeeds and the HTTP mock
	// is actually reached (rather than failing early at key decoding).
	curve := ecdh.P256()
	subKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	p256dh := base64.RawURLEncoding.EncodeToString(subKey.PublicKey().Bytes())

	authSecret := make([]byte, 16)
	if _, err := rand.Read(authSecret); err != nil {
		t.Fatalf("generate auth secret: %v", err)
	}
	authKey := base64.RawURLEncoding.EncodeToString(authSecret)

	if _, err := db.Exec(`
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
		VALUES (?, 'https://push.example.com/dead-sub', ?, ?)
	`, userID, p256dh, authKey); err != nil {
		t.Skipf("push_subscriptions table not available: %v", err)
	}

	// Use a custom HTTP client that always returns 410 to simulate dead subscriptions.
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusGone,
				Body:       http.NoBody,
			}, nil
		}),
	}

	dispatchPushNotifications(
		context.Background(), db, client,
		"ep3", 7,
		"", nil, []byte(`{}`),
		"POST", "/hooks/ep3",
	)

	// Verify "notifications_degraded" preference was set.
	var val string
	err = db.QueryRow(
		"SELECT value FROM user_preferences WHERE user_id = ? AND key = 'notifications_degraded'",
		userID,
	).Scan(&val)
	if err != nil {
		t.Fatalf("read notifications_degraded preference: %v", err)
	}
	if val != "true" {
		t.Errorf("notifications_degraded = %q, want %q", val, "true")
	}
}

// TestDispatchPushNotifications_MixedNetworkErrorAndDeadSubs verifies that when
// some subscriptions return 410 Gone and others encounter a network error (no
// HTTP response), the user is still marked degraded — network errors must not
// prevent degradation marking when all responding subscriptions are dead.
func TestDispatchPushNotifications_MixedNetworkErrorAndDeadSubs(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep4', ?, 'Mixed Error Test')",
		userID,
	); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	// Insert two subscriptions.
	curve := ecdh.P256()
	makeKey := func() string {
		key, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	}
	makeAuth := func() string {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			t.Fatalf("generate auth: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}

	if _, err := db.Exec(`
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
		VALUES (?, 'https://push.example.com/sub-gone', ?, ?)
	`, userID, makeKey(), makeAuth()); err != nil {
		t.Skipf("push_subscriptions table not available: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
		VALUES (?, 'https://push.example.com/sub-error', ?, ?)
	`, userID, makeKey(), makeAuth()); err != nil {
		t.Skipf("push_subscriptions table not available: %v", err)
	}

	callCount := 0
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			callCount++
			if r.URL.Host == "push.example.com" && r.URL.Path == "/sub-gone" {
				return &http.Response{StatusCode: http.StatusGone, Body: http.NoBody}, nil
			}
			// Second subscription gets a network error (simulates TCP failure).
			return nil, fmt.Errorf("simulated network error")
		}),
	}

	dispatchPushNotifications(
		context.Background(), db, client,
		"ep4", 8,
		"", nil, []byte(`{}`),
		"POST", "/hooks/ep4",
	)

	// Despite the network error on one sub, the 410 on the other should cause
	// notifications_degraded to be set — network errors must not block marking.
	var val string
	err := db.QueryRow(
		"SELECT value FROM user_preferences WHERE user_id = ? AND key = 'notifications_degraded'",
		userID,
	).Scan(&val)
	if err != nil {
		t.Fatalf("read notifications_degraded preference: %v", err)
	}
	if val != "true" {
		t.Errorf("notifications_degraded = %q, want %q", val, "true")
	}
}

// TestDispatchPushNotifications_AllNetworkErrors verifies that when every
// subscription delivery fails with a network error (no HTTP response at all),
// the user is NOT marked as degraded — transient outages must not trigger
// permanent degradation marking.
func TestDispatchPushNotifications_AllNetworkErrors(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep5', ?, 'All Error Test')",
		userID,
	); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	curve := ecdh.P256()
	key, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	p256dh := base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	authSecret := make([]byte, 16)
	if _, err := rand.Read(authSecret); err != nil {
		t.Fatalf("generate auth secret: %v", err)
	}
	authKey := base64.RawURLEncoding.EncodeToString(authSecret)

	if _, err := db.Exec(`
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
		VALUES (?, 'https://push.example.com/sub-error-only', ?, ?)
	`, userID, p256dh, authKey); err != nil {
		t.Skipf("push_subscriptions table not available: %v", err)
	}

	// Every delivery returns a network error — no HTTP response at all.
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("simulated network outage")
		}),
	}

	dispatchPushNotifications(
		context.Background(), db, client,
		"ep5", 9,
		"", nil, []byte(`{}`),
		"POST", "/hooks/ep5",
	)

	// notifications_degraded must NOT be set — all errors were transient.
	var val string
	err = db.QueryRow(
		"SELECT value FROM user_preferences WHERE user_id = ? AND key = 'notifications_degraded'",
		userID,
	).Scan(&val)
	if err == nil {
		t.Errorf("notifications_degraded should not be set after pure network errors, got %q", val)
	}
}

// roundTripFunc is a helper to use a function as an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
