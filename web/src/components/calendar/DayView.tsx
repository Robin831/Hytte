import { useMemo, useRef, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Clock, MapPin } from 'lucide-react'
import { formatTime } from '../../utils/formatDate'
import {
  type CalendarViewProps,
  type CalendarEvent,
  getCalendarColorMap,
  getEventColor,
  formatDateKey,
  isToday,
} from './types'

const HOUR_HEIGHT = 56 // px per hour slot — slightly taller than week view for readability
const HOURS = Array.from({ length: 24 }, (_, i) => i)

function getEventPosition(event: CalendarEvent, dayStart: Date): { top: number; height: number } {
  const dayStartMs = dayStart.getTime()
  const dayEndMs = new Date(dayStart.getFullYear(), dayStart.getMonth(), dayStart.getDate() + 1).getTime()
  const eventStartMs = new Date(event.start_time).getTime()
  const eventEndMs = new Date(event.end_time).getTime()
  const clampedStartMs = Math.max(eventStartMs, dayStartMs)
  const clampedEndMs = Math.min(eventEndMs, dayEndMs)
  const startMinutes = (clampedStartMs - dayStartMs) / (60 * 1000)
  const durationMinutes = Math.max((clampedEndMs - clampedStartMs) / (60 * 1000), 15)
  return {
    top: (startMinutes / 60) * HOUR_HEIGHT,
    height: (durationMinutes / 60) * HOUR_HEIGHT,
  }
}

const timeFormatOpts: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' }

export default function DayView({ events, calendars, rangeStart }: CalendarViewProps) {
  const { t } = useTranslation('common')
  const scrollRef = useRef<HTMLDivElement>(null)
  const colorMap = useMemo(() => getCalendarColorMap(calendars), [calendars])

  const dateKey = formatDateKey(rangeStart)
  const today = isToday(rangeStart)
  const dayStart = useMemo(
    () => new Date(rangeStart.getFullYear(), rangeStart.getMonth(), rangeStart.getDate()),
    [rangeStart],
  )

  const { allDay, timed } = useMemo(() => {
    const dayStartMs = dayStart.getTime()
    const dayEndMs = new Date(dayStart.getFullYear(), dayStart.getMonth(), dayStart.getDate() + 1).getTime()
    const allDay: CalendarEvent[] = []
    const timed: CalendarEvent[] = []
    for (const event of events) {
      if (event.all_day) {
        const startKey = event.start_time.slice(0, 10)
        const endKey = event.end_time.slice(0, 10)
        if (dateKey >= startKey && dateKey < endKey) {
          allDay.push(event)
        }
      } else {
        const eventStartMs = new Date(event.start_time).getTime()
        const eventEndMs = new Date(event.end_time).getTime()
        if (eventStartMs < dayEndMs && eventEndMs > dayStartMs) {
          timed.push(event)
        }
      }
    }
    timed.sort((a, b) => new Date(a.start_time).getTime() - new Date(b.start_time).getTime())
    return { allDay, timed }
  }, [events, dateKey, dayStart])

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

  return (
    <div className="flex flex-col overflow-hidden">
      {/* All-day events */}
      {allDay.length > 0 && (
        <div className="border-b border-gray-700/50 px-2 py-1.5">
          <div className="text-[10px] text-gray-500 mb-1">{t('calendar.allDay')}</div>
          <div className="space-y-1">
            {allDay.map(event => (
              <div
                key={event.id}
                className="flex items-center gap-2 rounded px-2 py-1.5 text-white/90"
                style={{ backgroundColor: getEventColor(event, colorMap) + 'cc' }}
              >
                <span className="text-sm font-medium truncate">{event.title}</span>
                {event.location && (
                  <span className="text-xs text-white/60 flex items-center gap-1 shrink-0">
                    <MapPin size={10} />
                    <span className="truncate max-w-[12rem]">{event.location}</span>
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Scrollable time grid */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto" style={{ maxHeight: 'calc(100vh - 16rem)' }}>
        <div className="flex relative" style={{ height: `${24 * HOUR_HEIGHT}px` }}>
          {/* Time labels */}
          <div className="w-14 sm:w-16 shrink-0 relative">
            {HOURS.map(hour => (
              <div
                key={hour}
                className="absolute right-2 text-xs text-gray-500 -translate-y-1/2"
                style={{ top: `${hour * HOUR_HEIGHT}px` }}
              >
                {formatTime(new Date(2024, 0, 1, hour, 0), timeFormatOpts)}
              </div>
            ))}
          </div>

          {/* Event column */}
          <div className="flex-1 relative">
            {/* Hour grid lines */}
            {HOURS.map(hour => (
              <div
                key={hour}
                className="absolute w-full border-t border-gray-700/30"
                style={{ top: `${hour * HOUR_HEIGHT}px` }}
              />
            ))}

            {/* Current time indicator */}
            {today && (() => {
              const minutes = now.getHours() * 60 + now.getMinutes()
              const top = (minutes / 60) * HOUR_HEIGHT
              return (
                <div className="absolute w-full z-10 pointer-events-none" style={{ top: `${top}px` }}>
                  <div className="relative">
                    <div className="absolute -left-1 -top-1 w-2 h-2 rounded-full bg-red-500" />
                    <div className="h-px bg-red-500 w-full" />
                  </div>
                </div>
              )
            })()}

            {/* Timed events */}
            {timed.map(event => {
              const { top, height } = getEventPosition(event, dayStart)
              const color = getEventColor(event, colorMap)
              const showDetails = height >= 40

              return (
                <div
                  key={event.id}
                  className="absolute left-1 right-1 sm:right-4 rounded-lg px-2.5 py-1.5 overflow-hidden text-white/90 hover:opacity-90 transition-opacity z-[2] border-l-4"
                  style={{
                    top: `${top}px`,
                    height: `${Math.max(height, 24)}px`,
                    backgroundColor: color + '33',
                    borderLeftColor: color,
                    minHeight: '24px',
                  }}
                >
                  <div className="text-sm font-medium truncate">{event.title}</div>
                  {showDetails && (
                    <div className="flex items-center gap-3 mt-0.5 text-xs text-white/60">
                      <span className="flex items-center gap-1">
                        <Clock size={10} />
                        {formatTime(event.start_time, timeFormatOpts)} – {formatTime(event.end_time, timeFormatOpts)}
                      </span>
                      {event.location && (
                        <span className="flex items-center gap-1 truncate">
                          <MapPin size={10} />
                          <span className="truncate">{event.location}</span>
                        </span>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      </div>
    </div>
  )
}
