package trips

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/races"
	"github.com/Robin831/Hytte/internal/training"
)

// Every trip text is kept in nb, en and th. When someone writes a text in one
// language, the others are filled in by Claude in the background; until then
// the page falls back to the language that exists.

// gap is one text missing some languages.
type gap struct {
	ID      string            `json:"id"`
	Have    map[string]string `json:"have"`
	Missing []string          `json:"missing"`
	set     func(lang, text string)
}

func isI18nMap(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	for k, v := range m {
		if k != "nb" && k != "en" && k != "th" {
			return false
		}
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

// collectGaps walks a JSON value for {nb,en,th} objects missing languages.
func collectGaps(v any, gaps *[]gap, next *int) {
	switch t := v.(type) {
	case map[string]any:
		if isI18nMap(t) {
			have := map[string]string{}
			var missing []string
			for _, l := range Languages {
				if s, _ := t[l].(string); strings.TrimSpace(s) != "" {
					have[l] = s
				} else {
					missing = append(missing, l)
				}
			}
			if len(have) > 0 && len(missing) > 0 {
				*next++
				m := t
				*gaps = append(*gaps, gap{ID: strconv.Itoa(*next), Have: have, Missing: missing,
					set: func(lang, text string) { m[lang] = text }})
			}
			return
		}
		for _, child := range t {
			collectGaps(child, gaps, next)
		}
	case []any:
		for _, child := range t {
			collectGaps(child, gaps, next)
		}
	}
}

// Translator fills missing languages. Run is injectable for tests.
type Translator struct {
	DB     *sql.DB
	Run    func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, float64, error)
	Config func(ctx context.Context, db *sql.DB) (*training.ClaudeConfig, error)
}

// NewTranslator wires the real Claude CLI and the shared research budget.
func NewTranslator(db *sql.DB) *Translator {
	return &Translator{DB: db, Run: training.RunPromptWithCost, Config: func(ctx context.Context, db *sql.DB) (*training.ClaudeConfig, error) {
		return races.BackgroundClaude(ctx, db, races.TranslationModel)
	}}
}

const translatePrompt = `Translate short texts from a family trip planner (flights, hotels, checklists, day plans). Languages: nb = Norwegian bokmål, en = English, th = Thai (natural Thai, Gregorian years).
For each item, translate the text it has into each language listed in "missing". Keep names, places, flight numbers, codes, addresses, phone numbers, prices and times exactly as written. Keep the tone short and practical.

Items:
%s

Reply with ONLY a JSON object mapping each item id to an object of the missing languages, e.g. {"1": {"en": "...", "th": "..."}}`

// fill translates the gaps in place with one Claude call and returns the
// number of texts filled.
func (t *Translator) fill(ctx context.Context, gaps []gap, userID int64, what string) int {
	if len(gaps) == 0 {
		return 0
	}
	cfg, err := t.Config(ctx, t.DB)
	if err != nil {
		log.Printf("trips: translation skipped (%s): %v", what, err)
		return 0
	}
	items, _ := json.MarshalIndent(gaps, "", " ")
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	reply, cost, err := t.Run(callCtx, cfg, fmt.Sprintf(translatePrompt, items))
	if err != nil {
		races.LogBackgroundRun(ctx, t.DB, "translate", userID, cost, 0, what, err.Error())
		return 0
	}
	start := strings.Index(reply, "{")
	var out map[string]map[string]string
	if start < 0 || json.NewDecoder(strings.NewReader(reply[start:])).Decode(&out) != nil {
		races.LogBackgroundRun(ctx, t.DB, "translate", userID, cost, 0, what, "could not read the translation reply")
		return 0
	}
	filled := 0
	for _, g := range gaps {
		for _, lang := range g.Missing {
			if text := strings.TrimSpace(out[g.ID][lang]); text != "" {
				g.set(lang, text)
				filled++
			}
		}
	}
	races.LogBackgroundRun(ctx, t.DB, "translate", userID, cost, filled, what, "")
	return filled
}

// TranslateTrip fills missing languages in a trip's document and checklists.
// It only writes back when the trip hasn't changed meanwhile.
func (t *Translator) TranslateTrip(ctx context.Context, tripID, userID int64) {
	var enc, updated string
	if err := t.DB.QueryRowContext(ctx, `SELECT doc, updated_at FROM trips WHERE id = ?`, tripID).Scan(&enc, &updated); err != nil {
		return
	}
	var generic any
	var doc Doc
	if err := decryptJSON(enc, &doc); err != nil {
		return
	}
	b, _ := json.Marshal(doc)
	_ = json.Unmarshal(b, &generic)

	var gaps []gap
	next := 0
	collectGaps(generic, &gaps, &next)

	// Checklist texts are rows; collect them too.
	type rowText struct {
		table, id string
		value     any
	}
	var rowsToSave []rowText
	groups, _ := t.DB.QueryContext(ctx, `SELECT id, title FROM trip_check_groups WHERE trip_id = ?`, tripID)
	if groups != nil {
		for groups.Next() {
			var id int64
			var title string
			if groups.Scan(&id, &title) != nil {
				continue
			}
			var v any
			if decryptJSON(title, &v) == nil {
				collectGaps(map[string]any{"t": v}, &gaps, &next)
				rowsToSave = append(rowsToSave, rowText{"trip_check_groups", strconv.FormatInt(id, 10), v})
			}
		}
		groups.Close()
	}
	items, _ := t.DB.QueryContext(ctx, `SELECT id, texts FROM trip_check_items WHERE trip_id = ?`, tripID)
	if items != nil {
		for items.Next() {
			var id int64
			var texts string
			if items.Scan(&id, &texts) != nil {
				continue
			}
			var v any
			if decryptJSON(texts, &v) == nil {
				collectGaps(v, &gaps, &next)
				rowsToSave = append(rowsToSave, rowText{"trip_check_items", strconv.FormatInt(id, 10), v})
			}
		}
		items.Close()
	}

	if t.fill(ctx, gaps, userID, fmt.Sprintf("trip %d", tripID)) == 0 {
		return
	}
	if b, err := json.Marshal(generic); err == nil {
		var filled Doc
		if json.Unmarshal(b, &filled) == nil {
			if encDoc, err := encryptJSON(filled); err == nil {
				if _, err := t.DB.ExecContext(ctx, `UPDATE trips SET doc = ? WHERE id = ? AND updated_at = ?`, encDoc, tripID, updated); err != nil {
					log.Printf("trips: save translations for trip %d: %v", tripID, err)
				}
			}
		}
	}
	for _, r := range rowsToSave {
		enc, err := encryptJSON(r.value)
		if err != nil {
			continue
		}
		col := "texts"
		if r.table == "trip_check_groups" {
			col = "title"
		}
		if _, err := t.DB.ExecContext(ctx, `UPDATE `+r.table+` SET `+col+` = ? WHERE id = ?`, enc, r.id); err != nil {
			log.Printf("trips: save checklist translations: %v", err)
		}
	}
}

// TranslateSoon runs TranslateTrip in the background.
func TranslateSoon(db *sql.DB, tripID, userID int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		NewTranslator(db).TranslateTrip(ctx, tripID, userID)
	}()
}
