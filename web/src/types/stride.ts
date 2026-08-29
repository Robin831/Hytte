export interface StridePlanSession {
  warmup: string
  main_set: string
  cooldown: string
  strides: string
  target_hr_cap: number
  description: string
}

export interface DayPlan {
  date: string
  rest_day: boolean
  session?: StridePlanSession
}

export interface StrideEvaluation {
  planned_type: string
  actual_type: string
  compliance: 'compliant' | 'partial' | 'missed' | 'bonus' | 'rest_day'
  date?: string
  notes: string
  flags: string[] | null
  adjustments: string
  questions?: string[]
}

export interface StrideEvaluationRecord {
  id: number
  user_id: number
  plan_id: number
  workout_id: number | null
  eval: StrideEvaluation
  created_at: string
  workout_context_summary?: string
}

export interface StridePlan {
  id: number
  user_id: number
  week_start: string
  week_end: string
  phase: string
  plan: DayPlan[]
  model: string
  // Set when the week was materialised from a macro block week
  // (stride_macro_weeks.id); null for plans generated without a macro plan.
  macro_week_id?: number | null
  // The coach's prose on how this week deviates from its macro week target.
  // Empty (or absent, on a legacy plan) when there is nothing to say.
  adjustment_summary?: string
  created_at: string
}

export interface WeekSummary {
  plan_id: number
  week_start: string
  week_end: string
  phase: string
  sessions_planned: number
  sessions_completed: number
  completion_rate: number
  // Per-zone moving-time aggregates from /api/stride/history. May be 0 when the
  // user has no HR zones configured or no workouts in the week have avg_heart_rate.
  easy_seconds?: number
  threshold_seconds?: number
  hard_seconds?: number
  // Optional total distance across the week's workouts (meters). Emitted by
  // /api/stride/history for all pages; kept optional for type safety.
  total_distance_meters?: number
}

// StrideWorkout is one workout-library entry: a reusable session definition
// the weekly plan generator draws from (GET/POST/PUT /api/stride/workouts).
export interface StrideWorkout {
  id: number
  name: string
  workout_type: string
  warmup: string
  main_set: string
  cooldown: string
  strides: string
  target_hr_cap: string
  description: string
  source: string // 'manual' | 'ai'
  rating: number // 0 = unrated, 1..5
  times_used: number
  last_used_at?: string
  is_reference: boolean
  archived: boolean
  blocks: string[]
  created_at: string
}

// ── Macro plan (long-term block) ─────────────────────────────────────────────
// Mirrors the Go types in internal/stride/macro.go. GET /api/stride/macro/current
// answers with MacroPlanView; a user with no active block gets a 404, which the
// page treats as "no macro plan" rather than an error.

// MacroGoal is the block's objective. Improving half-marathon performance is
// always the main priority, so target_hm_time_s is set even for a block with no
// half marathon on the calendar.
export interface MacroGoal {
  primary_focus: string
  statement: string
  target_hm_time_s: number
  benchmark: string
  rationale: string
  // The A-priority race the block is built around, or null for a development
  // block with no explicit end test.
  anchor_race_id: number | null
}

// Mesocycle is one named segment of the block's periodisation.
export interface MacroMesocycle {
  name: string
  phase: string
  start_week: string
  weeks: number
  focus: string
  race_id: number | null
}

// KeySession is one session a macro week must contain, optionally pinned to a
// workout-library entry.
export interface MacroKeySession {
  type: string
  focus: string
  library_id: number | null
}

// MacroWeek is one week of the block — the contract the weekly generator has to
// honour when it materialises that week into a 7-day plan.
export interface MacroWeek {
  id: number
  macro_plan_id: number
  user_id: number
  week_start: string
  seq: number
  phase: string
  mesocycle: string
  load_level: string
  target_km: number
  target_sessions: number
  race_id: number | null
  key_sessions: MacroKeySession[] | null
  intent: string
  status: string
}

export interface MacroPlan {
  id: number
  user_id: number
  start_week: string
  end_week: string
  status: string
  // '' when fresh, e.g. 'races_changed' when the race calendar moved under it.
  stale_reason: string
  goal: MacroGoal
  periodisation: MacroMesocycle[]
  model: string
  generated_by: string
  previous_plan_id: number | null
  created_at: string
}

// GoalRevision is one append-only entry in a block's goal history.
export interface GoalRevision {
  id: number
  macro_plan_id: number
  user_id: number
  week_start: string
  goal: MacroGoal
  reason: string
  source: string
  created_at: string
}

// MacroPlanView is the payload every macro endpoint answers with: the block,
// its week rows, and the goal history behind it. The weeks live at the top
// level, not nested inside plan.
export interface MacroPlanView {
  plan: MacroPlan
  weeks: MacroWeek[]
  current_goal_revision: GoalRevision | null
  revisions: GoalRevision[]
}
