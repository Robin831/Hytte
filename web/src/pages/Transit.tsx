import React, { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Bus, RefreshCw, Settings, Search, Plus, Trash2, Circle, GripVertical } from 'lucide-react'
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
  departures: Departure[]
}

interface FavoriteStop {
  id: string
  name: string
  routes: string[]
}

interface SearchResult {
  id: string
  name: string
}

const REFRESH_INTERVAL_MS = 30_000

function minutesUntil(departureTime: string): number {
  const diff = new Date(departureTime).getTime() - Date.now()
  return Math.round(diff / 60_000)
}

function formatDeparture(departureTime: string, t: (key: string) => string): string {
  const mins = minutesUntil(departureTime)
  if (mins <= 0) return '0 ' + t('transit:min')
  if (mins < 30) return `${mins} ${t('transit:min')}`
  return new Date(departureTime).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

export default function Transit() {
  const { t } = useTranslation(['transit', 'common'])

  const [stops, setStops] = useState<StopDepartures[]>([])
  const [loading, setLoading] = useState(true)
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
  const [refreshKey, setRefreshKey] = useState(0)

  // Initial load + auto-refresh every 30 seconds.
  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
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
        setLoading(false)
      }
    })()

    const interval = setInterval(() => setRefreshKey(k => k + 1), REFRESH_INTERVAL_MS)

    return () => {
      controller.abort()
      clearInterval(interval)
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
    setFavoriteStops(prev => [...prev, { id: result.id, name: result.name, routes: [] }])
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
        {stops.map(stop => (
          <div key={stop.stop_id} className="bg-gray-800 rounded-xl overflow-hidden">
            <div className="flex items-center gap-2 px-4 py-3 border-b border-gray-700">
              <Bus size={16} className="text-blue-400 shrink-0" />
              <h2 className="text-sm font-semibold text-white">{stop.stop_name}</h2>
            </div>

            {stop.departures.length === 0 ? (
              <p className="px-4 py-3 text-sm text-gray-400">{t('transit:noDepartures')}</p>
            ) : (
              <div className="divide-y divide-gray-700/50">
                {stop.departures.map((dep) => {
                  const mins = minutesUntil(dep.departure_time)
                  return (
                    <div key={`${dep.line}-${dep.departure_time}`} className="flex items-center gap-3 px-4 py-2.5">
                      {/* Line badge */}
                      <span className="inline-flex items-center justify-center min-w-[2.25rem] px-1.5 py-0.5 rounded bg-blue-700 text-white text-xs font-bold shrink-0">
                        {dep.line}
                      </span>

                      {/* Destination */}
                      <span className="flex-1 text-sm text-gray-200 truncate">
                        {dep.destination}
                      </span>

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

                      {/* Time */}
                      <span className={`text-sm font-medium shrink-0 ${mins <= 1 ? 'text-red-400' : mins <= 5 ? 'text-orange-400' : 'text-white'}`}>
                        {formatDeparture(dep.departure_time, t as (key: string) => string)}
                      </span>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
