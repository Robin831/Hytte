package allowance

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/family"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("allowance: writeJSON encode error: %v", err)
	}
}

func errResponse(msg string) map[string]string {
	return map[string]string{"error": msg}
}

// requireParent verifies the authenticated user is a parent and returns true if so.
func requireParent(db *sql.DB, w http.ResponseWriter, user *auth.User) bool {
	isParent, err := family.IsParent(db, user.ID)
	if err != nil {
		log.Printf("allowance: is_parent check user %d: %v", user.ID, err)
		writeJSON(w, http.StatusInternalServerError, errResponse("failed to check parent status"))
		return false
	}
	if !isParent {
		writeJSON(w, http.StatusForbidden, errResponse("only parents can perform this action"))
		return false
	}
	return true
}

// requireChild verifies the authenticated user is linked to a parent and returns the link.
// Returns nil and writes an error response if not a child.
func requireChild(db *sql.DB, w http.ResponseWriter, user *auth.User) *family.FamilyLink {
	link, err := family.GetParent(db, user.ID)
	if err != nil {
		log.Printf("allowance: get parent for child %d: %v", user.ID, err)
		writeJSON(w, http.StatusInternalServerError, errResponse("failed to get family link"))
		return nil
	}
	if link == nil {
		writeJSON(w, http.StatusForbidden, errResponse("not linked to a parent account"))
		return nil
	}
	return link
}

// ---- Parent handlers ----

// ListChoresHandler returns all chores owned by the authenticated parent.
// GET /api/allowance/chores
func ListChoresHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		chores, err := GetChores(db, user.ID)
		if err != nil {
			log.Printf("allowance: list chores parent %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to list chores"))
			return
		}
		if chores == nil {
			chores = []Chore{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"chores": chores})
	}
}

// CreateChoreHandler creates a new chore for the authenticated parent.
// POST /api/allowance/chores
func CreateChoreHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		var req struct {
			ChildID          *int64  `json:"child_id"`
			Name             string  `json:"name"`
			Description      string  `json:"description"`
			Amount           float64 `json:"amount"`
			Frequency        string  `json:"frequency"`
			Icon             string  `json:"icon"`
			RequiresApproval *bool   `json:"requires_approval"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, errResponse("name is required"))
			return
		}
		if req.Amount < 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("amount must be non-negative"))
			return
		}
		if req.Frequency == "" {
			req.Frequency = "daily"
		}
		if req.Frequency != "daily" && req.Frequency != "weekly" && req.Frequency != "once" {
			writeJSON(w, http.StatusBadRequest, errResponse("frequency must be daily, weekly, or once"))
			return
		}
		if req.Icon == "" {
			req.Icon = "🧹"
		}
		requiresApproval := true
		if req.RequiresApproval != nil {
			requiresApproval = *req.RequiresApproval
		}

		chore, err := CreateChore(db, user.ID, req.ChildID, req.Name, req.Description,
			req.Amount, req.Frequency, req.Icon, requiresApproval)
		if err != nil {
			log.Printf("allowance: create chore parent %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to create chore"))
			return
		}
		writeJSON(w, http.StatusCreated, chore)
	}
}

// UpdateChoreHandler updates an existing chore owned by the authenticated parent.
// PUT /api/allowance/chores/{id}
func UpdateChoreHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		choreID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid chore ID"))
			return
		}

		existing, err := GetChoreByID(db, choreID, user.ID)
		if err != nil {
			if errors.Is(err, ErrChoreNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("chore not found"))
				return
			}
			log.Printf("allowance: get chore %d parent %d: %v", choreID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to get chore"))
			return
		}

		var req struct {
			ChildID          *int64   `json:"child_id"`
			Name             *string  `json:"name"`
			Description      *string  `json:"description"`
			Amount           *float64 `json:"amount"`
			Frequency        *string  `json:"frequency"`
			Icon             *string  `json:"icon"`
			RequiresApproval *bool    `json:"requires_approval"`
			Active           *bool    `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}

		childID := existing.ChildID
		if req.ChildID != nil {
			childID = req.ChildID
		}
		name := existing.Name
		if req.Name != nil {
			name = *req.Name
		}
		description := existing.Description
		if req.Description != nil {
			description = *req.Description
		}
		amount := existing.Amount
		if req.Amount != nil {
			amount = *req.Amount
		}
		frequency := existing.Frequency
		if req.Frequency != nil {
			frequency = *req.Frequency
		}
		icon := existing.Icon
		if req.Icon != nil {
			icon = *req.Icon
		}
		requiresApproval := existing.RequiresApproval
		if req.RequiresApproval != nil {
			requiresApproval = *req.RequiresApproval
		}
		active := existing.Active
		if req.Active != nil {
			active = *req.Active
		}

		chore, err := UpdateChore(db, choreID, user.ID, childID, name, description,
			amount, frequency, icon, requiresApproval, active)
		if err != nil {
			if errors.Is(err, ErrChoreNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("chore not found"))
				return
			}
			log.Printf("allowance: update chore %d parent %d: %v", choreID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to update chore"))
			return
		}
		writeJSON(w, http.StatusOK, chore)
	}
}

