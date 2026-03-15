import { useState, useEffect, useCallback } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { ArrowLeft, GitCompareArrows, ListChecks } from 'lucide-react'
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
import type { Workout, Lap, ComparisonResult } from '../types/training'

function formatPace(secPerKm: number): string {
  if (secPerKm <= 0) return '--:--'
  let mins = Math.floor(secPerKm / 60)
  let secs = Math.round(secPerKm % 60)
  if (secs === 60) { mins++; secs = 0 }
  return `${mins}:${secs.toString().padStart(2, '0')}`
}

function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = Math.round(seconds % 60)
  return `${m}:${s.toString().padStart(2, '0')}`
}

function LapPicker({
  label,
  laps,
  selected,
  onToggle,
  color,
}: {
  label: string
  laps: Lap[]
  selected: number[]
  onToggle: (index: number) => void
  color: string
}) {
  return (
    <div>
      <h3 className={`text-sm font-medium mb-2 ${color}`}>{label}</h3>
      <div className="space-y-1">
        {laps.map((lap, idx) => {
          const pos = selected.indexOf(idx)
          const isSelected = pos !== -1
          return (
            <button
              key={lap.id}
              type="button"
              onClick={() => onToggle(idx)}
              className={`w-full text-left px-3 py-2 rounded-lg text-sm flex items-center gap-3 transition-colors ${
                isSelected
                  ? 'bg-gray-600 ring-1 ring-gray-400'
                  : 'bg-gray-800 hover:bg-gray-700'
              }`}
            >
              <span className={`w-5 h-5 rounded flex items-center justify-center text-xs font-bold shrink-0 ${
                isSelected ? 'bg-blue-500 text-white' : 'bg-gray-700 text-gray-500'
              }`}>
                {isSelected ? pos + 1 : ''}
              </span>
              <span className="text-gray-300">Lap {lap.lap_number}</span>
              <span className="ml-auto text-gray-500 text-xs tabular-nums">
                {formatDuration(lap.duration_seconds)} · {formatPace(lap.avg_pace_sec_per_km)} /km · {lap.avg_heart_rate} bpm
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
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
  const [error, setError] = useState<string | null>(null)

  // Lap selection state
  const [lapSelectMode, setLapSelectMode] = useState(false)
  const [pickedLapsA, setPickedLapsA] = useState<number[]>([])
  const [pickedLapsB, setPickedLapsB] = useState<number[]>([])

  const lapsA = workoutA?.laps ?? []
  const lapsB = workoutB?.laps ?? []
  const hasMismatchedLaps = lapsA.length > 0 && lapsB.length > 0 && lapsA.length !== lapsB.length

  // Reset lap selection when workouts change
  useEffect(() => {
    setLapSelectMode(false)
    setPickedLapsA([])
    setPickedLapsB([])
  }, [selectedA, selectedB])

  useEffect(() => {
    if (!user) return
    const load = async () => {
      try {
        const res = await fetch('/api/training/workouts', { credentials: 'include' })
        if (res.ok) {
          const data = await res.json()
          setWorkouts(data.workouts || [])
        } else {
          setError('Failed to load workouts')
        }
      } catch {
        setError('Failed to load workouts')
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [user])

  const runComparison = useCallback(async (lapsAParam?: number[], lapsBParam?: number[]) => {
    if (!selectedA || !selectedB || selectedA === selectedB) return
    setComparing(true)
    setComparison(null)
    setError(null)
    try {
      let compareUrl = `/api/training/compare?a=${selectedA}&b=${selectedB}`
      if (lapsAParam && lapsBParam) {
        compareUrl += `&laps_a=${lapsAParam.join(',')}&laps_b=${lapsBParam.join(',')}`
      }
      const [cRes, aRes, bRes] = await Promise.all([
        fetch(compareUrl, { credentials: 'include' }),
        fetch(`/api/training/workouts/${selectedA}`, { credentials: 'include' }),
        fetch(`/api/training/workouts/${selectedB}`, { credentials: 'include' }),
      ])
      if (cRes.ok) {
        const cData = await cRes.json()
        setComparison(cData.comparison)
      } else {
        setError('Failed to load comparison')
      }
      if (aRes.ok) {
        const aData = await aRes.json()
        setWorkoutA(aData.workout)
      }
      if (bRes.ok) {
        const bData = await bRes.json()
        setWorkoutB(bData.workout)
      }
    } catch {
      setError('Failed to compare workouts')
    } finally {
      setComparing(false)
    }
  }, [selectedA, selectedB])

  // Auto-compare when workouts are selected
  useEffect(() => {
    if (!selectedA || !selectedB || selectedA === selectedB) return
    runComparison()
  }, [selectedA, selectedB, runComparison])

  function toggleLap(side: 'a' | 'b', index: number) {
    const setter = side === 'a' ? setPickedLapsA : setPickedLapsB
    setter(prev => {
      const pos = prev.indexOf(index)
      if (pos !== -1) return prev.filter(i => i !== index)
      return [...prev, index]
    })
  }

  function handleCompareSelected() {
    if (pickedLapsA.length === 0 || pickedLapsA.length !== pickedLapsB.length) return
    runComparison(pickedLapsA, pickedLapsB)
  }

  // Build HR overlay chart data from both workouts' samples.
  const overlayData = (() => {
    if (!workoutA?.samples?.points.length || !workoutB?.samples?.points.length) return []
    const maxLen = Math.max(workoutA.samples.points.length, workoutB.samples.points.length)
    const step = Math.max(1, Math.floor(maxLen / 300))
    const data: { time: number; hrA?: number; hrB?: number }[] = []

    const samplesA = workoutA.samples.points
    const samplesB = workoutB.samples.points

    for (let i = 0; i < maxLen; i += step) {
      // Use the actual sample timestamp (ms → minutes) so the x-axis reflects true
      // elapsed time regardless of recording interval, avoiding repeated/non-monotonic values.
      const tMs = i < samplesA.length ? samplesA[i].t : samplesB[i].t
      const point: { time: number; hrA?: number; hrB?: number } = {
        time: tMs / 60000,
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

  const canCompareSelected = pickedLapsA.length > 0 && pickedLapsA.length === pickedLapsB.length
  const showLapPicker = lapSelectMode && lapsA.length > 0 && lapsB.length > 0

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

      {error && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
          {error}
        </div>
      )}

      {comparing && <p className="text-gray-400 mb-4">Comparing...</p>}

      {comparison && !comparison.compatible && (
        <div className="bg-yellow-500/10 border border-yellow-500/20 rounded-xl p-4 mb-6">
          <p className="text-yellow-400 mb-3">Workouts are not directly comparable: {comparison.reason}</p>
          {hasMismatchedLaps && !lapSelectMode && (
            <button
              type="button"
              onClick={() => setLapSelectMode(true)}
              className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm font-medium rounded-lg transition-colors"
            >
              <ListChecks size={16} />
              Pick laps to compare
            </button>
          )}
        </div>
      )}

      {/* Manual lap selection toggle for compatible workouts */}
      {comparison?.compatible && lapsA.length > 0 && lapsB.length > 0 && !lapSelectMode && (
        <div className="mb-6">
          <button
            type="button"
            onClick={() => setLapSelectMode(true)}
            className="flex items-center gap-2 text-sm text-gray-400 hover:text-white transition-colors"
          >
            <ListChecks size={16} />
            Select specific laps to compare
          </button>
        </div>
      )}

      {/* Side-by-side lap picker */}
      {showLapPicker && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold">Select Laps to Compare</h2>
            <button
              type="button"
              onClick={() => {
                setLapSelectMode(false)
                setPickedLapsA([])
                setPickedLapsB([])
                if (comparison?.compatible) return
                // If was incompatible, no need to re-run — keep existing state
              }}
              className="text-sm text-gray-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
          </div>
          <p className="text-sm text-gray-400 mb-4">
            Select the same number of laps from each workout. Laps are paired in the order you select them.
            {pickedLapsA.length !== pickedLapsB.length && pickedLapsA.length + pickedLapsB.length > 0 && (
              <span className="text-yellow-400 ml-1">
                — Select {pickedLapsA.length > pickedLapsB.length
                  ? `${pickedLapsA.length - pickedLapsB.length} more from B`
                  : `${pickedLapsB.length - pickedLapsA.length} more from A`
                } to match.
              </span>
            )}
          </p>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <LapPicker
              label={`Workout A — ${comparison?.workout_a.title ?? 'Loading...'} (${lapsA.length} laps)`}
              laps={lapsA}
              selected={pickedLapsA}
              onToggle={(idx) => toggleLap('a', idx)}
              color="text-blue-400"
            />
            <LapPicker
              label={`Workout B — ${comparison?.workout_b.title ?? 'Loading...'} (${lapsB.length} laps)`}
              laps={lapsB}
              selected={pickedLapsB}
              onToggle={(idx) => toggleLap('b', idx)}
              color="text-orange-400"
            />
          </div>

          <div className="mt-4 flex items-center gap-3">
            <button
              type="button"
              onClick={handleCompareSelected}
              disabled={!canCompareSelected || comparing}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-600 disabled:cursor-not-allowed text-white text-sm font-medium rounded-lg transition-colors"
            >
              Compare {canCompareSelected ? `${pickedLapsA.length} lap${pickedLapsA.length > 1 ? 's' : ''}` : 'selected laps'}
            </button>
            {canCompareSelected && (
              <span className="text-xs text-gray-500">
                Pairing: {pickedLapsA.map((a, i) => `A${lapsA[a].lap_number}↔B${lapsB[pickedLapsB[i]].lap_number}`).join(', ')}
              </span>
            )}
          </div>
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
                        <td className="py-2 pr-4 text-gray-400">
                          {d.lap_number_a !== d.lap_number_b
                            ? `A${d.lap_number_a} ↔ B${d.lap_number_b}`
                            : d.lap_number}
                        </td>
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
