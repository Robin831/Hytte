package stride

// The macro-block section: the plan context every prompt that touches a week of
// an active block renders. AdjustWeek plans inside it (adjust_prompt.go) and the
// plan-editing chat edits inside it (chat_prompt.go), so the block is rendered
// once here rather than written twice and allowed to drift.
//
// The renderer is pure: it takes the already-loaded block, target week and goal
// revision and returns text. loadMacroPlanBlock is the convenience wrapper for
// callers that only have a week start, and returns "" when no active block
// covers it.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// macroWeekAt returns the block's week starting at weekStart.
func macroWeekAt(block *MacroPlan, weekStart string) (MacroWeek, bool) {
	if block == nil {
		return MacroWeek{}, false
	}
	for _, w := range block.Weeks {
		if w.WeekStart == weekStart {
			return w, true
		}
	}
	return MacroWeek{}, false
}

// currentBlockGoal returns the block's goal in whichever revision is current,
// together with a label naming that revision for the prompt.
//
// A block always has its initial revision, so a missing history is a read
// failure rather than a new athlete — fall back to the block's own goal rather
// than dropping it.
//
// AdjustWeek reads the goal through here too, so the target the +/-3% clamp
// measures a proposal against is exactly the target the prompt showed the
// coach. Deriving it twice is how a proposal comes to be judged against a
// number the model never saw.
func currentBlockGoal(ctx context.Context, db *sql.DB, userID int64, block *MacroPlan) (goal MacroGoal, revisionLabel string) {
	goal, revisionLabel = block.Goal, "initial"
	revisions, err := ListGoalRevisions(ctx, db, block.ID, userID)
	if err != nil {
		log.Printf("stride: load goal revisions for macro plan %d: %v", block.ID, err)
		return goal, revisionLabel
	}
	if n := len(revisions); n > 0 {
		latest := revisions[n-1]
		return latest.Goal, fmt.Sprintf("revision %d, set %s, source %s", n, latest.WeekStart, latest.Source)
	}
	return goal, revisionLabel
}

// loadMacroPlanBlock resolves the active macro block covering weekStart and
// renders its section. Returns "" when the athlete has no active block, when no
// week of that block starts at weekStart, or when the lookup fails — a missing
// block is the ordinary state for an athlete whose plan predates macro
// planning, so the caller renders nothing rather than failing.
func loadMacroPlanBlock(ctx context.Context, db *sql.DB, userID int64, weekStart string) string {
	block, err := GetActiveMacroPlan(ctx, db, userID, weekStart)
	if err != nil {
		log.Printf("stride: load active macro plan for user %d week %s: %v", userID, weekStart, err)
		return ""
	}
	if block == nil {
		return ""
	}
	target, ok := macroWeekAt(block, weekStart)
	if !ok {
		return ""
	}
	goal, revisionLabel := currentBlockGoal(ctx, db, userID, block)
	return renderMacroPlanBlock(block, target, goal, revisionLabel)
}

// renderMacroPlanBlock renders the block this week belongs to: the goal in its
// current revision, where the week sits in the periodisation, the week's own
// spec, and the weeks either side of it for continuity.
//
// It takes the already-loaded block, its already-resolved target week and its
// already-resolved goal rather than looking any of them up again, because
// buildBlockProgressTable, buildFitnessSignals and the goal clamp in AdjustWeek
// need the same values; reading them once means every one of them describes the
// same plan even if a regeneration lands while the prompt is being built.
func renderMacroPlanBlock(block *MacroPlan, target MacroWeek, goal MacroGoal, revisionLabel string) string {
	weekStart := target.WeekStart

	var sb strings.Builder
	sb.WriteString("## Macro Block\n")
	sb.WriteString("This week is one week of an existing training block. The block's goal and this week's spec are the contract you are adjusting inside.\n")
	fmt.Fprintf(&sb, "- Block: %s through %s (%d weeks), status %s\n", block.StartWeek, block.EndWeek, len(block.Weeks), block.Status)
	if block.StaleReason != "" {
		fmt.Fprintf(&sb, "- Block is marked stale (%s): the race calendar changed since it was built. Work inside it anyway and say so in your summary.\n", block.StaleReason)
	}
	sb.WriteString("\n")

	fmt.Fprintf(&sb, "### Block Goal (%s)\n", revisionLabel)
	if goal.PrimaryFocus != "" {
		fmt.Fprintf(&sb, "- Focus: %s\n", goal.PrimaryFocus)
	}
	if goal.Statement != "" {
		fmt.Fprintf(&sb, "- Statement: %s\n", goal.Statement)
	}
	if goal.TargetHMTimeS > 0 {
		fmt.Fprintf(&sb, "- Target half-marathon time: %s\n", formatRaceTime(goal.TargetHMTimeS))
	}
	if goal.Benchmark != "" {
		fmt.Fprintf(&sb, "- Benchmark: %s\n", goal.Benchmark)
	}
	if goal.Rationale != "" {
		fmt.Fprintf(&sb, "- Rationale: %s\n", goal.Rationale)
	}
	sb.WriteString("\n")

	sb.WriteString("### Current mesocycle\n")
	fmt.Fprintf(&sb, "%s\n\n", describeMesocycle(block, target))

	fmt.Fprintf(&sb, "### Target week — %s (week %d of %d)\n", target.WeekStart, target.Seq, len(block.Weeks))
	fmt.Fprintf(&sb, "- Phase: %s — NEVER change this\n", target.Phase)
	fmt.Fprintf(&sb, "- Load level: %s\n", target.LoadLevel)
	fmt.Fprintf(&sb, "- Target distance: %.1f km\n", target.TargetKm)
	fmt.Fprintf(&sb, "- Target sessions: %d\n", target.TargetSessions)
	sb.WriteString(renderKeySessions(target.KeySessions))
	if target.Intent != "" {
		fmt.Fprintf(&sb, "- Intent: %s\n", target.Intent)
	}
	if target.RaceID != nil {
		fmt.Fprintf(&sb, "- Contains race id=%d — see the race calendar below\n", *target.RaceID)
	}
	if lib := libraryBlockForPhase(target.Phase); lib != "" {
		fmt.Fprintf(&sb, "- Suitable training block for library selection: %s\n", lib)
	}
	sb.WriteString("\n")

	if prev, ok := macroWeekAt(block, shiftWeek(weekStart, -1)); ok {
		sb.WriteString(renderNeighbourWeek("Previous macro week", prev, len(block.Weeks)))
	} else {
		sb.WriteString("### Previous macro week\nNone — the target week is the first week of the block.\n\n")
	}
	if next, ok := macroWeekAt(block, shiftWeek(weekStart, 1)); ok {
		sb.WriteString(renderNeighbourWeek("Next macro week", next, len(block.Weeks)))
	} else {
		sb.WriteString("### Next macro week\nNone — the target week is the last week of the block.\n\n")
	}

	return sb.String()
}

