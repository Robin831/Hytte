package wordfeud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// solverMockDictionary creates a Dictionary with words useful for solver tests.
func solverMockDictionary() *Dictionary {
	d := &Dictionary{path: "mock"}
	d.trie = NewTrie()
	for _, w := range []string{"HEST", "HEI", "ER", "EN", "REST", "STEIN", "ARS", "ÆRE", "ØST"} {
		d.trie.Insert(w)
	}
	d.loaded = true
	return d
}

// emptyBoardPayload builds a 15x15 null board JSON array.
func emptyBoardPayload() string {
	row := "[" + strings.Repeat("null,", 14) + "null]"
	rows := make([]string, 15)
	for i := range rows {
		rows[i] = row
	}
	return "[" + strings.Join(rows, ",") + "]"
}

func TestSolveHandlerSuccess(t *testing.T) {
	dict := solverMockDictionary()
	handler := SolveHandler(dict)

	body := `{"board":` + emptyBoardPayload() + `,"rack":"HEST"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/solve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp SolveResult
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Moves) == 0 {
		t.Error("expected at least one move result")
	}
}

func TestSolveHandlerInvalidBoardDimensions(t *testing.T) {
	dict := solverMockDictionary()
	handler := SolveHandler(dict)

	// Only 14 rows
	row := "[" + strings.Repeat("null,", 14) + "null]"
	rows := make([]string, 14)
	for i := range rows {
		rows[i] = row
	}
	board := "[" + strings.Join(rows, ",") + "]"

	body := `{"board":` + board + `,"rack":"HEST"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/solve", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSolveHandlerInvalidRackEmpty(t *testing.T) {
	dict := solverMockDictionary()
	handler := SolveHandler(dict)

	body := `{"board":` + emptyBoardPayload() + `,"rack":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/solve", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSolveHandlerInvalidRackTooLong(t *testing.T) {
	dict := solverMockDictionary()
	handler := SolveHandler(dict)

	body := `{"board":` + emptyBoardPayload() + `,"rack":"ABCDEFGH"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/solve", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSolveHandlerInvalidRackCharset(t *testing.T) {
	dict := solverMockDictionary()
	handler := SolveHandler(dict)

	body := `{"board":` + emptyBoardPayload() + `,"rack":"HE1T"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/solve", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSolveHandlerInvalidBoardCellLetter(t *testing.T) {
	dict := solverMockDictionary()
	handler := SolveHandler(dict)

	// Place an invalid letter ("1") in row 0, col 0
	row0 := `[{"letter":"1","is_blank":false},` + strings.Repeat("null,", 13) + "null]"
	rows := make([]string, 15)
	rows[0] = row0
	for i := 1; i < 15; i++ {
		rows[i] = "[" + strings.Repeat("null,", 14) + "null]"
	}
	board := "[" + strings.Join(rows, ",") + "]"

	body := `{"board":` + board + `,"rack":"HEST"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/solve", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSolveHandlerDictionaryUnavailable(t *testing.T) {
	// Unloaded dictionary with a path that won't resolve
	dict := &Dictionary{path: "/nonexistent/dict.txt"}

	handler := SolveHandler(dict)

	body := `{"board":` + emptyBoardPayload() + `,"rack":"HEST"}`
	req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/solve", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}
