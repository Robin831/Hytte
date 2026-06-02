// Shared types for the math game modes (Marathon, Blitz, and future modes).
// These were previously re-declared independently in MathMarathon.tsx and
// MathBlitz.tsx and had begun to drift; they now live here as the single
// canonical source consumed by every mode page and by useMathSession.
import type { UnlockedAchievement } from '../../components/math/UnlockedAchievements'

export type { UnlockedAchievement }

export type MathMode = 'marathon' | 'blitz'

export type Op = '*' | '/'

export interface Fact {
  a: number
  b: number
  op: Op
  expected: number
}

// FinishSummary is the canonical shape returned by the finish endpoint. It is
// the superset of what the modes use: best_streak is only meaningful for Blitz
// and is absent from Marathon responses, so it is optional here.
export interface FinishSummary {
  session_id: number
  mode: string
  started_at: string
  ended_at: string
  duration_ms: number
  total_correct: number
  total_wrong: number
  score_num: number
  best_streak?: number
}

export interface FinishResponse {
  summary: FinishSummary
  unlocked_achievements?: UnlockedAchievement[]
  leaderboard_rank?: number | null
}

// Phase is the session state machine shared by all modes.
export type Phase = 'idle' | 'starting' | 'playing' | 'finishing' | 'done' | 'error'

// SessionStartResponse is the body returned by POST /api/math/sessions. Blitz
// includes the first question inline; Marathon generates its facts client-side
// and so leaves first_question undefined.
export interface SessionStartResponse {
  session_id: number
  first_question?: Fact
}
