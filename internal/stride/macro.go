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

// Macro plan phases. A macro week's phase drives what the weekly generator is
// allowed to prescribe for that week.
const (
	MacroPhaseBase     = "base"
	MacroPhaseBuild    = "build"
	MacroPhasePeak     = "peak"
	MacroPhaseTaper    = "taper"
	MacroPhaseRace     = "race"
	MacroPhaseRecovery = "recovery"
)

// Macro week load levels. Independent of phase: a build-phase week can still
// be a deload.
const (
	LoadLevelDeload = "deload"
	LoadLevelNormal = "normal"
	LoadLevelBuild  = "build"
	LoadLevelPeak   = "peak"
	LoadLevelTaper  = "taper"
)

// Macro plan statuses. A plan stays 'active' until a newer block replaces it,
// at which point it becomes 'superseded'. Staleness (a race added, changed or
// removed inside the horizon) is tracked separately in StaleReason so the UI
// can show a banner without the plan losing its active role.
const (
	MacroPlanStatusActive     = "active"
	MacroPlanStatusSuperseded = "superseded"
)

// Macro week statuses. 'planned' until the weekly job turns it into a
// stride_plans row ('materialised'), or the week passes unused ('skipped').
const (
	MacroWeekStatusPlanned      = "planned"
	MacroWeekStatusMaterialised = "materialised"
	MacroWeekStatusSkipped      = "skipped"
)

// How a macro plan came to exist: the Monday cron ('scheduled'), the user
// pressing Regenerate ('manual'), or the horizon running out and a fresh
// 26-week block being appended ('extension').
const (
	MacroGeneratedByScheduled = "scheduled"
	MacroGeneratedByManual    = "manual"
	MacroGeneratedByExtension = "extension"
)

// Where a goal revision came from: the block's first goal ('initial'), the
// weekly job auto-applying drift ('weekly'), or the user editing it ('manual').
const (
	GoalRevisionSourceInitial = "initial"
	GoalRevisionSourceWeekly  = "weekly"
	GoalRevisionSourceManual  = "manual"
)

// Stale reasons recorded on a macro plan. Empty means not stale.
const (
	// MacroStaleRacesChanged marks a plan whose horizon no longer matches the
	// race calendar (a race was added, edited or deleted inside it). The plan
	// is never auto-regenerated — the user gets a banner and a Regenerate button.
	MacroStaleRacesChanged = "races_changed"
)

// MacroGoal is the block's objective. Improving half-marathon performance is
// always the main priority, so TargetHMTimeS is set even for blocks with no
// half marathon on the calendar — there it is judged against the race
// prediction model instead of an actual race result.
type MacroGoal struct {
	PrimaryFocus  string `json:"primary_focus"`
	Statement     string `json:"statement"`
	TargetHMTimeS int    `json:"target_hm_time_s"`
	Benchmark     string `json:"benchmark"`
	Rationale     string `json:"rationale"`
	// AnchorRaceID points at the A-priority race the block is built around,
	// or nil for a pure development block with no explicit end test.
	AnchorRaceID *int64 `json:"anchor_race_id"`
}

// Mesocycle is one named segment of the block's periodisation (for example a
// four-week build). Weeks is the segment length; StartWeek is its first Monday.
type Mesocycle struct {
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	StartWeek string `json:"start_week"`
	Weeks     int    `json:"weeks"`
	Focus     string `json:"focus"`
	RaceID    *int64 `json:"race_id"`
}

// KeySession is one session the macro week must contain, optionally pinned to
// a library workout (stride_workouts).
type KeySession struct {
	Type      string `json:"type"`
	Focus     string `json:"focus"`
	LibraryID *int64 `json:"library_id"`
}

// MacroPlan is a long-horizon training block: a goal, a periodisation, and one
// MacroWeek per week between StartWeek and EndWeek (both Mondays).
type MacroPlan struct {
	ID             int64       `json:"id"`
	UserID         int64       `json:"user_id"`
	StartWeek      string      `json:"start_week"`    // YYYY-MM-DD, Monday
	EndWeek        string      `json:"end_week"`      // YYYY-MM-DD, Monday of the last week
	Status         string      `json:"status"`        // active | superseded
	StaleReason    string      `json:"stale_reason"`  // '' when fresh, e.g. races_changed
	Goal           MacroGoal   `json:"goal"`          // encrypted at rest (goal_json)
	Periodisation  []Mesocycle `json:"periodisation"` // encrypted at rest (periodisation_json)
	Prompt         string      `json:"-"`             // encrypted at rest
	Response       string      `json:"-"`             // encrypted at rest
	Model          string      `json:"model"`
	GeneratedBy    string      `json:"generated_by"` // scheduled | manual | extension
	PreviousPlanID *int64      `json:"previous_plan_id"`
	CreatedAt      string      `json:"created_at"`      // RFC3339, as stored
	Weeks          []MacroWeek `json:"weeks,omitempty"` // populated by GetActiveMacroPlan
}

