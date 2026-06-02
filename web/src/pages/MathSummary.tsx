import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Timer, Zap, AlertTriangle } from 'lucide-react'
import { useAuth } from '../auth'
import { computeWeakestFacts, findUserRank } from './mathSummaryUtils'
import type { LeaderboardEntry, StatsCell, StatsResponse } from './mathSummaryUtils'

interface MarathonBest {
  duration_ms: number
  total_wrong: number
}

interface BlitzBest {
  score_num: number
  best_streak: number
}

interface LeaderboardResponse {
  entries: LeaderboardEntry[]
}

interface SummaryData {
  marathon: MarathonBest | null
  blitz: BlitzBest | null
  rank: number | null
  weakest: StatsCell[]
}

function formatMarathonTime(ms: number): string {
  const totalSeconds = Math.max(0, ms) / 1000
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = Math.floor(totalSeconds - minutes * 60)
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

function cellProblem(cell: StatsCell): string {
  if (cell.op === '*') return `${cell.a} × ${cell.b}`
  return `${cell.a * cell.b} ÷ ${cell.b}`
}

async function fetchJSON<T>(url: string, signal: AbortSignal): Promise<T> {
  const res = await fetch(url, { credentials: 'include', signal })
  if (!res.ok) throw new Error('fetch failed')
  return res.json() as Promise<T>
}

export default function MathSummary() {
  const { t } = useTranslation('regnemester')
  const { user } = useAuth()

  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [data, setData] = useState<SummaryData | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    const { signal } = controller

    async function load() {
      setLoading(true)
      setError(false)

      const results = await Promise.allSettled([
        fetchJSON<{ best: MarathonBest | null }>('/api/math/marathon/best', signal),
        fetchJSON<{ best: BlitzBest | null }>('/api/math/blitz/best', signal),
        fetchJSON<LeaderboardResponse>('/api/math/leaderboard?mode=blitz&period=all', signal),
        fetchJSON<StatsResponse>('/api/math/stats', signal),
      ])
      if (signal.aborted) return

      // If every call failed, hide the banner entirely; the mode grid still
      // renders below. A single failing call only blanks that one metric.
      if (results.every(r => r.status === 'rejected')) {
        setError(true)
        setLoading(false)
        return
      }

      const marathon = results[0].status === 'fulfilled' ? results[0].value.best : null
      const blitz = results[1].status === 'fulfilled' ? results[1].value.best : null
      const entries = results[2].status === 'fulfilled' ? results[2].value.entries : []
      const stats = results[3].status === 'fulfilled' ? results[3].value : null

      setData({
        marathon,
        blitz,
        rank: findUserRank(entries, user?.id),
        weakest: stats ? computeWeakestFacts(stats, 3) : [],
      })
      setLoading(false)
    }

    load().catch(() => {
      if (!signal.aborted) {
        setError(true)
        setLoading(false)
      }
    })

    return () => { controller.abort() }
  }, [user?.id])

  if (error) return null

  if (loading) {
    return (
      <div
        className="mb-6 rounded-lg border border-gray-700 bg-gray-800/60 p-4 sm:p-5 animate-pulse"
        aria-hidden="true"
      >
        <div className="grid grid-cols-2 gap-3 sm:gap-4 mb-4">
          <div className="h-16 rounded-md bg-gray-700/60" />
          <div className="h-16 rounded-md bg-gray-700/60" />
        </div>
        <div className="h-4 w-24 rounded bg-gray-700/60 mb-2" />
        <div className="flex gap-2">
          <div className="h-7 w-20 rounded-full bg-gray-700/60" />
          <div className="h-7 w-20 rounded-full bg-gray-700/60" />
          <div className="h-7 w-20 rounded-full bg-gray-700/60" />
        </div>
      </div>
    )
  }

  if (!data) return null

  const noPb = t('summary.noPbYet')

  return (
    <section
      aria-labelledby="summary-heading"
      className="mb-6 rounded-lg border border-gray-700 bg-gray-800/60 p-4 sm:p-5"
    >
      <h2 id="summary-heading" className="sr-only">{t('summary.heading')}</h2>

      <div className="grid grid-cols-2 gap-3 sm:gap-4">
        <div className="rounded-md border border-gray-700 bg-gray-800 p-3 sm:p-4">
          <div className="flex items-center gap-2 text-xs uppercase tracking-wide text-gray-400 mb-1">
            <Timer size={16} className="text-blue-400 shrink-0" />
            {t('summary.marathonLabel')}
          </div>
          {data.marathon ? (
            <div className="text-2xl sm:text-3xl font-bold text-white tabular-nums">
              {formatMarathonTime(data.marathon.duration_ms)}
            </div>
          ) : (
            <div className="text-base sm:text-lg font-medium text-gray-500">{noPb}</div>
          )}
        </div>

        <div className="rounded-md border border-gray-700 bg-gray-800 p-3 sm:p-4">
          <div className="flex items-center gap-2 text-xs uppercase tracking-wide text-gray-400 mb-1">
            <Zap size={16} className="text-yellow-400 shrink-0" />
            {t('summary.blitzLabel')}
          </div>
          {data.blitz ? (
            <div className="flex items-baseline gap-2 flex-wrap">
              <span className="text-2xl sm:text-3xl font-bold text-white tabular-nums">
                {data.blitz.score_num.toLocaleString()}
              </span>
              {data.rank != null && (
                <span className="text-sm font-semibold text-yellow-300 tabular-nums">
                  {t('summary.rank', { rank: data.rank })}
                </span>
              )}
            </div>
          ) : (
            <div className="text-base sm:text-lg font-medium text-gray-500">{noPb}</div>
          )}
        </div>
      </div>

      {data.weakest.length > 0 && (
        <div className="mt-4">
          <div className="flex items-center gap-2 text-xs uppercase tracking-wide text-gray-400 mb-2">
            <AlertTriangle size={16} className="text-red-400 shrink-0" />
            {t('summary.weakestHeading')}
          </div>
          <div className="flex flex-wrap gap-2">
            {data.weakest.map(cell => {
              const problem = cellProblem(cell)
              const accuracy = Math.round(cell.accuracy_pct)
              return (
                <Link
                  key={`${cell.op}-${cell.a}-${cell.b}`}
                  to="/math/heatmap"
                  aria-label={t('summary.chipAria', { problem, accuracy })}
                  className="inline-flex items-center gap-1.5 rounded-full border border-red-500/40 bg-red-500/10 px-3 py-1 text-sm font-medium text-red-200 hover:border-red-400 hover:bg-red-500/20 transition-colors tabular-nums"
                >
                  <span>{problem}</span>
                  <span className="text-xs text-red-300/80">{accuracy}%</span>
                </Link>
              )
            })}
          </div>
        </div>
      )}
    </section>
  )
}
