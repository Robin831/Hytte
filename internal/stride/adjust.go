package stride

// The AdjustWeek entrypoint, and the parsing and persistence for one
// materialised training week.
//
// The AdjustWeek prompt (adjust_prompt.go) answers with an envelope: the week
// itself plus what the coach did to it and what it wants to propose beyond its
// own authority. The legacy weekly generator (generate.go) answers with a bare
// array of the same day objects. Both land here, and both are written by the
// same transaction, so "the week, its provenance and its consequences" is one
// all-or-nothing write regardless of which prompt produced it.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// adjustWeekFunc is the seam tests replace so the routing decisions of the two
// callers — which week RunWeekly and GeneratePlanHandler each ask for — can be
// asserted without a Claude call. Production always runs AdjustWeek.
//
// Package-level and mutable, the same shape as runPromptFunc, so a test that
// swaps it mutates state shared with every other test in the package: the ones
// that do must not call t.Parallel(). If a third seam of this shape is ever
// needed, gather them into a config value passed into the callers instead of
// widening that race surface again.
var adjustWeekFunc = AdjustWeek

// AdjustWeek materialises the training week starting at weekStart (a Monday):
// it adjusts the athlete's macro block's plan for that week against what has
// actually happened since, and stores the result as that week's stride plan.
//
// It is the single entrypoint for weekly plan generation. The Monday run
// (RunWeekly) and the manual trigger (GeneratePlanHandler) both come through
// here, so the two can never disagree about which prompt an athlete's week was
// built from.
//
// When no active macro block covers weekStart it falls back to the legacy
// whole-week generator so the athlete still gets a week. That is a routing
// decision, not an error path: macro generation may have failed on Monday, or
// Stride may have been enabled only moments ago, and a week the athlete can
// train is worth more than a block-aware one they do not get. Every other
// failure — a macro lookup that broke, a model error, an unreadable answer, a
// failed write — is returned rather than swallowed, because falling back there
// would hide a real fault behind a silently worse plan.
//
// Like the legacy generator it sets no deadline of its own: the caller's
// context is the whole budget for the Claude call, which is why RunWeekly wraps
// weeklyPlanTimeout around it.
//
// Returns nil without writing anything when the athlete has not enabled Stride.
func AdjustWeek(ctx context.Context, db *sql.DB, userID int64, weekStart string) error {
	// A non-Monday week start matches no macro week, no stride_plans row and no
	// weekly summary, so it would degrade into a silently block-less plan for a
	// week nothing else in Stride recognises. Reject it here instead.
	if _, err := parseMondayWeek(weekStart); err != nil {
		return fmt.Errorf("adjust week: %w", err)
	}
	weekEnd := shiftDays(weekStart, 6)

	prefs, claudeCfg, enabled, err := resolveStrideConfig(db, userID)
	if err != nil || !enabled {
		return err
	}

	built, err := buildAdjustPrompt(ctx, db, userID, weekStart, weekEnd)
	if errors.Is(err, ErrNoMacroWeek) {
		log.Printf("stride: user %d week %s: no macro week to adjust — planning the week from scratch", userID, weekStart)
		return generatePlanLegacy(ctx, db, userID, prefs, claudeCfg, weekStart, weekEnd)
	}
	if err != nil {
		return fmt.Errorf("build adjust prompt: %w", err)
	}

	response, err := runPromptFunc(ctx, claudeCfg, built.Prompt)
	if err != nil {
		return fmt.Errorf("Claude prompt: %w", err)
	}

	// parseAdjustEnvelope, not parsePlanEnvelope: this prompt asked for the
	// {week, adjustment} envelope, so a bare day array is a shape slip and must
	// fail rather than be written as "the coach changed nothing".
	env, err := parseAdjustEnvelope(response, weekStart, weekEnd)
	if err != nil {
		return fmt.Errorf("parse adjust response: %w", err)
	}

	// The macro week is what the plan row links back to and what flips to
	// 'materialised' in the same transaction as the write.
	macroWeekID := built.MacroWeek.ID
	return saveWeeklyPlan(ctx, db, weeklyPlanWrite{
		UserID:    userID,
		WeekStart: weekStart,
		WeekEnd:   weekEnd,
		// Denormalised from the macro week, never from the coach's answer: the
		// phase is fixed by the block and the prompt says so.
		Phase:       built.MacroWeek.Phase,
		Envelope:    env,
		Prompt:      built.Prompt,
		Response:    response,
		Model:       claudeCfg.Model,
		Notes:       built.Notes,
		MacroWeekID: &macroWeekID,
		MacroPlanID: built.MacroPlan.ID,
		CurrentGoal: built.CurrentGoal,
	})
}

