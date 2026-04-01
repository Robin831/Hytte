import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, RotateCcw, ChevronDown } from 'lucide-react'
import type { StuckBead } from '../hooks/useForgeStatus'
import ConfirmDialog from './ConfirmDialog'
import { usePanelCollapse } from '../hooks/usePanelCollapse'

interface NeedsAttentionCardProps {
  stuck: StuckBead[]
  onRetried?: (beadId: string) => void
  showToast: (message: string, type: 'success' | 'error') => void
}

export default function NeedsAttentionCard({ stuck, onRetried, showToast }: NeedsAttentionCardProps) {
  const { t } = useTranslation('forge')
  const [retrying, setRetrying] = useState<Record<string, boolean>>({})
  const [confirmRetry, setConfirmRetry] = useState<StuckBead | null>(null)
  const [isOpen, toggle] = usePanelCollapse('needs-attention')

  async function handleRetry(bead: StuckBead) {
    setConfirmRetry(null)
    setRetrying(prev => ({ ...prev, [bead.bead_id]: true }))
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
      setRetrying(prev => ({ ...prev, [bead.bead_id]: false }))
    }
  }

  return (
    <div id="needs-attention" className="bg-gray-800 rounded-xl border border-amber-600/30 overflow-hidden">
      <button
        type="button"
        onClick={toggle}
        className={`w-full flex items-center gap-2 px-5 py-4 text-left hover:bg-gray-700/30 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-inset ${isOpen ? 'border-b border-gray-700/50' : ''}`}
        aria-expanded={isOpen}
        aria-controls="needs-attention-panel"
      >
        <AlertTriangle size={18} className={stuck.length > 0 ? 'text-amber-400 shrink-0' : 'text-gray-500 shrink-0'} />
        <h2 className="text-sm font-medium text-gray-300">{t('attention.title')}</h2>
        <span className="ml-auto flex items-center gap-2">
          {stuck.length > 0 && (
            <span className="flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-amber-500/20 text-amber-400 text-xs font-medium">
              {stuck.length}
            </span>
          )}
          <ChevronDown
            size={16}
            className={`shrink-0 text-gray-400 transition-transform duration-200 ${isOpen ? 'rotate-180' : ''}`}
            aria-hidden="true"
          />
        </span>
      </button>

      <div id="needs-attention-panel" hidden={!isOpen}>
      {stuck.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('attention.empty')}</p>
      ) : (
        <div className="divide-y divide-gray-700/40">
          {stuck.map(bead => (
            <div key={bead.bead_id} className="px-5 py-4 flex flex-col gap-3 min-h-[44px]">
              <div className="flex items-start justify-between gap-3">
                <div className="flex flex-col gap-0.5 min-w-0">
                  <span className="text-sm font-mono text-amber-400 truncate">{bead.bead_id}</span>
                  <span className="text-xs text-gray-500">
                    {bead.anvil} · {t('attention.retryCount', { count: bead.retry_count })}
                    {bead.clarification_needed && (
                      <span className="ml-2 text-yellow-500">{t('attention.clarificationNeeded')}</span>
                    )}
                  </span>
                </div>

                <button
                  type="button"
                  onClick={() => setConfirmRetry(bead)}
                  disabled={retrying[bead.bead_id]}
                  aria-label={t('attention.retryLabel', { id: bead.bead_id })}
                  className="flex items-center gap-1.5 min-h-[44px] min-w-[44px] px-3 rounded-lg text-sm font-medium transition-colors
                    bg-amber-600/20 text-amber-300 border border-amber-600/30
                    hover:bg-amber-600/30 disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
                >
                  <RotateCcw size={14} className={retrying[bead.bead_id] ? 'animate-spin' : ''} />
                  {t('attention.retry')}
                </button>
              </div>

              {bead.last_error && (
                <p className="text-xs text-red-400 bg-red-900/20 rounded px-3 py-2 break-words">
                  {bead.last_error}
                </p>
              )}
            </div>
          ))}
        </div>
      )}
      </div>

      <ConfirmDialog
        open={confirmRetry !== null}
        title={t('attention.retryConfirmTitle')}
        message={t('attention.retryConfirmMessage', { id: confirmRetry?.bead_id ?? '' })}
        confirmLabel={t('attention.retry')}
        onConfirm={() => { if (confirmRetry) void handleRetry(confirmRetry) }}
        onCancel={() => setConfirmRetry(null)}
      />
    </div>
  )
}
