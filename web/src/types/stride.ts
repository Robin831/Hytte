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
