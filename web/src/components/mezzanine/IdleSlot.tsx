import { useTranslation } from 'react-i18next'
import { MonitorOff } from 'lucide-react'

interface IdleSlotProps {
  slotIndex: number
}

export default function IdleSlot({ slotIndex }: IdleSlotProps) {
  const { t } = useTranslation('forge')

  return (
    <div
      className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-gray-700/50 bg-gray-800/30 px-4 py-8 text-gray-600"
      aria-label={t('mezzanine.idleSlot', { index: slotIndex + 1 })}
    >
      <MonitorOff size={24} className="text-gray-700" />
      <span className="text-sm">{t('mezzanine.idle')}</span>
    </div>
  )
}
