package allowance

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/family"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
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

// verifyParentChildLink checks that childID is linked to parentID in family_links.
// Returns false and writes a 400/500 response if the check fails.
func verifyParentChildLink(db *sql.DB, w http.ResponseWriter, parentID, childID int64) bool {
	var id int64
	err := db.QueryRow(`SELECT id FROM family_links WHERE parent_id = ? AND child_id = ?`, parentID, childID).Scan(&id)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusBadRequest, errResponse("child_id is not linked to your account"))
		return false
	}
	if err != nil {
		log.Printf("allowance: verify parent-child link parent %d child %d: %v", parentID, childID, err)
		writeJSON(w, http.StatusInternalServerError, errResponse("failed to verify family link"))
		return false
	}
	return true
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
			ChildID          *int64   `json:"child_id"`
			Name             string   `json:"name"`
			Description      string   `json:"description"`
			Amount           float64  `json:"amount"`
			Frequency        string   `json:"frequency"`
			Icon             string   `json:"icon"`
			RequiresApproval *bool    `json:"requires_approval"`
			CompletionMode   string   `json:"completion_mode"`
			MinTeamSize      *int64   `json:"min_team_size"`
			TeamBonusPct     *float64 `json:"team_bonus_pct"`
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
		if req.CompletionMode == "" {
			req.CompletionMode = "solo"
		}
		if req.CompletionMode != "solo" && req.CompletionMode != "team" {
			writeJSON(w, http.StatusBadRequest, errResponse("completion_mode must be solo or team"))
			return
		}
		minTeamSize := int64(2)
		if req.MinTeamSize != nil {
			if *req.MinTeamSize < 2 {
				writeJSON(w, http.StatusBadRequest, errResponse("min_team_size must be at least 2"))
				return
			}
			minTeamSize = *req.MinTeamSize
		}
		teamBonusPct := 10.0
		if req.TeamBonusPct != nil {
			if *req.TeamBonusPct < 0 || *req.TeamBonusPct > 100 {
				writeJSON(w, http.StatusBadRequest, errResponse("team_bonus_pct must be between 0 and 100"))
				return
			}
			teamBonusPct = *req.TeamBonusPct
		}
		if req.ChildID != nil {
			if !verifyParentChildLink(db, w, user.ID, *req.ChildID) {
				return
			}
		}
		requiresApproval := true
		if req.RequiresApproval != nil {
			requiresApproval = *req.RequiresApproval
		}

		chore, err := CreateChore(db, user.ID, req.ChildID, req.Name, req.Description,
			req.Amount, req.Frequency, req.Icon, requiresApproval, req.CompletionMode, minTeamSize, teamBonusPct)
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
			CompletionMode   *string  `json:"completion_mode"`
			MinTeamSize      *int64   `json:"min_team_size"`
			TeamBonusPct     *float64 `json:"team_bonus_pct"`
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
			if *req.Name == "" {
				writeJSON(w, http.StatusBadRequest, errResponse("name must not be empty"))
				return
			}
			name = *req.Name
		}
		description := existing.Description
		if req.Description != nil {
			description = *req.Description
		}
		amount := existing.Amount
		if req.Amount != nil {
			if *req.Amount < 0 {
				writeJSON(w, http.StatusBadRequest, errResponse("amount must be non-negative"))
				return
			}
			amount = *req.Amount
		}
		frequency := existing.Frequency
		if req.Frequency != nil {
			if *req.Frequency != "daily" && *req.Frequency != "weekly" && *req.Frequency != "once" {
				writeJSON(w, http.StatusBadRequest, errResponse("frequency must be daily, weekly, or once"))
				return
			}
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
		completionMode := existing.CompletionMode
		if req.CompletionMode != nil {
			if *req.CompletionMode != "solo" && *req.CompletionMode != "team" {
				writeJSON(w, http.StatusBadRequest, errResponse("completion_mode must be solo or team"))
				return
			}
			completionMode = *req.CompletionMode
		}
		minTeamSize := existing.MinTeamSize
		if req.MinTeamSize != nil {
			if *req.MinTeamSize < 2 {
				writeJSON(w, http.StatusBadRequest, errResponse("min_team_size must be at least 2"))
				return
			}
			minTeamSize = *req.MinTeamSize
		}
		teamBonusPct := existing.TeamBonusPct
		if req.TeamBonusPct != nil {
			if *req.TeamBonusPct < 0 || *req.TeamBonusPct > 100 {
				writeJSON(w, http.StatusBadRequest, errResponse("team_bonus_pct must be between 0 and 100"))
				return
			}
			teamBonusPct = *req.TeamBonusPct
		}

		chore, err := UpdateChore(db, choreID, user.ID, childID, name, description,
			amount, frequency, icon, requiresApproval, active, completionMode, minTeamSize, teamBonusPct)
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

		// Auto-approve stale completions (all children, default 24h) before listing.
		if _, err := AutoApproveStaleCompletions(db, user.ID, 0, 24); err != nil {
			log.Printf("allowance: auto-approve pending list parent %d: %v", user.ID, err)
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

		// Notify the child asynchronously.
		go func(childID, completionID int64) {
			// Respect the child's quiet hours before sending.
			if quiethours.IsActive(db, childID) {
				return
			}
			body := "A chore was approved — check your earnings!"
			// Try to enrich the message with the chore name.
			if chore, err := GetChoreByID(db, comp.ChoreID, user.ID); err == nil {
				body = fmt.Sprintf("'%s' approved — check your earnings!", chore.Name)
			}
			payload, err := json.Marshal(push.Notification{
				Title: "Chore approved!",
				Body:  body,
				URL:   "/chores",
				Tag:   fmt.Sprintf("allowance-approval-%d", completionID),
			})
			if err != nil {
				log.Printf("allowance: marshal approval push payload child %d completion %d: %v", childID, completionID, err)
				return
			}
			if _, err := push.SendToUser(db, push.DefaultHTTPClient, childID, payload); err != nil {
				log.Printf("allowance: approval push child %d: %v", childID, err)
			}
		}(comp.ChildID, comp.ID)
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength != 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}

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
		if req.ChildID != nil {
			if !verifyParentChildLink(db, w, user.ID, *req.ChildID) {
				return
			}
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

// generatePayoutsForWeek auto-calculates and upserts payout records for all children
// of parentID for the given weekStart. Errors are logged but do not fail the request.
func generatePayoutsForWeek(db *sql.DB, parentID int64, weekStart string) {
	children, err := family.GetChildren(db, parentID)
	if err != nil {
		log.Printf("allowance: generate payouts get children parent %d: %v", parentID, err)
		return
	}
	for _, child := range children {
		earnings, err := CalculateWeeklyEarnings(db, parentID, child.ChildID, weekStart)
		if err != nil {
			log.Printf("allowance: generate payout calculate earnings parent %d child %d week %s: %v", parentID, child.ChildID, weekStart, err)
			continue
		}
		if _, err := UpsertPayout(db, parentID, child.ChildID, weekStart, earnings.BaseAllowance, earnings.BonusAmount, earnings.TotalAmount); err != nil {
			log.Printf("allowance: generate payout upsert parent %d child %d week %s: %v", parentID, child.ChildID, weekStart, err)
		}
	}
}

// ListPayoutsHandler returns payout history for the authenticated parent and
// auto-generates payout records for the current week before returning.
// GET /api/allowance/payouts?child={id}&weeks=4
func ListPayoutsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		// Auto-generate payout records for the current week so the parent always
		// sees up-to-date summaries when they open the payouts tab.
		generatePayoutsForWeek(db, user.ID, MondayOf(time.Now().UTC()))

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
		if req.FlatAmount < 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("flat_amount must be non-negative"))
			return
		}
		if req.Multiplier == 0 {
			req.Multiplier = 1.0
		}
		if req.Multiplier < 1.0 {
			writeJSON(w, http.StatusBadRequest, errResponse("multiplier must be >= 1.0"))
			return
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
		if req.BaseWeeklyAmount < 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("base_weekly_amount must be >= 0"))
			return
		}
		const maxAutoApproveHours = 168
		autoApproveHours := 24
		if req.AutoApproveHours != nil {
			if *req.AutoApproveHours < 1 {
				writeJSON(w, http.StatusBadRequest, errResponse("auto_approve_hours must be >= 1"))
				return
			}
			if *req.AutoApproveHours > maxAutoApproveHours {
				writeJSON(w, http.StatusBadRequest, errResponse("auto_approve_hours must be <= 168"))
				return
			}
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

// QualityBonusHandler adds an extra quality bonus amount to a completion.
// POST /api/allowance/quality-bonus/{id}
func QualityBonusHandler(db *sql.DB) http.HandlerFunc {
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
			Amount float64 `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		if math.IsNaN(req.Amount) || math.IsInf(req.Amount, 0) || req.Amount < 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("amount must be a finite non-negative number"))
			return
		}

		comp, err := AddQualityBonus(db, completionID, user.ID, req.Amount)
		if err != nil {
			if errors.Is(err, ErrCompletionNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("completion not found"))
				return
			}
			log.Printf("allowance: quality bonus completion %d parent %d: %v", completionID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to add quality bonus"))
			return
		}
		writeJSON(w, http.StatusOK, comp)
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength != 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		if req.Date == "" {
			req.Date = time.Now().Format("2006-01-02")
		} else if _, err := time.Parse("2006-01-02", req.Date); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("date must be in YYYY-MM-DD format"))
			return
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

// ApproveExtraHandler allows a parent to approve a claimed/completed extra task.
// POST /api/allowance/extras/{id}/approve
func ApproveExtraHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !requireParent(db, w, user) {
			return
		}

		extraID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid extra ID"))
			return
		}

		extra, err := ApproveExtra(db, extraID, user.ID)
		if err != nil {
			if errors.Is(err, ErrExtraNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("extra task not found or not in a claimable state"))
				return
			}
			log.Printf("allowance: approve extra %d parent %d: %v", extraID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to approve extra"))
			return
		}
		writeJSON(w, http.StatusOK, extra)
	}
}

// CompleteExtraHandler allows a child to mark a claimed extra as completed.
// POST /api/allowance/my/complete-extra/{id}
func CompleteExtraHandler(db *sql.DB) http.HandlerFunc {
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

		extra, err := CompleteExtra(db, extraID, user.ID)
		if err != nil {
			switch {
			case errors.Is(err, ErrExtraNotFound):
				writeJSON(w, http.StatusNotFound, errResponse("extra task not found"))
			case errors.Is(err, ErrExtraNotOpen):
				writeJSON(w, http.StatusConflict, errResponse("extra task is not in claimed state"))
			default:
				log.Printf("allowance: complete extra %d child %d: %v", extraID, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, errResponse("failed to complete extra"))
			}
			return
		}
		writeJSON(w, http.StatusOK, extra)
	}
}

// TeamStartHandler creates a 'waiting_for_team' completion for a team chore and records
// the initiating child as the first participant.
// POST /api/allowance/my/team-start/{chore_id}
func TeamStartHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}

		choreID, err := strconv.ParseInt(chi.URLParam(r, "chore_id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid chore ID"))
			return
		}

		var req struct {
			Date string `json:"date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength != 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		if req.Date == "" {
			req.Date = time.Now().Format("2006-01-02")
		} else if _, err := time.Parse("2006-01-02", req.Date); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("date must be in YYYY-MM-DD format"))
			return
		}

		completion, err := StartTeamCompletion(db, link.ParentID, choreID, user.ID, req.Date)
		if err != nil {
			switch {
			case errors.Is(err, ErrChoreNotFound):
				writeJSON(w, http.StatusNotFound, errResponse("chore not found"))
			case errors.Is(err, ErrChoreNotTeamMode):
				writeJSON(w, http.StatusBadRequest, errResponse("chore is not in team completion mode"))
			case errors.Is(err, ErrCompletionExists):
				writeJSON(w, http.StatusConflict, errResponse("a team session for this chore already exists today"))
			default:
				log.Printf("allowance: team-start chore %d child %d: %v", choreID, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, errResponse("failed to start team session"))
			}
			return
		}
		writeJSON(w, http.StatusCreated, completion)

		// Notify siblings that a team chore session is starting.
		go notifyTeamStart(db, choreID, user.ID, link.ParentID, completion.ID)
	}
}

