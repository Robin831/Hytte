import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { GitPullRequest } from 'lucide-react'
import { Dialog, DialogHeader, DialogBody } from '../ui/dialog'

interface PRModalProps {
  open: boolean
  onClose: () => void
}

export default function PRModal({ open, onClose }: PRModalProps) {
  const { t } = useTranslation('forge')
  const titleId = useId()

  return (
    <Dialog open={open} onClose={onClose} maxWidth="max-w-2xl" aria-labelledby={titleId}>
      <DialogHeader id={titleId} title={t('prModal.title')} onClose={onClose} />
      <DialogBody>
        <div className="flex flex-col items-center justify-center py-12 text-gray-400">
          <GitPullRequest size={40} className="mb-4 text-gray-600" />
          <p className="text-sm">{t('prModal.placeholder')}</p>
        </div>
      </DialogBody>
    </Dialog>
  )
}
