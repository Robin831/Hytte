import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
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
import { useTranslation } from 'react-i18next'
import { formatDate } from '../utils/formatDate'
import { isAutoTag, isAITag, displayTag } from '../tags'
import type { TFunction } from 'i18next'
import type { Workout, Lap, ComparisonResult, CachedComparisonAnalysis, ComparisonAnalysisSummary } from '../types/training'

function workoutOptionLabel(w: Workout): string {
  const tagPart = w.tags
    ?.filter((t) => isAutoTag(t) || isAITag(t))
    .map(displayTag)
    .join(', ')
  return tagPart
    ? `${w.title} — ${tagPart} — ${formatDate(w.started_at)}`
    : `${w.title} — ${formatDate(w.started_at)}`
}

function formatPace(secPerKm: number, t: TFunction<'training'>): string {
  if (secPerKm <= 0) return '--:--'
  let mins = Math.floor(secPerKm / 60)
  let secs = Math.round(secPerKm % 60)
  if (secs === 60) { mins++; secs = 0 }
  return `${mins}:${secs.toString().padStart(2, '0')} ${t('units.pace')}`
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
  const { t } = useTranslation(['training', 'common'])
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
              aria-label={isSelected
                ? t('compare.lapPicker.lapSelectedLabel', { number: lapNum, position: pos + 1 })
                : t('compare.lapPicker.lapLabel', { number: lapNum })
              }
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
              <span className="text-gray-300">{t('compare.lapPicker.lapLabel', { number: lapNum })}</span>
              <span className="ml-auto text-gray-500 text-xs tabular-nums">
                {formatDuration(lap.duration_seconds)} · {formatPace(lap.avg_pace_sec_per_km, t)} · {lap.avg_heart_rate > 0 ? `${lap.avg_heart_rate} ${t('units.bpm')}` : '-'}
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
  const { t } = useTranslation(['training', 'common'])
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

  // O(1) workout lookup for the previous analyses list
  const workoutMap = useMemo(() => new Map(workouts.map(w => [w.id, w])), [workouts])

  const lapsA = workoutA?.laps ?? []
  const lapsB = workoutB?.laps ?? []
  const hasMismatchedLaps = lapsA.length > 0 && lapsB.length > 0 && lapsA.length !== lapsB.length

  // Ref to abort in-flight manual comparison requests
  const manualAbortRef = useRef<AbortController | null>(null)
  // Ref to abort in-flight analysis requests
  const analysisAbortRef = useRef<AbortController | null>(null)
  // Ref to abort in-flight loadAnalysis requests
  const loadAnalysisAbortRef = useRef<AbortController | null>(null)
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
          setError(t('errors.failedToLoadWorkouts'))
        }
      } catch {
        setError(t('errors.failedToLoadWorkouts'))
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [user, t])

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
        errors.push(t('errors.failedToLoadComparison'))
      }
      if (!isLapRecompare) {
        const aRes = results[1]
        const bRes = results[2]
        if (aRes.ok) {
          const aData = await aRes.json()
          if (signal?.aborted || !mountedRef.current) return
          setWorkoutA(aData.workout)
        } else {
          errors.push(t('errors.failedToLoadWorkoutADetails'))
        }
        if (bRes.ok) {
          const bData = await bRes.json()
          if (signal?.aborted || !mountedRef.current) return
          setWorkoutB(bData.workout)
        } else {
          errors.push(t('errors.failedToLoadWorkoutBDetails'))
        }
      }
      if (errors.length > 0) {
        setError(errors.join('; '))
      }
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(t('errors.failedToCompare'))
    } finally {
      if (mountedRef.current) {
        setComparing(false)
      }
    }
  }, [mountedRef, t])

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
        if (mountedRef.current) setAnalysesListError(t('errors.failedToLoadPreviousAnalyses'))
      }
    } catch {
      if (mountedRef.current) setAnalysesListError(t('errors.failedToLoadPreviousAnalyses'))
    } finally {
      if (mountedRef.current) setLoadingAnalyses(false)
    }
  }, [user?.is_admin, t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
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
        if (!mountedRef.current) return
        setPreviousAnalyses(prev => prev.filter(a => a.id !== id))
        // If the deleted analysis matches the current comparison (either order), clear the cached analysis
        const deleted = previousAnalyses.find(a => a.id === id)
        if (deleted) {
          const dA = String(deleted.workout_id_a)
          const dB = String(deleted.workout_id_b)
          if ((dA === selectedA && dB === selectedB) || (dA === selectedB && dB === selectedA)) {
            setAnalysis(null)
          }
        }
      } else {
        if (mountedRef.current) setDeleteError(t('errors.failedToDeleteAnalysis'))
      }
    } catch {
      if (mountedRef.current) setDeleteError(t('errors.failedToDeleteAnalysis'))
    } finally {
      if (mountedRef.current) setDeletingId(null)
    }
  }

  async function loadAnalysis(a: ComparisonAnalysisSummary) {
    setSelectedA(String(a.workout_id_a))
    setSelectedB(String(a.workout_id_b))

    // Cancel any in-flight loadAnalysis request before starting a new one
    loadAnalysisAbortRef.current?.abort()
    const controller = new AbortController()
    loadAnalysisAbortRef.current = controller

    // Also fetch and populate the saved analysis content
    try {
      const res = await fetch(`/api/training/compare/analyses/${a.id}`, {
        credentials: 'include',
        signal: controller.signal,
      })

      // Ignore if this request is no longer the latest or component unmounted
      if (!res.ok || !mountedRef.current || loadAnalysisAbortRef.current !== controller) {
        return
      }

      const data = await res.json()

      // Double-check component is still mounted and this is the latest request
      if (mountedRef.current && loadAnalysisAbortRef.current === controller) {
        setAnalysis(data)
      }
    } catch (err: unknown) {
      // Ignore abort errors; user may have initiated another load
      if (err instanceof DOMException && err.name === 'AbortError') {
        return
      }
      // Silently fail — user can still click Analyze to load
    } finally {
      // Clear the controller ref if it's still pointing at this request
      if (loadAnalysisAbortRef.current === controller) {
        loadAnalysisAbortRef.current = null
      }
    }
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
      const lapsAParam = searchParams.get('laps_a')
      const lapsBParam = searchParams.get('laps_b')
      if (lapsAParam) {
        params.set('laps_a', lapsAParam)
      }
      if (lapsBParam) {
        params.set('laps_b', lapsBParam)
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
        setAnalysisError(data?.error || t('errors.failedToAnalyzeComparison'))
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      if (mountedRef.current) setAnalysisError(t('errors.failedToConnectToClaude'))
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
        <ArrowLeft size={16} /> {t('backToTraining')}
      </Link>

      <div className="flex items-center gap-3 mb-6">
        <GitCompareArrows size={24} className="text-purple-400" />
        <h1 className="text-2xl font-bold">{t('compare.title')}</h1>
      </div>

      {/* Selectors */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <div>
          <label className="block text-sm text-gray-400 mb-1">{t('compare.workoutA')}</label>
          <select
            value={selectedA}
            onChange={(e) => setSelectedA(e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">{t('compare.selectWorkout')}</option>
            {workouts.map((w) => (
              <option key={w.id} value={w.id}>
                {workoutOptionLabel(w)}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-sm text-gray-400 mb-1">{t('compare.workoutB')}</label>
          <select
            value={selectedB}
            onChange={(e) => setSelectedB(e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">{t('compare.selectWorkout')}</option>
            {workouts.map((w) => (
              <option key={w.id} value={w.id}>
                {workoutOptionLabel(w)}
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
            {t('compare.previousAnalyses.title')}
          </h2>
          <div className="space-y-2">
            {previousAnalyses.map((a) => {
              const wA = workoutMap.get(a.workout_id_a)
              const wB = workoutMap.get(a.workout_id_b)
              const idA = String(a.workout_id_a)
              const idB = String(a.workout_id_b)
              const isActive =
                (idA === selectedA && idB === selectedB) ||
                (idA === selectedB && idB === selectedA)
              return (
                <div
                  key={a.id}
                  className={`flex items-start gap-3 p-3 rounded-lg transition-colors ${
                    isActive ? 'bg-purple-500/10 ring-1 ring-purple-500/30' : 'bg-gray-700/50 hover:bg-gray-700'
                  }`}
                >
                  <div className="flex-1 min-w-0">
                    <p className="text-sm text-gray-200 truncate">
                      {wA?.title ?? t('compare.workoutFallback', { id: a.workout_id_a })} {t('compare.previousAnalyses.vs')} {wB?.title ?? t('compare.workoutFallback', { id: a.workout_id_b })}
                    </p>
                    {a.summary && (
                      <p className="text-xs text-gray-400 mt-1 line-clamp-2">{a.summary}</p>
                    )}
                    <p className="text-xs text-gray-500 mt-1">
                      {formatDate(a.created_at, { month: 'short', day: 'numeric', year: 'numeric' })}
                      {' · '}{a.model}
                    </p>
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    {!isActive && (
                      <button
                        type="button"
                        onClick={() => loadAnalysis(a)}
                        className="p-1.5 text-gray-400 hover:text-white rounded transition-colors"
                        title={t('compare.previousAnalyses.load')}
                        aria-label={t('compare.previousAnalyses.load')}
                      >
                        <ExternalLink size={14} />
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => deleteAnalysis(a.id)}
                      disabled={deletingId === a.id}
                      className="p-1.5 text-gray-400 hover:text-red-400 rounded transition-colors disabled:opacity-50"
                      title={t('compare.previousAnalyses.delete')}
                      aria-label={deletingId === a.id ? t('compare.previousAnalyses.deleting') : t('compare.previousAnalyses.delete')}
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
          {t('compare.previousAnalyses.loading')}
        </div>
      )}

      {analysesListError && user?.is_admin && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
          {analysesListError}
        </div>
      )}

      {deleteError && user?.is_admin && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
          {deleteError}
        </div>
      )}

      {error && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
          {error}
        </div>
      )}

      {comparing && <p className="text-gray-400 mb-4">{t('compare.comparing')}</p>}

      {comparison && !comparison.compatible && (
        <div className="bg-yellow-500/10 border border-yellow-500/20 rounded-xl p-4 mb-6">
          <p className="text-yellow-400 mb-3">{t('compare.notComparable', { reason: comparison.reason })}</p>
          {hasMismatchedLaps && !lapSelectMode && (
            <button
              type="button"
              onClick={() => setLapSelectMode(true)}
              className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm font-medium rounded-lg transition-colors"
            >
              <ListChecks size={16} />
              {t('compare.pickLaps')}
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
            {t('compare.selectSpecificLaps')}
          </button>
        </div>
      )}

      {/* Side-by-side lap picker */}
      {showLapPicker && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold">{t('compare.lapPicker.title')}</h2>
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
              {t('common:actions.cancel')}
            </button>
          </div>
          <p className="text-sm text-gray-400 mb-4">
            {t('compare.lapPicker.instruction')}
            {pickedLapsA.length !== pickedLapsB.length && pickedLapsA.length + pickedLapsB.length > 0 && (
              <span className="text-yellow-400 ml-1">
                {pickedLapsA.length > pickedLapsB.length
                  ? t('compare.lapPicker.moreFromB', { count: pickedLapsA.length - pickedLapsB.length })
                  : t('compare.lapPicker.moreFromA', { count: pickedLapsB.length - pickedLapsA.length })
                }
              </span>
            )}
          </p>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <LapPicker
              id="lappicker-a"
              label={t('compare.lapPicker.workoutALabel', { title: comparison?.workout_a.title ?? t('compare.loading'), count: lapsA.length })}
              laps={lapsA}
              selected={pickedLapsA}
              onToggle={(idx) => toggleLap('a', idx)}
              color="text-blue-400"
            />
            <LapPicker
              id="lappicker-b"
              label={t('compare.lapPicker.workoutBLabel', { title: comparison?.workout_b.title ?? t('compare.loading'), count: lapsB.length })}
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
              {canCompareSelected
                ? t('compare.lapPicker.compareLaps', { count: pickedLapsA.length })
                : t('compare.lapPicker.compareSelectedLaps')
              }
            </button>
            {canCompareSelected && (
              <span className="text-xs text-gray-500">
                {t('compare.lapPicker.pairing')} {pickedLapsA.map((a, i) => {
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
            <h2 className="text-lg font-semibold mb-2">{t('compare.summary.title')}</h2>
            <p className="text-lg">{comparison.summary.verdict}</p>
            <div className="flex gap-6 mt-3 text-sm text-gray-400">
              <p>
                {t('compare.summary.avgHRDelta')}{' '}
                <span className={comparison.summary.avg_hr_delta < 0 ? 'text-green-400' : comparison.summary.avg_hr_delta > 0 ? 'text-red-400' : 'text-white'}>
                  {comparison.summary.avg_hr_delta > 0 ? '+' : ''}{comparison.summary.avg_hr_delta.toFixed(1)} {t('units.bpm')}
                </span>
              </p>
              <p>
                {t('compare.summary.avgPaceDelta')}{' '}
                <span className={comparison.summary.avg_pace_delta < 0 ? 'text-green-400' : comparison.summary.avg_pace_delta > 0 ? 'text-red-400' : 'text-white'}>
                  {comparison.summary.avg_pace_delta > 0 ? '+' : ''}{comparison.summary.avg_pace_delta.toFixed(1)}s {t('units.pace')}
                </span>
              </p>
            </div>
          </div>

          {/* Lap comparison table */}
          {comparison.lap_deltas && comparison.lap_deltas.length > 0 && (
            <div className="bg-gray-800 rounded-xl p-6 mb-6">
              <h2 className="text-lg font-semibold mb-4">{t('compare.intervalComparison.title')}</h2>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-gray-400 border-b border-gray-700">
                      <th className="text-left py-2 pr-4">{t('compare.intervalComparison.lap')}</th>
                      <th className="text-right py-2 px-3">{t('compare.intervalComparison.hrA')}</th>
                      <th className="text-right py-2 px-3">{t('compare.intervalComparison.hrB')}</th>
                      <th className="text-right py-2 px-3">{t('compare.intervalComparison.hrDelta')}</th>
                      <th className="text-right py-2 px-3">{t('compare.intervalComparison.paceA')}</th>
                      <th className="text-right py-2 px-3">{t('compare.intervalComparison.paceB')}</th>
                      <th className="text-right py-2 pl-3">{t('compare.intervalComparison.paceDelta')}</th>
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
                        <td className="py-2 px-3 text-right">{formatPace(d.pace_a_sec_per_km, t)}</td>
                        <td className="py-2 px-3 text-right">{formatPace(d.pace_b_sec_per_km, t)}</td>
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
              <h2 className="text-lg font-semibold mb-4">{t('compare.hrOverlay.title')}</h2>
              <div className="w-full h-72" role="img" aria-label={t('compare.hrOverlay.ariaLabel')}>
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={overlayData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                    <XAxis dataKey="time" tick={{ fill: '#9ca3af', fontSize: 11 }} label={{ value: t('compare.hrOverlay.minutes'), position: 'insideBottom', offset: -3, fill: '#9ca3af', fontSize: 11 }} />
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
              {t('compare.aiAnalysis.title')}
            </h2>
            <div className="flex gap-2">
              {analysis ? (
                <button
                  onClick={() => runAnalysis(true)}
                  disabled={analyzing}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm disabled:opacity-50"
                >
                  {analyzing ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                  {t('compare.aiAnalysis.reanalyze')}
                </button>
              ) : (
                <button
                  onClick={() => runAnalysis(false)}
                  disabled={analyzing}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-purple-600 hover:bg-purple-700 rounded-lg text-sm disabled:opacity-50"
                >
                  {analyzing ? <Loader2 size={14} className="animate-spin" /> : <Sparkles size={14} />}
                  {t('compare.aiAnalysis.analyze')}
                </button>
              )}
            </div>
          </div>

          {analyzing && !analysis && (
            <div className="flex items-center gap-3 text-gray-400 text-sm">
              <Loader2 size={16} className="animate-spin" />
              {t('compare.aiAnalysis.analyzing')}
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
                  <h3 className="text-sm font-medium text-green-400 mb-1">{t('compare.aiAnalysis.strengths')}</h3>
                  <ul className="list-disc list-inside text-sm text-gray-300 space-y-1">
                    {analysis.strengths.map((s, i) => (
                      <li key={i}>{s}</li>
                    ))}
                  </ul>
                </div>
              )}

              {analysis.weaknesses?.length > 0 && (
                <div>
                  <h3 className="text-sm font-medium text-red-400 mb-1">{t('compare.aiAnalysis.areasToImprove')}</h3>
                  <ul className="list-disc list-inside text-sm text-gray-300 space-y-1">
                    {analysis.weaknesses.map((w, i) => (
                      <li key={i}>{w}</li>
                    ))}
                  </ul>
                </div>
              )}

              {analysis.observations?.length > 0 && (
                <div>
                  <h3 className="text-sm font-medium text-blue-400 mb-1">{t('compare.aiAnalysis.observations')}</h3>
                  <ul className="list-disc list-inside text-sm text-gray-300 space-y-1">
                    {analysis.observations.map((o, i) => (
                      <li key={i}>{o}</li>
                    ))}
                  </ul>
                </div>
              )}

              <p className="text-xs text-gray-500">
                {t('compare.aiAnalysis.analyzedBy', {
                  model: analysis.model,
                  date: formatDate(analysis.created_at, { month: 'short', day: 'numeric', year: 'numeric' }),
                })}
                {analysis.cached && ` ${t('compare.aiAnalysis.cached')}`}
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
