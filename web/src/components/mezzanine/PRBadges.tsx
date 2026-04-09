import { CheckCircle, XCircle, Clock } from 'lucide-react'
import type { TFunction } from 'i18next'

export interface PRStatusFields {
  ci_passing: boolean
  ci_pending: boolean
  has_approval: boolean
  changes_requested: boolean
  is_conflicting: boolean
  has_unresolved_threads: boolean
}

export function CIBadge({ pr, t }: { pr: PRStatusFields; t: TFunction<'forge'> }) {
  if (pr.ci_passing) {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-green-500/15 text-green-400 border border-green-500/25">
        <CheckCircle size={12} />
        <span className="hidden sm:inline">{t('prModal.ciPass')}</span>
      </span>
    )
  }
  if (pr.ci_pending) {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-yellow-500/15 text-yellow-400 border border-yellow-500/25">
        <Clock size={12} />
        <span className="hidden sm:inline">{t('prModal.ciPending')}</span>
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-red-500/15 text-red-400 border border-red-500/25">
      <XCircle size={12} />
      <span className="hidden sm:inline">{t('prModal.ciFail')}</span>
    </span>
  )
}

export function ReviewBadge({ pr, t }: { pr: PRStatusFields; t: TFunction<'forge'> }) {
  if (pr.has_approval) {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-green-500/15 text-green-400 border border-green-500/25">
        <CheckCircle size={12} />
        <span className="hidden sm:inline">{t('prModal.reviewApproved')}</span>
      </span>
    )
  }
  if (pr.changes_requested) {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-red-500/15 text-red-400 border border-red-500/25">
        <XCircle size={12} />
        <span className="hidden sm:inline">{t('prModal.reviewChanges')}</span>
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-gray-500/15 text-gray-400 border border-gray-500/25">
      <Clock size={12} />
      <span className="hidden sm:inline">{t('prModal.reviewPending')}</span>
    </span>
  )
}
