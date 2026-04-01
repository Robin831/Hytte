package forge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testConfigYAML = `anvils:
  test-repo:
    path: /tmp/test-repo
    max_smiths: 2
    auto_dispatch: tagged
    auto_dispatch_min_priority: 0
    auto_dispatch_tag: forgeReady
    auto_merge: true
settings:
  max_total_smiths: 3
  poll_interval: 30s
  smith_timeout: 30m0s
  stale_interval: 5m0s
  bellows_interval: 2m0s
  rate_limit_backoff: 10m0s
  max_ci_fix_attempts: 5
  max_pipeline_iterations: 5
  max_rebase_attempts: 3
  max_review_attempts: 5
  max_review_fix_attempts: 5
  providers:
    - claude/claude-sonnet-4-6
  smith_providers:
    - claude/claude-opus-4-6
  claude_flags: []
  auto_learn_rules: true
  schematic_enabled: true
  crucible_enabled: false
  wicket_enabled: true
  copilot_daily_request_limit: 15
  smelter_interval: 72h
notifications:
  enabled: true
  teams:
    webhook_url: ""
  webhooks:
    - name: Test
      url: https://example.com/hook
`

func setupTestConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	forgeDir := filepath.Join(dir, ".forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeTestConfig(t *testing.T, dir string) {
	t.Helper()
	cfgPath := filepath.Join(dir, ".forge", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(testConfigYAML), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGetConfigHandler_SymlinkRejected(t *testing.T) {
	dir := setupTestConfigDir(t)
	writeTestConfig(t, dir)
	t.Setenv("HOME", dir)

	// Create a symlink pointing to the config file.
	symlinkDir := filepath.Join(dir, ".forge-link")
	if err := os.MkdirAll(symlinkDir, 0755); err != nil {
		t.Fatal(err)
	}
	realCfg := filepath.Join(dir, ".forge", "config.yaml")
	linkPath := filepath.Join(dir, ".forge", "config-link.yaml")
	if err := os.Symlink(realCfg, linkPath); err != nil {
		t.Fatal(err)
	}
	// Replace the config file with a symlink to test rejection.
	if err := os.Remove(realCfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkPath, realCfg); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/forge/config", nil)
	rec := httptest.NewRecorder()
	GetConfigHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for symlink, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetConfigHandler_Success(t *testing.T) {
	dir := setupTestConfigDir(t)
	writeTestConfig(t, dir)
	t.Setenv("HOME", dir)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/config", nil)
	rec := httptest.NewRecorder()
	GetConfigHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var cfg ForgeConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if len(cfg.Anvils) != 1 {
		t.Errorf("expected 1 anvil, got %d", len(cfg.Anvils))
	}
	anvil, ok := cfg.Anvils["test-repo"]
	if !ok {
		t.Fatal("missing test-repo anvil")
	}
	if anvil.MaxSmiths != 2 {
		t.Errorf("expected max_smiths=2, got %d", anvil.MaxSmiths)
	}
	if cfg.Settings.MaxTotalSmiths != 3 {
		t.Errorf("expected max_total_smiths=3, got %d", cfg.Settings.MaxTotalSmiths)
	}
	if !cfg.Settings.SchematicEnabled {
		t.Error("expected schematic_enabled=true")
	}
	if cfg.Settings.CrucibleEnabled {
		t.Error("expected crucible_enabled=false")
	}
	if !cfg.Notifications.Enabled {
		t.Error("expected notifications.enabled=true")
	}
}

func TestGetConfigHandler_NotFound(t *testing.T) {
	dir := setupTestConfigDir(t)
	// Don't write the config file.
	t.Setenv("HOME", dir)

	req := httptest.NewRequest(http.MethodGet, "/api/forge/config", nil)
	rec := httptest.NewRecorder()
	GetConfigHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPutConfigHandler_Success(t *testing.T) {
	dir := setupTestConfigDir(t)
	writeTestConfig(t, dir)
	t.Setenv("HOME", dir)

	updated := ForgeConfig{
		Anvils: map[string]AnvilConfig{
			"test-repo": {
				Path:      "/tmp/test-repo",
				MaxSmiths: 4,
			},
		},
		Settings: ForgeSettings{
			MaxTotalSmiths:    5,
			PollInterval:      "60s",
			SmithTimeout:      "45m0s",
			SchematicEnabled:  true,
			CrucibleEnabled:   true,
			WicketEnabled:     false,
			Providers:         []string{"claude/claude-sonnet-4-6"},
			SmithProviders:    []string{"claude/claude-opus-4-6"},
			ClaudeFlags:       []string{},
			SmelterInterval:   "72h",
		},
		Notifications: NotificationConfig{
			Enabled: false,
		},
	}

	body, _ := json.Marshal(updated)
	req := httptest.NewRequest(http.MethodPut, "/api/forge/config", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	PutConfigHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify file was written by reading it back.
	getReq := httptest.NewRequest(http.MethodGet, "/api/forge/config", nil)
	getRec := httptest.NewRecorder()
	GetConfigHandler().ServeHTTP(getRec, getReq)

	var readBack ForgeConfig
	if err := json.Unmarshal(getRec.Body.Bytes(), &readBack); err != nil {
		t.Fatalf("failed to read back config: %v", err)
	}
	if readBack.Settings.MaxTotalSmiths != 5 {
		t.Errorf("expected max_total_smiths=5, got %d", readBack.Settings.MaxTotalSmiths)
	}
	if !readBack.Settings.CrucibleEnabled {
		t.Error("expected crucible_enabled=true after update")
	}
	if readBack.Notifications.Enabled {
		t.Error("expected notifications.enabled=false after update")
	}
}

func TestPutConfigHandler_InvalidJSON(t *testing.T) {
	dir := setupTestConfigDir(t)
	writeTestConfig(t, dir)
	t.Setenv("HOME", dir)

	req := httptest.NewRequest(http.MethodPut, "/api/forge/config", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	PutConfigHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
