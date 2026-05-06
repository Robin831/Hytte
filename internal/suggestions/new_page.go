package suggestions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

// NewPageWindowDays is the look-back window for prior new_page suggestions
// embedded in the prompt for de-duplication. The window matches the bead
// specification (last 30 days).
const NewPageWindowDays = 30

// NewPageTimeout caps the single Claude call (plus one retry on malformed
// JSON). Mirrors PerPageTimeout in shape but is set independently so the
// new-page pass can be tuned without affecting per-page generation.
const NewPageTimeout = 90 * time.Second

// newPageGenerated is the JSON shape we expect for a single new-page
// suggestion. Type is fixed to TypeNewPage on insert and is therefore not
// included in the schema sent to Claude.
type newPageGenerated struct {
	Size  string `json:"size"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// RunNewPageSuggestion runs a separate Claude pass that proposes one entirely
// new page idea. On success it inserts a single row with page_slug set to the
// synthetic NewPageSlug, type=TypeNewPage, source=SourceClaude, and
// status=StatusPending. The function returns counts in a RunResult so the
// caller can fold them into the overall nightly/run-handler totals.
//
// Errors at any step (loading context, calling Claude, parsing JSON, inserting)
// are logged and surfaced via RunResult.Errors. The returned error is reserved
// for context cancellation so the scheduler can decide whether to abort the
// run; all other failures are non-fatal and counted in Errors.
func RunNewPageSuggestion(
	ctx context.Context,
	db *sql.DB,
	cfg *training.ClaudeConfig,
	userID int64,
) (RunResult, error) {
	var result RunResult

	if err := ctx.Err(); err != nil {
		return result, err
	}

	runCtx, cancel := context.WithTimeout(ctx, NewPageTimeout)
	defer cancel()

	registered := AllRegistered()

	recent, err := recentNewPageSuggestions(runCtx, db, userID, NewPageWindowDays)
	if err != nil {
		log.Printf("suggestions: load recent new_page suggestions: %v; continuing without", err)
		recent = nil
	}

	prompt := buildNewPagePrompt(registered, recent)

	parsed, err := callAndParseNewPage(runCtx, cfg, prompt)
	if err != nil {
		log.Printf("suggestions: new_page generation failed: %v", err)
		result.Errors++
		return result, nil
	}

	row := Suggestion{
		UserID:      userID,
		GeneratedAt: time.Now().UTC(),
		PageSlug:    NewPageSlug,
		Source:      SourceClaude,
		Type:        TypeNewPage,
		Size:        parsed.Size,
		Title:       parsed.Title,
		Body:        parsed.Body,
		Status:      StatusPending,
	}
	if _, err := Insert(runCtx, db, row); err != nil {
		log.Printf("suggestions: insert new_page suggestion: %v", err)
		result.Errors++
		return result, nil
	}
	result.Generated++
	return result, nil
}

// callAndParseNewPage invokes runPromptFn and parses the response, retrying
// once on malformed JSON. The retry uses the same prompt — no nudge text is
// appended because the original prompt already insists on JSON-only output.
// This mirrors the behaviour in callAndParse for per-page generation so both
// passes have the same retry semantics.
func callAndParseNewPage(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (newPageGenerated, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxRetriesOnMalformedJSON; attempt++ {
		resp, err := runPromptFn(ctx, cfg, prompt)
		if err != nil {
			return newPageGenerated{}, fmt.Errorf("claude prompt: %w", err)
		}
		parsed, err := parseNewPageResponse(resp)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
		log.Printf("suggestions: new_page parse attempt %d failed: %v", attempt+1, err)
	}
	return newPageGenerated{}, fmt.Errorf("parse new_page response after %d attempts: %w", MaxRetriesOnMalformedJSON+1, lastErr)
}

// parseNewPageResponse strips an optional markdown fence and decodes a single
// validated newPageGenerated object. Mirrors parseSuggestionsResponse but
// expects an object, not an array, and validates only size/title/body.
func parseNewPageResponse(response string) (newPageGenerated, error) {
	response = strings.TrimSpace(response)

	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) >= 3 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var item newPageGenerated
	if err := json.Unmarshal([]byte(response), &item); err != nil {
		return newPageGenerated{}, fmt.Errorf("unmarshal: %w", err)
	}
	if !validSizes[item.Size] {
		return newPageGenerated{}, fmt.Errorf("invalid size %q", item.Size)
	}
	if strings.TrimSpace(item.Title) == "" {
		return newPageGenerated{}, fmt.Errorf("empty title")
	}
	if strings.TrimSpace(item.Body) == "" {
		return newPageGenerated{}, fmt.Errorf("empty body")
	}
	return item, nil
}

// buildNewPagePrompt assembles the prompt for a single new-page idea. The
// registry is included so Claude does not propose a page that already exists,
// and recent new_page suggestions are listed so it can avoid repeating its
// own past ideas. Output schema is a single JSON object with size/title/body.
func buildNewPagePrompt(allRegistered []Page, recentNewPageSuggestions []Suggestion) string {
	var sb strings.Builder

	sb.WriteString("You are a senior product engineer brainstorming new pages for a personal web app called Hytte.\n")
	sb.WriteString("Your job: propose ONE entirely new page that does not already exist in the registry below.\n")
	sb.WriteString("Pick something genuinely useful — not a thin variant of an existing page.\n\n")

	if len(allRegistered) > 0 {
		sb.WriteString("## Existing pages (do NOT propose anything that overlaps with these)\n")
		for _, p := range allRegistered {
			fmt.Fprintf(&sb, "- %s — %s", p.Slug, p.Title)
			if p.Description != "" {
				fmt.Fprintf(&sb, ": %s", p.Description)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(recentNewPageSuggestions) > 0 {
		sb.WriteString("## Recent new-page suggestions (avoid repeating these)\n")
		for _, s := range recentNewPageSuggestions {
			fmt.Fprintf(&sb, "- [%s/%s] %s\n", s.Status, s.Size, s.Title)
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`## Output format

Return ONLY a single JSON object. Do not wrap in markdown fences.
The object must have these fields and only these fields:

- "size": one of "s" (under a day), "m" (one to three days), "l" (more than three days)
- "title": short imperative sentence naming the new page, max 80 chars, no trailing period
- "body": 2 to 5 sentences explaining what the page does, who it is for, and the user-visible value it adds

Example shape (do NOT copy these contents):
{"size": "m", "title": "...", "body": "..."}
`)

	return sb.String()
}

// recentNewPageSuggestions returns suggestions of type new_page whose status
// is not "rejected" and that were generated within the last `days` days.
// Used by buildNewPagePrompt to discourage repeats.
func recentNewPageSuggestions(ctx context.Context, db *sql.DB, userID int64, days int) ([]Suggestion, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, generated_at, page_slug, source, type, size,
		       title, body_enc, status, feedback_enc, plan_enc, bead_id,
		       rejected_at, planned_at, bead_created_at
		FROM suggestions
		WHERE user_id = ? AND type = ?
		  AND status IN (?, ?, ?)
		  AND generated_at >= ?
		ORDER BY generated_at DESC
	`, userID, TypeNewPage,
		StatusPending, StatusPlanned, StatusBeadCreated,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("recent new_page suggestions: %w", err)
	}
	defer rows.Close()

	var out []Suggestion
	for rows.Next() {
		s, err := scanSuggestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
