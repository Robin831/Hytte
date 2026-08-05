import React, { useState, useEffect, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Bus, RefreshCw, Settings, Search, Plus, Trash2, Circle, GripVertical, Footprints, AlertTriangle } from 'lucide-react'
import { Skeleton } from '../components/ui/skeleton'

interface Departure {
  line: string
  destination: string
  departure_time: string
  is_realtime: boolean
  platform?: string
  delay_minutes: number
}

interface StopDepartures {
  stop_id: string
  stop_name: string
  /** Walking offset for this stop, mirrored from the saved settings blob. */
  walk_minutes?: number
  departures: Departure[]
  /**
   * Set when this stop's fetch failed upstream (error or timeout). Absent for
   * healthy stops — including ones that genuinely have no departures right now,
   * which must keep rendering the neutral empty state.
   */
  error?: boolean
}

interface FavoriteStop {
  id: string
  name: string
  routes: string[]
  walk_minutes?: number
}

interface SearchResult {
  id: string
  name: string
}

/** A stop paired with the departures that are still reachable right now. */
interface VisibleStop {
  stop: StopDepartures
  visible: Departure[]
  departedCount: number
}

const REFRESH_INTERVAL_MS = 30_000
const MAX_WALK_MINUTES = 120
/** Below this many still-reachable departures a stop asks for fresh data. */
const LOW_DEPARTURE_THRESHOLD = 2
/** Minimum gap between off-cycle refreshes, counted across all stops. */
const OFF_CYCLE_REFRESH_MIN_GAP_MS = 10_000

/** NSR stop IDs contain colons; strip them so the DOM id stays selector-safe. */
function walkInputId(stopId: string): string {
  return `walk-${stopId.replace(/[^A-Za-z0-9_-]/g, '-')}`
}

/**
 * Minutes until the user has to leave: the departure time minus the stop's
 * walking offset. With no offset configured this is simply the time until
 * departure, which keeps the pre-offset rendering unchanged.
 */
function minutesUntil(departureTime: string, now: number, walkMinutes = 0): number {
  const diff = new Date(departureTime).getTime() - walkMinutes * 60_000 - now
  return Math.round(diff / 60_000)
}

/**
 * Epoch millis at which the user has to set off to catch this departure. With
 * no walking offset this is the departure time itself.
 */
function leaveTime(departureTime: string, walkMinutes: number): number {
  return new Date(departureTime).getTime() - walkMinutes * 60_000
}