// GoalUpdate is the coach's proposed change to the block's target half-marathon
// time. The _s suffix on the wire key is deliberate and matches
// adjustOutputContract, MacroGoal.TargetHMTimeS and the stored
// target_hm_time_s column: the unit is part of the name everywhere, so no
// reader (model included) has to guess whether the number is seconds or
// minutes.
type GoalUpdate struct {
	TargetHMTimeS int    `json:"target_hm_time_s"`
	Reason        string `json:"reason"`
}

// PlanAdjustment is the "adjustment" half of the envelope: whether the returned
// week departs from the macro week's spec, the prose saying how and why, and an
// optional goal proposal.
type PlanAdjustment struct {
	Deviates   bool        `json:"deviates"`
	Summary    string      `json:"summary"`
	GoalUpdate *GoalUpdate `json:"goal_update"`
}

// PlanEnvelope is the whole object the AdjustWeek prompt returns. Its json tags
// mirror adjustOutputContract key for key — change the two together.
//
// The legacy weekly response has no envelope around it, so parsePlanEnvelope
// wraps a bare array in one with a zero PlanAdjustment. A zero adjustment is
// indistinguishable from "the coach followed the macro week exactly and
// proposed nothing", which is the right reading of a prompt that never had a
// macro week to depart from.
type PlanEnvelope struct {
	Week       []DayPlan      `json:"week"`
	Adjustment PlanAdjustment `json:"adjustment"`
}

// parsePlanEnvelope unwraps a plan response into a PlanEnvelope, accepting
// either the envelope object or the legacy bare array of 7 day objects. The
// week is validated against weekStart..weekEnd exactly as parsePlanResponse
// validates the legacy shape — the same 7 dates, no duplicates, a session on
// every non-rest day.
//
// This is the reader for callers that asked for the BARE ARRAY contract, where
// an envelope is a bonus rather than the promise. Only the summary of that
// bonus is kept: the legacy caller has no macro block, so any goal_update it
// carries is rejected by the clamp (see weeklyPlanWrite.MacroPlanID) and goal
// proposals deliberately do not flow through this path. Callers that asked for
// the AdjustWeek envelope must use parseAdjustEnvelope, which refuses to read a
// bare array as "the coach changed nothing".
//
// The shape is chosen from the first significant byte rather than by trying
// both blindly, so a malformed envelope reports why the envelope failed instead
// of the useless "cannot unmarshal object into []DayPlan" the array attempt
// would give. That first byte decides the shape completely: JSON allows only
// whitespace before a value, and the whitespace is gone by then, so a body
// starting with anything but '{' or '[' cannot parse as either shape and is
// rejected here rather than run through two attempts that must both fail.
func parsePlanEnvelope(response, weekStart, weekEnd string) (PlanEnvelope, error) {
	// Trim after unfencing as well: the dispatch below reads body[0] as the
	// first SIGNIFICANT byte, so a leading newline would misroute a perfectly
	// good envelope (and a blank-but-non-empty body would slip past the
	// emptiness guard into unmarshal noise).
	body := strings.TrimSpace(stripCodeFence(response))
	if body == "" {
		return PlanEnvelope{}, errors.New("empty plan response")
	}

	switch body[0] {
	case '{':
		return parseEnvelopeObject(body, weekStart, weekEnd)
	case '[':
		return parseLegacyPlanArray(body, weekStart, weekEnd)
	default:
		return PlanEnvelope{}, fmt.Errorf(
			"plan response is neither an envelope object nor a bare day array; it starts with %q",
			truncateForError(body))
	}
}

// truncateForError returns the head of a bad response, short enough to log and
// long enough to recognise. AI prose, so never logged in full.
func truncateForError(body string) string {
	const max = 40
	runes := []rune(body)
	if len(runes) <= max {
		return body
	}
	return string(runes[:max]) + "..."
}

