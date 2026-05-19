import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, MessageSquare } from 'lucide-react'
import { Skeleton } from '../../components/ui/skeleton'
import { useAuth } from '../../auth'
import Composer from './Composer'
import { formatRelative } from './utils'

interface ChatViewProps {
  conversationId: number | null
  onBack: () => void
}

interface Conversation {
  id: number
  name: string
  owner_user_id: number
  created_at: string
  last_message_at: string
  unread_count: number
  member_ids: number[]
}

export interface ChatMessage {
  id: number
  conversation_id: number
  sender_user_id: number
  body: string
  attachment_path?: string
  attachment_mime?: string
  created_at: string
}

interface MemberInfo {
  label: string
  emoji: string
}

interface FamilyChild {
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface SiblingInfo {
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface ParentInfo {
  user_id: number
  name: string
  picture: string
}

export default function ChatView({ conversationId, onBack }: ChatViewProps) {
  const { t, i18n } = useTranslation('familyChat')
  const { user, familyStatus } = useAuth()

  const [conversation, setConversation] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [memberLookup, setMemberLookup] = useState<Map<number, MemberInfo>>(new Map())

  const messagesEndRef = useRef<HTMLDivElement>(null)

  const rtf = useMemo(
    () => new Intl.RelativeTimeFormat(i18n.language, { numeric: 'auto' }),
    [i18n.language],
  )

  // Build a label/emoji lookup for every user the current user can name,
  // so member chips and sender labels render with friendly names. The
  // current user is always included from auth context.
  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    ;(async () => {
      const lookup = new Map<number, MemberInfo>()
      lookup.set(user.id, { label: user.name || user.email || `#${user.id}`, emoji: '👤' })
      try {
        if (familyStatus?.is_parent) {
          const res = await fetch('/api/family/children', {
            credentials: 'include',
            signal: controller.signal,
          })
          if (res.ok) {
            const data = await res.json()
            const kids: FamilyChild[] = data.children ?? []
            for (const k of kids) {
              lookup.set(k.child_id, {
                label: k.nickname || `#${k.child_id}`,
                emoji: k.avatar_emoji || '⭐',
              })
            }
          }
        }
        if (familyStatus?.is_child) {
          const res = await fetch('/api/family/my-family', {
            credentials: 'include',
            signal: controller.signal,
          })
          if (res.ok) {
            const data = await res.json()
            const parent: ParentInfo | undefined = data.parent
            if (parent?.user_id) {
              lookup.set(parent.user_id, {
                label: parent.name || t('newModal.parent'),
                emoji: '👤',
              })
            }
            const siblings: SiblingInfo[] = data.siblings ?? []
            for (const s of siblings) {
              lookup.set(s.child_id, {
                label: s.nickname || `#${s.child_id}`,
                emoji: s.avatar_emoji || '⭐',
              })
            }
          }
        }
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        // Non-fatal: chips fall back to "Member #id" if the lookup is empty.
      }
      if (!controller.signal.aborted) setMemberLookup(lookup)
    })()
    return () => { controller.abort() }
  }, [user, familyStatus, t])

