package infra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestUpdateToolHandler_UnknownTool(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/api/infra/update/{tool}", UpdateToolHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/infra/update/unknown", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUpdateToolHandler_ForgeNotFound(t *testing.T) {
	// Point HOME to a temp dir with no restart.sh.
	tmp := t.TempDir()
	orig := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() {
		if err := os.Setenv("HOME", orig); err != nil {
			t.Fatalf("failed to restore HOME: %v", err)
		}
	}()

	r := chi.NewRouter()
	r.Post("/api/infra/update/{tool}", UpdateToolHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/infra/update/forge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestUpdateToolHandler_ForgeSuccess(t *testing.T) {
	tmp := t.TempDir()
	forgeDir := filepath.Join(tmp, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("failed to create forge dir: %v", err)
	}
	// Create a no-op script.
	if err := os.WriteFile(filepath.Join(forgeDir, "restart.sh"), []byte("#!/bin/sh\ntrue\n"), 0o755); err != nil {
		t.Fatalf("failed to write restart.sh: %v", err)
	}

	orig := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() {
		if err := os.Setenv("HOME", orig); err != nil {
			t.Fatalf("failed to restore HOME: %v", err)
		}
	}()

	r := chi.NewRouter()
	r.Post("/api/infra/update/{tool}", UpdateToolHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/infra/update/forge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}

	// Wait for the goroutine's sleep (200ms) plus the script execution to
	// complete so the temp dir is not removed while it is still running.
	time.Sleep(500 * time.Millisecond)
}

// stubRunners builds a runner map with a single tool mapped to the given function.
func stubRunners(tool string, fn toolRunner) map[string]toolRunner {
	return map[string]toolRunner{tool: fn}
}

func successRunner(_ context.Context) (string, string, error) {
	return "updated successfully", "", nil
}

func failureRunner(_ context.Context) (string, string, error) {
	return "partial output", "error: command failed", errors.New("exit status 1")
}

func TestUpdateToolHandler_BeadsSuccess(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/api/infra/update/{tool}", updateToolHandlerWithRunners(stubRunners("beads", successRunner)))

	req := httptest.NewRequest(http.MethodPost, "/api/infra/update/beads", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"success":true`) {
		t.Errorf("expected success:true in body, got: %s", body)
	}
	if !strings.Contains(body, "updated successfully") {
		t.Errorf("expected stdout in body, got: %s", body)
	}
}

func TestUpdateToolHandler_BeadsDownloadError(t *testing.T) {
	stubRunner := func(_ context.Context) (string, string, error) {
		return "", "curl: (22) The requested URL returned error: 404", errors.New("failed to download beads install script: exit status 22")
	}

	r := chi.NewRouter()
	r.Post("/api/infra/update/{tool}", updateToolHandlerWithRunners(stubRunners("beads", stubRunner)))

	req := httptest.NewRequest(http.MethodPost, "/api/infra/update/beads", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"success":false`) {
		t.Errorf("expected success:false in body, got: %s", body)
	}
}

func TestUpdateToolHandler_BeadsExecutionError(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/api/infra/update/{tool}", updateToolHandlerWithRunners(stubRunners("beads", failureRunner)))

	req := httptest.NewRequest(http.MethodPost, "/api/infra/update/beads", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"success":false`) {
		t.Errorf("expected success:false in body, got: %s", body)
	}
	if !strings.Contains(body, "partial output") {
		t.Errorf("expected stdout in body, got: %s", body)
	}
}

// TestUpdateToolHandler_ToolSuccess verifies that each new tool returns
// success:true when its runner succeeds.
func TestUpdateToolHandler_ToolSuccess(t *testing.T) {
	tools := []string{"claude", "go", "node", "npm", "git", "dolt"}

	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			r := chi.NewRouter()
			r.Post("/api/infra/update/{tool}", updateToolHandlerWithRunners(stubRunners(tool, successRunner)))

			req := httptest.NewRequest(http.MethodPost, "/api/infra/update/"+tool, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
			body := w.Body.String()
			if !strings.Contains(body, `"success":true`) {
				t.Errorf("expected success:true, got: %s", body)
			}
			if !strings.Contains(body, "updated successfully") {
				t.Errorf("expected stdout in body, got: %s", body)
			}
		})
	}
}

// TestUpdateToolHandler_ToolFailure verifies that each new tool returns
// success:false with stdout/stderr when its runner fails.
func TestUpdateToolHandler_ToolFailure(t *testing.T) {
	tools := []string{"claude", "go", "node", "npm", "git", "dolt"}

	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			r := chi.NewRouter()
			r.Post("/api/infra/update/{tool}", updateToolHandlerWithRunners(stubRunners(tool, failureRunner)))

			req := httptest.NewRequest(http.MethodPost, "/api/infra/update/"+tool, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
			body := w.Body.String()
			if !strings.Contains(body, `"success":false`) {
				t.Errorf("expected success:false, got: %s", body)
			}
			if !strings.Contains(body, "partial output") {
				t.Errorf("expected stdout in body, got: %s", body)
			}
			if !strings.Contains(body, "error: command failed") {
				t.Errorf("expected stderr in body, got: %s", body)
			}
		})
	}
}

// TestUpdateToolHandler_SuccessInvalidatesCache verifies that a successful
// tool update clears the versions cache.
func TestUpdateToolHandler_SuccessInvalidatesCache(t *testing.T) {
	// Pre-populate cache.
	versionsCacheInstance.mu.Lock()
	versionsCacheInstance.data = map[string]string{"test": "1.0"}
	versionsCacheInstance.fetchedAt = time.Now()
	versionsCacheInstance.mu.Unlock()

	r := chi.NewRouter()
	r.Post("/api/infra/update/{tool}", updateToolHandlerWithRunners(stubRunners("claude", successRunner)))

	req := httptest.NewRequest(http.MethodPost, "/api/infra/update/claude", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	versionsCacheInstance.mu.Lock()
	defer versionsCacheInstance.mu.Unlock()
	if versionsCacheInstance.data != nil {
		t.Error("expected cache to be invalidated after successful update")
	}
}

// TestUpdateToolHandler_FailureDoesNotInvalidateCache verifies that a failed
// tool update does NOT clear the versions cache.
func TestUpdateToolHandler_FailureDoesNotInvalidateCache(t *testing.T) {
	// Pre-populate cache.
	versionsCacheInstance.mu.Lock()
	versionsCacheInstance.data = map[string]string{"test": "1.0"}
	versionsCacheInstance.fetchedAt = time.Now()
	versionsCacheInstance.mu.Unlock()

	r := chi.NewRouter()
	r.Post("/api/infra/update/{tool}", updateToolHandlerWithRunners(stubRunners("claude", failureRunner)))

	req := httptest.NewRequest(http.MethodPost, "/api/infra/update/claude", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	versionsCacheInstance.mu.Lock()
	defer versionsCacheInstance.mu.Unlock()
	if versionsCacheInstance.data == nil {
		t.Error("expected cache to remain populated after failed update")
	}
}
