package auth

import (
	"testing"
)

func TestGetPreferences_Empty(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	prefs, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if len(prefs) != 0 {
		t.Errorf("expected empty prefs, got %v", prefs)
	}
}

func TestSetAndGetPreference(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if err := SetPreference(db, userID, "theme", "dark"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}

	prefs, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if prefs["theme"] != "dark" {
		t.Errorf("expected theme=dark, got %q", prefs["theme"])
	}
}

func TestSetPreference_Upsert(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if err := SetPreference(db, userID, "theme", "dark"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	if err := SetPreference(db, userID, "theme", "light"); err != nil {
		t.Fatalf("SetPreference upsert: %v", err)
	}

	prefs, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if prefs["theme"] != "light" {
		t.Errorf("expected theme=light after upsert, got %q", prefs["theme"])
	}
}

func TestSetPreference_MultipleKeys(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if err := SetPreference(db, userID, "theme", "dark"); err != nil {
		t.Fatalf("SetPreference theme: %v", err)
	}
	if err := SetPreference(db, userID, "home_location", "Oslo"); err != nil {
		t.Fatalf("SetPreference home_location: %v", err)
	}

	prefs, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if len(prefs) != 2 {
		t.Errorf("expected 2 prefs, got %d", len(prefs))
	}
	if prefs["theme"] != "dark" {
		t.Errorf("expected theme=dark, got %q", prefs["theme"])
	}
	if prefs["home_location"] != "Oslo" {
		t.Errorf("expected home_location=Oslo, got %q", prefs["home_location"])
	}
}

func TestDeleteAllPreferences(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	if err := SetPreference(db, userID, "theme", "dark"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	if err := DeleteAllPreferences(db, userID); err != nil {
		t.Fatalf("DeleteAllPreferences: %v", err)
	}

	prefs, err := GetPreferences(db, userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if len(prefs) != 0 {
		t.Errorf("expected empty prefs after delete, got %v", prefs)
	}
}
