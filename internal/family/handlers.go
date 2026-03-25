package family

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("family: writeJSON encode error: %v", err)
	}
}

// StatusHandler returns the family role of the authenticated user.
// GET /api/family/status
func StatusHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		isParent, err := IsParent(db, user.ID)
		if err != nil {
			log.Printf("family: is_parent check user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check family status"})
			return
		}

		isChild, err := IsChild(db, user.ID)
		if err != nil {
			log.Printf("family: is_child check user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check family status"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"is_parent": isParent,
			"is_child":  isChild,
		})
	}
}

// ListChildrenHandler returns all children linked to the authenticated parent.
// GET /api/family/children
func ListChildrenHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		children, err := GetChildren(db, user.ID)
		if err != nil {
			log.Printf("family: list children user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list children"})
			return
		}
		if children == nil {
			children = []FamilyLink{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"children": children})
	}
}

// UnlinkChildHandler removes a child link by child user ID.
// DELETE /api/family/children/{id}
func UnlinkChildHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		childID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid child ID"})
			return
		}

		if err := RemoveChild(db, user.ID, childID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "child link not found"})
				return
			}
			log.Printf("family: unlink child %d for parent %d: %v", childID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to unlink child"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// UpdateChildHandler updates the nickname and avatar emoji for a linked child.
// PUT /api/family/children/{id}
func UpdateChildHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		childID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid child ID"})
			return
		}

		var body struct {
			Nickname    string `json:"nickname"`
			AvatarEmoji string `json:"avatar_emoji"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		link, err := UpdateChild(db, user.ID, childID, body.Nickname, body.AvatarEmoji)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "child link not found"})
				return
			}
			log.Printf("family: update child %d for parent %d: %v", childID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update child"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"link": link})
	}
}

// GenerateInviteHandler generates an invite code for the authenticated parent.
// POST /api/family/invite
func GenerateInviteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Only users who are not already linked as a child may generate invites.
		isChild, err := IsChild(db, user.ID)
		if err != nil {
			log.Printf("family: is_child check user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check family status"})
			return
		}
		if isChild {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "child accounts cannot generate invite codes"})
			return
		}

		invite, err := GenerateInviteCode(db, user.ID)
		if err != nil {
			log.Printf("family: generate invite user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate invite code"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"invite": invite})
	}
}

// AcceptInviteHandler accepts an invite code, linking the authenticated user as a child.
// POST /api/family/invite/accept
func AcceptInviteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Users who already have linked children cannot become a child themselves.
		isParent, err := IsParent(db, user.ID)
		if err != nil {
			log.Printf("family: is_parent check user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check family status"})
			return
		}
		if isParent {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "accounts with linked children cannot be linked as a child"})
			return
		}

		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		body.Code = strings.TrimSpace(strings.ToUpper(body.Code))
		if body.Code == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code is required"})
			return
		}

		link, err := AcceptInviteCode(db, body.Code, user.ID)
		if err != nil {
			switch {
			case errors.Is(err, ErrInvalidCode):
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "invalid invite code"})
			case errors.Is(err, ErrCodeAlreadyUsed):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "invite code has already been used"})
			case errors.Is(err, ErrCodeExpired):
				writeJSON(w, http.StatusGone, map[string]string{"error": "invite code has expired"})
			case errors.Is(err, ErrAlreadyLinked):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "account is already linked to a parent"})
			case errors.Is(err, ErrSelfLink):
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot link to your own account"})
			default:
				log.Printf("family: accept invite user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to accept invite code"})
			}
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"link": link})
	}
}

// verifyParentChild returns nil if a family_links row exists for parentID → childID,
// or an error (including sql.ErrNoRows) otherwise.
func verifyParentChild(ctx context.Context, db *sql.DB, parentID, childID int64) error {
	var id int64
	return db.QueryRowContext(ctx, `
		SELECT id FROM family_links WHERE parent_id = ? AND child_id = ?
	`, parentID, childID).Scan(&id)
}

// childWorkoutStreaks returns the current and longest workout streaks for a user.
// Dates are calculated from UTC day boundaries.
func childWorkoutStreaks(ctx context.Context, db *sql.DB, userID int64) (current, longest int, err error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT date(started_at) AS d
		FROM workouts
		WHERE user_id = ?
		ORDER BY d DESC
	`, userID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var dates []time.Time
	for rows.Next() {
		var ds string
		if err := rows.Scan(&ds); err != nil {
			return 0, 0, err
		}
		if ds == "" {
			log.Printf("family: childWorkoutStreaks: empty date for user %d", userID)
			continue
		}
		t, err := time.Parse("2006-01-02", ds)
		if err != nil {
			log.Printf("family: childWorkoutStreaks: invalid date %q for user %d: %v", ds, userID, err)
			continue
		}
		dates = append(dates, t)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	if len(dates) == 0 {
		return 0, 0, nil
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)

	// Current streak: consecutive days ending today or yesterday.
	if dates[0].Equal(today) || dates[0].Equal(today.AddDate(0, 0, -1)) {
		expected := dates[0]
		for _, d := range dates {
			if d.Equal(expected) {
				current++
				expected = expected.AddDate(0, 0, -1)
			} else {
				break
			}
		}
	}

	// Longest streak: find the longest consecutive daily run.
	longest = 1
	run := 1
	for i := 1; i < len(dates); i++ {
		if dates[i-1].Sub(dates[i]) == 24*time.Hour {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 1
		}
	}

	return current, longest, nil
}

// ChildStatsHandler returns star balance, level, streaks, weekly stats, and recent transactions
// for a child. The authenticated user must be the parent of the requested child.
// GET /api/family/children/{id}/stats
func ChildStatsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		childID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid child ID"})
			return
		}

		if err := verifyParentChild(r.Context(), db, user.ID, childID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "not authorized to view this child's data"})
			} else {
				log.Printf("family: verify parent-child user %d child %d: %v", user.ID, childID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
			return
		}

		// Star balance.
		var totalEarned, totalSpent, currentBalance int
		err = db.QueryRowContext(r.Context(), `
			SELECT COALESCE(total_earned, 0), COALESCE(total_spent, 0), COALESCE(current_balance, 0)
			FROM star_balances WHERE user_id = ?
		`, childID).Scan(&totalEarned, &totalSpent, &currentBalance)
		if err != nil && err != sql.ErrNoRows {
			log.Printf("family: child stats balance user %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load balance"})
			return
		}

		// Level info (defaults if missing).
		level, xp := 1, 0
		levelTitle := "Rookie Runner"
		if scanErr := db.QueryRowContext(r.Context(), `
			SELECT xp, level, title FROM user_levels WHERE user_id = ?
		`, childID).Scan(&xp, &level, &levelTitle); scanErr != nil && scanErr != sql.ErrNoRows {
			log.Printf("family: child stats level user %d: %v", childID, scanErr)
		}

		// Workout streaks.
		currentStreak, longestStreak, streakErr := childWorkoutStreaks(r.Context(), db, childID)
		if streakErr != nil {
			log.Printf("family: child stats streaks user %d: %v", childID, streakErr)
		}

		// Weekly stats (Monday-based calendar weeks, UTC).
		now := time.Now().UTC()
		daysSinceMonday := (int(now.Weekday()) + 6) % 7
		thisWeekStart := now.AddDate(0, 0, -daysSinceMonday).Truncate(24 * time.Hour)
		lastWeekStart := thisWeekStart.AddDate(0, 0, -7)
		thisWeekStartStr := thisWeekStart.Format(time.RFC3339)
		lastWeekStartStr := lastWeekStart.Format(time.RFC3339)

		var thisWeekStars, thisWeekWorkouts int
		if scanErr := db.QueryRowContext(r.Context(), `
			SELECT COALESCE(SUM(amount), 0), COUNT(DISTINCT reference_id)
			FROM star_transactions
			WHERE user_id = ? AND created_at >= ? AND amount > 0
		`, childID, thisWeekStartStr).Scan(&thisWeekStars, &thisWeekWorkouts); scanErr != nil {
			log.Printf("family: child stats this-week query user %d: %v", childID, scanErr)
		}

		var lastWeekStars, lastWeekWorkouts int
		if scanErr := db.QueryRowContext(r.Context(), `
			SELECT COALESCE(SUM(amount), 0), COUNT(DISTINCT reference_id)
			FROM star_transactions
			WHERE user_id = ? AND created_at >= ? AND created_at < ? AND amount > 0
		`, childID, lastWeekStartStr, thisWeekStartStr).Scan(&lastWeekStars, &lastWeekWorkouts); scanErr != nil {
			log.Printf("family: child stats last-week query user %d: %v", childID, scanErr)
		}

		// Recent star transactions (last 10).
		txRows, err := db.QueryContext(r.Context(), `
			SELECT id, amount, reason, description, created_at
			FROM star_transactions
			WHERE user_id = ?
			ORDER BY created_at DESC
			LIMIT 10
		`, childID)
		if err != nil {
			log.Printf("family: child stats transactions user %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load transactions"})
			return
		}
		defer txRows.Close()

		type txRecord struct {
			ID          int64  `json:"id"`
			Amount      int    `json:"amount"`
			Reason      string `json:"reason"`
			Description string `json:"description"`
			CreatedAt   string `json:"created_at"`
		}
		recentTxns := []txRecord{}
		for txRows.Next() {
			var tx txRecord
			if err := txRows.Scan(&tx.ID, &tx.Amount, &tx.Reason, &tx.Description, &tx.CreatedAt); err != nil {
				log.Printf("family: child stats tx scan user %d: %v", childID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan transactions"})
				return
			}
			recentTxns = append(recentTxns, tx)
		}
		if err := txRows.Err(); err != nil {
			log.Printf("family: child stats tx rows user %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read transactions"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"current_balance":            currentBalance,
			"total_earned":               totalEarned,
			"total_spent":                totalSpent,
			"level":                      level,
			"xp":                         xp,
			"title":                      levelTitle,
			"current_streak":             currentStreak,
			"longest_streak":             longestStreak,
			"this_week_stars":            thisWeekStars,
			"this_week_starred_workouts": thisWeekWorkouts,
			"last_week_stars":            lastWeekStars,
			"last_week_starred_workouts": lastWeekWorkouts,
			"recent_transactions":        recentTxns,
			"active_challenges":          []any{},
		})
	}
}

