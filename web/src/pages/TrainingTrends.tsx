import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, TrendingUp, Sparkles } from 'lucide-react'
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  BarChart,
  Bar,
} from 'recharts'
import { useAuth } from '../auth'
import type { WeeklySummary, ProgressionGroup } from '../types/training'

function formatDuration(seconds: number): string {
  const h = (seconds / 3600).toFixed(1)
  return `${h}h`
}

function formatPace(secPerKm: number): string {
  if (secPerKm <= 0) return '--:--'
  let mins = Math.floor(secPerKm / 60)
  let secs = Math.round(secPerKm % 60)
  if (secs === 60) { mins++; secs = 0 }
  return `${mins}:${secs.toString().padStart(2, '0')}`
}

export default function TrainingTrends() {
  const { user } = useAuth()
  const [summaries, setSummaries] = useState<WeeklySummary[]>([])
  const [groups, setGroups] = useState<ProgressionGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedGroup, setSelectedGroup] = useState<string>('')

  useEffect(() => {
    if (!user) return
    const load = async () => {
      try {
        const [sRes, pRes] = await Promise.all([
          fetch('/api/training/summary', { credentials: 'include' }),
          fetch('/api/training/progression', { credentials: 'include' }),
        ])
        if (sRes.ok) {
          const sData = await sRes.json()
          setSummaries(sData.summaries || [])
        } else {
          setError('Failed to load summaries')
        }
        if (pRes.ok) {
          const pData = await pRes.json()
          setGroups(pData.groups || [])
          if (pData.groups?.length > 0) {
            setSelectedGroup(`${pData.groups[0].tag}:${pData.groups[0].sport}:${pData.groups[0].lap_count}`)
          }
        } else {
          setError('Failed to load progression data')
        }
      } catch {
        setError('Failed to load trend data')
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [user])

  const groupKey = (g: ProgressionGroup) => `${g.tag}:${g.sport}:${g.lap_count}`
  const tagCounts = groups.reduce((acc, g) => { acc[g.tag] = (acc[g.tag] || 0) + 1; return acc }, {} as Record<string, number>)
  const displayTag = (tag: string) => tag.startsWith('auto:') ? tag.slice(5) : tag
  const activeGroup = groups.find((g) => groupKey(g) === selectedGroup)

  // Prepare weekly volume chart data (most recent first, reverse for chart).
  const volumeData = summaries
    .slice()
    .reverse()
    .map((s) => ({
      week: new Date(s.week_start + 'T00:00:00').toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
      hours: Number((s.total_duration_seconds / 3600).toFixed(1)),
      km: Number((s.total_distance_meters / 1000).toFixed(1)),
      count: s.workout_count,
    }))

  // Prepare progression chart data.
  const progressionData = activeGroup?.workouts.map((w) => ({
    date: new Date(w.date).toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
    avgHR: w.avg_hr,
    avgPace: w.avg_pace_sec_per_km,
  })) || []

  if (loading) {
    return (
      <div className="max-w-4xl mx-auto px-4 py-8">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-gray-800 rounded w-48" />
          <div className="h-64 bg-gray-800 rounded" />
        </div>
      </div>
    )
  }

  return (
    <>
      {error && (
        <div className="max-w-4xl mx-auto px-4 pt-4">
          <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">{error}</div>
        </div>
      )}
    <div className="max-w-4xl mx-auto px-4 py-8">
      <Link to="/training" className="flex items-center gap-2 text-gray-400 hover:text-white mb-4 text-sm">
        <ArrowLeft size={16} /> Back to training
      </Link>

      <div className="flex items-center gap-3 mb-6">
        <TrendingUp size={24} className="text-green-400" />
        <h1 className="text-2xl font-bold">Training Trends</h1>
      </div>

      {/* Weekly volume */}
      {volumeData.length > 0 && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          <h2 className="text-lg font-semibold mb-4">Weekly Volume</h2>
          <div className="w-full h-64" role="img" aria-label="Weekly training volume">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={volumeData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="week" tick={{ fill: '#9ca3af', fontSize: 10 }} />
                <YAxis tick={{ fill: '#9ca3af', fontSize: 11 }} label={{ value: 'Hours', angle: -90, position: 'insideLeft', fill: '#9ca3af', fontSize: 11 }} />
                <Tooltip
                  contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px', color: '#e5e7eb' }}
                  formatter={(value, name) => {
                    if (name === 'hours') return [formatDuration(Number(value) * 3600), 'Duration']
                    if (name === 'km') return [`${value} km`, 'Distance']
                    return [value, name]
                  }}
                />
                <Bar dataKey="hours" fill="#3b82f6" radius={[4, 4, 0, 0]} name="hours" />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Progression by tag */}
      {groups.length > 0 && (
        <div className="bg-gray-800 rounded-xl p-6">
          <h2 className="text-lg font-semibold mb-4">Progression by Workout Type</h2>

          <div className="flex gap-2 mb-4 flex-wrap">
            {groups.map((g) => {
              const key = groupKey(g)
              const isAuto = g.tag.startsWith('auto:')
              const tagName = displayTag(g.tag)
              const label = tagCounts[g.tag] > 1 ? `${tagName} (${g.sport}, ${g.lap_count}L)` : tagName
              return (
              <button
                key={key}
                onClick={() => setSelectedGroup(key)}
                aria-pressed={selectedGroup === key}
                title={isAuto ? 'Auto-generated from workout structure' : undefined}
                className={`inline-flex items-center gap-1 px-3 py-1.5 text-xs rounded-lg transition-colors ${
                  selectedGroup === key
                    ? 'bg-blue-500/20 text-blue-400 border border-blue-500/40'
                    : 'bg-gray-700 text-gray-400 hover:text-white border border-gray-600'
                }`}
              >
                {isAuto && <Sparkles size={10} />}
                {label} ({g.workouts.length})
              </button>
              )
            })}
          </div>

          {progressionData.length > 1 && (
            <div className="space-y-6">
              {/* HR trend */}
              <div>
                <h3 className="text-sm text-gray-400 mb-2">Average Heart Rate Trend</h3>
                <div className="w-full h-48" role="img" aria-label="Heart rate progression trend">
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={progressionData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                      <XAxis dataKey="date" tick={{ fill: '#9ca3af', fontSize: 10 }} />
                      <YAxis domain={['dataMin - 5', 'dataMax + 5']} tick={{ fill: '#9ca3af', fontSize: 11 }} />
                      <Tooltip
                        contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px', color: '#e5e7eb' }}
                        formatter={(value) => [`${Number(value)} bpm`, 'Avg HR']}
                      />
                      <Line type="monotone" dataKey="avgHR" stroke="#ef4444" strokeWidth={2} dot={{ r: 3, fill: '#ef4444' }} name="Avg HR" />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
                <p className="text-xs text-gray-500 mt-1">
                  Decreasing HR at the same effort = improving fitness
                </p>
              </div>

              {/* Pace trend */}
              <div>
                <h3 className="text-sm text-gray-400 mb-2">Average Pace Trend</h3>
                <div className="w-full h-48" role="img" aria-label="Pace progression trend">
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={progressionData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                      <XAxis dataKey="date" tick={{ fill: '#9ca3af', fontSize: 10 }} />
                      <YAxis
                        reversed
                        domain={['dataMin - 5', 'dataMax + 5']}
                        tick={{ fill: '#9ca3af', fontSize: 11 }}
                        tickFormatter={(v: number) => formatPace(v)}
                      />
                      <Tooltip
                        contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px', color: '#e5e7eb' }}
                        formatter={(value) => [formatPace(Number(value)), 'Avg Pace']}
                      />
                      <Line type="monotone" dataKey="avgPace" stroke="#3b82f6" strokeWidth={2} dot={{ r: 3, fill: '#3b82f6' }} name="Avg Pace" />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
                <p className="text-xs text-gray-500 mt-1">
                  Faster pace at the same HR = improving fitness
                </p>
              </div>
            </div>
          )}

          {progressionData.length <= 1 && activeGroup && (
            <p className="text-gray-500 text-sm">
              Need at least 2 workouts tagged &quot;{displayTag(activeGroup.tag)}&quot; to show trends.
            </p>
          )}
        </div>
      )}

      {groups.length === 0 && summaries.length === 0 && (
        <div className="bg-gray-800 rounded-xl p-12 text-center">
          <TrendingUp size={48} className="mx-auto mb-4 text-gray-600" />
          <h2 className="text-xl font-semibold mb-2">No trend data yet</h2>
          <p className="text-gray-400">Import workouts and add tags to see progression trends</p>
        </div>
      )}
    </div>
    </>
  )
}
