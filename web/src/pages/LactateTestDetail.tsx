import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useAuth } from '../auth'
import {
  Activity, ArrowLeft, Pencil, Trash2, Save, X, Plus,
  ChevronDown, ChevronUp, Timer, Gauge, CircleDot,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { LactateTest, Analysis } from '../types/lactate'
import LactateCurveChart from '../components/charts/LactateCurveChart'
import DualAxisChart from '../components/charts/DualAxisChart'
import { formatDate } from '../utils/formatDate'

interface EditStage {
  id: number
  stage_number: number
  speed_kmh: string
  lactate_mmol: string
  heart_rate_bpm: string
  rpe: string
  notes: string
}

let _editStageIdCounter = 0
function nextEditStageId() { return ++_editStageIdCounter }

const trafficColors = {
  green: { bg: 'bg-green-500/20', border: 'border-green-500/40', text: 'text-green-400', dot: 'bg-green-500' },
  yellow: { bg: 'bg-yellow-500/20', border: 'border-yellow-500/40', text: 'text-yellow-400', dot: 'bg-yellow-500' },
  red: { bg: 'bg-red-500/20', border: 'border-red-500/40', text: 'text-red-400', dot: 'bg-red-500' },
}

const zoneColors = [
  'bg-green-500/20 text-green-400 border-green-500/30',
  'bg-blue-500/20 text-blue-400 border-blue-500/30',
  'bg-yellow-500/20 text-yellow-400 border-yellow-500/30',
  'bg-orange-500/20 text-orange-400 border-orange-500/30',
  'bg-red-500/20 text-red-400 border-red-500/30',
]

export default function LactateTestDetail() {
  const { user } = useAuth()
  const { t } = useTranslation(['lactate', 'common'])
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const [test, setTest] = useState<LactateTest | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // Edit state
  const [editing, setEditing] = useState(false)
  const [editDate, setEditDate] = useState('')
  const [editComment, setEditComment] = useState('')
  const [editStages, setEditStages] = useState<EditStage[]>([])
  const [saving, setSaving] = useState(false)

  // Delete state
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleting, setDeleting] = useState(false)

  // Source workout state
  const [workoutTitle, setWorkoutTitle] = useState<string | null>(null)

  // Analysis state
  const [analysis, setAnalysis] = useState<Analysis | null>(null)
  const [analysisLoading, setAnalysisLoading] = useState(false)
  const [analysisError, setAnalysisError] = useState('')
  const [expandedSection, setExpandedSection] = useState<string | null>(null)
  const [selectedMethod, setSelectedMethod] = useState('')
  const [activeZoneSystem, setActiveZoneSystem] = useState(0)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    if (!user || !id) return
    const controller = new AbortController()
    const load = async () => {
      try {
        const res = await fetch(`/api/lactate/tests/${id}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) {
          if (res.status === 404) throw new Error('Test not found')
          throw new Error('Failed to load test')
        }
        const data = await res.json()
        setTest(data.test)
      } catch (err: unknown) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        setLoading(false)
      }
    }
    load()
    return () => controller.abort()
  }, [user, id])

  // Fetch source workout title when test has a workout_id
  useEffect(() => {
    if (!test?.workout_id) {
      setWorkoutTitle(null)
      return
    }
    const controller = new AbortController()
    const fetchWorkout = async () => {
      try {
        const res = await fetch(`/api/training/workouts/${test.workout_id}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (res.ok) {
          const data = await res.json()
          setWorkoutTitle(data.workout?.title ?? null)
        }
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setWorkoutTitle(null)
        }
      }
    }
    fetchWorkout()
    return () => controller.abort()
  }, [test?.workout_id])

  // Abort any in-flight analysis request on unmount
  useEffect(() => {
    return () => { abortRef.current?.abort() }
  }, [])

  const fetchAnalysis = useCallback(async (method?: string) => {
    if (!id) return
    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setAnalysisLoading(true)
    setAnalysisError('')
    try {
      const params = method ? `?method=${encodeURIComponent(method)}` : ''
      const res = await fetch(`/api/lactate/tests/${id}/analysis${params}`, {
        credentials: 'include',
        signal: controller.signal,
      })
      if (!res.ok) throw new Error('Failed to load analysis')
      setAnalysis(await res.json())
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') {
        setAnalysis(null)
        setAnalysisError(t('errors.failedToLoadAnalysis'))
      }
    } finally {
      setAnalysisLoading(false)
    }
  }, [id])

  // Auto-load analysis when test loads with enough stages
  useEffect(() => {
    if (!test || test.stages.length < 2 || editing || !id) return
    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller
    const load = async () => {
      setExpandedSection('curve')
      setAnalysisLoading(true)
      setAnalysisError('')
      try {
        const res = await fetch(`/api/lactate/tests/${id}/analysis`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error('Failed to load analysis')
        setAnalysis(await res.json())
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setAnalysis(null)
          setAnalysisError('Failed to load analysis')
        }
      } finally {
        setAnalysisLoading(false)
      }
    }
    load()
    return () => controller.abort()
  }, [test, editing, id])

  const handleMethodChange = (method: string) => {
    setSelectedMethod(method)
    fetchAnalysis(method)
  }

  const toggleSection = (section: string) => {
    setExpandedSection(expandedSection === section ? null : section)
  }

  const startEditing = () => {
    if (!test) return
    setEditing(true)
    setEditDate(test.date)
    setEditComment(test.comment)
    setEditStages(
      test.stages.map((s) => ({
        id: nextEditStageId(),
        stage_number: s.stage_number,
        speed_kmh: s.speed_kmh.toString(),
        lactate_mmol: s.lactate_mmol.toString(),
        heart_rate_bpm: s.heart_rate_bpm.toString(),
        rpe: s.rpe !== null ? s.rpe.toString() : '',
        notes: s.notes,
      }))
    )
    setAnalysis(null)
    setAnalysisError('')
    setError('')
  }

  const cancelEditing = () => {
    setEditing(false)
    setError('')
  }

  const addEditStage = () => {
    if (!test) return
    const lastSpeed = editStages.length > 0
      ? parseFloat(editStages[editStages.length - 1].speed_kmh)
      : test.start_speed_kmh - test.speed_increment_kmh
    setEditStages((prev) => [
      ...prev,
      {
        id: nextEditStageId(),
        stage_number: prev.length,
        speed_kmh: (lastSpeed + test.speed_increment_kmh).toFixed(1),
        lactate_mmol: '',
        heart_rate_bpm: '',
        rpe: '',
        notes: '',
      },
    ])
  }

  const removeEditStage = (index: number) => {
    if (editStages.length <= 2) return
    setEditStages((prev) => prev.filter((_, i) => i !== index))
  }

  const updateEditStage = (index: number, field: keyof EditStage, value: string) => {
    setEditStages((prev) =>
      prev.map((s, i) => (i === index ? { ...s, [field]: value } : s))
    )
  }

  const handleSave = async () => {
    if (!test) return

    const stagesPayload = editStages
      .filter((s) => s.lactate_mmol !== '')
      .map((s) => ({
        stage_number: s.stage_number,
        speed_kmh: parseFloat(s.speed_kmh),
        lactate_mmol: parseFloat(s.lactate_mmol),
        heart_rate_bpm: parseInt(s.heart_rate_bpm) || 0,
        rpe: s.rpe ? parseInt(s.rpe) : null,
        notes: s.notes,
      }))

    if (stagesPayload.length < 2) {
      setError(t('errors.minTwoStagesEdit'))
      return
    }

    for (let i = 0; i < stagesPayload.length; i++) {
      const s = stagesPayload[i]
      if (!isFinite(s.speed_kmh) || s.speed_kmh <= 0) {
        setError(t('errors.speedPositive', { number: i + 1 }))
        return
      }
      if (!isFinite(s.lactate_mmol) || s.lactate_mmol < 0) {
        setError(t('errors.lactateNonNegative', { number: i + 1 }))
        return
      }
      if (s.rpe !== null && (s.rpe < 6 || s.rpe > 20)) {
        setError(t('errors.rpeRange', { number: i + 1 }))
        return
      }
      if (i > 0 && s.speed_kmh <= stagesPayload[i - 1].speed_kmh) {
        setError(t('errors.speedsIncreasing', { a: i, b: i + 1 }))
        return
      }
    }

    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setSaving(true)
    setError('')
    try {
      const body = {
        date: editDate,
        comment: editComment,
        protocol_type: test.protocol_type,
        warmup_duration_min: test.warmup_duration_min,
        stage_duration_min: test.stage_duration_min,
        start_speed_kmh: test.start_speed_kmh,
        speed_increment_kmh: test.speed_increment_kmh,
        stages: stagesPayload,
      }

      const res = await fetch(`/api/lactate/tests/${test.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        signal: controller.signal,
        body: JSON.stringify(body),
      })

      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: t('errors.failedToUpdateTest') }))
        throw new Error(data.error || t('errors.failedToUpdateTest'))
      }

      const data = await res.json()
      setTest(data.test)
      setEditing(false)
      setSelectedMethod('')
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('errors.failedToUpdateTest'))
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!test) return
    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setDeleting(true)
    try {
      const res = await fetch(`/api/lactate/tests/${test.id}`, {
        method: 'DELETE',
        credentials: 'include',
        signal: controller.signal,
      })
      if (!res.ok) throw new Error(t('errors.failedToDeleteTest'))
      navigate('/lactate')
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('errors.failedToDeleteTest'))
      setDeleting(false)
      setShowDeleteConfirm(false)
    }
  }

  if (!user) {
    return (
      <div className="p-6">
        <p className="text-gray-400">{t('signInToView')}</p>
      </div>
    )
  }

  if (loading) {
    return (
      <div className="max-w-4xl mx-auto p-4 md:p-6">
        <div className="text-center py-12 text-gray-400">{t('detail.loading')}</div>
      </div>
    )
  }

  if (error && !test) {
    return (
      <div className="max-w-4xl mx-auto p-4 md:p-6">
        <div className="flex items-center gap-3 mb-6">
          <Link to="/lactate" className="text-gray-400 hover:text-white transition-colors" aria-label={t('backToTests')}>
            <ArrowLeft size={20} />
          </Link>
          <h1 className="text-2xl font-bold">{t('detail.testNotFound')}</h1>
        </div>
        <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 text-red-400">
          {error}
        </div>
      </div>
    )
  }

  if (!test) return null

  const formatTestDate = (dateStr: string) => {
    const [y, m, d] = dateStr.split('-').map(Number)
    return formatDate(new Date(y, m - 1, d), { year: 'numeric', month: 'long', day: 'numeric' })
  }

  return (
    <div className="max-w-4xl mx-auto p-4 md:p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <Link to="/lactate" className="text-gray-400 hover:text-white transition-colors" aria-label={t('backToTests')}>
            <ArrowLeft size={20} />
          </Link>
          <Activity size={24} className="text-blue-400" />
          <div>
            <h1 className="text-2xl font-bold">
              {test.comment || formatTestDate(test.date)}
            </h1>
            {test.comment && (
              <p className="text-sm text-gray-400">{formatTestDate(test.date)}</p>
            )}
          </div>
        </div>
        {!editing && (
          <div className="flex items-center gap-2">
            <button
              onClick={startEditing}
              className="flex items-center gap-2 px-3 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors cursor-pointer"
            >
              <Pencil size={14} />
              {t('detail.edit')}
            </button>
            <button
              onClick={() => setShowDeleteConfirm(true)}
              aria-label="Delete test"
              className="flex items-center gap-2 px-3 py-2 bg-gray-700 hover:bg-red-600 rounded-lg text-sm transition-colors cursor-pointer"
            >
              <Trash2 size={14} />
            </button>
          </div>
        )}
      </div>

      {error && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 mb-6 text-red-400">
          {error}
        </div>
      )}

      {/* Delete confirmation */}
      {showDeleteConfirm && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-xl p-4 mb-6">
          <p className="text-red-400 font-medium mb-3">{t('detail.delete.confirm')}</p>
          <div className="flex gap-2">
            <button
              onClick={handleDelete}
              disabled={deleting}
              className="px-4 py-2 bg-red-600 hover:bg-red-500 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors cursor-pointer"
            >
              {deleting ? t('detail.delete.deleting') : t('detail.delete.delete')}
            </button>
            <button
              onClick={() => setShowDeleteConfirm(false)}
              className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors cursor-pointer"
            >
              {t('common:actions.cancel')}
            </button>
          </div>
        </div>
      )}

      {/* Edit mode */}
      {editing ? (
        <div className="space-y-4">
          <div className="bg-gray-800 rounded-xl p-6 space-y-4">
            <div className="flex items-center justify-between mb-2">
              <h2 className="text-lg font-semibold">{t('detail.editTest')}</h2>
              <div className="flex gap-2">
                <button
                  onClick={cancelEditing}
                  className="flex items-center gap-1 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors cursor-pointer"
                >
                  <X size={14} />
                  {t('common:actions.cancel')}
                </button>
                <button
                  onClick={handleSave}
                  disabled={saving}
                  className="flex items-center gap-1 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors cursor-pointer"
                >
                  <Save size={14} />
                  {saving ? t('detail.saving') : t('detail.save')}
                </button>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <label htmlFor="edit-date" className="block text-sm text-gray-400 mb-1">{t('detail.labels.date')}</label>
                <input
                  id="edit-date"
                  type="date"
                  value={editDate}
                  onChange={(e) => setEditDate(e.target.value)}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
              <div>
                <label htmlFor="edit-comment" className="block text-sm text-gray-400 mb-1">{t('detail.labels.comment')}</label>
                <input
                  id="edit-comment"
                  type="text"
                  value={editComment}
                  onChange={(e) => setEditComment(e.target.value)}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
            </div>

            <h3 className="font-medium text-sm text-gray-400 mt-4">{t('detail.stagesLabel')}</h3>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-gray-400 border-b border-gray-700">
                    <th className="text-left py-2 pr-2 w-8">{t('columns.number')}</th>
                    <th className="text-left py-2 pr-2">{t('columns.speedKmh')}</th>
                    <th className="text-left py-2 pr-2">{t('columns.lactateUnit')}</th>
                    <th className="text-left py-2 pr-2">{t('columns.hrBpm')}</th>
                    <th className="text-left py-2 pr-2">{t('columns.rpe')}</th>
                    <th className="text-left py-2 pr-2">{t('columns.notes')}</th>
                    <th className="w-8"></th>
                  </tr>
                </thead>
                <tbody>
                  {editStages.map((stage, i) => (
                    <tr key={stage.id} className="border-b border-gray-700/50">
                      <td className="py-2 pr-2 text-gray-500">{i + 1}</td>
                      <td className="py-2 pr-2">
                        <input
                          type="number"
                          step="0.1"
                          value={stage.speed_kmh}
                          onChange={(e) => updateEditStage(i, 'speed_kmh', e.target.value)}
                          aria-label={t('new.stages.stageSpeedLabel', { number: i + 1 })}
                          className="w-20 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                        />
                      </td>
                      <td className="py-2 pr-2">
                        <input
                          type="number"
                          step="0.1"
                          min="0"
                          value={stage.lactate_mmol}
                          onChange={(e) => updateEditStage(i, 'lactate_mmol', e.target.value)}
                          aria-label={t('new.stages.stageLactateLabel', { number: i + 1 })}
                          className="w-20 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                        />
                      </td>
                      <td className="py-2 pr-2">
                        <input
                          type="number"
                          min="0"
                          value={stage.heart_rate_bpm}
                          onChange={(e) => updateEditStage(i, 'heart_rate_bpm', e.target.value)}
                          aria-label={t('new.stages.stageHeartRateLabel', { number: i + 1 })}
                          className="w-20 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                        />
                      </td>
                      <td className="py-2 pr-2">
                        <input
                          type="number"
                          min="6"
                          max="20"
                          value={stage.rpe}
                          onChange={(e) => updateEditStage(i, 'rpe', e.target.value)}
                          aria-label={t('new.stages.stageRpeLabel', { number: i + 1 })}
                          className="w-16 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                        />
                      </td>
                      <td className="py-2 pr-2">
                        <input
                          type="text"
                          value={stage.notes}
                          onChange={(e) => updateEditStage(i, 'notes', e.target.value)}
                          aria-label={t('new.stages.stageNotesLabel', { number: i + 1 })}
                          className="w-24 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                        />
                      </td>
                      <td className="py-2">
                        <button
                          onClick={() => removeEditStage(i)}
                          disabled={editStages.length <= 2}
                          className="text-gray-600 hover:text-red-400 disabled:opacity-30 disabled:cursor-not-allowed transition-colors cursor-pointer"
                          aria-label={t('new.stages.removeStageLabel', { number: i + 1 })}
                        >
                          <Trash2 size={14} />
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <button
              onClick={addEditStage}
              className="flex items-center gap-1 text-sm text-blue-400 hover:text-blue-300 transition-colors cursor-pointer"
            >
              <Plus size={14} />
              {t('detail.addStage')}
            </button>
          </div>
        </div>
      ) : (
        <>
          {/* Read-only test details */}
          <div className="bg-gray-800 rounded-xl p-6 mb-4">
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
              <div>
                <span className="text-gray-500 block text-xs mb-0.5">{t('detail.labels.protocol')}</span>
                <span className="font-medium">{test.protocol_type}</span>
              </div>
              <div>
                <span className="text-gray-500 block text-xs mb-0.5">{t('detail.labels.warmup')}</span>
                <span className="font-medium">{test.warmup_duration_min} {t('units.min')}</span>
              </div>
              <div>
                <span className="text-gray-500 block text-xs mb-0.5">{t('detail.labels.stageDuration')}</span>
                <span className="font-medium">{test.stage_duration_min} {t('units.min')}</span>
              </div>
              <div>
                <span className="text-gray-500 block text-xs mb-0.5">{t('detail.labels.speed')}</span>
                <span className="font-medium">{test.start_speed_kmh} + {test.speed_increment_kmh} {t('units.kmh')}</span>
              </div>
            </div>
          </div>

          {/* Source workout link */}
          {test.workout_id && (
            <div className="bg-gray-800 rounded-xl px-6 py-3 mb-4 flex items-center gap-2 text-sm">
              <span className="text-gray-400">{t('detail.sourceWorkout')}:</span>
              <span className="text-white font-medium">{workoutTitle ?? `#${test.workout_id}`}</span>
              <Link
                to={`/training/${test.workout_id}`}
                className="text-blue-400 hover:text-blue-300 transition-colors ml-1"
              >
                {t('detail.viewWorkout')}
              </Link>
            </div>
          )}

          {/* Stages table */}
          <div className="bg-gray-800 rounded-xl p-6 mb-4">
            <h2 className="font-semibold mb-3">{t('detail.stages', { count: test.stages.length })}</h2>
            {test.stages.length === 0 ? (
              <p className="text-gray-500 text-sm">{t('detail.noStages')}</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-gray-400 border-b border-gray-700">
                      <th className="text-left py-2 pr-4">{t('columns.number')}</th>
                      <th className="text-left py-2 pr-4">{t('columns.speed')}</th>
                      <th className="text-left py-2 pr-4">{t('columns.lactate')}</th>
                      <th className="text-left py-2 pr-4">{t('columns.hr')}</th>
                      <th className="text-left py-2 pr-4">{t('columns.rpe')}</th>
                      <th className="text-left py-2">{t('columns.notes')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {test.stages.map((s) => (
                      <tr key={s.stage_number} className="border-b border-gray-700/50">
                        <td className="py-2 pr-4 text-gray-500">{s.stage_number}</td>
                        <td className="py-2 pr-4">{s.speed_kmh.toFixed(1)} {t('units.kmh')}</td>
                        <td className="py-2 pr-4">{s.lactate_mmol.toFixed(1)} {t('units.mmol')}</td>
                        <td className="py-2 pr-4">{s.heart_rate_bpm > 0 ? `${s.heart_rate_bpm} ${t('units.bpm')}` : '—'}</td>
                        <td className="py-2 pr-4">{s.rpe ?? '—'}</td>
                        <td className="py-2 text-gray-400">{s.notes || '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {/* Analysis section */}
          {analysisLoading && (
            <div className="text-center py-8 text-gray-400">{t('detail.loadingAnalysis')}</div>
          )}

          {analysisError && !analysisLoading && (
            <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 mb-4 text-red-400">
              {analysisError}
            </div>
          )}

          {/* Charts - show when test has data, even before analysis loads */}
          {test.stages.length >= 2 && !editing && (
            <>
              <CollapsibleSection
                title={t('detail.sections.lactateCurve')}
                icon={<Activity size={20} />}
                isOpen={expandedSection === 'curve'}
                onToggle={() => toggleSection('curve')}
              >
                <LactateCurveChart
                  stages={test.stages}
                  thresholds={analysis?.thresholds}
                  selectedMethod={selectedMethod || analysis?.method_used}
                />
              </CollapsibleSection>

              {test.stages.some((s) => s.heart_rate_bpm > 0) && (
                <CollapsibleSection
                  title={t('detail.sections.lactateAndHr')}
                  icon={<Gauge size={20} />}
                  isOpen={expandedSection === 'dual'}
                  onToggle={() => toggleSection('dual')}
                >
                  <DualAxisChart stages={test.stages} />
                </CollapsibleSection>
              )}
            </>
          )}

          {analysis && !analysisLoading && (
            <>
              {/* Method selector */}
              {analysis.thresholds.filter((thr) => thr.valid).length > 1 && (
                <div className="bg-gray-800 rounded-xl p-4 mb-4">
                  <label htmlFor="method-select" className="block text-sm text-gray-400 mb-2">{t('detail.analysis.thresholdMethod')}</label>
                  <select
                    id="method-select"
                    value={selectedMethod}
                    onChange={(e) => handleMethodChange(e.target.value)}
                    className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  >
                    <option value="">{t('detail.analysis.autoFirstValid')}</option>
                    {analysis.thresholds
                      .filter((thr) => thr.valid)
                      .map((thr) => (
                        <option key={thr.method} value={thr.method}>
                          {thr.method} ({thr.speed_kmh.toFixed(1)} {t('units.kmh')}, {thr.lactate_mmol.toFixed(1)} {t('units.mmol')})
                        </option>
                      ))}
                  </select>
                  {analysis.method_used && (
                    <p className="text-xs text-gray-500 mt-1">{t('detail.analysis.using', { method: analysis.method_used })}</p>
                  )}
                </div>
              )}

              {/* Thresholds */}
              <CollapsibleSection
                title={t('detail.sections.thresholdResults')}
                icon={<Gauge size={20} />}
                isOpen={expandedSection === 'thresholds'}
                onToggle={() => toggleSection('thresholds')}
              >
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  {analysis.thresholds.map((thr) => (
                    <div
                      key={thr.method}
                      className={`rounded-lg border p-4 ${
                        thr.valid
                          ? thr.method === analysis.method_used
                            ? 'border-blue-500/50 bg-blue-500/10'
                            : 'border-gray-700 bg-gray-800/50'
                          : 'border-gray-700/50 bg-gray-800/30 opacity-60'
                      }`}
                    >
                      <div className="flex items-center justify-between mb-2">
                        <span className="font-medium text-sm">{thr.method}</span>
                        {thr.valid ? (
                          <span className="text-xs bg-green-500/20 text-green-400 px-2 py-0.5 rounded-full">{t('detail.analysis.valid')}</span>
                        ) : (
                          <span className="text-xs bg-gray-600/20 text-gray-500 px-2 py-0.5 rounded-full">{t('detail.analysis.na')}</span>
                        )}
                      </div>
                      {thr.valid ? (
                        <div className="space-y-1 text-sm">
                          <p><span className="text-gray-400">{t('detail.analysis.speedLabel')}</span> {thr.speed_kmh.toFixed(2)} {t('units.kmh')}</p>
                          <p><span className="text-gray-400">{t('detail.analysis.lactateLabel')}</span> {thr.lactate_mmol.toFixed(2)} {t('units.mmol')}</p>
                          {thr.heart_rate_bpm > 0 && (
                            <p><span className="text-gray-400">{t('detail.analysis.hrLabel')}</span> {thr.heart_rate_bpm} {t('units.bpm')}</p>
                          )}
                        </div>
                      ) : (
                        <p className="text-xs text-gray-500">{thr.reason}</p>
                      )}
                    </div>
                  ))}
                </div>
              </CollapsibleSection>

              {/* Traffic Lights */}
              {analysis.traffic_lights.length > 0 && (
                <CollapsibleSection
                  title={t('detail.sections.trafficLights')}
                  icon={<CircleDot size={20} />}
                  isOpen={expandedSection === 'traffic'}
                  onToggle={() => toggleSection('traffic')}
                >
                  <div className="overflow-x-auto">
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="text-gray-400 border-b border-gray-700">
                          <th className="text-left py-2 pr-4">{t('columns.stage')}</th>
                          <th className="text-left py-2 pr-4">{t('columns.speed')}</th>
                          <th className="text-left py-2 pr-4">{t('columns.lactate')}</th>
                          <th className="text-left py-2 pr-4">{t('detail.statusLabel')}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {analysis.traffic_lights.map((tl) => {
                          const colors = trafficColors[tl.light]
                          return (
                            <tr key={tl.stage_number} className="border-b border-gray-800">
                              <td className="py-2 pr-4">{tl.stage_number}</td>
                              <td className="py-2 pr-4">{tl.speed_kmh.toFixed(1)} {t('units.kmh')}</td>
                              <td className="py-2 pr-4">{tl.lactate_mmol.toFixed(1)} {t('units.mmol')}</td>
                              <td className="py-2 pr-4">
                                <span className={`inline-flex items-center gap-2 px-2 py-1 rounded-md ${colors.bg} ${colors.border} border`}>
                                  <span className={`w-2.5 h-2.5 rounded-full ${colors.dot}`} />
                                  <span className={`text-xs font-medium ${colors.text}`}>{tl.label}</span>
                                </span>
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                </CollapsibleSection>
              )}

              {/* Training Zones */}
              {analysis.zones && analysis.zones.length > 0 && (
                <CollapsibleSection
                  title={t('detail.sections.trainingZones')}
                  icon={<Activity size={20} />}
                  isOpen={expandedSection === 'zones'}
                  onToggle={() => toggleSection('zones')}
                >
                  {!analysis.zones[0]?.max_hr && (
                    <div className="bg-amber-500/10 border border-amber-500/30 rounded-lg p-3 mb-4 text-amber-400 text-sm">
                      {t('detail.analysis.maxHrWarning')}{' '}
                      <Link to="/settings" className="underline hover:text-amber-300">{t('detail.analysis.configureMaxHr')}</Link> {t('detail.zones.maxHrWarningAccurateSuffix')}
                    </div>
                  )}
                  <div className="flex gap-2 mb-4" role="group" aria-label={t('detail.zones.selectZoneSystem')}>
                    {analysis.zones.map((zr, idx) => (
                      <button
                        key={zr.system}
                        onClick={() => setActiveZoneSystem(idx)}
                        aria-pressed={activeZoneSystem === idx}
                        className={`px-3 py-1.5 text-sm rounded-lg transition-colors cursor-pointer ${
                          activeZoneSystem === idx
                            ? 'bg-blue-500/20 text-blue-400 border border-blue-500/40'
                            : 'bg-gray-700 text-gray-400 border border-gray-600 hover:text-white'
                        }`}
                      >
                        {zr.system === 'olympiatoppen' ? t('detail.zones.olympiatoppen') : t('detail.zones.norwegian')}
                      </button>
                    ))}
                  </div>
                  {(() => {
                    const zoneIdx = activeZoneSystem < analysis.zones.length ? activeZoneSystem : 0
                    const zr = analysis.zones[zoneIdx]
                    return (
                      <div className="space-y-2">
                        <p className="text-xs text-gray-500 mb-3">
                          {t('detail.analysis.thresholdBasis', { speed: zr.threshold_speed_kmh.toFixed(1) })}
                          {zr.threshold_hr > 0 && ` ${t('detail.analysis.thresholdHrSuffix', { hr: zr.threshold_hr })}`}
                          {zr.max_hr ? ` ${t('detail.analysis.maxHrSuffix', { maxHr: zr.max_hr })}` : ''}
                        </p>
                        {zr.zones.map((z) => (
                          <div
                            key={z.zone}
                            className={`rounded-lg border p-3 ${zoneColors[z.zone - 1] || 'border-gray-700 bg-gray-800'}`}
                          >
                            <div className="flex items-center justify-between mb-1">
                              <span className="font-medium text-sm">{z.name}</span>
                              <span className="text-xs opacity-75">{z.description}</span>
                            </div>
                            <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs opacity-80">
                              <span>{t('detail.zones.speedRange', { min: z.min_speed_kmh.toFixed(1), max: z.max_speed_kmh.toFixed(1) })}</span>
                              {z.max_hr > 0 && <span>{t('detail.zones.hrRange', { min: z.min_hr, max: z.max_hr })}</span>}
                              <span>{z.lactate_to >= 20 ? t('detail.zones.lactateRangeMax', { from: z.lactate_from.toFixed(1) }) : t('detail.zones.lactateRange', { from: z.lactate_from.toFixed(1), to: z.lactate_to.toFixed(1) })}</span>
                            </div>
                          </div>
                        ))}
                      </div>
                    )
                  })()}
                </CollapsibleSection>
              )}

              {/* Race Predictions */}
              {analysis.predictions && analysis.predictions.length > 0 && (
                <CollapsibleSection
                  title={t('detail.sections.racePredictions')}
                  icon={<Timer size={20} />}
                  isOpen={expandedSection === 'predictions'}
                  onToggle={() => toggleSection('predictions')}
                >
                  <p className="text-xs text-gray-500 mb-3">
                    {t('detail.analysis.riegelFormula')}
                  </p>
                  <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                    {analysis.predictions.map((p) => (
                      <div key={p.name} className="bg-gray-800/50 border border-gray-700 rounded-lg p-4">
                        <div className="text-sm font-medium mb-2">{p.name}</div>
                        <div className="text-2xl font-bold text-blue-400 mb-2">{p.time_formatted}</div>
                        <div className="flex justify-between text-xs text-gray-400">
                          <span>{p.pace_min_km}</span>
                          <span>{p.speed_kmh.toFixed(1)} {t('units.kmh')}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                </CollapsibleSection>
              )}
            </>
          )}
        </>
      )}
    </div>
  )
}

function CollapsibleSection({
  title,
  icon,
  isOpen,
  onToggle,
  children,
}: {
  title: string
  icon: React.ReactNode
  isOpen: boolean
  onToggle: () => void
  children: React.ReactNode
}) {
  const contentId = `section-${title.replace(/\s+/g, '-').toLowerCase()}`
  return (
    <div className="bg-gray-800 rounded-xl mb-4 overflow-hidden">
      <button
        onClick={onToggle}
        aria-expanded={isOpen}
        aria-controls={contentId}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-gray-700/50 transition-colors cursor-pointer"
      >
        <span className="text-blue-400">{icon}</span>
        <span className="font-semibold flex-1">{title}</span>
        {isOpen ? <ChevronUp size={18} className="text-gray-400" /> : <ChevronDown size={18} className="text-gray-400" />}
      </button>
      {isOpen && <div id={contentId} className="px-4 pb-4">{children}</div>}
    </div>
  )
}
