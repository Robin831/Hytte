import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2 } from 'lucide-react'

export type SpeedPlanSegmentKind = 'warmup' | 'interval' | 'pause' | 'cooldown'

export interface SpeedPlanSegment {
  kind: SpeedPlanSegmentKind | string
  speed_kmph: number
  duration_sec: number
  repeats: number
  same_as_previous: boolean
}

interface SpeedPlanEditorProps {
  value: SpeedPlanSegment[]
  onChange: (segments: SpeedPlanSegment[]) => void
}

const KIND_OPTIONS: SpeedPlanSegmentKind[] = ['warmup', 'interval', 'pause', 'cooldown']

function emptySegment(kind: SpeedPlanSegmentKind): SpeedPlanSegment {
  return { kind, speed_kmph: 0, duration_sec: 0, repeats: 1, same_as_previous: false }
}

function safeNumber(raw: string): number {
  const n = Number(raw)
  return Number.isFinite(n) && n >= 0 ? n : 0
}

export default function SpeedPlanEditor({ value, onChange }: SpeedPlanEditorProps) {
  const { t } = useTranslation('training')
  const sharedIntervalSpeedId = useId()
  const sharedPauseSpeedId = useId()
  const sharedPauseDurationId = useId()
  const addKindId = useId()

  const intervalSegments = useMemo(() => value.filter(s => s.kind === 'interval'), [value])
  const pauseSegments = useMemo(() => value.filter(s => s.kind === 'pause'), [value])

  const [sameSpeedForIntervals, setSameSpeedForIntervals] = useState(true)
  const [addKind, setAddKind] = useState<SpeedPlanSegmentKind>('interval')

  const sharedIntervalSpeed = intervalSegments[0]?.speed_kmph ?? 0
  const sharedPauseSpeed = pauseSegments[0]?.speed_kmph ?? 0
  const sharedPauseDuration = pauseSegments[0]?.duration_sec ?? 0

  function updateSegment(index: number, patch: Partial<SpeedPlanSegment>) {
    const next = value.map((seg, i) => (i === index ? { ...seg, ...patch } : seg))
    onChange(next)
  }

  function updateAllOfKind(kind: SpeedPlanSegmentKind, patch: Partial<SpeedPlanSegment>) {
    const next = value.map(seg => (seg.kind === kind ? { ...seg, ...patch } : seg))
    onChange(next)
  }

  function appendSegment(kind: SpeedPlanSegmentKind) {
    const seed = emptySegment(kind)
    if (kind === 'interval' && sameSpeedForIntervals && intervalSegments.length > 0) {
      seed.speed_kmph = sharedIntervalSpeed
    }
    if (kind === 'pause' && pauseSegments.length > 0) {
      seed.speed_kmph = sharedPauseSpeed
      seed.duration_sec = sharedPauseDuration
    }
    onChange([...value, seed])
  }

  function removeSegment(index: number) {
    onChange(value.filter((_, i) => i !== index))
  }

  function handleSameSpeedToggle(next: boolean) {
    setSameSpeedForIntervals(next)
    if (next && intervalSegments.length > 0) {
      updateAllOfKind('interval', { speed_kmph: sharedIntervalSpeed })
    }
  }

  function kindLabel(kind: string): string {
    if (KIND_OPTIONS.includes(kind as SpeedPlanSegmentKind)) {
      return t(`speedPlan.kinds.${kind}`)
    }
    return kind
  }

  return (
    <div className="flex flex-col gap-3" data-testid="speed-plan-editor">
      <label className="flex items-center gap-2 text-sm text-gray-300">
        <input
          type="checkbox"
          checked={sameSpeedForIntervals}
          onChange={(e) => handleSameSpeedToggle(e.target.checked)}
          data-testid="speed-plan-same-speed-toggle"
          className="h-4 w-4 rounded border-gray-600 bg-gray-800 text-blue-500 focus:ring-blue-500"
        />
        <span>{t('speedPlan.sameSpeedToggle')}</span>
      </label>

      {sameSpeedForIntervals && intervalSegments.length > 0 && (
        <div className="rounded border border-gray-700 bg-gray-800/40 p-3">
          <label htmlFor={sharedIntervalSpeedId} className="block text-xs font-medium text-gray-300">
            {t('speedPlan.sharedIntervalSpeed')}
          </label>
          <div className="mt-1 flex items-center gap-2">
            <input
              id={sharedIntervalSpeedId}
              type="number"
              min={0}
              step="0.1"
              value={sharedIntervalSpeed}
              onChange={(e) =>
                updateAllOfKind('interval', { speed_kmph: safeNumber(e.target.value) })
              }
              data-testid="speed-plan-shared-interval-speed"
              className="w-24 rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white focus:border-blue-500 focus:outline-none"
            />
            <span className="text-xs text-gray-400">{t('speedPlan.kmh')}</span>
          </div>
        </div>
      )}

      {pauseSegments.length > 0 && (
        <div className="rounded border border-gray-700 bg-gray-800/40 p-3">
          <span className="block text-xs font-medium text-gray-300">
            {t('speedPlan.sharedPause')}
          </span>
          <div className="mt-1 flex flex-wrap items-end gap-3">
            <div className="flex flex-col">
              <label htmlFor={sharedPauseSpeedId} className="text-[11px] text-gray-400">
                {t('speedPlan.kmh')}
              </label>
              <input
                id={sharedPauseSpeedId}
                type="number"
                min={0}
                step="0.1"
                value={sharedPauseSpeed}
                onChange={(e) =>
                  updateAllOfKind('pause', { speed_kmph: safeNumber(e.target.value) })
                }
                data-testid="speed-plan-shared-pause-speed"
                className="w-24 rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white focus:border-blue-500 focus:outline-none"
              />
            </div>
            <div className="flex flex-col">
              <label htmlFor={sharedPauseDurationId} className="text-[11px] text-gray-400">
                {t('speedPlan.sec')}
              </label>
              <input
                id={sharedPauseDurationId}
                type="number"
                min={0}
                step={1}
                value={sharedPauseDuration}
                onChange={(e) =>
                  updateAllOfKind('pause', { duration_sec: Math.round(safeNumber(e.target.value)) })
                }
                data-testid="speed-plan-shared-pause-duration"
                className="w-24 rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white focus:border-blue-500 focus:outline-none"
              />
            </div>
          </div>
        </div>
      )}

      <ul className="flex flex-col gap-2">
        {value.map((seg, index) => {
          const isInterval = seg.kind === 'interval'
          const isPause = seg.kind === 'pause'
          const hideSpeed = (isInterval && sameSpeedForIntervals) || isPause
          const hideDuration = isPause
          const speedInputId = `speed-plan-row-${index}-speed`
          const durationInputId = `speed-plan-row-${index}-duration`
          return (
            <li
              key={index}
              data-testid={`speed-plan-row-${index}`}
              className="flex flex-wrap items-end gap-2 rounded border border-gray-800 bg-gray-900/40 p-2"
            >
              <span className="min-w-[5rem] text-xs font-medium uppercase tracking-wide text-gray-400">
                {kindLabel(seg.kind)}
              </span>

              {!hideSpeed && (
                <div className="flex flex-col">
                  <label htmlFor={speedInputId} className="text-[11px] text-gray-400">
                    {t('speedPlan.kmh')}
                  </label>
                  <input
                    id={speedInputId}
                    type="number"
                    min={0}
                    step="0.1"
                    value={seg.speed_kmph}
                    onChange={(e) => updateSegment(index, { speed_kmph: safeNumber(e.target.value) })}
                    data-testid={`speed-plan-row-${index}-speed`}
                    className="w-24 rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white focus:border-blue-500 focus:outline-none"
                  />
                </div>
              )}

              {!hideDuration && (
                <div className="flex flex-col">
                  <label htmlFor={durationInputId} className="text-[11px] text-gray-400">
                    {t('speedPlan.sec')}
                  </label>
                  <input
                    id={durationInputId}
                    type="number"
                    min={0}
                    step={1}
                    value={seg.duration_sec}
                    onChange={(e) =>
                      updateSegment(index, { duration_sec: Math.round(safeNumber(e.target.value)) })
                    }
                    data-testid={`speed-plan-row-${index}-duration`}
                    className="w-24 rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white focus:border-blue-500 focus:outline-none"
                  />
                </div>
              )}

              <button
                type="button"
                onClick={() => removeSegment(index)}
                aria-label={t('speedPlan.removeRow')}
                data-testid={`speed-plan-row-${index}-remove`}
                className="ml-auto inline-flex h-9 items-center justify-center rounded px-2 text-gray-400 hover:bg-gray-800 hover:text-red-400"
              >
                <Trash2 size={16} />
              </button>
            </li>
          )
        })}
      </ul>

      <div className="flex flex-wrap items-center gap-2">
        <label htmlFor={addKindId} className="text-xs text-gray-400">
          {t('speedPlan.addKindLabel')}
        </label>
        <select
          id={addKindId}
          value={addKind}
          onChange={(e) => setAddKind(e.target.value as SpeedPlanSegmentKind)}
          data-testid="speed-plan-add-kind"
          className="rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white focus:border-blue-500 focus:outline-none"
        >
          {KIND_OPTIONS.map((kind) => (
            <option key={kind} value={kind}>
              {t(`speedPlan.kinds.${kind}`)}
            </option>
          ))}
        </select>
        <button
          type="button"
          onClick={() => appendSegment(addKind)}
          data-testid="speed-plan-add"
          className="inline-flex min-h-[36px] items-center gap-1 rounded bg-blue-600 px-3 py-1 text-sm font-medium text-white hover:bg-blue-500"
        >
          <Plus size={16} />
          <span>{t('speedPlan.add')}</span>
        </button>
      </div>
    </div>
  )
}
