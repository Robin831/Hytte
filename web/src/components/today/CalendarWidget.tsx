import { useEffect, useReducer } from 'react'
import { Calendar } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTime, toLocalDateString } from '../../utils/formatDate'

interface CalendarEvent {
  id: string
  title: string
  start: string
  end: string
  all_day?: boolean
}

interface CalendarResponse {
  events: CalendarEvent[]
}

type State = { loading: boolean; error: boolean; data: CalendarEvent[] }
type Action = { type: 'start' } | { type: 'done'; data: CalendarEvent[] } | { type: 'error' }

function reducer(_state: State, action: Action): State {
  switch (action.type) {
    case 'start': return { loading: true, error: false, data: _state.data }
    case 'done': return { loading: false, error: false, data: action.data }
    case 'error': return { loading: false, error: true, data: _state.data }
  }
}

export default function CalendarWidget() {
  const { t } = useTranslation('today')
  const [{ loading, data }, dispatch] = useReducer(reducer, { loading: true, error: false, data: [] })

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'start' })
    const today = toLocalDateString()
    fetch(`/api/calendar/events?from=${today}&limit=3`, {
      credentials: 'include',
      signal: controller.signal,
    })
      .then((r) => (r.ok ? (r.json() as Promise<CalendarResponse>) : Promise.reject()))
      .then((d) => dispatch({ type: 'done', data: d.events ?? [] }))
      .catch(() => { if (!controller.signal.aborted) dispatch({ type: 'error' }) })
    return () => controller.abort()
  }, [])

  if (loading && data.length === 0) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Calendar size={16} className="shrink-0" />
        <span>{t('calendar.loading')}</span>
      </div>
    )
  }

  if (data.length === 0) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Calendar size={16} className="shrink-0" />
        <span>{t('calendar.noEvents')}</span>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-1 text-sm">
      {data.slice(0, 3).map((event) => (
        <div key={event.id} className="flex items-center gap-2">
          <Calendar size={14} className="text-gray-400 shrink-0" />
          <span className="text-gray-300 truncate">{event.title}</span>
          <span className="text-gray-500 ml-auto shrink-0">
            {event.all_day
              ? t('calendar.allDay')
              : formatTime(event.start, { hour: '2-digit', minute: '2-digit' })}
          </span>
        </div>
      ))}
    </div>
  )
}
