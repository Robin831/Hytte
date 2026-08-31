package stride

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// macroRequestTimeout bounds a hand-triggered macro generation end to end. It is
// the budget macroClaudeTimeout gives each of the macroGenerateAttempts Claude
// calls a generation may make, which is all but the whole request — the DB
// reads either side of them are milliseconds. Bounding the request as well as
// the calls is what stops a wedged CLI from holding the handler (and the
// athlete's lock) open for as long as the process lives.
const macroRequestTimeout = macroGenerateAttempts * macroClaudeTimeout

// macroOpenEndedWeek is a week key past any real horizon, used as the upper
// bound when asking for "every active block from this week onwards" rather than
// the blocks overlapping a specific 26-week window.
const macroOpenEndedWeek = "9999-12-31"

// MacroPlanView is what the macro endpoints return: the block itself, its week
// rows, the goal the athlete is currently training towards, and the full goal
// history behind it. All four GET/POST macro endpoints answer in this shape so a
// client parses one payload regardless of how the block was obtained.
//
// The weeks are serialised once, at the top level — buildMacroPlanView clears
// MacroPlan.Weeks (json:"weeks,omitempty") after moving them across, so the
// nested plan object does not repeat all 26 of them.
type MacroPlanView struct {
	Plan  *MacroPlan  `json:"plan"`
	Weeks []MacroWeek `json:"weeks"`
	// CurrentGoalRevision is the newest entry of Revisions, or nil for a block
	// with no goal history at all (only reachable for rows written outside
	// CreateMacroPlan, which always writes an 'initial' revision).
	CurrentGoalRevision *GoalRevision  `json:"current_goal_revision"`
	Revisions           []GoalRevision `json:"revisions"`
	// HasNextBlock reports whether an active block already covers the Monday
	// after this one ends. It is the same coverage check EnsureMacroPlan makes
	// before spending a Claude call on an extension, exposed so the UI can grey
	// out its Extend button instead of letting the athlete queue a third block.
	HasNextBlock bool `json:"has_next_block"`
}

// buildMacroPlanView loads a block's goal history and assembles the response
// shape around it. plan must already carry its weeks — both GetActiveMacroPlan
// and GetMacroPlanByID load them — and is consumed by the view rather than
// copied, so callers must not reuse it afterwards.
func buildMacroPlanView(ctx context.Context, db *sql.DB, plan *MacroPlan) (*MacroPlanView, error) {
	revisions, err := ListGoalRevisions(ctx, db, plan.ID, plan.UserID)
	if err != nil {
		return nil, err
	}

	hasNext, err := hasSuccessorMacroPlan(ctx, db, plan.UserID, plan.EndWeek)
	if err != nil {
		return nil, err
	}

	view := &MacroPlanView{Plan: plan, Weeks: plan.Weeks, Revisions: revisions, HasNextBlock: hasNext}
	if view.Weeks == nil {
		view.Weeks = []MacroWeek{}
	}
	plan.Weeks = nil
	if len(revisions) > 0 {
		// ListGoalRevisions is oldest first, so the goal in force is the last.
		view.CurrentGoalRevision = &revisions[len(revisions)-1]
	}
	return view, nil
}

// hasSuccessorMacroPlan reports whether the athlete has an active block over the
// Monday after endWeek. Coverage rather than an exact start_week match, for the
// same reason EnsureMacroPlan checks it that way: a block that starts earlier
// and runs through that Monday already extends the horizon.
//
// An end_week that will not parse is not an error the read should fail on — the
// answer is simply "no successor known", which at worst offers an Extend the
// generator would reject.
func hasSuccessorMacroPlan(ctx context.Context, db *sql.DB, userID int64, endWeek string) (bool, error) {
	end, err := parseWeekDate(endWeek)
	if err != nil {
		log.Printf("stride: parse end week %q for user %d: %v", endWeek, userID, err)
		return false, nil
	}
	next := end.AddDate(0, 0, 7).Format(dateLayout)

	spans, err := listActiveMacroPlanSpans(ctx, db, userID, next, next)
	if err != nil {
		return false, err
	}
	return len(spans) > 0, nil
}

// GetCurrentMacroPlanHandler returns the active macro block covering the week
// the athlete is training now, or 404 when they have none.
// GET /api/stride/macro/current
func GetCurrentMacroPlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// GetActiveMacroPlan compares week keys as strings against start_week
		// and end_week, both Mondays — so today's date has to be snapped back to
		// this week's Monday or a lookup on a Tuesday would fall past the last
		// week of the block.
		thisMonday, _ := currentWeek()

		plan, err := GetActiveMacroPlan(r.Context(), db, user.ID, thisMonday)
		if err != nil {
			log.Printf("stride: get current macro plan for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get current macro block"})
			return
		}
		if plan == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active macro block"})
			return
		}

		writeMacroPlanView(w, r, db, http.StatusOK, plan)
	}
}

// GetMacroPlanHandler returns one macro block by id, including superseded ones
// so the athlete can look back at a block they regenerated away.
// GET /api/stride/macro/{id}
//
// A block owned by somebody else answers 404, not 403: the store scopes the
// lookup by user, so "not yours" and "does not exist" are the same answer and
// the endpoint never confirms that an id exists.
func GetMacroPlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid macro block ID"})
			return
		}

		plan, err := GetMacroPlanByID(r.Context(), db, id, user.ID)
		if err != nil {
			if errors.Is(err, ErrMacroPlanNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "macro block not found"})
				return
			}
			log.Printf("stride: get macro plan %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get macro block"})
			return
		}

		writeMacroPlanView(w, r, db, http.StatusOK, plan)
	}
}

