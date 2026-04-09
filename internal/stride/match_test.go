package stride

import (
	"encoding/json"
	"testing"

	"github.com/Robin831/Hytte/internal/training"
)

// makeSessions builds a []PlannedSession from date strings for test convenience.
func makeSessions(dates ...string) []PlannedSession {
	sessions := make([]PlannedSession, len(dates))
	for i, d := range dates {
		sessions[i] = PlannedSession{
			Date:    d,
			Session: &Session{MainSet: "threshold intervals", Description: "test session"},
		}
	}
	return sessions
}

// makeWorkout creates a minimal Workout with the given StartedAt and Sport.
func makeWorkout(startedAt, sport string) training.Workout {
	return training.Workout{
		ID:        1,
		Sport:     sport,
		StartedAt: startedAt,
	}
}

func TestMatchWorkoutToSession_ExactDate(t *testing.T) {
	sessions := makeSessions("2026-04-07", "2026-04-08", "2026-04-10")
	w := makeWorkout("2026-04-08T07:30:00Z", "running")

	got := MatchWorkoutToSession(w, sessions)
	if got == nil {
		t.Fatal("expected a match, got nil")
	}
	if got.Date != "2026-04-08" {
		t.Errorf("expected date 2026-04-08, got %s", got.Date)
	}
}

func TestMatchWorkoutToSession_FuzzyMinus1Day(t *testing.T) {
	// Workout on Tuesday, session on Wednesday — fuzzy ±1 day, running sport.
	sessions := makeSessions("2026-04-08")
	w := makeWorkout("2026-04-07T19:00:00Z", "running")

	got := MatchWorkoutToSession(w, sessions)
	if got == nil {
		t.Fatal("expected fuzzy match, got nil")
	}
	if got.Date != "2026-04-08" {
		t.Errorf("expected date 2026-04-08, got %s", got.Date)
	}
}

func TestMatchWorkoutToSession_FuzzyPlus1Day(t *testing.T) {
	// Workout on Thursday, session on Wednesday — fuzzy ±1 day.
	sessions := makeSessions("2026-04-08")
	w := makeWorkout("2026-04-09T06:00:00Z", "running")

	got := MatchWorkoutToSession(w, sessions)
	if got == nil {
		t.Fatal("expected fuzzy match, got nil")
	}
}

func TestMatchWorkoutToSession_FuzzyWrongSport(t *testing.T) {
	// Same ±1 day window but cycling does not match a running session.
	sessions := makeSessions("2026-04-08")
	w := makeWorkout("2026-04-07T10:00:00Z", "cycling")

	got := MatchWorkoutToSession(w, sessions)
	if got != nil {
		t.Errorf("expected no match for cycling, got %+v", got)
	}
}

func TestMatchWorkoutToSession_TooFarAway(t *testing.T) {
	// Workout is 2 days away — should not match even for running.
	sessions := makeSessions("2026-04-08")
	w := makeWorkout("2026-04-06T08:00:00Z", "running")

	got := MatchWorkoutToSession(w, sessions)
	if got != nil {
		t.Errorf("expected no match (2 days away), got %+v", got)
	}
}

func TestMatchWorkoutToSession_NoSessions(t *testing.T) {
	w := makeWorkout("2026-04-08T08:00:00Z", "running")
	got := MatchWorkoutToSession(w, nil)
	if got != nil {
		t.Errorf("expected nil for empty sessions, got %+v", got)
	}
}

func TestMatchWorkoutToSession_EmptyStartedAt(t *testing.T) {
	sessions := makeSessions("2026-04-08")
	w := makeWorkout("", "running")
	got := MatchWorkoutToSession(w, sessions)
	if got != nil {
		t.Errorf("expected nil for empty StartedAt, got %+v", got)
	}
}

func TestMatchWorkoutToSession_TrailRunning(t *testing.T) {
	// Trail runs are represented as Sport "running" with SubSport "trail".
	sessions := makeSessions("2026-04-08")
	w := training.Workout{
		StartedAt: "2026-04-07T09:00:00Z",
		Sport:     "running",
		SubSport:  "trail",
	}

	got := MatchWorkoutToSession(w, sessions)
	if got == nil {
		t.Fatal("expected fuzzy match for running trail workout, got nil")
	}
}

// makePlan creates a Plan with the given DayPlans serialised as JSON.
func makePlan(days []DayPlan) Plan {
	b, _ := json.Marshal(days)
	return Plan{Plan: b}
}

func TestMatchAllWorkouts_ExactMatches(t *testing.T) {
	days := []DayPlan{
		{Date: "2026-04-07", RestDay: true},
		{Date: "2026-04-08", RestDay: false, Session: &Session{MainSet: "threshold"}},
		{Date: "2026-04-09", RestDay: true},
		{Date: "2026-04-10", RestDay: false, Session: &Session{MainSet: "easy run"}},
	}
	plan := makePlan(days)

	workouts := []training.Workout{
		makeWorkout("2026-04-08T07:00:00Z", "running"),
		makeWorkout("2026-04-10T07:00:00Z", "running"),
	}

	matches := MatchAllWorkouts(workouts, plan)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].PlannedSession == nil || matches[0].PlannedSession.Date != "2026-04-08" {
		t.Errorf("match[0]: expected session date 2026-04-08, got %v", matches[0].PlannedSession)
	}
	if matches[1].PlannedSession == nil || matches[1].PlannedSession.Date != "2026-04-10" {
		t.Errorf("match[1]: expected session date 2026-04-10, got %v", matches[1].PlannedSession)
	}
}

