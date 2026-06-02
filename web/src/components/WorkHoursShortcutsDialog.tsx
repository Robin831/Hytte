import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog, DialogHeader, DialogBody } from './ui/dialog'

const SHORTCUTS = [
  { keys: ['P'], labelKey: 'shortcuts.punch' as const },
  { keys: ['J', '←'], labelKey: 'shortcuts.prevDay' as const },
  { keys: ['K', '→'], labelKey: 'shortcuts.nextDay' as const },
  { keys: ['T'], labelKey: 'shortcuts.today' as const },
  { keys: ['1'], labelKey: 'shortcuts.viewDay' as const },
  { keys: ['2'], labelKey: 'shortcuts.viewWeek' as const },
  { keys: ['3'], labelKey: 'shortcuts.viewMonth' as const },
  { keys: ['4'], labelKey: 'shortcuts.viewSettings' as const },
  { keys: ['?'], labelKey: 'shortcuts.help' as const },
]

interface WorkHoursShortcutsDialogProps {
  open: boolean
  onClose: () => void
}

function WorkHoursShortcutsDialog({ open, onClose }: WorkHoursShortcutsDialogProps) {
  const { t } = useTranslation('workhours')
  const titleId = useId()

  return (
    <Dialog open={open} onClose={onClose} maxWidth="max-w-md" aria-labelledby={titleId}>
      <DialogHeader
        id={titleId}
        title={t('shortcuts.title')}
        onClose={onClose}
        closeLabel={t('shortcuts.close')}
      />
      <DialogBody>
        <ul className="space-y-2.5">
          {SHORTCUTS.map(s => (
            <li key={s.labelKey} className="flex items-center justify-between gap-4">
              <span className="text-sm text-gray-300">{t(s.labelKey)}</span>
              <span className="flex items-center gap-1">
                {s.keys.map((k, i) => (
                  <kbd
                    key={i}
                    className="min-w-[1.75rem] text-center rounded border border-gray-600 bg-gray-800 px-1.5 py-0.5 text-xs font-mono text-gray-200"
                  >
                    {k}
                  </kbd>
                ))}
              </span>
            </li>
          ))}
        </ul>
      </DialogBody>
    </Dialog>
  )
}

export { WorkHoursShortcutsDialog }
