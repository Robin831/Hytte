import { useTranslation } from 'react-i18next'
import { RefreshCw } from 'lucide-react'

interface InlineRetryProps {
  message: string
  onRetry: () => void
}

/**
 * Card-sized inline error with a retry button. Used where a sub-fetch of the
 * Salary page fails so the affected card explains itself instead of silently
 * rendering nothing.
 */
export default function InlineRetry({ message, onRetry }: InlineRetryProps) {
  const { t } = useTranslation('salary')

  return (
    <div
      role="alert"
      className="bg-gray-800 rounded-xl p-5 flex flex-wrap items-center justify-between gap-3"
    >
      <p className="text-sm text-red-400">{message}</p>
      <button
        type="button"
        onClick={onRetry}
        className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-200 text-sm rounded-lg transition-colors"
      >
        <RefreshCw size={14} />
        {t('actions.retry')}
      </button>
    </div>
  )
}
