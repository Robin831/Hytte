import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, RotateCcw } from 'lucide-react'
import type { StuckBead } from '../hooks/useForgeStatus'

interface NeedsAttentionCardProps {
  stuck: StuckBead[]
  onRetried?: (beadId: string) => void
}

export default function NeedsAttentionCard({ stuck, onRetried }: NeedsAttentionCardProps) {
  const { t } = useTranslation('forge')
  const [retrying, setRetrying] = useState<Record<string, boolean>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})

  async function handleRetry(beadId: string) {
    setRetrying(prev => ({ ...prev, [beadId]: true }))
    setErrors(prev => { const next = { ...prev }; delete next[beadId]; return next })
    try {
      const res = await fetch(`/api/forge/beads/${encodeURIComponent(beadId)}/retry`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setErrors(prev => ({ ...prev, [beadId]: (data as { error?: string }).error ?? `HTTP ${res.status}` }))
      } else {
        onRetried?.(beadId)
      }
    } catch (err) {
      setErrors(prev => ({ ...prev, [beadId]: err instanceof Error ? err.message : t('unknownError') }))
    } finally {
      setRetrying(prev => ({ ...prev, [beadId]: false }))
    }
  }

  return (
    <div className="bg-gray-800 rounded-xl border border-amber-600/30 overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-gray-700/50">
        <AlertTriangle size={18} className={stuck.length > 0 ? 'text-amber-400 shrink-0' : 'text-gray-500 shrink-0'} />
        <h2 className="text-sm font-medium text-gray-300">{t('attention.title')}</h2>
        {stuck.length > 0 && (
          <span className="ml-auto flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-amber-500/20 text-amber-400 text-xs font-medium">
            {stuck.length}
          </span>
        )}
      </div>

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
                  onClick={() => void handleRetry(bead.bead_id)}
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

              {errors[bead.bead_id] && (
                <p className="text-xs text-red-400">{errors[bead.bead_id]}</p>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
