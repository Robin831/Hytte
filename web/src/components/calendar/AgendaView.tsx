import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Clock, MapPin, CalendarDays } from 'lucide-react'
import { formatDate, formatTime } from '../../utils/formatDate'
import {
  type CalendarViewProps,
  type CalendarEvent,
  getCalendarColorMap,
  getEventColor,
  formatDateKey,
  isToday,
  addDays,
} from './types'

function isTomorrow(date: Date): boolean {
  const tomorrow = addDays(new Date(), 1)
  return formatDateKey(date) === formatDateKey(tomorrow)
}

function groupEventsByDate(events: CalendarEvent[], locale: string): Map<string, CalendarEvent[]> {
  const groups = new Map<string, CalendarEvent[]>()

  const sorted = [...events].sort((a, b) => {
    const aTime = new Date(a.start_time).getTime()
    const bTime = new Date(b.start_time).getTime()
    if (aTime !== bTime) return aTime - bTime
    if (a.all_day && !b.all_day) return -1
    if (!a.all_day && b.all_day) return 1
    return a.title.localeCompare(b.title, locale)
  })

  for (const event of sorted) {
    let key: string
    if (event.all_day) {
      key = event.start_time.slice(0, 10)
    } else {
      key = formatDateKey(new Date(event.start_time))
    }
    const existing = groups.get(key)
    if (existing) {
      existing.push(event)
    } else {
      groups.set(key, [event])
    }
  }

  return groups
}

const dateFormatOpts: Intl.DateTimeFormatOptions = {
  weekday: 'long',
  month: 'long',
  day: 'numeric',
}

const timeFormatOpts: Intl.DateTimeFormatOptions = {
  hour: '2-digit',
  minute: '2-digit',
}

export default function AgendaView({ events, calendars, locale }: CalendarViewProps) {
  const { t } = useTranslation('common')

  const colorMap = useMemo(() => getCalendarColorMap(calendars), [calendars])
  const grouped = useMemo(() => groupEventsByDate(events, locale), [events, locale])

  return (
    <div className="space-y-1" role="list" aria-label={t('calendar.agenda')}>
      {grouped.size === 0 && (
        <div className="text-center py-12 text-gray-500">
          <CalendarDays size={32} className="mx-auto mb-2 opacity-50" />
          <p>{t('calendar.noEvents')}</p>
        </div>
      )}

      {Array.from(grouped.entries()).map(([dateKey, dayEvents]) => {
        const date = new Date(dateKey + 'T00:00:00')
        const today = isToday(date)
        const tomorrow = isTomorrow(date)

        let dateLabel = formatDate(date, dateFormatOpts)
        if (today) dateLabel = `${t('calendar.todayLabel')} — ${dateLabel}`
        else if (tomorrow) dateLabel = `${t('calendar.tomorrowLabel')} — ${dateLabel}`

        return (
          <div key={dateKey} role="listitem">
            <h2 className={`sticky top-0 z-[1] px-3 py-2 text-sm font-medium rounded-lg mb-1 ${
              today
                ? 'bg-blue-900/40 text-blue-300 border border-blue-800/50'
                : 'bg-gray-800/60 text-gray-300'
            }`}>
              {dateLabel}
            </h2>

            <div className="space-y-1 mb-3">
              {dayEvents.map(event => {
                const calColor = getEventColor(event, colorMap)

                return (
                  <article
                    key={event.id}
                    className="flex gap-3 px-3 py-2.5 rounded-lg hover:bg-gray-800/50 transition-colors group"
                    aria-label={event.title}
                  >
                    <div
                      className="w-1 rounded-full shrink-0 mt-0.5"
                      style={{ backgroundColor: calColor, minHeight: '1.25rem' }}
                    />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-start gap-2">
                        <span className="font-medium text-sm text-white truncate">
                          {event.title}
                        </span>
                      </div>
                      <div className="flex items-center gap-1 mt-0.5 text-xs text-gray-400">
                        <Clock size={12} className="shrink-0" />
                        {event.all_day ? (
                          <span>{t('calendar.allDay')}</span>
                        ) : (
                          <time dateTime={event.start_time}>
                            {formatTime(event.start_time, timeFormatOpts)}
                            {' – '}
                            {formatTime(event.end_time, timeFormatOpts)}
                          </time>
                        )}
                      </div>
                      {event.location && (
                        <div className="flex items-center gap-1 mt-0.5 text-xs text-gray-500">
                          <MapPin size={12} className="shrink-0" />
                          <span className="truncate">{event.location}</span>
                        </div>
                      )}
                    </div>
                  </article>
                )
              })}
            </div>
          </div>
        )
      })}
    </div>
  )
}
