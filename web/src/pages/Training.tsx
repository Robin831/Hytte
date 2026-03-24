import { useState, useEffect, useCallback, useRef } from 'react'
import { Link } from 'react-router-dom'
import { Dumbbell, Upload, TrendingUp, BarChart3, RefreshCw, X } from 'lucide-react'
import { useAuth } from '../auth'
import { useTranslation } from 'react-i18next'
import { formatDate, formatTime, formatNumber } from '../utils/formatDate'
import type { Workout, WeeklySummary } from '../types/training'
import TagBadge from '../components/TagBadge'

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
  const [summaries, setSummaries] = useState<WeeklySummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [uploading, setUploading] = useState(false)
  const [uploadResult, setUploadResult] = useState<{ imported: number; errors: string[] } | null>(null)
  const [dragActive, setDragActive] = useState(false)
  const [refreshTick, setRefreshTick] = useState(0)
  const [hasNewWorkouts, setHasNewWorkouts] = useState(false)
  const latestWorkoutIdRef = useRef<number | null>(null)

  function formatDuration(seconds: number): string {
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (h > 0) return t('units.hours_minutes', { h, m })
    return t('units.minutes', { m })
  }

  function formatDistance(meters: number): string {
    if (meters < 1000) return `${Math.round(meters)} ${t('units.m')}`
    return `${formatNumber(meters / 1000, { minimumFractionDigits: 1, maximumFractionDigits: 1 })} ${t('units.km')}`
  }

  function formatPace(secPerKm: number): string {
    if (secPerKm <= 0) return '--:--'
    let mins = Math.floor(secPerKm / 60)
    let secs = Math.round(secPerKm % 60)
    if (secs === 60) { mins++; secs = 0 }
    return `${mins}:${secs.toString().padStart(2, '0')} ${t('units.pace')}`
  }

  useEffect(() => {
    if (!user) return
    ;(async () => {
      try {
        const [wRes, sRes] = await Promise.all([
          fetch('/api/training/workouts', { credentials: 'include' }),
          fetch('/api/training/summary', { credentials: 'include' }),
        ])
        if (wRes.ok) {
          const wData = await wRes.json()
          const list: Workout[] = wData.workouts || []
          setWorkouts(list)
          latestWorkoutIdRef.current = list.length > 0 ? Math.max(...list.map(w => w.id)) : null
        } else {
          setError(t('errors.failedToLoadWorkouts'))
        }
        if (sRes.ok) {
          const sData = await sRes.json()
          setSummaries(sData.summaries || [])
        } else {
          setError(t('errors.failedToLoadSummaries'))
        }
      } catch {
        setError(t('errors.failedToLoadTrainingData'))
      } finally {
        setLoading(false)
      }
    })()
  }, [user, refreshTick, t])

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    const poll = async () => {
      if (document.hidden) return
      try {
        const res = await fetch('/api/training/workouts', { credentials: 'include', signal: controller.signal })
        if (!res.ok) return
        const data = await res.json()
        const list: Workout[] = data.workouts || []
        const maxId = list.length > 0 ? Math.max(...list.map(w => w.id)) : null
        if (maxId !== null && latestWorkoutIdRef.current !== null && maxId > latestWorkoutIdRef.current) {
          setHasNewWorkouts(true)
        }
      } catch {
        // silently ignore polling errors (including AbortError on unmount)
      }
    }
    const intervalId = setInterval(poll, 15000)
    return () => { clearInterval(intervalId); controller.abort() }
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
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <Dumbbell size={24} className="text-orange-400" />
          <h1 className="text-2xl font-bold">{t('title')}</h1>
        </div>
        <div className="flex gap-2">
          {workouts.length > 0 && (
            <>
              <Link
                to="/training/trends"
                className="flex items-center gap-2 px-4 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm transition-colors"
              >
                <TrendingUp size={16} />
                {t('nav.trends')}
              </Link>
              <Link
                to="/training/compare"
                className="flex items-center gap-2 px-4 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm transition-colors"
              >
                <BarChart3 size={16} />
                {t('nav.compare')}
              </Link>
            </>
          )}
        </div>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
          {error}
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
            onClick={() => { setHasNewWorkouts(false); setRefreshTick(prev => prev + 1) }}
            className="flex items-center gap-2 text-orange-400 hover:text-orange-300 transition-colors"
          >
            <RefreshCw size={16} />
            {t('workouts.newWorkoutsAvailable')}
          </button>
          <button
            type="button"
            onClick={() => setHasNewWorkouts(false)}
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
                <p className="text-lg font-bold">{formatDuration(s.total_duration_seconds)}</p>
                <p className="text-sm text-gray-400">{formatDistance(s.total_distance_meters)}</p>
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
          {workouts.map((w) => {
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
                  <div className="flex items-center gap-2">
                    <p className="font-medium truncate">{w.title}</p>
                    {w.tags && w.tags.length > 0 && (
                      <div className="flex gap-1">
                        {w.tags.map((tag) => (
                          <TagBadge key={tag} tag={tag} />
                        ))}
                      </div>
                    )}
                  </div>
                  <p className="text-sm text-gray-400">
                    {dateStr} · {timeStr}
                  </p>
                </div>
                <div className="flex gap-6 text-sm text-gray-400">
                  <div className="text-right">
                    <p className="font-medium text-white">{formatDuration(w.duration_seconds)}</p>
                    <p>{formatDistance(w.distance_meters)}</p>
                  </div>
                  {w.avg_heart_rate > 0 && (
                    <div className="text-right">
                      <p className="font-medium text-white">{w.avg_heart_rate} {t('units.bpm')}</p>
                      <p>{t('workouts.avgHR')}</p>
                    </div>
                  )}
                  {w.avg_pace_sec_per_km > 0 && (
                    <div className="text-right hidden sm:block">
                      <p className="font-medium text-white">{formatPace(w.avg_pace_sec_per_km)}</p>
                      <p>{t('workouts.pace')}</p>
                    </div>
                  )}
                </div>
              </Link>
            )
          })}
        </div>
      )}
    </div>
  )
}
