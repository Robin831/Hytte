import { useEffect, useMemo, useReducer, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Trophy } from 'lucide-react'
import { useAuth } from '../auth'
import { formatDate } from '../utils/formatDate'

type Mode = 'marathon' | 'blitz'
type Period = 'all' | 'week'

interface LeaderboardEntry {
  user_id: number
  name: string
  avatar_emoji: string
  is_parent: boolean
  score: number | null
  session_id: number | null
  achieved_at: string | null
  rank: number | null
}

interface LeaderboardResponse {
  mode: Mode
  period: Period
  entries: LeaderboardEntry[]
}

type FetchState = { loading: boolean; data: LeaderboardResponse | null; error: string }
type FetchAction =
  | { type: 'start' }
  | { type: 'success'; data: LeaderboardResponse }
  | { type: 'error'; message: string }

function fetchReducer(_state: FetchState, action: FetchAction): FetchState {
  switch (action.type) {
    case 'start': return { loading: true, error: '', data: null }
    case 'success': return { loading: false, error: '', data: action.data }
    case 'error': return { loading: false, error: action.message, data: null }
  }
}

function formatMarathonScore(ms: number): string {
  const totalSeconds = Math.max(0, ms) / 1000
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = Math.floor(totalSeconds - minutes * 60)
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

export default function MathLeaderboard() {
  const { t } = useTranslation('regnemester')
  const { user } = useAuth()

  const [mode, setMode] = useState<Mode>('marathon')
  const [period, setPeriod] = useState<Period>('all')
  const [{ loading, data, error }, dispatch] = useReducer(fetchReducer, { loading: true, data: null, error: '' })

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'start' })
    fetch(`/api/math/leaderboard?mode=${mode}&period=${period}`, {
      credentials: 'include',
      signal: controller.signal,
    })
      .then(res => {
        if (!res.ok) throw new Error('fetch failed')
        return res.json() as Promise<LeaderboardResponse>
      })
      .then(json => {
        if (!controller.signal.aborted) dispatch({ type: 'success', data: json })
      })
      .catch(err => {
        if (controller.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
        dispatch({ type: 'error', message: t('leaderboard.errorLoad') })
      })
    return () => { controller.abort() }
  }, [mode, period, t])

  const MODES: { key: Mode; label: string }[] = useMemo(() => ([
    { key: 'marathon', label: t('leaderboard.modeMarathon') },
    { key: 'blitz', label: t('leaderboard.modeBlitz') },
  ]), [t])

  const PERIODS: { key: Period; label: string }[] = useMemo(() => ([
    { key: 'all', label: t('leaderboard.periodAll') },
    { key: 'week', label: t('leaderboard.periodWeek') },
  ]), [t])

  const renderScore = (score: number | null): string => {
    if (score == null) return '—'
    if (mode === 'marathon') return formatMarathonScore(score)
    return score.toLocaleString()
  }

  return (
    <div className="max-w-3xl mx-auto p-4 sm:p-6 space-y-5">
      <div className="flex items-center gap-3">
        <Link
          to="/math"
          aria-label={t('back')}
          className="text-gray-400 hover:text-white transition-colors"
        >
          <ArrowLeft size={20} />
        </Link>
        <Trophy size={24} className="text-yellow-400 shrink-0" />
        <h1 className="text-2xl sm:text-3xl font-bold text-white">
          {t('leaderboard.title')}
        </h1>
      </div>

      <div className="flex gap-1 bg-gray-800/60 rounded-lg border border-gray-700 p-1" role="tablist" aria-label={t('leaderboard.modeTabsLabel')}>
        {MODES.map(({ key, label }) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={mode === key}
            onClick={() => setMode(key)}
            className={`flex-1 py-2 px-3 rounded-md text-sm font-medium transition-colors ${
              mode === key
                ? 'bg-blue-500/20 text-blue-300 border border-blue-500/30'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      <div className="flex gap-1 bg-gray-800/60 rounded-lg border border-gray-700 p-1" role="tablist" aria-label={t('leaderboard.periodTabsLabel')}>
        {PERIODS.map(({ key, label }) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={period === key}
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

      {!loading && error && (
        <div className="rounded border border-red-500/50 bg-red-500/10 px-3 py-2 text-sm text-red-300">
          {error}
        </div>
      )}

      {!loading && !error && data && (
        data.entries.length === 0 ? (
          <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-8 text-center">
            <Trophy size={40} className="text-yellow-400/40 mx-auto mb-3" />
            <p className="text-gray-400">{t('leaderboard.empty')}</p>
          </div>
        ) : (
          <div className="bg-gray-800/60 rounded-xl border border-gray-700 overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-700">
                  <th className="w-14 text-left text-gray-400 font-medium px-3 sm:px-4 py-3 text-xs uppercase tracking-wide">
                    {t('leaderboard.rank')}
                  </th>
                  <th className="text-left text-gray-400 font-medium px-3 sm:px-4 py-3 text-xs uppercase tracking-wide">
                    {t('leaderboard.player')}
                  </th>
                  <th className="text-right text-gray-400 font-medium px-3 sm:px-4 py-3 text-xs uppercase tracking-wide">
                    {t('leaderboard.score')}
                  </th>
                  <th className="hidden sm:table-cell text-right text-gray-400 font-medium px-4 py-3 text-xs uppercase tracking-wide">
                    {t('leaderboard.when')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.entries.map(entry => {
                  const isMe = user?.id === entry.user_id
                  return (
                    <tr
                      key={entry.user_id}
                      className={`border-b border-gray-700/50 last:border-0 ${
                        isMe ? 'bg-yellow-500/10' : 'hover:bg-gray-700/30'
                      }`}
                    >
                      <td className="px-3 sm:px-4 py-3 tabular-nums text-gray-300">
                        {entry.rank == null ? '—' : `#${entry.rank}`}
                      </td>
                      <td className="px-3 sm:px-4 py-3">
                        <div className="flex items-center gap-2">
                          <span className="text-lg" role="img" aria-hidden="true">
                            {entry.avatar_emoji || '👤'}
                          </span>
                          <span className={`font-medium ${isMe ? 'text-yellow-300' : 'text-white'}`}>
                            {entry.name || t('leaderboard.anonymous')}
                          </span>
                          {entry.is_parent && (
                            <span className="text-xs text-gray-400">
                              {t('leaderboard.parentTag')}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-3 sm:px-4 py-3 text-right text-white font-semibold tabular-nums">
                        {renderScore(entry.score)}
                      </td>
                      <td className="hidden sm:table-cell px-4 py-3 text-right text-gray-400 tabular-nums">
                        {entry.achieved_at
                          ? formatDate(entry.achieved_at, { year: 'numeric', month: 'short', day: 'numeric' })
                          : '—'}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )
      )}
    </div>
  )
}
