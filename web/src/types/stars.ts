export interface Balance {
  current_balance: number
  total_earned: number
  total_spent: number
  level: number
  xp: number
  title: string
  emoji?: string
}

export interface Transaction {
  id: number
  amount: number
  reason: string
  description: string
  created_at: string
}

export interface TransactionsResponse {
  transactions: Transaction[]
  weekly_stars: number
  weekly_starred_workouts: number
  weekly_distance_meters: number
  weekly_duration_seconds: number
}

export interface StreakEntry {
  current_count: number
  longest_count: number
  last_activity: string
  shield_active: boolean
}

export interface StreaksResponse {
  daily_workout: StreakEntry
  weekly_workout: StreakEntry
}

export interface JourneyWaypoint {
  name: string
  distance_km: number
  description: string
  emoji: string
  crossed?: boolean
}

export interface JourneyTheme {
  key: string
  name: string
  emoji: string
  total_distance_km: number
}

export interface JourneyResponse {
  theme: string
  theme_name: string
  theme_emoji: string
  total_distance_m: number
  total_journey_distance_km: number
  current_waypoint: JourneyWaypoint
  next_waypoint: JourneyWaypoint | null
  progress_in_leg_percent: number
  distance_to_next_km: number
  waypoints: JourneyWaypoint[]
  available_themes: JourneyTheme[]
}
