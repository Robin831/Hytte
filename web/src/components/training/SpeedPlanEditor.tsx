import { useTranslation } from 'react-i18next'

export interface SpeedPlanSegment {
  kind: string
  speed_kmph: number
  duration_sec: number
  repeats?: number
  same_as_previous?: boolean
}

interface SpeedPlanEditorProps {
  value: SpeedPlanSegment[]
  onChange: (segments: SpeedPlanSegment[]) => void
}

export default function SpeedPlanEditor({ value }: SpeedPlanEditorProps) {
  const { t } = useTranslation('training')
  return (
    <div
      data-testid="speed-plan-editor"
      className="rounded border border-gray-700 bg-gray-800/40 p-3 text-sm text-gray-400"
    >
      {t('workoutContextModal.speedPlan.placeholder', { count: value.length })}
    </div>
  )
}
