import { useState, useEffect, useCallback, useRef, useId, type ReactNode } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { ArrowLeft, Trash2, Save, GitCompareArrows, Sparkles, RefreshCw, Loader2, TrendingUp, TrendingDown, ArrowRight, Minus, AlertTriangle, CheckCircle2, Info, FlaskConical } from 'lucide-react'
import { useAuth } from '../auth'
import { useTranslation } from 'react-i18next'
import { formatDate, formatTime, formatNumber } from '../utils/formatDate'
import type { Workout, ZoneDistribution, WorkoutAnalysis, CachedInsights, Lap, RacePredictions } from '../types/training'
import WorkoutHRChart from '../components/charts/WorkoutHRChart'
import WorkoutPaceChart from '../components/charts/WorkoutPaceChart'
import HRZoneCard from '../components/training/HRZoneCard'
import TrendCard from '../components/training/TrendCard'
import RacePredictionsCard from '../components/training/RacePredictionsCard'
import TagBadge from '../components/TagBadge'
import { isAutoTag, isAITag } from '../tags'
import LactateImportDialog from '../components/LactateImportDialog'

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  if (h > 0) return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
  return `${m}:${s.toString().padStart(2, '0')}`
}

function computePacingSplit(laps: Lap[]): 'positive' | 'negative' | 'even' | null {
  // Ignore very short laps (< 200 m) such as warmup/cooldown/trailing partial laps.
  const valid = laps.filter((l) => l.avg_pace_sec_per_km > 0 && l.distance_meters >= 200)
  if (valid.length < 2) return null

  const totalDistance = valid.reduce((s, l) => s + l.distance_meters, 0)
  const halfDistance = totalDistance / 2

  let firstDuration = 0, firstDistance = 0
  let secondDuration = 0, secondDistance = 0
  let cumulative = 0

  for (const lap of valid) {
    cumulative += lap.distance_meters
    if (cumulative <= halfDistance) {
      firstDuration += lap.duration_seconds
      firstDistance += lap.distance_meters
    } else {
      secondDuration += lap.duration_seconds
      secondDistance += lap.distance_meters
    }
  }

  if (firstDistance === 0 || secondDistance === 0) return null

  // Derive pace from total_time / total_distance for each half (distance-weighted).
  const firstPace = (firstDuration / firstDistance) * 1000
  const secondPace = (secondDuration / secondDistance) * 1000

  const ratio = Math.abs(firstPace - secondPace) / firstPace
  if (ratio < 0.03) return 'even'
  // positive split = first half faster (lower sec/km) = slowing down
  return firstPace < secondPace ? 'positive' : 'negative'
}

