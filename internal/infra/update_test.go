package infra

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", orig)

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
	os.MkdirAll(forgeDir, 0o755)
	// Create a no-op script.
	os.WriteFile(filepath.Join(forgeDir, "restart.sh"), []byte("#!/bin/sh\ntrue\n"), 0o755)

	orig := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", orig)

	r := chi.NewRouter()
	r.Post("/api/infra/update/{tool}", UpdateToolHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/infra/update/forge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}
}
