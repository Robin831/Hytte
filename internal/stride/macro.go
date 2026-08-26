package stride

import "time"

// Macro plan phases. A macro week's phase drives what the weekly generator is
// allowed to prescribe for that week.
const (
	MacroPhaseBase     = "base"
	MacroPhaseBuild    = "build"
	MacroPhasePeak     = "peak"
	MacroPhaseTaper    = "taper"
	MacroPhaseRace     = "race"
	MacroPhaseRecovery = "recovery"
)

// Macro week load levels. Independent of phase: a build-phase week can still
// be a deload.
const (
	LoadLevelDeload = "deload"
	LoadLevelNormal = "normal"
	LoadLevelBuild  = "build"
	LoadLevelPeak   = "peak"
	LoadLevelTaper  = "taper"
)

// Macro plan statuses. A plan stays 'active' until a newer block replaces it,
// at which point it becomes 'superseded'. Staleness (a race added, changed or
// removed inside the horizon) is tracked separately in StaleReason so the UI
// can show a banner without the plan losing its active role.
const (
	MacroPlanStatusActive     = "active"
	MacroPlanStatusSuperseded = "superseded"
)

// Macro week statuses. 'planned' until the weekly job turns it into a
// stride_plans row ('materialised'), or the week passes unused ('skipped').
const (
	MacroWeekStatusPlanned      = "planned"
	MacroWeekStatusMaterialised = "materialised"
	MacroWeekStatusSkipped      = "skipped"
)

// How a macro plan came to exist: the Monday cron ('scheduled'), the user
// pressing Regenerate ('manual'), or the horizon running out and a fresh
// 26-week block being appended ('extension').
const (
	MacroGeneratedByScheduled = "scheduled"
	MacroGeneratedByManual    = "manual"
	MacroGeneratedByExtension = "extension"
)

// Where a goal revision came from: the block's first goal ('initial'), the
// weekly job auto-applying drift ('weekly'), or the user editing it ('manual').
const (
	GoalRevisionSourceInitial = "initial"
	GoalRevisionSourceWeekly  = "weekly"
	GoalRevisionSourceManual  = "manual"
)

// Stale reasons recorded on a macro plan. Empty means not stale.
const (
	// MacroStaleRacesChanged marks a plan whose horizon no longer matches the
	// race calendar (a race was added, edited or deleted inside it). The plan
	// is never auto-regenerated — the user gets a banner and a Regenerate button.
	MacroStaleRacesChanged = "races_changed"
)

// MacroGoal is the block's objective. Improving half-marathon performance is
// always the main priority, so TargetHMTimeS is set even for blocks with no
// half marathon on the calendar — there it is judged against the race
// prediction model instead of an actual race result.
type MacroGoal struct {
	PrimaryFocus  string `json:"primary_focus"`
	Statement     string `json:"statement"`
	TargetHMTimeS int    `json:"target_hm_time_s"`
	Benchmark     string `json:"benchmark"`
	Rationale     string `json:"rationale"`
	// AnchorRaceID points at the A-priority race the block is built around,
	// or nil for a pure development block with no explicit end test.
	AnchorRaceID *int64 `json:"anchor_race_id"`
}

// Mesocycle is one named segment of the block's periodisation (for example a
// four-week build). Weeks is the segment length; StartWeek is its first Monday.
type Mesocycle struct {
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	StartWeek string `json:"start_week"`
	Weeks     int    `json:"weeks"`
	Focus     string `json:"focus"`
	RaceID    *int64 `json:"race_id"`
}

// KeySession is one session the macro week must contain, optionally pinned to
// a library workout (stride_workouts).
type KeySession struct {
	Type      string `json:"type"`
	Focus     string `json:"focus"`
	LibraryID *int64 `json:"library_id"`
}

// MacroPlan is a long-horizon training block: a goal, a periodisation, and one
// MacroWeek per week between StartWeek and EndWeek (both Mondays).
type MacroPlan struct {
	ID             int64       `json:"id"`
	UserID         int64       `json:"user_id"`
	StartWeek      string      `json:"start_week"`    // YYYY-MM-DD, Monday
	EndWeek        string      `json:"end_week"`      // YYYY-MM-DD, Monday of the last week
	Status         string      `json:"status"`        // active | superseded
	StaleReason    string      `json:"stale_reason"`  // '' when fresh, e.g. races_changed
	Goal           MacroGoal   `json:"goal"`          // encrypted at rest (goal_json)
	Periodisation  []Mesocycle `json:"periodisation"` // encrypted at rest (periodisation_json)
	Prompt         string      `json:"-"`             // encrypted at rest
	Response       string      `json:"-"`             // encrypted at rest
	Model          string      `json:"model"`
	GeneratedBy    string      `json:"generated_by"` // scheduled | manual | extension
	PreviousPlanID *int64      `json:"previous_plan_id"`
	CreatedAt      time.Time   `json:"created_at"`
	Weeks          []MacroWeek `json:"weeks,omitempty"` // populated by GetActiveMacroPlan
}

// MacroWeek is one week of a macro plan — the contract the weekly 7-day
// generator has to honour when it materialises that week.
type MacroWeek struct {
	ID             int64        `json:"id"`
	MacroPlanID    int64        `json:"macro_plan_id"`
	UserID         int64        `json:"user_id"`
	WeekStart      string       `json:"week_start"` // YYYY-MM-DD, Monday
	Seq            int          `json:"seq"`        // 1-based position within the block
	Phase          string       `json:"phase"`
	Mesocycle      string       `json:"mesocycle"`
	LoadLevel      string       `json:"load_level"`
	TargetKm       float64      `json:"target_km"`
	TargetSessions int          `json:"target_sessions"`
	RaceID         *int64       `json:"race_id"`
	KeySessions    []KeySession `json:"key_sessions"` // encrypted at rest (key_sessions_json)
	Intent         string       `json:"intent"`       // encrypted at rest
	Status         string       `json:"status"`       // planned | materialised | skipped
}

// GoalRevision is one append-only entry in a macro plan's goal history.
type GoalRevision struct {
	ID          int64     `json:"id"`
	MacroPlanID int64     `json:"macro_plan_id"`
	UserID      int64     `json:"user_id"`
	WeekStart   string    `json:"week_start"`
	Goal        MacroGoal `json:"goal"`   // encrypted at rest (goal_json)
	Reason      string    `json:"reason"` // encrypted at rest
	Source      string    `json:"source"` // initial | weekly | manual
	CreatedAt   time.Time `json:"created_at"`
}