// DeactivateChoreHandler deactivates a chore (soft delete).
// DELETE /api/allowance/chores/{id}
func DeactivateChoreHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		choreID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid chore ID"))
			return
		}

		if err := DeactivateChore(db, choreID, user.ID); err != nil {
			if errors.Is(err, ErrChoreNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("chore not found"))
				return
			}
			log.Printf("allowance: deactivate chore %d parent %d: %v", choreID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to deactivate chore"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ListPendingHandler returns all pending completions for the parent's children.
// GET /api/allowance/pending
func ListPendingHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		pending, err := GetPendingCompletions(db, user.ID)
		if err != nil {
			log.Printf("allowance: list pending parent %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to list pending completions"))
			return
		}
		if pending == nil {
			pending = []CompletionWithDetails{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"pending": pending})
	}
}

// ApproveCompletionHandler approves a pending completion.
// POST /api/allowance/approve/{id}
func ApproveCompletionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		completionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid completion ID"))
			return
		}

		comp, err := ApproveCompletion(db, completionID, user.ID)
		if err != nil {
			switch {
			case errors.Is(err, ErrCompletionNotFound):
				writeJSON(w, http.StatusNotFound, errResponse("completion not found"))
			case errors.Is(err, ErrCompletionNotPending):
				writeJSON(w, http.StatusConflict, errResponse("completion is not pending"))
			default:
				log.Printf("allowance: approve completion %d parent %d: %v", completionID, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, errResponse("failed to approve completion"))
			}
			return
		}
		writeJSON(w, http.StatusOK, comp)
	}
}

// RejectCompletionHandler rejects a pending completion with an optional reason.
// POST /api/allowance/reject/{id}
func RejectCompletionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		completionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid completion ID"))
			return
		}

		var req struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req) // reason is optional

		comp, err := RejectCompletion(db, completionID, user.ID, req.Reason)
		if err != nil {
			switch {
			case errors.Is(err, ErrCompletionNotFound):
				writeJSON(w, http.StatusNotFound, errResponse("completion not found"))
			case errors.Is(err, ErrCompletionNotPending):
				writeJSON(w, http.StatusConflict, errResponse("completion is not pending"))
			default:
				log.Printf("allowance: reject completion %d parent %d: %v", completionID, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, errResponse("failed to reject completion"))
			}
			return
		}
		writeJSON(w, http.StatusOK, comp)
	}
}

// ListExtrasHandler returns all extras for the authenticated parent.
// GET /api/allowance/extras
func ListExtrasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		extras, err := GetExtras(db, user.ID)
		if err != nil {
			log.Printf("allowance: list extras parent %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to list extras"))
			return
		}
		if extras == nil {
			extras = []Extra{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"extras": extras})
	}
}

