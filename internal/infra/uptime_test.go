package infra

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecordCheck(t *testing.T) {
	db := setupTestDB(t)

	err := RecordCheck(db, "health_checks", "API Server", StatusOK, "")
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	records, err := GetRecentChecks(db, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Module != "health_checks" {
		t.Errorf("expected module health_checks, got %s", records[0].Module)
	}
	if records[0].Target != "API Server" {
		t.Errorf("expected target 'API Server', got '%s'", records[0].Target)
	}
	if records[0].Status != "ok" {
		t.Errorf("expected status ok, got %s", records[0].Status)
	}
}

func TestGetUptimeStats_Empty(t *testing.T) {
	db := setupTestDB(t)

	stats, err := GetUptimeStats(db)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalChecks != 0 {
		t.Errorf("expected 0 total checks, got %d", stats.TotalChecks)
	}
	// With no data, uptime should be 100% (no failures).
	if stats.Uptime24h != 100 {
		t.Errorf("expected 100%% uptime with no data, got %.1f%%", stats.Uptime24h)
	}
}

func TestGetUptimeStats_WithData(t *testing.T) {
	db := setupTestDB(t)

	// Insert some checks: 3 ok, 1 down.
	for range 3 {
		if err := RecordCheck(db, "health_checks", "svc", StatusOK, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := RecordCheck(db, "health_checks", "svc", StatusDown, "timeout"); err != nil {
		t.Fatal(err)
	}

	stats, err := GetUptimeStats(db)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalChecks != 4 {
		t.Errorf("expected 4 total checks, got %d", stats.TotalChecks)
	}
	// 3/4 = 75%
	if stats.Uptime24h != 75 {
		t.Errorf("expected 75%% uptime, got %.1f%%", stats.Uptime24h)
	}
}

func TestUptimeModule_NoHistory(t *testing.T) {
	db := setupTestDB(t)
	mod := NewUptimeModule(db)

	result := mod.Check()
	if result.Status != StatusOK {
		t.Errorf("expected ok with no history, got %s", result.Status)
	}
	if result.Name != "uptime" {
		t.Errorf("expected name uptime, got %s", result.Name)
	}
}

func TestUptimeModule_WithHistory(t *testing.T) {
	db := setupTestDB(t)

	for range 3 {
		_ = RecordCheck(db, "health_checks", "svc", StatusOK, "")
	}

	mod := NewUptimeModule(db)
	result := mod.Check()

	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s", result.Status)
	}
	if result.Message != "100.0% uptime (24h)" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestGetRecentChecks_Limit(t *testing.T) {
	db := setupTestDB(t)

	for range 5 {
		_ = RecordCheck(db, "health_checks", "svc", StatusOK, "")
	}

	records, err := GetRecentChecks(db, 3)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 records (limited), got %d", len(records))
	}
}

func TestUptimeHistoryHandler(t *testing.T) {
	db := setupTestDB(t)
	_ = RecordCheck(db, "health_checks", "API", StatusOK, "")

	req := withUser(httptest.NewRequest("GET", "/api/infra/uptime", nil), 1)
	rec := httptest.NewRecorder()
	UptimeHistoryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Stats   UptimeStats    `json:"stats"`
		Records []UptimeRecord `json:"records"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Records) != 1 {
		t.Errorf("expected 1 record, got %d", len(body.Records))
	}
	if body.Stats.TotalChecks != 1 {
		t.Errorf("expected 1 total check, got %d", body.Stats.TotalChecks)
	}
}

func TestClearUptimeHistoryHandler(t *testing.T) {
	db := setupTestDB(t)
	_ = RecordCheck(db, "health_checks", "API", StatusOK, "")

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/uptime", nil), 1)
	rec := httptest.NewRecorder()
	ClearUptimeHistoryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	records, err := GetRecentChecks(db, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records after clear, got %d", len(records))
	}
}
