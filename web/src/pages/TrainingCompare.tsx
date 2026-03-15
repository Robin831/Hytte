import { useState, useEffect } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { ArrowLeft, GitCompareArrows } from 'lucide-react'
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts'
import { useAuth } from '../auth'
import type { Workout, ComparisonResult } from '../types/training'

function formatPace(secPerKm: number): string {
  if (secPerKm <= 0) return '--:--'
  const mins = Math.floor(secPerKm / 60)
  const secs = Math.round(secPerKm % 60)
  return `${mins}:${secs.toString().padStart(2, '0')}`
}

export default function TrainingCompare() {
  const { user } = useAuth()
  const [searchParams] = useSearchParams()
  const [workouts, setWorkouts] = useState<Workout[]>([])
  const [selectedA, setSelectedA] = useState(searchParams.get('a') || '')
  const [selectedB, setSelectedB] = useState(searchParams.get('b') || '')
  const [comparison, setComparison] = useState<ComparisonResult | null>(null)
  const [workoutA, setWorkoutA] = useState<Workout | null>(null)
  const [workoutB, setWorkoutB] = useState<Workout | null>(null)
  const [loading, setLoading] = useState(true)
  const [comparing, setComparing] = useState(false)

  useEffect(() => {
    if (!user) return
    const load = async () => {
      try {
        const res = await fetch('/api/training/workouts', { credentials: 'include' })
        if (res.ok) {
          const data = await res.json()
          setWorkouts(data.workouts || [])
        }
      } catch { /* ignore */ }
      setLoading(false)
    }
    load()
  }, [user])

  useEffect(() => {
    if (!selectedA || !selectedB || selectedA === selectedB) return
    async function run() {
      setComparing(true)
      setComparison(null)
      try {
        const [cRes, aRes, bRes] = await Promise.all([
          fetch(`/api/training/compare?a=${selectedA}&b=${selectedB}`, { credentials: 'include' }),
          fetch(`/api/training/workouts/${selectedA}`, { credentials: 'include' }),
          fetch(`/api/training/workouts/${selectedB}`, { credentials: 'include' }),
        ])
        if (cRes.ok) {
          const cData = await cRes.json()
          setComparison(cData.comparison)
        }
        if (aRes.ok) {
          const aData = await aRes.json()
          setWorkoutA(aData.workout)
        }
        if (bRes.ok) {
          const bData = await bRes.json()
          setWorkoutB(bData.workout)
        }
      } catch { /* ignore */ }
      setComparing(false)
    }
    run()
  }, [selectedA, selectedB])

  // Build HR overlay chart data from both workouts' samples.
  const overlayData = (() => {
    if (!workoutA?.samples?.points.length || !workoutB?.samples?.points.length) return []
    const maxLen = Math.max(workoutA.samples.points.length, workoutB.samples.points.length)
    const step = Math.max(1, Math.floor(maxLen / 300))
    const data: { time: number; hrA?: number; hrB?: number }[] = []

    const samplesA = workoutA.samples.points
    const samplesB = workoutB.samples.points

    for (let i = 0; i < maxLen; i += step) {
      const point: { time: number; hrA?: number; hrB?: number } = {
        time: Math.round((samplesA[Math.min(i, samplesA.length - 1)]?.t || i * 1000) / 60000),
      }
      if (i < samplesA.length && samplesA[i].hr) point.hrA = samplesA[i].hr
      if (i < samplesB.length && samplesB[i].hr) point.hrB = samplesB[i].hr
      data.push(point)
    }
    return data
  })()

  if (loading) {
    return (
      <div className="max-w-4xl mx-auto px-4 py-8">
        <div className="animate-pulse h-48 bg-gray-800 rounded" />
      </div>
    )
  }

  return (
    <div className="max-w-4xl mx-auto px-4 py-8">
      <Link to="/training" className="flex items-center gap-2 text-gray-400 hover:text-white mb-4 text-sm">
        <ArrowLeft size={16} /> Back to training
      </Link>

      <div className="flex items-center gap-3 mb-6">
        <GitCompareArrows size={24} className="text-purple-400" />
        <h1 className="text-2xl font-bold">Compare Workouts</h1>
      </div>

      {/* Selectors */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <div>
          <label className="block text-sm text-gray-400 mb-1">Workout A</label>
          <select
            value={selectedA}
            onChange={(e) => setSelectedA(e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">Select workout...</option>
            {workouts.map((w) => (
              <option key={w.id} value={w.id}>
                {w.title} — {new Date(w.started_at).toLocaleDateString(undefined)}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-sm text-gray-400 mb-1">Workout B</label>
          <select
            value={selectedB}
            onChange={(e) => setSelectedB(e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">Select workout...</option>
            {workouts.map((w) => (
              <option key={w.id} value={w.id}>
                {w.title} — {new Date(w.started_at).toLocaleDateString(undefined)}
              </option>
            ))}
          </select>
        </div>
      </div>

      {comparing && <p className="text-gray-400 mb-4">Comparing...</p>}

      {comparison && !comparison.compatible && (
        <div className="bg-yellow-500/10 border border-yellow-500/20 rounded-xl p-4 mb-6">
          <p className="text-yellow-400">Workouts are not directly comparable: {comparison.reason}</p>
        </div>
      )}

      {comparison?.compatible && comparison.summary && (
        <>
          {/* Verdict */}
          <div className="bg-gray-800 rounded-xl p-6 mb-6">
            <h2 className="text-lg font-semibold mb-2">Summary</h2>
            <p className="text-lg">{comparison.summary.verdict}</p>
            <div className="flex gap-6 mt-3 text-sm text-gray-400">
              <p>
                Avg HR delta:{' '}
                <span className={comparison.summary.avg_hr_delta < 0 ? 'text-green-400' : comparison.summary.avg_hr_delta > 0 ? 'text-red-400' : 'text-white'}>
                  {comparison.summary.avg_hr_delta > 0 ? '+' : ''}{comparison.summary.avg_hr_delta.toFixed(1)} bpm
                </span>
              </p>
              <p>
                Avg pace delta:{' '}
                <span className={comparison.summary.avg_pace_delta < 0 ? 'text-green-400' : comparison.summary.avg_pace_delta > 0 ? 'text-red-400' : 'text-white'}>
                  {comparison.summary.avg_pace_delta > 0 ? '+' : ''}{comparison.summary.avg_pace_delta.toFixed(1)}s /km
                </span>
              </p>
            </div>
          </div>

          {/* Lap comparison table */}
          {comparison.lap_deltas && comparison.lap_deltas.length > 0 && (
            <div className="bg-gray-800 rounded-xl p-6 mb-6">
              <h2 className="text-lg font-semibold mb-4">Interval Comparison</h2>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-gray-400 border-b border-gray-700">
                      <th className="text-left py-2 pr-4">Lap</th>
                      <th className="text-right py-2 px-3">HR (A)</th>
                      <th className="text-right py-2 px-3">HR (B)</th>
                      <th className="text-right py-2 px-3">HR Δ</th>
                      <th className="text-right py-2 px-3">Pace (A)</th>
                      <th className="text-right py-2 px-3">Pace (B)</th>
                      <th className="text-right py-2 pl-3">Pace Δ</th>
                    </tr>
                  </thead>
                  <tbody>
                    {comparison.lap_deltas.map((d) => (
                      <tr key={d.lap_number} className="border-b border-gray-700/50">
                        <td className="py-2 pr-4 text-gray-400">{d.lap_number}</td>
                        <td className="py-2 px-3 text-right">{d.avg_hr_a}</td>
                        <td className="py-2 px-3 text-right">{d.avg_hr_b}</td>
                        <td className={`py-2 px-3 text-right font-medium ${d.hr_delta < 0 ? 'text-green-400' : d.hr_delta > 0 ? 'text-red-400' : ''}`}>
                          {d.hr_delta > 0 ? '+' : ''}{d.hr_delta}
                        </td>
                        <td className="py-2 px-3 text-right">{formatPace(d.pace_a_sec_per_km)}</td>
                        <td className="py-2 px-3 text-right">{formatPace(d.pace_b_sec_per_km)}</td>
                        <td className={`py-2 pl-3 text-right font-medium ${d.pace_delta_sec_per_km < 0 ? 'text-green-400' : d.pace_delta_sec_per_km > 0 ? 'text-red-400' : ''}`}>
                          {d.pace_delta_sec_per_km > 0 ? '+' : ''}{d.pace_delta_sec_per_km.toFixed(1)}s
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* HR Overlay chart */}
          {overlayData.length > 0 && (
            <div className="bg-gray-800 rounded-xl p-6">
              <h2 className="text-lg font-semibold mb-4">Heart Rate Overlay</h2>
              <div className="w-full h-72" role="img" aria-label="Heart rate overlay comparison">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={overlayData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                    <XAxis dataKey="time" tick={{ fill: '#9ca3af', fontSize: 11 }} label={{ value: 'Minutes', position: 'insideBottom', offset: -3, fill: '#9ca3af', fontSize: 11 }} />
                    <YAxis domain={['dataMin - 10', 'dataMax + 10']} tick={{ fill: '#9ca3af', fontSize: 11 }} />
                    <Tooltip contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px', color: '#e5e7eb' }} />
                    <Legend wrapperStyle={{ color: '#9ca3af', fontSize: 12 }} />
                    <Line type="monotone" dataKey="hrA" stroke="#3b82f6" strokeWidth={1.5} dot={false} name={comparison.workout_a.title} />
                    <Line type="monotone" dataKey="hrB" stroke="#f97316" strokeWidth={1.5} dot={false} name={comparison.workout_b.title} />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
