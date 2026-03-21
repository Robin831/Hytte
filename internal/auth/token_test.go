package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAuthOrToken_NoAuthNoToken(t *testing.T) {
	db := setupTestDB(t)

	handler := RequireAuthOrToken(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without auth")
	}))

	req := httptest.NewRequest("POST", "/api/training/upload", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuthOrToken_ValidSession(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var gotUser *User
	handler := RequireAuthOrToken(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/training/upload", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if gotUser == nil || gotUser.ID != userID {
		t.Errorf("expected user %d in context", userID)
	}
}

func TestRequireAuthOrToken_ValidBearerToken(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	t.Setenv("HYTTE_UPLOAD_TOKEN", "supersecret")
	// Point the upload token at the test user we just created. In production
	// this defaults to a fixed user ID (e.g. 1), but in tests the
	// auto-incremented ID may differ, so we override it here.
	t.Setenv("HYTTE_UPLOAD_USER_ID", itoa(userID))

	var gotUser *User
	handler := RequireAuthOrToken(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/training/upload", nil)
	req.Header.Set("Authorization", "Bearer supersecret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if gotUser == nil || gotUser.ID != userID {
		t.Errorf("expected user %d in context, got %v", userID, gotUser)
	}
}

func TestRequireAuthOrToken_WrongBearerToken(t *testing.T) {
	db := setupTestDB(t)

	t.Setenv("HYTTE_UPLOAD_TOKEN", "supersecret")

	handler := RequireAuthOrToken(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with wrong token")
	}))

	req := httptest.NewRequest("POST", "/api/training/upload", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestRequireAuthOrToken_WrongBearerWithValidSession verifies that a wrong
// bearer token does NOT block a valid session cookie — the middleware must fall
// through to cookie auth rather than rejecting immediately.
func TestRequireAuthOrToken_WrongBearerWithValidSession(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Setenv("HYTTE_UPLOAD_TOKEN", "supersecret")
	t.Setenv("HYTTE_UPLOAD_USER_ID", itoa(userID))

	var gotUser *User
	handler := RequireAuthOrToken(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/training/upload", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (session fallback), got %d", rec.Code)
	}
	if gotUser == nil || gotUser.ID != userID {
		t.Errorf("expected user %d in context, got %v", userID, gotUser)
	}
}

// TestRequireAuthOrToken_ValidBearerWithValidSession verifies that when both
// a valid bearer token and a valid session cookie are present, the bearer token
// wins (it is checked first) and the correct user is injected.
func TestRequireAuthOrToken_ValidBearerWithValidSession(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)
	token, _, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Setenv("HYTTE_UPLOAD_TOKEN", "supersecret")
	t.Setenv("HYTTE_UPLOAD_USER_ID", itoa(userID))

	var gotUser *User
	handler := RequireAuthOrToken(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/training/upload", nil)
	req.Header.Set("Authorization", "Bearer supersecret")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if gotUser == nil || gotUser.ID != userID {
		t.Errorf("expected user %d in context, got %v", userID, gotUser)
	}
}

func TestRequireAuthOrToken_TokenDisabledWhenEnvUnset(t *testing.T) {
	db := setupTestDB(t)

	// Ensure env var is absent.
	t.Setenv("HYTTE_UPLOAD_TOKEN", "")

	handler := RequireAuthOrToken(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without a valid session")
	}))

	// Sending a bearer header should not count when token auth is disabled.
	req := httptest.NewRequest("POST", "/api/training/upload", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// No session cookie either, so falls through to session check → 401.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

