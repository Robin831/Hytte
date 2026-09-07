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

// macroGenerateAttempts is how many times one GenerateMacroPlan call may ask
// the model for a block before giving up: the first ask plus two corrective
// retries. ValidateMacroPlan writes its problems to be handed back verbatim,
// and in practice a rejection is the model rounding a ramp to a tidy number a
// hair over the +10% limit (36 km -> 40 km where 39.6 is the ceiling) — a
// retry that is shown the problem almost always lands. Parse failures take
// the same path: a truncated or oddly-fenced answer is as retryable as an
// invalid one. Each attempt gets its own macroClaudeTimeout.
const macroGenerateAttempts = 3

// macroTransportRetries is how many extra calls a transport-level CLI failure
// — killed by a signal, or exited without a word on stderr — may buy, on top of
// macroGenerateAttempts. One is enough for the case it was written for: the
// Claude CLI auto-updates, its supervisor restarts and SIGKILLs the call in
// flight, and the very next call lands on the new binary. More than one would
// start to look like retrying a dead API, which is what the corrective budget
// must not be spent on either.
const macroTransportRetries = 1

// macroTransportRetryDelay is the pause before that retry, giving a supervisor
// restart time to finish rather than racing it. A var, not a const, so tests
// need not sit through it.
var macroTransportRetryDelay = 5 * time.Second

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
// was built from, and only then write it. An answer that fails to parse or
// validate is asked again, up to macroGenerateAttempts calls in total, with
// the rejected answer and the validator's problems appended to the prompt
// (macroRetryPrompt); the prompt stored with the block is the one the
// accepted answer was actually given. Validation happens before the write
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
// manual mode regenerate over whatever the athlete has, replacing the active
// block covering the start week if there is one. In every mode the plan the new
// block follows is recorded as PreviousPlanID, and every active block the new
// 26-week horizon overlaps is retired in the same transaction that inserts it —
// including one that starts after the new block does, which a scheduled
// extension leaves behind and which is nobody's lineage parent. That
// transaction re-checks the athlete's active blocks itself, so a second
// generation racing this one is rejected with ErrOverlappingMacroPlan rather
// than leaving two active blocks behind.
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

	endWeek := macroEndWeek(startDate)

	// Resolved before the Claude call so an extension with nothing to continue
	// fails in milliseconds instead of after a 26-week generation. It is only
	// what the new block *replaces* that is decided here — CreateMacroPlan
	// re-checks which blocks are actually active when it writes.
	lineage, err := macroBlockLineage(ctx, db, userID, start, endWeek, mode)
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

	// Call, parse and validate as one unit, retried with the rejection fed
	// back (macroRetryPrompt) — a bad answer costs one more call instead of
	// failing the whole generation.
	//
	// A transport-level CLI death gets one extra call from macroTransportRetries,
	// a budget deliberately kept apart from macroGenerateAttempts: a killed
	// process never produced an answer to correct, so spending a corrective
	// attempt on it would take one away from the rejection it was reserved for.
	// That budget is spent once per generation, not once per attempt — a CLI
	// killed on the first call leaves nothing for a second death during a later
	// corrective attempt, which is deliberate: two deaths in one generation
	// look less like an upgrade landing mid-call than like a CLI that cannot
	// stay up.
	// The case this exists for is the CLI auto-updating mid-call — its
	// supervisor restarts on a binary change and SIGKILLs the live worker,
	// which otherwise costs the athlete a whole 26-week regeneration. A genuine
	// API failure (the CLI exiting with a reason on stderr) still fails fast
	// rather than looping a several-minute call against an upstream that is
	// down.
	attemptPrompt := prompt
	var (
		response         string
		parsed           *MacroPlanResponse
		transportRetries = macroTransportRetries
	)
	for attempt := 1; ; {
		callCtx, cancel := context.WithTimeout(ctx, macroClaudeTimeout)
		response, err = runPromptFunc(callCtx, claudeCfg, attemptPrompt)
		cancel()
		if err != nil {
			if !training.IsCLITransportError(err) || transportRetries == 0 {
				return nil, fmt.Errorf("Claude prompt: %w", err)
			}
			transportRetries--
			log.Printf("stride: macro block attempt %d/%d for user %d hit a Claude CLI transport error, retrying once: %v",
				attempt, macroGenerateAttempts, userID, err)
			// Pause before asking again so a supervisor restart has finished by
			// the time the retry starts, and bail rather than sleep if the
			// caller has already given up.
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("Claude prompt: %w", err)
			case <-time.After(macroTransportRetryDelay):
			}
			// The prompt is unchanged: there is no answer to correct, and the
			// attempt counter stays where it is.
			continue
		}

		parsed, err = parseMacroPlanResponse(response)
		if err == nil {
			err = ValidateMacroPlan(parsed, MacroValidationContext{
				StartWeek:         start,
				WeeklyDistanceCap: parseWeeklyDistanceCap(prefs["stride_weekly_distance_cap"]),
				Races:             races,
			})
		}
		if err == nil {
			break
		}
		if attempt == macroGenerateAttempts {
			return nil, fmt.Errorf("giving up after %d attempts: %w", macroGenerateAttempts, err)
		}
		log.Printf("stride: macro block attempt %d/%d for user %d rejected, retrying with feedback: %v",
			attempt, macroGenerateAttempts, userID, err)
		attemptPrompt = macroRetryPrompt(prompt, response, err)
		attempt++
	}

	plan := &MacroPlan{
		UserID:         userID,
		StartWeek:      start,
		EndWeek:        endWeek,
		Status:         MacroPlanStatusActive,
		Goal:           parsed.Goal,
		Periodisation:  parsed.Mesocycles,
		Prompt:         attemptPrompt,
		Response:       response,
		Model:          claudeCfg.Model,
		GeneratedBy:    string(mode),
		PreviousPlanID: lineage.previousPlanID,
	}

	reason := fmt.Sprintf("Initial goal for the %d-week block starting %s (%s).",
		MacroBlockWeeks, start, mode)
	if err := CreateMacroPlanReplacing(ctx, db, plan, macroWeeksFromResponse(parsed.Weeks), reason,
		lineage.supersede); err != nil {
		return nil, fmt.Errorf("persist macro plan: %w", err)
	}
	return plan, nil
}