// notifyTeamStart sends push notifications to siblings (excluding the starter) when a
// team chore session begins. Extracted for testability.
func notifyTeamStart(db *sql.DB, choreID, starterID, parentID, completionID int64) {
	chore, err := GetChoreByID(db, choreID, parentID)
	if err != nil {
		log.Printf("allowance: team-start push: get chore %d: %v", choreID, err)
		return
	}
	siblings, err := family.GetChildren(db, parentID)
	if err != nil {
		log.Printf("allowance: team-start push: get siblings parent %d: %v", parentID, err)
		return
	}
	payload, err := json.Marshal(push.Notification{
		Title: "Team chore starting!",
		Body:  fmt.Sprintf("Join '%s' — a teammate is waiting!", chore.Name),
		URL:   "/chores",
		Tag:   fmt.Sprintf("team-start-%d", completionID),
	})
	if err != nil {
		log.Printf("allowance: team-start push: marshal: %v", err)
		return
	}
	for _, sibling := range siblings {
		if sibling.ChildID == starterID {
			continue
		}
		if quiethours.IsActive(db, sibling.ChildID) {
			continue
		}
		if _, sendErr := push.SendToUser(db, push.DefaultHTTPClient, sibling.ChildID, payload); sendErr != nil {
			log.Printf("allowance: team-start push sibling %d: %v", sibling.ChildID, sendErr)
		}
	}
}

