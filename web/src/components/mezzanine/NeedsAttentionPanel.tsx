import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import type { StuckBead } from '../../hooks/useForgeStatus'
import NeedsAttentionItem from './NeedsAttentionItem'

interface NeedsAttentionPanelProps {
  stuck: StuckBead[]
  showToast: (message: string, type: 'success' | 'error') => void
  onBeadClick?: (beadId: string) => void
  onRetried?: (beadId: string) => void
}

export default function NeedsAttentionPanel({
  stuck,
  showToast,
  onBeadClick,
  onRetried,
}: NeedsAttentionPanelProps) {
  const { t } = useTranslation('forge')

  const attentionBeads = useMemo(() => {
    return stuck.filter(
      b => b.needs_human || b.clarification_needed || b.dispatch_failures > 0 || b.retry_count > 0
    )
  }, [stuck])

  if (attentionBeads.length === 0) return null

  return (
    <section aria-label={t('mezzanine.needsAttention.title')}>
      <div className="flex items-center gap-2 mb-2">
        <AlertTriangle size={16} className="text-amber-400 shrink-0" />
        <h2 className="text-sm font-medium text-gray-300">
          {t('mezzanine.needsAttention.title')}
        </h2>
        <span className="flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-amber-500/20 text-amber-400 text-xs font-medium">
          {attentionBeads.length}
        </span>
      </div>

      <div className="bg-gray-800 rounded-xl border border-amber-600/30 overflow-hidden divide-y divide-gray-700/40">
        {attentionBeads.map(bead => (
          <NeedsAttentionItem
            key={bead.bead_id}
            bead={bead}
            showToast={showToast}
            onBeadClick={onBeadClick}
            onRetried={onRetried}
          />
        ))}
      </div>
    </section>
  )
}