// ChildWorkoutsHandler returns a paginated list of workouts for a child.
// Response includes date, sport, duration, distance, avg HR, calories, ascent, and stars earned.
// GPS/sample data is never included. The authenticated user must be the parent of the child.
// GET /api/family/children/{id}/workouts?limit=20&offset=0
func ChildWorkoutsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		childID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid child ID"})
			return
		}

		if err := verifyParentChild(r.Context(), db, user.ID, childID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "not authorized to view this child's workouts"})
			} else {
				log.Printf("family: verify parent-child user %d child %d: %v", user.ID, childID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
			return
		}

		limit := 20
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		// Total count for pagination.
		var total int
		if err := db.QueryRowContext(r.Context(), `
			SELECT COUNT(*) FROM workouts WHERE user_id = ?
		`, childID).Scan(&total); err != nil {
			log.Printf("family: child workouts count user %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to count workouts"})
			return
		}

		// Workouts with stars earned per workout via LEFT JOIN aggregation.
		// GPS/sample data is intentionally excluded.
		rows, err := db.QueryContext(r.Context(), `
			SELECT w.id, w.started_at, w.sport, w.duration_seconds, w.distance_meters,
			       w.avg_heart_rate, w.calories, w.ascent_meters,
			       COALESCE(s.stars, 0) AS stars
			FROM workouts w
			LEFT JOIN (
				SELECT reference_id, user_id, SUM(amount) AS stars
				FROM star_transactions
				WHERE amount > 0
				GROUP BY reference_id, user_id
			) s ON s.reference_id = w.id AND s.user_id = w.user_id
			WHERE w.user_id = ?
			ORDER BY w.started_at DESC
			LIMIT ? OFFSET ?
		`, childID, limit, offset)
		if err != nil {
			log.Printf("family: child workouts query user %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workouts"})
			return
		}
		defer rows.Close()

		type childWorkout struct {
			ID              int64   `json:"id"`
			StartedAt       string  `json:"started_at"`
			Sport           string  `json:"sport"`
			DurationSeconds int     `json:"duration_seconds"`
			DistanceMeters  float64 `json:"distance_meters"`
			AvgHeartRate    int     `json:"avg_heart_rate"`
			Calories        int     `json:"calories"`
			AscentMeters    float64 `json:"ascent_meters"`
			Stars           int     `json:"stars"`
		}

		workouts := []childWorkout{}
		for rows.Next() {
			var wo childWorkout
			if err := rows.Scan(
				&wo.ID, &wo.StartedAt, &wo.Sport, &wo.DurationSeconds, &wo.DistanceMeters,
				&wo.AvgHeartRate, &wo.Calories, &wo.AscentMeters, &wo.Stars,
			); err != nil {
				log.Printf("family: child workouts scan user %d: %v", childID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan workouts"})
				return
			}
			workouts = append(workouts, wo)
		}
		if err := rows.Err(); err != nil {
			log.Printf("family: child workouts rows user %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read workouts"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"workouts": workouts,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	}
}

// familyPushClient is used for reward-related push notifications from the family package.
var familyPushClient = &http.Client{Timeout: 10 * time.Second}

// sendClaimResolvedPush sends a push notification to a child when a claim is resolved.
// Errors are logged and not propagated.
func sendClaimResolvedPush(db *sql.DB, childID int64, rewardTitle, status string) {
	var body string
	if status == "approved" {
		body = fmt.Sprintf("Your reward '%s' was approved! 🎉", rewardTitle)
	} else {
		body = fmt.Sprintf("Your reward claim for '%s' was denied. Stars have been refunded.", rewardTitle)
	}
	payload := push.Notification{
		Title: "Reward Update",
		Body:  body,
		Tag:   "reward-resolved",
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("family: marshal claim resolved push: %v", err)
		return
	}
	if _, err := push.SendToUser(db, familyPushClient, childID, payloadBytes); err != nil {
		log.Printf("family: send claim resolved push to child %d: %v", childID, err)
	}
}

// ListRewardsHandler returns all rewards created by the authenticated parent.
// GET /api/family/rewards
func ListRewardsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		rewards, err := GetRewards(db, user.ID)
		if err != nil {
			log.Printf("family: list rewards user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list rewards"})
			return
		}
		if rewards == nil {
			rewards = []Reward{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"rewards": rewards})
	}
}

