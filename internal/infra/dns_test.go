package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// mockResolver is a test DNS resolver that returns configured results.
type mockResolver struct {
	hosts  map[string][]string
	cnames map[string]string
	mx     map[string][]*net.MX
	txt    map[string][]string
	ns     map[string][]*net.NS
	err    error
}

func (m *mockResolver) LookupHost(hostname string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	if addrs, ok := m.hosts[hostname]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: hostname}
}

func (m *mockResolver) LookupCNAME(hostname string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if cname, ok := m.cnames[hostname]; ok {
		return cname, nil
	}
	return "", &net.DNSError{Err: "no CNAME", Name: hostname}
}

func (m *mockResolver) LookupMX(hostname string) ([]*net.MX, error) {
	if m.err != nil {
		return nil, m.err
	}
	if records, ok := m.mx[hostname]; ok {
		return records, nil
	}
	return nil, &net.DNSError{Err: "no MX", Name: hostname}
}

func (m *mockResolver) LookupTXT(hostname string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	if records, ok := m.txt[hostname]; ok {
		return records, nil
	}
	return nil, &net.DNSError{Err: "no TXT", Name: hostname}
}

func (m *mockResolver) LookupNS(hostname string) ([]*net.NS, error) {
	if m.err != nil {
		return nil, m.err
	}
	if records, ok := m.ns[hostname]; ok {
		return records, nil
	}
	return nil, &net.DNSError{Err: "no NS", Name: hostname}
}

// --- DNS monitor CRUD tests ---

func TestListDNSMonitors_Empty(t *testing.T) {
	db := setupTestDB(t)
	monitors, err := ListDNSMonitors(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(monitors) != 0 {
		t.Errorf("expected 0 monitors, got %d", len(monitors))
	}
}

func TestAddAndListDNSMonitors(t *testing.T) {
	db := setupTestDB(t)

	mon, err := AddDNSMonitor(db, 1, "Example", "example.com", "A")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if mon.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if mon.Hostname != "example.com" || mon.RecordType != "A" {
		t.Errorf("unexpected monitor: %+v", mon)
	}

	monitors, err := ListDNSMonitors(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(monitors))
	}
}

func TestDeleteDNSMonitor(t *testing.T) {
	db := setupTestDB(t)

	mon, err := AddDNSMonitor(db, 1, "Test", "test.com", "A")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := DeleteDNSMonitor(db, 1, mon.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	monitors, err := ListDNSMonitors(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(monitors) != 0 {
		t.Errorf("expected 0 monitors after delete, got %d", len(monitors))
	}
}

func TestDeleteDNSMonitor_NotFound(t *testing.T) {
	db := setupTestDB(t)
	err := DeleteDNSMonitor(db, 1, 999)
	if err == nil {
		t.Error("expected error for non-existent monitor")
	}
}

// --- DNS module Check tests ---

func TestDNSModule_NoMonitors(t *testing.T) {
	db := setupTestDB(t)
	mod := NewDNSModule(db)

	result := mod.Check(1)
	if result.Status != StatusUnknown {
		t.Errorf("expected unknown with no monitors, got %s", result.Status)
	}
	if result.Name != "dns" {
		t.Errorf("expected name dns, got %s", result.Name)
	}
}

func TestDNSModule_SuccessA(t *testing.T) {
	db := setupTestDB(t)

	if _, err := AddDNSMonitor(db, 1, "Google", "google.com", "A"); err != nil {
		t.Fatal(err)
	}

	mod := &DNSModule{
		db: db,
		resolver: &mockResolver{
			hosts: map[string][]string{
				"google.com": {"142.250.80.46"},
			},
		},
	}

	result := mod.Check(1)
	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s: %s", result.Status, result.Message)
	}

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", result.Details)
	}
	monitors, ok := details["monitors"].([]DNSCheckResult)
	if !ok {
		t.Fatalf("expected []DNSCheckResult, got %T", details["monitors"])
	}
	if len(monitors) != 1 {
		t.Fatalf("expected 1 result, got %d", len(monitors))
	}
	if monitors[0].Status != string(StatusOK) {
		t.Errorf("expected ok, got %s", monitors[0].Status)
	}
	if len(monitors[0].ResolvedValues) == 0 {
		t.Error("expected resolved values")
	}
}

