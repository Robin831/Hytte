import { useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'

interface ShortcutHelpModalProps {
  open: boolean
  onClose: () => void
}

interface ShortcutEntry {
  key: string
  label: string
}

export default function ShortcutHelpModal({ open, onClose }: ShortcutHelpModalProps) {
  const { t } = useTranslation('forge')

  const SHORTCUTS: ShortcutEntry[] = [
    { key: 'r', label: t('mezzanine.shortcuts.refresh') },
    { key: 'm', label: t('mezzanine.shortcuts.mergeFirst') },
    { key: 'k', label: t('mezzanine.shortcuts.killFocused') },
    { key: 'q', label: t('mezzanine.shortcuts.focusQueue') },
    { key: 'w', label: t('mezzanine.shortcuts.focusWorkers') },
    { key: 'e', label: t('mezzanine.shortcuts.focusEvents') },
    { key: '1-6', label: t('mezzanine.shortcuts.focusWorkerN') },
    { key: 'p', label: t('mezzanine.shortcuts.togglePRModal') },
    { key: '?', label: t('mezzanine.shortcuts.showHelp') },
    { key: 'Esc', label: t('mezzanine.shortcuts.closeModal') },
  ]
  const closeButtonRef = useRef<HTMLButtonElement>(null)
  const previousFocusRef = useRef<Element | null>(null)

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape' || e.key === '?') {
        e.preventDefault()
        onClose()
        return
      }
      // Trap focus inside the modal — only one focusable element (close button)
      if (e.key === 'Tab') {
        e.preventDefault()
        closeButtonRef.current?.focus()
      }
    },
    [onClose],
  )

  useEffect(() => {
    if (!open) return
    // Save the previously focused element to restore on close
    previousFocusRef.current = document.activeElement
    // Focus the close button when modal opens
    closeButtonRef.current?.focus()
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      // Restore focus to the previously focused element
      if (previousFocusRef.current instanceof HTMLElement) {
        previousFocusRef.current.focus()
      }
    }
  }, [open, handleKeyDown])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-labelledby="shortcut-help-title"
    >
      <div
        className="bg-gray-800 border border-gray-700 rounded-xl shadow-2xl w-full max-w-sm mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700/50">
          <h2 id="shortcut-help-title" className="text-base font-semibold text-white">
            {t('mezzanine.shortcuts.title')}
          </h2>
          <button
            ref={closeButtonRef}
            type="button"
            onClick={onClose}
            aria-label={t('mezzanine.shortcuts.closeModal')}
            className="text-gray-400 hover:text-white transition-colors"
          >
            <X size={18} />
          </button>
        </div>

        <div className="px-5 py-3">
          <table className="w-full">
            <tbody>
              {SHORTCUTS.map((s) => (
                <tr key={s.key} className="border-b border-gray-700/30 last:border-0">
                  <td className="py-2 pr-4">
                    <kbd className="inline-block min-w-[2rem] text-center px-2 py-0.5 bg-gray-900 border border-gray-600 rounded text-xs font-mono text-gray-200">
                      {s.key}
                    </kbd>
                  </td>
                  <td className="py-2 text-sm text-gray-300">
                    {s.label}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
