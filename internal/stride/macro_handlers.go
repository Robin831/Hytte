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
// calls a generation may make, plus the extra call and the pause a
// macroTransportRetries retry adds on top of them — together all but the whole
// request, since the DB reads either side are milliseconds. The transport
// retry has to be counted here or a generation that spends one would be cut
// off by this deadline before it had used the corrective attempts it was still
// owed, and the athlete would see a timeout instead. Bounding the request as
// well as the calls is what stops a wedged CLI from holding the handler (and
// the athlete's lock) open for as long as the process lives.
//
// A var rather than a const only because macroTransportRetryDelay is one (tests
// shrink it); its value is fixed at start-up.
var macroRequestTimeout = macroGenerateAttempts*macroClaudeTimeout +
	macroTransportRetries*(macroClaudeTimeout+macroTransportRetryDelay)

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
//
// The generation itself runs in the background (startMacroGeneration) and the
// request answers 202 as soon as it is under way; the outcome is published on
// the training SSE hub. Answering only once the block is written did not
// survive production: a 26-week generation takes minutes, Cloudflare drops a
// request that has sent nothing for ~100s, and the cancelled request context
// SIGKILLed the Claude CLI mid-flight (2026-09-07).
func GenerateMacroPlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		release, ok := TryLockUser(user.ID)
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a generation is already running — try again in a moment"})
			return
		}

		startWeek, _ := upcomingWeek()
		startMacroGeneration(db, user.ID, macroActionGenerate, startWeek, MacroModeManual, release)
		writeMacroAccepted(w, macroActionGenerate, startWeek)
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
// wants /macro/generate. Like /macro/generate the Claude call runs in the
// background and the request answers 202; only the start-week lookup, which is
// milliseconds, happens before the answer.
func ExtendMacroPlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		release, ok := TryLockUser(user.ID)
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a generation is already running — try again in a moment"})
			return
		}

		startWeek, err := macroExtensionStartWeek(r.Context(), db, user.ID)
		if err != nil {
			release()
			if errors.Is(err, ErrNoPreviousMacroPlan) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("stride: resolve extension start week for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to extend macro block"})
			return
		}

		startMacroGeneration(db, user.ID, macroActionExtend, startWeek, MacroModeExtension, release)
		writeMacroAccepted(w, macroActionExtend, startWeek)
	}
}

// macroAction names a hand-triggered generation in the 202 body, the SSE
// events and the log, so the page can tell which of its two buttons an
// outcome belongs to.
const (
	macroActionGenerate = "generate"
	macroActionExtend   = "extend"
)

// MacroAccepted is the 202 body of the two POST macro endpoints: the block is
// not there yet, this is what was started. The page waits for the matching
// stride_macro_ready / stride_macro_failed event rather than for a body.
type MacroAccepted struct {
	Status    string `json:"status"`
	Action    string `json:"action"`
	StartWeek string `json:"start_week"`
}

func writeMacroAccepted(w http.ResponseWriter, action, startWeek string) {
	writeJSON(w, http.StatusAccepted, MacroAccepted{Status: "generating", Action: action, StartWeek: startWeek})
}

// startMacroGeneration runs one hand-triggered generation off the request
// goroutine. The caller has already taken the athlete's lock; release is called
// when the run ends, whatever the outcome, so the lock is held for exactly as
// long as a Claude call could be writing a block — the same window the Monday
// run holds it for.
//
// The run gets its own context rather than the request's: the request is
// answered and gone within milliseconds, and cancelling with it is precisely
// the failure this exists to avoid. macroRequestTimeout still bounds the run so
// a wedged CLI cannot hold the lock for as long as the process lives.
//
// The outcome is published on the training hub for the athlete: a ready event
// carrying the new block's id, or a failed event carrying the same message the
// synchronous endpoint used to answer with, so the page shows the athlete the
// reason (a race that is not theirs, Stride switched off) rather than a
// generic error.
func startMacroGeneration(db *sql.DB, userID int64, action, startWeek string, mode MacroMode, release func()) {
	go func() {
		defer release()

		ctx, cancel := context.WithTimeout(context.Background(), macroRequestTimeout)
		defer cancel()

		plan, err := generateMacroPlanFunc(ctx, db, userID, startWeek, mode)
		if err != nil {
			msg := macroGenerateErrorMessage(userID, action, startWeek, err)
			training.DefaultHub().Publish(userID, training.Event{Type: training.EventStrideMacroFailed, Action: action, Error: msg})
			return
		}

		log.Printf("stride: %s macro block %d for user %d starting %s", action, plan.ID, userID, startWeek)
		training.DefaultHub().Publish(userID, training.Event{Type: training.EventStrideMacroReady, Action: action, MacroPlanID: plan.ID})
	}()
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
// response shape.
func writeMacroPlanView(w http.ResponseWriter, r *http.Request, db *sql.DB, status int, plan *MacroPlan) {
	view, err := buildMacroPlanView(r.Context(), db, plan)
	if err != nil {
		log.Printf("stride: build macro plan view for plan %d: %v", plan.ID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load macro block"})
		return
	}
	writeJSON(w, status, view)
}

// macroGenerateErrorMessage turns a GenerateMacroPlan failure into the message
// the stride_macro_failed event carries. Everything the athlete can act on — a
// feature they have switched off, a race they do not own, a concurrent
// generation that got there first — says so; anything else is a generic
// message with the detail in the log.
func macroGenerateErrorMessage(userID int64, action, startWeek string, err error) string {
	switch {
	case errors.Is(err, ErrStrideNotEnabled),
		errors.Is(err, training.ErrClaudeNotEnabled),
		errors.Is(err, ErrNoPreviousMacroPlan):
		return err.Error()
	case errors.Is(err, ErrOverlappingMacroPlan):
		return "another macro block was created while this one was generating — reload and try again"
	case errors.Is(err, ErrForeignReference):
		return "the generated block references a race or workout that is not yours"
	case errors.Is(err, context.DeadlineExceeded):
		log.Printf("stride: %s macro block for user %d starting %s timed out after %s", action, userID, startWeek, macroRequestTimeout)
		return "macro block generation timed out — try again"
	default:
		log.Printf("stride: %s macro block for user %d starting %s: %v", action, userID, startWeek, err)
		return "failed to generate macro block"
	}
}