  // Load conversation metadata + initial messages whenever the selected
  // conversation changes. Both requests share an AbortController so a fast
  // selection switch cannot race a stale response into state.
  useEffect(() => {
    if (conversationId === null) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setConversation(null)
      setMessages([])
      setError('')
      setLoading(false)
      return
    }
    const controller = new AbortController()
    setLoading(true)
    setError('')
    setMessages([])
    setConversation(null)
    ;(async () => {
      try {
        const [convRes, msgRes] = await Promise.all([
          fetch(`/api/familychat/conversations/${conversationId}`, {
            credentials: 'include',
            signal: controller.signal,
          }),
          fetch(`/api/familychat/conversations/${conversationId}/messages`, {
            credentials: 'include',
            signal: controller.signal,
          }),
        ])
        if (!convRes.ok) throw new Error('conversation failed')
        if (!msgRes.ok) throw new Error('messages failed')
        const convData = await convRes.json()
        const msgData = await msgRes.json()
        if (controller.signal.aborted) return
        setConversation(convData.conversation ?? null)
        // The API returns newest-first; display oldest at top to bottom.
        const sorted: ChatMessage[] = (msgData.messages ?? []).slice().reverse()
        setMessages(sorted)
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(t('errors.failedToLoadMessages'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [conversationId, t])

  // Auto-scroll to the bottom whenever the message list updates. useLayoutEffect
  // avoids a visible jump between initial paint and the scroll snap.
  useLayoutEffect(() => {
    if (messagesEndRef.current) {
      messagesEndRef.current.scrollIntoView({ block: 'end' })
    }
  }, [messages.length, conversationId])

  const handleMessageCreated = useCallback((msg: ChatMessage) => {
    // Defensive: if the user switched conversations while a send was in
    // flight, drop the message rather than leaking it into the wrong chat.
    if (msg.conversation_id !== conversationId) return
    setMessages(prev => {
      // Guard against the rare case where SSE (sub-task 4) and the REST
      // response both deliver the same message: drop any duplicate id.
      if (prev.some(m => m.id === msg.id)) return prev
      return [...prev, msg]
    })
  }, [conversationId])

  const memberChips = useMemo(() => {
    if (!conversation) return []
    return conversation.member_ids.map(id => {
      const info = memberLookup.get(id)
      const isSelf = user?.id === id
      return {
        id,
        label: isSelf
          ? t('chat.you')
          : info?.label ?? t('chat.memberFallback', { id }),
        emoji: info?.emoji ?? '👤',
        isSelf,
      }
    })
  }, [conversation, memberLookup, t, user?.id])

  if (conversationId === null) {
    return (
      <div
        className="flex flex-col items-center justify-center h-full text-center px-6 text-gray-400"
        data-testid="family-chat-view"
      >
        <MessageSquare size={48} className="mb-3 text-gray-600" aria-hidden="true" />
        <p className="font-medium text-gray-300">{t('chat.noSelectionTitle')}</p>
        <p className="text-sm text-gray-500 mt-1">{t('chat.noSelectionHint')}</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full min-h-0" data-testid="family-chat-view">
      <header className="flex items-center gap-2 px-3 sm:px-4 py-3 border-b border-gray-800 bg-gray-950 shrink-0">
        <button
          type="button"
          onClick={onBack}
          aria-label={t('chat.back')}
          className="md:hidden p-1.5 -ml-1 text-gray-300 hover:text-white rounded-md cursor-pointer"
        >
          <ChevronLeft size={20} aria-hidden="true" />
        </button>
        <div className="flex-1 min-w-0">
          <h2 className="text-base sm:text-lg font-semibold text-white truncate">
            {loading && !conversation ? (
              <Skeleton className="h-5 w-40" />
            ) : (
              conversation?.name || t('unnamedConversation')
            )}
          </h2>
          {memberChips.length > 0 && (
            <ul
              className="flex flex-wrap gap-1.5 mt-1.5"
              aria-label={t('chat.membersLabel')}
              role="list"
            >
              {memberChips.map(chip => (
                <li
                  key={chip.id}
                  className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs border ${
                    chip.isSelf
                      ? 'bg-blue-500/15 border-blue-500/40 text-blue-200'
                      : 'bg-gray-800 border-gray-700 text-gray-300'
                  }`}
                >
                  <span aria-hidden="true">{chip.emoji}</span>
                  <span className="truncate max-w-[10rem]">{chip.label}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </header>

      <div
        className="flex-1 min-h-0 overflow-y-auto px-3 sm:px-4 py-3 space-y-2"
        role="log"
        aria-live="polite"
        aria-relevant="additions"
      >
        {loading && (
          <div className="space-y-3" role="status" aria-busy="true">
            <span className="sr-only">{t('loading')}</span>
            <Skeleton className="h-12 w-3/4" />
            <Skeleton className="h-12 w-2/3 ml-auto" />
            <Skeleton className="h-12 w-1/2" />
          </div>
        )}

        {!loading && error && (
          <div className="p-3 bg-red-900/40 border border-red-700 rounded-lg text-red-300 text-sm">
            {error}
          </div>
        )}

        {!loading && !error && messages.length === 0 && (
          <div className="flex flex-col items-center justify-center h-full text-center text-gray-500 py-12">
            <MessageSquare size={32} className="mb-2 text-gray-600" aria-hidden="true" />
            <p className="text-sm">{t('chat.emptyMessages')}</p>
          </div>
        )}

        {!loading && !error && messages.map(msg => {
          const isOwn = user?.id === msg.sender_user_id
          const senderInfo = memberLookup.get(msg.sender_user_id)
          const senderLabel = senderInfo?.label ?? t('chat.memberFallback', { id: msg.sender_user_id })
          const relative = formatRelative(msg.created_at, rtf, t('time.justNow'))
          return (
            <div
              key={msg.id}
              className={`flex flex-col ${isOwn ? 'items-end' : 'items-start'}`}
            >
              {!isOwn && (
                <span className="text-xs text-gray-400 mb-0.5 px-1">{senderLabel}</span>
              )}
              <div
                className={`max-w-[85%] sm:max-w-[70%] px-3 py-2 rounded-2xl text-sm whitespace-pre-wrap break-words ${
                  isOwn
                    ? 'bg-blue-600 text-white rounded-br-sm'
                    : 'bg-gray-800 text-gray-100 rounded-bl-sm'
                }`}
              >
                {msg.body}
              </div>
              {relative && (
                <span className="text-[10px] text-gray-500 mt-0.5 px-1">{relative}</span>
              )}
            </div>
          )
        })}
        <div ref={messagesEndRef} />
      </div>

      <div className="border-t border-gray-800 bg-gray-950 shrink-0">
        <Composer conversationId={conversationId} onMessageCreated={handleMessageCreated} />
      </div>
    </div>
  )
}