// CreateRewardHandler creates a new reward for the authenticated parent.
// POST /api/family/rewards
func CreateRewardHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			StarCost    int    `json:"star_cost"`
			IconEmoji   string `json:"icon_emoji"`
			IsActive    *bool  `json:"is_active"`
			MaxClaims   *int   `json:"max_claims"`
			ParentNote  string `json:"parent_note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if strings.TrimSpace(body.Title) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
			return
		}
		if body.StarCost < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "star_cost must be non-negative"})
			return
		}
		if body.MaxClaims != nil && *body.MaxClaims <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_claims must be >= 1 or omitted for unlimited"})
			return
		}
		isActive := true
		if body.IsActive != nil {
			isActive = *body.IsActive
		}
		if body.IconEmoji == "" {
			body.IconEmoji = "🎁"
		}

		reward, err := CreateReward(db, user.ID, body.Title, body.Description, body.IconEmoji, body.ParentNote, body.StarCost, isActive, body.MaxClaims)
		if err != nil {
			log.Printf("family: create reward user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create reward"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"reward": reward})
	}
}

// UpdateRewardHandler updates an existing reward owned by the authenticated parent.
// PUT /api/family/rewards/{id}
func UpdateRewardHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		rewardID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reward ID"})
			return
		}

		var body struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			StarCost    int    `json:"star_cost"`
			IconEmoji   string `json:"icon_emoji"`
			IsActive    *bool  `json:"is_active"`
			MaxClaims   *int   `json:"max_claims"`
			ParentNote  string `json:"parent_note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if strings.TrimSpace(body.Title) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
			return
		}
		if body.StarCost < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "star_cost must be non-negative"})
			return
		}
		if body.MaxClaims != nil && *body.MaxClaims <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_claims must be >= 1 or omitted for unlimited"})
			return
		}
		isActive := true
		if body.IsActive != nil {
			isActive = *body.IsActive
		}
		if body.IconEmoji == "" {
			body.IconEmoji = "🎁"
		}

		reward, err := UpdateReward(db, rewardID, user.ID, body.Title, body.Description, body.IconEmoji, body.ParentNote, body.StarCost, isActive, body.MaxClaims)
		if err != nil {
			if errors.Is(err, ErrRewardNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "reward not found"})
				return
			}
			log.Printf("family: update reward %d user %d: %v", rewardID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update reward"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"reward": reward})
	}
}