// parseAdjustEnvelope reads a response to the AdjustWeek contract, which
// promised the {week, adjustment} envelope. Unlike parsePlanEnvelope it will
// not fall back to the bare array: under this contract a naked 7-day array is a
// shape slip, and reading it as an envelope with a zero adjustment would write
// the week with deviates=false, an empty summary and no goal_update — silently
// losing the coach's stated rationale and telling next week's prompt the week
// was spec-conforming. A hard error re-runs the prompt instead.
//
// AdjustWeek is its only caller, and must stay so: routing the adjust
// response through parsePlanEnvelope instead would leave the bare-array
// refusal above enforced nowhere.
func parseAdjustEnvelope(response, weekStart, weekEnd string) (PlanEnvelope, error) {
	body := strings.TrimSpace(stripCodeFence(response))
	if body == "" {
		return PlanEnvelope{}, errors.New("empty plan response")
	}
	if body[0] == '[' {
		return PlanEnvelope{}, errors.New(
			"adjust response is a bare day array, not the {week, adjustment} envelope the prompt asked for")
	}
	return parseEnvelopeObject(body, weekStart, weekEnd)
}

// parseEnvelopeObject reads the {week, adjustment} shape.
func parseEnvelopeObject(body, weekStart, weekEnd string) (PlanEnvelope, error) {
	var env PlanEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return PlanEnvelope{}, fmt.Errorf("unmarshal plan envelope JSON: %w", err)
	}
	if err := validatePlanDays(env.Week, weekStart, weekEnd); err != nil {
		return PlanEnvelope{}, err
	}
	env.Adjustment.Summary = strings.TrimSpace(env.Adjustment.Summary)
	return env, nil
}

// parseLegacyPlanArray reads the bare [7 DayPlan] shape the weekly generator's
// contract asks for and wraps it in an envelope with a zero adjustment.
func parseLegacyPlanArray(body, weekStart, weekEnd string) (PlanEnvelope, error) {
	var week []DayPlan
	if err := json.Unmarshal([]byte(body), &week); err != nil {
		return PlanEnvelope{}, fmt.Errorf("unmarshal plan JSON: %w", err)
	}
	if err := validatePlanDays(week, weekStart, weekEnd); err != nil {
		return PlanEnvelope{}, err
	}
	return PlanEnvelope{Week: week}, nil
}

// goalDriftTolerance bounds how far the weekly job may move the block's target
// half-marathon time on its own authority: +/-3% of the current target. A
// larger proposal is a change of goal rather than a drift correction, so it is
// the athlete's call — it is logged and survives in adjustment_summary, and
// nowhere else.
const goalDriftTolerance = 0.03

// goalUpdateDecision is the outcome of clamping a proposed goal_update.
type goalUpdateDecision struct {
	Accepted bool
	// Rejection says in one clause why the proposal was not applied, for the
	// log line. Empty when Accepted.
	Rejection string
}

// evaluateGoalUpdate decides whether a proposed goal_update may be applied
// automatically. It is accepted only when it carries a reason AND the proposed
// target is within goalDriftTolerance of currentTargetS. Everything else — a
// missing reason, a nonsensical time, a block with no target to measure
// against, or drift beyond the tolerance — is a rejection rather than an error:
// a bad proposal must never cost the athlete the week that carried it.
func evaluateGoalUpdate(currentTargetS int, update *GoalUpdate) goalUpdateDecision {
	switch {
	case update == nil:
		return goalUpdateDecision{Rejection: "no goal update proposed"}
	case strings.TrimSpace(update.Reason) == "":
		// adjustmentRules tells the coach a proposal without a reason is
		// discarded; this is where that promise is kept.
		return goalUpdateDecision{Rejection: "the proposal carries no reason"}
	case update.TargetHMTimeS <= 0:
		return goalUpdateDecision{Rejection: fmt.Sprintf(
			"the proposed target %ds is not a positive time", update.TargetHMTimeS)}
	case currentTargetS <= 0:
		return goalUpdateDecision{Rejection: "the block has no current target half-marathon time to measure the drift against"}
	}

	drift := math.Abs(float64(update.TargetHMTimeS-currentTargetS)) / float64(currentTargetS)
	if drift > goalDriftTolerance {
		return goalUpdateDecision{Rejection: fmt.Sprintf(
			"the proposed target %s is %.1f%% from the current %s, beyond the %.0f%% the weekly job may apply on its own",
			formatRaceTime(update.TargetHMTimeS), drift*100,
			formatRaceTime(currentTargetS), goalDriftTolerance*100)}
	}
	return goalUpdateDecision{Accepted: true}
}

