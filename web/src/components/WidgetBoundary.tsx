import { Component, Fragment, type ErrorInfo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, RotateCw } from 'lucide-react'

interface WidgetBoundaryFallbackProps {
  label?: string
  className?: string
  onRetry: () => void
}

// Class components can't use hooks, so the localized fallback lives in its own
// function component.
function WidgetBoundaryFallback({ label, className = '', onRetry }: WidgetBoundaryFallbackProps) {
  const { t } = useTranslation('dashboard')

  return (
    <div role="alert" className={`bg-gray-800 rounded-xl p-6 ${className}`}>
      <div className="flex items-start gap-3">
        <AlertTriangle size={20} className="text-amber-400 shrink-0 mt-0.5" />
        <div className="min-w-0">
          <h2 className="text-sm font-medium text-gray-200">
            {label ? t('widgetError.titleWithLabel', { label }) : t('widgetError.title')}
          </h2>
          <p className="text-xs text-gray-400 mt-1">{t('widgetError.message')}</p>
          <button
            type="button"
            onClick={onRetry}
            className="mt-3 inline-flex items-center gap-1.5 text-xs text-blue-400 hover:text-blue-300"
          >
            <RotateCw size={14} />
            {t('widgetError.retry')}
          </button>
        </div>
      </div>
    </div>
  )
}

interface WidgetBoundaryProps {
  children: ReactNode
  /** Human-readable widget name shown in the fallback tile. */
  label?: string
  /** Extra classes for the fallback tile, e.g. grid spans the widget itself uses. */
  className?: string
}

interface WidgetBoundaryState {
  hasError: boolean
  resetKey: number
}

/**
 * Isolates a single dashboard widget: a render-time throw degrades to a compact
 * failure tile instead of unmounting the whole dashboard. Retry bumps a key so
 * the child subtree remounts without a page reload.
 */
class WidgetBoundary extends Component<WidgetBoundaryProps, WidgetBoundaryState> {
  state: WidgetBoundaryState = { hasError: false, resetKey: 0 }

  static getDerivedStateFromError(): Partial<WidgetBoundaryState> {
    return { hasError: true }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    // Keep failures debuggable in the browser console — the fallback tile
    // deliberately shows no technical detail.
    console.error(
      '[WidgetBoundary] widget failed to render:',
      this.props.label ?? '(unlabeled)',
      error,
      errorInfo.componentStack,
    )
  }

  handleRetry = () => {
    this.setState((prev) => ({ hasError: false, resetKey: prev.resetKey + 1 }))
  }

  render() {
    if (this.state.hasError) {
      return (
        <WidgetBoundaryFallback
          label={this.props.label}
          className={this.props.className}
          onRetry={this.handleRetry}
        />
      )
    }

    // A keyed Fragment remounts the subtree on retry without adding a DOM node,
    // so the widget stays a direct child of the dashboard grid.
    return <Fragment key={this.state.resetKey}>{this.props.children}</Fragment>
  }
}

export default WidgetBoundary
