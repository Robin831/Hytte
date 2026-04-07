package webhooks

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestIconForSource(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"github", ""},
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
		nil, []byte(`{"event":"test"}`), "POST", "/hooks/nonexistent",
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
		map[string]string{"X-Github-Event": "push"},
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
		headers, body,
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
		t.Fatalf("insert push subscription: %v", err)
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
		nil, []byte(`{}`),
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
		t.Fatalf("insert push subscription: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
		VALUES (?, 'https://push.example.com/sub-error', ?, ?)
	`, userID, makeKey(), makeAuth()); err != nil {
		t.Fatalf("insert push subscription: %v", err)
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
		nil, []byte(`{}`),
		"POST", "/hooks/ep4",
	)
	if callCount != 2 {
		t.Errorf("expected 2 RoundTrip calls (one per subscription), got %d", callCount)
	}

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
		t.Fatalf("insert push subscription: %v", err)
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
		nil, []byte(`{}`),
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
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unexpected DB error checking notifications_degraded: %v", err)
	}
}

// TestDispatchPushNotifications_QuietHoursSkip verifies that when quiet hours
// are active for the endpoint owner, the push dispatch is skipped entirely and
// no HTTP requests are made to push endpoints.
func TestDispatchPushNotifications_QuietHoursSkip(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep-qh', ?, 'Quiet Hours Test')",
		userID,
	); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	// Insert a push subscription so we can detect if it's contacted.
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
		VALUES (?, 'https://push.example.com/qh-test', ?, ?)
	`, userID, p256dh, authKey); err != nil {
		t.Fatalf("insert push subscription: %v", err)
	}

	// Configure quiet hours using a ±1 hour window around the current UTC time
	// so that quiet hours are always active when this test runs, without
	// relying on a near-miss boundary like 00:00–23:59 that excludes 23:59.
	nowUTC := time.Now().UTC()
	nowMin := nowUTC.Hour()*60 + nowUTC.Minute()
	startMin := (nowMin - 60 + 1440) % 1440
	endMin := (nowMin + 60) % 1440
	quietStart := fmt.Sprintf("%02d:%02d", startMin/60, startMin%60)
	quietEnd := fmt.Sprintf("%02d:%02d", endMin/60, endMin%60)

	for _, kv := range [][2]string{
		{"quiet_hours_enabled", "true"},
		{"quiet_hours_start", quietStart},
		{"quiet_hours_end", quietEnd},
		{"quiet_hours_timezone", "UTC"},
	} {
		if _, err := db.Exec(
			`INSERT INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)
			 ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`,
			userID, kv[0], kv[1],
		); err != nil {
			t.Fatalf("set preference %s: %v", kv[0], err)
		}
	}

	callCount := 0
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			callCount++
			return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
		}),
	}

	dispatchPushNotifications(
		context.Background(), db, client,
		"ep-qh", 10,
		nil, []byte(`{}`),
		"POST", "/hooks/ep-qh",
	)

	if callCount != 0 {
		t.Errorf("expected 0 push requests during quiet hours, got %d", callCount)
	}
}

// TestDispatchPushNotifications_FilteredBySource verifies that when a source
// is disabled in notification filters, no push requests are made.
func TestDispatchPushNotifications_FilteredBySource(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep-filter', ?, 'Filter Test')",
		userID,
	); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	// Insert a push subscription so we can detect if it's contacted.
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
		VALUES (?, 'https://push.example.com/filter-test', ?, ?)
	`, userID, p256dh, authKey); err != nil {
		t.Fatalf("insert push subscription: %v", err)
	}

	// Disable GitHub source in notification filters.
	if _, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (?, 'notification_filter_sources', '{"github":false,"generic":true}')`,
		userID,
	); err != nil {
		t.Fatalf("set filter preference: %v", err)
	}

	callCount := 0
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			callCount++
			return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
		}),
	}

	dispatchPushNotifications(
		context.Background(), db, client,
		"ep-filter", 11,
		map[string]string{"X-Github-Event": "push"}, []byte(`{"ref":"refs/heads/main","commits":[{}]}`),
		"POST", "/hooks/ep-filter",
	)

	if callCount != 0 {
		t.Errorf("expected 0 push requests when source is filtered, got %d", callCount)
	}
}

// TestDispatchPushNotifications_FilteredByEventType verifies that when a
// specific GitHub event type is disabled, no push requests are made for that
// event, but other event types still go through.
func TestDispatchPushNotifications_FilteredByEventType(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep-evt', ?, 'Event Filter Test')",
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
		VALUES (?, 'https://push.example.com/evt-test', ?, ?)
	`, userID, p256dh, authKey); err != nil {
		t.Fatalf("insert push subscription: %v", err)
	}

	// Disable "push" events but keep "release" enabled.
	if _, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (?, 'notification_filter_events', '{"push":false,"release":true}')`,
		userID,
	); err != nil {
		t.Fatalf("set filter preference: %v", err)
	}

	callCount := 0
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			callCount++
			return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
		}),
	}

	// Dispatch a "push" event — should be filtered out.
	dispatchPushNotifications(
		context.Background(), db, client,
		"ep-evt", 12,
		map[string]string{"X-Github-Event": "push"}, []byte(`{"ref":"refs/heads/main","commits":[{}]}`),
		"POST", "/hooks/ep-evt",
	)
	if callCount != 0 {
		t.Errorf("expected 0 push requests for disabled 'push' event, got %d", callCount)
	}

	// Dispatch a "release" event — should go through.
	dispatchPushNotifications(
		context.Background(), db, client,
		"ep-evt", 13,
		map[string]string{"X-Github-Event": "release"}, []byte(`{"action":"published","release":{"tag_name":"v1.0"}}`),
		"POST", "/hooks/ep-evt",
	)
	if callCount != 1 {
		t.Errorf("expected 1 push request for enabled 'release' event, got %d", callCount)
	}
}

