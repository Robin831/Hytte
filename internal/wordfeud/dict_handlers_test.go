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

func TestSearchHandler(t *testing.T) {
	dict := mockDictionary()
	handler := SearchHandler(dict)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantWords  []string // words that must appear in results
	}{
		{
			name:       "starts_with HE",
			body:       `{"pattern":"HE","mode":"starts_with"}`,
			wantStatus: http.StatusOK,
			wantWords:  []string{"HEST", "HEI"},
		},
		{
			name:       "ends_with ST",
			body:       `{"pattern":"ST","mode":"ends_with"}`,
			wantStatus: http.StatusOK,
			wantWords:  []string{"HEST", "REST", "ØST"},
		},
		{
			name:       "contains ES",
			body:       `{"pattern":"ES","mode":"contains"}`,
			wantStatus: http.StatusOK,
			wantWords:  []string{"HEST", "REST"},
		},
		{
			name:       "invalid mode",
			body:       `{"pattern":"HE","mode":"anagram"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty pattern",
			body:       `{"pattern":"","mode":"starts_with"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "pattern too long",
			body:       `{"pattern":"ABCDEFGHIJKLMNOPQ","mode":"starts_with"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid character in pattern",
			body:       `{"pattern":"HE*ST","mode":"contains"}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/search", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			var resp struct {
				Words    []FoundWord `json:"words"`
				Total    int         `json:"total"`
				Returned int         `json:"returned"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			// Total must equal Returned when all results fit within the cap.
			if resp.Total != resp.Returned {
				t.Errorf("total=%d != returned=%d (results were unexpectedly truncated for a small dictionary)", resp.Total, resp.Returned)
			}

			wordSet := make(map[string]bool, len(resp.Words))
			for _, w := range resp.Words {
				wordSet[w.Word] = true
			}
			for _, expected := range tt.wantWords {
				if !wordSet[expected] {
					t.Errorf("expected word %q not found in results %v", expected, resp.Words)
				}
			}

			// Verify results are sorted by score descending, then alphabetically.
			for i := 1; i < len(resp.Words); i++ {
				a, b := resp.Words[i-1], resp.Words[i]
				if a.Score < b.Score || (a.Score == b.Score && a.Word > b.Word) {
					t.Errorf("results not sorted: %v (score=%d) before %v (score=%d)", a.Word, a.Score, b.Word, b.Score)
				}
			}
		})
	}
}

func TestSearchHandlerTotalVsReturned(t *testing.T) {
	// Build a large mock dictionary so results exceed the 200-word cap.
	d := &Dictionary{path: "mock-large"}
	d.trie = NewTrie()
	// Insert 250 words that all start with "AB".
	for i := 0; i < 250; i++ {
		word := make([]rune, 4)
		word[0] = 'A'
		word[1] = 'B'
		word[2] = rune('A' + i/26)
		word[3] = rune('A' + i%26)
		d.trie.Insert(string(word))
	}
	d.loaded = true

	handler := SearchHandler(d)
	body := `{"pattern":"AB","mode":"starts_with"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp struct {
		Words    []FoundWord `json:"words"`
		Total    int         `json:"total"`
		Returned int         `json:"returned"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Total != 250 {
		t.Errorf("total = %d, want 250 (real total before cap)", resp.Total)
	}
	if resp.Returned != 200 {
		t.Errorf("returned = %d, want 200 (capped)", resp.Returned)
	}
	if len(resp.Words) != 200 {
		t.Errorf("words length = %d, want 200", len(resp.Words))
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
