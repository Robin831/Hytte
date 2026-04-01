import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ListOrdered, ChevronDown, ChevronRight } from 'lucide-react'
import type { AnvilQueue } from '../hooks/useForgeStatus'

interface QueueSummaryCardProps {
  queue: AnvilQueue[]
  onBeadClick?: (beadId: string) => void
}

interface AnvilSectionProps {
  anvilQueue: AnvilQueue
  onBeadClick?: (beadId: string) => void
}

function AnvilSection({ anvilQueue, onBeadClick }: AnvilSectionProps) {
  const { t } = useTranslation('forge')
  const [open, setOpen] = useState(true)

  return (
    <div className="border-b border-gray-700/40 last:border-0">
      <button
        type="button"
        onClick={() => setOpen(prev => !prev)}
        aria-expanded={open}
        aria-label={t('queue.toggleAnvil', { anvil: anvilQueue.anvil })}
        className="w-full flex items-center gap-2 px-5 py-3 text-left hover:bg-gray-700/30 transition-colors min-h-[44px]"
      >
        {open ? (
          <ChevronDown size={14} className="text-gray-500 shrink-0" />
        ) : (
          <ChevronRight size={14} className="text-gray-500 shrink-0" />
        )}
        <span className="text-sm font-medium text-gray-300 truncate">{anvilQueue.anvil}</span>
        <span className="ml-auto flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-cyan-500/20 text-cyan-400 text-xs font-medium shrink-0">
          {anvilQueue.beads.length}
        </span>
      </button>

      {open && (
        <ul className="px-5 pb-3 flex flex-col gap-1">
          {anvilQueue.beads.map(bead => (
            <li key={bead.bead_id} className="flex items-center gap-2 py-1">
              <button
                type="button"
                onClick={() => onBeadClick?.(bead.bead_id)}
                className="text-xs font-mono text-cyan-400 hover:text-cyan-300 hover:underline shrink-0 transition-colors"
              >
                {bead.bead_id}
              </button>
              {bead.title && (
                <span className="text-xs text-gray-400 truncate">{bead.title}</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export default function QueueSummaryCard({ queue, onBeadClick }: QueueSummaryCardProps) {
  const { t } = useTranslation('forge')

  const totalBeads = queue.reduce((sum, aq) => sum + aq.beads.length, 0)

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-gray-700/50">
        <ListOrdered size={18} className="text-cyan-400 shrink-0" />
        <h2 className="text-sm font-medium text-gray-300">{t('queue.title')}</h2>
        {totalBeads > 0 && (
          <span className="ml-auto text-xs text-gray-500">
            {t('queue.totalBeads', { total: totalBeads })}
          </span>
        )}
      </div>

      {queue.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('queue.empty')}</p>
      ) : (
        <div>
          {queue.map(aq => (
            <AnvilSection key={aq.anvil} anvilQueue={aq} onBeadClick={onBeadClick} />
          ))}
        </div>
      )}
    </div>
  )
}
