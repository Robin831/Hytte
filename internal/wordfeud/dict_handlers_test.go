package wordfeud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockDictionary creates a Dictionary backed by a small in-memory trie.
func mockDictionary() *Dictionary {
	d := &Dictionary{path: "mock"}
	d.trie = NewTrie()
	for _, w := range []string{"HEST", "HEI", "ER", "EN", "ÆRE", "ØST", "ÅR", "REST", "STEIN"} {
		d.trie.Insert(w)
	}
	d.loaded = true
	return d
}

func TestFindHandler(t *testing.T) {
	dict := mockDictionary()
	handler := FindHandler(dict)

	body := `{"letters":"HEST"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/find", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp struct {
		Words []FoundWord `json:"words"`
		Total int         `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Total == 0 {
		t.Error("expected at least one word result")
	}

	// HEST should be in the results.
	found := false
	for _, w := range resp.Words {
		if w.Word == "HEST" {
			found = true
			break
		}
	}
	if !found {
		t.Error("HEST not found in results")
	}
}

func TestFindHandlerEmptyLetters(t *testing.T) {
	dict := mockDictionary()
	handler := FindHandler(dict)

	body := `{"letters":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/find", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestFindHandlerTooManyLetters(t *testing.T) {
	dict := mockDictionary()
	handler := FindHandler(dict)

	body := `{"letters":"ABCDEFGHIJKLMNOP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/find", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestValidateHandler(t *testing.T) {
	dict := mockDictionary()
	handler := ValidateHandler(dict)

	tests := []struct {
		word  string
		valid bool
	}{
		{"HEST", true},
		{"ÆRE", true},
		{"XYZZY", false},
	}

	for _, tt := range tests {
		body := `{"word":"` + tt.word + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/validate", strings.NewReader(body))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("validate(%q) status = %d, want %d", tt.word, rr.Code, http.StatusOK)
			continue
		}

		var resp struct {
			Word  string `json:"word"`
			Valid bool   `json:"valid"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Errorf("validate(%q) unmarshal: %v", tt.word, err)
			continue
		}
		if resp.Valid != tt.valid {
			t.Errorf("validate(%q) valid = %v, want %v", tt.word, resp.Valid, tt.valid)
		}
	}
}

func TestValidateHandlerEmptyWord(t *testing.T) {
	dict := mockDictionary()
	handler := ValidateHandler(dict)

	body := `{"word":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/validate", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestTilesHandler(t *testing.T) {
	handler := TilesHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/wordfeud/tiles", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp struct {
		Tiles []TileInfo `json:"tiles"`
		Total int        `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 104 {
		t.Errorf("total = %d, want 104", resp.Total)
	}
	if len(resp.Tiles) != len(NorwegianTiles) {
		t.Errorf("tiles count = %d, want %d", len(resp.Tiles), len(NorwegianTiles))
	}
}
