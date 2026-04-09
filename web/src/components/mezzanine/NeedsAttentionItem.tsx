import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { RotateCcw, ExternalLink, XCircle, CheckCircle, Hammer, ShieldCheck } from 'lucide-react'
import type { StuckBead } from '../../hooks/useForgeStatus'
import ConfirmDialog from '../ConfirmDialog'
import { useBeadActions } from '../../hooks/useBeadActions'

interface NeedsAttentionItemProps {
  bead: StuckBead
  showToast: (message: string, type: 'success' | 'error') => void
  onBeadClick?: (beadId: string) => void
  onRetried?: (beadId: string) => void
  highlighted?: boolean
}

export default function NeedsAttentionItem({
  bead,
  showToast,
  onBeadClick,
  onRetried,
  highlighted,
}: NeedsAttentionItemProps) {
  const { t } = useTranslation('forge')
  const [confirmAction, setConfirmAction] = useState<'dismiss' | 'approve' | 'forceSmith' | 'wardenRerun' | null>(null)
  const { acting, handleAction } = useBeadActions({ showToast, onRetried })

  const isActing = !!acting[bead.bead_id]

  return (
    <>
      <div className={`px-4 py-3 flex flex-col gap-2 ${highlighted ? 'bg-amber-900/20 ring-1 ring-inset ring-amber-500/40' : ''}`}>
        <div className="flex items-start justify-between gap-2">
          <div className="flex flex-col gap-0.5 min-w-0">
            <button
              type="button"
              onClick={() => onBeadClick?.(bead.bead_id)}
              className="text-sm font-mono text-amber-400 hover:text-amber-300 hover:underline truncate transition-colors text-left"
            >
              {bead.bead_id}
            </button>
            <span className="text-xs text-gray-500">
              {bead.anvil} · {t('attention.retryCount', { count: bead.retry_count })}
              {bead.clarification_needed && (
                <span className="ml-2 text-yellow-500">{t('attention.clarificationNeeded')}</span>
              )}
              {bead.needs_human && !bead.clarification_needed && (
                <span className="ml-2 text-orange-400">{t('needsHuman')}</span>
              )}
            </span>
          </div>

          <div className="flex items-center gap-1 shrink-0">
            <button
              type="button"
              onClick={() => void handleAction('retry', bead.bead_id)}
              disabled={isActing}
              aria-label={t('attention.retryLabel', { id: bead.bead_id })}
              title={t('attention.retry')}
              className="flex items-center justify-center min-h-[36px] min-w-[36px] rounded-lg text-sm transition-colors
                bg-amber-600/20 text-amber-300 border border-amber-600/30
                hover:bg-amber-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <RotateCcw size={14} className={isActing ? 'animate-spin' : ''} />
            </button>

            <button
              type="button"
              onClick={() => setConfirmAction('approve')}
              disabled={isActing}
              aria-label={t('attention.approve')}
              title={t('attention.approve')}
              className="flex items-center justify-center min-h-[36px] min-w-[36px] rounded-lg text-sm transition-colors
                bg-green-600/20 text-green-300 border border-green-600/30
                hover:bg-green-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <CheckCircle size={14} />
            </button>

            <button
              type="button"
              onClick={() => setConfirmAction('forceSmith')}
              disabled={isActing}
              aria-label={t('attention.forceSmith')}
              title={t('attention.forceSmith')}
              className="flex items-center justify-center min-h-[36px] min-w-[36px] rounded-lg text-sm transition-colors
                bg-purple-600/20 text-purple-300 border border-purple-600/30
                hover:bg-purple-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Hammer size={14} />
            </button>

            <button
              type="button"
              onClick={() => setConfirmAction('wardenRerun')}
              disabled={isActing}
              aria-label={t('attention.wardenRerun')}
              title={t('attention.wardenRerun')}
              className="flex items-center justify-center min-h-[36px] min-w-[36px] rounded-lg text-sm transition-colors
                bg-cyan-600/20 text-cyan-300 border border-cyan-600/30
                hover:bg-cyan-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <ShieldCheck size={14} />
            </button>

            <button
              type="button"
              onClick={() => onBeadClick?.(bead.bead_id)}
              disabled={isActing}
              aria-label={t('mezzanine.needsAttention.viewLabel', { id: bead.bead_id })}
              title={t('mezzanine.needsAttention.view')}
              className="flex items-center justify-center min-h-[36px] min-w-[36px] rounded-lg text-sm transition-colors
                bg-blue-600/20 text-blue-300 border border-blue-600/30
                hover:bg-blue-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <ExternalLink size={14} />
            </button>

            <button
              type="button"
              onClick={() => setConfirmAction('dismiss')}
              disabled={isActing}
              aria-label={t('mezzanine.needsAttention.dismissLabel', { id: bead.bead_id })}
              title={t('attention.dismiss')}
              className="flex items-center justify-center min-h-[36px] min-w-[36px] rounded-lg text-sm transition-colors
                bg-red-600/20 text-red-300 border border-red-600/30
                hover:bg-red-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <XCircle size={14} />
            </button>
          </div>
        </div>

        {bead.last_error && (
          <p className="text-xs text-red-400 bg-red-900/20 rounded px-2.5 py-1.5 break-words line-clamp-2">
            {bead.last_error}
          </p>
        )}
      </div>

      <ConfirmDialog
        open={confirmAction !== null}
        title={
          confirmAction === 'dismiss' ? t('attention.dismissConfirmTitle')
            : confirmAction === 'approve' ? t('attention.approveConfirmTitle')
            : confirmAction === 'forceSmith' ? t('attention.forceSmithConfirmTitle')
            : confirmAction === 'wardenRerun' ? t('attention.wardenRerunConfirmTitle')
            : ''
        }
        message={
          confirmAction === 'dismiss' ? t('attention.dismissConfirmMessage', { id: bead.bead_id })
            : confirmAction === 'approve' ? t('attention.approveConfirmMessage', { id: bead.bead_id })
            : confirmAction === 'forceSmith' ? t('attention.forceSmithConfirmMessage', { id: bead.bead_id })
            : confirmAction === 'wardenRerun' ? t('attention.wardenRerunConfirmMessage', { id: bead.bead_id })
            : ''
        }
        confirmLabel={
          confirmAction === 'dismiss' ? t('attention.dismiss')
            : confirmAction === 'approve' ? t('attention.approve')
            : confirmAction === 'forceSmith' ? t('attention.forceSmith')
            : confirmAction === 'wardenRerun' ? t('attention.wardenRerun')
            : ''
        }
        destructive={confirmAction === 'dismiss'}
        onConfirm={() => { const action = confirmAction; setConfirmAction(null); if (action) void handleAction(action, bead.bead_id) }}
        onCancel={() => setConfirmAction(null)}
      />
    </>
  )
}