func TestDNSModule_FailedLookup(t *testing.T) {
	db := setupTestDB(t)

	if _, err := AddDNSMonitor(db, 1, "Bad", "nonexistent.invalid", "A"); err != nil {
		t.Fatal(err)
	}

	mod := &DNSModule{
		db: db,
		resolver: &mockResolver{
			err: fmt.Errorf("no such host"),
		},
	}

	result := mod.Check(1)
	if result.Status != StatusDown {
		t.Errorf("expected down for failed lookup, got %s: %s", result.Status, result.Message)
	}
}

func TestDNSModule_MXLookup(t *testing.T) {
	db := setupTestDB(t)

	if _, err := AddDNSMonitor(db, 1, "Mail", "example.com", "MX"); err != nil {
		t.Fatal(err)
	}

	mod := &DNSModule{
		db: db,
		resolver: &mockResolver{
			mx: map[string][]*net.MX{
				"example.com": {
					{Host: "mail.example.com.", Pref: 10},
				},
			},
		},
	}

	result := mod.Check(1)
	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s: %s", result.Status, result.Message)
	}
}

func TestDNSModule_FiltersPrivateIPs(t *testing.T) {
	db := setupTestDB(t)

	if _, err := AddDNSMonitor(db, 1, "Mixed", "mixed.example.com", "A"); err != nil {
		t.Fatal(err)
	}

	mod := &DNSModule{
		db: db,
		resolver: &mockResolver{
			hosts: map[string][]string{
				"mixed.example.com": {"8.8.8.8", "192.168.1.1", "1.1.1.1"},
			},
		},
	}

	result := mod.Check(1)
	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s: %s", result.Status, result.Message)
	}

	details := result.Details.(map[string]any)
	monitors := details["monitors"].([]DNSCheckResult)
	if len(monitors) != 1 {
		t.Fatalf("expected 1 result, got %d", len(monitors))
	}

	// Private IPs should be filtered out.
	for _, v := range monitors[0].ResolvedValues {
		if v == "192.168.1.1" {
			t.Error("private IP 192.168.1.1 should have been filtered from resolved values")
		}
	}
	if len(monitors[0].ResolvedValues) != 2 {
		t.Errorf("expected 2 public IPs, got %d: %v", len(monitors[0].ResolvedValues), monitors[0].ResolvedValues)
	}
}

// --- DNS handler tests ---

func TestListDNSMonitorsHandler(t *testing.T) {
	db := setupTestDB(t)
	if _, err := AddDNSMonitor(db, 1, "Test", "example.com", "A"); err != nil {
		t.Fatal(err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/infra/dns-monitors", nil), 1)
	rec := httptest.NewRecorder()
	ListDNSMonitorsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Monitors []DNSMonitor `json:"monitors"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Monitors) != 1 {
		t.Errorf("expected 1 monitor, got %d", len(body.Monitors))
	}
}

func TestAddDNSMonitorHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Example","hostname":"example.com","record_type":"A"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/dns-monitors", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddDNSMonitorHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddDNSMonitorHandler_DefaultRecordType(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Example","hostname":"example.com"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/dns-monitors", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddDNSMonitorHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify default record type was set to A.
	monitors, err := ListDNSMonitors(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(monitors))
	}
	if monitors[0].RecordType != "A" {
		t.Errorf("expected record_type 'A', got %q", monitors[0].RecordType)
	}
}

func TestAddDNSMonitorHandler_InvalidRecordType(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Test","hostname":"example.com","record_type":"INVALID"}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/dns-monitors", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddDNSMonitorHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAddDNSMonitorHandler_MissingFields(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"","hostname":""}`
	req := withUser(httptest.NewRequest("POST", "/api/infra/dns-monitors", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AddDNSMonitorHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteDNSMonitorHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	mon, err := AddDNSMonitor(db, 1, "Test", "example.com", "A")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	idStr := strconv.FormatInt(mon.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/infra/dns-monitors/"+idStr, nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", idStr)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteDNSMonitorHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteDNSMonitorHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/infra/dns-monitors/999", nil), 1)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	DeleteDNSMonitorHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
