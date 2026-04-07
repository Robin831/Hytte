import { useTranslation } from 'react-i18next'
import ConfirmDialog from './ConfirmDialog'

interface MergeConfirmDialogProps {
  open: boolean
  prNumber: number
  onConfirm: () => void
  onCancel: () => void
}

export default function MergeConfirmDialog({ open, prNumber, onConfirm, onCancel }: MergeConfirmDialogProps) {
  const { t } = useTranslation('forge')

  return (
    <ConfirmDialog
      open={open}
      title={t('mezzanine.pipeline.mergeDialog.title')}
      message={t('mezzanine.pipeline.mergeDialog.message', { number: prNumber })}
      confirmLabel={t('mezzanine.pipeline.mergeDialog.confirm')}
      onConfirm={onConfirm}
      onCancel={onCancel}
    />
  )
}
