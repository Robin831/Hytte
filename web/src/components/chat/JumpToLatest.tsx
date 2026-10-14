import { memo } from 'react'
import { ArrowDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

// Floating "jump to latest" pill over the messages pane; the parent shows it
// only while the view is scrolled away from the bottom.
function JumpToLatest({ onClick }: { onClick: () => void }) {
  const { t } = useTranslation('chat')
  return (
    <button
      onClick={onClick}
      className="absolute bottom-4 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 px-3 py-2 rounded-full bg-gray-800 hover:bg-gray-700 text-gray-200 text-xs font-medium shadow-lg border border-gray-700 transition-colors cursor-pointer"
      aria-label={t('jumpToLatest')}
    >
      <ArrowDown size={14} />
      {t('jumpToLatest')}
    </button>
  )
}

export default memo(JumpToLatest)
