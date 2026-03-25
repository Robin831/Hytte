export interface Stage {
  id?: number
  stage_number: number
  speed_kmh: number
  lactate_mmol: number
  heart_rate_bpm: number
  rpe: number | null
  notes: string
}

export interface LactateTest {
  id: number
  date: string
  comment: string
  protocol_type: string
  warmup_duration_min: number
  stage_duration_min: number
  start_speed_kmh: number
  speed_increment_kmh: number
  workout_id?: number
  stages: Stage[]
  created_at: string
  updated_at: string
}

export interface ThresholdResult {
  method: string
  speed_kmh: number
  lactate_mmol: number
  heart_rate_bpm: number
  valid: boolean
  reason?: string
}

export interface TrainingZone {
  zone: number
  name: string
  description: string
  min_speed_kmh: number
  max_speed_kmh: number
  min_hr: number
  max_hr: number
  lactate_from: number
  lactate_to: number
}

export interface ZonesResult {
  system: string
  threshold_speed_kmh: number
  threshold_hr: number
  max_hr?: number
  zones: TrainingZone[]
}

export interface RacePrediction {
  name: string
  distance_km: number
  time_seconds: number
  time_formatted: string
  pace_min_km: string
  speed_kmh: number
}

export interface TrafficLight {
  stage_number: number
  speed_kmh: number
  lactate_mmol: number
  light: 'green' | 'yellow' | 'red'
  label: string
}

export interface Analysis {
  thresholds: ThresholdResult[]
  zones: ZonesResult[]
  predictions: RacePrediction[]
  traffic_lights: TrafficLight[]
  method_used: string
}
