import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog, DialogHeader, DialogBody } from './ui/dialog'

interface ShortcutRow {
  keys: string[]
  labelKey: string
}

// Each row lists the key(s) that trigger the action; multiple keys are alternatives.
const SHORTCUTS: ShortcutRow[] = [
  { keys: ['P'], labelKey: 'shortcuts.punch' },
  { keys: ['J', '←'], labelKey: 'shortcuts.prevDay' },
  { keys: ['K', '→'], labelKey: 'shortcuts.nextDay' },
  { keys: ['T'], labelKey: 'shortcuts.today' },
  { keys: ['1'], labelKey: 'shortcuts.viewDay' },
  { keys: ['2'], labelKey: 'shortcuts.viewWeek' },
  { keys: ['3'], labelKey: 'shortcuts.viewMonth' },
  { keys: ['4'], labelKey: 'shortcuts.viewSettings' },
  { keys: ['?'], labelKey: 'shortcuts.help' },
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
