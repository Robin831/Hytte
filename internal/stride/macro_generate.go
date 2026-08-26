package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

// macroClaudeTimeout bounds the single Claude call that produces a whole
// 26-week block. It mirrors evalClaudeTimeout (evaluate.go): the macro prompt
// is larger and its answer longer than an evaluation's, so anything tighter
// would be the first thing to break under an API latency spike, and on expiry
// exec.CommandContext SIGKILLs the CLI mid-flight.
const macroClaudeTimeout = 300 * time.Second

// ErrStrideNotEnabled is returned when the athlete has not enabled Stride.
// Generating a block spends a long Claude call on someone who has switched the
// feature off, so the gate is checked before anything else is loaded.
var ErrStrideNotEnabled = errors.New("Stride is not enabled — enable it in settings")

// ErrNoPreviousMacroPlan is returned when extension mode is asked to continue a
// block the athlete does not have. An extension is defined by what it
// continues, so with nothing to continue the caller wants MacroModeScheduled
// instead — silently planning a fresh block under the "extension" label would
// write a lie into generated_by.
var ErrNoPreviousMacroPlan = errors.New("no previous macro block to extend")

// GenerateMacroPlan builds, validates and persists one MacroBlockWeeks-week
// macro training block for the athlete, starting at the Monday of startWeek's
// week. startWeek is a YYYY-MM-DD date like every other week key in the package
// and need not be a Monday — it is snapped through NormaliseMacroStartWeek.
//
// The flow is: assemble the athlete inputs (buildMacroInputs), pair them with
// macroInstructions, call Claude through the runPromptFunc seam under
// macroClaudeTimeout, decode the answer into MacroPlanResponse, check it with
// ValidateMacroPlan against the same horizon, cap and race calendar the prompt
// was built from, and only then write it. Validation happens before the write
// on purpose: CreateMacroPlan's transaction covers the plan row, its weeks and
// the block's 'initial' goal revision, and a rejected plan must never reach it.
// The prompt and the raw answer are stored with the block; CreateMacroPlan
// encrypts both (along with the goal and every week's prose) before they reach
// SQLite, the same way the analysis feature protects its prompt/response pair.
//
// mode decides both what the prompt is seeded with and what the block is
// recorded as. Extension mode continues the block that is running out — its
// goal, periodisation and closing weeks go into the prompt so the new block
// picks up where the old one ends instead of restarting from base — and fails
// with ErrNoPreviousMacroPlan when there is nothing to continue. Scheduled and
// manual mode replace whatever active block already covers the start week, if
// any. In every mode the plan the new block follows is recorded as
// PreviousPlanID, which is what retires it in the same transaction that
// inserts the new one. That transaction re-checks the athlete's active blocks
// itself, so a second generation racing this one is rejected with
// ErrOverlappingMacroPlan rather than leaving two active blocks behind.
//
// On success the returned plan carries its assigned ID, created_at and weeks.
func GenerateMacroPlan(ctx context.Context, db *sql.DB, userID int64, startWeek string, mode MacroMode) (*MacroPlan, error) {
	if !mode.valid() {
		return nil, fmt.Errorf("invalid macro mode %q", mode)
	}

	// Every entry point computes its start from a clock, a previous block's end
	// week or user input, so snap it to a Monday rather than letting
	// buildMacroInputs reject it.
	start, err := NormaliseMacroStartWeek(startWeek)
	if err != nil {
		return nil, fmt.Errorf("normalise start week: %w", err)
	}
	startDate, err := parseMondayWeek(start)
	if err != nil {
		return nil, fmt.Errorf("parse start week: %w", err)
	}

	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return nil, fmt.Errorf("load preferences: %w", err)
	}
	if prefs["stride_enabled"] != "true" {
		return nil, ErrStrideNotEnabled
	}

	claudeCfg, err := training.LoadClaudeConfig(db, userID)
	if err != nil {
		return nil, fmt.Errorf("load Claude config: %w", err)
	}
	if !claudeCfg.Enabled {
		return nil, training.ErrClaudeNotEnabled
	}
	// Same default as the weekly generator: a block is the harder of the two
	// jobs, so it must not silently run on a cheaper model than the week.
	applyStrideModelDefault(prefs, claudeCfg)

	// Resolved before the Claude call so an extension with nothing to continue
	// fails in milliseconds instead of after a 26-week generation. It is only
	// the *lineage* that is decided here — CreateMacroPlan re-checks which
	// blocks are actually active when it writes.
	previous, err := macroPreviousPlan(ctx, db, userID, start, mode)
	if err != nil {
		return nil, err
	}

	// The race calendar comes back with the inputs, so the answer is checked
	// against exactly the races the prompt showed the coach. Reading it a
	// second time here would let an edit made during the (up to 300s) call
	// reject a plan for a mismatch it could not have known about.
	inputs, races, err := buildMacroInputs(ctx, db, userID, start, mode)
	if err != nil {
		return nil, fmt.Errorf("build macro inputs: %w", err)
	}
	prompt := macroInstructions + "\n\n" + inputs

	callCtx, cancel := context.WithTimeout(ctx, macroClaudeTimeout)
	defer cancel()
	response, err := runPromptFunc(callCtx, claudeCfg, prompt)
	if err != nil {
		return nil, fmt.Errorf("Claude prompt: %w", err)
	}

	parsed, err := parseMacroPlanResponse(response)
	if err != nil {
		return nil, err
	}

	if err := ValidateMacroPlan(parsed, MacroValidationContext{
		StartWeek:         start,
		WeeklyDistanceCap: parseWeeklyDistanceCap(prefs["stride_weekly_distance_cap"]),
		Races:             races,
	}); err != nil {
		return nil, err
	}

	plan := &MacroPlan{
		UserID:        userID,
		StartWeek:     start,
		EndWeek:       macroEndWeek(startDate),
		Status:        MacroPlanStatusActive,
		Goal:          parsed.Goal,
		Periodisation: parsed.Mesocycles,
		Prompt:        prompt,
		Response:      response,
		Model:         claudeCfg.Model,
		GeneratedBy:   string(mode),
	}
	if previous != nil {
		plan.PreviousPlanID = &previous.ID
	}

	reason := fmt.Sprintf("Initial goal for the %d-week block starting %s (%s).",
		MacroBlockWeeks, start, mode)
	if err := CreateMacroPlan(ctx, db, plan, macroWeeksFromResponse(parsed.Weeks), reason); err != nil {
		return nil, fmt.Errorf("persist macro plan: %w", err)
	}
	return plan, nil
}

