import { useEffect, useReducer } from 'react'
import { Clock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toLocalDateString } from '../../utils/formatDate'

interface DaySummary {
  hours_worked: number
  net_minutes: number
  net_hours: number
}

interface DayResponse {
  summary: DaySummary
}

interface PunchResponse {
  session: { start_time: string } | null
}

interface PrefsResponse {
  preferences?: Record<string, string>
}

interface State {
  loading: boolean
  error: boolean
  summary: DaySummary | null
  punched: boolean
  targetMinutes: number
}

type Action =
  | { type: 'start' }
  | { type: 'day'; summary: DaySummary }
  | { type: 'punch'; active: boolean }
  | { type: 'prefs'; targetMinutes: number }
  | { type: 'error' }

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'start': return { ...state, loading: true, error: false }
    case 'day': return { ...state, loading: false, summary: action.summary }
    case 'punch': return { ...state, punched: action.active }
    case 'prefs': return { ...state, targetMinutes: action.targetMinutes }
    case 'error': return { ...state, loading: false, error: true }
  }
}

const DEFAULT_TARGET_MINUTES = 450 // 7h 30m standard Norwegian workday

function formatHoursMinutes(totalMinutes: number): string {
  const h = Math.floor(totalMinutes / 60)
  const m = totalMinutes % 60
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

export default function WorkHoursWidget() {
  const { t } = useTranslation('today')
  const [{ loading, summary, punched, targetMinutes }, dispatch] = useReducer(reducer, {
    loading: true,
    error: false,
    summary: null,
    punched: false,
    targetMinutes: DEFAULT_TARGET_MINUTES,
  })

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'start' })

    const today = toLocalDateString()
    fetch(`/api/workhours/day?date=${today}`, { credentials: 'include', signal: controller.signal })
      .then((r) => (r.ok ? (r.json() as Promise<DayResponse>) : Promise.reject()))
      .then((d) => dispatch({ type: 'day', summary: d.summary }))
      .catch(() => { if (!controller.signal.aborted) dispatch({ type: 'error' }) })

    fetch('/api/workhours/punch-session', { credentials: 'include', signal: controller.signal })
      .then((r) => (r.ok ? (r.json() as Promise<PunchResponse>) : Promise.reject()))
      .then((d) => dispatch({ type: 'punch', active: d.session !== null }))
      .catch(() => {})

    fetch('/api/settings/preferences', { credentials: 'include', signal: controller.signal })
      .then((r) => (r.ok ? (r.json() as Promise<PrefsResponse>) : Promise.reject()))
      .then((d) => {
        const val = d.preferences?.work_hours_standard_day
        if (val) {
          const mins = parseInt(val, 10)
          if (!isNaN(mins) && mins > 0) dispatch({ type: 'prefs', targetMinutes: mins })
        }
      })
      .catch(() => {})

    return () => controller.abort()
  }, [])

  if (loading && !summary) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Clock size={16} className="shrink-0" />
        <span>{t('workHours.loading')}</span>
      </div>
    )
  }

  if (!summary) return null

  const worked = formatHoursMinutes(summary.net_minutes)
  const remaining = Math.max(0, targetMinutes - summary.net_minutes)

  return (
    <div className="flex items-center gap-2 text-sm">
      <Clock size={16} className="text-gray-400 shrink-0" />
      <span className="text-gray-300">{worked}</span>
      {punched && (
        <span className="inline-block w-1.5 h-1.5 rounded-full bg-green-400 shrink-0" title={t('workHours.punchedIn')} />
      )}
      {remaining > 0 ? (
        <span className="text-gray-500">
          {formatHoursMinutes(remaining)} {t('workHours.remaining')}
        </span>
      ) : (
        <span className="text-green-400">{t('workHours.done')}</span>
      )}
    </div>
  )
}
