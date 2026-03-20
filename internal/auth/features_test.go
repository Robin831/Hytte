package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func setupFeaturesTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys=ON")
	if err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			picture TEXT NOT NULL DEFAULT '',
			google_id TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_admin INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL
		);
		CREATE TABLE user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE user_features (
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			feature_key TEXT NOT NULL,
			enabled     INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, feature_key)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createFeaturesTestUser(t *testing.T, db *sql.DB, email, googleID string, isAdmin bool) int64 {
	t.Helper()
	admin := 0
	if isAdmin {
		admin = 1
	}
	res, err := db.Exec(
		"INSERT INTO users (google_id, email, name, is_admin) VALUES (?, ?, 'Test', ?)",
		googleID, email, admin,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

func TestGetUserFeatures_Defaults(t *testing.T) {
	db := setupFeaturesTestDB(t)
	uid := createFeaturesTestUser(t, db, "user@test.com", "g1", false)

	features, err := GetUserFeatures(db, uid, false)
	if err != nil {
		t.Fatalf("GetUserFeatures: %v", err)
	}

	for k, v := range FeatureDefaults {
		if features[k] != v {
			t.Errorf("feature %q: got %v, want %v", k, features[k], v)
		}
	}
}

func TestGetUserFeatures_AdminAllTrue(t *testing.T) {
	db := setupFeaturesTestDB(t)
	uid := createFeaturesTestUser(t, db, "admin@test.com", "g2", true)

	features, err := GetUserFeatures(db, uid, true)
	if err != nil {
		t.Fatalf("GetUserFeatures: %v", err)
	}

	for k := range FeatureDefaults {
		if !features[k] {
			t.Errorf("admin feature %q should be true", k)
		}
	}
}

func TestGetUserFeatures_WithOverride(t *testing.T) {
	db := setupFeaturesTestDB(t)
	uid := createFeaturesTestUser(t, db, "user@test.com", "g3", false)

	// Enable a default-off feature.
	if err := SetUserFeature(db, uid, "training", true); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}
	// Disable a default-on feature.
	if err := SetUserFeature(db, uid, "dashboard", false); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}

	features, err := GetUserFeatures(db, uid, false)
	if err != nil {
		t.Fatalf("GetUserFeatures: %v", err)
	}

	if !features["training"] {
		t.Error("training should be enabled after override")
	}
	if features["dashboard"] {
		t.Error("dashboard should be disabled after override")
	}
}

func TestSetUserFeature_Upsert(t *testing.T) {
	db := setupFeaturesTestDB(t)
	uid := createFeaturesTestUser(t, db, "user@test.com", "g4", false)

	// Set to true.
	if err := SetUserFeature(db, uid, "notes", true); err != nil {
		t.Fatalf("SetUserFeature true: %v", err)
	}
	features, _ := GetUserFeatures(db, uid, false)
	if !features["notes"] {
		t.Error("notes should be true")
	}

	// Upsert to false.
	if err := SetUserFeature(db, uid, "notes", false); err != nil {
		t.Fatalf("SetUserFeature false: %v", err)
	}
	features, _ = GetUserFeatures(db, uid, false)
	if features["notes"] {
		t.Error("notes should be false after upsert")
	}
}

func TestGetAllUsersFeatures(t *testing.T) {
	db := setupFeaturesTestDB(t)
	createFeaturesTestUser(t, db, "admin@test.com", "g5", true)
	uid2 := createFeaturesTestUser(t, db, "user@test.com", "g6", false)

	if err := SetUserFeature(db, uid2, "infra", true); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}

	users, err := GetAllUsersFeatures(db)
	if err != nil {
		t.Fatalf("GetAllUsersFeatures: %v", err)
	}

	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	// Admin user should have all features true.
	if !users[0].IsAdmin {
		t.Error("first user should be admin")
	}
	for k := range FeatureDefaults {
		if !users[0].Features[k] {
			t.Errorf("admin feature %q should be true", k)
		}
	}

	// Regular user should have infra enabled via override.
	if users[1].Features["infra"] != true {
		t.Error("regular user should have infra enabled")
	}
	if users[1].Features["training"] != false {
		t.Error("regular user should have training disabled (default)")
	}
}