// macroPreviousPlan returns the block the new one follows, or nil when there is
// none. What counts as "the previous block" depends on the mode: an extension
// continues the block whose horizon is running out, while a scheduled or manual
// block replaces whatever active block already covers the start week. Both end
// up in PreviousPlanID, which retires that block as part of CreateMacroPlan's
// transaction.
func macroPreviousPlan(ctx context.Context, db *sql.DB, userID int64, startWeek string, mode MacroMode) (*MacroPlan, error) {
	if mode == MacroModeExtension {
		prev, err := loadPreviousMacroPlan(ctx, db, userID, startWeek)
		if err != nil {
			return nil, fmt.Errorf("load previous macro plan: %w", err)
		}
		if prev == nil {
			return nil, ErrNoPreviousMacroPlan
		}
		return prev, nil
	}
	plan, err := GetActiveMacroPlan(ctx, db, userID, startWeek)
	if err != nil {
		return nil, fmt.Errorf("load active macro plan: %w", err)
	}
	return plan, nil
}

// parseMacroPlanResponse decodes the coach's answer into the response types.
// Code fences are stripped through the shared stripCodeFence helper — the same
// unwrapping parsePlanResponse gives the weekly plan, so a fix to that
// heuristic lands once and covers both response paths.
func parseMacroPlanResponse(response string) (*MacroPlanResponse, error) {
	var plan MacroPlanResponse
	if err := json.Unmarshal([]byte(stripCodeFence(response)), &plan); err != nil {
		return nil, fmt.Errorf("unmarshal macro plan JSON: %w", err)
	}
	return &plan, nil
}

// macroWeeksFromResponse turns the coach's weeks into rows for the store. Every
// column the coach does not author — ids, ownership, the plan it belongs to —
// is left for CreateMacroPlan to fill in; status is pinned to 'planned' because
// a freshly generated week has by definition not been materialised yet.
func macroWeeksFromResponse(weeks []MacroWeekResponse) []MacroWeek {
	rows := make([]MacroWeek, len(weeks))
	for i, w := range weeks {
		rows[i] = MacroWeek{
			WeekStart:      w.WeekStart,
			Seq:            w.Seq,
			Phase:          w.Phase,
			Mesocycle:      w.Mesocycle,
			LoadLevel:      w.LoadLevel,
			TargetKm:       w.TargetKm,
			TargetSessions: w.TargetSessions,
			RaceID:         w.RaceID,
			KeySessions:    w.KeySessions,
			Intent:         w.Intent,
			Status:         MacroWeekStatusPlanned,
		}
	}
	return rows
}

// parseWeeklyDistanceCap reads the stride_weekly_distance_cap preference as a
// number of kilometres. An unset or unparseable value yields 0, which
// ValidateMacroPlan reads as "no cap configured" — the cap is the athlete's own
// free-text setting, so a typo in it must not block a whole block from
// generating.
func parseWeeklyDistanceCap(raw string) float64 {
	if raw == "" {
		return 0
	}
	km, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		log.Printf("stride: ignoring unparseable stride_weekly_distance_cap %q: %v", raw, err)
		return 0
	}
	if km < 0 {
		return 0
	}
	return km
}
