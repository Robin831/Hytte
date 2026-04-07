import { useTranslation } from 'react-i18next'
import { sectionColors } from '../forgeQueueUi'

interface QueueItemProps {
  beadId: string
  title: string
  priority: number
  status: string
  section: string
  onBeadClick?: (beadId: string) => void
}

export default function QueueItem({ beadId, title, priority, status, section, onBeadClick }: QueueItemProps) {
  const { t } = useTranslation('forge')
  const cls = sectionColors[section] ?? sectionColors['unlabeled']

  return (
    <li className="px-3 py-2.5 border-b border-gray-700/30 last:border-0 hover:bg-gray-700/20 transition-colors">
      {/* Row 1: bead ID + priority */}
      <div className="flex items-center gap-2 min-w-0">
        <button
          type="button"
          onClick={() => onBeadClick?.(beadId)}
          aria-label={t('mezzanine.queueSidebar.viewBead', { beadId })}
          className="text-xs font-mono text-cyan-400 hover:text-cyan-300 hover:underline shrink-0 transition-colors"
        >
          {beadId}
        </button>
        {priority > 0 && (
          <span className="text-xs text-gray-500 shrink-0" aria-label={t('mezzanine.queueSidebar.priority', { priority })}>P{priority}</span>
        )}
        <span className={`ml-auto inline-flex items-center px-1.5 py-0.5 rounded text-[10px] border font-medium shrink-0 ${cls}`}>
          {t(`fullQueue.section.${section}`, { defaultValue: section })}
        </span>
      </div>

      {/* Row 2: title + status */}
      {(title || status) && (
        <div className="flex items-center gap-2 mt-1 min-w-0">
          {title && (
            <span className="text-xs text-gray-300 truncate">{title}</span>
          )}
          {status && (
            <span className="ml-auto text-[10px] text-gray-500 shrink-0 italic">{status}</span>
          )}
        </div>
      )}
    </li>
  )
}
