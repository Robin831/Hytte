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
}
