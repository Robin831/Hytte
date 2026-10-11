import { useState } from 'react'
import { Clock, LogIn, LogOut } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { getJSON, useWidgetData } from './useWidgetData'
import { useCurrentTime } from '../../hooks/useCurrentTime'
import { toLocalDateString } from '../../utils/formatDate'

interface OpenSession {
  date: string
  start_time: string
}

interface DaySummary {
  net_minutes: number
  standard_minutes: number
}

interface WorkHours {
  session: OpenSession | null
  summary: DaySummary | null
}

async function loadWorkHours(signal: AbortSignal): Promise<WorkHours> {
  const [punch, day] = await Promise.all([
    getJSON<{ session: OpenSession | null }>('/api/workhours/punch-session', signal),
    getJSON<{ summary: DaySummary | null }>(`/api/workhours/day?date=${toLocalDateString()}`, signal),
  ])
  return { session: punch.session ?? null, summary: day.summary ?? null }
}

const hhmm = (d: Date) => `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
const toMinutes = (t: string) => {
  const [h, m] = t.split(':').map(Number)
  return h * 60 + m
}
const duration = (mins: number) => `${Math.floor(mins / 60)}:${String(mins % 60).padStart(2, '0')}`

async function post(url: string, body: unknown): Promise<boolean> {
  const res = await fetch(url, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  return res.ok
}

/** Punch in and out, and how far through today's hours you are. */
export default function WorkHoursWidget() {
  const { t } = useTranslation('dashboard')
  const now = useCurrentTime()
  const { data, loaded, failed, reload } = useWidgetData(loadWorkHours)
  const [busy, setBusy] = useState(false)
  const [punchFailed, setPunchFailed] = useState(false)

  if (!loaded) return null

  const punch = async (run: () => Promise<boolean>) => {
    setBusy(true)
    setPunchFailed(false)
    try {
      if (!(await run())) setPunchFailed(true)
    } catch {
      setPunchFailed(true)
    } finally {
      setBusy(false)
      reload()
    }
  }

  const session = data?.session ?? null
  const punchIn = () => punch(() => post('/api/workhours/punch-in', { date: toLocalDateString(), start_time: hhmm(new Date()) }))
  const punchOut = () =>
    punch(() => {
      const end = hhmm(new Date())
      // An end before the start can only be a shift that ran past midnight.
      return post('/api/workhours/punch-out', {
        end_time: end,
        crosses_midnight: session !== null && toMinutes(end) < toMinutes(session.start_time),
      })
    })

  // Time on the clock since punching in, wrapping once past midnight.
  const elapsed = session ? (now.getHours() * 60 + now.getMinutes() - toMinutes(session.start_time) + 1440) % 1440 : 0
  const net = (data?.summary?.net_minutes ?? 0) + elapsed
  const standard = data?.summary?.standard_minutes ?? 0
  const done = standard > 0 && net >= standard

  const button = 'inline-flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium text-white disabled:opacity-50 cursor-pointer'
  return (
    <Widget title={t('widgets.workHours.title')}>
      {!data ? (
        <p className="text-sm text-gray-400">{t('widgets.workHours.unavailable')}</p>
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="flex min-w-0 items-center gap-2 text-sm">
              <Clock size={16} className={`shrink-0 ${session ? 'text-green-400' : 'text-gray-500'}`} aria-hidden="true" />
              {session ? (
                <span className="text-green-300">
                  {t('widgets.workHours.punchedIn', { time: session.start_time })}
                  <span className="ml-2 tabular-nums text-gray-400">{duration(elapsed)}</span>
                </span>
              ) : (
                <span className="text-gray-300">{done ? t('widgets.workHours.done') : t('widgets.workHours.notPunchedIn')}</span>
              )}
            </p>
            {session ? (
              <button type="button" onClick={punchOut} disabled={busy} className={`${button} bg-amber-700 hover:bg-amber-600`}>
                <LogOut size={16} aria-hidden="true" /> {t('widgets.workHours.punchOut')}
              </button>
            ) : (
              <button type="button" onClick={punchIn} disabled={busy} className={`${button} bg-green-700 hover:bg-green-600`}>
                <LogIn size={16} aria-hidden="true" /> {t('widgets.workHours.punchIn')}
              </button>
            )}
          </div>

          {standard > 0 && (
            <>
              <div
                className="mt-4 h-1.5 w-full rounded-full bg-gray-700"
                role="progressbar"
                aria-valuenow={Math.min(net, standard)}
                aria-valuemin={0}
                aria-valuemax={standard}
                aria-label={t('widgets.workHours.progress')}
              >
                <div
                  className={`h-1.5 rounded-full ${done ? 'bg-green-500' : 'bg-blue-500'}`}
                  style={{ width: `${Math.min(100, (net / standard) * 100)}%` }}
                />
              </div>
              <p className="mt-1.5 flex justify-between text-xs tabular-nums text-gray-400">
                <span>{t('widgets.workHours.worked', { time: duration(net) })}</span>
                <span>{done ? t('widgets.workHours.done') : t('widgets.workHours.remaining', { time: duration(standard - net) })}</span>
              </p>
            </>
          )}
        </>
      )}
      {(punchFailed || (failed && data)) && (
        <p role="alert" className="mt-2 text-xs text-red-300">{t('widgets.workHours.error')}</p>
      )}
      <WidgetLink to="/workhours">{t('widgets.workHours.open')}</WidgetLink>
    </Widget>
  )
}