// macroRetryPrompt rebuilds the generation prompt after a rejected attempt:
// the original ask, the answer the model gave, and why it was rejected, with
// an instruction to return the whole corrected plan. The base prompt is
// repeated rather than referenced because every attempt is a fresh stateless
// call — the model has no memory of the ask it is being corrected on.
func macroRetryPrompt(base, response string, rejection error) string {
	return fmt.Sprintf(`%s

## Correction required

A previous attempt at this task produced the answer below, and it was rejected.

Previous answer:

%s

Rejected because:

%s

Return the complete corrected plan in exactly the same JSON format, fixing every problem listed above without introducing new ones. Adjust neighbouring weeks where that is what it takes to satisfy the rules.`, base, response, rejection)
}

// macroLineage is what a new block displaces: previousPlanID is the block it
// descends from (nil for a first block), and supersede lists every active block
// that must be retired for the athlete to be left with one plan per week —
// previousPlanID included.
type macroLineage struct {
	previousPlanID *int64
	supersede      []int64
}

// macroBlockLineage decides what the new block replaces. Whatever the mode, the
// blocks that must be retired are all the active ones overlapping the new
// horizon [startWeek, endWeek] — the same window CreateMacroPlan's overlap
// check spans, so a block it would reject is always one this resolved and
// demoted. Resolving over a narrower window (say, only the block covering
// startWeek) leaves a block that starts later inside the horizon unnamable, and
// every generation then burns its Claude call only to fail the check.
//
// Which of them is the *lineage parent* still depends on the mode: an extension
// continues the block whose horizon is running out — usually one that ends
// before the new block starts and so does not overlap it at all — while a
// scheduled or manual block descends from the active block covering its start
// week, which is the earliest of the overlapping ones.
func macroBlockLineage(ctx context.Context, db *sql.DB, userID int64, startWeek, endWeek string, mode MacroMode) (macroLineage, error) {
	spans, err := listActiveMacroPlanSpans(ctx, db, userID, startWeek, endWeek)
	if err != nil {
		return macroLineage{}, fmt.Errorf("load overlapping macro plans: %w", err)
	}
	lineage := macroLineage{supersede: make([]int64, 0, len(spans))}
	for _, span := range spans {
		lineage.supersede = append(lineage.supersede, span.ID)
	}

	if mode == MacroModeExtension {
		prev, err := loadPreviousMacroPlan(ctx, db, userID, startWeek)
		if err != nil {
			return macroLineage{}, fmt.Errorf("load previous macro plan: %w", err)
		}
		if prev == nil {
			return macroLineage{}, ErrNoPreviousMacroPlan
		}
		lineage.previousPlanID = &prev.ID
		return lineage, nil
	}
	if len(spans) > 0 {
		lineage.previousPlanID = &spans[0].ID
	}
	return lineage, nil
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