// weeklyPlanWrite is everything one materialisation of a training week writes.
// The block-aware fields are all optional so the legacy weekly generator, which
// plans without a macro block, uses the same call.
type weeklyPlanWrite struct {
	UserID    int64
	WeekStart string // YYYY-MM-DD, Monday
	WeekEnd   string // YYYY-MM-DD, the following Sunday

	// Phase is the macro week's phase, denormalised onto the weekly row so the
	// UI and the next week's prompt can read it without joining the block. It
	// is a pass-through: this write never derives or validates it, and the
	// legacy path leaves it "".
	Phase string

	Envelope PlanEnvelope

	Prompt   string // plaintext; encrypted here
	Response string // plaintext; encrypted here
	Model    string

	// Notes are the unconsumed notes the prompt was rendered from. They are
	// consumed in the same transaction, so a failed plan write never silently
	// eats a note the athlete wrote.
	Notes []Note

	// MacroWeekID links the weekly row back to the macro week it materialises,
	// and names the row whose status flips to 'materialised'. nil when the week
	// was planned without a block.
	MacroWeekID *int64

	// MacroPlanID and CurrentGoal are the block an accepted goal_update is
	// recorded against, and the goal the +/-3% clamp measures the proposal
	// from. Left zero on the legacy path, which makes every proposal a
	// rejection — correct, since there is no goal history to append to.
	MacroPlanID int64
	CurrentGoal MacroGoal
}

