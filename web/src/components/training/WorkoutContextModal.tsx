import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../ui/dialog'

export type Surface = 'Treadmill' | 'Outside' | ''
export type RunType = 'slow' | 'interval' | ''
export type HRSource = 'chest' | 'watch' | ''

export interface SpeedPlanSegment {
  kind: string
  speed_kmph: number
  duration_sec: number
  repeats?: number
  same_as_previous?: boolean
}

export interface WorkoutContext {
  workout_id?: number
  surface: string
  run_type: string
  hr_source: string
  feel_notes: string
  speed_plan: SpeedPlanSegment[]
  completed_at?: string | null
}

interface WorkoutContextModalProps {
  workoutId: string
  isOpen: boolean
  onClose: () => void
  initialContext?: WorkoutContext
}

interface ToggleOption<T extends string> {
  value: T
  label: string
}

interface ToggleGroupProps<T extends string> {
  legend: string
  name: string
  value: T
  options: ToggleOption<T>[]
  onChange: (value: T) => void
}

function ToggleGroup<T extends string>({ legend, name, value, options, onChange }: ToggleGroupProps<T>) {
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="text-sm font-medium text-gray-300">{legend}</legend>
      <div role="radiogroup" aria-label={legend} className="grid grid-cols-2 gap-2">
        {options.map((option) => {
          const selected = option.value === value
          return (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={selected}
              data-testid={`toggle-${name}-${option.value}`}
              onClick={() => onChange(option.value)}
              className={`min-h-[44px] rounded-lg border px-3 py-2 text-sm font-medium transition-colors
                ${selected
                  ? 'border-blue-500 bg-blue-600 text-white'
                  : 'border-gray-700 bg-gray-800 text-gray-300 hover:border-gray-500 hover:text-white'
                }`}
            >
              {option.label}
            </button>
          )
        })}
      </div>
    </fieldset>
  )
}

export default function WorkoutContextModal({
  workoutId,
  isOpen,
  onClose,
  initialContext,
}: WorkoutContextModalProps) {
  const { t } = useTranslation('training')
  const titleId = useId()
  const feelNotesId = useId()

  const [surface, setSurface] = useState<Surface>((initialContext?.surface as Surface) ?? '')
  const [runType, setRunType] = useState<RunType>((initialContext?.run_type as RunType) ?? '')
  const [hrSource, setHrSource] = useState<HRSource>((initialContext?.hr_source as HRSource) ?? '')
  const [feelNotes, setFeelNotes] = useState<string>(initialContext?.feel_notes ?? '')
  const [speedPlan, setSpeedPlan] = useState<SpeedPlanSegment[]>(initialContext?.speed_plan ?? [])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  function handleSurfaceChange(next: Surface) {
    setSurface(next)
    if (next !== 'Treadmill') {
      setSpeedPlan([])
    }
  }

  // Reset local state when the modal is reopened with a different context.
  useEffect(() => {
    if (!isOpen) return
    setSurface((initialContext?.surface as Surface) ?? '')
    setRunType((initialContext?.run_type as RunType) ?? '')
    setHrSource((initialContext?.hr_source as HRSource) ?? '')
    setFeelNotes(initialContext?.feel_notes ?? '')
    setSpeedPlan(initialContext?.speed_plan ?? [])
    setError('')
  }, [isOpen, initialContext])

  async function handleSave() {
    setSaving(true)
    setError('')
    try {
      const res = await fetch(`/api/training/workouts/${workoutId}/context`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          surface,
          run_type: runType,
          hr_source: hrSource,
          feel_notes: feelNotes,
          speed_plan: speedPlan,
        }),
      })
      if (!res.ok) {
        const data = (await res.json().catch(() => ({}))) as { error?: string }
        throw new Error(data.error ?? t('workoutContextModal.errors.saveFailed'))
      }
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('workoutContextModal.errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={isOpen}
      onClose={onClose}
      maxWidth="max-w-md"
      aria-labelledby={titleId}
    >
      <DialogHeader
        id={titleId}
        title={t('workoutContextModal.title')}
        onClose={onClose}
        closeLabel={t('workoutContextModal.cancel')}
      />

      <DialogBody className="space-y-5">
        <ToggleGroup<Surface>
          legend={t('workoutContextModal.surface.label')}
          name="surface"
          value={surface}
          onChange={handleSurfaceChange}
          options={[
            { value: 'Treadmill', label: t('workoutContextModal.surface.treadmill') },
            { value: 'Outside', label: t('workoutContextModal.surface.outside') },
          ]}
        />

        <ToggleGroup<RunType>
          legend={t('workoutContextModal.runType.label')}
          name="runType"
          value={runType}
          onChange={setRunType}
          options={[
            { value: 'slow', label: t('workoutContextModal.runType.slow') },
            { value: 'interval', label: t('workoutContextModal.runType.interval') },
          ]}
        />

        <ToggleGroup<HRSource>
          legend={t('workoutContextModal.hrSource.label')}
          name="hrSource"
          value={hrSource}
          onChange={setHrSource}
          options={[
            { value: 'chest', label: t('workoutContextModal.hrSource.chest') },
            { value: 'watch', label: t('workoutContextModal.hrSource.watch') },
          ]}
        />

        {surface === 'Treadmill' && (
          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium text-gray-300">
              {t('workoutContextModal.speedPlan.label')}
            </span>
            <div
              data-testid="speed-plan-placeholder"
              className="rounded border border-gray-700 bg-gray-800/40 p-3 text-sm text-gray-400"
            >
              {t('workoutContextModal.speedPlan.placeholder')}
            </div>
          </div>
        )}

        <div className="flex flex-col gap-2">
          <label htmlFor={feelNotesId} className="text-sm font-medium text-gray-300">
            {t('workoutContextModal.feelNotes.label')}
          </label>
          <textarea
            id={feelNotesId}
            value={feelNotes}
            onChange={(e) => setFeelNotes(e.target.value)}
            placeholder={t('workoutContextModal.feelNotes.placeholder')}
            rows={4}
            className="w-full resize-y rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm text-white placeholder-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {error && <p className="text-sm text-red-400">{error}</p>}
      </DialogBody>

      <DialogFooter>
        <button
          type="button"
          onClick={onClose}
          disabled={saving}
          className="min-h-[44px] rounded-lg px-4 py-2 text-sm font-medium text-gray-300 hover:text-white transition-colors disabled:opacity-50"
        >
          {t('workoutContextModal.cancel')}
        </button>
        <button
          type="button"
          onClick={handleSave}
          disabled={saving}
          className="min-h-[44px] rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 transition-colors disabled:opacity-50"
        >
          {saving ? t('workoutContextModal.saving') : t('workoutContextModal.save')}
        </button>
      </DialogFooter>
    </Dialog>
  )
}
