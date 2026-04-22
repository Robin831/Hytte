import { useEffect, useReducer } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Trophy } from 'lucide-react'
import { useAuth } from '../../auth'

type Mode = 'marathon' | 'blitz'

interface LeaderboardEntry {
  user_id: number
  rank: number | null
  score: number | null
}

interface LeaderboardResponse {
  entries: LeaderboardEntry[]
}

interface Ranks {
  all: number | null
  week: number | null
}

type RankState = { ranks: Ranks | null; loading: boolean }
type RankAction =
  | { type: 'start' }
  | { type: 'done'; ranks: Ranks }

function rankReducer(_state: RankState, action: RankAction): RankState {
  switch (action.type) {
    case 'start': return { ranks: null, loading: true }
    case 'done': return { ranks: action.ranks, loading: false }
  }
}

async function fetchRank(
  mode: Mode,
  period: 'all' | 'week',
  userID: number,
  signal: AbortSignal,
): Promise<number | null> {
  const res = await fetch(`/api/math/leaderboard?mode=${mode}&period=${period}`, {
    credentials: 'include',
    signal,
  })
  if (!res.ok) return null
  const data = (await res.json()) as LeaderboardResponse
  const me = data.entries.find(e => e.user_id === userID)
  if (!me || me.rank == null) return null
  return me.rank
}

// FinishRank renders the caller's current rank for the given mode across
// both time windows and links to /math/leaderboard. The sessionId prop
// forces a re-fetch each time a new run finishes so the shown rank reflects
// the run just stored server-side.
export function FinishRank({ mode, sessionId }: { mode: Mode; sessionId: number | null }) {
  const { t } = useTranslation('regnemester')
  const { user } = useAuth()
  const [{ ranks, loading }, dispatch] = useReducer(rankReducer, { ranks: null, loading: false })

  useEffect(() => {
    if (!user || sessionId == null) return
    const controller = new AbortController()
    const { signal } = controller
    dispatch({ type: 'start' })
    Promise.all([
      fetchRank(mode, 'all', user.id, signal),
      fetchRank(mode, 'week', user.id, signal),
    ])
      .then(([all, week]) => {
        if (signal.aborted) return
        dispatch({ type: 'done', ranks: { all, week } })
      })
      .catch(err => {
        if (signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
        // Surface unreachable leaderboard as Unranked instead of a permanent
        // loading spinner. Network errors on the finish screen are
        // non-critical — the run itself has already been recorded.
        dispatch({ type: 'done', ranks: { all: null, week: null } })
      })
    return () => { controller.abort() }
  }, [mode, sessionId, user])

  if (!user) return null

  const render = (r: number | null) => (r == null ? t('leaderboard.unranked') : `#${r}`)

  return (
    <div className="rounded-lg border border-gray-700 bg-gray-800 p-4 flex items-center gap-3">
      <Trophy size={20} className="text-yellow-400 shrink-0" />
      <div className="flex-1 min-w-0">
        <div className="text-xs uppercase tracking-wide text-gray-400">{t('leaderboard.yourRank')}</div>
        {loading || !ranks ? (
          <div className="text-gray-300 text-sm">{t('leaderboard.loading')}</div>
        ) : (
          <div className="text-white font-semibold tabular-nums">
            {t('leaderboard.rankSummary', { all: render(ranks.all), week: render(ranks.week) })}
          </div>
        )}
      </div>
      <Link
        to="/math/leaderboard"
        className="text-sm font-medium text-blue-300 hover:text-blue-200 whitespace-nowrap"
      >
        {t('leaderboard.viewLinkShort')}
      </Link>
    </div>
  )
}
