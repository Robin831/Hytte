import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, MessageSquarePlus } from 'lucide-react'
import { Skeleton } from '../../components/ui/skeleton'
import { formatRelative } from './utils'
import { useFamilyChat } from './FamilyChatContext'

interface ConversationListProps {
  selectedConversationId: number | null
  onSelectConversation: (id: number) => void
  onNewConversation: () => void
}

interface Conversation {
  id: number
  name: string
  owner_user_id: number
  created_at: string
  last_message_at: string
  unread_count: number
  member_ids: number[]
  last_message_preview: string
  last_message_sender_id?: number
}

export default function ConversationList({
  selectedConversationId,
  onSelectConversation,
  onNewConversation,
}: ConversationListProps) {
  const { t, i18n } = useTranslation('familyChat')
  const { refreshSignal } = useFamilyChat()
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const rtf = useMemo(
    () => new Intl.RelativeTimeFormat(i18n.language, { numeric: 'auto' }),
    [i18n.language],
  )

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        setLoading(true)
        setError('')
        const res = await fetch('/api/familychat/conversations', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error('failed')
        const data = await res.json()
        setConversations(data.conversations ?? [])
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(t('errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [t, refreshSignal])

  return (
    <div className="flex flex-col h-full" data-testid="family-chat-conversation-list">
      <header className="flex items-center justify-between px-4 py-3 border-b border-gray-800">
        <h1 className="text-lg font-semibold text-white">{t('title')}</h1>
        <button
          type="button"
          onClick={onNewConversation}
          className="flex items-center gap-1 px-2.5 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-lg transition-colors cursor-pointer"
          aria-label={t('newConversation')}
          title={t('newConversation')}
        >
          <Plus size={16} aria-hidden="true" />
          <span className="hidden sm:inline">{t('newConversation')}</span>
        </button>
      </header>

      <div className="flex-1 overflow-y-auto">
        {loading && (
          <div className="p-4 space-y-3" role="status" aria-live="polite" aria-busy="true">
            <span className="sr-only">{t('loading')}</span>
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        )}

        {!loading && error && (
          <div className="m-4 p-3 bg-red-900/40 border border-red-700 rounded-lg text-red-300 text-sm">
            {error}
          </div>
        )}

        {!loading && !error && conversations.length === 0 && (
          <div className="p-6 text-center text-gray-400">
            <MessageSquarePlus size={32} className="mx-auto mb-3 text-gray-500" aria-hidden="true" />
            <p className="font-medium text-gray-300">{t('empty.title')}</p>
            <p className="text-sm text-gray-500 mt-1">{t('empty.hint')}</p>
          </div>
        )}

        {!loading && !error && conversations.length > 0 && (
          <ul className="divide-y divide-gray-800" role="list">
            {conversations.map(conv => {
              const isSelected = conv.id === selectedConversationId
              const timestamp = conv.last_message_at || conv.created_at
              const relative = formatRelative(timestamp, rtf, t('time.justNow'))
              const previewText = conv.last_message_preview || t('empty.noMessages')
              return (
                <li key={conv.id}>
                  <button
                    type="button"
                    onClick={() => onSelectConversation(conv.id)}
                    aria-current={isSelected ? 'true' : undefined}
                    className={`w-full text-left px-4 py-3 flex gap-3 items-start transition-colors cursor-pointer ${
                      isSelected ? 'bg-gray-800' : 'hover:bg-gray-900'
                    }`}
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="font-medium text-white truncate">
                          {conv.name || t('unnamedConversation')}
                        </p>
                        {conv.unread_count > 0 && (
                          <span
                            className="ml-auto flex-shrink-0 inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1.5 rounded-full bg-blue-600 text-white text-xs font-semibold"
                            aria-label={t('unreadCount', { count: conv.unread_count })}
                          >
                            {conv.unread_count > 99 ? '99+' : conv.unread_count}
                          </span>
                        )}
                      </div>
                      <p
                        className={`text-sm truncate mt-0.5 ${
                          conv.last_message_preview ? 'text-gray-400' : 'text-gray-500 italic'
                        }`}
                      >
                        {previewText}
                      </p>
                      {relative && (
                        <p className="text-xs text-gray-500 mt-0.5">{relative}</p>
                      )}
                    </div>
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </div>
  )
}
