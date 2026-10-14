import { memo } from 'react'
import { ChevronLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import ModelSelect from './ModelSelect'
import type { ChatHeaderProps } from './types'

// Top bar of the chat pane: back-to-list button (mobile only), the active
// conversation's title, and the model selector.
function ChatHeader({ title, model, modelDisabled, onModelChange, onBack }: ChatHeaderProps) {
  const { t } = useTranslation('chat')
  return (
    <div className="flex items-center gap-3 px-4 h-14 border-b border-gray-700 shrink-0 bg-gray-900">
      <button
        onClick={onBack}
        className="md:hidden p-1 text-gray-400 hover:text-white cursor-pointer"
        aria-label={t('conversation.backLabel')}
      >
        <ChevronLeft size={20} />
      </button>
      <div className="min-w-0 flex-1">
        {title !== null ? (
          <h2 className="text-sm font-medium truncate">
            {title || t('newConversation')}
          </h2>
        ) : (
          <h2 className="text-sm font-medium text-gray-400 truncate">{t('header.selectOrStart')}</h2>
        )}
      </div>
      <ModelSelect value={model} disabled={modelDisabled} onChange={onModelChange} />
    </div>
  )
}

export default memo(ChatHeader)
