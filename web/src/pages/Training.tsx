import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { Dumbbell, Upload, TrendingUp, BarChart3 } from 'lucide-react'
import { useAuth } from '../auth'
import type { Workout, WeeklySummary } from '../types/training'

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

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function formatDistance(meters: number): string {
  if (meters < 1000) return `${Math.round(meters)}m`
  return `${(meters / 1000).toFixed(1)} km`
}

function formatPace(secPerKm: number): string {
  if (secPerKm <= 0) return '--:--'
  const mins = Math.floor(secPerKm / 60)
  const secs = Math.round(secPerKm % 60)
  return `${mins}:${secs.toString().padStart(2, '0')} /km`
}

export default function Training() {
  const { user } = useAuth()
  const [workouts, setWorkouts] = useState<Workout[]>([])
  const [summaries, setSummaries] = useState<WeeklySummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [uploading, setUploading] = useState(false)
  const [uploadResult, setUploadResult] = useState<{ imported: number; errors: string[] } | null>(null)
  const [dragActive, setDragActive] = useState(false)

  const loadData = useCallback(async () => {
    if (!user) return
    try {
      const [wRes, sRes] = await Promise.all([
        fetch('/api/training/workouts', { credentials: 'include' }),
        fetch('/api/training/summary', { credentials: 'include' }),
      ])
      if (wRes.ok) {
        const wData = await wRes.json()
        setWorkouts(wData.workouts || [])
      }
      if (sRes.ok) {
        const sData = await sRes.json()
        setSummaries(sData.summaries || [])
      }
    } catch {
      setError('Failed to load training data')
    } finally {
      setLoading(false)
    }
  }, [user])

  useEffect(() => {
    if (!user) return
    async function run() {
      try {
        const [wRes, sRes] = await Promise.all([
          fetch('/api/training/workouts', { credentials: 'include' }),
          fetch('/api/training/summary', { credentials: 'include' }),
        ])
        if (wRes.ok) {
          const wData = await wRes.json()
          setWorkouts(wData.workouts || [])
        }
        if (sRes.ok) {
          const sData = await sRes.json()
          setSummaries(sData.summaries || [])
        }
      } catch {
        setError('Failed to load training data')
      } finally {
        setLoading(false)
      }
    }
    run()
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
        setError(data.error || 'Upload failed')
        return
      }
      setUploadResult({
        imported: (data.imported || []).length,
        errors: data.errors || [],
      })
      loadData()
    } catch {
      setError('Upload failed')
    } finally {
      setUploading(false)
    }
  }, [loadData])

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
          <h1 className="text-2xl font-bold">Training</h1>
        </div>
        <div className="flex gap-2">
          {workouts.length > 0 && (
            <>
              <Link
                to="/training/trends"
                className="flex items-center gap-2 px-4 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm transition-colors"
              >
                <TrendingUp size={16} />
                Trends
              </Link>
              <Link
                to="/training/compare"
                className="flex items-center gap-2 px-4 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm transition-colors"
              >
                <BarChart3 size={16} />
                Compare
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
            {uploadResult.imported} workout{uploadResult.imported !== 1 ? 's' : ''} imported
          </p>
          {uploadResult.errors.map((e, i) => (
            <p key={i} className="text-yellow-400 mt-1">{e}</p>
          ))}
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
          {uploading ? 'Uploading...' : 'Drag & drop .fit files here'}
        </p>
        <label className="inline-flex items-center gap-2 px-4 py-2 bg-orange-500 hover:bg-orange-600 rounded-lg text-sm font-medium cursor-pointer transition-colors">
          <Upload size={16} />
          Browse files
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
          <h2 className="text-lg font-semibold mb-3">Weekly Volume</h2>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            {summaries.slice(0, 4).map((s) => (
              <div key={s.week_start} className="bg-gray-800 rounded-xl p-4">
                <p className="text-xs text-gray-500 mb-1">
                  {new Date(s.week_start + 'T00:00:00').toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}
                </p>
                <p className="text-lg font-bold">{formatDuration(s.total_duration_seconds)}</p>
                <p className="text-sm text-gray-400">{formatDistance(s.total_distance_meters)}</p>
                <p className="text-xs text-gray-500">{s.workout_count} workout{s.workout_count !== 1 ? 's' : ''}</p>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Workout list */}
      {workouts.length === 0 ? (
        <div className="bg-gray-800 rounded-xl p-12 text-center">
          <Dumbbell size={48} className="mx-auto mb-4 text-gray-600" />
          <h2 className="text-xl font-semibold mb-2">No workouts yet</h2>
          <p className="text-gray-400">Import .fit files from your Coros watch to get started</p>
        </div>
      ) : (
        <div className="space-y-2">
          <h2 className="text-lg font-semibold mb-3">Workouts</h2>
          {workouts.map((w) => {
            const date = new Date(w.started_at)
            const dateStr = date.toLocaleDateString(undefined, {
              year: 'numeric',
              month: 'short',
              day: 'numeric',
            })
            const timeStr = date.toLocaleTimeString(undefined, {
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
                          <span key={tag} className="bg-gray-700 text-gray-400 px-2 py-0.5 rounded-full text-xs">
                            {tag}
                          </span>
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
                      <p className="font-medium text-white">{w.avg_heart_rate} bpm</p>
                      <p>avg HR</p>
                    </div>
                  )}
                  {w.avg_pace_sec_per_km > 0 && (
                    <div className="text-right hidden sm:block">
                      <p className="font-medium text-white">{formatPace(w.avg_pace_sec_per_km)}</p>
                      <p>pace</p>
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
