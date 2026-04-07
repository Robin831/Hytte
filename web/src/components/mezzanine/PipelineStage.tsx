import { useTranslation } from 'react-i18next'
import PipelineBeadCard from './PipelineBeadCard'
import type { PipelineBeadInfo } from './PipelineBeadCard'

export type StageKey = 'queue' | 'schematic' | 'smith' | 'temper' | 'warden' | 'pr' | 'merged'

const stageAccent: Record<StageKey, string> = {
  queue: 'border-t-gray-500',
  schematic: 'border-t-purple-500',
  smith: 'border-t-orange-500',
  temper: 'border-t-yellow-500',
  warden: 'border-t-cyan-500',
  pr: 'border-t-blue-500',
  merged: 'border-t-green-500',
}

interface PipelineStageProps {
  stage: StageKey
  beads: PipelineBeadInfo[]
  onBeadClick?: (beadId: string) => void
}

export default function PipelineStage({ stage, beads, onBeadClick }: PipelineStageProps) {
  const { t } = useTranslation('forge')

  return (
    <div
      className={`flex flex-col min-w-[120px] flex-1 rounded bg-gray-900/50 border border-gray-700/40 border-t-2 ${stageAccent[stage]}`}
      role="region"
      aria-label={t(`mezzanine.pipeline.stages.${stage}`)}
    >
      <div className="flex items-center justify-between px-2.5 py-1.5 border-b border-gray-700/30">
        <span className="text-xs font-medium text-gray-300">
          {t(`mezzanine.pipeline.stages.${stage}`)}
        </span>
        {beads.length > 0 && (
          <span className="flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full bg-gray-700/60 text-gray-400 text-[10px] font-medium">
            {beads.length}
          </span>
        )}
      </div>

      <div className="flex flex-col gap-1.5 p-2 min-h-[48px]">
        {beads.map(bead => (
          <PipelineBeadCard key={bead.beadId} bead={bead} onBeadClick={onBeadClick} />
        ))}
      </div>
    </div>
  )
}
