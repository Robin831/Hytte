import { CheckCircle, AlertCircle, AlertTriangle } from 'lucide-react'
import type { Toast } from '../hooks/useToast'

interface ToastListProps {
  toasts: Toast[]
  /** Required for toasts with an action — the toast closes once the action fires. */
  onDismiss?: (id: number) => void
}

export default function ToastList({ toasts, onDismiss }: ToastListProps) {
  if (toasts.length === 0) return null

  return (
    <div
      className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 pointer-events-none"
      aria-live="polite"
      aria-atomic="false"
    >
      {toasts.map(toast => {
        const action = toast.action
        return (
          <div
            key={toast.id}
            role="status"
            className={`flex items-center gap-2.5 rounded-lg px-4 py-3 text-sm font-medium shadow-lg border
            ${toast.type === 'success'
              ? 'bg-green-900/90 text-green-200 border-green-700'
              : toast.type === 'warning'
              ? 'bg-amber-900/90 text-amber-200 border-amber-700'
              : 'bg-red-900/90 text-red-200 border-red-700'
            }${action ? ' pointer-events-auto' : ''}`}
          >
            {toast.type === 'success'
              ? <CheckCircle size={16} className="shrink-0 text-green-400" />
              : toast.type === 'warning'
              ? <AlertTriangle size={16} className="shrink-0 text-amber-400" />
              : <AlertCircle size={16} className="shrink-0 text-red-400" />
            }
            {toast.message}
            {action && (
              <button
                type="button"
                onClick={() => {
                  action.onClick()
                  onDismiss?.(toast.id)
                }}
                className="shrink-0 ml-1 rounded px-2 py-1 -my-1 text-xs font-semibold uppercase tracking-wide underline underline-offset-2 cursor-pointer hover:bg-white/10 focus:outline-none focus-visible:ring-2 focus-visible:ring-white/70"
              >
                {action.label}
              </button>
            )}
          </div>
        )
      })}
    </div>
  )
}