// TeamJoinHandler adds the authenticated child to an existing 'waiting_for_team' session.
// Promotes the completion to 'pending' once min_team_size is reached.
// POST /api/allowance/my/team-join/{completion_id}
func TeamJoinHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}

		completionID, err := strconv.ParseInt(chi.URLParam(r, "completion_id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid completion ID"))
			return
		}

		completion, err := JoinTeamCompletion(db, link.ParentID, completionID, user.ID)
		if err != nil {
			switch {
			case errors.Is(err, ErrCompletionNotFound):
				writeJSON(w, http.StatusNotFound, errResponse("team session not found"))
			case errors.Is(err, ErrSessionNotWaiting):
				writeJSON(w, http.StatusConflict, errResponse("team session is no longer accepting new members"))
			case errors.Is(err, ErrAlreadyJoined):
				writeJSON(w, http.StatusConflict, errResponse("already joined this team session"))
			default:
				log.Printf("allowance: team-join completion %d child %d: %v", completionID, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, errResponse("failed to join team session"))
			}
			return
		}
		writeJSON(w, http.StatusOK, completion)

		// When the team reaches min size, notify all participants that it's ready.
		// completion.Status is only "pending" if this join actually triggered the promotion
		// (RowsAffected > 0 in JoinTeamCompletion), so duplicate notifications are prevented.
		if completion.Status == "pending" {
			go notifyTeamComplete(db, completion.ChoreID, completionID, link.ParentID)
		}
	}
}

