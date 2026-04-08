import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { RefreshCw, ChevronLeft, ChevronRight, MapPin, Clock, Filter, CalendarDays } from 'lucide-react'
import { useAuth } from '../auth'
import { formatDate, formatTime } from '../utils/formatDate'

interface CalendarEvent {
  id: string
  calendar_id: string
  title: string
  description?: string
  location?: string
  start_time: string
  end_time: string
  all_day: boolean
  status: string
  color?: string
}

interface CalendarInfo {
  id: string
  summary: string
  description?: string
  background_color?: string
  foreground_color?: string
  primary: boolean
  selected: boolean
}

function startOfDay(date: Date): Date {
  const d = new Date(date)
  d.setHours(0, 0, 0, 0)
  return d
}

function endOfDay(date: Date): Date {
  const d = new Date(date)
  d.setHours(23, 59, 59, 0)
  return d
}

function addDays(date: Date, days: number): Date {
  const d = new Date(date)
  d.setDate(d.getDate() + days)
  return d
}

/** Format a Date as RFC3339 without fractional seconds, e.g. "2026-04-08T00:00:00Z". */
function toRFC3339(date: Date): string {
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

function formatDateKey(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

function isToday(date: Date): boolean {
  const now = new Date()
  return formatDateKey(date) === formatDateKey(now)
}

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
    // All-day events first within the same start time
    if (a.all_day && !b.all_day) return -1
    if (!a.all_day && b.all_day) return 1
    return a.title.localeCompare(b.title, locale)
  })

  for (const event of sorted) {
    // All-day events have UTC-midnight start times — use UTC date to avoid timezone shift.
    // Timed events use local date so they appear under the correct local day.
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

export default function CalendarPage() {
  const { t, i18n } = useTranslation('common')
  const { user } = useAuth()
  const locale = i18n.language

  const [events, setEvents] = useState<CalendarEvent[]>([])
  const [calendars, setCalendars] = useState<CalendarInfo[]>([])
  const [connected, setConnected] = useState<boolean | null>(null)
  const [calendarsLoading, setCalendarsLoading] = useState(true)
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showSelector, setShowSelector] = useState(false)
  const [savingCalendars, setSavingCalendars] = useState(false)

  // Date range: start from today, show 14 days ahead by default
  const [rangeStart, setRangeStart] = useState(() => startOfDay(new Date()))
  const daysToShow = 14

  const selectorRef = useRef<HTMLDivElement>(null)

  const fetchEvents = useCallback(async (sync = false, signal?: AbortSignal) => {
    if (!user) return
    try {
      // Derive rangeEnd internally so this callback doesn't depend on a new Date instance each render.
      // Use RFC3339 without fractional seconds (backend parseFlexibleTime doesn't support ms).
      // End is 23:59:59 of the last displayed day to avoid including events from the next period.
      const startParam = toRFC3339(rangeStart)
      const endParam = toRFC3339(endOfDay(addDays(rangeStart, daysToShow - 1)))
      const url = `/api/calendar/events?start=${encodeURIComponent(startParam)}&end=${encodeURIComponent(endParam)}${sync ? '&sync=true' : ''}`
      const res = await fetch(url, { credentials: 'include', signal })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      setEvents(data.events ?? [])
      setError(null)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(t('calendar.errors.failedToLoad'))
      console.error('Failed to load calendar events:', err)
    }
  }, [user, rangeStart, t])

  // Fetch calendars once on mount
  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    fetch('/api/calendar/calendars', { credentials: 'include', signal: controller.signal })
      .then(res => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json()
      })
      .then(data => {
        let cals: CalendarInfo[] = data.calendars ?? []
        // If no calendars are explicitly selected, default to the primary calendar so
        // the UI selection matches the backend's "no preference → show primary" behaviour.
        if (cals.length > 0 && !cals.some(c => c.selected)) {
          cals = cals.map(c => ({ ...c, selected: c.primary }))
        }
        setCalendars(cals)
        setConnected(data.connected ?? false)
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setConnected(false)
        console.error('Failed to load calendars:', err)
      })
      .finally(() => {
        if (!controller.signal.aborted) setCalendarsLoading(false)
      })
    return () => controller.abort()
  }, [user])

  // Fetch events whenever rangeStart changes (includes initial load).
  // Use sync=false to avoid hitting Google APIs on every navigation; the Sync button handles explicit refresh.
  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    fetchEvents(false, controller.signal).finally(() => {
      if (!controller.signal.aborted) setLoading(false)
    })
    return () => controller.abort()
  }, [user, rangeStart, fetchEvents])

  // Close calendar selector on outside click
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (selectorRef.current && !selectorRef.current.contains(e.target as Node)) {
        setShowSelector(false)
      }
    }
    if (showSelector) {
      document.addEventListener('mousedown', handleClick)
      return () => document.removeEventListener('mousedown', handleClick)
    }
  }, [showSelector])

  const handleSync = async () => {
    setSyncing(true)
    try {
      const res = await fetch('/api/calendar/sync', {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      await fetchEvents(false)
      setError(null)
    } catch (err) {
      setError(t('calendar.errors.syncFailed'))
      console.error('Calendar sync failed:', err)
    } finally {
      setSyncing(false)
    }
  }

  const handleToggleCalendar = async (calendarId: string, currentlySelected: boolean) => {
    // Capture previous state before optimistic update so revert is deterministic
    const prevCalendars = calendars
    const updated = prevCalendars.map(c =>
      c.id === calendarId ? { ...c, selected: !currentlySelected } : c
    )
    setCalendars(updated)

    const selectedIds = updated.filter(c => c.selected).map(c => c.id)
    // Prevent saving an empty selection — fall back to ['primary'] so the backend
    // doesn't silently revert to its own default and create a UI/results mismatch.
    const idsToSave = selectedIds.length > 0 ? selectedIds : ['primary']

    setSavingCalendars(true)
    try {
      const res = await fetch('/api/calendar/settings', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ calendar_ids: idsToSave }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      // Refetch events with the new calendar selection (no Google sync, just cached)
      await fetchEvents(false)
    } catch (err) {
      // Revert to the state we captured before the optimistic update
      setCalendars(prevCalendars)
      console.error('Failed to save calendar settings:', err)
    } finally {
      setSavingCalendars(false)
    }
  }

  const goToday = () => { setLoading(true); setRangeStart(startOfDay(new Date())) }
  const goPrev = () => { setLoading(true); setRangeStart(prev => addDays(prev, -daysToShow)) }
  const goNext = () => { setLoading(true); setRangeStart(prev => addDays(prev, daysToShow)) }

  if (!user) {
    return (
      <div className="p-6">
        <h1 className="text-2xl font-bold mb-4">{t('calendar.title')}</h1>
        <p className="text-gray-400">{t('calendar.signInRequired')}</p>
      </div>
    )
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

  const rangeFormatOpts: Intl.DateTimeFormatOptions = {
    month: 'short',
    day: 'numeric',
  }

  const grouped = groupEventsByDate(events, locale)

  return (
    <div className="p-4 sm:p-6 max-w-3xl mx-auto">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center gap-3 mb-6">
        <h1 className="text-2xl font-bold">{t('calendar.title')}</h1>

        <div className="flex items-center gap-2 sm:ml-auto">
          {/* Calendar selector toggle */}
          {connected === true && calendars.length > 0 && (
            <div className="relative" ref={selectorRef}>
              <button
                onClick={() => setShowSelector(!showSelector)}
                className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg bg-gray-800 text-gray-300 hover:text-white hover:bg-gray-700 transition-colors cursor-pointer"
                aria-label={t('calendar.selectCalendars')}
                aria-expanded={showSelector}
                aria-haspopup="true"
              >
                <Filter size={16} />
                <span className="hidden sm:inline">{t('calendar.calendars')}</span>
              </button>

              {/* Calendar selector dropdown */}
              {showSelector && (
                <div className="absolute right-0 top-full mt-1 w-72 bg-gray-800 rounded-lg border border-gray-700 shadow-xl z-10 py-2" role="menu">
                  <div className="px-3 py-1.5 text-xs font-medium text-gray-400 uppercase tracking-wide">
                    {t('calendar.selectCalendars')}
                  </div>
                  {calendars.map(cal => (
                    <button
                      key={cal.id}
                      onClick={() => handleToggleCalendar(cal.id, cal.selected)}
                      disabled={savingCalendars}
                      role="menuitemcheckbox"
                      aria-checked={cal.selected}
                      className="flex items-center gap-3 w-full px-3 py-2 text-left text-sm hover:bg-gray-700/50 transition-colors cursor-pointer disabled:opacity-50"
                    >
                      <span
                        className="w-3 h-3 rounded-sm shrink-0 border"
                        style={{
                          backgroundColor: cal.selected ? (cal.background_color || '#4285f4') : 'transparent',
                          borderColor: cal.background_color || '#4285f4',
                        }}
                      />
                      <span className={cal.selected ? 'text-white' : 'text-gray-400'}>
                        {cal.summary}
                        {cal.primary && (
                          <span className="ml-1.5 text-xs text-gray-500">({t('calendar.primary')})</span>
                        )}
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Sync button */}
          {connected === true && (
            <button
              onClick={handleSync}
              disabled={syncing}
              className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg bg-gray-800 text-gray-300 hover:text-white hover:bg-gray-700 transition-colors cursor-pointer disabled:opacity-50"
              aria-label={t('calendar.refresh')}
            >
              <RefreshCw size={16} className={syncing ? 'animate-spin' : ''} />
              <span className="hidden sm:inline">{t('calendar.refresh')}</span>
            </button>
          )}
        </div>
      </div>

      {/* Not connected state — only shown once calendars have loaded to avoid flash for connected users */}
      {!loading && !calendarsLoading && connected === false && (
        <div className="rounded-lg bg-gray-800/50 border border-gray-700 p-6 text-center">
          <CalendarDays size={40} className="mx-auto text-gray-500 mb-3" />
          <p className="text-gray-300 mb-2">{t('calendar.notConnected')}</p>
          <p className="text-sm text-gray-500">{t('calendar.notConnectedHint')}</p>
        </div>
      )}

      {/* Date navigation */}
      {connected === true && (
        <nav className="flex items-center gap-2 mb-4" aria-label={t('calendar.agenda')}>
          <button
            onClick={goPrev}
            className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-800 transition-colors cursor-pointer"
            aria-label={t('calendar.previousPeriod')}
          >
            <ChevronLeft size={20} />
          </button>
          <button
            onClick={goToday}
            className="px-3 py-1 text-sm rounded-lg bg-gray-800 text-gray-300 hover:text-white hover:bg-gray-700 transition-colors cursor-pointer"
          >
            {t('calendar.today')}
          </button>
          <button
            onClick={goNext}
            className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-800 transition-colors cursor-pointer"
            aria-label={t('calendar.nextPeriod')}
          >
            <ChevronRight size={20} />
          </button>
          <span className="text-sm text-gray-400 ml-2" aria-live="polite">
            {formatDate(rangeStart, rangeFormatOpts)} – {formatDate(addDays(rangeStart, daysToShow - 1), rangeFormatOpts)}
          </span>
        </nav>
      )}

      {/* Loading state */}
      {loading && (
        <div className="flex items-center justify-center h-64">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
        </div>
      )}

      {/* Error state */}
      {error && (
        <div role="alert" className="rounded-lg bg-red-900/30 border border-red-800 p-4 mb-4 text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* Agenda list */}
      {!loading && connected === true && (
        <div className="space-y-1" role="list" aria-label={t('calendar.agenda')}>
          {grouped.size === 0 && !error && (
            <div className="text-center py-12 text-gray-500">
              <CalendarDays size={32} className="mx-auto mb-2 opacity-50" />
              <p>{t('calendar.noEvents')}</p>
            </div>
          )}

          {(() => {
            // Build calendar color lookup once, not inside every day's render iteration
            const calColorMap = new Map(calendars.map(c => [c.id, c.background_color]))
            return Array.from(grouped.entries()).map(([dateKey, dayEvents]) => {
            const date = new Date(dateKey + 'T00:00:00')
            const today = isToday(date)
            const tomorrow = isTomorrow(date)

            let dateLabel = formatDate(date, dateFormatOpts)
            if (today) dateLabel = `${t('calendar.todayLabel')} — ${dateLabel}`
            else if (tomorrow) dateLabel = `${t('calendar.tomorrowLabel')} — ${dateLabel}`

            return (
              <div key={dateKey} role="listitem">
                {/* Date header */}
                <h2 className={`sticky top-0 z-[1] px-3 py-2 text-sm font-medium rounded-lg mb-1 ${
                  today
                    ? 'bg-blue-900/40 text-blue-300 border border-blue-800/50'
                    : 'bg-gray-800/60 text-gray-300'
                }`}>
                  {dateLabel}
                </h2>

                {/* Events for this date */}
                <div className="space-y-1 mb-3">
                  {dayEvents.map(event => {
                    const calColor = event.color || calColorMap.get(event.calendar_id) || '#4285f4'

                    return (
                      <article
                        key={event.id}
                        className="flex gap-3 px-3 py-2.5 rounded-lg hover:bg-gray-800/50 transition-colors group"
                        aria-label={event.title}
                      >
                        {/* Color indicator */}
                        <div
                          className="w-1 rounded-full shrink-0 mt-0.5"
                          style={{ backgroundColor: calColor, minHeight: '1.25rem' }}
                        />

                        {/* Event content */}
                        <div className="flex-1 min-w-0">
                          <div className="flex items-start gap-2">
                            <span className="font-medium text-sm text-white truncate">
                              {event.title}
                            </span>
                          </div>

                          {/* Time */}
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

                          {/* Location */}
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
          })
          })()}
        </div>
      )}
    </div>
  )
}
