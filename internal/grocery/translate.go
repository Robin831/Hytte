package grocery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Robin831/Hytte/internal/training"
)

// TranslatedItem represents a single grocery item after translation/normalization.
type TranslatedItem struct {
	Content        string `json:"item"`
	OriginalText   string `json:"original"`
	SourceLanguage string `json:"language"`
}

// promptRunner abstracts the Claude CLI call for testability.
type promptRunner func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, error)

// runPrompt is the default prompt runner. Override in tests.
var runPrompt promptRunner = training.RunPrompt

// TranslateAndNormalize sends grocery input text to Claude CLI for translation
// to Norwegian and normalization for a shopping list.
func TranslateAndNormalize(ctx context.Context, cfg *training.ClaudeConfig, input string) ([]TranslatedItem, error) {
	prompt := fmt.Sprintf(`Translate these grocery items to Norwegian. Input language: auto-detect. Return a JSON array of objects with keys "item" (Norwegian name), "original" (original text), and "language" (detected ISO 639-1 code, e.g. "th", "en", "nb"). Normalize for a shopping list (standard Norwegian grocery names, include quantities if mentioned). If already Norwegian, just normalize. Return ONLY the JSON array, no markdown fences or extra text.

Input: %s`, input)

	output, err := runPrompt(ctx, cfg, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude translation: %w", err)
	}

	// Strip markdown code fences if present.
	output = strings.TrimSpace(output)
	if strings.HasPrefix(output, "```") {
		lines := strings.Split(output, "\n")
		// Remove first line (```json) and last line (```)
		if len(lines) >= 3 {
			output = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	output = strings.TrimSpace(output)

	var items []TranslatedItem
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		const maxPreview = 200
		preview := output
		if len(preview) > maxPreview {
			preview = preview[:maxPreview] + "..."
		}
		return nil, fmt.Errorf("parsing claude response (output length %d): %w (preview: %s)", len(output), err, preview)
	}

	return items, nil
}
