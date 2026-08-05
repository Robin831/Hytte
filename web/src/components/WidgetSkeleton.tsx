import { useTranslation } from 'react-i18next'
import { Skeleton } from './ui/skeleton'

interface WidgetSkeletonProps {
  /** Human-readable widget name, announced while the chunk loads. */
  label?: string
  /** Extra grid classes the widget's cell uses, e.g. `col-span-full`. */
  className?: string
}

/**
 * Placeholder tile for a code-split widget whose chunk has not arrived yet.
 * It mirrors the `Widget` card shell (background, radius, padding) and reserves
 * a widget-sized minimum height so the grid does not reflow when the real
 * widget swaps in.
 */
function WidgetSkeleton({ label, className = '' }: WidgetSkeletonProps) {
  const { t } = useTranslation('dashboard')

  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      className={`bg-gray-800 rounded-xl p-6 min-h-[200px] ${className}`}
    >
      <span className="sr-only">
        {label ? t('widgetLoading.titleWithLabel', { label }) : t('widgetLoading.title')}
      </span>
      <Skeleton className="h-3 w-24 mb-4" />
      <div className="space-y-3">
        <Skeleton className="h-5 w-3/4" />
        <Skeleton className="h-5 w-1/2" />
        <Skeleton className="h-5 w-2/3" />
      </div>
    </div>
  )
}

export default WidgetSkeleton
