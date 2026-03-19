import { useState, useEffect } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { ArrowLeft, Trash2, Save, GitCompareArrows, Sparkles, ChevronDown, ChevronUp } from 'lucide-react'
import { useAuth } from '../auth'
import type { Workout, ZoneDistribution, CachedInsights } from '../types/training'
import WorkoutHRChart from '../components/charts/WorkoutHRChart'
import WorkoutPaceChart from '../components/charts/WorkoutPaceChart'
import TagBadge from '../components/TagBadge'

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  if (h > 0) return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
  return `${m}:${s.toString().padStart(2, '0')}`
}

function formatDistance(meters: number): string {
  if (meters < 1000) return `${Math.round(meters)} m`
  return `${(meters / 1000).toFixed(2)} km`
}

function formatPace(secPerKm: number): string {
  if (secPerKm <= 0) return '--:--'
  let mins = Math.floor(secPerKm / 60)
  let secs = Math.round(secPerKm % 60)
  if (secs === 60) { mins++; secs = 0 }
  return `${mins}:${secs.toString().padStart(2, '0')} /km`
}

const zoneColors = ['#22c55e', '#84cc16', '#eab308', '#f97316', '#ef4444']

export default function TrainingDetail() {
  const { id } = useParams<{ id: string }>()
  const { user } = useAuth()
  const navigate = useNavigate()
  const [workout, setWorkout] = useState<Workout | null>(null)
  const [zones, setZones] = useState<ZoneDistribution[]>([])
  const [similar, setSimilar] = useState<Workout[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  const [editTitle, setEditTitle] = useState('')
  const [editTags, setEditTags] = useState('')
  const [saving, setSaving] = useState(false)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [insights, setInsights] = useState<CachedInsights | null>(null)
  const [insightsLoading, setInsightsLoading] = useState(false)
  const [insightsError, setInsightsError] = useState('')
  const [insightsOpen, setInsightsOpen] = useState(true)

  useEffect(() => {
    setInsights(null)
    setInsightsError('')
    setInsightsLoading(false)
    setInsightsOpen(true)
  }, [id])

  useEffect(() => {
    if (!user || !id) return
    async function run() {
      try {
        const [wRes, zRes, sRes] = await Promise.all([
          fetch(`/api/training/workouts/${id}`, { credentials: 'include' }),
          fetch(`/api/training/workouts/${id}/zones`, { credentials: 'include' }),
          fetch(`/api/training/workouts/${id}/similar`, { credentials: 'include' }),
        ])

        if (!wRes.ok) {
          setError('Workout not found')
          return
        }
        const wData = await wRes.json()
        setWorkout(wData.workout)
        setEditTitle(wData.workout.title)
        setEditTags((wData.workout.tags || []).filter((t: string) => !t.startsWith('auto:')).join(', '))

        if (zRes.ok) {
          const zData = await zRes.json()
          setZones(zData.zones || [])
        }
        if (sRes.ok) {
          const sData = await sRes.json()
          setSimilar(sData.similar || [])
        }
      } catch {
        setError('Failed to load workout')
      } finally {
        setLoading(false)
      }
    }
    run()
  }, [user, id])

  const handleSave = async () => {
    if (!workout) return
    setSaving(true)
    try {
      const res = await fetch(`/api/training/workouts/${workout.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title: editTitle,
          tags: editTags.split(',').map((t) => t.trim()).filter(Boolean),
        }),
      })
      if (res.ok) {
        const data = await res.json()
        setWorkout(data.workout)
        setEditing(false)
      } else {
        const data = await res.json().catch(() => ({})) as { error?: string }
        setError(data.error || 'Failed to save')
      }
    } catch {
      setError('Failed to save')
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!workout) return
    try {
      const res = await fetch(`/api/training/workouts/${workout.id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (res.ok) {
        navigate('/training')
      } else {
        const data = await res.json().catch(() => ({})) as { error?: string }
        setError(data.error || 'Failed to delete')
        setShowDeleteConfirm(false)
      }
    } catch {
      setError('Failed to delete')
    }
  }

  const handleInsights = async () => {
    if (!workout) return
    setInsightsLoading(true)
    setInsightsError('')
    try {
      const res = await fetch(`/api/training/workouts/${workout.id}/insights`, {
        method: 'POST',
        credentials: 'include',
      })
      if (res.ok) {
        const data = await res.json()
        setInsights(data.insights)
      } else {
        const data = await res.json().catch(() => ({})) as { error?: string }
        setInsightsError(data.error || 'Failed to generate insights')
      }
    } catch {
      setInsightsError('Failed to generate insights')
    } finally {
      setInsightsLoading(false)
    }
  }

  if (loading) {
    return (
      <div className="max-w-4xl mx-auto px-4 py-8">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-gray-800 rounded w-64" />
          <div className="h-48 bg-gray-800 rounded" />
        </div>
      </div>
    )
  }

  if (error || !workout) {
    return (
      <div className="max-w-4xl mx-auto px-4 py-8">
        <Link to="/training" className="flex items-center gap-2 text-gray-400 hover:text-white mb-4">
          <ArrowLeft size={16} /> Back to training
        </Link>
        <p className="text-red-400">{error || 'Workout not found'}</p>
      </div>
    )
  }

  const date = new Date(workout.started_at)

  return (
    <div className="max-w-4xl mx-auto px-4 py-8">
      {/* Header */}
      <Link to="/training" className="flex items-center gap-2 text-gray-400 hover:text-white mb-4 text-sm">
        <ArrowLeft size={16} /> Back to training
      </Link>

      <div className="flex items-start justify-between mb-6">
        <div>
          {editing ? (
            <div className="space-y-2">
              <input
                value={editTitle}
                onChange={(e) => setEditTitle(e.target.value)}
                className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-lg font-bold w-full focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              {workout.tags?.some((t) => t.startsWith('auto:')) && (
                <div className="flex gap-1 flex-wrap">
                  {workout.tags.filter((t) => t.startsWith('auto:')).map((tag) => (
                    <TagBadge key={tag} tag={tag} />
                  ))}
                </div>
              )}
              <input
                value={editTags}
                onChange={(e) => setEditTags(e.target.value)}
                placeholder="Tags (comma separated): 6x6, intervals"
                className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm w-full focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <div className="flex gap-2">
                <button
                  onClick={handleSave}
                  disabled={saving}
                  className="flex items-center gap-1 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 rounded-lg text-sm disabled:opacity-50"
                >
                  <Save size={14} /> Save
                </button>
                <button
                  onClick={() => setEditing(false)}
                  className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm"
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <>
              <h1 className="text-2xl font-bold mb-1">{workout.title}</h1>
              <p className="text-gray-400">
                {date.toLocaleDateString(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })}
                {' · '}
                {date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}
              </p>
              {workout.tags && workout.tags.length > 0 && (
                <div className="flex gap-1 mt-2 flex-wrap">
                  {workout.tags.map((tag) => (
                    <TagBadge key={tag} tag={tag} />
                  ))}
                </div>
              )}
            </>
          )}
        </div>
        {!editing && (
          <div className="flex gap-2">
            <button
              onClick={() => setEditing(true)}
              className="px-3 py-1.5 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm"
            >
              Edit
            </button>
            <button
              onClick={() => setShowDeleteConfirm(true)}
              className="p-1.5 text-gray-400 hover:text-red-400 rounded-lg transition-colors"
              title="Delete workout"
            >
              <Trash2 size={18} />
            </button>
          </div>
        )}
      </div>

      {/* Delete confirmation */}
      {showDeleteConfirm && (
        <div className="mb-4 p-4 bg-red-500/10 border border-red-500/20 rounded-lg">
          <p className="text-red-400 mb-3">Delete this workout? This cannot be undone.</p>
          <div className="flex gap-2">
            <button onClick={handleDelete} className="px-4 py-2 bg-red-600 hover:bg-red-700 rounded-lg text-sm">
              Delete
            </button>
            <button onClick={() => setShowDeleteConfirm(false)} className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm">
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Summary stats */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-6">
        <StatCard label="Duration" value={formatDuration(workout.duration_seconds)} />
        <StatCard label="Distance" value={formatDistance(workout.distance_meters)} />
        {workout.avg_heart_rate > 0 && (
          <StatCard label="Avg HR" value={`${workout.avg_heart_rate} bpm`} sub={`Max: ${workout.max_heart_rate}`} />
        )}
        {workout.avg_pace_sec_per_km > 0 && (
          <StatCard label="Avg Pace" value={formatPace(workout.avg_pace_sec_per_km)} />
        )}
        {workout.calories > 0 && <StatCard label="Calories" value={`${workout.calories}`} />}
        {workout.ascent_meters > 0 && (
          <StatCard label="Elevation" value={`↑${Math.round(workout.ascent_meters)}m`} sub={`↓${Math.round(workout.descent_meters)}m`} />
        )}
        {workout.avg_cadence > 0 && <StatCard label="Cadence" value={`${workout.avg_cadence} spm`} />}
      </div>

      {/* Charts */}
      {workout.samples && workout.samples.points.length > 0 && (
        <div className="space-y-6 mb-6">
          <div className="bg-gray-800 rounded-xl p-6">
            <h2 className="text-lg font-semibold mb-4">Heart Rate</h2>
            <WorkoutHRChart samples={workout.samples.points} avgHeartRate={workout.avg_heart_rate} />
          </div>
          <div className="bg-gray-800 rounded-xl p-6">
            <h2 className="text-lg font-semibold mb-4">Pace</h2>
            <WorkoutPaceChart samples={workout.samples.points} avgPaceSecPerKm={workout.avg_pace_sec_per_km} />
          </div>
        </div>
      )}

      {/* HR Zone Distribution */}
      {zones.length > 0 && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          <h2 className="text-lg font-semibold mb-4">HR Zone Distribution</h2>
          <div className="space-y-2">
            {zones.map((z, i) => {
              const isFirstZone = i === 0
              const isLastZone = i === zones.length - 1
              const bpmRange = isLastZone
                ? `>${z.min_hr}`
                : isFirstZone
                  ? `<${z.max_hr}`
                  : `${z.min_hr}–<${z.max_hr}`
              const totalSecs = Math.round(z.duration_seconds ?? 0)
              const mins = Math.floor(totalSecs / 60)
              const secs = totalSecs % 60
              const timeStr = `${mins}m ${String(secs).padStart(2, '0')}s`
              return (
                <div key={z.zone} className="flex items-center gap-3">
                  <span className="text-xs text-gray-400 w-24 shrink-0">Z{z.zone} {z.name}</span>
                  <span className="text-xs text-gray-500 w-20 shrink-0 tabular-nums">{bpmRange} bpm</span>
                  <div className="flex-1 bg-gray-700 rounded-full h-5 overflow-hidden">
                    <div
                      className="h-full rounded-full transition-all"
                      style={{
                        width: `${Math.max(z.percentage, 1)}%`,
                        backgroundColor: zoneColors[i] || '#6b7280',
                      }}
                    />
                  </div>
                  <span className="text-xs text-gray-400 w-12 text-right shrink-0">{z.percentage.toFixed(1)}%</span>
                  <span className="text-xs text-gray-500 w-16 text-right shrink-0 tabular-nums">{timeStr}</span>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* Training Insights (AI) */}
      {user?.is_admin && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          {!insights && !insightsLoading && (
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-semibold flex items-center gap-2">
                  <Sparkles size={18} />
                  Training Insights
                </h2>
                <p className="text-sm text-gray-400 mt-1">Get AI-powered coaching feedback for this workout</p>
              </div>
              <button
                onClick={handleInsights}
                className="px-4 py-2 bg-purple-600 hover:bg-purple-700 rounded-lg text-sm font-medium flex items-center gap-2 transition-colors"
              >
                <Sparkles size={16} />
                Analyze Workout
              </button>
            </div>
          )}
          {insightsError && (
            <p className="text-red-400 text-sm mt-2">{insightsError}</p>
          )}
          {insightsLoading && (
            <div className="flex items-center gap-3 py-4">
              <div className="animate-spin rounded-full h-5 w-5 border-2 border-purple-500 border-t-transparent" />
              <span className="text-gray-400 text-sm">Analyzing workout with AI... this may take a moment</span>
            </div>
          )}
          {insights && (
            <div>
              <button
                onClick={() => setInsightsOpen(!insightsOpen)}
                aria-expanded={insightsOpen}
                aria-controls="insights-content"
                className="flex items-center justify-between w-full"
              >
                <h2 className="text-lg font-semibold flex items-center gap-2">
                  <Sparkles size={18} className="text-purple-400" />
                  Training Insights
                </h2>
                <div className="flex items-center gap-2">
                  {insights.cached && (
                    <span className="text-xs text-gray-500">cached</span>
                  )}
                  {insightsOpen ? <ChevronUp size={18} className="text-gray-400" /> : <ChevronDown size={18} className="text-gray-400" />}
                </div>
              </button>
              {insightsOpen && (
                <div id="insights-content" className="mt-4 space-y-4">
                  <div>
                    <h3 className="text-sm font-medium text-purple-400 mb-1">Effort Summary</h3>
                    <p className="text-sm text-gray-300">{insights.effort_summary}</p>
                  </div>
                  <div>
                    <h3 className="text-sm font-medium text-purple-400 mb-1">Pacing Analysis</h3>
                    <p className="text-sm text-gray-300">{insights.pacing_analysis}</p>
                  </div>
                  <div>
                    <h3 className="text-sm font-medium text-purple-400 mb-1">HR Zones</h3>
                    <p className="text-sm text-gray-300">{insights.hr_zones}</p>
                  </div>
                  {insights.observations && insights.observations.length > 0 && (
                    <div>
                      <h3 className="text-sm font-medium text-purple-400 mb-1">Observations</h3>
                      <ul className="list-disc list-inside space-y-1">
                        {insights.observations.map((obs, i) => (
                          <li key={i} className="text-sm text-gray-300">{obs}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                  {insights.suggestions && insights.suggestions.length > 0 && (
                    <div>
                      <h3 className="text-sm font-medium text-purple-400 mb-1">Suggestions</h3>
                      <ul className="list-disc list-inside space-y-1">
                        {insights.suggestions.map((sug, i) => (
                          <li key={i} className="text-sm text-gray-300">{sug}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                  <p className="text-xs text-gray-600 mt-2">
                    Generated by {insights.model} · {new Date(insights.created_at).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}
                  </p>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* Laps */}
      {workout.laps && workout.laps.length > 1 && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          <h2 className="text-lg font-semibold mb-4">Laps / Intervals</h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-gray-400 border-b border-gray-700">
                  <th className="text-left py-2 pr-4">#</th>
                  <th className="text-right py-2 px-4">Duration</th>
                  <th className="text-right py-2 px-4">Distance</th>
                  <th className="text-right py-2 px-4">Avg HR</th>
                  <th className="text-right py-2 px-4">Max HR</th>
                  <th className="text-right py-2 pl-4">Pace</th>
                </tr>
              </thead>
              <tbody>
                {workout.laps.map((lap) => (
                  <tr key={lap.lap_number} className="border-b border-gray-700/50">
                    <td className="py-2 pr-4 text-gray-400">{lap.lap_number}</td>
                    <td className="py-2 px-4 text-right">{formatDuration(Math.round(lap.duration_seconds))}</td>
                    <td className="py-2 px-4 text-right">{formatDistance(lap.distance_meters)}</td>
                    <td className="py-2 px-4 text-right">{lap.avg_heart_rate > 0 ? lap.avg_heart_rate : '-'}</td>
                    <td className="py-2 px-4 text-right">{lap.max_heart_rate > 0 ? lap.max_heart_rate : '-'}</td>
                    <td className="py-2 pl-4 text-right">{formatPace(lap.avg_pace_sec_per_km)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Similar workouts */}
      {similar.length > 0 && (
        <div className="bg-gray-800 rounded-xl p-6">
          <h2 className="text-lg font-semibold mb-4 flex items-center gap-2">
            <GitCompareArrows size={18} />
            Similar Workouts
          </h2>
          <div className="space-y-2">
            {similar.map((s) => (
              <div key={s.id} className="flex items-center justify-between">
                <Link
                  to={`/training/${s.id}`}
                  className="text-sm text-blue-400 hover:text-blue-300"
                >
                  {s.title}
                </Link>
                <Link
                  to={`/training/compare?a=${workout.id}&b=${s.id}`}
                  className="text-xs bg-gray-700 hover:bg-gray-600 px-3 py-1 rounded-lg"
                >
                  Compare
                </Link>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function StatCard({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="bg-gray-800 rounded-xl p-4">
      <p className="text-xs text-gray-500 mb-1">{label}</p>
      <p className="text-lg font-bold">{value}</p>
      {sub && <p className="text-xs text-gray-400">{sub}</p>}
    </div>
  )
}
