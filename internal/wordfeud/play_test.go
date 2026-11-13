package wordfeud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// gameWith builds a game with tiles at rendered (row, col) — stored the way
// the API parser does, Board[col][row] — and a rack ("*" = blank).
func gameWith(tiles map[[2]int]string, rack string) *GameState {
	gs := &GameState{IsRunning: true, IsMyTurn: true, BagCount: 50, Ruleset: RulesetNorwegian}
	for pos, l := range tiles {
		gs.Board[pos[1]][pos[0]] = &Tile{Letter: l}
	}
	for _, r := range rack {
		if r == '*' {
			gs.Rack = append(gs.Rack, blankTile())
		} else {
			gs.Rack = append(gs.Rack, Tile{Letter: string(r)})
		}
	}
	return gs
}

func testTrie(words ...string) *Trie {
	t := NewTrie()
	for _, w := range words {
		t.Insert(w)
	}
	return t
}

func TestPreviewFirstMoveAndScore(t *testing.T) {
	gs := gameWith(nil, "HUSETAB")
	trie := testTrie("HUS")
	// HUS across the centre star (row 7, cols 6–8): no multipliers there.
	p := Preview(gs, []PlacedTile{{7, 6, "H", false}, {7, 7, "U", false}, {7, 8, "S", false}}, trie)
	if p.Problem != "" || p.MainWord != "HUS" || !p.AllValid {
		t.Fatalf("preview = %+v", p)
	}
	want := LetterValue['H'] + LetterValue['U'] + LetterValue['S']
	if p.Score != want {
		t.Fatalf("score = %d, want %d", p.Score, want)
	}
	// The first word must cover the centre.
	if p := Preview(gs, []PlacedTile{{0, 0, "H", false}, {0, 1, "U", false}}, trie); p.Problem != "first_move_center" {
		t.Fatalf("off-centre first move: %+v", p)
	}
}

func TestPreviewExtendsExistingWord(t *testing.T) {
	// HUS on row 7, cols 6–8: extend it to HUSET, and play SKI down
	// through the S using the blank as K.
	gs := gameWith(map[[2]int]string{{7, 6}: "H", {7, 7}: "U", {7, 8}: "S"}, "ET*KIAB")
	trie := testTrie("HUS", "HUSET", "SKI")
	p := Preview(gs, []PlacedTile{{7, 9, "E", false}, {7, 10, "T", false}}, trie)
	if p.Problem != "" || p.MainWord != "HUSET" || len(p.Words) != 1 || !p.AllValid {
		t.Fatalf("HUSET = %+v", p)
	}
	// Vertical through the existing S, using the blank as K.
	p = Preview(gs, []PlacedTile{{8, 8, "K", true}, {9, 8, "I", false}}, trie)
	if p.Problem != "" || p.MainWord != "SKI" {
		t.Fatalf("SKI = %+v", p)
	}
	// An invalid word is reported, not refused (Wordfeud decides).
	p = Preview(gs, []PlacedTile{{7, 9, "T", false}}, trie)
	if p.Problem != "" || p.MainWord != "HUST" || p.AllValid || p.Words[0].Valid {
		t.Fatalf("HUST = %+v", p)
	}
}

func TestPreviewProblems(t *testing.T) {
	gs := gameWith(map[[2]int]string{{7, 6}: "H", {7, 7}: "U", {7, 8}: "S"}, "ETKIAB*")
	cases := []struct {
		name  string
		tiles []PlacedTile
		want  string
	}{
		{"none", nil, "no_tiles"},
		{"occupied", []PlacedTile{{7, 7, "E", false}}, "occupied"},
		{"not in line", []PlacedTile{{7, 9, "E", false}, {8, 10, "T", false}}, "not_in_line"},
		{"gap", []PlacedTile{{7, 9, "E", false}, {7, 11, "T", false}}, "gap"},
		{"not connected", []PlacedTile{{0, 0, "E", false}, {0, 1, "T", false}}, "not_connected"},
		{"not in rack", []PlacedTile{{7, 9, "Z", false}}, "not_in_rack"},
		{"bad letter", []PlacedTile{{7, 9, "1", false}}, "bad_letter"},
		{"two blanks", []PlacedTile{{7, 9, "E", true}, {7, 10, "T", true}}, "not_in_rack"},
	}
	for _, c := range cases {
		if p := Preview(gs, c.tiles, nil); p.Problem != c.want {
			t.Errorf("%s: problem = %q, want %q", c.name, p.Problem, c.want)
		}
	}
}