// saveWeeklyPlan persists one materialised training week.
//
// Everything the week implies happens in ONE transaction: the stride_plans
// upsert (keyed on user_id + week_start), the macro week's flip to
// 'materialised', the consumption of the notes the prompt was built from, and
// an accepted goal revision. A failure anywhere rolls the lot back, so the
// athlete never ends up with a macro week marked materialised by a plan that
// was not stored, or a goal revision justified by a week that does not exist.
//
// A proposed goal_update is clamped first (see evaluateGoalUpdate): within
// +/-3% and carrying a reason it becomes a stride_goal_revisions row with
// source 'weekly'; anything larger is logged and left to survive in
// adjustment_summary alone.
func saveWeeklyPlan(ctx context.Context, db *sql.DB, w weeklyPlanWrite) error {
	// Re-marshal the validated week to canonical JSON for storage. Only the
	// days are stored in plan_json — the adjustment lives in its own column so
	// buildRecentAdjustments can read it without decoding every plan.
	planBytes, err := json.Marshal(w.Envelope.Week)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}

	encPrompt, err := encryption.EncryptField(w.Prompt)
	if err != nil {
		return fmt.Errorf("encrypt prompt: %w", err)
	}
	encResponse, err := encryption.EncryptField(w.Response)
	if err != nil {
		return fmt.Errorf("encrypt response: %w", err)
	}
	// AI-authored prose, encrypted at rest like the rest of Stride's prose
	// columns. An empty summary stays empty — EncryptField passes "" through.
	encSummary, err := encryption.EncryptField(w.Envelope.Adjustment.Summary)
	if err != nil {
		return fmt.Errorf("encrypt adjustment summary: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Clamp the goal proposal before opening the transaction: the decision is
	// pure, and a rejection is worth logging whether or not the write lands.
	revision := plannedGoalRevision(w, now)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Upsert into stride_plans (unique on user_id + week_start). The
	// provenance columns are set from excluded like the rest: the row always
	// describes the generation that most recently produced it, so re-planning a
	// week without a block correctly unlinks it from the macro week it no
	// longer came from. chat_session_id and chat_session_msg_floor are
	// deliberately absent — the coaching conversation survives a re-plan.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO stride_plans
			(user_id, week_start, week_end, phase, plan_json, prompt, response, model,
			 macro_week_id, adjustment_summary, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, week_start) DO UPDATE SET
			week_end           = excluded.week_end,
			phase              = excluded.phase,
			plan_json          = excluded.plan_json,
			prompt             = excluded.prompt,
			response           = excluded.response,
			model              = excluded.model,
			macro_week_id      = excluded.macro_week_id,
			adjustment_summary = excluded.adjustment_summary,
			created_at         = excluded.created_at
	`, w.UserID, w.WeekStart, w.WeekEnd, w.Phase, string(planBytes), encPrompt, encResponse,
		w.Model, w.MacroWeekID, encSummary, now)
	if err != nil {
		return fmt.Errorf("insert stride plan: %w", err)
	}

	// The macro week has now been turned into an actual 7-day plan.
	if w.MacroWeekID != nil {
		if err := markMacroWeekStatusTx(ctx, tx, *w.MacroWeekID, w.UserID, MacroWeekStatusMaterialised); err != nil {
			return fmt.Errorf("mark macro week %d materialised: %w", *w.MacroWeekID, err)
		}
	}

	if len(w.Notes) > 0 {
		noteIDs := make([]int64, len(w.Notes))
		for i, n := range w.Notes {
			noteIDs[i] = n.ID
		}
		if err := MarkNotesConsumed(ctx, tx, w.UserID, noteIDs, "weekly"); err != nil {
			return fmt.Errorf("mark notes consumed: %w", err)
		}
	}

	if revision != nil {
		// Ownership is not re-checked the way AddGoalRevision checks it: the
		// block and its goal came from this user's own active plan, and the
		// only field moved is the target time, so there is no id here the
		// athlete did not already own.
		if _, _, err := insertGoalRevisionTx(ctx, tx, revision); err != nil {
			return fmt.Errorf("record weekly goal revision: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit plan tx: %w", err)
	}

	if revision != nil {
		log.Printf("stride: user %d week %s: applied goal update to %s (drift within %.0f%%)",
			w.UserID, w.WeekStart, formatRaceTime(revision.Goal.TargetHMTimeS), goalDriftTolerance*100)
	}

	// Record which library workouts the plan drew on — after commit, so a
	// failed plan never inflates usage. Best-effort telemetry; a re-generate
	// of the same week counts again, which slightly overstates usage but
	// never understates recency (last_used_at is the plan's week).
	var usedIDs []int64
	for _, day := range w.Envelope.Week {
		if day.Session != nil && day.Session.LibraryID > 0 {
			usedIDs = append(usedIDs, day.Session.LibraryID)
		}
	}
	if len(usedIDs) > 0 {
		RecordLibraryUsage(ctx, db, w.UserID, usedIDs, w.WeekStart)
	}

	return nil
}

// plannedGoalRevision applies the +/-3% clamp to the envelope's goal_update and
// returns the revision to insert, or nil when nothing is to be applied. Every
// nil return that had a proposal behind it is logged, because a rejected
// proposal leaves no trace in the database at all — the log and the week's
// adjustment_summary are the only record that the coach asked.
func plannedGoalRevision(w weeklyPlanWrite, now string) *GoalRevision {
	update := w.Envelope.Adjustment.GoalUpdate
	if update == nil {
		return nil
	}

	decision := evaluateGoalUpdate(w.CurrentGoal.TargetHMTimeS, update)
	if !decision.Accepted {
		log.Printf("stride: user %d week %s: goal update not applied — %s; it survives only in the adjustment summary",
			w.UserID, w.WeekStart, decision.Rejection)
		return nil
	}
	if w.MacroPlanID == 0 {
		log.Printf("stride: user %d week %s: goal update is within tolerance but the week has no macro block to record it against; not applied",
			w.UserID, w.WeekStart)
		return nil
	}

	// Only the target time moves. Focus, statement, benchmark, rationale and
	// the anchor race are the block's, and the weekly coach may not rewrite
	// them — it was asked for a time, not for a new goal.
	goal := w.CurrentGoal
	goal.TargetHMTimeS = update.TargetHMTimeS

	return &GoalRevision{
		MacroPlanID: w.MacroPlanID,
		UserID:      w.UserID,
		WeekStart:   w.WeekStart,
		Goal:        goal,
		Reason:      update.Reason,
		Source:      GoalRevisionSourceWeekly,
		CreatedAt:   now,
	}
}