function formatDeparture(
  departureTime: string,
  now: number,
  walkMinutes: number,
  t: (key: string) => string,
): string {
  const mins = minutesUntil(departureTime, now, walkMinutes)
  if (mins <= 0) {
    // Only departures whose leave time is still ahead are rendered, so a
    // rounded-down zero means "right now", never "too late".
    if (walkMinutes <= 0) return '0 ' + t('transit:min')
    return t('transit:leaveNow')
  }
  if (mins < 30) return `${mins} ${t('transit:min')}`
  return new Date(new Date(departureTime).getTime() - walkMinutes * 60_000)
    .toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

export default function Transit() {
  const { t } = useTranslation(['transit', 'common'])

  const [stops, setStops] = useState<StopDepartures[]>([])
  const [loading, setLoading] = useState(true)
  // True while any departures fetch is in flight, including silent refreshes.
  // Drives the per-stop retry affordance without clearing the rows already on
  // screen, so healthy stops stay visible while a failing one is retried.
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)

  const [showSettings, setShowSettings] = useState(false)
  const [favoriteStops, setFavoriteStops] = useState<FavoriteStop[]>([])
  const [settingsSaving, setSettingsSaving] = useState(false)
  const [settingsMsg, setSettingsMsg] = useState<string | null>(null)

  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<SearchResult[]>([])
  const [searching, setSearching] = useState(false)

  // Track which stop ID is pending removal confirmation.
  const [confirmRemoveId, setConfirmRemoveId] = useState<string | null>(null)

  // Drag-and-drop state.
  const [dragIndex, setDragIndex] = useState<number | null>(null)
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null)

  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)
  const searchAbortRef = useRef<AbortController | null>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  // Show the loading skeleton only on the very first fetch; subsequent refreshes
  // (30s interval, tab-visibility, manual) update departures silently in place.
  const isInitialLoad = useRef(true)
  const [refreshKey, setRefreshKey] = useState(0)

  // Tick a clock value every second so relative departure labels ("5 min")
  // recompute against the current time between the 30s Entur polls. This is a
  // pure client-side timer — deliberately NOT wired to refreshKey, so it never
  // triggers a data fetch.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  // Drop departures the user can no longer make. Recomputed on every 1s tick,
  // so a row leaves the list the moment its leave time passes instead of
  // lingering until the next 30s poll. The strict `>` keeps a zero-offset stop
  // showing "0 min" right up to the departure timestamp.
  const visibleStops = useMemo<VisibleStop[]>(
    () =>
      stops.map(stop => {
        const walkMinutes = stop.walk_minutes ?? 0
        const visible = stop.departures.filter(
          dep => leaveTime(dep.departure_time, walkMinutes) > now
        )
        return { stop, visible, departedCount: stop.departures.length - visible.length }
      }),
    [stops, now]
  )

  // Timestamp of the last off-cycle refresh. Held in a ref so the rate limit
  // survives re-renders without feeding back into the render cycle.
  const lastOffCycleRefreshRef = useRef(0)

  // When pruning leaves a stop with almost nothing to show, pull fresh data
  // rather than waiting out the rest of the 30s cadence. The 10s gate is what
  // stops this from looping if Entur keeps handing back the same stale window.
  useEffect(() => {
    const isShort = visibleStops.some(
      ({ stop, visible }) =>
        visible.length < LOW_DEPARTURE_THRESHOLD && visible.length < stop.departures.length
    )
    if (!isShort) return
    if (now - lastOffCycleRefreshRef.current < OFF_CYCLE_REFRESH_MIN_GAP_MS) return
    lastOffCycleRefreshRef.current = now
    setRefreshKey(k => k + 1)
  }, [visibleStops, now])

  // Initial load + auto-refresh every 30 seconds, paused while the tab is hidden.
  useEffect(() => {
    const controller = new AbortController()

    const fetchDepartures = async () => {
      if (isInitialLoad.current) setLoading(true)
      setRefreshing(true)
      try {
        const res = await fetch('/api/transit/departures', { credentials: 'include', signal: controller.signal })
        if (!res.ok) throw new Error(await res.text())
        const data: { stops: StopDepartures[] } = await res.json()
        setStops(data.stops)
        setError(null)
        setLastUpdated(new Date())
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(t('transit:error'))
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          setRefreshing(false)
          isInitialLoad.current = false
        }
      }
    }

    const stopPolling = () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current)
        intervalRef.current = null
      }
    }

    // Only poll while the tab is visible; guard against accumulating intervals.
    const startPolling = () => {
      if (intervalRef.current || document.hidden) return
      intervalRef.current = setInterval(() => setRefreshKey(k => k + 1), REFRESH_INTERVAL_MS)
    }

    const handleVisibilityChange = () => {
      if (document.hidden) {
        // Pause polling so backgrounded tabs don't burn Entur API budget.
        stopPolling()
      } else {
        // On return, immediately refresh and resume the regular cadence.
        void fetchDepartures()
        startPolling()
      }
    }

    if (!document.hidden) {
      void fetchDepartures()
      startPolling()
    }
    document.addEventListener('visibilitychange', handleVisibilityChange)

    return () => {
      controller.abort()
      stopPolling()
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [refreshKey, t])

  // Load saved stops when settings panel opens.
  useEffect(() => {
    if (!showSettings) return
    fetch('/api/transit/settings', { credentials: 'include' })
      .then(r => r.ok ? r.json() : { stops: [] })
      .then((data: { stops: FavoriteStop[] }) => setFavoriteStops(data.stops))
      .catch(() => {})
  }, [showSettings])

  // Debounced stop search with AbortController to prevent stale results.
  useEffect(() => {
    if (searchTimeout.current) clearTimeout(searchTimeout.current)
    if (searchQuery.trim().length < 2) {
      searchTimeout.current = setTimeout(() => setSearchResults([]), 0)
      return
    }
    searchTimeout.current = setTimeout(async () => {
      // Abort any previous in-flight request before starting a new one.
      if (searchAbortRef.current) searchAbortRef.current.abort()
      const controller = new AbortController()
      searchAbortRef.current = controller
      setSearching(true)
      try {
        const res = await fetch(
          '/api/transit/search?q=' + encodeURIComponent(searchQuery),
          { credentials: 'include', signal: controller.signal }
        )
        if (!res.ok) return
        const data: { results: SearchResult[] } = await res.json()
        setSearchResults(data.results)
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        // non-critical
      } finally {
        if (!controller.signal.aborted) setSearching(false)
      }
    }, 300)
    return () => {
      if (searchTimeout.current) clearTimeout(searchTimeout.current)
      searchAbortRef.current?.abort()
    }
  }, [searchQuery])

  function addStop(result: SearchResult) {
    if (favoriteStops.some(s => s.id === result.id)) return
    setFavoriteStops(prev => [...prev, { id: result.id, name: result.name, routes: [], walk_minutes: 0 }])
    setSearchQuery('')
    setSearchResults([])
  }

  function confirmRemove(id: string) {
    setConfirmRemoveId(id)
  }

  function doRemoveStop(id: string) {
    setFavoriteStops(prev => prev.filter(s => s.id !== id))
    setConfirmRemoveId(null)
  }

  function updateRoutes(id: string, value: string) {
    const routes = value
      .split(',')
      .map(r => r.trim())
      .filter(r => r.length > 0)
    setFavoriteStops(prev =>
      prev.map(s => s.id === id ? { ...s, routes } : s)
    )
  }

  // Clamp client-side to the same 0-120 range the API enforces, so a rejected
  // PUT is an edge case rather than the normal way to discover the limit.
  function updateWalkMinutes(id: string, value: string) {
    const parsed = Number.parseInt(value, 10)
    const walkMinutes = Number.isNaN(parsed)
      ? 0
      : Math.min(MAX_WALK_MINUTES, Math.max(0, parsed))
    setFavoriteStops(prev =>
      prev.map(s => s.id === id ? { ...s, walk_minutes: walkMinutes } : s)
    )
  }

  // Drag handlers for reordering stops.
  function handleDragStart(index: number, e?: React.DragEvent) {
    if (e && e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      // Minimal payload required by some browsers (e.g., Firefox) to enable drag
      e.dataTransfer.setData('text/plain', String(index))
    }
    setDragIndex(index)
  }

  function handleDragOver(e: React.DragEvent, index: number) {
    e.preventDefault()
    setDragOverIndex(index)
  }

  function handleDrop(e: React.DragEvent, dropIndex: number) {
    e.preventDefault()
    if (dragIndex === null || dragIndex === dropIndex) {
      setDragOverIndex(null)
      return
    }
    setFavoriteStops(prev => {
      if (dragIndex === null) return prev
      const next = [...prev]
      const [moved] = next.splice(dragIndex, 1)
      const targetIndex = dragIndex < dropIndex ? dropIndex - 1 : dropIndex
      next.splice(targetIndex, 0, moved)
      return next
    })
    setDragIndex(null)
    setDragOverIndex(null)
  }

  function handleDragEnd() {
    setDragIndex(null)
    setDragOverIndex(null)
  }

  async function saveSettings() {
    setSettingsSaving(true)
    setSettingsMsg(null)
    try {
      const res = await fetch('/api/transit/settings', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ stops: favoriteStops }),
      })
      if (!res.ok) throw new Error()
      setSettingsMsg(t('transit:settingsSaved'))
      // Refresh departures with new stop config.
      setRefreshKey(k => k + 1)
    } catch {
      setSettingsMsg(t('transit:settingsError'))
    } finally {
      setSettingsSaving(false)
    }
  }

  return (
    <div className="p-4 md:p-6 max-w-3xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <Bus size={24} className="text-blue-400" />
          <h1 className="text-xl font-semibold text-white">{t('transit:title')}</h1>
        </div>
        <div className="flex items-center gap-2">
          {lastUpdated && !loading && (
            <span className="text-xs text-gray-500">
              {lastUpdated.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
            </span>
          )}
          <button
            onClick={() => { setRefreshKey(k => k + 1) }}
            disabled={loading}
            className="p-2 rounded-lg text-gray-400 hover:text-white hover:bg-gray-800 transition-colors disabled:opacity-50 cursor-pointer"
            aria-label={t('common:actions.refresh')}
            title={t('common:actions.refresh')}
          >
            <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
          </button>
          <button
            onClick={() => {
              setShowSettings(v => {
                if (v) {
                  setConfirmRemoveId(null)
                  setDragIndex(null)
                  setDragOverIndex(null)
                }
                return !v
              })
            }}
            className={`p-2 rounded-lg transition-colors cursor-pointer ${showSettings ? 'text-blue-400 bg-gray-800' : 'text-gray-400 hover:text-white hover:bg-gray-800'}`}
            aria-label={showSettings ? t('transit:hideSettings') : t('transit:showSettings')}
            title={showSettings ? t('transit:hideSettings') : t('transit:showSettings')}
          >
            <Settings size={16} />
          </button>
        </div>
      </div>

      {/* Settings panel */}
      {showSettings && (
        <div className="mb-6 bg-gray-800 rounded-xl p-4 space-y-4">
          <h2 className="text-sm font-medium text-white">{t('transit:settings')}</h2>

          {/* Stop search */}
          <div className="relative">
            <div className="relative">
              <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input
                type="text"
                value={searchQuery}
                onChange={e => {
                  const val = e.target.value
                  if (val.trim().length < 2) setSearchResults([])
                  setSearchQuery(val)
                }}
                placeholder={t('transit:searchStops')}
                aria-label={t('transit:searchStops')}
                className="w-full pl-8 pr-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
              />
              {searching && (
                <RefreshCw size={12} className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 animate-spin" />
              )}
            </div>
            {searchResults.length > 0 && (
              <div className="absolute z-10 w-full mt-1 bg-gray-700 border border-gray-600 rounded-lg shadow-lg overflow-hidden">
                {searchResults.map(r => (
                  <button
                    key={r.id}
                    onClick={() => addStop(r)}
                    className="flex items-center gap-2 w-full px-3 py-2 text-sm text-gray-200 hover:bg-gray-600 text-left cursor-pointer"
                  >
                    <Plus size={12} className="text-gray-400 shrink-0" />
                    <span className="truncate">{r.name}</span>
                  </button>
                ))}
              </div>
            )}
            {searchQuery.trim().length >= 2 && !searching && searchResults.length === 0 && (
              <p className="mt-1 text-xs text-gray-500">{t('transit:noResults')}</p>
            )}
          </div>

          {/* Saved stops */}
          {favoriteStops.length === 0 ? (
            <p className="text-sm text-gray-400">{t('transit:noSavedStops')}</p>
          ) : (
            <div className="space-y-2">
              {favoriteStops.map((stop, index) => (
                <div
                  key={stop.id}
                  onDragOver={e => handleDragOver(e, index)}
                  onDrop={e => handleDrop(e, index)}
                  className={`flex items-start gap-2 bg-gray-700 rounded-lg p-3 transition-opacity ${dragOverIndex === index && dragIndex !== index ? 'opacity-50 ring-2 ring-blue-500' : ''}`}
                >
                  {/* Drag handle */}
                  <button
                    type="button"
                    draggable
                    onDragStart={e => handleDragStart(index, e)}
                    onDragEnd={handleDragEnd}
                    className="text-gray-500 hover:text-gray-300 cursor-grab active:cursor-grabbing mt-0.5 shrink-0 rounded focus:outline-none focus:text-gray-300 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-900"
                    aria-label={t('transit:dragToReorder')}
                    title={t('transit:dragToReorder')}
                    onKeyDown={e => {
                      if (e.key === 'ArrowUp' && index > 0) {
                        e.preventDefault()
                        setFavoriteStops(prev => {
                          const next = [...prev]
                          const [moved] = next.splice(index, 1)
                          next.splice(index - 1, 0, moved)
                          return next
                        })
                      } else if (e.key === 'ArrowDown' && index < favoriteStops.length - 1) {
                        e.preventDefault()
                        setFavoriteStops(prev => {
                          const next = [...prev]
                          const [moved] = next.splice(index, 1)
                          next.splice(index + 1, 0, moved)
                          return next
                        })
                      }
                    }}
                  >
                    <GripVertical size={14} />
                  </button>

                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-white truncate">{stop.name}</p>
                    <input
                      type="text"
                      defaultValue={stop.routes.join(', ')}
                      onBlur={e => updateRoutes(stop.id, e.target.value)}
                      placeholder={t('transit:filterRoutesPlaceholder')}
                      aria-label={t('transit:filterRoutes')}
                      className="mt-1 w-full px-2 py-1 bg-gray-600 border border-gray-500 rounded text-xs text-gray-200 placeholder-gray-500 focus:outline-none focus:border-blue-500"
                    />
                    <div className="mt-1 flex items-center gap-2">
                      <label
                        htmlFor={walkInputId(stop.id)}
                        className="flex items-center gap-1 text-xs text-gray-300 shrink-0"
                      >
                        <Footprints size={12} className="text-gray-400" aria-hidden="true" />
                        {t('transit:walkMinutes')}
                      </label>
                      <input
                        id={walkInputId(stop.id)}
                        type="number"
                        min={0}
                        max={MAX_WALK_MINUTES}
                        step={1}
                        inputMode="numeric"
                        value={String(stop.walk_minutes ?? 0)}
                        onChange={e => updateWalkMinutes(stop.id, e.target.value)}
                        className="w-16 px-2 py-1 bg-gray-600 border border-gray-500 rounded text-xs text-gray-200 focus:outline-none focus:border-blue-500"
                      />
                    </div>
                  </div>

                  {/* Remove button or inline confirmation */}
                  {confirmRemoveId === stop.id ? (
                    <div className="flex items-center gap-1 shrink-0 mt-0.5">
                      <span className="text-xs text-gray-300 mr-1">{t('transit:confirmRemove')}</span>
                      <button
                        type="button"
                        onClick={() => doRemoveStop(stop.id)}
                        className="px-2 py-0.5 text-xs bg-red-700 hover:bg-red-600 text-white rounded transition-colors cursor-pointer"
                      >
                        {t('transit:confirmRemoveYes')}
                      </button>
                      <button
                        type="button"
                        onClick={() => setConfirmRemoveId(null)}
                        className="px-2 py-0.5 text-xs bg-gray-600 hover:bg-gray-500 text-gray-200 rounded transition-colors cursor-pointer"
                      >
                        {t('transit:confirmRemoveNo')}
                      </button>
                    </div>
                  ) : (
                    <button
                      type="button"
                      onClick={() => confirmRemove(stop.id)}
                      className="text-gray-400 hover:text-red-400 transition-colors cursor-pointer mt-0.5 shrink-0"
                      aria-label={t('transit:removeStop')}
                      title={t('transit:removeStop')}
                    >
                      <Trash2 size={14} />
                    </button>
                  )}
                </div>
              ))}
              <p className="text-xs text-gray-400">{t('transit:walkMinutesHint')}</p>
            </div>
          )}

          {/* Save button + feedback */}
          <div className="flex items-center gap-3">
            <button
              onClick={saveSettings}
              disabled={settingsSaving}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
            >
              {settingsSaving ? t('transit:saving') : t('transit:saveSettings')}
            </button>
            {settingsMsg && (
              <span className="text-xs text-gray-400">{settingsMsg}</span>
            )}
          </div>
        </div>
      )}

      {/* Departures */}
      {error && !loading && (
        <div className="bg-red-900/30 border border-red-700 text-red-300 rounded-xl p-4 text-sm">
          {error}
        </div>
      )}

      {loading && stops.length === 0 && (
        <div className="space-y-3" role="status" aria-live="polite" aria-busy="true">
          <span className="sr-only">{t('loading')}</span>
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </div>
      )}

      <div className="space-y-4">
        {visibleStops.map(({ stop, visible, departedCount }) => (
          <div key={stop.stop_id} className="bg-gray-800 rounded-xl overflow-hidden">
            <div className="flex items-center gap-2 px-4 py-3 border-b border-gray-700">
              <Bus size={16} className="text-blue-400 shrink-0" />
              <h2 className="text-sm font-semibold text-white">{stop.stop_name}</h2>
            </div>

            {/* A failed stop is checked before the empty state: both arrive with
                zero departures, but "we couldn't reach Entur" must never read as
                "no bus tonight". */}
            {stop.error ? (
              <div className="flex flex-wrap items-center gap-2 px-4 py-3 bg-amber-900/20">
                <AlertTriangle size={16} className="text-amber-400 shrink-0" aria-hidden="true" />
                <p className="flex-1 min-w-0 text-sm text-amber-300">{t('transit:stopError')}</p>
                <button
                  type="button"
                  onClick={() => { setRefreshKey(k => k + 1) }}
                  disabled={refreshing}
                  className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-amber-800/50 hover:bg-amber-700/60 disabled:opacity-50 text-amber-100 text-xs font-medium transition-colors cursor-pointer shrink-0"
                >
                  <RefreshCw size={12} className={refreshing ? 'animate-spin' : ''} aria-hidden="true" />
                  {t('transit:retryStop')}
                </button>
              </div>
            ) : visible.length === 0 ? (
              <p className="px-4 py-3 text-sm text-gray-400">{t('transit:noDepartures')}</p>
            ) : (
              <div className="divide-y divide-gray-700/50">
                {visible.map((dep) => {
                  const walkMinutes = stop.walk_minutes ?? 0
                  // Minutes until the user must leave, not until the bus goes.
                  const mins = minutesUntil(dep.departure_time, now, walkMinutes)
                  return (
                    <div
                      key={`${dep.line}-${dep.departure_time}`}
                      className="flex items-center gap-2 px-4 py-2.5 sm:gap-3"
                    >
                      {/* Line badge */}
                      <span className="inline-flex items-center justify-center min-w-[2.25rem] px-1.5 py-0.5 rounded bg-blue-700 text-white text-xs font-bold shrink-0">
                        {dep.line}
                      </span>

                      {/* Destination */}
                      <span className="flex-1 min-w-0 text-sm text-gray-200 truncate">
                        {dep.destination}
                      </span>

                      {/* Walking offset badge — only when the stop has one configured.
                          Kept compact (icon + minutes) so it fits alongside the delay
                          text and the time column at 375px; the full wording lives in
                          the accessible label. */}
                      {walkMinutes > 0 && (
                        <span
                          className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-gray-700 text-gray-300 text-xs whitespace-nowrap shrink-0"
                          title={t('transit:walkBadgeTitle', { minutes: walkMinutes })}
                          aria-label={t('transit:walkBadgeTitle', { minutes: walkMinutes })}
                        >
                          <Footprints size={12} aria-hidden="true" />
                          {t('transit:walkBadge', { minutes: walkMinutes })}
                        </span>
                      )}

                      {/* Delay indicator */}
                      {dep.delay_minutes > 0 && (
                        <span className="text-xs text-orange-400 shrink-0">
                          {t('transit:delayed', { minutes: dep.delay_minutes })}
                        </span>
                      )}

                      {/* Realtime indicator */}
                      <Circle
                        size={8}
                        className={`shrink-0 ${dep.is_realtime ? 'text-green-400 fill-green-400' : 'text-gray-500 fill-gray-500'}`}
                        aria-label={dep.is_realtime ? t('transit:realtime') : t('transit:scheduled')}
                      />

                      {/* Time to leave */}
                      <span className={`text-sm font-medium whitespace-nowrap shrink-0 ${mins <= 1 ? 'text-red-400' : mins <= 5 ? 'text-orange-400' : 'text-white'}`}>
                        {formatDeparture(dep.departure_time, now, walkMinutes, t as (key: string) => string)}
                      </span>
                    </div>
                  )
                })}
              </div>
            )}

            {/* Pruned rows are accounted for below the list so they never push
                an actionable departure down the card. */}
            {departedCount > 0 && (
              <p className="px-4 py-2 text-xs text-gray-500">
                {t('transit:departed', { count: departedCount })}
              </p>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