// notifyTeamComplete sends push notifications to all team participants when the team
// reaches its minimum size and the completion is promoted to 'pending' approval.
// Extracted for testability.
func notifyTeamComplete(db *sql.DB, choreID, completionID, parentID int64) {
	chore, err := GetChoreByID(db, choreID, parentID)
	if err != nil {
		log.Printf("allowance: team-join complete push: get chore %d: %v", choreID, err)
		return
	}
	rows, queryErr := db.Query(
		`SELECT child_id FROM allowance_team_completions WHERE completion_id = ?`,
		completionID,
	)
	if queryErr != nil {
		log.Printf("allowance: team-join complete push: get participants %d: %v", completionID, queryErr)
		return
	}
	defer rows.Close()
	var participantIDs []int64
	for rows.Next() {
		var pid int64
		if scanErr := rows.Scan(&pid); scanErr != nil {
			log.Printf("allowance: team-join complete push: scan: %v", scanErr)
			return
		}
		participantIDs = append(participantIDs, pid)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		log.Printf("allowance: team-join complete push: rows error completion %d: %v", completionID, rowsErr)
		return
	}
	payload, err := json.Marshal(push.Notification{
		Title: "Team complete!",
		Body:  fmt.Sprintf("'%s' is done — waiting for parent approval.", chore.Name),
		URL:   "/chores",
		Tag:   fmt.Sprintf("team-complete-%d", completionID),
	})
	if err != nil {
		log.Printf("allowance: team-join complete push: marshal: %v", err)
		return
	}
	for _, pid := range participantIDs {
		if quiethours.IsActive(db, pid) {
			continue
		}
		if _, sendErr := push.SendToUser(db, push.DefaultHTTPClient, pid, payload); sendErr != nil {
			log.Printf("allowance: team-join complete push participant %d: %v", pid, sendErr)
		}
	}
}

