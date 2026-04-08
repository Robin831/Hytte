import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { formatDate } from '../../utils/formatDate'
import {
  type CalendarViewProps,
  type CalendarEvent,
  getCalendarColorMap,
  getEventColor,
  formatDateKey,
  addDays,
  isToday,
} from './types'

/** Get Monday-based start of week */
function startOfWeek(date: Date): Date {
  const d = new Date(date)
  const day = d.getDay()
  // Monday = 1, Sunday = 0 → shift Sunday to 7
  const diff = day === 0 ? 6 : day - 1
  d.setDate(d.getDate() - diff)
  d.setHours(0, 0, 0, 0)
  return d
}

function getMonthGridDates(year: number, month: number): Date[] {
  const firstOfMonth = new Date(year, month, 1)
  const lastOfMonth = new Date(year, month + 1, 0)
  const gridStart = startOfWeek(firstOfMonth)
  const gridEnd = startOfWeek(addDays(lastOfMonth, 7)) // ensure we fill last row

  const dates: Date[] = []
  let current = new Date(gridStart)
  while (current < gridEnd) {
    dates.push(new Date(current))
    current = addDays(current, 1)
  }
  // Ensure exactly 6 rows (42 cells) for consistent layout
  while (dates.length < 42) {
    dates.push(new Date(current))
    current = addDays(current, 1)
  }
  // But trim to 5 rows if the 6th row is entirely in the next month
  if (dates.length > 35 && dates[35].getMonth() !== month) {
    dates.length = 35
  }
  return dates
}

function getEventsForDate(events: CalendarEvent[], dateKey: string): CalendarEvent[] {
  return events.filter(event => {
    if (event.all_day) {
      // All-day events span from start_time to end_time (exclusive end in Google)
      const startKey = event.start_time.slice(0, 10)
      const endKey = event.end_time.slice(0, 10)
      return dateKey >= startKey && dateKey < endKey
    }
    return formatDateKey(new Date(event.start_time)) === dateKey
  })
}

const MAX_VISIBLE_EVENTS = 3

export default function MonthView({ events, calendars, rangeStart, onNavigateToDay }: CalendarViewProps) {
  const { t } = useTranslation('common')

  const year = rangeStart.getFullYear()
  const month = rangeStart.getMonth()
  const colorMap = useMemo(() => getCalendarColorMap(calendars), [calendars])
  const gridDates = useMemo(() => getMonthGridDates(year, month), [year, month])

  const weekdayHeaders = useMemo(() => {
    // Generate Monday-Sunday headers
    const base = new Date(2024, 0, 1) // a Monday
    return Array.from({ length: 7 }, (_, i) => {
      const d = addDays(base, i)
      return formatDate(d, { weekday: 'short' })
    })
  }, [])

  return (
    <div className="select-none">
      {/* Weekday headers */}
      <div className="grid grid-cols-7 mb-1">
        {weekdayHeaders.map((day, i) => (
          <div key={i} className="text-center text-xs font-medium text-gray-500 py-1">
            {day}
          </div>
        ))}
      </div>

      {/* Day grid */}
      <div className="grid grid-cols-7 border-t border-l border-gray-700/50">
        {gridDates.map((date) => {
          const dateKey = formatDateKey(date)
          const inMonth = date.getMonth() === month
          const today = isToday(date)
          const dayEvents = getEventsForDate(events, dateKey)
          const overflow = dayEvents.length - MAX_VISIBLE_EVENTS

          return (
            <button
              key={dateKey}
              onClick={() => onNavigateToDay(date)}
              className={`
                border-r border-b border-gray-700/50 p-1 min-h-[4.5rem] sm:min-h-[6rem] text-left
                transition-colors hover:bg-gray-800/50 cursor-pointer
                ${inMonth ? '' : 'opacity-40'}
              `}
            >
              {/* Day number */}
              <div className="flex items-center justify-center sm:justify-start mb-0.5">
                <span
                  className={`
                    text-xs sm:text-sm w-6 h-6 flex items-center justify-center rounded-full
                    ${today ? 'bg-blue-600 text-white font-bold' : 'text-gray-300'}
                  `}
                >
                  {date.getDate()}
                </span>
              </div>

              {/* Event pills (desktop) / dots (mobile) */}
              <div className="hidden sm:block space-y-0.5">
                {dayEvents.slice(0, MAX_VISIBLE_EVENTS).map(event => (
                  <div
                    key={event.id}
                    className="text-[10px] leading-tight truncate rounded px-1 py-0.5 text-white/90"
                    style={{ backgroundColor: getEventColor(event, colorMap) + 'cc' }}
                    title={event.title}
                  >
                    {event.all_day ? event.title : `${new Date(event.start_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false }).slice(0, 5)} ${event.title}`}
                  </div>
                ))}
                {overflow > 0 && (
                  <div className="text-[10px] text-gray-400 px-1">
                    +{overflow} {t('calendar.more')}
                  </div>
                )}
              </div>

              {/* Mobile: colored dots */}
              <div className="flex sm:hidden gap-0.5 flex-wrap justify-center">
                {dayEvents.slice(0, 5).map(event => (
                  <span
                    key={event.id}
                    className="w-1.5 h-1.5 rounded-full"
                    style={{ backgroundColor: getEventColor(event, colorMap) }}
                  />
                ))}
                {dayEvents.length > 5 && (
                  <span className="text-[8px] text-gray-400">+</span>
                )}
              </div>
            </button>
          )
        })}
      </div>
    </div>
  )
}
