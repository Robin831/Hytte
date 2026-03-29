import React, { useEffect, useId } from 'react'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/utils'

interface DialogProps {
  open: boolean
  onClose: () => void
  children: React.ReactNode
  className?: string
  maxWidth?: string
  'aria-labelledby'?: string
}

function Dialog({
  open,
  onClose,
  children,
  className,
  maxWidth = 'max-w-md',
  'aria-labelledby': ariaLabelledby,
}: DialogProps) {
  useEffect(() => {
    if (!open) return
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, onClose])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={ariaLabelledby}
        className={cn(
          'w-full rounded-lg bg-gray-900 border border-gray-700 shadow-xl flex flex-col max-h-[90vh]',
          maxWidth,
          className
        )}
      >
        {children}
      </div>
    </div>
  )
}

interface DialogHeaderProps {
  id?: string
  title: string
  onClose: () => void
  closeLabel?: string
}

function DialogHeader({ id, title, onClose, closeLabel }: DialogHeaderProps) {
  return (
    <div className="flex items-center justify-between px-6 py-4 border-b border-gray-700 shrink-0">
      <h2 id={id} className="text-lg font-semibold text-white">{title}</h2>
      <button
        type="button"
        onClick={onClose}
        aria-label={closeLabel ?? 'Close'}
        className="text-gray-400 hover:text-white transition-colors"
      >
        <X size={20} />
      </button>
    </div>
  )
}

function DialogBody({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn('flex-1 overflow-y-auto px-6 py-4', className)}>
      {children}
    </div>
  )
}

function DialogFooter({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn('flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-700 shrink-0', className)}>
      {children}
    </div>
  )
}

interface ConfirmDialogProps {
  open: boolean
  onClose: () => void
  onConfirm: () => void
  title: string
  message?: string
  confirmLabel?: string
  cancelLabel?: string
  variant?: 'destructive' | 'default'
}

function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title,
  message,
  confirmLabel,
  cancelLabel,
  variant = 'destructive',
}: ConfirmDialogProps) {
  const { t } = useTranslation('common')
  const titleId = useId()

  const effectiveConfirmLabel = confirmLabel ?? (variant === 'destructive' ? t('confirm.delete') : t('confirm.confirm'))
  const effectiveCancelLabel = cancelLabel ?? t('confirm.cancel')

  return (
    <Dialog open={open} onClose={onClose} maxWidth="max-w-sm" aria-labelledby={titleId}>
      <DialogHeader id={titleId} title={title} onClose={onClose} />
      {message && (
        <DialogBody>
          <p className="text-sm text-gray-300">{message}</p>
        </DialogBody>
      )}
      <DialogFooter>
        <button
          type="button"
          onClick={onClose}
          className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors"
        >
          {effectiveCancelLabel}
        </button>
        <button
          type="button"
          onClick={() => { onConfirm(); onClose() }}
          className={cn(
            'px-4 py-2 text-sm font-medium rounded transition-colors',
            variant === 'destructive'
              ? 'bg-red-700 hover:bg-red-600 text-white'
              : 'bg-blue-600 hover:bg-blue-500 text-white'
          )}
        >
          {effectiveConfirmLabel}
        </button>
      </DialogFooter>
    </Dialog>
  )
}

export { Dialog, DialogHeader, DialogBody, DialogFooter, ConfirmDialog }