func TestRequireFeature_Allowed(t *testing.T) {
	db := setupFeaturesTestDB(t)
	uid := createFeaturesTestUser(t, db, "user@test.com", "g7", false)
	if err := SetUserFeature(db, uid, "training", true); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}

	handler := RequireFeature(db, "training")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	user := &User{ID: uid, IsAdmin: false}
	req = req.WithContext(ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireFeature_Denied(t *testing.T) {
	db := setupFeaturesTestDB(t)
	uid := createFeaturesTestUser(t, db, "user@test.com", "g8", false)

	handler := RequireFeature(db, "training")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	user := &User{ID: uid, IsAdmin: false}
	req = req.WithContext(ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRequireFeature_AdminBypass(t *testing.T) {
	db := setupFeaturesTestDB(t)
	uid := createFeaturesTestUser(t, db, "admin@test.com", "g9", true)

	handler := RequireFeature(db, "training")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	user := &User{ID: uid, IsAdmin: true}
	req = req.WithContext(ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for admin, got %d", rr.Code)
	}
}

func TestAdminSetFeatureHandler_Success(t *testing.T) {
	db := setupFeaturesTestDB(t)
	adminID := createFeaturesTestUser(t, db, "admin@test.com", "g10", true)
	targetID := createFeaturesTestUser(t, db, "user@test.com", "g11", false)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := ContextWithUser(req.Context(), &User{ID: adminID, IsAdmin: true})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Put("/admin/users/{id}/features", AdminSetFeatureHandler(db))

	body := strings.NewReader(`{"feature":"training","enabled":true}`)
	req := httptest.NewRequest("PUT", "/admin/users/"+itoa(targetID)+"/features", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify the feature was set.
	features, _ := GetUserFeatures(db, targetID, false)
	if !features["training"] {
		t.Error("training should be enabled after admin set")
	}
}

func TestAdminSetFeatureHandler_UnknownFeature(t *testing.T) {
	db := setupFeaturesTestDB(t)
	adminID := createFeaturesTestUser(t, db, "admin@test.com", "g12", true)
	targetID := createFeaturesTestUser(t, db, "user@test.com", "g13", false)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := ContextWithUser(req.Context(), &User{ID: adminID, IsAdmin: true})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Put("/admin/users/{id}/features", AdminSetFeatureHandler(db))

	body := strings.NewReader(`{"feature":"nonexistent","enabled":true}`)
	req := httptest.NewRequest("PUT", "/admin/users/"+itoa(targetID)+"/features", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown feature, got %d", rr.Code)
	}
}

func TestAdminListUsersHandler(t *testing.T) {
	db := setupFeaturesTestDB(t)
	createFeaturesTestUser(t, db, "admin@test.com", "g14", true)
	createFeaturesTestUser(t, db, "user@test.com", "g15", false)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := ContextWithUser(req.Context(), &User{ID: 1, IsAdmin: true})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/admin/users", AdminListUsersHandler(db))

	req := httptest.NewRequest("GET", "/admin/users", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var users []UserFeatureSet
	if err := json.NewDecoder(rr.Body).Decode(&users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

func TestRequireAdmin_NonAdmin(t *testing.T) {
	handler := RequireAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	user := &User{ID: 1, IsAdmin: false}
	req = req.WithContext(ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", rr.Code)
	}
}

func TestRequireAdmin_Admin(t *testing.T) {
	handler := RequireAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	user := &User{ID: 1, IsAdmin: true}
	req = req.WithContext(ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for admin, got %d", rr.Code)
	}
}

func TestAdminSetFeatureHandler_InvalidUserID(t *testing.T) {
	db := setupFeaturesTestDB(t)
	adminID := createFeaturesTestUser(t, db, "admin@test.com", "g20", true)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := ContextWithUser(req.Context(), &User{ID: adminID, IsAdmin: true})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Put("/admin/users/{id}/features", AdminSetFeatureHandler(db))

	body := strings.NewReader(`{"feature":"training","enabled":true}`)
	req := httptest.NewRequest("PUT", "/admin/users/abc/features", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid user ID, got %d", rr.Code)
	}
}

func TestAdminSetFeatureHandler_NonExistentUser(t *testing.T) {
	db := setupFeaturesTestDB(t)
	adminID := createFeaturesTestUser(t, db, "admin@test.com", "g21", true)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := ContextWithUser(req.Context(), &User{ID: adminID, IsAdmin: true})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Put("/admin/users/{id}/features", AdminSetFeatureHandler(db))

	body := strings.NewReader(`{"feature":"training","enabled":true}`)
	req := httptest.NewRequest("PUT", "/admin/users/99999/features", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent user, got %d", rr.Code)
	}
}

func TestAdminSetFeatureHandler_InvalidBody(t *testing.T) {
	db := setupFeaturesTestDB(t)
	adminID := createFeaturesTestUser(t, db, "admin@test.com", "g22", true)
	targetID := createFeaturesTestUser(t, db, "user@test.com", "g23", false)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := ContextWithUser(req.Context(), &User{ID: adminID, IsAdmin: true})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Put("/admin/users/{id}/features", AdminSetFeatureHandler(db))

	body := strings.NewReader(`not json`)
	req := httptest.NewRequest("PUT", "/admin/users/"+itoa(targetID)+"/features", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid body, got %d", rr.Code)
	}
}

func TestAdminListUsersHandler_VerifyContent(t *testing.T) {
	db := setupFeaturesTestDB(t)
	createFeaturesTestUser(t, db, "admin@test.com", "g24", true)
	uid2 := createFeaturesTestUser(t, db, "user@test.com", "g25", false)

	// Set a feature override for the regular user.
	if err := SetUserFeature(db, uid2, "training", true); err != nil {
		t.Fatalf("SetUserFeature: %v", err)
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := ContextWithUser(req.Context(), &User{ID: 1, IsAdmin: true})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/admin/users", AdminListUsersHandler(db))

	req := httptest.NewRequest("GET", "/admin/users", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var users []UserFeatureSet
	if err := json.NewDecoder(rr.Body).Decode(&users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	// Admin should have all features true.
	for _, k := range FeatureKeys {
		if !users[0].Features[k] {
			t.Errorf("admin feature %q should be true in API response", k)
		}
	}

	// Regular user should have training enabled via override.
	if !users[1].Features["training"] {
		t.Error("regular user should have training enabled in API response")
	}
	// Regular user should have default-off features still off.
	if users[1].Features["infra"] {
		t.Error("regular user should have infra disabled (default)")
	}
}

func TestAdminSetFeatureHandler_ToggleAndVerifyRoundTrip(t *testing.T) {
	db := setupFeaturesTestDB(t)
	adminID := createFeaturesTestUser(t, db, "admin@test.com", "g26", true)
	targetID := createFeaturesTestUser(t, db, "user@test.com", "g27", false)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := ContextWithUser(req.Context(), &User{ID: adminID, IsAdmin: true})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Put("/admin/users/{id}/features", AdminSetFeatureHandler(db))
	r.Get("/admin/users", AdminListUsersHandler(db))

	// Enable a feature.
	body := strings.NewReader(`{"feature":"infra","enabled":true}`)
	req := httptest.NewRequest("PUT", "/admin/users/"+itoa(targetID)+"/features", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on enable, got %d", rr.Code)
	}

	// Verify via list endpoint.
	req = httptest.NewRequest("GET", "/admin/users", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	var users []UserFeatureSet
	if err := json.NewDecoder(rr.Body).Decode(&users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Find the regular user.
	var regularUser *UserFeatureSet
	for i := range users {
		if !users[i].IsAdmin {
			regularUser = &users[i]
			break
		}
	}
	if regularUser == nil {
		t.Fatal("regular user not found in response")
	}
	if !regularUser.Features["infra"] {
		t.Error("infra should be enabled after toggle")
	}

	// Disable it again.
	body = strings.NewReader(`{"feature":"infra","enabled":false}`)
	req = httptest.NewRequest("PUT", "/admin/users/"+itoa(targetID)+"/features", body)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on disable, got %d", rr.Code)
	}

	features, _ := GetUserFeatures(db, targetID, false)
	if features["infra"] {
		t.Error("infra should be disabled after second toggle")
	}
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}