func TestMatchAllWorkouts_BonusWorkout(t *testing.T) {
	days := []DayPlan{
		{Date: "2026-04-08", RestDay: false, Session: &Session{MainSet: "threshold"}},
	}
	plan := makePlan(days)

	workouts := []training.Workout{
		makeWorkout("2026-04-08T07:00:00Z", "running"), // matches
		makeWorkout("2026-04-11T07:00:00Z", "running"), // bonus — no session
	}

	matches := MatchAllWorkouts(workouts, plan)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].PlannedSession == nil {
		t.Error("match[0] should have a planned session")
	}
	if matches[1].PlannedSession != nil {
		t.Errorf("match[1] should be nil (bonus workout), got %v", matches[1].PlannedSession)
	}
}

func TestMatchAllWorkouts_NoClaiming(t *testing.T) {
	// Two workouts close to the same session date: first should claim, second should not.
	days := []DayPlan{
		{Date: "2026-04-08", RestDay: false, Session: &Session{MainSet: "threshold"}},
	}
	plan := makePlan(days)

	workouts := []training.Workout{
		makeWorkout("2026-04-08T07:00:00Z", "running"), // exact match — claims session
		makeWorkout("2026-04-08T18:00:00Z", "running"), // same day — session already claimed
	}

	matches := MatchAllWorkouts(workouts, plan)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].PlannedSession == nil {
		t.Error("match[0] should have a planned session")
	}
	if matches[1].PlannedSession != nil {
		t.Errorf("match[1] should be nil (session already claimed), got %v", matches[1].PlannedSession)
	}
}

func TestMatchAllWorkouts_EmptyPlan(t *testing.T) {
	plan := makePlan([]DayPlan{})
	workouts := []training.Workout{
		makeWorkout("2026-04-08T07:00:00Z", "running"),
	}
	matches := MatchAllWorkouts(workouts, plan)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].PlannedSession != nil {
		t.Errorf("expected nil planned session for empty plan, got %v", matches[0].PlannedSession)
	}
}

// --- PlannedSessionForDate ---

func TestPlannedSessionForDate_RestDay(t *testing.T) {
	days := []DayPlan{
		{Date: "2026-04-07", RestDay: true},
		{Date: "2026-04-08", RestDay: false, Session: &Session{MainSet: "threshold"}},
	}
	plan := makePlan(days)

	session, isRestDay := PlannedSessionForDate(plan, "2026-04-07")
	if !isRestDay {
		t.Error("expected rest day, got false")
	}
	if session != nil {
		t.Errorf("expected nil session for rest day, got %+v", session)
	}
}

func TestPlannedSessionForDate_PlannedSession(t *testing.T) {
	days := []DayPlan{
		{Date: "2026-04-07", RestDay: true},
		{Date: "2026-04-08", RestDay: false, Session: &Session{MainSet: "threshold", Description: "Threshold run"}},
	}
	plan := makePlan(days)

	session, isRestDay := PlannedSessionForDate(plan, "2026-04-08")
	if isRestDay {
		t.Error("expected non-rest day, got true")
	}
	if session == nil {
		t.Fatal("expected session, got nil")
	}
	if session.Session.MainSet != "threshold" {
		t.Errorf("expected main_set 'threshold', got %q", session.Session.MainSet)
	}
}

func TestPlannedSessionForDate_DateNotInPlan(t *testing.T) {
	days := []DayPlan{
		{Date: "2026-04-07", RestDay: true},
	}
	plan := makePlan(days)

	session, isRestDay := PlannedSessionForDate(plan, "2026-04-10")
	if isRestDay {
		t.Error("expected false for date not in plan")
	}
	if session != nil {
		t.Errorf("expected nil session for date not in plan, got %+v", session)
	}
}

func TestPlannedSessionForDate_InvalidPlanJSON(t *testing.T) {
	plan := Plan{Plan: json.RawMessage(`invalid`)}
	session, isRestDay := PlannedSessionForDate(plan, "2026-04-07")
	if isRestDay {
		t.Error("expected false for invalid JSON")
	}
	if session != nil {
		t.Error("expected nil session for invalid JSON")
	}
}

func TestMatchAllWorkouts_EmptyWorkouts(t *testing.T) {
	days := []DayPlan{
		{Date: "2026-04-08", RestDay: false, Session: &Session{MainSet: "threshold"}},
	}
	plan := makePlan(days)
	matches := MatchAllWorkouts(nil, plan)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for nil workouts, got %d", len(matches))
	}
}
