export interface LeaderboardEntry {
  user_id: number
  nickname: string
  avatar_emoji: string
  is_parent: boolean
  stars: number
  workout_count: number
  streak: number
  rank: number
}

export interface LeaderboardResponse {
  period: string
  generated_at: string
  leaderboard_visible: boolean
  entries: LeaderboardEntry[]
}

export const MEDAL = ['🥇', '🥈', '🥉'] as const
