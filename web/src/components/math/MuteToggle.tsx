import { useTranslation } from 'react-i18next'
import { Volume2, VolumeX } from 'lucide-react'

interface MuteToggleProps {
  muted: boolean
  onToggle: () => void
  className?: string
}

export function MuteToggle({ muted, onToggle, className }: MuteToggleProps) {
  const { t } = useTranslation('regnemester')
  const label = muted ? t('feedback.unmuteAria') : t('feedback.muteAria')
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={label}
      aria-pressed={muted}
      title={`${label} (${t('feedback.shortcutHint')})`}
      className={
        className ??
        'inline-flex items-center justify-center rounded-lg border border-gray-700 hover:border-gray-500 bg-gray-800 hover:bg-gray-700 text-gray-200 hover:text-white w-9 h-9'
      }
    >
      {muted ? <VolumeX size={18} /> : <Volume2 size={18} />}
    </button>
  )
}