export default function TrainingDetail() {
  const { id } = useParams<{ id: string }>()
  const { user, hasFeature } = useAuth()
  const { t } = useTranslation(['training', 'lactate', 'common'])
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
  const [analysis, setAnalysis] = useState<WorkoutAnalysis | null>(null)
  const [analyzing, setAnalyzing] = useState(false)
  const [analysisError, setAnalysisError] = useState('')
  const [insights, setInsights] = useState<CachedInsights | null>(null)
  const [racePredictions, setRacePredictions] = useState<RacePredictions | null>(null)
  const [showLactateImport, setShowLactateImport] = useState(false)

  function formatDistance(meters: number): string {
    if (meters < 1000) return `${Math.round(meters)} ${t('units.m')}`
    return `${formatNumber(meters / 1000, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ${t('units.km')}`
  }

  function formatPace(secPerKm: number): string {
    if (secPerKm <= 0) return '--:--'
    let mins = Math.floor(secPerKm / 60)
    let secs = Math.round(secPerKm % 60)
    if (secs === 60) { mins++; secs = 0 }
    return `${mins}:${secs.toString().padStart(2, '0')} ${t('units.pace')}`
  }

  useEffect(() => {
    if (!user || !id) return
    async function run() {
      setLoading(true)
      setError('')
      setWorkout(null)
      setAnalysis(null)
      setAnalysisError('')
      setInsights(null)
      setRacePredictions(null)
      setZones([])
      setSimilar([])
      try {
        const isAdmin = user?.is_admin ?? false
        const fetches: Promise<Response>[] = [
          fetch(`/api/training/workouts/${id}`, { credentials: 'include' }),
          fetch(`/api/training/workouts/${id}/zones`, { credentials: 'include' }),
          fetch(`/api/training/workouts/${id}/similar`, { credentials: 'include' }),
        ]
        if (isAdmin) {
          fetches.push(fetch(`/api/training/workouts/${id}/analysis`, { credentials: 'include' }))
          fetches.push(fetch(`/api/training/workouts/${id}/insights`, { credentials: 'include' }))
          fetches.push(fetch('/api/training/predictions', { credentials: 'include' }))
        }

        const [wRes, zRes, sRes, aRes, iRes, rRes] = await Promise.all(fetches)

        if (!wRes.ok) {
          setError(t('errors.workoutNotFound'))
          return
        }
        const wData = await wRes.json()
        setWorkout(wData.workout)
        setEditTitle(wData.workout.title)
        setEditTags((wData.workout.tags || []).filter((tag: string) => !isAutoTag(tag) && !isAITag(tag)).join(', '))

        if (zRes.ok) {
          const zData = await zRes.json()
          setZones(zData.zones || [])
        }
        if (sRes.ok) {
          const sData = await sRes.json()
          setSimilar(sData.similar || [])
        }
        if (aRes && aRes.ok) {
          const aData = await aRes.json()
          setAnalysis(aData.analysis || null)
        } else {
          setAnalysis(null)
        }
        if (iRes) {
          if (iRes.ok) {
            const iData = await iRes.json()
            const raw = iData.insights
            if (raw) {
              raw.suggestions = raw.suggestions ?? []
              raw.confidence_score = raw.confidence_score ?? 0
            }
            setInsights(raw || null)
          } else if (iRes.status !== 404) {
            console.warn('Failed to load workout insights:', iRes.status)
          }
        }
        if (rRes && rRes.ok) {
          const rData = await rRes.json()
          if (rData.predictions && rData.predictions.length > 0) {
            setRacePredictions(rData)
          }
        }
      } catch {
        setError(t('errors.failedToLoadWorkout'))
      } finally {
        setLoading(false)
      }
    }
    run()
  }, [user, id, t])

  // Poll for analysis completion when status is 'pending'.
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const refreshWorkoutAndAnalysis = useCallback(async () => {
    if (!id) return
    try {
      const [wRes, aRes] = await Promise.all([
        fetch(`/api/training/workouts/${id}`, { credentials: 'include' }),
        fetch(`/api/training/workouts/${id}/analysis`, { credentials: 'include' }),
      ])
      if (wRes.ok) {
        const wData = await wRes.json()
        setWorkout(wData.workout)
      }
      if (aRes.ok) {
        const aData = await aRes.json()
        setAnalysis(aData.analysis || null)
      }
    } catch {
      // Ignore polling errors.
    }
  }, [id])

  useEffect(() => {
    if (workout?.analysis_status === 'pending') {
      pollRef.current = setInterval(refreshWorkoutAndAnalysis, 5000)
      return () => {
        if (pollRef.current) clearInterval(pollRef.current)
      }
    }
    // Clear any existing poll when status changes away from pending.
    if (pollRef.current) {
      clearInterval(pollRef.current)
      pollRef.current = null
    }
  }, [workout?.analysis_status, refreshWorkoutAndAnalysis])

  const runAnalysis = async (deleteFirst: boolean) => {
    if (!workout) return
    setAnalyzing(true)
    setAnalysisError('')
    try {
      if (deleteFirst) {
        const delRes = await fetch(`/api/training/workouts/${workout.id}/analysis`, {
          method: 'DELETE',
          credentials: 'include',
        })
        if (!delRes.ok && delRes.status !== 404) {
          setAnalysisError(t('errors.failedToClearCache'))
          return
        }
      }
      const res = await fetch(`/api/training/workouts/${workout.id}/analyze`, {
        method: 'POST',
        credentials: 'include',
      })
      if (res.ok) {
        const data = await res.json()
        setAnalysis(data.analysis)
        // Reload workout to get updated tags.
        const wRes = await fetch(`/api/training/workouts/${workout.id}`, { credentials: 'include' })
        if (wRes.ok) {
          const wData = await wRes.json()
          setWorkout(wData.workout)
        }
      } else {
        const data = await res.json().catch(() => ({})) as { error?: string }
        setAnalysisError(data.error || t('errors.analysisFailed'))
      }
    } catch {
      setAnalysisError(t('errors.failedToConnectToClaude'))
    } finally {
      setAnalyzing(false)
    }
  }

  const handleAnalyze = () => runAnalysis(false)
  const handleReanalyze = () => runAnalysis(true)

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
          tags: editTags.split(',').map((tag) => tag.trim()).filter(Boolean),
        }),
      })
      if (res.ok) {
        const data = await res.json()
        setWorkout(data.workout)
        setEditing(false)
      } else {
        const data = await res.json().catch(() => ({})) as { error?: string }
        setError(data.error || t('errors.failedToSave'))
      }
    } catch {
      setError(t('errors.failedToSave'))
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
        setError(data.error || t('errors.failedToDelete'))
        setShowDeleteConfirm(false)
      }
    } catch {
      setError(t('errors.failedToDelete'))
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
          <ArrowLeft size={16} /> {t('backToTraining')}
        </Link>
        <p className="text-red-400">{error || t('errors.workoutNotFound')}</p>
      </div>
    )
  }

  const date = new Date(workout.started_at)
  const aiTags = analysis?.tags ? analysis.tags.split(',').map(tag => tag.trim()).filter(Boolean) : []

  return (
    <div className="max-w-4xl mx-auto px-4 py-8">
      {/* Header */}
      <Link to="/training" className="flex items-center gap-2 text-gray-400 hover:text-white mb-4 text-sm">
        <ArrowLeft size={16} /> {t('backToTraining')}
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
              {workout.tags?.some((tag) => tag.startsWith('auto:')) && (
                <div className="flex gap-1 flex-wrap">
                  {workout.tags.filter((tag) => tag.startsWith('auto:')).map((tag) => (
                    <TagBadge key={tag} tag={tag} />
                  ))}
                </div>
              )}
              <input
                value={editTags}
                onChange={(e) => setEditTags(e.target.value)}
                placeholder={t('detail.tagsPlaceholder')}
                className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm w-full focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <div className="flex gap-2">
                <button
                  onClick={handleSave}
                  disabled={saving}
                  className="flex items-center gap-1 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 rounded-lg text-sm disabled:opacity-50"
                >
                  <Save size={14} /> {t('actions.save', { ns: 'common' })}
                </button>
                <button
                  onClick={() => setEditing(false)}
                  className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm"
                >
                  {t('actions.cancel', { ns: 'common' })}
                </button>
              </div>
            </div>
          ) : (
            <>
              <h1 className="text-2xl font-bold mb-1">{workout.title}</h1>
              <p className="text-gray-400">
                {formatDate(date, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })}
                {' · '}
                {formatTime(date, { hour: '2-digit', minute: '2-digit' })}
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
            {/* Show "Import as Lactate Test" for running workouts only (treadmill workouts
                are logged as running sport; flag: expand to all sports if desired). */}
            {workout.sport === 'running' && hasFeature('lactate') && (
              <button
                onClick={() => setShowLactateImport(true)}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm"
              >
                <FlaskConical size={14} />
                {t('detail.importAsLactateTest', { ns: 'lactate' })}
              </button>
            )}
            <button
              onClick={() => setEditing(true)}
              className="px-3 py-1.5 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm"
            >
              {t('detail.edit')}
            </button>
            <button
              onClick={() => setShowDeleteConfirm(true)}
              className="p-1.5 text-gray-400 hover:text-red-400 rounded-lg transition-colors"
              title={t('detail.deleteWorkoutTitle')}
            >
              <Trash2 size={18} />
            </button>
          </div>
        )}
      </div>

      {/* Delete confirmation */}
      {showDeleteConfirm && (
        <div className="mb-4 p-4 bg-red-500/10 border border-red-500/20 rounded-lg">
          <p className="text-red-400 mb-3">{t('detail.deleteConfirm')}</p>
          <div className="flex gap-2">
            <button onClick={handleDelete} className="px-4 py-2 bg-red-600 hover:bg-red-700 rounded-lg text-sm">
              {t('actions.delete', { ns: 'common' })}
            </button>
            <button onClick={() => setShowDeleteConfirm(false)} className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm">
              {t('actions.cancel', { ns: 'common' })}
            </button>
          </div>
        </div>
      )}

      {/* Lactate import dialog */}
      {showLactateImport && workout && (
        <LactateImportDialog
          workoutId={workout.id.toString()}
          onClose={() => setShowLactateImport(false)}
          onSuccess={(testId) => navigate(`/lactate/${testId}`)}
        />
      )}

      {/* AI Analysis section — admin only */}
      {user?.is_admin && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold flex items-center gap-2">
              <Sparkles size={18} className="text-purple-400" />
              {t('analysis.title')}
            </h2>
            <div className="flex gap-2">
              {analysis ? (
                <button
                  onClick={handleReanalyze}
                  disabled={analyzing || workout.analysis_status === 'pending'}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm disabled:opacity-50"
                >
                  {analyzing ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                  {t('analysis.reanalyze')}
                </button>
              ) : workout.analysis_status === 'pending' ? (
                <span className="flex items-center gap-1.5 px-3 py-1.5 text-gray-400 text-sm">
                  <Loader2 size={14} className="animate-spin" />
                  {t('analysis.analysisPending')}
                </span>
              ) : workout.analysis_status === 'failed' ? (
                <button
                  onClick={handleAnalyze}
                  disabled={analyzing}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-red-600 hover:bg-red-700 rounded-lg text-sm disabled:opacity-50"
                >
                  {analyzing ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                  {t('analysis.retryAnalysis')}
                </button>
              ) : (
                <button
                  onClick={handleAnalyze}
                  disabled={analyzing}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-purple-600 hover:bg-purple-700 rounded-lg text-sm disabled:opacity-50"
                >
                  {analyzing ? <Loader2 size={14} className="animate-spin" /> : <Sparkles size={14} />}
                  {t('analysis.analyze')}
                </button>
              )}
            </div>
          </div>

          {(analyzing || workout.analysis_status === 'pending') && !analysis && (
            <div className="flex items-center gap-3 text-gray-400 text-sm">
              <Loader2 size={16} className="animate-spin" />
              {t('analysis.analyzing')}
            </div>
          )}

          {workout.analysis_status === 'failed' && !analysis && !analysisError && (
            <p className="text-red-400 text-sm">{t('analysis.analysisFailed')}</p>
          )}

          {analysisError && (
            <p className="text-red-400 text-sm">{analysisError}</p>
          )}

          {analysis && (
            <div className="space-y-3">
              {analysis.title && (
                <p className="text-sm font-medium text-purple-300">{t('analysis.analysisTitle', { title: analysis.title })}</p>
              )}
              {aiTags.length > 0 && (
                <div className="flex gap-1.5 flex-wrap">
                  {aiTags.map((tag) => (
                    <TagBadge key={tag} tag={tag} />
                  ))}
                </div>
              )}
              {analysis.summary && (
                <p className="text-gray-300 text-sm">{analysis.summary}</p>
              )}
              {analysis.trend_analysis && (
                <div className="mt-4 pt-4 border-t border-gray-700 space-y-3">
                  <div className="flex items-center gap-2">
                    <h3 className="text-sm font-semibold text-gray-300">{t('analysis.trendTitle')}</h3>
                    {analysis.trend_analysis.fitness_direction === 'improving' && (
                      <span className="flex items-center gap-1 px-2 py-0.5 bg-green-500/15 text-green-400 text-xs rounded-full font-medium">
                        <TrendingUp size={12} /> {t('analysis.trendImproving')}
                      </span>
                    )}
                    {analysis.trend_analysis.fitness_direction === 'declining' && (
                      <span className="flex items-center gap-1 px-2 py-0.5 bg-red-500/15 text-red-400 text-xs rounded-full font-medium">
                        <TrendingDown size={12} /> {t('analysis.trendDeclining')}
                      </span>
                    )}
                    {analysis.trend_analysis.fitness_direction === 'stable' && (
                      <span className="flex items-center gap-1 px-2 py-0.5 bg-blue-500/15 text-blue-400 text-xs rounded-full font-medium">
                        <ArrowRight size={12} /> {t('analysis.trendStable')}
                      </span>
                    )}
                    {analysis.trend_analysis.fitness_direction === 'insufficient data' && (
                      <span className="flex items-center gap-1 px-2 py-0.5 bg-gray-500/15 text-gray-400 text-xs rounded-full font-medium">
                        <Minus size={12} /> {t('analysis.trendInsufficientData')}
                      </span>
                    )}
                  </div>
                  {analysis.trend_analysis.comparison_to_recent && (
                    <p className="text-sm text-gray-300 bg-gray-700/50 rounded-lg px-3 py-2">
                      {analysis.trend_analysis.comparison_to_recent}
                    </p>
                  )}
                  {analysis.trend_analysis.notable_changes && analysis.trend_analysis.notable_changes.length > 0 && (
                    <div>
                      <p className="text-xs text-gray-500 mb-1">{t('analysis.trendNotableChanges')}</p>
                      <ul className="space-y-1">
                        {analysis.trend_analysis.notable_changes.map((change) => (
                          <li key={change} className="flex items-start gap-2 text-sm text-gray-300">
                            <span className="text-purple-400 mt-0.5">•</span>
                            {change}
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              )}
              <p className="text-xs text-gray-500">
                {t('analysis.analyzedBy', {
                  model: analysis.model,
                  date: formatDate(analysis.created_at, { month: 'short', day: 'numeric', year: 'numeric' }),
                })}
              </p>
            </div>
          )}
        </div>
      )}

      {/* Trend Card — admin only, uses insights trend_analysis only (analysis.trend_analysis is shown inline above) */}
      {user?.is_admin && insights?.trend_analysis && (
        <TrendCard trendAnalysis={insights.trend_analysis} />
      )}

      {/* Race Predictions Card — admin only */}
      {user?.is_admin && racePredictions && (
        <RacePredictionsCard data={racePredictions} />
      )}

      {/* Effort & Pacing Card */}
      <EffortPacingCard
        effortSummary={insights?.effort_summary ?? null}
        pacingSplit={workout.laps && workout.laps.length >= 2 ? computePacingSplit(workout.laps) : null}
        hrDrift={workout.hr_drift_pct ?? null}
        paceCV={workout.pace_cv_pct ?? null}
      />

      {/* Risk Flags Section */}
      {insights && insights.risk_flags && insights.risk_flags.length > 0 && (
        <RiskFlagsSection riskFlags={insights.risk_flags} />
      )}

      {/* Suggestions Card */}
      {insights && insights.suggestions.length > 0 && (
        <SuggestionsCard suggestions={insights.suggestions} />
      )}

      {/* Confidence Indicator */}
      {insights && (
        <ConfidenceIndicator score={insights.confidence_score} note={insights.confidence_note} />
      )}

      {/* Summary stats */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-6">
        <StatCard label={t('detail.stats.duration')} value={formatDuration(workout.duration_seconds)} />
        <StatCard label={t('detail.stats.distance')} value={formatDistance(workout.distance_meters)} />
        {workout.avg_heart_rate > 0 && (
          <StatCard
            label={t('detail.stats.avgHR')}
            value={`${workout.avg_heart_rate} ${t('units.bpm')}`}
            sub={t('detail.stats.maxHR', { value: workout.max_heart_rate })}
          />
        )}
        {workout.avg_pace_sec_per_km > 0 && (
          <StatCard label={t('detail.stats.avgPace')} value={formatPace(workout.avg_pace_sec_per_km)} />
        )}
        {workout.calories > 0 && <StatCard label={t('detail.stats.calories')} value={`${workout.calories}`} />}
        {workout.ascent_meters > 0 && (
          <StatCard
            label={t('detail.stats.elevation')}
            value={`↑${Math.round(workout.ascent_meters)}${t('units.m')}`}
            sub={`↓${Math.round(workout.descent_meters)}${t('units.m')}`}
          />
        )}
        {workout.avg_cadence > 0 && <StatCard label={t('detail.stats.cadence')} value={`${workout.avg_cadence} ${t('units.spm')}`} />}
      </div>

      {/* Charts */}
      {workout.samples && workout.samples.points.length > 0 && (
        <div className="space-y-6 mb-6">
          <div className="bg-gray-800 rounded-xl p-6">
            <h2 className="text-lg font-semibold mb-4">{t('detail.charts.heartRate')}</h2>
            <WorkoutHRChart samples={workout.samples.points} avgHeartRate={workout.avg_heart_rate} />
          </div>
          <div className="bg-gray-800 rounded-xl p-6">
            <h2 className="text-lg font-semibold mb-4">{t('detail.charts.pace')}</h2>
            <WorkoutPaceChart samples={workout.samples.points} avgPaceSecPerKm={workout.avg_pace_sec_per_km} />
          </div>
        </div>
      )}

      {/* HR Zone Distribution */}
      <HRZoneCard
        zones={zones}
        thresholdContext={insights?.threshold_context}
        hrDrift={workout.hr_drift_pct ?? null}
      />

      {/* Laps */}
      {workout.laps && workout.laps.length > 1 && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          <h2 className="text-lg font-semibold mb-4">{t('detail.laps.title')}</h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-gray-400 border-b border-gray-700">
                  <th className="text-left py-2 pr-4">{t('detail.laps.number')}</th>
                  <th className="text-right py-2 px-4">{t('detail.laps.duration')}</th>
                  <th className="text-right py-2 px-4">{t('detail.laps.distance')}</th>
                  <th className="text-right py-2 px-4">{t('detail.laps.avgHR')}</th>
                  <th className="text-right py-2 px-4">{t('detail.laps.maxHR')}</th>
                  <th className="text-right py-2 pl-4">{t('detail.laps.pace')}</th>
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
            {t('detail.similar.title')}
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
                  {t('detail.similar.compare')}
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

function EffortPacingCard({
  effortSummary,
  pacingSplit,
  hrDrift,
  paceCV,
}: {
  effortSummary: string | null
  pacingSplit: 'positive' | 'negative' | 'even' | null
  hrDrift: number | null
  paceCV: number | null
}) {
  const { t } = useTranslation('training')
  const hasData = effortSummary || pacingSplit || hrDrift !== null || paceCV !== null
  if (!hasData) return null

  return (
    <div className="bg-gray-800 rounded-xl p-5 mb-6">
      <h2 className="text-sm font-semibold text-gray-400 mb-3">{t('effortPacing.title')}</h2>
      {effortSummary && (
        <p className="text-sm text-gray-200 mb-3">{effortSummary}</p>
      )}
      <div className="flex flex-wrap gap-2">
        {pacingSplit === 'positive' && (
          <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-yellow-500/15 border border-yellow-500/30 text-yellow-400 text-xs rounded-full font-medium">
            <TrendingDown size={12} />
            {t('effortPacing.positiveSplit')}
          </span>
        )}
        {pacingSplit === 'negative' && (
          <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-green-500/15 border border-green-500/30 text-green-400 text-xs rounded-full font-medium">
            <TrendingUp size={12} />
            {t('effortPacing.negativeSplit')}
          </span>
        )}
        {pacingSplit === 'even' && (
          <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-blue-500/15 border border-blue-500/30 text-blue-400 text-xs rounded-full font-medium">
            <Minus size={12} />
            {t('effortPacing.evenSplit')}
          </span>
        )}
        {hrDrift !== null && (
          <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-gray-700 text-gray-300 text-xs rounded-full">
            {t('effortPacing.hrDrift')}: {formatNumber(hrDrift, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%
          </span>
        )}
        {paceCV !== null && (
          <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-gray-700 text-gray-300 text-xs rounded-full">
            {t('effortPacing.paceCV')}: {formatNumber(paceCV, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%
          </span>
        )}
      </div>
    </div>
  )
}

function RiskFlagsSection({ riskFlags }: { riskFlags: string[] }) {
  const { t } = useTranslation('training')
  return (
    <div className="bg-gray-800 rounded-xl p-5 mb-6">
      <h2 className="text-sm font-semibold text-gray-400 mb-3 flex items-center gap-1.5">
        <AlertTriangle size={14} className="text-yellow-400" />
        {t('riskFlags.title')}
      </h2>
      <div className="flex flex-wrap gap-2">
        {riskFlags.map((flag, i) => {
          const isSevere = /overreach|injury|critical|stop|danger/i.test(flag)
          return (
            <span
              key={`${i}-${flag}`}
              className={`inline-flex items-center gap-1 px-2.5 py-1 text-xs rounded-full border ${
                isSevere
                  ? 'bg-red-500/15 border-red-500/30 text-red-400'
                  : 'bg-yellow-500/15 border-yellow-500/30 text-yellow-400'
              }`}
            >
              <AlertTriangle size={12} />
              {flag}
            </span>
          )
        })}
      </div>
    </div>
  )
}

function SuggestionsCard({ suggestions }: { suggestions: string[] }) {
  const { t } = useTranslation('training')
  if (!suggestions.length) return null
  return (
    <div className="bg-gray-800 rounded-xl p-5 mb-6">
      <h2 className="text-sm font-semibold text-gray-400 mb-3">{t('suggestions.title')}</h2>
      <div className="flex flex-wrap gap-2">
        {suggestions.map((s, i) => (
          <span
            key={`${i}-${s}`}
            className="inline-flex items-center px-3 py-1.5 bg-blue-500/10 border border-blue-500/20 text-blue-300 text-xs rounded-full"
          >
            {s}
          </span>
        ))}
      </div>
    </div>
  )
}

function ConfidenceIndicator({ score, note }: { score?: number; note?: string }) {
  const { t } = useTranslation('training')
  const [tooltipVisible, setTooltipVisible] = useState(false)
  const baseId = useId()
  const tooltipId = `${baseId}-confidence-tooltip`
  if (score === undefined || score === null) return null

  let icon: ReactNode
  if (score > 0.8) {
    icon = <CheckCircle2 size={14} className="text-green-400 shrink-0" />
  } else if (score >= 0.5) {
    icon = <Info size={14} className="text-yellow-400 shrink-0" />
  } else {
    icon = <AlertTriangle size={14} className="text-orange-400 shrink-0" />
  }

  return (
    <div
      className="relative inline-flex items-center gap-1.5 text-xs text-gray-400 mb-6 cursor-default"
      onMouseEnter={() => setTooltipVisible(true)}
      onMouseLeave={() => setTooltipVisible(false)}
      onFocus={() => setTooltipVisible(true)}
      onBlur={() => setTooltipVisible(false)}
      tabIndex={note ? 0 : undefined}
      aria-describedby={note ? tooltipId : undefined}
    >
      {icon}
      <span>{t('confidence.label', { value: formatNumber(score * 100, { maximumFractionDigits: 0 }) })}</span>
      {note && (
        <div
          id={tooltipId}
          role="tooltip"
          aria-hidden={!tooltipVisible}
          className={
            'absolute left-0 bottom-full mb-2 px-2.5 py-1.5 bg-gray-700 border border-gray-600 text-gray-200 text-xs rounded-lg pointer-events-none z-10 w-64 whitespace-normal shadow-lg transition-opacity ' +
            (tooltipVisible ? 'opacity-100 visible' : 'opacity-0 invisible')
          }
        >
          {note}
        </div>
      )}
    </div>
  )
}
