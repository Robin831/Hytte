import { useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'

interface ShortcutHelpModalProps {
  open: boolean
  onClose: () => void
}

interface ShortcutEntry {
  key: string
  labelKey: string
}

const SHORTCUTS: ShortcutEntry[] = [
  { key: 'r', labelKey: 'mezzanine.shortcuts.refresh' },
  { key: 'm', labelKey: 'mezzanine.shortcuts.mergeFirst' },
  { key: 'k', labelKey: 'mezzanine.shortcuts.killFocused' },
  { key: 'q', labelKey: 'mezzanine.shortcuts.focusQueue' },
  { key: 'w', labelKey: 'mezzanine.shortcuts.focusWorkers' },
  { key: 'e', labelKey: 'mezzanine.shortcuts.focusEvents' },
  { key: '1-6', labelKey: 'mezzanine.shortcuts.focusWorkerN' },
  { key: '?', labelKey: 'mezzanine.shortcuts.showHelp' },
  { key: 'Esc', labelKey: 'mezzanine.shortcuts.closeModal' },
]

export default function ShortcutHelpModal({ open, onClose }: ShortcutHelpModalProps) {
  const { t } = useTranslation('forge')

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape' || e.key === '?') {
        e.preventDefault()
        onClose()
      }
    },
    [onClose],
  )

  useEffect(() => {
    if (!open) return
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, handleKeyDown])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={t('mezzanine.shortcuts.title')}
    >
      <div
        className="bg-gray-800 border border-gray-700 rounded-xl shadow-2xl w-full max-w-sm mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700/50">
          <h2 className="text-base font-semibold text-white">
            {t('mezzanine.shortcuts.title')}
          </h2>
          <button
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
                    {t(s.labelKey)}
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
