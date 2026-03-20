import { useState, useEffect, useCallback, useRef } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { ArrowLeft, GitCompareArrows, ListChecks, Sparkles, Loader2, RefreshCw, History, Trash2, ExternalLink } from 'lucide-react'
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
import type { Workout, Lap, ComparisonResult, CachedComparisonAnalysis, ComparisonAnalysisSummary } from '../types/training'

function formatPace(secPerKm: number): string {
  if (secPerKm <= 0) return '--:--'
  let mins = Math.floor(secPerKm / 60)
  let secs = Math.round(secPerKm % 60)
  if (secs === 60) { mins++; secs = 0 }
  return `${mins}:${secs.toString().padStart(2, '0')}`
}

function formatDuration(seconds: number): string {
  const total = Math.round(seconds)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

function LapPicker({
  id,
  label,
  laps,
  selected,
  onToggle,
  color,
}: {
  id: string
  label: string
  laps: Lap[]
  selected: number[]
  onToggle: (index: number) => void
  color: string
}) {
  return (
    <div>
      <h3 id={id} className={`text-sm font-medium mb-2 ${color}`}>{label}</h3>
      <div role="group" aria-labelledby={id} className="space-y-1">
        {laps.map((lap, idx) => {
          const pos = selected.indexOf(idx)
          const isSelected = pos !== -1
          const lapNum = lap.lap_number
          return (
            <button
              key={lap.id}
              type="button"
              aria-label={`Lap ${lapNum}${isSelected ? `, selected as pair ${pos + 1}` : ''}`}
              aria-pressed={isSelected}
              onClick={() => onToggle(idx)}
              className={`w-full text-left px-3 py-2 rounded-lg text-sm flex items-center gap-3 transition-colors ${
                isSelected
                  ? 'bg-gray-600 ring-1 ring-gray-400'
                  : 'bg-gray-800 hover:bg-gray-700'
              }`}
            >
              <span aria-hidden="true" className={`w-5 h-5 rounded flex items-center justify-center text-xs font-bold shrink-0 ${
                isSelected ? 'bg-blue-500 text-white' : 'bg-gray-700 text-gray-500'
              }`}>
                {isSelected ? pos + 1 : ''}
              </span>
              <span className="text-gray-300">Lap {lapNum}</span>
              <span className="ml-auto text-gray-500 text-xs tabular-nums">
                {formatDuration(lap.duration_seconds)} · {formatPace(lap.avg_pace_sec_per_km)} /km · {lap.avg_heart_rate > 0 ? `${lap.avg_heart_rate} bpm` : '-'}
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

  // AI analysis state
  const [analysis, setAnalysis] = useState<CachedComparisonAnalysis | null>(null)
  const [analyzing, setAnalyzing] = useState(false)
  const [analysisError, setAnalysisError] = useState('')

  // Previous analyses state
  const [previousAnalyses, setPreviousAnalyses] = useState<ComparisonAnalysisSummary[]>([])
  const [loadingAnalyses, setLoadingAnalyses] = useState(false)
  const [analysesListError, setAnalysesListError] = useState('')
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [deleteError, setDeleteError] = useState('')

  // Lap selection state
  const [lapSelectMode, setLapSelectMode] = useState(false)
  const [pickedLapsA, setPickedLapsA] = useState<number[]>([])
  const [pickedLapsB, setPickedLapsB] = useState<number[]>([])

  const lapsA = workoutA?.laps ?? []
  const lapsB = workoutB?.laps ?? []
  const hasMismatchedLaps = lapsA.length > 0 && lapsB.length > 0 && lapsA.length !== lapsB.length

  // Ref to abort in-flight manual comparison requests
  const manualAbortRef = useRef<AbortController | null>(null)
  // Ref to abort in-flight analysis requests
  const analysisAbortRef = useRef<AbortController | null>(null)
  // Track mounted state to avoid calling setState after unmount
  const mountedRef = useRef(true)
  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      analysisAbortRef.current?.abort()
    }
  }, [])

  // Reset lap selection, analysis, and abort any in-flight manual comparison when workouts change
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLapSelectMode(false)
    setPickedLapsA([])
    setPickedLapsB([])
    setAnalysis(null)
    setAnalysisError('')
    setAnalyzing(false)
    analysisAbortRef.current?.abort()
    analysisAbortRef.current = null
    manualAbortRef.current?.abort()
    manualAbortRef.current = null
  }, [selectedA, selectedB])

  // Abort any in-flight manual comparison on unmount
  useEffect(() => {
    return () => { manualAbortRef.current?.abort() }
  }, [])

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

  const runComparison = useCallback(async function runComparison(
    idA: string,
    idB: string,
    lapsAParam?: number[],
    lapsBParam?: number[],
    signal?: AbortSignal,
  ) {
    if (!idA || !idB || idA === idB) return
    // Lap re-comparisons (lapsAParam present) reuse already-loaded workout data.
    // Initial comparisons always fetch workout details alongside the comparison.
    const isLapRecompare = !!lapsAParam
    setComparing(true)
    // Only clear the previous comparison on initial load; preserve it during lap
    // recomparisons so the page doesn't go blank if the request is aborted or fails.
    if (!isLapRecompare) setComparison(null)
    setError(null)
    try {
      let compareUrl = `/api/training/compare?a=${idA}&b=${idB}`
      if (lapsAParam && lapsBParam) {
        compareUrl += `&laps_a=${lapsAParam.join(',')}&laps_b=${lapsBParam.join(',')}`
      }

      const fetches: Promise<Response>[] = [
        fetch(compareUrl, { credentials: 'include', signal }),
      ]
      if (!isLapRecompare) {
        fetches.push(
          fetch(`/api/training/workouts/${idA}`, { credentials: 'include', signal }),
          fetch(`/api/training/workouts/${idB}`, { credentials: 'include', signal }),
        )
      }

      const results = await Promise.all(fetches)
      if (signal?.aborted || !mountedRef.current) return
      const errors: string[] = []
      const cRes = results[0]
      if (cRes.ok) {
        const cData = await cRes.json()
        if (signal?.aborted || !mountedRef.current) return
        setComparison(cData.comparison)
      } else {
        errors.push('Failed to load comparison')
      }
      if (!isLapRecompare) {
        const aRes = results[1]
        const bRes = results[2]
        if (aRes.ok) {
          const aData = await aRes.json()
          if (signal?.aborted || !mountedRef.current) return
          setWorkoutA(aData.workout)
        } else {
          errors.push('Failed to load workout A details')
        }
        if (bRes.ok) {
          const bData = await bRes.json()
          if (signal?.aborted || !mountedRef.current) return
          setWorkoutB(bData.workout)
        } else {
          errors.push('Failed to load workout B details')
        }
      }
      if (errors.length > 0) {
        setError(errors.join('; '))
      }
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError('Failed to compare workouts')
    } finally {
      if (mountedRef.current) {
        setComparing(false)
      }
    }
  }, [mountedRef])

  // Auto-compare when workouts are selected
  useEffect(() => {
    if (!selectedA || !selectedB || selectedA === selectedB) return
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    runComparison(selectedA, selectedB, undefined, undefined, controller.signal)
    return () => controller.abort()
  }, [selectedA, selectedB, runComparison])


  // Fetch previous analyses for admin users
  const fetchPreviousAnalyses = useCallback(async () => {
    if (!user?.is_admin) return
    setLoadingAnalyses(true)
    setAnalysesListError('')
    try {
      const res = await fetch('/api/training/compare/analyses', { credentials: 'include' })
      if (res.ok) {
        const data = await res.json()
        if (mountedRef.current) setPreviousAnalyses(data)
      } else {
        if (mountedRef.current) setAnalysesListError('Failed to load previous analyses')
      }
    } catch {
      if (mountedRef.current) setAnalysesListError('Failed to load previous analyses')
    } finally {
      if (mountedRef.current) setLoadingAnalyses(false)
    }
  }, [user?.is_admin])

  useEffect(() => {
    fetchPreviousAnalyses()
  }, [fetchPreviousAnalyses])

  async function deleteAnalysis(id: number) {
    setDeletingId(id)
    setDeleteError('')
    try {
      const res = await fetch(`/api/training/compare/analyses/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (res.ok || res.status === 204) {
        setPreviousAnalyses(prev => prev.filter(a => a.id !== id))
        // If the deleted analysis matches the current comparison, clear the cached analysis
        const deleted = previousAnalyses.find(a => a.id === id)
        if (deleted && String(deleted.workout_id_a) === selectedA && String(deleted.workout_id_b) === selectedB) {
          setAnalysis(null)
        }
      } else {
        if (mountedRef.current) setDeleteError('Failed to delete analysis')
      }
    } catch {
      if (mountedRef.current) setDeleteError('Failed to delete analysis')
    } finally {
      if (mountedRef.current) setDeletingId(null)
    }
  }

  function loadAnalysis(a: ComparisonAnalysisSummary) {
    setSelectedA(String(a.workout_id_a))
    setSelectedB(String(a.workout_id_b))
  }

  async function runAnalysis(force: boolean) {
    if (!selectedA || !selectedB || analyzing) return
    analysisAbortRef.current?.abort()
    const controller = new AbortController()
    analysisAbortRef.current = controller
    setAnalyzing(true)
    setAnalysisError('')
    try {
      const params = new URLSearchParams()
      params.set('a', String(selectedA))
      params.set('b', String(selectedB))
      if (force) {
        params.set('force', '1')
      }
      // Include current lap selection (if any) so analysis matches the visible comparison
      const lapsA = searchParams.get('laps_a')
      const lapsB = searchParams.get('laps_b')
      if (lapsA) {
        params.set('laps_a', lapsA)
      }
      if (lapsB) {
        params.set('laps_b', lapsB)
      }
      const url = `/api/training/compare/analyze?${params.toString()}`
      const res = await fetch(url, { method: 'POST', credentials: 'include', signal: controller.signal })
      if (!mountedRef.current) return
      if (res.ok) {
        const data = await res.json()
        if (!mountedRef.current || controller.signal.aborted) return
        setAnalysis(data.analysis)
        fetchPreviousAnalyses()
      } else {
        const data = await res.json().catch(() => null)
        if (!mountedRef.current || controller.signal.aborted) return
        setAnalysisError(data?.error || 'Failed to analyze comparison')
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      if (mountedRef.current) setAnalysisError('Failed to connect to Claude')
    } finally {
      if (mountedRef.current) setAnalyzing(false)
    }
  }

  function toggleLap(side: 'a' | 'b', index: number) {
    const laps = side === 'a' ? lapsA : lapsB
    if (index < 0 || index >= laps.length) return
    const setter = side === 'a' ? setPickedLapsA : setPickedLapsB
    setter(prev => {
      const pos = prev.indexOf(index)
      if (pos !== -1) return prev.filter(i => i !== index)
      return [...prev, index]
    })
  }

  function handleCompareSelected() {
    if (pickedLapsA.length === 0 || pickedLapsA.length !== pickedLapsB.length) return
    manualAbortRef.current?.abort()
    const controller = new AbortController()
    manualAbortRef.current = controller
    runComparison(selectedA, selectedB, pickedLapsA, pickedLapsB, controller.signal)
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

      {/* Previous Analyses — admin only */}
      {user?.is_admin && previousAnalyses.length > 0 && (
        <div className="bg-gray-800 rounded-xl p-4 mb-6">
          <h2 className="text-sm font-semibold flex items-center gap-2 mb-3 text-gray-300">
            <History size={16} className="text-purple-400" />
            Previous Analyses
          </h2>
          <div className="space-y-2">
            {previousAnalyses.map((a) => {
              const wA = workouts.find(w => w.id === a.workout_id_a)
              const wB = workouts.find(w => w.id === a.workout_id_b)
              const isActive = String(a.workout_id_a) === selectedA && String(a.workout_id_b) === selectedB
              return (
                <div
                  key={a.id}
                  className={`flex items-start gap-3 p-3 rounded-lg transition-colors ${
                    isActive ? 'bg-purple-500/10 ring-1 ring-purple-500/30' : 'bg-gray-700/50 hover:bg-gray-700'
                  }`}
                >
                  <div className="flex-1 min-w-0">
                    <p className="text-sm text-gray-200 truncate">
                      {wA?.title ?? `Workout #${a.workout_id_a}`} vs {wB?.title ?? `Workout #${a.workout_id_b}`}
                    </p>
                    {a.summary && (
                      <p className="text-xs text-gray-400 mt-1 line-clamp-2">{a.summary}</p>
                    )}
                    <p className="text-xs text-gray-500 mt-1">
                      {new Date(a.created_at).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}
                      {' · '}{a.model}
                    </p>
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    {!isActive && (
                      <button
                        type="button"
                        onClick={() => loadAnalysis(a)}
                        className="p-1.5 text-gray-400 hover:text-white rounded transition-colors"
                        title="Load this comparison"
                      >
                        <ExternalLink size={14} />
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => deleteAnalysis(a.id)}
                      disabled={deletingId === a.id}
                      className="p-1.5 text-gray-400 hover:text-red-400 rounded transition-colors disabled:opacity-50"
                      title="Delete this analysis"
                    >
                      {deletingId === a.id ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {loadingAnalyses && previousAnalyses.length === 0 && user?.is_admin && (
        <div className="flex items-center gap-2 text-gray-500 text-sm mb-6">
          <Loader2 size={14} className="animate-spin" />
          Loading previous analyses...
        </div>
      )}

      {analysesListError && user?.is_admin && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
          {analysesListError}
        </div>
      )}

      {deleteError && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
          {deleteError}
        </div>
      )}

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
                manualAbortRef.current?.abort()
                manualAbortRef.current = null
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
              id="lappicker-a"
              label={`Workout A — ${comparison?.workout_a.title ?? 'Loading...'} (${lapsA.length} laps)`}
              laps={lapsA}
              selected={pickedLapsA}
              onToggle={(idx) => toggleLap('a', idx)}
              color="text-blue-400"
            />
            <LapPicker
              id="lappicker-b"
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
                Pairing: {pickedLapsA.map((a, i) => {
                  const bIdx = pickedLapsB[i]
                  const lapNumA = a < lapsA.length ? lapsA[a].lap_number : '?'
                  const lapNumB = bIdx !== undefined && bIdx < lapsB.length ? lapsB[bIdx].lap_number : '?'
                  return `A${lapNumA}↔B${lapNumB}`
                }).join(', ')}
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
            <div className="bg-gray-800 rounded-xl p-6 mb-6">
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

      {/* AI Comparison Analysis — admin only, shown for any selected pair (including incompatible) */}
      {user?.is_admin && selectedA && selectedB && (
        <div className="bg-gray-800 rounded-xl p-6 mt-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold flex items-center gap-2">
              <Sparkles size={18} className="text-purple-400" />
              AI Comparison Analysis
            </h2>
            <div className="flex gap-2">
              {analysis ? (
                <button
                  onClick={() => runAnalysis(true)}
                  disabled={analyzing}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm disabled:opacity-50"
                >
                  {analyzing ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                  Re-analyze
                </button>
              ) : (
                <button
                  onClick={() => runAnalysis(false)}
                  disabled={analyzing}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-purple-600 hover:bg-purple-700 rounded-lg text-sm disabled:opacity-50"
                >
                  {analyzing ? <Loader2 size={14} className="animate-spin" /> : <Sparkles size={14} />}
                  Analyze with Claude
                </button>
              )}
            </div>
          </div>

          {analyzing && !analysis && (
            <div className="flex items-center gap-3 text-gray-400 text-sm">
              <Loader2 size={16} className="animate-spin" />
              Analyzing comparison with Claude...
            </div>
          )}

          {analysisError && (
            <p className="text-red-400 text-sm">{analysisError}</p>
          )}

          {analysis && (
            <div className="space-y-4">
              {analysis.summary && (
                <p className="text-gray-300 text-sm">{analysis.summary}</p>
              )}

              {analysis.strengths?.length > 0 && (
                <div>
                  <h3 className="text-sm font-medium text-green-400 mb-1">Strengths</h3>
                  <ul className="list-disc list-inside text-sm text-gray-300 space-y-1">
                    {analysis.strengths.map((s, i) => (
                      <li key={i}>{s}</li>
                    ))}
                  </ul>
                </div>
              )}

              {analysis.weaknesses?.length > 0 && (
                <div>
                  <h3 className="text-sm font-medium text-red-400 mb-1">Areas to Improve</h3>
                  <ul className="list-disc list-inside text-sm text-gray-300 space-y-1">
                    {analysis.weaknesses.map((w, i) => (
                      <li key={i}>{w}</li>
                    ))}
                  </ul>
                </div>
              )}

              {analysis.observations?.length > 0 && (
                <div>
                  <h3 className="text-sm font-medium text-blue-400 mb-1">Observations</h3>
                  <ul className="list-disc list-inside text-sm text-gray-300 space-y-1">
                    {analysis.observations.map((o, i) => (
                      <li key={i}>{o}</li>
                    ))}
                  </ul>
                </div>
              )}

              <p className="text-xs text-gray-500">
                Analyzed by {analysis.model} · {new Date(analysis.created_at).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}
                {analysis.cached && ' · cached'}
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
