import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from './ui/dialog'

const CHIP_KEYS = ['reject.chips.notFinished', 'reject.chips.redoProperly', 'reject.chips.wrongDay'] as const

interface RejectReasonDialogProps {
  open: boolean
  // Number of completions being rejected; above 1 the title switches to the batch wording.
  count?: number
  submitting?: boolean
  onCancel: () => void
  onConfirm: (reason: string) => void
}

export default function RejectReasonDialog({ open, ...rest }: RejectReasonDialogProps) {
  // Mounting the form only while open resets the reason on every open.
  if (!open) return null
  return <RejectReasonForm {...rest} />
}

function RejectReasonForm({ count = 1, submitting = false, onCancel, onConfirm }: Omit<RejectReasonDialogProps, 'open'>) {
  const { t } = useTranslation('allowance')
  const titleId = useId()
  const textareaId = useId()
  const [reason, setReason] = useState('')

  const handleCancel = () => {
    if (!submitting) onCancel()
  }

  return (
    <Dialog open onClose={handleCancel} maxWidth="max-w-sm" aria-labelledby={titleId}>
      <DialogHeader
        id={titleId}
        title={count > 1 ? t('reject.titleBatch', { count }) : t('reject.title')}
        onClose={handleCancel}
      />
      <DialogBody className="space-y-3">
        <div className="flex flex-wrap gap-2">
          {CHIP_KEYS.map(key => {
            const label = t(key)
            const selected = reason === label
            return (
              <button
                key={key}
                type="button"
                onClick={() => setReason(label)}
                aria-pressed={selected}
                className={`px-3 py-1.5 rounded-full text-sm border transition-colors cursor-pointer ${
                  selected
                    ? 'bg-red-600/30 border-red-500 text-red-200'
                    : 'bg-gray-800 border-gray-600 text-gray-300 hover:bg-gray-700'
                }`}
              >
                {label}
              </button>
            )
          })}
        </div>
        <label htmlFor={textareaId} className="block text-sm text-gray-400">
          {t('reject.reasonOptional')}
        </label>
        <textarea
          id={textareaId}
          value={reason}
          onChange={e => setReason(e.target.value)}
          placeholder={t('reject.placeholder')}
          rows={3}
          maxLength={500}
          className="w-full rounded-lg bg-gray-800 border border-gray-600 px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-red-500 resize-none"
        />
      </DialogBody>
      <DialogFooter>
        <button
          type="button"
          onClick={handleCancel}
          disabled={submitting}
          className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors disabled:opacity-50 cursor-pointer"
        >
          {t('reject.cancel')}
        </button>
        <button
          type="button"
          onClick={() => onConfirm(reason.trim())}
          disabled={submitting}
          className="px-4 py-2 text-sm font-medium rounded bg-red-700 hover:bg-red-600 text-white transition-colors disabled:opacity-50 cursor-pointer"
        >
          {t('reject.confirm')}
        </button>
      </DialogFooter>
    </Dialog>
  )
}
