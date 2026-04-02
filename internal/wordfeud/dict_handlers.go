package wordfeud

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"
)

// FindHandler handles POST /api/wordfeud/find.
// Accepts JSON {"letters": "ABCDE*"} and returns word suggestions ranked by score.
// Letters should be A-Z, Æ, Ø, Å or * for blanks. Max 7 letters (one full rack).
func FindHandler(dict *Dictionary) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)

		var body struct {
			Letters string `json:"letters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		letters := strings.TrimSpace(body.Letters)
		if letters == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "letters is required"})
			return
		}

		// Validate: only A-Z, Æ, Ø, Å, * allowed, max 7 characters (one full rack).
		upper := strings.ToUpper(letters)
		count := utf8.RuneCountInString(upper)
		if count > 7 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "maximum 7 letters allowed"})
			return
		}
		for _, r := range upper {
			if r != '*' && !isWordfeudLetter(r) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid character in letters — use A-Z, Æ, Ø, Å, or * for blank"})
				return
			}
		}

		trie, err := dict.Trie()
		if err != nil {
			log.Printf("wordfeud: failed to load dictionary from %q: %v", dict.path, err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "dictionary not available"})
			return
		}

		results := FindWords(trie, upper)
		totalMatches := len(results)

		// Limit results to top 200 to keep response size reasonable (does not affect totalMatches).
		if len(results) > 200 {
			results = results[:200]
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"words":    results,
			"total":    totalMatches,
			"returned": len(results),
		})
	}
}

// SearchHandler handles POST /api/wordfeud/search.
// Accepts JSON {"pattern": "HE", "mode": "starts_with|ends_with|contains", "letters": "ABC*"} and returns
// matching words ranked by score. Pattern must be 1-15 letters (A-Z, Æ, Ø, Å).
// The optional "letters" field (rack letters, max 7, may include '*' for blanks) restricts
// results to words formable from the combined pool of pattern + rack letters.
func SearchHandler(dict *Dictionary) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)

		var body struct {
			Pattern string `json:"pattern"`
			Mode    string `json:"mode"`
			Letters string `json:"letters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		pattern := strings.TrimSpace(body.Pattern)
		if pattern == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pattern is required"})
			return
		}

		upper := strings.ToUpper(pattern)
		count := utf8.RuneCountInString(upper)
		if count > 15 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pattern too long (max 15 characters)"})
			return
		}
		for _, r := range upper {
			if !isWordfeudLetter(r) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid character in pattern — use A-Z, Æ, Ø, Å"})
				return
			}
		}

		// Optional rack letters to constrain results to formable words.
		letters := strings.TrimSpace(strings.ToUpper(body.Letters))
		if letters != "" {
			if utf8.RuneCountInString(letters) > 7 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "maximum 7 rack letters allowed"})
				return
			}
			for _, r := range letters {
				if r != '*' && !isWordfeudLetter(r) {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid character in letters — use A-Z, Æ, Ø, Å, or * for blank"})
					return
				}
			}
		}

		mode := strings.TrimSpace(body.Mode)
		switch mode {
		case "starts_with", "ends_with", "contains":
			// valid
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be starts_with, ends_with, or contains"})
			return
		}

		trie, err := dict.Trie()
		if err != nil {
			log.Printf("wordfeud: failed to load dictionary from %q: %v", dict.path, err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "dictionary not available"})
			return
		}

		allResults := SearchWords(trie, upper, mode, letters)
		total := len(allResults)

		results := allResults
		if len(results) > 200 {
			results = results[:200]
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"words":    results,
			"total":    total,
			"returned": len(results),
		})
	}
}

// ValidateHandler handles POST /api/wordfeud/validate.
// Accepts JSON {"word": "HEST"} and returns whether the word exists in the dictionary.
func ValidateHandler(dict *Dictionary) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)

		var body struct {
			Word string `json:"word"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		word := strings.TrimSpace(body.Word)
		if word == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "word is required"})
			return
		}

		upper := strings.ToUpper(word)
		if utf8.RuneCountInString(upper) > 15 {
			writeJSON(w, http.StatusOK, map[string]any{"word": upper, "valid": false})
			return
		}

		trie, err := dict.Trie()
		if err != nil {
			log.Printf("wordfeud: failed to load dictionary from %q: %v", dict.path, err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "dictionary not available"})
			return
		}

		valid := trie.Contains(upper)
		writeJSON(w, http.StatusOK, map[string]any{
			"word":  upper,
			"valid": valid,
		})
	}
}

// TilesHandler handles GET /api/wordfeud/tiles.
// Returns the Norwegian Wordfeud tile distribution (104 tiles).
func TilesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		total := 0
		for _, t := range NorwegianTiles {
			total += t.Count
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tiles": NorwegianTiles,
			"total": total,
		})
	}
}
