package allowance

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

// MondayOf returns the Monday of the ISO week containing t, formatted as YYYY-MM-DD.
func MondayOf(t time.Time) string {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7
	}
	monday := t.AddDate(0, 0, -(weekday - 1))
	return monday.Format("2006-01-02")
}

// CalculateWeeklyEarnings computes the earnings breakdown for a child for the week
// starting at weekStart (YYYY-MM-DD, Monday).
//
// It counts all approved completions (or pending completions that have been open
// for at least autoApproveHours) and sums their chore amounts. The base weekly
// allowance from Settings is added on top. Bonus rules are evaluated and applied.
func CalculateWeeklyEarnings(db *sql.DB, parentID, childID int64, weekStart string) (*WeeklyEarnings, error) {
	settings, err := GetSettings(db, parentID, childID)
	if err != nil {
		return nil, err
	}

	// Auto-approve any stale pending completions for this child before calculating.
	if _, err := AutoApproveStaleCompletions(db, parentID, childID, settings.AutoApproveHours); err != nil {
		// Non-fatal: log and proceed with current state.
		log.Printf("allowance: auto-approve stale completions for parent %d child %d: %v", parentID, childID, err)
	}

	completions, err := GetChildCompletionsForWeek(db, childID, weekStart)
	if err != nil {
		return nil, err
	}

	// Collect distinct chore IDs from approved own completions.
	choreIDSet := make(map[int64]struct{})
	approvedCompletionIDs := []int64{}
	for _, comp := range completions {
		if comp.Status == "approved" {
			choreIDSet[comp.ChoreID] = struct{}{}
			approvedCompletionIDs = append(approvedCompletionIDs, comp.ID)
		}
	}

	// Fetch team participations where this child is a joiner (not the initiator).
	weekStartParsed, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return nil, err
	}
	weekEndDate := weekStartParsed.AddDate(0, 0, 6).Format("2006-01-02")
	teamParticipations, err := GetTeamParticipationsForChildInWeek(db, childID, weekStart, weekEndDate)
	if err != nil {
		return nil, err
	}
	for _, tp := range teamParticipations {
		choreIDSet[tp.ChoreID] = struct{}{}
	}

	// Fetch chore amounts and team_bonus_pct for all relevant chores.
	type choreInfo struct {
		amount       float64
		teamBonusPct float64
	}
	choreDetails := make(map[int64]choreInfo, len(choreIDSet))
	if len(choreIDSet) > 0 {
		choreIDs := make([]int64, 0, len(choreIDSet))
		for id := range choreIDSet {
			choreIDs = append(choreIDs, id)
		}
		rows, err := db.Query(buildChoreDetailsQuery(len(choreIDs)), toAnySlice(choreIDs)...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var info choreInfo
			if err := rows.Scan(&id, &info.amount, &info.teamBonusPct); err != nil {
				return nil, err
			}
			choreDetails[id] = info
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// Get team participant counts for approved own completions to detect team bonus.
	teamParticipantCounts, err := GetTeamParticipantCounts(db, approvedCompletionIDs)
	if err != nil {
		return nil, err
	}
	if teamParticipantCounts == nil {
		teamParticipantCounts = map[int64]int{}
	}

	// Sum approved chore earnings for the week (own completions).
	var choreEarnings float64
	approvedCount := 0
	for _, comp := range completions {
		if comp.Status != "approved" {
			continue
		}
		approvedCount++
		info := choreDetails[comp.ChoreID]
		amount := info.amount // base amount is 0 if chore was deleted
		// Apply team bonus if this completion had team participants.
		if teamParticipantCounts[comp.ID] > 0 {
			amount = amount * (1 + info.teamBonusPct/100)
		}
		choreEarnings += amount
		choreEarnings += comp.QualityBonus
	}

	// Add earnings from team participations where this child was a joiner.
	for _, tp := range teamParticipations {
		info := choreDetails[tp.ChoreID]
		amount := info.amount * (1 + info.teamBonusPct/100)
		choreEarnings += amount
		choreEarnings += tp.QualityBonus
		approvedCount++
	}

	// Add approved extras for this child for the week.
	weekEnd := weekStartParsed.AddDate(0, 0, 7).Format(time.RFC3339)
	weekStartRFC := weekStartParsed.Format(time.RFC3339)
	var extraEarnings float64
	rows, err := db.Query(`
		SELECT amount FROM allowance_extras
		WHERE (child_id = ? OR claimed_by = ?)
		  AND status = 'approved'
		  AND approved_at >= ? AND approved_at < ?
		  AND parent_id = ?
	`, childID, childID, weekStartRFC, weekEnd, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var amt float64
		if err := rows.Scan(&amt); err != nil {
			return nil, err
		}
		extraEarnings += amt
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	choreEarnings += extraEarnings

	// Evaluate bonus rules.
	bonusRules, err := GetBonusRules(db, parentID)
	if err != nil {
		return nil, err
	}

	bonusAmount := calculateBonuses(bonusRules, choreEarnings, completions, weekStart)

	// Include any bingo bonus earned this week.
	bingoBonus, err := GetBingoBonusForWeek(db, childID, weekStart)
	if err != nil {
		return nil, fmt.Errorf("allowance: get bingo bonus: %w", err)
	}
	bonusAmount += bingoBonus

	baseAllowance := settings.BaseWeeklyAmount
	total := baseAllowance + choreEarnings + bonusAmount

	return &WeeklyEarnings{
		ChildID:       childID,
		WeekStart:     weekStart,
		BaseAllowance: baseAllowance,
		ChoreEarnings: choreEarnings,
		BonusAmount:   bonusAmount,
		TotalAmount:   total,
		Currency:      settings.Currency,
		ApprovedCount: approvedCount,
	}, nil
}

// calculateBonuses evaluates active bonus rules against the week's completions.
// Currently supports the full_week and streak bonus types.
func calculateBonuses(rules []BonusRule, baseEarnings float64, completions []Completion, weekStart string) float64 {
	if len(rules) == 0 || baseEarnings == 0 {
		return 0
	}

	var bonus float64
	for _, rule := range rules {
		if !rule.Active {
			continue
		}
		switch rule.Type {
		case "full_week":
			// Full week bonus: child has at least one approved completion on all 7 days.
			if hasCompletionEveryDay(completions, weekStart) {
				if rule.Multiplier > 1.0 {
					bonus += baseEarnings * (rule.Multiplier - 1.0)
				}
				bonus += rule.FlatAmount
			}
		case "streak":
			// Streak bonus: applied if the child has completions on all 7 days of the week.
			if hasCompletionEveryDay(completions, weekStart) {
				bonus += rule.FlatAmount
				if rule.Multiplier > 1.0 {
					bonus += baseEarnings * (rule.Multiplier - 1.0)
				}
			}
		}
		// early_bird and quality bonuses are applied at the individual completion level
		// and are not currently computed here (Phase 2).
	}
	return bonus
}

// buildChoreDetailsQuery returns a SQL query that selects id, amount, and team_bonus_pct
// from allowance_chores for n chore IDs.
func buildChoreDetailsQuery(n int) string {
	placeholders := strings.Repeat("?,", n)
	placeholders = placeholders[:len(placeholders)-1]
	return "SELECT id, amount, team_bonus_pct FROM allowance_chores WHERE id IN (" + placeholders + ")"
}

// toAnySlice converts a slice of int64 to []any for use in db.Query varargs.
func toAnySlice(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// hasCompletionEveryDay returns true when there is at least one approved completion
// for each of the 7 days of the week starting at weekStart.
func hasCompletionEveryDay(completions []Completion, weekStart string) bool {
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return false
	}
	days := make(map[string]bool, 7)
	for i := range 7 {
		days[start.AddDate(0, 0, i).Format("2006-01-02")] = false
	}
	for _, c := range completions {
		if c.Status == "approved" {
			if _, ok := days[c.Date]; ok {
				days[c.Date] = true
			}
		}
	}
	for _, done := range days {
		if !done {
			return false
		}
	}
	return true
}
