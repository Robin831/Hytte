package stride

// Parsing and persistence for one materialised training week.
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

// GoalUpdate is the coach's proposed change to the block's target half-marathon
// time. The time is in SECONDS, matching both the "target_hm_time" key in
// adjustOutputContract and MacroGoal.TargetHMTimeS.
type GoalUpdate struct {
	TargetHMTimeS int    `json:"target_hm_time"`
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
// The shape is chosen from the first non-space byte rather than by trying both
// blindly, so a malformed envelope reports why the envelope failed instead of
// the useless "cannot unmarshal object into []DayPlan" the array attempt would
// give. A leading byte that is neither token is genuinely ambiguous (a model
// that emitted a stray prefix, say), and only there are both shapes tried.
func parsePlanEnvelope(response, weekStart, weekEnd string) (PlanEnvelope, error) {
	body := stripCodeFence(response)
	if body == "" {
		return PlanEnvelope{}, errors.New("empty plan response")
	}

	switch body[0] {
	case '{':
		return parseEnvelopeObject(body, weekStart, weekEnd)
	case '[':
		return parseLegacyPlanArray(body, weekStart, weekEnd)
	}

	env, objErr := parseEnvelopeObject(body, weekStart, weekEnd)
	if objErr == nil {
		return env, nil
	}
	env, arrErr := parseLegacyPlanArray(body, weekStart, weekEnd)
	if arrErr == nil {
		return env, nil
	}
	return PlanEnvelope{}, fmt.Errorf(
		"plan response is neither an envelope object (%v) nor a bare day array (%v)", objErr, arrErr)
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
		if err := insertGoalRevisionTx(ctx, tx, revision); err != nil {
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
