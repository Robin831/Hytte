import { useTranslation } from 'react-i18next'
import Widget from './Widget'
import { Skeleton } from './ui/skeleton'

interface WidgetSkeletonProps {
  /** Human-readable widget name, announced while the chunk loads. */
  label?: string
  /** Extra grid classes the widget's cell uses, e.g. `col-span-full`. */
  className?: string
}

/**
 * Placeholder tile for a code-split widget whose chunk has not arrived yet.
 * It reuses the shared `Widget` card shell so the background, radius and
 * padding cannot drift from the real widget, and reserves a widget-sized
 * minimum height so the grid does not reflow when the real widget swaps in.
 */
function WidgetSkeleton({ label, className }: WidgetSkeletonProps) {
  const { t } = useTranslation('dashboard')

  return (
    <Widget className={['min-h-[200px]', className].filter(Boolean).join(' ')}>
      <span role="status" aria-live="polite" className="sr-only">
        {label ? t('widgetLoading.titleWithLabel', { label }) : t('widgetLoading.title')}
      </span>
      <Skeleton className="h-3 w-24 mb-4" />
      <div className="space-y-3">
        <Skeleton className="h-5 w-3/4" />
        <Skeleton className="h-5 w-1/2" />
        <Skeleton className="h-5 w-2/3" />
      </div>
    </Widget>
  )
}

export default WidgetSkeleton
