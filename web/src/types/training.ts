export interface Workout {
  id: number
  user_id: number
  sport: string
  title: string
  started_at: string
  duration_seconds: number
  distance_meters: number
  avg_heart_rate: number
  max_heart_rate: number
  avg_pace_sec_per_km: number
  avg_cadence: number
  calories: number
  ascent_meters: number
  descent_meters: number
  fit_file_hash: string
  analysis_status: string
  title_source: string
  created_at: string
  laps?: Lap[]
  tags?: string[]
  samples?: Samples
}

export interface Lap {
  id: number
  workout_id: number
  lap_number: number
  start_offset_ms: number
  duration_seconds: number
  distance_meters: number
  avg_heart_rate: number
  max_heart_rate: number
  avg_pace_sec_per_km: number
  avg_cadence: number
}

export interface Sample {
  t: number
  hr?: number
  spd?: number
  cad?: number
  alt?: number
  dist?: number
}

export interface Samples {
  points: Sample[]
}

export interface ComparisonResult {
  workout_a: WorkoutSummary
  workout_b: WorkoutSummary
  compatible: boolean
  reason?: string
  lap_deltas?: LapDelta[]
  summary?: ComparisonSummary
}

export interface WorkoutSummary {
  id: number
  title: string
  started_at: string
  sport: string
}

export interface LapDelta {
  lap_number: number
  lap_number_a: number
  lap_number_b: number
  duration_diff_seconds: number
  avg_hr_a: number
  avg_hr_b: number
  hr_delta: number
  pace_a_sec_per_km: number
  pace_b_sec_per_km: number
  pace_delta_sec_per_km: number
}

export interface ComparisonSummary {
  avg_hr_delta: number
  avg_pace_delta: number
  verdict: string
}

export interface ProgressionGroup {
  tag: string
  sport: string
  lap_count: number
  workouts: ProgressionPoint[]
}

export interface ProgressionPoint {
  workout_id: number
  date: string
  avg_hr: number
  avg_pace_sec_per_km: number
  recovery_hr?: number
}

export interface WeeklySummary {
  week_start: string
  total_duration_seconds: number
  total_distance_meters: number
  workout_count: number
  avg_heart_rate: number
}

export interface TrendAnalysis {
  fitness_direction: string
  comparison_to_recent: string
  notable_changes: string[]
}

export interface TrainingInsights {
  effort_summary: string
  pacing_analysis: string
  hr_zones: string
  threshold_context?: string
  observations: string[]
  suggestions: string[]
  trend_analysis?: TrendAnalysis
}

export interface CachedInsights extends TrainingInsights {
  model: string
  created_at: string
  cached: boolean
}

export interface ZoneDistribution {
  zone: number
  name: string
  min_hr: number
  max_hr: number
  duration_seconds: number
  percentage: number
}

export interface ComparisonAnalysis {
  summary: string
  strengths: string[]
  weaknesses: string[]
  observations: string[]
}

export interface CachedComparisonAnalysis extends ComparisonAnalysis {
  workout_id_a: number
  workout_id_b: number
  model: string
  created_at: string
  cached: boolean
}

export interface ComparisonAnalysisSummary {
  id: number
  workout_id_a: number
  workout_id_b: number
  model: string
  created_at: string
  summary: string
}

export interface WorkoutAnalysis {
  id: number
  user_id: number
  workout_id: number
  analysis_type: string
  model: string
  response_json?: string
  tags: string
  summary: string
  title: string
  created_at: string
}
