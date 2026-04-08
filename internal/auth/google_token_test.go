package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

func TestSaveAndLoadGoogleToken(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-google-token-storage")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	userID := createTestUser(t, db)

	expiry := time.Now().Add(3600 * time.Second).UTC().Truncate(time.Second)
	original := &GoogleToken{
		AccessToken:  "ya29.access-token-abc",
		RefreshToken: "1//refresh-token-xyz",
		TokenType:    "Bearer",
		Expiry:       expiry,
		Scopes:       "openid email profile https://www.googleapis.com/auth/calendar.readonly",
	}

	if err := SaveGoogleToken(db, userID, original); err != nil {
		t.Fatalf("SaveGoogleToken: %v", err)
	}

	// Verify tokens are stored as ciphertext (enc: prefix) in the DB
	var rawAccess, rawRefresh string
	err := db.QueryRow(`SELECT access_token, refresh_token FROM google_tokens WHERE user_id = ?`, userID).Scan(&rawAccess, &rawRefresh)
	if err != nil {
		t.Fatalf("query raw tokens: %v", err)
	}
	if !strings.HasPrefix(rawAccess, "enc:") {
		t.Errorf("access_token in DB should be encrypted (enc: prefix), got %q", rawAccess)
	}
	if !strings.HasPrefix(rawRefresh, "enc:") {
		t.Errorf("refresh_token in DB should be encrypted (enc: prefix), got %q", rawRefresh)
	}
	if rawAccess == original.AccessToken {
		t.Error("access_token in DB should not equal plaintext")
	}
	if rawRefresh == original.RefreshToken {
		t.Error("refresh_token in DB should not equal plaintext")
	}

	loaded, err := LoadGoogleToken(db, userID)
	if err != nil {
		t.Fatalf("LoadGoogleToken: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadGoogleToken returned nil, want token")
	}
	if loaded.AccessToken != original.AccessToken {
		t.Errorf("AccessToken: got %q, want %q", loaded.AccessToken, original.AccessToken)
	}
	if loaded.RefreshToken != original.RefreshToken {
		t.Errorf("RefreshToken: got %q, want %q", loaded.RefreshToken, original.RefreshToken)
	}
	if loaded.TokenType != "Bearer" {
		t.Errorf("TokenType: got %q, want %q", loaded.TokenType, "Bearer")
	}
	if !loaded.Expiry.Equal(expiry) {
		t.Errorf("Expiry: got %v, want %v", loaded.Expiry, expiry)
	}
	if loaded.Scopes != original.Scopes {
		t.Errorf("Scopes: got %q, want %q", loaded.Scopes, original.Scopes)
	}
}

func TestLoadGoogleToken_NotFound(t *testing.T) {
	db := setupTestDB(t)

	token, err := LoadGoogleToken(db, 9999)
	if err != nil {
		t.Fatalf("LoadGoogleToken: unexpected error: %v", err)
	}
	if token != nil {
		t.Errorf("LoadGoogleToken: got %v, want nil", token)
	}
}

func TestHasGoogleToken(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-google-has-token")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	userID := createTestUser(t, db)

	has, err := HasGoogleToken(db, userID)
	if err != nil {
		t.Fatalf("HasGoogleToken: %v", err)
	}
	if has {
		t.Error("HasGoogleToken: got true before saving, want false")
	}

	if err := SaveGoogleToken(db, userID, &GoogleToken{
		AccessToken:  "tok",
		RefreshToken: "ref",
		TokenType:    "Bearer",
	}); err != nil {
		t.Fatalf("SaveGoogleToken: %v", err)
	}

	has, err = HasGoogleToken(db, userID)
	if err != nil {
		t.Fatalf("HasGoogleToken: %v", err)
	}
	if !has {
		t.Error("HasGoogleToken: got false after saving, want true")
	}
}

func TestDeleteGoogleToken(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-google-delete-token")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if err := SaveGoogleToken(db, userID, &GoogleToken{
		AccessToken:  "tok",
		RefreshToken: "ref",
		TokenType:    "Bearer",
	}); err != nil {
		t.Fatalf("SaveGoogleToken: %v", err)
	}

	if err := DeleteGoogleToken(db, userID); err != nil {
		t.Fatalf("DeleteGoogleToken: %v", err)
	}

	token, err := LoadGoogleToken(db, userID)
	if err != nil {
		t.Fatalf("LoadGoogleToken after delete: %v", err)
	}
	if token != nil {
		t.Error("LoadGoogleToken after delete: got token, want nil")
	}
}

func TestSaveGoogleToken_Upsert(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-google-upsert")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	userID := createTestUser(t, db)

	first := &GoogleToken{AccessToken: "first-access", RefreshToken: "first-refresh", TokenType: "Bearer"}
	if err := SaveGoogleToken(db, userID, first); err != nil {
		t.Fatalf("first SaveGoogleToken: %v", err)
	}

	second := &GoogleToken{AccessToken: "second-access", RefreshToken: "second-refresh", TokenType: "Bearer"}
	if err := SaveGoogleToken(db, userID, second); err != nil {
		t.Fatalf("second SaveGoogleToken: %v", err)
	}

	loaded, err := LoadGoogleToken(db, userID)
	if err != nil {
		t.Fatalf("LoadGoogleToken: %v", err)
	}
	if loaded.AccessToken != "second-access" {
		t.Errorf("AccessToken: got %q, want %q", loaded.AccessToken, "second-access")
	}
	if loaded.RefreshToken != "second-refresh" {
		t.Errorf("RefreshToken: got %q, want %q", loaded.RefreshToken, "second-refresh")
	}
}

func TestGoogleTokenIsExpired(t *testing.T) {
	past := &GoogleToken{Expiry: time.Now().Add(-1 * time.Hour)}
	if !past.IsExpired() {
		t.Error("expected past token to be expired")
	}

	future := &GoogleToken{Expiry: time.Now().Add(2 * time.Hour)}
	if future.IsExpired() {
		t.Error("expected future token to not be expired")
	}

	zero := &GoogleToken{}
	if zero.IsExpired() {
		t.Error("expected zero expiry token to not be expired")
	}
}
