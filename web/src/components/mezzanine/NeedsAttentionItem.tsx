import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { RotateCcw, MessageSquare, XCircle } from 'lucide-react'
import type { StuckBead } from '../../hooks/useForgeStatus'
import ConfirmDialog from '../ConfirmDialog'

interface NeedsAttentionItemProps {
  bead: StuckBead
  showToast: (message: string, type: 'success' | 'error') => void
  onBeadClick?: (beadId: string) => void
  onRetried?: (beadId: string) => void
}

export default function NeedsAttentionItem({
  bead,
  showToast,
  onBeadClick,
  onRetried,
}: NeedsAttentionItemProps) {
  const { t } = useTranslation('forge')
  const [acting, setActing] = useState(false)
  const [showDismissConfirm, setShowDismissConfirm] = useState(false)

  async function handleRetry() {
    setActing(true)
    try {
      const res = await fetch(`/api/forge/beads/${encodeURIComponent(bead.bead_id)}/retry`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t('attention.retrySuccess', { id: bead.bead_id }), 'success')
        onRetried?.(bead.bead_id)
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setActing(false)
    }
  }

  async function handleDismiss() {
    setShowDismissConfirm(false)
    setActing(true)
    try {
      const res = await fetch(`/api/forge/beads/${encodeURIComponent(bead.bead_id)}/dismiss`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t('attention.dismissSuccess', { id: bead.bead_id }), 'success')
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setActing(false)
    }
  }

  function handleClarify() {
    onBeadClick?.(bead.bead_id)
  }

  return (
    <>
      <div className="px-4 py-3 flex flex-col gap-2">
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
              onClick={() => void handleRetry()}
              disabled={acting}
              aria-label={t('attention.retryLabel', { id: bead.bead_id })}
              title={t('attention.retry')}
              className="flex items-center justify-center min-h-[36px] min-w-[36px] rounded-lg text-sm transition-colors
                bg-amber-600/20 text-amber-300 border border-amber-600/30
                hover:bg-amber-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <RotateCcw size={14} className={acting ? 'animate-spin' : ''} />
            </button>

            <button
              type="button"
              onClick={handleClarify}
              disabled={acting}
              aria-label={t('mezzanine.needsAttention.clarifyLabel', { id: bead.bead_id })}
              title={t('mezzanine.needsAttention.clarify')}
              className="flex items-center justify-center min-h-[36px] min-w-[36px] rounded-lg text-sm transition-colors
                bg-blue-600/20 text-blue-300 border border-blue-600/30
                hover:bg-blue-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <MessageSquare size={14} />
            </button>

            <button
              type="button"
              onClick={() => setShowDismissConfirm(true)}
              disabled={acting}
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
        open={showDismissConfirm}
        title={t('attention.dismissConfirmTitle')}
        message={t('attention.dismissConfirmMessage', { id: bead.bead_id })}
        confirmLabel={t('attention.dismiss')}
        destructive
        onConfirm={() => void handleDismiss()}
        onCancel={() => setShowDismissConfirm(false)}
      />
    </>
  )
}
