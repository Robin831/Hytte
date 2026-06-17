import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { Dumbbell, Upload, TrendingUp, BarChart3, RefreshCw, X, Database } from 'lucide-react'
import { useAuth } from '../auth'
import { useTranslation } from 'react-i18next'
import { formatDate, formatTime } from '../utils/formatDate'
import { formatDistance, formatDuration, formatPace } from '../utils/training'
import type { Workout, WeeklySummary } from '../types/training'
import TagBadge from '../components/TagBadge'
import WorkoutFilterBar from '../components/WorkoutFilterBar'

// Page size for the paginated workout list. The list endpoint clamps this
// server-side; keeping it modest bounds the initial payload and DOM size so a
// large history no longer makes the page load cost grow linearly.
const PAGE_SIZE = 25

const sportIcons: Record<string, string> = {
  running: '🏃',
  cycling: '🚴',
  swimming: '🏊',
  walking: '🚶',
  hiking: '🥾',
  strength: '💪',
  rowing: '🚣',
  cross_country_skiing: '⛷️',
  other: '🏋️',
}

export default function Training() {
  const { user } = useAuth()
  const { t } = useTranslation(['training', 'common'])
  const [workouts, setWorkouts] = useState<Workout[]>([])
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [summaries, setSummaries] = useState<WeeklySummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [uploading, setUploading] = useState(false)
  const [uploadResult, setUploadResult] = useState<{ imported: number; errors: string[] } | null>(null)
  const [dragActive, setDragActive] = useState(false)
  const [refreshTick, setRefreshTick] = useState(0)
  const [hasNewWorkouts, setHasNewWorkouts] = useState(false)
  const [backfilling, setBackfilling] = useState(false)
  const [backfillResult, setBackfillResult] = useState<string | null>(null)
  const latestWorkoutIdRef = useRef<number | null>(null)
  const hasNewWorkoutsRef = useRef(false)

  // Client-side filter state. These narrow the already-loaded `workouts` array
  // in memory only — no backend query or network request is involved.
  const [sportFilter, setSportFilter] = useState('')
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [query, setQuery] = useState('')

  const toggleTag = useCallback((tag: string) => {
    setSelectedTags((prev) =>
      prev.includes(tag) ? prev.filter((t) => t !== tag) : [...prev, tag],
    )
  }, [])

  const clearFilters = useCallback(() => {
    setSportFilter('')
    setSelectedTags([])
    setQuery('')
  }, [])

  // Derived, filtered view of the loaded workouts. Filters combine with AND
  // across the three types; for tags a workout must carry ALL selected tags.
  const filteredWorkouts = useMemo(() => {
    const q = query.trim().toLowerCase()
    return workouts.filter(
      (w) =>
        (!sportFilter || w.sport === sportFilter) &&
        (selectedTags.length === 0 ||
          selectedTags.every((tag) => (w.tags ?? []).includes(tag))) &&
        (!q || w.title.toLowerCase().includes(q)),
    )
  }, [workouts, sportFilter, selectedTags, query])

  const filtersActive = sportFilter !== '' || selectedTags.length > 0 || query !== ''

  useEffect(() => {
    if (!user) return
    let cancelled = false
    ;(async () => {
      try {
        // Load only the first bounded page of workouts. The new-workout baseline
        // comes from the cheap /latest endpoint rather than the page's max id:
        // an older-dated .fit import can carry a higher id than anything on page
        // one, so seeding the ref from the page would falsely trip the banner.
        const [wRes, sRes, lRes] = await Promise.all([
          fetch(`/api/training/workouts?limit=${PAGE_SIZE}`, { credentials: 'include' }),
          fetch('/api/training/summary', { credentials: 'include' }),
          fetch('/api/training/workouts/latest', { credentials: 'include' }),
        ])
        if (cancelled) return
        if (wRes.ok) {
          const wData = await wRes.json()
          const list: Workout[] = wData.workouts || []
          setWorkouts(list)
          setNextCursor(wData.next_cursor ?? null)
        } else {
          setError(t('errors.failedToLoadWorkouts'))
        }
        if (lRes.ok) {
          const lData = await lRes.json()
          latestWorkoutIdRef.current = typeof lData.latest_id === 'number' && lData.latest_id > 0
            ? lData.latest_id
            : null
        }
        if (sRes.ok) {
          const sData = await sRes.json()
          setSummaries(sData.summaries || [])
        } else {
          setError(t('errors.failedToLoadSummaries'))
        }
      } catch {
        if (!cancelled) setError(t('errors.failedToLoadTrainingData'))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => { cancelled = true }
  }, [user, refreshTick, t])

  // handleLoadMore appends the next older page of workouts using the keyset
  // cursor returned by the list endpoint, deduplicating by id at the page
  // boundary. The control hides once next_cursor is null (history exhausted).
  const handleLoadMore = useCallback(async () => {
    if (!nextCursor || loadingMore) return
    setLoadingMore(true)
    try {
      const res = await fetch(
        `/api/training/workouts?limit=${PAGE_SIZE}&cursor=${encodeURIComponent(nextCursor)}`,
        { credentials: 'include' },
      )
      if (!res.ok) {
        setError(t('errors.failedToLoadWorkouts'))
        return
      }
      const data = await res.json()
      const more: Workout[] = data.workouts || []
      setWorkouts(prev => {
        const existing = new Set(prev.map(w => w.id))
        return [...prev, ...more.filter(w => !existing.has(w.id))]
      })
      setNextCursor(data.next_cursor ?? null)
    } catch {
      setError(t('errors.failedToLoadWorkouts'))
    } finally {
      setLoadingMore(false)
    }
  }, [nextCursor, loadingMore, t])

  // handleLoadNew fetches the first page after the /latest poll flags an upload
  // and prepends only the workouts not already loaded, preserving the older
  // pages the user has already paged in (and the current cursor). New uploads
  // always carry the newest started_at, so prepending keeps the DESC ordering.
  const handleLoadNew = useCallback(async () => {
    try {
      const [wRes, sRes] = await Promise.all([
        fetch(`/api/training/workouts?limit=${PAGE_SIZE}`, { credentials: 'include' }),
        fetch('/api/training/summary', { credentials: 'include' }),
      ])
      if (wRes.ok) {
        const wData = await wRes.json()
        const list: Workout[] = wData.workouts || []
        hasNewWorkoutsRef.current = false
        setHasNewWorkouts(false)
        if (workouts.length === 0) {
          setWorkouts(list)
          setNextCursor(wData.next_cursor ?? null)
        } else {
          setWorkouts(prev => {
            const existing = new Set(prev.map(w => w.id))
            return [...list.filter(w => !existing.has(w.id)), ...prev]
          })
        }
        if (list.length > 0) {
          latestWorkoutIdRef.current = Math.max(
            latestWorkoutIdRef.current ?? 0,
            ...list.map(w => w.id),
          )
        }
      }
      if (sRes.ok) {
        const sData = await sRes.json()
        setSummaries(sData.summaries || [])
      }
    } catch {
      // Transient failure — banner stays visible so the user can retry. The SSE
      // reconcile on the next reconnect will re-flag if needed.
    }
  }, [workouts])

  useEffect(() => {
    if (!user) return
    // Subscribe to a per-user SSE stream that the backend pings whenever a
    // workout is imported. This replaces the old visibility-aware polling
    // loop: a quiet page makes zero periodic /latest requests, and the "new
    // workouts" banner appears within ~1-2s of an upload. EventSource handles
    // reconnection automatically; on each (re)connect we do a single /latest
    // fetch to cover any events missed while disconnected.
    let cancelled = false

    // reconcile checks the cheap /latest endpoint and trips the banner if a
    // workout id higher than what we've seen has appeared. Used on connect and
    // reconnect so missed events are not lost.
    const reconcile = async () => {
      try {
        const res = await fetch('/api/training/workouts/latest', { credentials: 'include' })
        if (!res.ok) return
        const data = await res.json()
        const maxId: number = typeof data.latest_id === 'number' ? data.latest_id : 0
        if (cancelled) return
        const seen = latestWorkoutIdRef.current
        if (maxId > 0 && (seen === null || maxId > seen)) {
          latestWorkoutIdRef.current = maxId
          hasNewWorkoutsRef.current = true
          setHasNewWorkouts(true)
        }
      } catch {
        // Transient network failure — EventSource will reconnect and we will
        // reconcile again on the next open.
      }
    }

    const es = new EventSource('/api/training/events', { withCredentials: true })

    // onopen fires on the initial connection and after every automatic
    // reconnect, so this single /latest fetch covers the reconnect-reconcile
    // requirement without any manual backoff/visibility bookkeeping.
    es.onopen = () => { reconcile() }

    es.addEventListener('workout_new', (e: MessageEvent) => {
      if (cancelled) return
      let maxId = 0
      try {
        const data = JSON.parse(e.data)
        if (typeof data.latest_id === 'number') maxId = data.latest_id
      } catch {
        // Malformed payload — fall back to trusting the signal itself below.
      }
      const seen = latestWorkoutIdRef.current
      if (maxId === 0 || seen === null || maxId > seen) {
        if (maxId > 0) latestWorkoutIdRef.current = maxId
        hasNewWorkoutsRef.current = true
        setHasNewWorkouts(true)
      }
    })

    return () => {
      cancelled = true
      es.close()
    }
  }, [user])

  const handleUpload = useCallback(async (files: FileList | File[]) => {
    if (!files.length) return
    setUploading(true)
    setUploadResult(null)
    setError('')

    const formData = new FormData()
    for (const file of files) {
      formData.append('files', file)
    }

    try {
      const res = await fetch('/api/training/upload', {
        method: 'POST',
        credentials: 'include',
        body: formData,
      })
      const data = await res.json()
      if (!res.ok) {
        setError(data.error || t('errors.uploadFailed'))
        return
      }
      setUploadResult({
        imported: (data.imported || []).length,
        errors: data.errors || [],
      })
      setRefreshTick(prev => prev + 1)
    } catch {
      setError(t('errors.uploadFailed'))
    } finally {
      setUploading(false)
    }
  }, [t])

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragActive(false)
    const files = Array.from(e.dataTransfer.files).filter(f =>
      f.name.toLowerCase().endsWith('.fit')
    )
    if (files.length > 0) handleUpload(files)
  }, [handleUpload])

  const handleFileInput = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) handleUpload(e.target.files)
    e.target.value = ''
  }, [handleUpload])

  const handleBackfill = useCallback(async () => {
    if (backfilling) return
    setBackfilling(true)
    setBackfillResult(null)
    setError('')
    try {
      const res = await fetch('/api/training/metrics/backfill', {
        method: 'POST',
        credentials: 'include',
      })
      const data = await res.json()
      if (!res.ok) {
        setError(data.error || t('backfill.error'))
        return
      }
      const count: number = data.updated ?? 0
      if (count === 0) {
        setBackfillResult(t('backfill.successNone'))
      } else {
        setBackfillResult(t('backfill.success', { count }))
      }
      setRefreshTick((prev) => prev + 1)
    } catch {
      setError(t('backfill.error'))
    } finally {
      setBackfilling(false)
    }
  }, [backfilling, t, setRefreshTick])

  if (loading) {
    return (
      <div className="max-w-5xl mx-auto px-4 py-8">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-gray-800 rounded w-48" />
          <div className="h-32 bg-gray-800 rounded" />
          <div className="h-32 bg-gray-800 rounded" />
        </div>
      </div>
    )
  }

  return (
    <div className="max-w-5xl mx-auto px-4 py-8">
      {/* Header */}
      <div className="flex items-center justify-between mb-6 gap-2">
        <div className="flex items-center gap-3 min-w-0">
          <Dumbbell size={24} className="text-orange-400 flex-shrink-0" />
          <h1 className="text-2xl font-bold truncate">{t('title')}</h1>
        </div>
        <div className="flex gap-2 flex-shrink-0">
          {workouts.length > 0 && (
            <>
              <Link
                to="/training/trends"
                className="flex items-center gap-2 px-3 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm transition-colors"
                aria-label={t('nav.trends')}
              >
                <TrendingUp size={16} />
                <span className="hidden sm:inline">{t('nav.trends')}</span>
              </Link>
              <Link
                to="/training/compare"
                className="flex items-center gap-2 px-3 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm transition-colors"
                aria-label={t('nav.compare')}
              >
                <BarChart3 size={16} />
                <span className="hidden sm:inline">{t('nav.compare')}</span>
              </Link>
            </>
          )}
          <button
            type="button"
            onClick={handleBackfill}
            disabled={backfilling}
            className="flex items-center gap-2 px-3 py-2 bg-gray-800 hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed rounded-lg text-sm transition-colors"
            aria-label={backfilling ? t('backfill.running') : t('backfill.button')}
          >
            <Database size={16} />
            <span className="hidden sm:inline">{backfilling ? t('backfill.running') : t('backfill.button')}</span>
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
          {error}
        </div>
      )}

      {backfillResult && (
        <div className="mb-4 p-3 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm">
          {backfillResult}
        </div>
      )}

      {uploadResult && (
        <div className="mb-4 p-3 bg-green-500/10 border border-green-500/20 rounded-lg text-sm">
          <p className="text-green-400">
            {t('upload.imported', { count: uploadResult.imported })}
          </p>
          {uploadResult.errors.map((e, i) => (
            <p key={i} className="text-yellow-400 mt-1">{e}</p>
          ))}
        </div>
      )}

      {hasNewWorkouts && (
        <div className="mb-4 flex items-center justify-between p-3 bg-orange-500/10 border border-orange-500/20 rounded-lg text-sm">
          <button
            type="button"
            onClick={handleLoadNew}
            className="flex items-center gap-2 text-orange-400 hover:text-orange-300 transition-colors"
          >
            <RefreshCw size={16} />
            {t('workouts.newWorkoutsAvailable')}
          </button>
          <button
            type="button"
            onClick={() => { hasNewWorkoutsRef.current = false; setHasNewWorkouts(false) }}
            className="text-gray-500 hover:text-gray-400 transition-colors"
            aria-label={t('common:actions.close')}
          >
            <X size={16} />
          </button>
        </div>
      )}

      {/* Upload zone */}
      <div
        className={`mb-6 border-2 border-dashed rounded-xl p-8 text-center transition-colors ${
          dragActive
            ? 'border-orange-400 bg-orange-400/5'
            : 'border-gray-700 hover:border-gray-600'
        }`}
        onDragOver={(e) => { e.preventDefault(); setDragActive(true) }}
        onDragLeave={() => setDragActive(false)}
        onDrop={handleDrop}
      >
        <Upload size={32} className="mx-auto mb-3 text-gray-500" />
        <p className="text-gray-400 mb-2">
          {uploading ? t('upload.uploading') : t('upload.dragDrop')}
        </p>
        <label className="inline-flex items-center gap-2 px-4 py-2 bg-orange-500 hover:bg-orange-600 rounded-lg text-sm font-medium cursor-pointer transition-colors">
          <Upload size={16} />
          {t('upload.browseFiles')}
          <input
            type="file"
            multiple
            accept=".fit"
            className="hidden"
            onChange={handleFileInput}
            disabled={uploading}
          />
        </label>
      </div>

      {/* Weekly summary cards */}
      {summaries.length > 0 && (
        <div className="mb-6">
          <h2 className="text-lg font-semibold mb-3">{t('weeklyVolume.title')}</h2>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            {summaries.slice(0, 4).map((s) => (
              <div key={s.week_start} className="bg-gray-800 rounded-xl p-4">
                <p className="text-xs text-gray-500 mb-1">
                  {formatDate(s.week_start + 'T00:00:00', { month: 'short', day: 'numeric' })}
                </p>
                <p className="text-lg font-bold">{formatDuration(s.total_duration_seconds, t, { style: 'human' })}</p>
                <p className="text-sm text-gray-400">{formatDistance(s.total_distance_meters, t)}</p>
                <p className="text-xs text-gray-500">{t('weeklyVolume.workoutCount', { count: s.workout_count })}</p>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Workout list */}
      {workouts.length === 0 ? (
        <div className="bg-gray-800 rounded-xl p-12 text-center">
          <Dumbbell size={48} className="mx-auto mb-4 text-gray-600" />
          <h2 className="text-xl font-semibold mb-2">{t('workouts.emptyTitle')}</h2>
          <p className="text-gray-400">{t('workouts.emptyDescription')}</p>
        </div>
      ) : (
        <div className="space-y-2">
          <h2 className="text-lg font-semibold mb-3">{t('workouts.title')}</h2>
          <WorkoutFilterBar
            workouts={workouts}
            sports={Object.keys(sportIcons)}
            sport={sportFilter}
            setSport={setSportFilter}
            selectedTags={selectedTags}
            toggleTag={toggleTag}
            query={query}
            setQuery={setQuery}
            onClear={clearFilters}
          />
          {filteredWorkouts.length === 0 ? (
            <div className="bg-gray-800 rounded-xl p-8 text-center">
              <p className="text-gray-400">{t('filters.noMatches')}</p>
              {filtersActive && (
                <button
                  type="button"
                  onClick={clearFilters}
                  className="mt-3 inline-flex items-center gap-1 px-3 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm text-gray-300 transition-colors"
                >
                  <X size={16} />
                  {t('filters.clear')}
                </button>
              )}
            </div>
          ) : (
          filteredWorkouts.map((w) => {
            const date = new Date(w.started_at)
            const dateStr = formatDate(date, {
              year: 'numeric',
              month: 'short',
              day: 'numeric',
            })
            const timeStr = formatTime(date, {
              hour: '2-digit',
              minute: '2-digit',
            })
            return (
              <Link
                key={w.id}
                to={`/training/${w.id}`}
                className="flex items-center gap-4 bg-gray-800 hover:bg-gray-700 border border-gray-700 hover:border-gray-600 rounded-xl p-4 transition-colors group"
              >
                <span className="text-2xl" title={w.sport}>
                  {sportIcons[w.sport] || sportIcons.other}
                </span>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 min-w-0">
                    <p className="font-medium truncate">{w.title}</p>
                    {w.tags && w.tags.length > 0 && (
                      <div className="flex gap-1 flex-shrink-0 overflow-hidden max-w-[80px] sm:max-w-none">
                        {w.tags.slice(0, 2).map((tag) => (
                          <TagBadge key={tag} tag={tag} />
                        ))}
                        {w.tags.length > 2 && (
                          <span className="hidden sm:inline-flex items-center">
                            {w.tags.slice(2).map((tag) => (
                              <TagBadge key={tag} tag={tag} />
                            ))}
                          </span>
                        )}
                      </div>
                    )}
                  </div>
                  <p className="text-sm text-gray-400">
                    {dateStr} · {timeStr}
                  </p>
                </div>
                <div className="flex gap-4 sm:gap-6 text-sm text-gray-400 flex-shrink-0">
                  <div className="text-right">
                    <p className="font-medium text-white">{formatDuration(w.duration_seconds, t, { style: 'human' })}</p>
                    <p>{formatDistance(w.distance_meters, t)}</p>
                  </div>
                  {w.avg_heart_rate > 0 && (
                    <div className="text-right hidden sm:block">
                      <p className="font-medium text-white">{w.avg_heart_rate} {t('units.bpm')}</p>
                      <p>{t('workouts.avgHR')}</p>
                    </div>
                  )}
                  {w.avg_pace_sec_per_km > 0 && (
                    <div className="text-right hidden sm:block">
                      <p className="font-medium text-white">{formatPace(w.avg_pace_sec_per_km, t)}</p>
                      <p>{t('workouts.pace')}</p>
                    </div>
                  )}
                </div>
              </Link>
            )
          })
          )}
          {nextCursor !== null ? (
            <div className="pt-2 flex justify-center">
              <button
                type="button"
                onClick={handleLoadMore}
                disabled={loadingMore}
                className="flex items-center gap-2 px-4 py-2 bg-gray-800 hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed rounded-lg text-sm transition-colors"
              >
                {loadingMore ? t('workouts.loadingMore') : t('workouts.loadMore')}
              </button>
            </div>
          ) : (
            workouts.length > PAGE_SIZE && (
              <p className="pt-2 text-center text-sm text-gray-500">{t('workouts.noMoreWorkouts')}</p>
            )
          )}
        </div>
      )}
    </div>
  )
}
