package skywatch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseKpData(t *testing.T) {
	data := `[
		["time_tag", "Kp", "observed", "noaa_scale"],
		["2026-04-07 00:00:00.000", 2.33, "observed", "0"],
		["2026-04-07 03:00:00.000", 3.67, "observed", "1"],
		["2026-04-07 06:00:00.000", 1.0, "estimated", "0"]
	]`

	entries, err := parseKpData([]byte(data))
	if err != nil {
		t.Fatalf("parseKpData error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	if entries[0].Kp != 2.33 {
		t.Errorf("entry[0].Kp = %v, want 2.33", entries[0].Kp)
	}
	if entries[1].Source != "observed" {
		t.Errorf("entry[1].Source = %q, want %q", entries[1].Source, "observed")
	}
	if entries[2].Source != "estimated" {
		t.Errorf("entry[2].Source = %q, want %q", entries[2].Source, "estimated")
	}
}

func TestParseKpDataStringKp(t *testing.T) {
	data := `[
		["time_tag", "Kp"],
		["2026-04-07 00:00:00.000", "4.50"]
	]`

	entries, err := parseKpData([]byte(data))
	if err != nil {
		t.Fatalf("parseKpData error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Kp != 4.5 {
		t.Errorf("Kp = %v, want 4.5", entries[0].Kp)
	}
}

func TestParseKpDataInsufficientRows(t *testing.T) {
	data := `[["time_tag", "Kp"]]`
	_, err := parseKpData([]byte(data))
	if err == nil {
		t.Error("expected error for insufficient rows")
	}
}

func TestApproximateGeomagneticLat(t *testing.T) {
	// Bergen, Norway: geographic 60.36N should yield ~58-64° geomagnetic.
	gml := ApproximateGeomagneticLat(60.36, 5.24)
	if gml < 57 || gml > 65 {
		t.Errorf("Bergen geomagnetic lat = %.1f, expected ~57-65", gml)
	}

	// Tromsoe should be higher geomagnetically.
	gmlTromso := ApproximateGeomagneticLat(69.65, 18.96)
	if gmlTromso <= gml {
		t.Errorf("Tromsoe geomagnetic lat %.1f should be > Bergen %.1f", gmlTromso, gml)
	}

	// Equator should be near 0.
	gmlEq := ApproximateGeomagneticLat(0, 0)
	if gmlEq < -15 || gmlEq > 15 {
		t.Errorf("Equator geomagnetic lat = %.1f, expected near 0", gmlEq)
	}
}

func TestMinKpForAurora(t *testing.T) {
	tests := []struct {
		geomagLat float64
		wantKp    float64
	}{
		{68.0, 1},
		{66.0, 2},
		{63.5, 3},
		{61.0, 4},
		{58.0, 5},
		{56.0, 6},
		{53.0, 7},
		{50.5, 8},
		{45.0, 9},
	}
	for _, tt := range tests {
		got := MinKpForAurora(tt.geomagLat)
		if got != tt.wantKp {
			t.Errorf("MinKpForAurora(%.1f) = %.0f, want %.0f", tt.geomagLat, got, tt.wantKp)
		}
	}
}

func TestClassifyProbability(t *testing.T) {
	tests := []struct {
		maxKp   float64
		minKp   float64
		hasData bool
		want    string
	}{
		{5, 4, true, "likely"},
		{4, 4, true, "likely"},
		{3.5, 4, true, "possible"},
		{3, 4, true, "possible"},
		{2, 4, true, "unlikely"},
		{0, 4, false, "unknown"},
	}
	for _, tt := range tests {
		got := ClassifyProbability(tt.maxKp, tt.minKp, tt.hasData)
		if got != tt.want {
			t.Errorf("ClassifyProbability(%.1f, %.1f, %v) = %q, want %q", tt.maxKp, tt.minKp, tt.hasData, got, tt.want)
		}
	}
}

func TestBuildAuroraForecast(t *testing.T) {
	observed := []KpEntry{
		{TimeTag: "2026-04-07 12:00:00.000", Kp: 3.0, Source: "observed"},
		{TimeTag: "2026-04-07 15:00:00.000", Kp: 4.5, Source: "observed"},
	}
	forecasted := []KpEntry{
		{TimeTag: "2026-04-07 18:00:00.000", Kp: 5.0, Source: "predicted"},
		{TimeTag: "2026-04-07 21:00:00.000", Kp: 6.0, Source: "predicted"},
		{TimeTag: "2026-04-08 00:00:00.000", Kp: 4.0, Source: "predicted"},
	}

	result := buildAuroraForecast(observed, forecasted, 60.36, 5.24)

	if result.CurrentKp == nil {
		t.Fatal("expected non-nil CurrentKp")
	}
	if *result.CurrentKp != 4.5 {
		t.Errorf("CurrentKp = %v, want 4.5", *result.CurrentKp)
	}
	if result.BestDirection != "N" {
		t.Errorf("BestDirection = %q, want %q", result.BestDirection, "N")
	}
	if result.Location.GeomagneticLat == 0 {
		t.Error("GeomagneticLat should not be 0 for Bergen")
	}
}

func TestAuroraHandlerWithCachedData(t *testing.T) {
	mockObserved := `[
		["time_tag", "Kp", "Kp_fraction", "a_running", "station_count"],
		["2026-04-07 12:00:00.000", 2.33, 2.33, 5, 8]
	]`
	mockForecast := `[
		["time_tag", "Kp", "observed", "noaa_scale"],
		["2026-04-07 21:00:00.000", 3.0, "predicted", "1"]
	]`

	svc := NewAuroraService()
	svc.cache["observed"] = &auroraCached{
		data:    []byte(mockObserved),
		expires: time.Now().Add(1 * time.Hour),
	}
	svc.cache["forecast"] = &auroraCached{
		data:    []byte(mockForecast),
		expires: time.Now().Add(1 * time.Hour),
	}

	req := httptest.NewRequest("GET", "/api/skywatch/aurora?lat=60.36&lon=5.24", nil)
	w := httptest.NewRecorder()

	svc.AuroraHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var result AuroraForecast
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if result.CurrentKp == nil {
		t.Fatal("expected non-nil CurrentKp")
	}
	if result.Probability == "" {
		t.Error("expected non-empty probability")
	}
	if result.Location.Lat != 60.36 {
		t.Errorf("Location.Lat = %v, want 60.36", result.Location.Lat)
	}
}

func TestAuroraHandlerInvalidCoords(t *testing.T) {
	svc := NewAuroraService()
	req := httptest.NewRequest("GET", "/api/skywatch/aurora?lat=100&lon=5", nil)
	w := httptest.NewRecorder()

	svc.AuroraHandler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAuroraHandlerDefaultCoords(t *testing.T) {
	mockObserved := `[
		["time_tag", "Kp"],
		["2026-04-07 12:00:00.000", 1.0]
	]`
	mockForecast := `[
		["time_tag", "Kp"],
		["2026-04-07 21:00:00.000", 2.0]
	]`

	svc := NewAuroraService()
	svc.cache["observed"] = &auroraCached{
		data:    []byte(mockObserved),
		expires: time.Now().Add(1 * time.Hour),
	}
	svc.cache["forecast"] = &auroraCached{
		data:    []byte(mockForecast),
		expires: time.Now().Add(1 * time.Hour),
	}

	req := httptest.NewRequest("GET", "/api/skywatch/aurora", nil)
	w := httptest.NewRecorder()

	svc.AuroraHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result AuroraForecast
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if result.Location.Lat != DefaultLat {
		t.Errorf("Location.Lat = %v, want %v (default)", result.Location.Lat, DefaultLat)
	}
}
