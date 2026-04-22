import { useEffect, useState } from 'react'
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

async function fetchRank(mode: Mode, period: 'all' | 'week', userID: number): Promise<number | null> {
  const res = await fetch(`/api/math/leaderboard?mode=${mode}&period=${period}`, { credentials: 'include' })
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
  const [ranks, setRanks] = useState<Ranks | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!user || sessionId == null) return
    const controller = new AbortController()
    setLoading(true)
    Promise.all([fetchRank(mode, 'all', user.id), fetchRank(mode, 'week', user.id)])
      .then(([all, week]) => {
        if (controller.signal.aborted) return
        setRanks({ all, week })
      })
      .catch(() => { /* non-critical display */ })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
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
