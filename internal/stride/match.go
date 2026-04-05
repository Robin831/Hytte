package stride

import (
	"encoding/json"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

// PlannedSession is a non-rest day entry extracted from a generated training plan.
type PlannedSession struct {
	Date    string   `json:"date"`
	Session *Session `json:"session"`
}

// WorkoutMatch pairs a completed workout with the planned session it corresponds to.
// PlannedSession is nil when the workout has no corresponding planned session
// (i.e. it is a bonus/unplanned workout).
type WorkoutMatch struct {
	Workout        training.Workout `json:"workout"`
	PlannedSession *PlannedSession  `json:"planned_session"`
}

// MatchWorkoutToSession attempts to match a workout to one of the provided planned
// sessions using the following priority:
//  1. Exact date match — workout date equals session date.
//  2. Fuzzy match — date is within ±1 day AND the workout sport is a running-based
//     activity compatible with a Stride plan session.
//
// Returns nil if no session matches (the workout is a bonus workout).
func MatchWorkoutToSession(workout training.Workout, sessions []PlannedSession) *PlannedSession {
	wDate := extractDate(workout.StartedAt)
	if wDate == "" {
		return nil
	}

	// Pass 1: exact date match.
	for i := range sessions {
		if sessions[i].Date == wDate {
			cp := sessions[i]
			return &cp
		}
	}

	// Pass 2: fuzzy match — ±1 day and compatible sport.
	if !isRunningActivity(workout.Sport) {
		return nil
	}
	wt, err := time.Parse("2006-01-02", wDate)
	if err != nil {
		return nil
	}
	for i := range sessions {
		st, err := time.Parse("2006-01-02", sessions[i].Date)
		if err != nil {
			continue
		}
		diff := wt.Sub(st)
		if diff < 0 {
			diff = -diff
		}
		if diff <= 24*time.Hour {
			cp := sessions[i]
			return &cp
		}
	}

	return nil
}

// MatchAllWorkouts matches each workout in the slice against the sessions in plan,
// returning one WorkoutMatch per workout. Each planned session can only be claimed
// by one workout (first come, first served in input order). Workouts with no
// matching session have a nil PlannedSession field.
func MatchAllWorkouts(workouts []training.Workout, plan Plan) []WorkoutMatch {
	sessions := extractPlannedSessions(plan)

	// Keep a mutable copy so we can remove claimed sessions.
	remaining := make([]PlannedSession, len(sessions))
	copy(remaining, sessions)

	matches := make([]WorkoutMatch, len(workouts))
	for i, w := range workouts {
		matched := MatchWorkoutToSession(w, remaining)
		if matched != nil {
			// Remove the claimed session to prevent double-matching.
			for j := range remaining {
				if remaining[j].Date == matched.Date {
					remaining = append(remaining[:j], remaining[j+1:]...)
					break
				}
			}
		}
		matches[i] = WorkoutMatch{Workout: w, PlannedSession: matched}
	}
	return matches
}

// extractPlannedSessions decodes the plan JSON and returns the non-rest day sessions.
func extractPlannedSessions(plan Plan) []PlannedSession {
	var dayPlans []DayPlan
	if err := json.Unmarshal(plan.Plan, &dayPlans); err != nil {
		return nil
	}
	var sessions []PlannedSession
	for _, dp := range dayPlans {
		if !dp.RestDay && dp.Session != nil {
			sessions = append(sessions, PlannedSession{
				Date:    dp.Date,
				Session: dp.Session,
			})
		}
	}
	return sessions
}

// extractDate returns the YYYY-MM-DD portion of a timestamp string,
// or an empty string if the input is too short.
func extractDate(timestamp string) string {
	if len(timestamp) >= 10 {
		return timestamp[:10]
	}
	return ""
}

// isRunningActivity returns true for sport values encoded as running in
// training.Workout.Sport.
func isRunningActivity(sport string) bool {
	return sport == "running"
}
