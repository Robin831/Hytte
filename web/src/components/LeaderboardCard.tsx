import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import { formatNumber } from '../utils/formatDate'
import { MEDAL, LeaderboardResponse } from '../types/leaderboard'

export default function LeaderboardCard() {
  const { t } = useTranslation('common')
  const { user } = useAuth()
  const [leaderboard, setLeaderboard] = useState<LeaderboardResponse | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/stars/leaderboard?period=weekly', { credentials: 'include' })
      .then(res => (res.ok ? res.json() : null))
      .then(data => setLeaderboard(data))
      .catch(() => setLeaderboard(null))
      .finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <div className="h-32 rounded-xl bg-gray-800 animate-pulse" />
  }

  if (!leaderboard || !leaderboard.leaderboard_visible) {
    return null
  }

  const top3 = leaderboard.entries.slice(0, 3)

  return (
    <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">
          {t('stars.leaderboard.title')}
        </h2>
        <Link
          to="/stars/leaderboard"
          className="text-xs text-blue-400 hover:text-blue-300 transition-colors"
        >
          {t('stars.leaderboard.viewFull')}
        </Link>
      </div>

      {top3.length === 0 ? (
        <p className="text-center text-sm text-gray-500 py-4">
          {t('stars.leaderboard.noEntries')}
        </p>
      ) : (
        <div className="flex justify-center gap-3">
          {top3.map((entry, index) => {
            const isCurrentUser = user?.id === entry.user_id
            const isParent = entry.avatar_emoji === ''
            return (
              <div
                key={entry.user_id}
                className={`flex flex-col items-center gap-1 flex-1 rounded-lg p-3 ${
                  isCurrentUser
                    ? 'bg-yellow-500/10 border border-yellow-500/30'
                    : 'bg-gray-700/30'
                }`}
              >
                <span className="text-2xl" role="img" aria-hidden="true">
                  {MEDAL[index] ?? `#${entry.rank}`}
                </span>
                <span className="text-xl" role="img" aria-hidden="true">
                  {entry.avatar_emoji || '👤'}
                </span>
                <p
                  className={`text-xs font-semibold text-center leading-tight ${
                    isCurrentUser ? 'text-yellow-300' : 'text-white'
                  }`}
                >
                  {entry.nickname}
                </p>
                {isParent && (
                  <p className="text-xs text-gray-400 leading-tight">
                    {t('stars.leaderboard.parent')}
                  </p>
                )}
                <p className="text-yellow-400 text-sm font-bold">
                  {formatNumber(entry.stars)} ⭐
                </p>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
