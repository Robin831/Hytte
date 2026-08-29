import { useTranslation } from 'react-i18next'
import { AlertTriangle, RefreshCw } from 'lucide-react'

interface StrideStaleBannerProps {
  // The block's stale_reason, e.g. 'races_changed'. Rendered through a keyed
  // message so a reason added later falls back to a generic line rather than
  // showing the raw enum.
  reason: string
  onRegenerate: () => void
  busy?: boolean
}

// Shown above the long-term plan when the block no longer matches the athlete's
// race calendar. A stale block is never regenerated automatically — this banner
// and its button are the only way it gets replaced.
export function StrideStaleBanner({ reason, onRegenerate, busy = false }: StrideStaleBannerProps) {
  const { t } = useTranslation('stride')

  return (
    <div
      role="status"
      className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 mb-3"
    >
      <AlertTriangle size={16} className="text-amber-400 shrink-0 mt-0.5" />
      <div className="flex-1 min-w-0">
        <p className="text-sm text-amber-200">
          {t(`longTermPlan.stale.${reason}`, { defaultValue: t('longTermPlan.stale.generic') })}
        </p>
        <p className="text-xs text-amber-200/70 mt-0.5">{t('longTermPlan.stale.hint')}</p>
      </div>
      <button
        type="button"
        onClick={onRegenerate}
        disabled={busy}
        className="flex items-center gap-1.5 shrink-0 px-2.5 py-1 text-xs font-medium rounded-lg bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-white transition-colors"
      >
        <RefreshCw size={12} className={busy ? 'animate-spin' : ''} />
        {t('longTermPlan.actions.regenerate')}
      </button>
    </div>
  )
}
