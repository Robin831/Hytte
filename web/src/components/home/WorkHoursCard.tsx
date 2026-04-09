import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import { Clock, LogIn } from 'lucide-react'

interface OpenSession {
  id: number
  date: string
  start_time: string
  punched_at: string
}

interface DaySummary {
  net_minutes: number
  standard_minutes: number
  balance_minutes: number
}

export default function WorkHoursCard() {
  const { t } = useTranslation('today')
  const { user } = useAuth()
  const [openSession, setOpenSession] = useState<OpenSession | null>(null)
  const [summary, setSummary] = useState<DaySummary | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [punchingIn, setPunchingIn] = useState(false)

  const fetchData = useCallback((signal?: AbortSignal) => {
    const today = new Date().toISOString().slice(0, 10)

    Promise.all([
      fetch('/api/workhours/punch-session', { credentials: 'include', signal }),
      fetch(`/api/workhours/day?date=${today}`, { credentials: 'include', signal }),
    ])
      .then(async ([punchRes, dayRes]) => {
        if (signal?.aborted) return
        if (!punchRes.ok || !dayRes.ok) throw new Error('Failed to fetch')
        const punchData = await punchRes.json()
        const dayData = await dayRes.json()
        if (signal?.aborted) return
        setOpenSession(punchData.session ?? null)
        setSummary(dayData.summary ?? null)
        setError(false)
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(true)
      })
      .finally(() => {
        if (!signal?.aborted) setLoading(false)
      })
  }, [])

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    fetchData(controller.signal)
    return () => { controller.abort() }
  }, [user, fetchData])

  const handlePunchIn = async () => {
    setPunchingIn(true)
    try {
      const now = new Date()
      const hh = String(now.getHours()).padStart(2, '0')
      const mm = String(now.getMinutes()).padStart(2, '0')
      const res = await fetch('/api/workhours/punch-in', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          date: now.toISOString().slice(0, 10),
          start_time: `${hh}:${mm}`,
        }),
      })
      if (!res.ok) throw new Error('Punch-in failed')
      // Refresh data after punch-in
      fetchData()
    } catch {
      setError(true)
    } finally {
      setPunchingIn(false)
    }
  }

  const formatMinutes = (mins: number): string => {
    const h = Math.floor(Math.abs(mins) / 60)
    const m = Math.abs(mins) % 60
    return `${h}:${String(m).padStart(2, '0')}`
  }

  const netMinutes = summary?.net_minutes ?? 0
  const standardMinutes = summary?.standard_minutes ?? 0
  const remaining = Math.max(0, standardMinutes - netMinutes)
  const isDone = standardMinutes > 0 && netMinutes >= standardMinutes

  return (
    <div className="bg-gray-800 rounded-xl p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xs uppercase tracking-wide text-gray-500">
          {t('widgets.workHoursTitle')}
        </h2>
        <Link to="/workhours" className="text-xs text-gray-500 hover:text-gray-400" aria-label={t('viewMore')}>
          →
        </Link>
      </div>

      {loading && (
        <div className="space-y-3" role="status" aria-live="polite">
          <span className="sr-only">{t('workHours.loading')}</span>
          <div className="h-4 bg-gray-700 rounded animate-pulse w-3/4" />
          <div className="h-4 bg-gray-700 rounded animate-pulse w-1/2" />
        </div>
      )}

      {error && !loading && (
        <p className="text-red-400 text-sm">{t('unavailable')}</p>
      )}

      {!loading && !error && (
        <div className="space-y-3">
          {/* Status line */}
          {openSession ? (
            <div className="flex items-center gap-2 text-sm">
              <Clock size={16} className="text-green-400 animate-pulse" />
              <span className="text-green-400">{t('workHours.punchedIn')}</span>
              <span className="text-gray-500 tabular-nums">{openSession.start_time}</span>
            </div>
          ) : (
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-sm">
                <Clock size={16} className="text-gray-500" />
                <span className="text-gray-400">
                  {isDone ? t('workHours.done') : t('workHours.notStarted')}
                </span>
              </div>
              {!isDone && (
                <button
                  onClick={handlePunchIn}
                  disabled={punchingIn}
                  className="flex items-center gap-1.5 text-xs text-blue-400 hover:text-blue-300 disabled:opacity-50"
                  aria-label={t('workHours.punchedIn')}
                >
                  <LogIn size={14} />
                </button>
              )}
            </div>
          )}

          {/* Hours summary */}
          {summary && standardMinutes > 0 && (
            <>
              <div className="w-full bg-gray-700 rounded-full h-1.5">
                <div
                  className={`h-1.5 rounded-full ${isDone ? 'bg-green-500' : 'bg-blue-500'}`}
                  style={{ width: `${Math.min(100, (netMinutes / standardMinutes) * 100)}%` }}
                />
              </div>
              <div className="flex justify-between text-xs text-gray-400 tabular-nums">
                <span>{formatMinutes(netMinutes)}</span>
                {isDone ? (
                  <span className="text-green-400">{t('workHours.done')}</span>
                ) : (
                  <span>{formatMinutes(remaining)} {t('workHours.remaining')}</span>
                )}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
