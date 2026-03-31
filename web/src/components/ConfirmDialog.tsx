import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

interface ConfirmDialogProps {
  open: boolean
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  destructive?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export default function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel,
  cancelLabel,
  destructive = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const { t } = useTranslation('common')
  const confirmRef = useRef<HTMLButtonElement>(null)
  const prevFocusRef = useRef<Element | null>(null)

  useEffect(() => {
    if (open) {
      prevFocusRef.current = document.activeElement
      confirmRef.current?.focus()
    } else if (prevFocusRef.current instanceof HTMLElement) {
      prevFocusRef.current.focus()
      prevFocusRef.current = null
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, onCancel])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-dialog-title"
    >
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/60"
        onClick={onCancel}
        aria-hidden="true"
      />

      {/* Dialog panel */}
      <div className="relative z-10 w-full max-w-sm rounded-xl bg-gray-800 border border-gray-700 shadow-2xl p-6 flex flex-col gap-4">
        <h2 id="confirm-dialog-title" className="text-base font-semibold text-white">
          {title}
        </h2>
        <p className="text-sm text-gray-400">{message}</p>

        <div className="flex gap-3 justify-end mt-1">
          <button
            type="button"
            onClick={onCancel}
            className="min-h-[44px] px-4 rounded-lg text-sm font-medium text-gray-300
              bg-gray-700 hover:bg-gray-600 transition-colors"
          >
            {cancelLabel ?? t('cancel')}
          </button>
          <button
            ref={confirmRef}
            type="button"
            onClick={onConfirm}
            className={`min-h-[44px] px-4 rounded-lg text-sm font-medium transition-colors
              ${destructive
                ? 'bg-red-600 hover:bg-red-500 text-white'
                : 'bg-amber-600 hover:bg-amber-500 text-white'
              }`}
          >
            {confirmLabel ?? t('confirm')}
          </button>
        </div>
      </div>
    </div>
  )
}
