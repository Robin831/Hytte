package suggestions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Robin831/Hytte/internal/training"
)

// ErrEmptyPlan is returned by PlanSuggestion when Claude produced no content.
var ErrEmptyPlan = errors.New("claude returned an empty plan")

// ErrPlanPersist wraps storage failures after a plan was generated.
var ErrPlanPersist = errors.New("failed to persist plan")

// PlanSuggestion generates an implementation plan for the suggestion via the
// Claude CLI (bounded by PlanTimeout) and persists it, moving the suggestion
// to planned. Shared by PlanHandler and maintenance tooling. Callers can
// distinguish outcomes via errors.Is: context.DeadlineExceeded (CLI timeout),
// ErrEmptyPlan, and ErrPlanPersist; anything else is a CLI failure.
func PlanSuggestion(ctx context.Context, db *sql.DB, cfg *training.ClaudeConfig, s *Suggestion, feedback string) (*Suggestion, error) {
	page := findPageBySlug(s.PageSlug)
	prompt := buildPlanPrompt(*s, page, feedback)

	cliCtx, cancel := context.WithTimeout(ctx, PlanTimeout)
	defer cancel()

	plan, _, err := runPromptFn(cliCtx, cfg, prompt)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(cliCtx.Err(), context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("run plan prompt: %w", err)
	}

	plan = strings.TrimSpace(plan)
	if plan == "" {
		return nil, ErrEmptyPlan
	}

	if err := MarkPlanned(ctx, db, s.ID, plan); err != nil {
		return nil, fmt.Errorf("%w: save: %v", ErrPlanPersist, err)
	}
	updated, err := GetByID(ctx, db, s.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: reload: %v", ErrPlanPersist, err)
	}
	return updated, nil
}