// GenerateMacroPlanHandler regenerates the athlete's macro block by hand, the
// block behind the Regenerate button a stale plan's banner offers.
// POST /api/stride/macro/generate
//
// The new block starts at the Monday the athlete trains next — the same week
// RunWeekly ensures — so the week in progress keeps the contract it was
// materialised against. Every active block the new 26-week horizon overlaps is
// superseded in the transaction that inserts it (GenerateMacroPlan does the
// demotion), and no stride_plans row is touched at all: weeks already
// materialised into a 7-day plan stay exactly as the athlete has them.
func GenerateMacroPlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		release, ok := TryLockUser(user.ID)
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a generation is already running — try again in a moment"})
			return
		}
		defer release()

		ctx, cancel := context.WithTimeout(r.Context(), macroRequestTimeout)
		defer cancel()

		startWeek, _ := upcomingWeek()
		plan, err := generateMacroPlanFunc(ctx, db, user.ID, startWeek, MacroModeManual)
		if err != nil {
			writeMacroGenerateError(w, user.ID, "generate", startWeek, err)
			return
		}

		writeMacroPlanView(w, r, db, http.StatusCreated, plan)
	}
}

// ExtendMacroPlanHandler appends a fresh block to the end of the athlete's
// horizon on demand, without waiting for the Monday run to reach the
// MacroExtensionLeadWeeks window.
// POST /api/stride/macro/extend
//
// The new block starts the Monday after the *last* active block ends, so an
// athlete who already has an extension queued gets a third block behind it
// rather than having the queued one regenerated away. With no active block left
// to continue there is nothing to extend and the answer is 409 — that athlete
// wants /macro/generate.
func ExtendMacroPlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		release, ok := TryLockUser(user.ID)
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a generation is already running — try again in a moment"})
			return
		}
		defer release()

		ctx, cancel := context.WithTimeout(r.Context(), macroRequestTimeout)
		defer cancel()

		startWeek, err := macroExtensionStartWeek(ctx, db, user.ID)
		if err != nil {
			if errors.Is(err, ErrNoPreviousMacroPlan) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("stride: resolve extension start week for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to extend macro block"})
			return
		}

		plan, err := generateMacroPlanFunc(ctx, db, user.ID, startWeek, MacroModeExtension)
		if err != nil {
			writeMacroGenerateError(w, user.ID, "extend", startWeek, err)
			return
		}

		writeMacroPlanView(w, r, db, http.StatusCreated, plan)
	}
}

// macroExtensionStartWeek returns the Monday an on-demand extension starts: one
// week after the end of the athlete's furthest-out active block. Blocks that
// already ended are ignored, so a horizon that ran out months ago does not
// produce a block starting in the past — that case is ErrNoPreviousMacroPlan and
// belongs to /macro/generate.
func macroExtensionStartWeek(ctx context.Context, db *sql.DB, userID int64) (string, error) {
	thisMonday, _ := currentWeek()
	spans, err := listActiveMacroPlanSpans(ctx, db, userID, thisMonday, macroOpenEndedWeek)
	if err != nil {
		return "", err
	}
	if len(spans) == 0 {
		return "", ErrNoPreviousMacroPlan
	}

	// Spans come back ordered by start_week, and a later start does not
	// guarantee a later end, so the furthest-out horizon is picked explicitly.
	last := spans[0].EndWeek
	for _, span := range spans[1:] {
		if span.EndWeek > last {
			last = span.EndWeek
		}
	}

	end, err := parseWeekDate(last)
	if err != nil {
		return "", err
	}
	return end.AddDate(0, 0, 7).Format(dateLayout), nil
}

// writeMacroPlanView loads the block's goal history and writes the shared
// response shape. A failure here means the block itself was written but cannot
// be read back, which is a 500 even on the POST paths — the generation is
// committed either way, so the athlete's retry finds it through /macro/current.
func writeMacroPlanView(w http.ResponseWriter, r *http.Request, db *sql.DB, status int, plan *MacroPlan) {
	// r.Context() rather than a generation's timed-out context: on the POST
	// paths the block is already committed, so reading it back must not fail
	// just because the Claude call ate the budget.
	view, err := buildMacroPlanView(r.Context(), db, plan)
	if err != nil {
		log.Printf("stride: build macro plan view for plan %d: %v", plan.ID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load macro block"})
		return
	}
	writeJSON(w, status, view)
}

// writeMacroGenerateError maps a GenerateMacroPlan failure onto a status code.
// Everything the athlete can act on — a feature they have switched off, a race
// they do not own, a concurrent generation that got there first — says so;
// anything else is a 500 with the detail in the log.
func writeMacroGenerateError(w http.ResponseWriter, userID int64, action, startWeek string, err error) {
	switch {
	case errors.Is(err, ErrStrideNotEnabled):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, training.ErrClaudeNotEnabled):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrNoPreviousMacroPlan):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrOverlappingMacroPlan):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "another macro block was created while this one was generating — reload and try again"})
	case errors.Is(err, ErrForeignReference):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the generated block references a race or workout that is not yours"})
	case errors.Is(err, context.DeadlineExceeded):
		log.Printf("stride: %s macro block for user %d starting %s timed out after %s", action, userID, startWeek, macroRequestTimeout)
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "macro block generation timed out — try again"})
	default:
		log.Printf("stride: %s macro block for user %d starting %s: %v", action, userID, startWeek, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate macro block"})
	}
}
