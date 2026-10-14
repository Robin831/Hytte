import { XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

// Rejected status line on a chore card, plus the parent's reason when one was given.
export default function RejectedLabel({ reason }: { reason?: string }) {
  const { t } = useTranslation('allowance')
  const trimmed = reason?.trim()
  return (
    <div className="flex items-center gap-1.5 mt-1 flex-wrap">
      <XCircle size={14} className="text-red-400" />
      <span className="text-red-400 text-sm font-medium">
        {t('myChores.rejected')}
      </span>
      {trimmed && (
        <span data-testid="rejection-reason" className="text-red-300/80 text-sm italic break-words min-w-0">
          {t('reject.reasonDisplay', { reason: trimmed })}
        </span>
      )}
    </div>
  )
}
