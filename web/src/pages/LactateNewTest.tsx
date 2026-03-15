import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../auth'
import { Activity, ArrowLeft, Plus, Trash2, ChevronRight, ChevronLeft } from 'lucide-react'

interface StageInput {
  id: number
  speed_kmh: string
  lactate_mmol: string
  heart_rate_bpm: string
  rpe: string
  notes: string
}

type WizardStep = 'protocol' | 'stages' | 'review'

let _stageIdCounter = 0

const defaultProtocol = {
  date: new Date().toISOString().slice(0, 10),
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

export default function LactateNewTest() {
  const { user } = useAuth()
  const navigate = useNavigate()
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

  if (!user) {
    return (
      <div className="p-6">
        <p className="text-gray-400">Sign in to record a lactate test.</p>
      </div>
    )
  }

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
      setError('Date is required')
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
      setError('Record at least 2 stages with lactate values')
      return
    }
    for (let i = 0; i < filledStages.length; i++) {
      const speed = parseFloat(filledStages[i].speed_kmh)
      if (!isFinite(speed) || speed <= 0) {
        setError(`Stage ${i + 1}: speed must be a positive number`)
        return
      }
      const lac = parseFloat(filledStages[i].lactate_mmol)
      if (!isFinite(lac) || lac < 0) {
        setError(`Stage ${i + 1}: lactate must be a non-negative number`)
        return
      }
      if (filledStages[i].rpe !== '') {
        const rpe = parseInt(filledStages[i].rpe)
        if (isNaN(rpe) || rpe < 6 || rpe > 20) {
          setError(`Stage ${i + 1}: RPE must be between 6 and 20`)
          return
        }
      }
      if (i > 0 && speed <= parseFloat(filledStages[i - 1].speed_kmh)) {
        setError(`Stage speeds must be strictly increasing (stages ${i} and ${i + 1})`)
        return
      }
    }
    setError('')
    setStep('review')
  }

  const handleSubmit = async () => {
    setSaving(true)
    setError('')
    try {
      const filledStages = stages
        .filter((s) => s.lactate_mmol !== '')
        .map((s, i) => ({
          stage_number: i + 1,
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
        body: JSON.stringify(body),
      })

      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'Failed to save test' }))
        throw new Error(data.error || 'Failed to save test')
      }

      const data = await res.json()
      navigate(`/lactate/${data.test.id}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save test')
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
          aria-label="Back to tests"
        >
          <ArrowLeft size={20} />
        </button>
        <Activity size={24} className="text-blue-400" />
        <h1 className="text-2xl font-bold">Record Test</h1>
      </div>

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
              {s === 'protocol' ? '1. Protocol' : s === 'stages' ? '2. Stages' : '3. Review'}
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
          <h2 className="text-lg font-semibold mb-2">Test Protocol</h2>

          <div>
            <label htmlFor="test-date" className="block text-sm text-gray-400 mb-1">Date</label>
            <input
              id="test-date"
              type="date"
              value={protocol.date}
              onChange={(e) => updateProtocol('date', e.target.value)}
              className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>

          <div>
            <label htmlFor="test-comment" className="block text-sm text-gray-400 mb-1">Comment (optional)</label>
            <input
              id="test-comment"
              type="text"
              value={protocol.comment}
              onChange={(e) => updateProtocol('comment', e.target.value)}
              placeholder="e.g., Pre-season test"
              className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>

          <div>
            <label htmlFor="protocol-type" className="block text-sm text-gray-400 mb-1">Protocol Type</label>
            <select
              id="protocol-type"
              value={protocol.protocol_type}
              onChange={(e) => updateProtocol('protocol_type', e.target.value)}
              className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              <option value="standard">Standard</option>
              <option value="custom">Custom</option>
            </select>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label htmlFor="warmup" className="block text-sm text-gray-400 mb-1">Warmup (min)</label>
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
              <label htmlFor="stage-duration" className="block text-sm text-gray-400 mb-1">Stage Duration (min)</label>
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
              <label htmlFor="start-speed" className="block text-sm text-gray-400 mb-1">Start Speed (km/h)</label>
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
              <label htmlFor="speed-increment" className="block text-sm text-gray-400 mb-1">Speed Increment (km/h)</label>
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

          <div className="flex justify-end pt-2">
            <button
              onClick={goToStages}
              className="flex items-center gap-2 px-5 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium transition-colors cursor-pointer"
            >
              Next: Record Stages
              <ChevronRight size={16} />
            </button>
          </div>
        </div>
      )}

      {/* Step 2: Stages */}
      {step === 'stages' && (
        <div className="bg-gray-800 rounded-xl p-6">
          <h2 className="text-lg font-semibold mb-4">Stage Data</h2>
          <p className="text-sm text-gray-400 mb-4">
            Enter lactate and heart rate values for each stage. Speed is pre-filled from your protocol but can be adjusted.
          </p>

          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-gray-400 border-b border-gray-700">
                  <th className="text-left py-2 pr-2 w-8">#</th>
                  <th className="text-left py-2 pr-2">Speed (km/h)</th>
                  <th className="text-left py-2 pr-2">Lactate (mmol/L)</th>
                  <th className="text-left py-2 pr-2">HR (bpm)</th>
                  <th className="text-left py-2 pr-2">RPE</th>
                  <th className="text-left py-2 pr-2">Notes</th>
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
                        aria-label={`Stage ${i + 1} speed`}
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
                        aria-label={`Stage ${i + 1} lactate`}
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
                        aria-label={`Stage ${i + 1} heart rate`}
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
                        aria-label={`Stage ${i + 1} RPE`}
                        className="w-16 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
                      />
                    </td>
                    <td className="py-2 pr-2">
                      <input
                        type="text"
                        value={stage.notes}
                        onChange={(e) => updateStage(i, 'notes', e.target.value)}
                        placeholder="—"
                        aria-label={`Stage ${i + 1} notes`}
                        className="w-24 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-sm placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
                      />
                    </td>
                    <td className="py-2">
                      <button
                        onClick={() => removeStage(i)}
                        disabled={stages.length <= 2}
                        className="text-gray-600 hover:text-red-400 disabled:opacity-30 disabled:cursor-not-allowed transition-colors cursor-pointer"
                        aria-label={`Remove stage ${i + 1}`}
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
            Add Stage
          </button>

          <div className="flex justify-between pt-4 mt-4 border-t border-gray-700">
            <button
              onClick={() => { setStep('protocol'); setError('') }}
              className="flex items-center gap-2 px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors cursor-pointer"
            >
              <ChevronLeft size={16} />
              Back
            </button>
            <button
              onClick={goToReview}
              className="flex items-center gap-2 px-5 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium transition-colors cursor-pointer"
            >
              Next: Review
              <ChevronRight size={16} />
            </button>
          </div>
        </div>
      )}

      {/* Step 3: Review */}
      {step === 'review' && (
        <div className="space-y-4">
          <div className="bg-gray-800 rounded-xl p-6">
            <h2 className="text-lg font-semibold mb-4">Review Test</h2>

            <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm mb-6">
              <div>
                <span className="text-gray-400">Date:</span>{' '}
                {(() => {
                  const [y, m, d] = protocol.date.split('-').map(Number)
                  return new Date(y, m - 1, d).toLocaleDateString(undefined, {
                    year: 'numeric', month: 'long', day: 'numeric',
                  })
                })()}
              </div>
              <div>
                <span className="text-gray-400">Protocol:</span> {protocol.protocol_type}
              </div>
              {protocol.comment && (
                <div className="col-span-2">
                  <span className="text-gray-400">Comment:</span> {protocol.comment}
                </div>
              )}
              <div>
                <span className="text-gray-400">Warmup:</span> {protocol.warmup_duration_min} min
              </div>
              <div>
                <span className="text-gray-400">Stage duration:</span> {protocol.stage_duration_min} min
              </div>
              <div>
                <span className="text-gray-400">Start speed:</span> {protocol.start_speed_kmh} km/h
              </div>
              <div>
                <span className="text-gray-400">Increment:</span> +{protocol.speed_increment_kmh} km/h
              </div>
            </div>

            <h3 className="font-medium text-sm text-gray-400 mb-2">
              Stages ({stages.filter((s) => s.lactate_mmol !== '').length} recorded)
            </h3>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-gray-400 border-b border-gray-700">
                    <th className="text-left py-2 pr-4">#</th>
                    <th className="text-left py-2 pr-4">Speed</th>
                    <th className="text-left py-2 pr-4">Lactate</th>
                    <th className="text-left py-2 pr-4">HR</th>
                    <th className="text-left py-2 pr-4">RPE</th>
                    <th className="text-left py-2">Notes</th>
                  </tr>
                </thead>
                <tbody>
                  {stages
                    .filter((s) => s.lactate_mmol !== '')
                    .map((s, i) => (
                      <tr key={i} className="border-b border-gray-700/50">
                        <td className="py-2 pr-4 text-gray-500">{i + 1}</td>
                        <td className="py-2 pr-4">{parseFloat(s.speed_kmh).toFixed(1)} km/h</td>
                        <td className="py-2 pr-4">{parseFloat(s.lactate_mmol).toFixed(1)} mmol/L</td>
                        <td className="py-2 pr-4">{s.heart_rate_bpm || '—'}{s.heart_rate_bpm ? ' bpm' : ''}</td>
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
              Back
            </button>
            <button
              onClick={handleSubmit}
              disabled={saving}
              className="flex items-center gap-2 px-6 py-2 bg-green-600 hover:bg-green-500 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors cursor-pointer"
            >
              {saving ? 'Saving...' : 'Save Test'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
