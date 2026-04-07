import { useTranslation } from 'react-i18next'

export interface PipelineBeadInfo {
  beadId: string
  title: string
  status: 'active' | 'done' | 'failed'
  anvil?: string
}

interface PipelineBeadCardProps {
  bead: PipelineBeadInfo
  onBeadClick?: (beadId: string) => void
}

const statusDot: Record<string, string> = {
  active: 'bg-blue-400 animate-pulse',
  done: 'bg-green-400',
  failed: 'bg-red-400',
}

export default function PipelineBeadCard({ bead, onBeadClick }: PipelineBeadCardProps) {
  const { t } = useTranslation('forge')

  return (
    <button
      type="button"
      onClick={() => onBeadClick?.(bead.beadId)}
      aria-label={t('mezzanine.queueSidebar.viewBead', { beadId: bead.beadId })}
      className="w-full text-left rounded border border-gray-700/50 bg-gray-800/80 px-2.5 py-1.5 hover:bg-gray-700/60 hover:border-gray-600/60 transition-colors group"
    >
      <div className="flex items-center gap-1.5 min-w-0">
        <span className={`shrink-0 h-1.5 w-1.5 rounded-full ${statusDot[bead.status] ?? statusDot.active}`} aria-hidden="true" />
        <span className="text-[11px] font-mono text-cyan-400 group-hover:text-cyan-300 shrink-0 transition-colors">
          {bead.beadId}
        </span>
      </div>
      {bead.title && (
        <p className="mt-0.5 text-[11px] text-gray-400 truncate leading-tight">
          {bead.title}
        </p>
      )}
    </button>
  )
}
