import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Trophy, ArrowLeft } from 'lucide-react'
import { useAuth } from '../auth'
import { formatNumber } from '../utils/formatDate'
import '../stars.css'

type Period = 'weekly' | 'monthly' | 'alltime'

interface LeaderboardEntry {
  user_id: number
  nickname: string
  avatar_emoji: string
  stars: number
  workout_count: number
  streak: number
  rank: number
}

interface LeaderboardResponse {
  period: string
  generated_at: string
  leaderboard_visible: boolean
  entries: LeaderboardEntry[]
}

const MEDAL = ['🥇', '🥈', '🥉'] as const

export default function StarLeaderboard() {
  const { t } = useTranslation('common')
  const { user } = useAuth()
  const [period, setPeriod] = useState<Period>('weekly')
  const [leaderboard, setLeaderboard] = useState<LeaderboardResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    setError(null)
    fetch(`/api/stars/leaderboard?period=${period}`, { credentials: 'include' })
      .then(res => {
        if (!res.ok) throw new Error('fetch failed')
        return res.json()
      })
      .then((data: LeaderboardResponse) => setLeaderboard(data))
      .catch(() => setError(t('stars.errors.failedToLoad')))
      .finally(() => setLoading(false))
  }, [period, t])

  const PERIODS: { key: Period; label: string }[] = [
    { key: 'weekly', label: t('stars.leaderboard.weekly') },
    { key: 'monthly', label: t('stars.leaderboard.monthly') },
    { key: 'alltime', label: t('stars.leaderboard.allTime') },
  ]

  return (
    <div className="p-6 max-w-2xl mx-auto space-y-6">
      <div className="flex items-center gap-3">
        <Link to="/stars" className="text-gray-400 hover:text-white transition-colors">
          <ArrowLeft size={20} />
        </Link>
        <Trophy size={24} className="text-yellow-400" />
        <h1 className="text-2xl font-semibold text-white">{t('stars.leaderboard.title')}</h1>
      </div>

      {/* Period toggle */}
      <div className="flex gap-1 bg-gray-800/60 rounded-lg border border-gray-700 p-1">
        {PERIODS.map(({ key, label }) => (
          <button
            key={key}
            onClick={() => setPeriod(key)}
            className={`flex-1 py-2 px-3 rounded-md text-sm font-medium transition-colors ${
              period === key
                ? 'bg-yellow-500/20 text-yellow-300 border border-yellow-500/30'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      {loading && (
        <div className="space-y-2">
          {[0, 1, 2].map(i => (
            <div key={i} className="h-14 rounded-lg bg-gray-800 animate-pulse" />
          ))}
        </div>
      )}

      {error && <div className="text-red-400">{error}</div>}

      {!loading && !error && leaderboard && !leaderboard.leaderboard_visible && (
        <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-8 text-center">
          <p className="text-gray-400">{t('stars.leaderboard.hidden')}</p>
        </div>
      )}

      {!loading && !error && leaderboard?.leaderboard_visible && (
        <>
          {leaderboard.entries.length === 0 ? (
            <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-8 text-center">
              <Trophy size={40} className="text-yellow-400/40 mx-auto mb-3" />
              <p className="text-gray-400">{t('stars.leaderboard.noEntries')}</p>
            </div>
          ) : (
            <div className="bg-gray-800/60 rounded-xl border border-gray-700 overflow-hidden">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-700">
                    <th className="w-12 text-left text-gray-400 font-medium px-4 py-3 text-xs uppercase tracking-wide">
                      {t('stars.leaderboard.rank')}
                    </th>
                    <th className="text-left text-gray-400 font-medium px-4 py-3 text-xs uppercase tracking-wide">
                      {t('stars.leaderboard.player')}
                    </th>
                    <th className="text-right text-gray-400 font-medium px-4 py-3 text-xs uppercase tracking-wide">
                      {t('stars.leaderboard.stars')}
                    </th>
                    <th className="hidden sm:table-cell text-right text-gray-400 font-medium px-4 py-3 text-xs uppercase tracking-wide">
                      {t('stars.leaderboard.workouts')}
                    </th>
                    <th className="hidden sm:table-cell text-right text-gray-400 font-medium px-4 py-3 text-xs uppercase tracking-wide">
                      {t('stars.leaderboard.streak')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {leaderboard.entries.map(entry => {
                    const isCurrentUser = user?.id === entry.user_id
                    const isParent = entry.avatar_emoji === ''
                    return (
                      <tr
                        key={entry.user_id}
                        className={`border-b border-gray-700/50 last:border-0 ${
                          isCurrentUser ? 'bg-yellow-500/10' : 'hover:bg-gray-700/30'
                        }`}
                      >
                        <td className="px-4 py-3 text-center">
                          <span className="text-base leading-none">
                            {entry.rank <= 3
                              ? MEDAL[entry.rank - 1]
                              : `#${entry.rank}`}
                          </span>
                          {entry.rank === 1 && (
                            <span className="ml-1" role="img" aria-hidden="true">
                              👑
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-3">
                          <div className="flex items-center gap-2">
                            <span className="text-lg" role="img" aria-hidden="true">
                              {entry.avatar_emoji || '👤'}
                            </span>
                            <span
                              className={`font-medium ${
                                isCurrentUser ? 'text-yellow-300' : 'text-white'
                              }`}
                            >
                              {entry.nickname}
                            </span>
                            {isParent && (
                              <span className="text-xs text-gray-400">
                                {t('stars.leaderboard.parent')}
                              </span>
                            )}
                          </div>
                        </td>
                        <td className="px-4 py-3 text-right text-yellow-400 font-bold">
                          {formatNumber(entry.stars)} ⭐
                        </td>
                        <td className="hidden sm:table-cell px-4 py-3 text-right text-gray-300">
                          {formatNumber(entry.workout_count)}
                        </td>
                        <td className="hidden sm:table-cell px-4 py-3 text-right text-orange-400">
                          {entry.streak > 0 ? `🔥 ${entry.streak}` : '—'}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  )
}