// DeleteRewardHandler permanently removes a reward owned by the authenticated parent.
// DELETE /api/family/rewards/{id}
func DeleteRewardHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		rewardID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reward ID"})
			return
		}

		if err := DeleteReward(db, rewardID, user.ID); err != nil {
			if errors.Is(err, ErrRewardNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "reward not found"})
				return
			}
			log.Printf("family: delete reward %d user %d: %v", rewardID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete reward"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ListClaimsHandler returns reward claims for all children of the authenticated parent.
// An optional ?status= query parameter filters by claim status (pending, approved, denied).
// GET /api/family/claims
func ListClaimsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		status := r.URL.Query().Get("status")

		claims, err := GetAllClaims(db, user.ID, status)
		if err != nil {
			log.Printf("family: list claims user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list claims"})
			return
		}
		if claims == nil {
			claims = []ClaimWithDetails{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"claims": claims})
	}
}

// ResolveClaimHandler approves or denies a pending reward claim.
// Denying a claim automatically refunds the child's stars.
// PUT /api/family/claims/{id}
func ResolveClaimHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		claimID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid claim ID"})
			return
		}

		var body struct {
			Status string `json:"status"`
			Note   string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Status != "approved" && body.Status != "denied" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be 'approved' or 'denied'"})
			return
		}

		claim, err := ResolveClaim(db, claimID, user.ID, body.Status, body.Note)
		if err != nil {
			switch {
			case errors.Is(err, ErrClaimNotFound):
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "claim not found"})
			case errors.Is(err, ErrClaimNotPending):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "claim has already been resolved"})
			default:
				log.Printf("family: resolve claim %d user %d: %v", claimID, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to resolve claim"})
			}
			return
		}

		// Send push notification to child asynchronously.
		go func() {
			title, titleErr := GetRewardTitleByID(db, claim.RewardID)
			if titleErr != nil {
				log.Printf("family: get reward title for push notification claim %d: %v", claimID, titleErr)
				return
			}
			sendClaimResolvedPush(db, claim.ChildID, title, claim.Status)
		}()

		writeJSON(w, http.StatusOK, map[string]any{"claim": claim})
	}
}
