import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import type { CalendarEvent, CalendarInfo } from '../calendar/types'
import { getEventColor, getCalendarColorMap, startOfDay, endOfDay } from '../calendar/types'
import Widget from '../Widget'

function toRFC3339(date: Date): string {
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

export default function CalendarWidget() {
  const { t } = useTranslation('dashboard')
  const { i18n } = useTranslation()
  const { user } = useAuth()
  const [events, setEvents] = useState<CalendarEvent[]>([])
  const [calendars, setCalendars] = useState<CalendarInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()

    const now = new Date()
    const start = startOfDay(now)
    const end = endOfDay(now)
    const url = `/api/calendar/events?start=${encodeURIComponent(toRFC3339(start))}&end=${encodeURIComponent(toRFC3339(end))}`

    Promise.all([
      fetch(url, { credentials: 'include', signal: controller.signal }),
      fetch('/api/calendar/calendars', { credentials: 'include', signal: controller.signal }),
    ])
      .then(async ([eventsRes, calendarsRes]) => {
        if (!eventsRes.ok || !calendarsRes.ok) throw new Error('Failed to fetch')
        const eventsData = await eventsRes.json()
        const calendarsData = await calendarsRes.json()
        setEvents(eventsData.events ?? [])
        setCalendars(calendarsData.calendars ?? [])
        setError(null)
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(t('widgets.calendar.error'))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => { controller.abort() }
  }, [user, t])

  const colorMap = getCalendarColorMap(calendars)

  const sorted = [...events].sort((a, b) => {
    if (a.all_day && !b.all_day) return -1
    if (!a.all_day && b.all_day) return 1
    return new Date(a.start_time).getTime() - new Date(b.start_time).getTime()
  })

  const formatTime = (event: CalendarEvent): string => {
    if (event.all_day) return t('widgets.calendar.allDay')
    return new Intl.DateTimeFormat(i18n.language, {
      hour: '2-digit',
      minute: '2-digit',
    }).format(new Date(event.start_time))
  }

  return (
    <Widget title={t('widgets.calendar.title')}>
      {loading && (
        <div className="space-y-2" role="status" aria-live="polite">
          <div className="h-4 bg-gray-700 rounded animate-pulse w-3/4" />
          <div className="h-4 bg-gray-700 rounded animate-pulse w-1/2" />
          <div className="h-4 bg-gray-700 rounded animate-pulse w-2/3" />
        </div>
      )}
      {error && !loading && (
        <p className="text-red-400 text-sm">{error}</p>
      )}
      {!loading && !error && sorted.length === 0 && (
        <p className="text-gray-500 text-sm">{t('widgets.calendar.noEvents')}</p>
      )}
      {!loading && !error && sorted.length > 0 && (
        <div className="space-y-2">
          {sorted.map((event) => (
            <div key={event.id} className="flex items-start gap-2 text-sm">
              <span
                className="mt-1.5 h-2.5 w-2.5 rounded-full shrink-0"
                style={{ backgroundColor: getEventColor(event, colorMap) }}
              />
              <span className="text-gray-400 shrink-0 w-14">{formatTime(event)}</span>
              <div className="min-w-0">
                <span className="text-gray-200 break-words">{event.title}</span>
                {event.location && (
                  <p className="text-gray-500 text-xs truncate">{event.location}</p>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
      {!loading && !error && (
        <a href="/calendar" className="block text-xs text-gray-500 hover:text-gray-400 mt-3">
          {t('widgets.calendar.viewCalendar')}
        </a>
      )}
    </Widget>
  )
}
