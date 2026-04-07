import { useState, useEffect, useReducer } from 'react'
import { Bus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTime } from '../../utils/formatDate'

interface Departure {
  line: string
  destination: string
  departure_time: string
  is_realtime: boolean
  delay_minutes: number
}

interface StopDepartures {
  stop_id: string
  stop_name: string
  departures: Departure[]
}

interface DeparturesResponse {
  stops: StopDepartures[]
}

type State = { loading: boolean; error: boolean; data: DeparturesResponse | null }
type Action = { type: 'start' } | { type: 'done'; data: DeparturesResponse } | { type: 'error' }

function reducer(_state: State, action: Action): State {
  switch (action.type) {
    case 'start': return { loading: true, error: false, data: _state.data }
    case 'done': return { loading: false, error: false, data: action.data }
    case 'error': return { loading: false, error: true, data: _state.data }
  }
}

export default function BusDepartureWidget() {
  const { t } = useTranslation('today')
  const [{ loading, data }, dispatch] = useReducer(reducer, { loading: true, error: false, data: null })
  const [tick, setTick] = useState(() => Date.now())

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'start' })
    fetch('/api/transit/departures', { credentials: 'include', signal: controller.signal })
      .then((r) => (r.ok ? (r.json() as Promise<DeparturesResponse>) : Promise.reject()))
      .then((d) => dispatch({ type: 'done', data: d }))
      .catch(() => { if (!controller.signal.aborted) dispatch({ type: 'error' }) })
    return () => controller.abort()
  }, [])

  // Re-render every 30s to keep relative times fresh
  useEffect(() => {
    const id = setInterval(() => setTick(Date.now()), 30_000)
    return () => clearInterval(id)
  }, [])

  if (loading && !data) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Bus size={16} className="shrink-0" />
        <span>{t('bus.loading')}</span>
      </div>
    )
  }

  // Find the next departure across all stops
  const allDepartures = data?.stops?.flatMap((s) => s.departures) ?? []
  const nowMs = tick
  const next = allDepartures
    .filter((d) => new Date(d.departure_time).getTime() > nowMs)
    .sort((a, b) => new Date(a.departure_time).getTime() - new Date(b.departure_time).getTime())[0]

  if (!next) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Bus size={16} className="shrink-0" />
        <span>{t('bus.noDepartures')}</span>
      </div>
    )
  }

  const depTime = formatTime(next.departure_time, { hour: '2-digit', minute: '2-digit' })
  const minutesUntil = Math.round((new Date(next.departure_time).getTime() - nowMs) / 60_000)

  return (
    <div className="flex items-center gap-2 text-sm">
      <Bus size={16} className="text-gray-400 shrink-0" />
      <span className="font-medium text-blue-400">{next.line}</span>
      <span className="text-gray-300 truncate">{next.destination}</span>
      <span className="text-gray-500 ml-auto shrink-0">
        {depTime}
        {minutesUntil <= 30 && (
          <span className="text-gray-400"> ({minutesUntil}{t('bus.min')})</span>
        )}
      </span>
    </div>
  )
}
