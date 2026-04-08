import { useMemo, useRef, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { formatDate, formatTime } from '../../utils/formatDate'
import {
  type CalendarViewProps,
  type CalendarEvent,
  getCalendarColorMap,
  getEventColor,
  formatDateKey,
  addDays,
  isToday,
} from './types'

const HOUR_HEIGHT = 48 // px per hour slot
const HOURS = Array.from({ length: 24 }, (_, i) => i)

function getWeekStart(date: Date): Date {
  const d = new Date(date)
  const day = d.getDay()
  const diff = day === 0 ? 6 : day - 1
  d.setDate(d.getDate() - diff)
  d.setHours(0, 0, 0, 0)
  return d
}

function getEventPosition(event: CalendarEvent): { top: number; height: number } {
  const start = new Date(event.start_time)
  const end = new Date(event.end_time)
  const startMinutes = start.getHours() * 60 + start.getMinutes()
  const endMinutes = end.getHours() * 60 + end.getMinutes()
  const duration = Math.max(endMinutes - startMinutes, 15) // minimum 15min display
  return {
    top: (startMinutes / 60) * HOUR_HEIGHT,
    height: (duration / 60) * HOUR_HEIGHT,
  }
}

function getEventsForDay(events: CalendarEvent[], dateKey: string): { allDay: CalendarEvent[]; timed: CalendarEvent[] } {
  const allDay: CalendarEvent[] = []
  const timed: CalendarEvent[] = []
  for (const event of events) {
    if (event.all_day) {
      const startKey = event.start_time.slice(0, 10)
      const endKey = event.end_time.slice(0, 10)
      if (dateKey >= startKey && dateKey < endKey) {
        allDay.push(event)
      }
    } else if (formatDateKey(new Date(event.start_time)) === dateKey) {
      timed.push(event)
    }
  }
  return { allDay, timed }
}

const timeFormatOpts: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' }

export default function WeekView({ events, calendars, rangeStart, onNavigateToDay }: CalendarViewProps) {
  const { t } = useTranslation('common')
  const scrollRef = useRef<HTMLDivElement>(null)
  const colorMap = useMemo(() => getCalendarColorMap(calendars), [calendars])

  const weekStart = useMemo(() => getWeekStart(rangeStart), [rangeStart])
  const weekDays = useMemo(() => Array.from({ length: 7 }, (_, i) => addDays(weekStart, i)), [weekStart])

  // Live-updating current time (updates every 60s)
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(id)
  }, [])

  // Scroll to current hour on mount
  useEffect(() => {
    if (scrollRef.current) {
      const scrollTo = Math.max(0, (now.getHours() - 1) * HOUR_HEIGHT)
      scrollRef.current.scrollTop = scrollTo
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // Collect all-day events per day
  const allDayByDay = useMemo(() => {
    return weekDays.map(day => {
      const dateKey = formatDateKey(day)
      return getEventsForDay(events, dateKey).allDay
    })
  }, [events, weekDays])

  const hasAllDay = allDayByDay.some(d => d.length > 0)

  return (
    <div className="flex flex-col overflow-hidden">
      {/* Day headers */}
      <div className="flex border-b border-gray-700/50">
        {/* Time gutter spacer */}
        <div className="w-12 sm:w-16 shrink-0" />
        <div className="grid grid-cols-7 flex-1">
          {weekDays.map((day) => {
            const today = isToday(day)
            return (
              <button
                key={formatDateKey(day)}
                onClick={() => onNavigateToDay(day)}
                className="text-center py-2 border-l border-gray-700/50 hover:bg-gray-800/50 transition-colors cursor-pointer"
              >
                <div className="text-[10px] sm:text-xs text-gray-500 uppercase">
                  {formatDate(day, { weekday: 'short' })}
                </div>
                <div className={`
                  text-sm sm:text-lg font-medium mx-auto w-7 h-7 sm:w-8 sm:h-8 flex items-center justify-center rounded-full
                  ${today ? 'bg-blue-600 text-white' : 'text-gray-200'}
                `}>
                  {day.getDate()}
                </div>
              </button>
            )
          })}
        </div>
      </div>

      {/* All-day events row */}
      {hasAllDay && (
        <div className="flex border-b border-gray-700/50">
          <div className="w-12 sm:w-16 shrink-0 text-[10px] text-gray-500 flex items-center justify-end pr-2">
            {t('calendar.allDay')}
          </div>
          <div className="grid grid-cols-7 flex-1">
            {allDayByDay.map((dayAllDay, i) => (
              <div key={i} className="border-l border-gray-700/50 p-0.5 min-h-[1.5rem]">
                {dayAllDay.map(event => (
                  <div
                    key={event.id}
                    className="text-[10px] leading-tight truncate rounded px-1 py-0.5 mb-0.5 text-white/90"
                    style={{ backgroundColor: getEventColor(event, colorMap) + 'cc' }}
                    title={event.title}
                  >
                    {event.title}
                  </div>
                ))}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Scrollable time grid */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto overflow-x-hidden" style={{ maxHeight: 'calc(100vh - 16rem)' }}>
        <div className="flex relative" style={{ height: `${24 * HOUR_HEIGHT}px` }}>
          {/* Time labels */}
          <div className="w-12 sm:w-16 shrink-0 relative">
            {HOURS.map(hour => (
              <div
                key={hour}
                className="absolute right-2 text-[10px] sm:text-xs text-gray-500 -translate-y-1/2"
                style={{ top: `${hour * HOUR_HEIGHT}px` }}
              >
                {formatTime(new Date(2024, 0, 1, hour, 0), timeFormatOpts)}
              </div>
            ))}
          </div>

          {/* Day columns */}
          <div className="grid grid-cols-7 flex-1 relative">
            {/* Hour grid lines */}
            {HOURS.map(hour => (
              <div
                key={hour}
                className="absolute w-full border-t border-gray-700/30"
                style={{ top: `${hour * HOUR_HEIGHT}px` }}
              />
            ))}

            {/* Current time indicator */}
            {weekDays.some(d => isToday(d)) && (() => {
              const todayIndex = weekDays.findIndex(d => isToday(d))
              const minutes = now.getHours() * 60 + now.getMinutes()
              const top = (minutes / 60) * HOUR_HEIGHT
              const leftPercent = (todayIndex / 7) * 100

              return (
                <div
                  className="absolute z-10 pointer-events-none"
                  style={{ top: `${top}px`, left: `${leftPercent}%`, width: `${100 / 7}%` }}
                >
                  <div className="relative">
                    <div className="absolute -left-1 -top-1 w-2 h-2 rounded-full bg-red-500" />
                    <div className="h-px bg-red-500 w-full" />
                  </div>
                </div>
              )
            })()}

            {weekDays.map((day, _dayIndex) => {
              const dateKey = formatDateKey(day)
              const { timed } = getEventsForDay(events, dateKey)
              const today = isToday(day)

              return (
                <div
                  key={dateKey}
                  className={`relative border-l border-gray-700/50 ${today ? 'bg-blue-950/20' : ''}`}
                >
                  {timed.map(event => {
                    const { top, height } = getEventPosition(event)
                    const color = getEventColor(event, colorMap)

                    return (
                      <div
                        key={event.id}
                        className="absolute left-0.5 right-0.5 rounded px-1 py-0.5 overflow-hidden text-white/90 hover:opacity-90 transition-opacity z-[2]"
                        style={{
                          top: `${top}px`,
                          height: `${Math.max(height, 18)}px`,
                          backgroundColor: color + 'cc',
                          minHeight: '18px',
                        }}
                        title={`${event.title}\n${formatTime(event.start_time, timeFormatOpts)} – ${formatTime(event.end_time, timeFormatOpts)}${event.location ? '\n' + event.location : ''}`}
                      >
                        <div className="text-[10px] leading-tight font-medium truncate">
                          {event.title}
                        </div>
                        {height >= 30 && (
                          <div className="text-[9px] leading-tight text-white/70 truncate">
                            {formatTime(event.start_time, timeFormatOpts)} – {formatTime(event.end_time, timeFormatOpts)}
                          </div>
                        )}
                      </div>
                    )
                  })}
                </div>
              )
            })}
          </div>
        </div>
      </div>
    </div>
  )
}