// MySiblingsHandler returns the basic identity info (child_id, nickname, avatar_emoji)
// for all siblings of the authenticated child. Used by the team-chore UI to display
// participant avatars without leaking stars-specific data (balance, level, title).
// GET /api/allowance/my/siblings
func MySiblingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}

		type siblingInfo struct {
			ChildID     int64  `json:"child_id"`
			Nickname    string `json:"nickname"`
			AvatarEmoji string `json:"avatar_emoji"`
		}

		rows, err := db.QueryContext(r.Context(), `
			SELECT child_id, nickname, avatar_emoji
			FROM family_links
			WHERE parent_id = ? AND child_id != ?
			ORDER BY created_at ASC
		`, link.ParentID, user.ID)
		if err != nil {
			log.Printf("allowance: my-siblings parent %d: %v", link.ParentID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to load siblings"))
			return
		}
		defer rows.Close()

		siblings := []siblingInfo{}
		for rows.Next() {
			var s siblingInfo
			if scanErr := rows.Scan(&s.ChildID, &s.Nickname, &s.AvatarEmoji); scanErr != nil {
				log.Printf("allowance: my-siblings scan parent %d: %v", link.ParentID, scanErr)
				writeJSON(w, http.StatusInternalServerError, errResponse("failed to read siblings"))
				return
			}
			siblings = append(siblings, s)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			log.Printf("allowance: my-siblings rows parent %d: %v", link.ParentID, rowsErr)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to read siblings"))
			return
		}
		writeJSON(w, http.StatusOK, siblings)
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

// ---- Savings goal handlers ----

// ListChildGoalsHandler returns savings goals for a specific child.
// GET /api/allowance/children/{id}/goals
func ListChildGoalsHandler(db *sql.DB) http.HandlerFunc {
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
		if !verifyParentChildLink(db, w, user.ID, childID) {
			return
		}
		goals, err := GetSavingsGoals(db, user.ID, childID)
		if err != nil {
			log.Printf("allowance: list goals parent %d child %d: %v", user.ID, childID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to list goals"))
			return
		}
		if goals == nil {
			goals = []SavingsGoal{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"goals": goals})
	}
}

// CreateChildGoalHandler creates a savings goal for a child.
// POST /api/allowance/children/{id}/goals
func CreateChildGoalHandler(db *sql.DB) http.HandlerFunc {
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
		if !verifyParentChildLink(db, w, user.ID, childID) {
			return
		}
		var req struct {
			Name         string  `json:"name"`
			TargetAmount float64 `json:"target_amount"`
			Deadline     *string `json:"deadline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, errResponse("name is required"))
			return
		}
		if req.TargetAmount <= 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("target_amount must be greater than 0"))
			return
		}
		if req.Deadline != nil && *req.Deadline != "" {
			if _, err := time.Parse("2006-01-02", *req.Deadline); err != nil {
				writeJSON(w, http.StatusBadRequest, errResponse("deadline must be in YYYY-MM-DD format"))
				return
			}
		} else {
			req.Deadline = nil
		}
		goal, err := CreateSavingsGoal(db, user.ID, childID, req.Name, req.TargetAmount, req.Deadline)
		if err != nil {
			log.Printf("allowance: create goal parent %d child %d: %v", user.ID, childID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to create goal"))
			return
		}
		writeJSON(w, http.StatusCreated, goal)
	}
}

// UpdateChildGoalHandler updates a savings goal for a child (parent version).
// PUT /api/allowance/children/{id}/goals/{goalId}
func UpdateChildGoalHandler(db *sql.DB) http.HandlerFunc {
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
		goalID, err := strconv.ParseInt(chi.URLParam(r, "goalId"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid goal ID"))
			return
		}
		if !verifyParentChildLink(db, w, user.ID, childID) {
			return
		}
		var req struct {
			Name          string  `json:"name"`
			TargetAmount  float64 `json:"target_amount"`
			CurrentAmount float64 `json:"current_amount"`
			Deadline      *string `json:"deadline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, errResponse("name is required"))
			return
		}
		if req.TargetAmount <= 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("target_amount must be greater than 0"))
			return
		}
		if req.CurrentAmount < 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("current_amount must be >= 0"))
			return
		}
		if req.Deadline != nil && *req.Deadline != "" {
			if _, err := time.Parse("2006-01-02", *req.Deadline); err != nil {
				writeJSON(w, http.StatusBadRequest, errResponse("deadline must be in YYYY-MM-DD format"))
				return
			}
		} else {
			req.Deadline = nil
		}
		goal, err := UpdateSavingsGoal(db, goalID, user.ID, childID, req.Name, req.TargetAmount, req.CurrentAmount, req.Deadline)
		if err != nil {
			if errors.Is(err, ErrGoalNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("savings goal not found"))
				return
			}
			log.Printf("allowance: update goal %d parent %d: %v", goalID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to update goal"))
			return
		}
		writeJSON(w, http.StatusOK, goal)
	}
}