// MacroWeek is one week of a macro plan — the contract the weekly 7-day
// generator has to honour when it materialises that week.
type MacroWeek struct {
	ID             int64        `json:"id"`
	MacroPlanID    int64        `json:"macro_plan_id"`
	UserID         int64        `json:"user_id"`
	WeekStart      string       `json:"week_start"` // YYYY-MM-DD, Monday
	Seq            int          `json:"seq"`        // 1-based position within the block
	Phase          string       `json:"phase"`
	Mesocycle      string       `json:"mesocycle"`
	LoadLevel      string       `json:"load_level"`
	TargetKm       float64      `json:"target_km"`
	TargetSessions int          `json:"target_sessions"`
	RaceID         *int64       `json:"race_id"`
	KeySessions    []KeySession `json:"key_sessions"` // encrypted at rest (key_sessions_json)
	Intent         string       `json:"intent"`       // encrypted at rest
	Status         string       `json:"status"`       // planned | materialised | skipped
}

// GoalRevision is one append-only entry in a macro plan's goal history.
type GoalRevision struct {
	ID          int64     `json:"id"`
	MacroPlanID int64     `json:"macro_plan_id"`
	UserID      int64     `json:"user_id"`
	WeekStart   string    `json:"week_start"`
	Goal        MacroGoal `json:"goal"`       // encrypted at rest (goal_json)
	Reason      string    `json:"reason"`     // encrypted at rest
	Source      string    `json:"source"`     // initial | weekly | manual
	CreatedAt   string    `json:"created_at"` // RFC3339, as stored
}

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
// week (any date is accepted; it is snapped through NormaliseMacroStartWeek).
//
// The flow is: assemble the athlete inputs (buildMacroInputs), pair them with
// macroInstructions, call Claude through the runPromptFunc seam under
// macroClaudeTimeout, decode the answer into MacroPlanResponse, check it with
// ValidateMacroPlan against the same horizon, cap and race calendar the prompt
// was built from, and only then write it. Validation happens before the write
// on purpose: CreateMacroPlan's transaction covers the plan row, its weeks and
// the block's 'initial' goal revision, and a rejected plan must never reach it.
//
// mode decides both what the prompt is seeded with and what the block is
// recorded as. Extension mode continues the block that is running out — its
// goal, periodisation and closing weeks go into the prompt so the new block
// picks up where the old one ends instead of restarting from base — and fails
// with ErrNoPreviousMacroPlan when there is nothing to continue. Scheduled and
// manual mode replace whatever active block already covers the start week, if
// any. In every mode the plan the new block follows is recorded as
// PreviousPlanID, which is what retires it in the same transaction that
// inserts the new one.
//
// On success the returned plan carries its assigned ID, created_at and weeks.
func GenerateMacroPlan(ctx context.Context, db *sql.DB, userID int64, startWeek time.Time, mode MacroMode) (*MacroPlan, error) {
	if !mode.valid() {
		return nil, fmt.Errorf("invalid macro mode %q", mode)
	}

	// Every entry point computes its start from a clock, a previous block's end
	// week or user input, so snap it to a Monday rather than letting
	// buildMacroInputs reject it.
	start, err := NormaliseMacroStartWeek(startWeek.Format(dateLayout))
	if err != nil {
		return nil, fmt.Errorf("normalise start week: %w", err)
	}
	startDate, err := parseMondayWeek(start)
	if err != nil {
		return nil, fmt.Errorf("parse start week: %w", err)
	}
	endWeek := startDate.AddDate(0, 0, 7*(MacroBlockWeeks-1)).Format(dateLayout)

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
	if claudeCfg.Model == "" {
		claudeCfg.Model = strideDefaultModel
	}

	// Resolved before the Claude call so an extension with nothing to continue
	// fails in milliseconds instead of after a 26-week generation.
	previous, err := macroPreviousPlan(ctx, db, userID, start, mode)
	if err != nil {
		return nil, err
	}

	inputs, err := buildMacroInputs(ctx, db, userID, start, mode)
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

	// The calendar the answer is checked against is the same one the prompt
	// rendered, read again rather than threaded through so the two cannot
	// drift apart in the caller.
	races, err := ListRaces(db, userID)
	if err != nil {
		return nil, fmt.Errorf("list races: %w", err)
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
		EndWeek:       endWeek,
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
// Code fences are stripped the same way parsePlanResponse strips them for the
// weekly plan: the contract says "no code fences", but a model that adds them
// anyway is answering correctly in the wrong wrapper and is not worth a retry.
func parseMacroPlanResponse(response string) (*MacroPlanResponse, error) {
	trimmed := strings.TrimSpace(response)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 {
			trimmed = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var plan MacroPlanResponse
	if err := json.Unmarshal([]byte(trimmed), &plan); err != nil {
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