func TestPlaceTilesRequest(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		if r.URL.Path == "/wf/game/9/move/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "content": map[string]any{"points": 12, "main_word": "HUSET"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "content": map[string]any{"type": "not_your_turn"}})
	}))
	defer srv.Close()
	c := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}

	res, err := c.PlaceTiles("tok", 9, RulesetNorwegian, []PlacedTile{{Row: 7, Col: 9, Letter: "E"}, {Row: 7, Col: 10, Letter: "T", Blank: true}}, []string{"HUSET"})
	if err != nil || res.Points != 12 || path != "/wf/game/9/move/" {
		t.Fatalf("move = %+v, %v (path %s)", res, err, path)
	}
	// Tiles go as [column, row, letter, blank] — the API's [x, y] order.
	move := got["move"].([]any)
	if first := move[0].([]any); first[0] != float64(9) || first[1] != float64(7) || first[2] != "E" || first[3] != false {
		t.Fatalf("first tile = %v", first)
	}
	if got["ruleset"] != float64(RulesetNorwegian) || got["words"].([]any)[0] != "HUSET" {
		t.Fatalf("body = %v", got)
	}

	// Wordfeud's refusals come back typed.
	err = c.Pass("tok", 9)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Type != "not_your_turn" || path != "/wf/game/9/pass/" {
		t.Fatalf("pass = %v (path %s)", err, path)
	}
}

// TestPlayMoveHandlerRoundTrip reads a game as the live API sends it (tiles
// as [x, y, letter, value, wild]), previews and plays a move given in
// rendered rows/columns, and checks the tiles go back in [x, y] order.
func TestPlayMoveHandlerRoundTrip(t *testing.T) {
	database := setupTestDB(t)
	user := createTestUser(t, database)
	encToken, err := encryption.EncryptField("test-token")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.SetPreference(database, user.ID, "wordfeud_session_token", encToken); err != nil {
		t.Fatal(err)
	}

	var moveBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := func(content any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "content": content})
		}
		switch r.URL.Path {
		case "/wf/game/9/":
			ok(map[string]any{"game": map[string]any{
				"id": 9, "is_running": true, "current_player": 0, "bag_count": 40, "ruleset": 1,
				"players": []map[string]any{
					{"username": "robin", "id": 1, "score": 20, "is_local": true},
					{"username": "kari", "id": 2, "score": 18},
				},
				"rack": []any{"E", "T", "K", "I", "A", "B", ""},
				// HUS on row 7, columns 6–8, as [x, y, letter, value, wild].
				"tiles": []any{[]any{6, 7, "H", 3, false}, []any{7, 7, "U", 4, false}, []any{8, 7, "S", 1, false}},
			}})
		case "/wf/game/9/move/":
			_ = json.NewDecoder(r.Body).Decode(&moveBody)
			ok(map[string]any{"points": 9})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
	cache := NewGameCache()

	call := func(h http.HandlerFunc, body string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, "/api/wordfeud/games/9/x", strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "9")
		req = requestWithUser(req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)), user)
		w := httptest.NewRecorder()
		h(w, req)
		var out map[string]any
		_ = json.NewDecoder(w.Body).Decode(&out)
		return w.Code, out
	}

	tiles := `{"tiles":[{"row":7,"col":9,"letter":"E"},{"row":7,"col":10,"letter":"T"}]}`
	code, prev := call(PreviewMoveHandler(database, client, cache, nil), tiles)
	if code != http.StatusOK || prev["problem"] != "" || prev["main_word"] != "HUSET" {
		t.Fatalf("preview = %d %v", code, prev)
	}
	code, res := call(PlayMoveHandler(database, client, cache, nil), tiles)
	if code != http.StatusOK || res["points"] != float64(9) || res["main_word"] != "HUSET" {
		t.Fatalf("move = %d %v", code, res)
	}
	move := moveBody["move"].([]any)
	if first := move[0].([]any); first[0] != float64(9) || first[1] != float64(7) {
		t.Fatalf("tile sent as %v, want [9, 7, …] (x = column, y = row)", first)
	}
	if moveBody["words"].([]any)[0] != "HUSET" || moveBody["ruleset"] != float64(1) {
		t.Fatalf("move body = %v", moveBody)
	}
	// A placement that can't be played never reaches Wordfeud.
	moveBody = nil
	code, res = call(PlayMoveHandler(database, client, cache, nil), `{"tiles":[{"row":0,"col":0,"letter":"E"}]}`)
	if code != http.StatusUnprocessableEntity || res["error"] != "too_short" || moveBody != nil {
		t.Fatalf("bad move = %d %v (sent %v)", code, res, moveBody)
	}
}
