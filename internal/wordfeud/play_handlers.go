package wordfeud

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// Invalidate drops a cached game, so the next read shows a move just made.
func (c *GameCache) Invalidate(userID, gameID int64) {
	c.mu.Lock()
	delete(c.entries, cacheKey{userID: userID, gameID: gameID})
	c.mu.Unlock()
}

// playContext is what a play handler works with.
type playContext struct {
	w      http.ResponseWriter
	userID int64
	gameID int64
	token  string
}

// withGameSession resolves the game id and the user's Wordfeud session, runs fn,
// and on an expired session logs in again and runs fn once more. fn returns
// the error from the Wordfeud call.
func withGameSession(db *sql.DB, client *Client, w http.ResponseWriter, r *http.Request, fn func(pc playContext) error) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid game ID"})
		return
	}
	token, err := getSessionToken(db, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read preferences"})
		return
	}
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no Wordfeud session token configured — add it in Settings"})
		return
	}
	pc := playContext{w: w, userID: user.ID, gameID: gameID, token: token}
	err = fn(pc)
	if errors.Is(err, ErrSessionExpired) {
		newToken, reloginErr := autoRelogin(db, user.ID, client)
		if reloginErr != nil {
			log.Printf("Wordfeud auto-relogin failed: %v", reloginErr)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Wordfeud session expired and auto-relogin failed — please re-authenticate in Settings"})
			return
		}
		pc.token = newToken
		err = fn(pc)
	}
	if err != nil {
		writePlayError(w, gameID, err)
	}
}

// writePlayError reports a failed Wordfeud call. Wordfeud's own refusals
// (illegal_word, not_your_turn, …) come back as 422 with their code so the
// page can explain them.
func writePlayError(w http.ResponseWriter, gameID int64, err error) {
	var apiErr *APIError
	switch {
	case errors.As(err, &apiErr):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": apiErr.Type, "message": apiErr.Message})
	case errors.Is(err, ErrGameNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "game not found"})
	case errors.Is(err, ErrSessionExpired):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Wordfeud session expired"})
	default:
		log.Printf("Wordfeud play error (game %d): %v", gameID, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "wordfeud_unreachable"})
	}
}

type tilesBody struct {
	Tiles []PlacedTile `json:"tiles"`
}

func decodeTiles(w http.ResponseWriter, r *http.Request) ([]PlacedTile, bool) {
	var body tilesBody
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return nil, false
	}
	return body.Tiles, true
}

func trieOf(dict *Dictionary) *Trie {
	if dict == nil {
		return nil
	}
	t, err := dict.Trie()
	if err != nil {
		log.Printf("wordfeud: dictionary unavailable for preview: %v", err)
		return nil
	}
	return t
}

// PreviewMoveHandler checks a placement: shape, rack, words, score.
// POST /api/wordfeud/games/{id}/preview {"tiles": [...]}
func PreviewMoveHandler(db *sql.DB, client *Client, cache *GameCache, dict *Dictionary) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tiles, ok := decodeTiles(w, r)
		if !ok {
			return
		}
		withGameSession(db, client, w, r, func(pc playContext) error {
			gs, err := GetGameCached(client, cache, pc.token, pc.userID, pc.gameID)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, Preview(gs, tiles, trieOf(dict)))
			return nil
		})
	}
}

// PlayMoveHandler plays a placement after checking it against a fresh copy
// of the game. Our dictionary is advisory — Wordfeud has the last word.
// POST /api/wordfeud/games/{id}/move {"tiles": [...]}
func PlayMoveHandler(db *sql.DB, client *Client, cache *GameCache, dict *Dictionary) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tiles, ok := decodeTiles(w, r)
		if !ok {
			return
		}
		withGameSession(db, client, w, r, func(pc playContext) error {
			gs, err := client.GetGame(pc.token, pc.gameID)
			if err != nil {
				return err
			}
			if !gs.IsRunning {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "game_over"})
				return nil
			}
			if !gs.IsMyTurn {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "not_your_turn"})
				return nil
			}
			p := Preview(gs, tiles, trieOf(dict))
			if p.Problem != "" {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": p.Problem})
				return nil
			}
			ruleset := gs.Ruleset
			if ruleset < 0 {
				ruleset = RulesetNorwegian
			}
			res, err := client.PlaceTiles(pc.token, pc.gameID, ruleset, tiles, []string{p.MainWord})
			if err != nil {
				return err
			}
			cache.Invalidate(pc.userID, pc.gameID)
			if res.Points == 0 {
				res.Points = p.Score
			}
			if res.MainWord == "" {
				res.MainWord = p.MainWord
			}
			writeJSON(w, http.StatusOK, res)
			return nil
		})
	}
}

// PassHandler passes the turn. POST /api/wordfeud/games/{id}/pass
func PassHandler(db *sql.DB, client *Client, cache *GameCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		withGameSession(db, client, w, r, func(pc playContext) error {
			if err := client.Pass(pc.token, pc.gameID); err != nil {
				return err
			}
			cache.Invalidate(pc.userID, pc.gameID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "passed"})
			return nil
		})
	}
}

// SwapHandler swaps rack tiles. POST /api/wordfeud/games/{id}/swap
// {"letters": ["A", "*"]} — "*" is a blank.
func SwapHandler(db *sql.DB, client *Client, cache *GameCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Letters []string `json:"letters"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Letters) == 0 || len(body.Letters) > maxRackSize {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pick 1–7 tiles to swap"})
			return
		}
		withGameSession(db, client, w, r, func(pc playContext) error {
			gs, err := client.GetGame(pc.token, pc.gameID)
			if err != nil {
				return err
			}
			have := rackCounts(gs)
			letters := make([]string, len(body.Letters))
			for i, l := range body.Letters {
				l = strings.ToUpper(strings.TrimSpace(l))
				have[l]--
				if have[l] < 0 {
					writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "not_in_rack"})
					return nil
				}
				if l == "*" {
					l = ""
				}
				letters[i] = l
			}
			if gs.BagCount < maxRackSize {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "bag_too_small"})
				return nil
			}
			if err := client.Swap(pc.token, pc.gameID, letters); err != nil {
				return err
			}
			cache.Invalidate(pc.userID, pc.gameID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "swapped"})
			return nil
		})
	}
}

// ResignHandler resigns the game. POST /api/wordfeud/games/{id}/resign
func ResignHandler(db *sql.DB, client *Client, cache *GameCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		withGameSession(db, client, w, r, func(pc playContext) error {
			if err := client.Resign(pc.token, pc.gameID); err != nil {
				return err
			}
			cache.Invalidate(pc.userID, pc.gameID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "resigned"})
			return nil
		})
	}
}