// CreateExtraHandler creates a one-off extra task.
// POST /api/allowance/extras
func CreateExtraHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		var req struct {
			ChildID   *int64  `json:"child_id"`
			Name      string  `json:"name"`
			Amount    float64 `json:"amount"`
			ExpiresAt *string `json:"expires_at"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, errResponse("name is required"))
			return
		}
		if req.Amount < 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("amount must be non-negative"))
			return
		}

		extra, err := CreateExtra(db, user.ID, req.ChildID, req.Name, req.Amount, req.ExpiresAt)
		if err != nil {
			log.Printf("allowance: create extra parent %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to create extra"))
			return
		}
		writeJSON(w, http.StatusCreated, extra)
	}
}

// ListPayoutsHandler returns payout history for the authenticated parent.
// GET /api/allowance/payouts?child={id}&weeks=4
func ListPayoutsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		var childID *int64
		if raw := r.URL.Query().Get("child"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, errResponse("invalid child ID"))
				return
			}
			childID = &id
		}

		weeks := 4
		if raw := r.URL.Query().Get("weeks"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				weeks = n
			}
		}

		payouts, err := GetPayouts(db, user.ID, childID, weeks)
		if err != nil {
			log.Printf("allowance: list payouts parent %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to list payouts"))
			return
		}
		if payouts == nil {
			payouts = []Payout{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"payouts": payouts})
	}
}

// MarkPaidHandler marks a payout as paid out.
// POST /api/allowance/payouts/{id}/paid
func MarkPaidHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		payoutID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid payout ID"))
			return
		}

		payout, err := MarkPayoutPaid(db, payoutID, user.ID)
		if err != nil {
			if errors.Is(err, ErrPayoutNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("payout not found"))
				return
			}
			log.Printf("allowance: mark paid payout %d parent %d: %v", payoutID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to mark payout as paid"))
			return
		}
		writeJSON(w, http.StatusOK, payout)
	}
}

// ListBonusRulesHandler returns bonus rules for the authenticated parent.
// GET /api/allowance/bonuses
func ListBonusRulesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		rules, err := GetBonusRules(db, user.ID)
		if err != nil {
			log.Printf("allowance: list bonus rules parent %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to list bonus rules"))
			return
		}
		if rules == nil {
			rules = []BonusRule{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"bonus_rules": rules})
	}
}

// UpdateBonusRulesHandler creates or updates a bonus rule.
// PUT /api/allowance/bonuses
func UpdateBonusRulesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		var req struct {
			Type       string  `json:"type"`
			Multiplier float64 `json:"multiplier"`
			FlatAmount float64 `json:"flat_amount"`
			Active     *bool   `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		validTypes := map[string]bool{
			"full_week":  true,
			"early_bird": true,
			"streak":     true,
			"quality":    true,
		}
		if !validTypes[req.Type] {
			writeJSON(w, http.StatusBadRequest, errResponse("type must be full_week, early_bird, streak, or quality"))
			return
		}
		if req.Multiplier == 0 {
			req.Multiplier = 1.0
		}
		active := true
		if req.Active != nil {
			active = *req.Active
		}

		rule, err := UpsertBonusRule(db, user.ID, req.Type, req.Multiplier, req.FlatAmount, active)
		if err != nil {
			log.Printf("allowance: upsert bonus rule parent %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to update bonus rule"))
			return
		}
		writeJSON(w, http.StatusOK, rule)
	}
}

// GetChildSettingsHandler returns allowance settings for a specific child.
// GET /api/allowance/children/{id}/settings
func GetChildSettingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		childID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid child ID"))
			return
		}

		settings, err := GetSettings(db, user.ID, childID)
		if err != nil {
			log.Printf("allowance: get settings parent %d child %d: %v", user.ID, childID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to get settings"))
			return
		}
		writeJSON(w, http.StatusOK, settings)
	}
}

