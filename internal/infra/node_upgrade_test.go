package infra

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseNodeMajor(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"v20.11.1\n", 20, false},
		{"v22.0.0", 22, false},
		{"v18.19.0\r\n", 18, false},
		{"  v24.0.0  ", 24, false},
		{"invalid", 0, true},
		{"", 0, true},
		{"v", 0, true},
		{"vABC.1.2", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseNodeMajor(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("parseNodeMajor(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
				return
			}
			if got != tc.want {
				t.Errorf("parseNodeMajor(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsKnownLTSMajor(t *testing.T) {
	if !isKnownLTSMajor(20) {
		t.Error("expected 20 to be a known LTS major")
	}
	if isKnownLTSMajor(19) {
		t.Error("expected 19 to NOT be a known LTS major")
	}
	if isKnownLTSMajor(0) {
		t.Error("expected 0 to NOT be a known LTS major")
	}
	if isKnownLTSMajor(999) {
		t.Error("expected 999 to NOT be a known LTS major")
	}
}

// stubNodeInstalledMajor replaces nodeInstalledMajor for the duration of the test.
func stubNodeInstalledMajor(t *testing.T, major int, err error) {
	t.Helper()
	orig := nodeInstalledMajor
	t.Cleanup(func() { nodeInstalledMajor = orig })
	nodeInstalledMajor = func(_ context.Context) (int, error) {
		return major, err
	}
}

func TestNodeLTSVersionsHandler_Success(t *testing.T) {
	stubNodeInstalledMajor(t, 20, nil)

	// Stub checker: 22 available, 24 not available.
	checker := func(_ context.Context, major int) (bool, error) {
		return major == 22, nil
	}

	handler := nodeLTSHandlerWith(checker)
	req := httptest.NewRequest(http.MethodGet, "/api/infra/node-lts-versions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp NodeLTSResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.CurrentMajor != 20 {
		t.Errorf("expected current_major=20, got %d", resp.CurrentMajor)
	}
	if len(resp.AvailableMajors) != 1 || resp.AvailableMajors[0] != 22 {
		t.Errorf("expected available_majors=[22], got %v", resp.AvailableMajors)
	}
}

func TestNodeLTSVersionsHandler_NoUpgradesAvailable(t *testing.T) {
	stubNodeInstalledMajor(t, 24, nil)

	// All known majors <= 24, so none should be available.
	checker := func(_ context.Context, major int) (bool, error) {
		return true, nil
	}

	handler := nodeLTSHandlerWith(checker)
	req := httptest.NewRequest(http.MethodGet, "/api/infra/node-lts-versions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp NodeLTSResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.AvailableMajors) != 0 {
		t.Errorf("expected no available majors, got %v", resp.AvailableMajors)
	}
}

func TestNodeLTSVersionsHandler_NodeDetectionError(t *testing.T) {
	stubNodeInstalledMajor(t, 0, errors.New("node not found"))

	checker := func(_ context.Context, major int) (bool, error) {
		return true, nil
	}

	handler := nodeLTSHandlerWith(checker)
	req := httptest.NewRequest(http.MethodGet, "/api/infra/node-lts-versions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestNodeLTSVersionsHandler_CheckerError(t *testing.T) {
	stubNodeInstalledMajor(t, 18, nil)

	// Checker fails for all versions — they should be skipped, not cause a 500.
	checker := func(_ context.Context, major int) (bool, error) {
		return false, errors.New("network error")
	}

	handler := nodeLTSHandlerWith(checker)
	req := httptest.NewRequest(http.MethodGet, "/api/infra/node-lts-versions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp NodeLTSResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.AvailableMajors) != 0 {
		t.Errorf("expected empty available_majors when checker errors, got %v", resp.AvailableMajors)
	}
}

// stubUpgradeRunner returns a nodeUpgradeRunner that records what was called.
func stubUpgradeRunner(succeed bool) nodeUpgradeRunner {
	return func(_ context.Context, major int) (string, string, error) {
		if succeed {
			return "upgrade ok", "", nil
		}
		return "partial", "something failed", errors.New("upgrade failed")
	}
}

func TestNodeMajorUpgradeHandler_Success(t *testing.T) {
	handler := nodeMajorUpgradeHandlerWith(stubUpgradeRunner(true))

	body := strings.NewReader(`{"major": 22}`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra/node-major-upgrade", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	respBody := w.Body.String()
	if !strings.Contains(respBody, `"success":true`) {
		t.Errorf("expected success:true, got: %s", respBody)
	}
	if !strings.Contains(respBody, "upgrade ok") {
		t.Errorf("expected stdout in body, got: %s", respBody)
	}
}

func TestNodeMajorUpgradeHandler_Failure(t *testing.T) {
	handler := nodeMajorUpgradeHandlerWith(stubUpgradeRunner(false))

	body := strings.NewReader(`{"major": 22}`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra/node-major-upgrade", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	respBody := w.Body.String()
	if !strings.Contains(respBody, `"success":false`) {
		t.Errorf("expected success:false, got: %s", respBody)
	}
	if !strings.Contains(respBody, "something failed") {
		t.Errorf("expected stderr in body, got: %s", respBody)
	}
}

func TestNodeMajorUpgradeHandler_InvalidBody(t *testing.T) {
	handler := nodeMajorUpgradeHandlerWith(stubUpgradeRunner(true))

	body := strings.NewReader(`{"major": 0}`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra/node-major-upgrade", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for major=0, got %d", w.Code)
	}
}

func TestNodeMajorUpgradeHandler_MalformedJSON(t *testing.T) {
	handler := nodeMajorUpgradeHandlerWith(stubUpgradeRunner(true))

	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra/node-major-upgrade", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d", w.Code)
	}
}

func TestNodeMajorUpgradeHandler_UnsupportedMajor(t *testing.T) {
	handler := nodeMajorUpgradeHandlerWith(stubUpgradeRunner(true))

	// 19 is not in knownLTSMajors — should be rejected.
	body := strings.NewReader(`{"major": 19}`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra/node-major-upgrade", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported major, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unsupported major version") {
		t.Errorf("expected unsupported error message, got: %s", w.Body.String())
	}
}

func TestNodeMajorUpgradeHandler_SuccessInvalidatesCache(t *testing.T) {
	// Pre-populate caches.
	versionsCacheInstance.mu.Lock()
	versionsCacheInstance.data = map[string]string{"node": "20.0.0"}
	versionsCacheInstance.fetchedAt = time.Now()
	versionsCacheInstance.mu.Unlock()

	latestCacheInstance.mu.Lock()
	latestCacheInstance.data = map[string]string{"node": "22.0.0"}
	latestCacheInstance.mu.Unlock()

	handler := nodeMajorUpgradeHandlerWith(stubUpgradeRunner(true))

	body := strings.NewReader(`{"major": 22}`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra/node-major-upgrade", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	versionsCacheInstance.mu.Lock()
	vData := versionsCacheInstance.data
	versionsCacheInstance.mu.Unlock()

	latestCacheInstance.mu.Lock()
	lData := latestCacheInstance.data
	latestCacheInstance.mu.Unlock()

	if vData != nil {
		t.Error("expected versions cache to be invalidated")
	}
	if lData != nil {
		t.Error("expected latest cache to be invalidated")
	}
}
