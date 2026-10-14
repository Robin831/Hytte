import { memo } from 'react'
import { Bot, Loader2, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import MessageRow from './MessageRow'
import type { ChatPageMessageListProps } from './types'

// Contents of the scrollable messages pane: the welcome screen, the loading
// spinner, the empty-thread hint, or the list of memoized message rows. Each
// row receives only its own message and per-row booleans, so a streamed token
// re-renders just the row whose message object changed. Specific to the Chat
// page (pages/Chat.tsx); the shared list used by TripChat and Stride's coach
// thread is ChatMessageList in ChatThread.tsx.
function ChatPageMessageList({
  messages,
  hasConversation,
  loading,
  sending,
  onNew,
  onRegenerate,
  onEdit,
  endRef,
}: ChatPageMessageListProps) {
  const { t } = useTranslation('chat')

  if (!hasConversation) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-gray-500">
        <Bot size={48} className="mb-4 opacity-30" />
        <p className="text-lg font-medium">{t('welcome.title')}</p>
        <p className="text-sm mt-1">{t('welcome.subtitle')}</p>
        <button
          onClick={onNew}
          className="mt-4 flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer"
        >
          <Plus size={16} />
          {t('newChat')}
        </button>
      </div>
    )
  }

  if (loading) {
    return (
      <div className="flex justify-center py-12">
        <Loader2 size={32} className="animate-spin text-gray-400" />
      </div>
    )
  }

  if (messages.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-gray-500">
        <Bot size={40} className="mb-3 opacity-30" />
        <p className="text-sm">{t('emptyMessages')}</p>
      </div>
    )
  }

  const lastMessageId = messages[messages.length - 1].id
  return (
    <div className="max-w-3xl mx-auto px-4 py-6 space-y-6">
      {messages.map(msg => (
        <MessageRow
          key={msg.id}
          message={msg}
          // While streaming, the assistant placeholder is the last
          // assistant row with negative id. Show the "streaming…"
          // indicator only while its content is still empty so once
          // tokens arrive the user sees the live text.
          pending={sending && msg.role === 'assistant' && msg.id < 0 && msg.content === ''}
          // Regenerate targets only the newest reply, and only once
          // it is a persisted row and nothing is streaming.
          showRegenerate={msg.role === 'assistant' && msg.id === lastMessageId && msg.id > 0 && !sending}
          // Optimistic user rows (negative ids) aren't persisted yet
          // and so can't be truncated server-side.
          showEdit={msg.role === 'user' && msg.id > 0}
          actionsDisabled={sending}
          onRegenerate={onRegenerate}
          onEdit={onEdit}
        />
      ))}
      <div ref={endRef} />
    </div>
  )
}

export default memo(ChatPageMessageList)