// UpdateChildSettingsHandler updates allowance settings for a specific child.
// PUT /api/allowance/children/{id}/settings
func UpdateChildSettingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		childID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid child ID"))
			return
		}

		var req struct {
			BaseWeeklyAmount float64 `json:"base_weekly_amount"`
			AutoApproveHours *int    `json:"auto_approve_hours"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		autoApproveHours := 24
		if req.AutoApproveHours != nil {
			autoApproveHours = *req.AutoApproveHours
		}

		settings, err := UpsertSettings(db, user.ID, childID, req.BaseWeeklyAmount, autoApproveHours)
		if err != nil {
			log.Printf("allowance: update settings parent %d child %d: %v", user.ID, childID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to update settings"))
			return
		}
		writeJSON(w, http.StatusOK, settings)
	}
}

// ---- Kid handlers ----

// MyChoresHandler returns today's chores with completion status for the authenticated child.
// GET /api/allowance/my/chores
func MyChoresHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}

		dateStr := r.URL.Query().Get("date")
		if dateStr == "" {
			dateStr = time.Now().Format("2006-01-02")
		}

		chores, err := GetChildChoresWithStatus(db, link.ParentID, user.ID, dateStr)
		if err != nil {
			log.Printf("allowance: my chores child %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to get chores"))
			return
		}
		if chores == nil {
			chores = []ChoreWithStatus{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"chores": chores, "date": dateStr})
	}
}

// CompleteChoreHandler records a child marking a chore as done.
// POST /api/allowance/my/complete/{id}
func CompleteChoreHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}

		choreID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid chore ID"))
			return
		}

		chore, err := GetChoreByID(db, choreID, link.ParentID)
		if err != nil {
			if errors.Is(err, ErrChoreNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("chore not found"))
				return
			}
			log.Printf("allowance: get chore %d for child %d: %v", choreID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to get chore"))
			return
		}
		if !chore.Active {
			writeJSON(w, http.StatusBadRequest, errResponse("chore is not active"))
			return
		}
		if chore.ChildID != nil && *chore.ChildID != user.ID {
			writeJSON(w, http.StatusForbidden, errResponse("chore is not assigned to you"))
			return
		}

		var req struct {
			Date  string `json:"date"`
			Notes string `json:"notes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Date == "" {
			req.Date = time.Now().Format("2006-01-02")
		}

		completion, err := CreateCompletion(db, choreID, user.ID, req.Date, req.Notes)
		if err != nil {
			if errors.Is(err, ErrCompletionExists) {
				writeJSON(w, http.StatusConflict, errResponse("chore already completed for this date"))
				return
			}
			log.Printf("allowance: complete chore %d child %d: %v", choreID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to record completion"))
			return
		}
		writeJSON(w, http.StatusCreated, completion)
	}
}

// MyExtrasHandler returns available extra tasks for the authenticated child.
// GET /api/allowance/my/extras
func MyExtrasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}

		extras, err := GetOpenExtras(db, link.ParentID, user.ID)
		if err != nil {
			log.Printf("allowance: my extras child %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to get extras"))
			return
		}
		if extras == nil {
			extras = []Extra{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"extras": extras})
	}
}

// ClaimExtraHandler allows a child to claim an open extra task.
// POST /api/allowance/my/claim-extra/{id}
func ClaimExtraHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if requireChild(db, w, user) == nil {
			return
		}

		extraID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid extra ID"))
			return
		}

		extra, err := ClaimExtra(db, extraID, user.ID)
		if err != nil {
			switch {
			case errors.Is(err, ErrExtraNotFound):
				writeJSON(w, http.StatusNotFound, errResponse("extra task not found"))
			case errors.Is(err, ErrExtraNotOpen):
				writeJSON(w, http.StatusConflict, errResponse("extra task is no longer available"))
			default:
				log.Printf("allowance: claim extra %d child %d: %v", extraID, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, errResponse("failed to claim extra"))
			}
			return
		}
		writeJSON(w, http.StatusOK, extra)
	}
}

// MyEarningsHandler returns this week's earnings breakdown for the authenticated child.
// GET /api/allowance/my/earnings
func MyEarningsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}

		weekStart := r.URL.Query().Get("week")
		if weekStart == "" {
			weekStart = MondayOf(time.Now())
		}

		earnings, err := CalculateWeeklyEarnings(db, link.ParentID, user.ID, weekStart)
		if err != nil {
			log.Printf("allowance: calculate earnings child %d week %s: %v", user.ID, weekStart, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to calculate earnings"))
			return
		}
		writeJSON(w, http.StatusOK, earnings)
	}
}

// MyHistoryHandler returns past weekly payout history for the authenticated child.
// GET /api/allowance/my/history
func MyHistoryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}

		weeks := 8
		if raw := r.URL.Query().Get("weeks"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				weeks = n
			}
		}

		payouts, err := GetPayouts(db, link.ParentID, &user.ID, weeks)
		if err != nil {
			log.Printf("allowance: my history child %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to get history"))
			return
		}
		if payouts == nil {
			payouts = []Payout{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"payouts": payouts})
	}
}
