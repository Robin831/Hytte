import { Link } from 'react-router'
import { MessageCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { getJSON, useWidgetData } from './useWidgetData'

interface Conversation {
  id: number
  name: string
  last_message_at: string
  unread_count: number
  last_message_preview: string
}

const loadConversations = (signal: AbortSignal) =>
  getJSON<{ conversations: Conversation[] | null }>('/api/familychat/conversations', signal).then((d) => d.conversations ?? [])

export default function FamilyChatWidget() {
  const { t } = useTranslation('dashboard')
  const { data, loaded } = useWidgetData(loadConversations)
  if (!loaded || !data || data.length === 0) return null

  const unread = data.reduce((sum, c) => sum + c.unread_count, 0)
  // Unread conversations first; the server already orders by latest activity.
  const shown = [...data].sort((a, b) => Number(b.unread_count > 0) - Number(a.unread_count > 0)).slice(0, 3)

  return (
    <Widget title={t('widgets.familyChat.title')}>
      <p className="flex items-center gap-2 text-base font-semibold">
        <MessageCircle size={16} className={`shrink-0 ${unread > 0 ? 'text-green-400' : 'text-gray-500'}`} aria-hidden="true" />
        {unread > 0 ? t('widgets.familyChat.unread', { count: unread }) : t('widgets.familyChat.caughtUp')}
      </p>
      <ul className="mt-3 space-y-2">
        {shown.map((c) => (
          <li key={c.id}>
            <Link to="/family-chat" className="flex items-start justify-between gap-3 text-sm hover:text-blue-300">
              <span className="min-w-0">
                <span className={`block truncate ${c.unread_count > 0 ? 'font-semibold text-gray-100' : 'text-gray-300'}`}>
                  {c.name || t('widgets.familyChat.unnamed')}
                </span>
                {c.last_message_preview && <span className="block truncate text-xs text-gray-500">{c.last_message_preview}</span>}
              </span>
              {c.unread_count > 0 && (
                <span className="shrink-0 rounded-full bg-green-700 px-2 py-0.5 text-xs font-semibold tabular-nums text-white">
                  {c.unread_count}
                </span>
              )}
            </Link>
          </li>
        ))}
      </ul>
      <WidgetLink to="/family-chat">{t('widgets.familyChat.open')}</WidgetLink>
    </Widget>
  )
}