// DeleteChildGoalHandler removes a savings goal.
// DELETE /api/allowance/children/{id}/goals/{goalId}
func DeleteChildGoalHandler(db *sql.DB) http.HandlerFunc {
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
		goalID, err := strconv.ParseInt(chi.URLParam(r, "goalId"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid goal ID"))
			return
		}
		if !verifyParentChildLink(db, w, user.ID, childID) {
			return
		}
		if err := DeleteSavingsGoal(db, goalID, user.ID, childID); err != nil {
			if errors.Is(err, ErrGoalNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("savings goal not found"))
				return
			}
			log.Printf("allowance: delete goal %d parent %d: %v", goalID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to delete goal"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// MyGoalsHandler returns savings goals for the authenticated child.
// GET /api/allowance/my/goals
func MyGoalsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}
		goals, err := GetSavingsGoals(db, link.ParentID, user.ID)
		if err != nil {
			log.Printf("allowance: my goals child %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to get goals"))
			return
		}
		if goals == nil {
			goals = []SavingsGoal{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"goals": goals})
	}
}

// CreateMyGoalHandler lets a child create their own savings goal.
// POST /api/allowance/my/goals
func CreateMyGoalHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}
		var req struct {
			Name         string  `json:"name"`
			TargetAmount float64 `json:"target_amount"`
			Deadline     *string `json:"deadline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, errResponse("name is required"))
			return
		}
		if req.TargetAmount <= 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("target_amount must be greater than 0"))
			return
		}
		if req.Deadline != nil && *req.Deadline != "" {
			if _, err := time.Parse("2006-01-02", *req.Deadline); err != nil {
				writeJSON(w, http.StatusBadRequest, errResponse("deadline must be in YYYY-MM-DD format"))
				return
			}
		} else {
			req.Deadline = nil
		}
		goal, err := CreateSavingsGoal(db, link.ParentID, user.ID, req.Name, req.TargetAmount, req.Deadline)
		if err != nil {
			log.Printf("allowance: create my goal child %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to create goal"))
			return
		}
		writeJSON(w, http.StatusCreated, goal)
	}
}

// MyBingoHandler returns the authenticated child's bingo card for the current (or
// requested) week, updating progress from approved completions before responding.
// GET /api/allowance/my/bingo?week=YYYY-MM-DD
func MyBingoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}

		weekStart := r.URL.Query().Get("week")
		if weekStart == "" {
			weekStart = MondayOf(time.Now())
		} else if _, err := time.Parse("2006-01-02", weekStart); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("week must be in YYYY-MM-DD format"))
			return
		}

		card, err := UpdateBingoProgress(db, user.ID, link.ParentID, weekStart)
		if err != nil {
			log.Printf("allowance: my bingo child %d week %s: %v", user.ID, weekStart, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to load bingo card"))
			return
		}
		writeJSON(w, http.StatusOK, card)
	}
}

// UpdateMyGoalHandler lets a child update the current saved amount on their goal.
// PUT /api/allowance/my/goals/{id}
func UpdateMyGoalHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		link := requireChild(db, w, user)
		if link == nil {
			return
		}
		goalID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid goal ID"))
			return
		}
		var req struct {
			CurrentAmount float64 `json:"current_amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid request body"))
			return
		}
		if req.CurrentAmount < 0 {
			writeJSON(w, http.StatusBadRequest, errResponse("current_amount must be >= 0"))
			return
		}
		// Fetch current goal to preserve name, target, deadline.
		existing, err := GetSavingsGoalByID(db, goalID, link.ParentID, user.ID)
		if err != nil {
			if errors.Is(err, ErrGoalNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("savings goal not found"))
				return
			}
			log.Printf("allowance: update my goal fetch child %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to update goal"))
			return
		}
		goal, err := UpdateSavingsGoal(db, goalID, link.ParentID, user.ID, existing.Name, existing.TargetAmount, req.CurrentAmount, existing.Deadline)
		if err != nil {
			if errors.Is(err, ErrGoalNotFound) {
				writeJSON(w, http.StatusNotFound, errResponse("savings goal not found"))
				return
			}
			log.Printf("allowance: update my goal %d child %d: %v", goalID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to update goal"))
			return
		}
		writeJSON(w, http.StatusOK, goal)
	}
}