// shiftWeek returns the Monday n weeks from weekStart, or "" when weekStart is
// not a parseable date (in which case no neighbour week can match it either).
func shiftWeek(weekStart string, n int) string {
	d, err := parseWeekDate(weekStart)
	if err != nil {
		return ""
	}
	return d.AddDate(0, 0, 7*n).Format(dateLayout)
}

// renderKeySessions lists the sessions the macro week is contracted to contain.
func renderKeySessions(sessions []KeySession) string {
	if len(sessions) == 0 {
		return "- Key sessions: none specified\n"
	}
	var sb strings.Builder
	sb.WriteString("- Key sessions:\n")
	for _, ks := range sessions {
		fmt.Fprintf(&sb, "  - %s", ks.Type)
		if ks.Focus != "" {
			fmt.Fprintf(&sb, " — %s", ks.Focus)
		}
		if ks.LibraryID != nil {
			fmt.Fprintf(&sb, " (library id %d)", *ks.LibraryID)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// renderNeighbourWeek renders the week before or after the target one in a
// single line of spec plus its intent — enough for continuity without spending
// the target week's level of detail on it.
func renderNeighbourWeek(title string, w MacroWeek, blockWeeks int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "### %s — %s (week %d of %d)\n", title, w.WeekStart, w.Seq, blockWeeks)
	fmt.Fprintf(&sb, "- %s / %s — %.1f km, %d sessions, status %s\n", w.Phase, w.LoadLevel, w.TargetKm, w.TargetSessions, w.Status)
	if w.Intent != "" {
		fmt.Fprintf(&sb, "- Intent: %s\n", w.Intent)
	}
	sb.WriteString("\n")
	return sb.String()
}

// describeMesocycle renders the target week's position in its mesocycle as
// "Build 2, week 2 of 4, focus threshold density". The mesocycle is looked up
// by the name the week carries — the periodisation and the weeks come out of
// one coach response, so the name is the authoritative link — and falls back to
// the segment whose date span covers the week when the name does not match.
func describeMesocycle(block *MacroPlan, target MacroWeek) string {
	m, ok := findMesocycle(block, target)
	if !ok {
		return "Not recorded for this block."
	}
	week := mesocycleWeekIndex(m, target.WeekStart)
	switch {
	case week <= 0 && m.Focus == "":
		return m.Name
	case week <= 0:
		return fmt.Sprintf("%s, focus %s", m.Name, m.Focus)
	case m.Focus == "":
		return fmt.Sprintf("%s, week %d of %d", m.Name, week, m.Weeks)
	default:
		return fmt.Sprintf("%s, week %d of %d, focus %s", m.Name, week, m.Weeks, m.Focus)
	}
}

// findMesocycle resolves the mesocycle a macro week belongs to, by name first
// and by date span second.
func findMesocycle(block *MacroPlan, target MacroWeek) (Mesocycle, bool) {
	for _, m := range block.Periodisation {
		if m.Name != "" && m.Name == target.Mesocycle {
			return m, true
		}
	}
	for _, m := range block.Periodisation {
		if idx := mesocycleWeekIndex(m, target.WeekStart); idx >= 1 && idx <= m.Weeks {
			return m, true
		}
	}
	return Mesocycle{}, false
}

// mesocycleWeekIndex returns the 1-based position of weekStart inside the
// mesocycle, or 0 when either date is unusable or the week falls outside it.
func mesocycleWeekIndex(m Mesocycle, weekStart string) int {
	start, err := parseWeekDate(m.StartWeek)
	if err != nil {
		return 0
	}
	week, err := parseWeekDate(weekStart)
	if err != nil {
		return 0
	}
	days := int(week.Sub(start).Hours() / 24)
	if days < 0 || days%7 != 0 {
		return 0
	}
	idx := days/7 + 1
	if m.Weeks > 0 && idx > m.Weeks {
		return 0
	}
	return idx
}
