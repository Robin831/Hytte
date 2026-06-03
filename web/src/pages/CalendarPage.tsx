import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { RefreshCw, ChevronLeft, ChevronRight, Filter, CalendarDays, List, LayoutGrid, Columns3, Calendar, AlertTriangle } from 'lucide-react'
import { useAuth } from '../auth'
import { formatDate } from '../utils/formatDate'
import {
  type CalendarInfo,
  type ViewMode,
  startOfDay,
  addDays,
  startOfWeekMonday,
} from '../components/calendar/types'
import { useCalendarEvents } from '../hooks/useCalendarEvents'
import { AGENDA_DAYS } from '../hooks/calendarUrl'
import MonthView from '../components/calendar/MonthView'
import WeekView from '../components/calendar/WeekView'
import DayView from '../components/calendar/DayView'
import AgendaView from '../components/calendar/AgendaView'

const STORAGE_KEY = 'hytte-calendar-view'

function loadViewMode(): ViewMode {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored && ['month', 'week', 'day', 'agenda'].includes(stored)) {
      return stored as ViewMode
    }
  } catch { /* ignore */ }
  return 'month'
}

export default function CalendarPage() {
  const { t, i18n } = useTranslation('common')
  const { user } = useAuth()
  const locale = i18n.language

  const [calendars, setCalendars] = useState<CalendarInfo[]>([])
  const [connected, setConnected] = useState<boolean | null>(null)
  const [calendarsLoading, setCalendarsLoading] = useState(true)
  const [calendarsError, setCalendarsError] = useState<string | null>(null)
  const [syncing, setSyncing] = useState(false)
  const [showSelector, setShowSelector] = useState(false)
  const [savingCalendars, setSavingCalendars] = useState(false)
  const [viewMode, setViewMode] = useState<ViewMode>(loadViewMode)
  const [rangeStart, setRangeStart] = useState(() => startOfDay(new Date()))

  const { events, loading, error, syncErrors, refetch } = useCalendarEvents(viewMode, rangeStart, user)

  const selectorRef = useRef<HTMLDivElement>(null)

  const handleSetViewMode = (mode: ViewMode) => {
    if (mode === viewMode) return
    setViewMode(mode)
    try { localStorage.setItem(STORAGE_KEY, mode) } catch { /* ignore */ }
  }

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
        if (cals.length > 0 && !cals.some(c => c.selected)) {
          cals = cals.map(c => ({ ...c, selected: c.primary }))
        }
        setCalendars(cals)
        setConnected(data.connected ?? false)
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setCalendarsError(t('calendar.errors.failedToLoad'))
        console.error('Failed to load calendars:', err)
      })
      .finally(() => {
        if (!controller.signal.aborted) setCalendarsLoading(false)
      })
    return () => controller.abort()
  }, [user, t])

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
    await refetch(true)
    setSyncing(false)
  }

  const handleToggleCalendar = async (calendarId: string, currentlySelected: boolean) => {
    const prevCalendars = calendars
    const updated = prevCalendars.map(c =>
      c.id === calendarId ? { ...c, selected: !currentlySelected } : c
    )
    setCalendars(updated)

    const selectedIds = updated.filter(c => c.selected).map(c => c.id)
    if (selectedIds.length === 0) {
      setCalendars(prevCalendars)
      return
    }

    setSavingCalendars(true)
    try {
      const res = await fetch('/api/calendar/settings', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ calendar_ids: selectedIds }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      await refetch(false)
    } catch (err) {
      setCalendars(prevCalendars)
      console.error('Failed to save calendar settings:', err)
    } finally {
      setSavingCalendars(false)
    }
  }

  const goToday = () => { setRangeStart(startOfDay(new Date())) }

  const goPrev = () => {
    setRangeStart(prev => {
      switch (viewMode) {
        case 'month': return new Date(prev.getFullYear(), prev.getMonth() - 1, 1)
        case 'week': return addDays(prev, -7)
        case 'day': return addDays(prev, -1)
        case 'agenda': return addDays(prev, -AGENDA_DAYS)
      }
    })
  }

  const goNext = () => {
    setRangeStart(prev => {
      switch (viewMode) {
        case 'month': return new Date(prev.getFullYear(), prev.getMonth() + 1, 1)
        case 'week': return addDays(prev, 7)
        case 'day': return addDays(prev, 1)
        case 'agenda': return addDays(prev, AGENDA_DAYS)
      }
    })
  }

  /** Navigate to a specific day — switches to day view */
  const handleNavigateToDay = (date: Date) => {
    handleSetViewMode('day')
    setRangeStart(startOfDay(date))
  }

  if (!user) {
    return (
      <div className="p-6">
        <h1 className="text-2xl font-bold mb-4">{t('calendar.title')}</h1>
        <p className="text-gray-400">{t('calendar.signInRequired')}</p>
      </div>
    )
  }

  const rangeLabel = (() => {
    const rangeFormatOpts: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' }
    switch (viewMode) {
      case 'month':
        return formatDate(rangeStart, { month: 'long', year: 'numeric' })
      case 'week': {
        const ws = startOfWeekMonday(rangeStart)
        const we = addDays(ws, 6)
        return `${formatDate(ws, rangeFormatOpts)} – ${formatDate(we, rangeFormatOpts)}`
      }
      case 'day':
        return formatDate(rangeStart, { weekday: 'long', month: 'long', day: 'numeric' })
      case 'agenda':
      default: {
        return `${formatDate(rangeStart, rangeFormatOpts)} – ${formatDate(addDays(rangeStart, AGENDA_DAYS - 1), rangeFormatOpts)}`
      }
    }
  })()

  const viewButtons: { mode: ViewMode; icon: React.ReactNode; labelKey: 'calendar.monthView' | 'calendar.weekView' | 'calendar.dayView' | 'calendar.agenda' }[] = [
    { mode: 'month', icon: <LayoutGrid size={14} />, labelKey: 'calendar.monthView' },
    { mode: 'week', icon: <Columns3 size={14} />, labelKey: 'calendar.weekView' },
    { mode: 'day', icon: <Calendar size={14} />, labelKey: 'calendar.dayView' },
    { mode: 'agenda', icon: <List size={14} />, labelKey: 'calendar.agenda' },
  ]

  const viewProps = {
    events,
    calendars,
    rangeStart,
    locale,
    onNavigateToDay: handleNavigateToDay,
  }

  return (
    <div className={`p-4 sm:p-6 ${viewMode === 'agenda' ? 'max-w-3xl' : 'max-w-6xl'} mx-auto`}>
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center gap-3 mb-4">
        <h1 className="text-2xl font-bold">{t('calendar.title')}</h1>

        <div className="flex items-center gap-2 sm:ml-auto flex-wrap">
          {/* View mode toggle */}
          {connected === true && (
            <div className="flex rounded-lg bg-gray-800 p-0.5" role="radiogroup" aria-label={t('calendar.viewMode')}>
              {viewButtons.map(({ mode, icon, labelKey }) => (
                <button
                  key={mode}
                  onClick={() => handleSetViewMode(mode)}
                  role="radio"
                  aria-checked={viewMode === mode}
                  aria-label={t(labelKey)}
                  className={`
                    flex items-center gap-1 px-2 py-1 text-xs rounded-md transition-colors cursor-pointer
                    ${viewMode === mode
                      ? 'bg-gray-700 text-white'
                      : 'text-gray-400 hover:text-gray-200'
                    }
                  `}
                >
                  {icon}
                  <span>{t(labelKey)}</span>
                </button>
              ))}
            </div>
          )}

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

          {/* Sync errors chip — visible only when one or more calendars failed to sync */}
          {connected === true && syncErrors.length > 0 && (() => {
            const calendarNames = syncErrors.map(se => {
              const cal = calendars.find(c => c.id === se.calendar_id)
              return cal?.summary || se.calendar_id
            })
            const joined = calendarNames.join(', ')
            const tooltip = t('calendar.syncErrors.tooltip', { calendars: joined })
            return (
              <span
                role="status"
                title={tooltip}
                aria-label={t('calendar.syncErrors.ariaLabel', { calendars: joined })}
                className="flex items-center gap-1.5 px-2 py-1 text-xs rounded-lg bg-amber-900/40 text-amber-300 border border-amber-700/60"
              >
                <AlertTriangle size={14} />
                <span>{t('calendar.syncErrors.chip')}</span>
              </span>
            )
          })()}
        </div>
      </div>

      {/* Not connected state */}
      {!loading && !calendarsLoading && connected === false && (
        <div className="rounded-lg bg-gray-800/50 border border-gray-700 p-6 text-center">
          <CalendarDays size={40} className="mx-auto text-gray-500 mb-3" />
          <p className="text-gray-300 mb-2">{t('calendar.notConnected')}</p>
          <p className="text-sm text-gray-500">{t('calendar.notConnectedHint')}</p>
        </div>
      )}

      {/* Date navigation */}
      {connected === true && (
        <nav className="flex items-center gap-2 mb-4" aria-label={t('calendar.title')}>
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
            {rangeLabel}
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
      {(error || calendarsError) && (
        <div role="alert" className="rounded-lg bg-red-900/30 border border-red-800 p-4 mb-4 text-red-300 text-sm">
          {error || calendarsError}
        </div>
      )}

      {/* View content */}
      {!loading && connected === true && (
        <>
          {viewMode === 'month' && <MonthView {...viewProps} />}
          {viewMode === 'week' && <WeekView {...viewProps} />}
          {viewMode === 'day' && <DayView {...viewProps} />}
          {viewMode === 'agenda' && <AgendaView {...viewProps} />}
        </>
      )}
    </div>
  )
}