// TestDispatchPushNotifications_ForgeEvent verifies that a webhook with an
// X-Forge-Event header is classified as source "forge" and uses the header
// value as the event type for filtering.
func TestDispatchPushNotifications_ForgeEvent(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep-forge', ?, 'Forge Test')",
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
		VALUES (?, 'https://push.example.com/forge-test', ?, ?)
	`, userID, p256dh, authKey); err != nil {
		t.Fatalf("insert push subscription: %v", err)
	}

	callCount := 0
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			callCount++
			return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
		}),
	}

	headers := map[string]string{"X-Forge-Event": "pr_ready_to_merge"}
	body := []byte(`{"event_type":"pr_ready_to_merge","message":"PR #42 is ready to merge"}`)

	// Forge event with default prefs — should be delivered.
	dispatchPushNotifications(
		context.Background(), db, client,
		"ep-forge", 20,
		headers, body,
		"POST", "/hooks/ep-forge",
	)
	if callCount != 1 {
		t.Errorf("expected 1 push request for forge event with default prefs, got %d", callCount)
	}

	// Now disable "pr_ready_to_merge" in notification_filter_events — dispatch
	// should be suppressed because the X-Forge-Event value is used as event type.
	if _, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (?, 'notification_filter_events', '{"pr_ready_to_merge":false}')`,
		userID,
	); err != nil {
		t.Fatalf("set filter preference: %v", err)
	}
	dispatchPushNotifications(
		context.Background(), db, client,
		"ep-forge", 21,
		headers, body,
		"POST", "/hooks/ep-forge",
	)
	if callCount != 1 {
		t.Errorf("expected 0 additional push requests when pr_ready_to_merge event is filtered, got %d extra", callCount-1)
	}
}

// TestDispatchPushNotifications_ForgeFilteredBySource verifies that when the
// forge source is disabled in notification filters, Forge webhooks are suppressed.
func TestDispatchPushNotifications_ForgeFilteredBySource(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if _, err := db.Exec(
		"INSERT INTO webhook_endpoints (id, user_id, name) VALUES ('ep-forge-filter', ?, 'Forge Filter Test')",
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
		VALUES (?, 'https://push.example.com/forge-filter-test', ?, ?)
	`, userID, p256dh, authKey); err != nil {
		t.Fatalf("insert push subscription: %v", err)
	}

	// Disable forge source.
	if _, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (?, 'notification_filter_sources', '{"forge":false,"github":true}')`,
		userID,
	); err != nil {
		t.Fatalf("set filter preference: %v", err)
	}

	callCount := 0
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			callCount++
			return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
		}),
	}

	headers := map[string]string{"X-Forge-Event": "pr_created"}
	body := []byte(`{"event_type":"pr_created","message":"New PR created"}`)

	dispatchPushNotifications(
		context.Background(), db, client,
		"ep-forge-filter", 21,
		headers, body,
		"POST", "/hooks/ep-forge-filter",
	)
	if callCount != 0 {
		t.Errorf("expected 0 push requests when forge source is filtered, got %d", callCount)
	}
}

// roundTripFunc is a helper to use a function as an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestForgeDeepLinkURL(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		body      []byte
		want      string
	}{
		{
			name:      "pr_ready_to_merge with bead_id",
			eventType: "pr_ready_to_merge",
			body:      []byte(`{"event_type":"pr_ready_to_merge","bead_id":"ext-53"}`),
			want:      "/forge/mezzanine?highlight=pr-ext-53",
		},
		{
			name:      "pr_ready_to_merge without bead_id",
			eventType: "pr_ready_to_merge",
			body:      []byte(`{"event_type":"pr_ready_to_merge"}`),
			want:      "/forge/mezzanine?section=pipeline",
		},
		{
			name:      "bead_failed with bead_id",
			eventType: "bead_failed",
			body:      []byte(`{"event_type":"bead_failed","bead_id":"Hytte-abc1"}`),
			want:      "/forge/mezzanine?section=needs-attention&bead=Hytte-abc1",
		},
		{
			name:      "bead_needs_human with bead_id",
			eventType: "bead_needs_human",
			body:      []byte(`{"event_type":"bead_needs_human","bead_id":"Hytte-xyz"}`),
			want:      "/forge/mezzanine?section=needs-attention&bead=Hytte-xyz",
		},
		{
			name:      "bead_failed without bead_id",
			eventType: "bead_failed",
			body:      []byte(`{"event_type":"bead_failed"}`),
			want:      "/forge/mezzanine?section=needs-attention",
		},
		{
			name:      "daily_cost routes to costs page",
			eventType: "daily_cost",
			body:      []byte(`{"event_type":"daily_cost","message":"Daily cost: $4.20"}`),
			want:      "/forge/mezzanine/costs",
		},
		{
			name:      "cost_limit_reached routes to costs page",
			eventType: "cost_limit_reached",
			body:      []byte(`{"event_type":"cost_limit_reached"}`),
			want:      "/forge/mezzanine/costs",
		},
		{
			name:      "unknown event type falls back to mezzanine",
			eventType: "worker_started",
			body:      []byte(`{"event_type":"worker_started"}`),
			want:      "/forge/mezzanine",
		},
		{
			name:      "empty body still works",
			eventType: "pr_ready_to_merge",
			body:      nil,
			want:      "/forge/mezzanine?section=pipeline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := forgeDeepLinkURL(tt.eventType, tt.body)
			if got != tt.want {
				t.Errorf("forgeDeepLinkURL(%q, ...) = %q, want %q", tt.eventType, got, tt.want)
			}
		})
	}
}
