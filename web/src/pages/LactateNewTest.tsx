import { useState, useRef, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../auth'
import { Activity, ArrowLeft, Plus, Trash2, ChevronRight, ChevronLeft, Upload, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDate } from '../utils/formatDate'
import type { Workout } from '../types/training'
import LactateImportDialog from '../components/LactateImportDialog'

interface StageInput {
  id: number
  speed_kmh: string
  lactate_mmol: string
  heart_rate_bpm: string
  rpe: string
  notes: string
}

type WizardStep = 'protocol' | 'stages' | 'review'
type Mode = 'manual' | 'import'

let _stageIdCounter = 0

function localDateString() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const defaultProtocol = {
  date: localDateString(),
  comment: '',
  protocol_type: 'standard' as 'standard' | 'custom',
  warmup_duration_min: '10',
  stage_duration_min: '5',
  start_speed_kmh: '11.5',
  speed_increment_kmh: '0.5',
}

function makeEmptyStage(stageNumber: number, startSpeed: number, increment: number): StageInput {
  return {
    id: ++_stageIdCounter,
    speed_kmh: (startSpeed + (stageNumber - 1) * increment).toFixed(1),
    lactate_mmol: '',
    heart_rate_bpm: '',
    rpe: '',
    notes: '',
  }
}

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

export default function LactateNewTest() {
  const { user } = useAuth()
  const { t } = useTranslation(['lactate', 'common'])
  const navigate = useNavigate()
  const [mode, setMode] = useState<Mode>('manual')
  const [step, setStep] = useState<WizardStep>('protocol')
  const [protocol, setProtocol] = useState(defaultProtocol)
  const [stages, setStages] = useState<StageInput[]>(() => {
    const initial: StageInput[] = []
    for (let i = 1; i <= 5; i++) {
      initial.push(makeEmptyStage(i, 11.5, 0.5))
    }
    return initial
  })
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const abortRef = useRef<AbortController | null>(null)

  // Import mode state
  const [workouts, setWorkouts] = useState<Workout[]>([])
  const [workoutsLoading, setWorkoutsLoading] = useState(false)
  const [workoutsError, setWorkoutsError] = useState('')
  const [workoutSearch, setWorkoutSearch] = useState('')
  const [selectedWorkoutId, setSelectedWorkoutId] = useState<string | null>(null)

  const formatDuration = (seconds: number): string => {
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (h > 0) return `${h}${t('units.hourAbbr')} ${m}${t('units.minuteAbbr')}`
    return `${m}${t('units.minuteAbbr')}`
  }

  // Abort any in-flight save on unmount
  useEffect(() => {
    return () => { abortRef.current?.abort() }
  }, [])

  if (!user) {
    return (
      <div className="p-6">
        <p className="text-gray-400">{t('signInToRecord')}</p>
      </div>
    )
  }

  const handleSwitchToImport = async () => {
    setMode('import')
    setError('')
    if (workouts.length === 0 && !workoutsLoading) {
      setWorkoutsLoading(true)
      setWorkoutsError('')
      try {
        const res = await fetch('/api/training/workouts', { credentials: 'include' })
        if (!res.ok) throw new Error(t('errors.failedToLoadWorkouts'))
        const data = await res.json()
        setWorkouts(data.workouts || [])
      } catch (err) {
        setWorkoutsError(err instanceof Error ? err.message : t('errors.failedToLoadWorkouts'))
      } finally {
        setWorkoutsLoading(false)
      }
    }
  }

  const handleSwitchToManual = () => {
    setMode('manual')
    setWorkoutsError('')
    setWorkoutSearch('')
  }

  const filteredWorkouts = workouts.filter((w) => {
    if (!workoutSearch.trim()) return true
    const q = workoutSearch.toLowerCase()
    return w.title.toLowerCase().includes(q) || w.sport.toLowerCase().includes(q)
  })

  const updateProtocol = (field: string, value: string) => {
    setProtocol((prev) => ({ ...prev, [field]: value }))
  }

  const recalculateSpeeds = (startSpeed: number, increment: number) => {
    setStages((prev) =>
      prev.map((s, i) => ({
        ...s,
        speed_kmh: (startSpeed + i * increment).toFixed(1),
      }))
    )
  }

  const addStage = () => {
    const startSpeed = parseFloat(protocol.start_speed_kmh) || 11.5
    const increment = parseFloat(protocol.speed_increment_kmh) || 0.5
    setStages((prev) => [
      ...prev,
      makeEmptyStage(prev.length + 1, startSpeed, increment),
    ])
  }

  const removeStage = (index: number) => {
    if (stages.length <= 2) return
    setStages((prev) => prev.filter((_, i) => i !== index))
  }

  const updateStage = (index: number, field: keyof StageInput, value: string) => {
    setStages((prev) =>
      prev.map((s, i) => (i === index ? { ...s, [field]: value } : s))
    )
  }

  const goToStages = () => {
    if (!protocol.date) {
      setError(t('errors.dateRequired'))
      return
    }
    setError('')
    const startSpeed = parseFloat(protocol.start_speed_kmh) || 11.5
    const increment = parseFloat(protocol.speed_increment_kmh) || 0.5
    recalculateSpeeds(startSpeed, increment)
    setStep('stages')
  }

  const goToReview = () => {
    const filledStages = stages.filter((s) => s.lactate_mmol !== '')
    if (filledStages.length < 2) {
      setError(t('errors.minTwoStages'))
      return
    }
    for (let i = 0; i < filledStages.length; i++) {
      const speed = parseFloat(filledStages[i].speed_kmh)
      if (!isFinite(speed) || speed <= 0) {
        setError(t('errors.speedPositive', { number: i + 1 }))
        return
      }
      const lac = parseFloat(filledStages[i].lactate_mmol)
      if (!isFinite(lac) || lac < 0) {
        setError(t('errors.lactateNonNegative', { number: i + 1 }))
        return
      }
      if (filledStages[i].rpe !== '') {
        const rpe = parseInt(filledStages[i].rpe)
        if (isNaN(rpe) || rpe < 6 || rpe > 20) {
          setError(t('errors.rpeRange', { number: i + 1 }))
          return
        }
      }
      if (i > 0 && speed <= parseFloat(filledStages[i - 1].speed_kmh)) {
        setError(t('errors.speedsIncreasing', { a: i, b: i + 1 }))
        return
      }
    }
    setError('')
    setStep('review')
  }

  const handleSubmit = async () => {
    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setSaving(true)
    setError('')
    try {
      const filledStages = stages
        .filter((s) => s.lactate_mmol !== '')
        .map((s, i) => ({
          stage_number: i,
          speed_kmh: parseFloat(s.speed_kmh),
          lactate_mmol: parseFloat(s.lactate_mmol),
          heart_rate_bpm: parseInt(s.heart_rate_bpm) || 0,
          rpe: s.rpe ? parseInt(s.rpe) : null,
          notes: s.notes,
        }))

      const body = {
        date: protocol.date,
        comment: protocol.comment,
        protocol_type: protocol.protocol_type,
        warmup_duration_min: parseInt(protocol.warmup_duration_min) || 10,
        stage_duration_min: parseInt(protocol.stage_duration_min) || 5,
        start_speed_kmh: parseFloat(protocol.start_speed_kmh) || 11.5,
        speed_increment_kmh: parseFloat(protocol.speed_increment_kmh) || 0.5,
        stages: filledStages,
      }

      const res = await fetch('/api/lactate/tests', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        signal: controller.signal,
        body: JSON.stringify(body),
      })

      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: t('errors.failedToSaveTest') }))
        throw new Error(data.error || t('errors.failedToSaveTest'))
      }

      const data = await res.json()
      navigate(`/lactate/${data.test.id}`)
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('errors.failedToSaveTest'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="max-w-3xl mx-auto p-4 md:p-6">
      <div className="flex items-center gap-3 mb-6">
        <button
          onClick={() => navigate('/lactate')}
          className="text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('backToTests')}
        >
          <ArrowLeft size={20} />
        </button>
        <Activity size={24} className="text-blue-400" />
        <h1 className="text-2xl font-bold">{t('new.title')}</h1>
      </div>

      {/* Import mode: Workout picker */}
      {mode === 'import' && (
        <div className="bg-gray-800 rounded-xl p-6 space-y-4">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold">{t('new.workoutPicker.title')}</h2>
            <button
              onClick={handleSwitchToManual}
              className="flex items-center gap-1 text-sm text-gray-400 hover:text-white transition-colors cursor-pointer"
            >
              <ChevronLeft size={16} />
              {t('new.workoutPicker.backToManual')}
            </button>
          </div>

          <div className="relative">
            <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" />
            <input
              type="search"
              value={workoutSearch}
              onChange={(e) => setWorkoutSearch(e.target.value)}
              placeholder={t('new.workoutPicker.searchPlaceholder')}
              aria-label={t('new.workoutPicker.searchPlaceholder')}
              className="w-full bg-gray-700 border border-gray-600 rounded-lg pl-9 pr-3 py-2 text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
            />
          </div>

          {workoutsError && (
            <p className="text-sm text-red-400">{workoutsError}</p>
          )}

          {workoutsLoading ? (
            <p className="text-sm text-gray-400 py-4 text-center">{t('new.workoutPicker.loading')}</p>
          ) : filteredWorkouts.length === 0 ? (
            <p className="text-sm text-gray-400 py-4 text-center">
              {workoutSearch.trim() ? t('new.workoutPicker.noResults') : t('new.workoutPicker.empty')}
            </p>
          ) : (
            <div className="divide-y divide-gray-700 max-h-96 overflow-y-auto rounded-lg border border-gray-700">
              {filteredWorkouts.map((w) => (
                <button
                  key={w.id}
                  onClick={() => setSelectedWorkoutId(String(w.id))}
                  className="w-full flex items-center gap-3 px-4 py-3 hover:bg-gray-700 transition-colors text-left cursor-pointer"
                >
                  <span className="text-lg shrink-0">{sportIcons[w.sport] ?? sportIcons.other}</span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-white truncate">
                      {w.title || w.sport.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())}
                    </p>
                    <p className="text-xs text-gray-400">
                      {formatDate(w.started_at, { year: 'numeric', month: 'short', day: 'numeric' })}
                      {w.duration_seconds > 0 && <span> · {formatDuration(w.duration_seconds)}</span>}
                    </p>
                  </div>
                  <ChevronRight size={16} className="text-gray-500 shrink-0" />
                </button>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Manual mode: wizard */}
      {mode === 'manual' && (
        <>
          {/* Step indicator */}
          <div className="flex items-center gap-2 mb-6 text-sm">
            {(['protocol', 'stages', 'review'] as WizardStep[]).map((s, i) => (
              <div key={s} className="flex items-center gap-2">
                {i > 0 && <ChevronRight size={14} className="text-gray-600" />}
                <span
                  className={`px-3 py-1 rounded-full ${
                    step === s
                      ? 'bg-blue-600 text-white'
                      : 'bg-gray-800 text-gray-500'
                  }`}
                >
                  {s === 'protocol' ? t('new.steps.protocol') : s === 'stages' ? t('new.steps.stages') : t('new.steps.review')}
                </span>
              </div>
            ))}
          </div>

          {error && (
            <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 mb-6 text-red-400">
              {error}
            </div>
          )}

          {/* Step 1: Protocol */}
          {step === 'protocol' && (
            <div className="bg-gray-800 rounded-xl p-6 space-y-4">
              <h2 className="text-lg font-semibold mb-2">{t('new.protocol.title')}</h2>

              <div>
                <label htmlFor="test-date" className="block text-sm text-gray-400 mb-1">{t('new.protocol.date')}</label>
                <input
                  id="test-date"
                  type="date"
                  value={protocol.date}
                  onChange={(e) => updateProtocol('date', e.target.value)}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>

              <div>
                <label htmlFor="test-comment" className="block text-sm text-gray-400 mb-1">{t('new.protocol.comment')}</label>
                <input
                  id="test-comment"
                  type="text"
                  value={protocol.comment}
                  onChange={(e) => updateProtocol('comment', e.target.value)}
                  placeholder={t('new.protocol.commentPlaceholder')}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>

              <div>
                <label htmlFor="protocol-type" className="block text-sm text-gray-400 mb-1">{t('new.protocol.protocolType')}</label>
                <select
                  id="protocol-type"
                  value={protocol.protocol_type}
                  onChange={(e) => updateProtocol('protocol_type', e.target.value)}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="standard">{t('new.protocol.standard')}</option>
                  <option value="custom">{t('new.protocol.custom')}</option>
                </select>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label htmlFor="warmup" className="block text-sm text-gray-400 mb-1">{t('new.protocol.warmup')}</label>
                  <input
                    id="warmup"
                    type="number"
                    min="0"
                    value={protocol.warmup_duration_min}
                    onChange={(e) => updateProtocol('warmup_duration_min', e.target.value)}
                    className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
                <div>
                  <label htmlFor="stage-duration" className="block text-sm text-gray-400 mb-1">{t('new.protocol.stageDuration')}</label>
                  <input
                    id="stage-duration"
                    type="number"
                    min="1"
                    max="30"
                    value={protocol.stage_duration_min}
                    onChange={(e) => updateProtocol('stage_duration_min', e.target.value)}
                    className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label htmlFor="start-speed" className="block text-sm text-gray-400 mb-1">{t('new.protocol.startSpeed')}</label>
                  <input
                    id="start-speed"
                    type="number"
                    step="0.5"
                    min="0.1"
                    value={protocol.start_speed_kmh}
                    onChange={(e) => updateProtocol('start_speed_kmh', e.target.value)}
                    className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
                <div>
                  <label htmlFor="speed-increment" className="block text-sm text-gray-400 mb-1">{t('new.protocol.speedIncrement')}</label>
                  <input
                    id="speed-increment"
                    type="number"
                    step="0.5"
                    min="0.1"
                    value={protocol.speed_increment_kmh}
                    onChange={(e) => updateProtocol('speed_increment_kmh', e.target.value)}
                    className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
              </div>

              <div className="flex items-center justify-between pt-2">
                <button
                  onClick={handleSwitchToImport}
                  className="flex items-center gap-2 px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors cursor-pointer"
                >
                  <Upload size={16} />
                  {t('new.importFromWorkout')}
                </button>
                <button
                  onClick={goToStages}
                  className="flex items-center gap-2 px-5 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium transition-colors cursor-pointer"
                >
                  {t('new.protocol.nextStages')}
                  <ChevronRight size={16} />
                </button>
              </div>
            </div>
          )}

          {/* Step 2: Stages */}
          {step === 'stages' && (
            <div className="bg-gray-800 rounded-xl p-6">
              <h2 className="text-lg font-semibold mb-4">{t('new.stages.title')}</h2>
              <p className="text-sm text-gray-400 mb-4">
                {t('new.stages.hint')}
              </p>

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
                    {stages.map((stage, i) => (
                      <tr key={stage.id} className="border-b border-gray-700/50">
                        <td className="py-2 pr-2 text-gray-500">{i + 1}</td>
                        <td className="py-2 pr-2">
                          <input
                            type="number"
                            step="0.1"
                            value={stage.speed_kmh}
                            onChange={(e) => updateStage(i, 'speed_kmh', e.target.value)}
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
                            onChange={(e) => updateStage(i, 'lactate_mmol', e.target.value)}
                            placeholder="—"
                            aria-label={t('new.stages.stageLactateLabel', { number: i + 1 })}
                            className="w-20 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
                          />
                        </td>
                        <td className="py-2 pr-2">
                          <input
                            type="number"
                            min="0"
                            value={stage.heart_rate_bpm}
                            onChange={(e) => updateStage(i, 'heart_rate_bpm', e.target.value)}
                            placeholder="—"
                            aria-label={t('new.stages.stageHeartRateLabel', { number: i + 1 })}
                            className="w-20 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
                          />
                        </td>
                        <td className="py-2 pr-2">
                          <input
                            type="number"
                            min="6"
                            max="20"
                            value={stage.rpe}
                            onChange={(e) => updateStage(i, 'rpe', e.target.value)}
                            placeholder="—"
                            aria-label={t('new.stages.stageRpeLabel', { number: i + 1 })}
                            className="w-16 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
                          />
                        </td>
                        <td className="py-2 pr-2">
                          <input
                            type="text"
                            value={stage.notes}
                            onChange={(e) => updateStage(i, 'notes', e.target.value)}
                            placeholder="—"
                            aria-label={t('new.stages.stageNotesLabel', { number: i + 1 })}
                            className="w-24 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
                          />
                        </td>
                        <td className="py-2">
                          <button
                            onClick={() => removeStage(i)}
                            disabled={stages.length <= 2}
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
                onClick={addStage}
                className="flex items-center gap-1 mt-3 text-sm text-blue-400 hover:text-blue-300 transition-colors cursor-pointer"
              >
                <Plus size={14} />
                {t('new.stages.addStage')}
              </button>

              <div className="flex justify-between pt-4 mt-4 border-t border-gray-700">
                <button
                  onClick={() => { setStep('protocol'); setError('') }}
                  className="flex items-center gap-2 px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors cursor-pointer"
                >
                  <ChevronLeft size={16} />
                  {t('common:actions.back')}
                </button>
                <button
                  onClick={goToReview}
                  className="flex items-center gap-2 px-5 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium transition-colors cursor-pointer"
                >
                  {t('new.stages.nextReview')}
                  <ChevronRight size={16} />
                </button>
              </div>
            </div>
          )}

          {/* Step 3: Review */}
          {step === 'review' && (
            <div className="space-y-4">
              <div className="bg-gray-800 rounded-xl p-6">
                <h2 className="text-lg font-semibold mb-4">{t('new.review.title')}</h2>

                <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm mb-6">
                  <div>
                    <span className="text-gray-400">{t('new.review.dateLabel')}</span>{' '}
                    {(() => {
                      const [y, m, d] = protocol.date.split('-').map(Number)
                      return formatDate(new Date(y, m - 1, d), {
                        year: 'numeric', month: 'long', day: 'numeric',
                      })
                    })()}
                  </div>
                  <div>
                    <span className="text-gray-400">{t('new.review.protocolLabel')}</span> {protocol.protocol_type}
                  </div>
                  {protocol.comment && (
                    <div className="col-span-2">
                      <span className="text-gray-400">{t('new.review.commentLabel')}</span> {protocol.comment}
                    </div>
                  )}
                  <div>
                    <span className="text-gray-400">{t('new.review.warmupLabel')}</span> {protocol.warmup_duration_min} {t('units.min')}
                  </div>
                  <div>
                    <span className="text-gray-400">{t('new.review.stageDurationLabel')}</span> {protocol.stage_duration_min} {t('units.min')}
                  </div>
                  <div>
                    <span className="text-gray-400">{t('new.review.startSpeedLabel')}</span> {protocol.start_speed_kmh} {t('units.kmh')}
                  </div>
                  <div>
                    <span className="text-gray-400">{t('new.review.incrementLabel')}</span> +{protocol.speed_increment_kmh} {t('units.kmh')}
                  </div>
                </div>

                <h3 className="font-medium text-sm text-gray-400 mb-2">
                  {t('new.review.stagesRecorded', { count: stages.filter((s) => s.lactate_mmol !== '').length })}
                </h3>
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
                      {stages
                        .filter((s) => s.lactate_mmol !== '')
                        .map((s, i) => (
                          <tr key={i} className="border-b border-gray-700/50">
                            <td className="py-2 pr-4 text-gray-500">{i + 1}</td>
                            <td className="py-2 pr-4">{parseFloat(s.speed_kmh).toFixed(1)} {t('units.kmh')}</td>
                            <td className="py-2 pr-4">{parseFloat(s.lactate_mmol).toFixed(1)} {t('units.mmol')}</td>
                            <td className="py-2 pr-4">{s.heart_rate_bpm || '—'}{s.heart_rate_bpm ? ` ${t('units.bpm')}` : ''}</td>
                            <td className="py-2 pr-4">{s.rpe || '—'}</td>
                            <td className="py-2 text-gray-400">{s.notes || '—'}</td>
                          </tr>
                        ))}
                    </tbody>
                  </table>
                </div>
              </div>

              <div className="flex justify-between">
                <button
                  onClick={() => { setStep('stages'); setError('') }}
                  className="flex items-center gap-2 px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors cursor-pointer"
                >
                  <ChevronLeft size={16} />
                  {t('common:actions.back')}
                </button>
                <button
                  onClick={handleSubmit}
                  disabled={saving}
                  className="flex items-center gap-2 px-6 py-2 bg-green-600 hover:bg-green-500 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors cursor-pointer"
                >
                  {saving ? t('new.review.saving') : t('new.review.saveTest')}
                </button>
              </div>
            </div>
          )}
        </>
      )}

      {/* LactateImportDialog: shown when a workout is selected from the picker */}
      {selectedWorkoutId && (
        <LactateImportDialog
          workoutId={selectedWorkoutId}
          onClose={() => setSelectedWorkoutId(null)}
          onSuccess={(testId) => navigate(`/lactate/${testId}`)}
        />
      )}
    </div>
  )
}
