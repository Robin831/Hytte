import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, MessageSquare, Download, X, WifiOff, Smile, MoreVertical } from 'lucide-react'
import { Skeleton } from '../../components/ui/skeleton'
import { useAuth } from '../../auth'
import Composer from './Composer'
import ReactionChips from './ReactionChips'
import ReactionPicker from './ReactionPicker'
import {
  addReaction,
  removeReaction,
  applyReactionEvent,
  editMessage,
  deleteMessage,
  type ReactionMap,
} from './api'
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
  edited_at?: string | null
  deleted_at?: string | null
  deleted_by?: number | null
  reactions?: ReactionMap
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
  const [lightbox, setLightbox] = useState<{ url: string; alt: string } | null>(null)
  // streamDropped flips to true while the SSE reconnect backoff is in flight.
  // It only ever shows the indicator after we've successfully connected once,
  // so the initial-load skeleton isn't shadowed by a "Reconnecting" badge.
  const [streamDropped, setStreamDropped] = useState(false)
  const [hasConnected, setHasConnected] = useState(false)
  // pickerForMsgId is the id of the bubble whose reaction picker is open, or
  // null when nothing is open. We only show one picker at a time.
  const [pickerForMsgId, setPickerForMsgId] = useState<number | null>(null)
  // menuForMsgId is the id of the own-message bubble whose actions menu (edit /
  // delete) is open. Only one menu is open at a time.
  const [menuForMsgId, setMenuForMsgId] = useState<number | null>(null)
  // editingMsgId is the id of the message currently being edited inline, with
  // editingDraft holding the in-progress text and editingError the most recent
  // save failure (so the user can retry without losing their draft).
  const [editingMsgId, setEditingMsgId] = useState<number | null>(null)
  const [editingDraft, setEditingDraft] = useState('')
  const [editingSaving, setEditingSaving] = useState(false)
  const [editingError, setEditingError] = useState('')
  // confirmDeleteId holds the id of the message whose delete-confirm modal is
  // open, or null when the modal is closed.
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null)
  const [deleteError, setDeleteError] = useState('')
  const deleteConfirmBtnRef = useRef<HTMLButtonElement>(null)
  const deletePrevFocusRef = useRef<Element | null>(null)

  const messagesEndRef = useRef<HTMLDivElement>(null)
  // currentUserIdRef shadows user?.id so the long-lived SSE reader closure
  // (recreated only when conversationId changes) can read the most recent
  // value without forcing the effect to re-run on auth changes.
  const currentUserIdRef = useRef<number | undefined>(user?.id)
  // lastPointerTypeRef is set by onPointerDown so onContextMenu knows whether
  // it was triggered by a touch long-press (open picker, suppress native menu)
  // or a mouse right-click (leave native menu alone; picker is on hover button).
  const lastPointerTypeRef = useRef<string>('mouse')
  useEffect(() => { currentUserIdRef.current = user?.id }, [user?.id])

  // Focus management + Escape handling for the delete confirmation modal,
  // matching the pattern used by ConfirmDialog.
  useEffect(() => {
    if (confirmDeleteId !== null) {
      deletePrevFocusRef.current = document.activeElement
      deleteConfirmBtnRef.current?.focus()
    } else if (deletePrevFocusRef.current instanceof HTMLElement) {
      deletePrevFocusRef.current.focus()
      deletePrevFocusRef.current = null
    }
  }, [confirmDeleteId])
  useEffect(() => {
    if (confirmDeleteId === null) return
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { setConfirmDeleteId(null); setDeleteError('') }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [confirmDeleteId])

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

  // Load conversation metadata + initial messages, then subscribe to the SSE
  // stream so new messages arrive without a refetch. The initial load and the
  // SSE subscription share a single AbortController so switching conversation
  // tears both down atomically; the SSE reader is also canceled explicitly so
  // tests (and the rare browser that doesn't propagate abort to a streaming
  // body) terminate the read loop deterministically.
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
    // lastId is the highest message id this client has rendered for the
    // current conversation. It seeds gap-fill queries on reconnect and is
    // updated by initial load, SSE events, and gap-fill responses.
    let lastId = 0
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    let reconnectAttempts = 0
    let activeReader: ReadableStreamDefaultReader<Uint8Array> | null = null

    setLoading(true)
    setError('')
    setMessages([])
    setConversation(null)
    setStreamDropped(false)
    setHasConnected(false)

    // appendIncoming deduplicates by id so a message that arrives via both
    // SSE and the POST response (the sender path) or via SSE and gap-fill
    // shows up exactly once.
    const appendIncoming = (msg: ChatMessage) => {
      if (msg.conversation_id !== conversationId) return
      if (msg.id > lastId) lastId = msg.id
      setMessages(prev => {
        if (prev.some(m => m.id === msg.id)) return prev
        return [...prev, msg]
      })
    }

    // applyReactionEventLocal merges an incoming reaction event into the
    // open message list. We can't compute the recipient's `me` flag from
    // the wire payload alone (the server broadcasts a single payload to
    // every subscriber), so the comparison happens here against the
    // current user's id.
    const applyReactionEventLocal = (
      payload: { message_id: number; user_id: number; emoji: string; count: number; conversation_id?: number },
      removed: boolean,
    ) => {
      if (payload.conversation_id !== undefined && payload.conversation_id !== conversationId) return
      setMessages(prev => {
        let changed = false
        const next = prev.map(m => {
          if (m.id !== payload.message_id) return m
          changed = true
          return {
            ...m,
            reactions: applyReactionEvent(m.reactions, payload, currentUserIdRef.current, removed),
          }
        })
        return changed ? next : prev
      })
    }

    // applyMessageEdited overwrites the body + edited_at of the matching
    // message. Keeps the existing reactions / attachment metadata intact.
    const applyMessageEdited = (
      payload: { message_id: number; body: string; edited_at: string; conversation_id?: number },
    ) => {
      if (payload.conversation_id !== undefined && payload.conversation_id !== conversationId) return
      setMessages(prev => prev.map(m =>
        m.id === payload.message_id
          ? { ...m, body: payload.body, edited_at: payload.edited_at }
          : m,
      ))
    }

    // applyMessageDeleted converts the matching message into a tombstone.
    // Body + attachment metadata are cleared so the bubble flips to the
    // "Message deleted" placeholder; deleted_at + deleted_by drive that
    // rendering and the timestamp tooltip.
    const applyMessageDeleted = (
      payload: { message_id: number; deleted_by: number; conversation_id?: number },
    ) => {
      if (payload.conversation_id !== undefined && payload.conversation_id !== conversationId) return
      const now = new Date().toISOString()
      setMessages(prev => prev.map(m =>
        m.id === payload.message_id
          ? {
              ...m,
              body: '',
              attachment_path: '',
              attachment_mime: '',
              edited_at: null,
              deleted_at: m.deleted_at ?? now,
              deleted_by: payload.deleted_by,
            }
          : m,
      ))
    }

    const fillGap = async () => {
      if (controller.signal.aborted) return
      try {
        const url = lastId > 0
          ? `/api/familychat/conversations/${conversationId}/messages?since=${lastId}`
          : `/api/familychat/conversations/${conversationId}/messages`
        const res = await fetch(url, { credentials: 'include', signal: controller.signal })
        if (!res.ok) return
        const data = await res.json()
        const msgs: ChatMessage[] = data.messages ?? []
        // API returns newest-first; sort ascending so lastId climbs
        // monotonically as we replay the burst.
        msgs.sort((a, b) => a.id - b.id)
        for (const m of msgs) appendIncoming(m)
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        // Non-fatal: the next reconnect attempt will retry.
      }
    }

    const scheduleReconnect = () => {
      if (controller.signal.aborted) return
      setStreamDropped(true)
      reconnectAttempts += 1
      // Exponential backoff capped at 30s to keep a server outage from
      // hammering the endpoint while still recovering quickly from a
      // transient blip.
      const delay = Math.min(30000, 1000 * 2 ** Math.min(reconnectAttempts, 5))
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null
        void connect(false)
      }, delay)
    }

    const connect = async (firstConnect: boolean) => {
      if (controller.signal.aborted) return
      // Skip the gap-fill on the very first connect: the initial /messages
      // fetch already covered everything up to lastId. On reconnects we
      // re-issue it so a disconnect window can't lose messages.
      if (!firstConnect) await fillGap()
      if (controller.signal.aborted) return
      let reader: ReadableStreamDefaultReader<Uint8Array> | null = null
      try {
        const res = await fetch(
          `/api/familychat/conversations/${conversationId}/stream`,
          { credentials: 'include', signal: controller.signal },
        )
        if (!res.ok || !res.body) {
          scheduleReconnect()
          return
        }
        reconnectAttempts = 0
        reader = res.body.getReader()
        activeReader = reader
        setStreamDropped(false)
        setHasConnected(true)
        const decoder = new TextDecoder()
        let buffer = ''
        let eventName = 'message'
        let dataLines: string[] = []
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          let nl = buffer.indexOf('\n')
          while (nl >= 0) {
            let line = buffer.slice(0, nl)
            buffer = buffer.slice(nl + 1)
            if (line.endsWith('\r')) line = line.slice(0, -1)
            if (line === '') {
              if (dataLines.length > 0) {
                try {
                  const payload = JSON.parse(dataLines.join('\n'))
                  if (eventName === 'message_new' && payload?.message) {
                    appendIncoming(payload.message as ChatMessage)
                  } else if (
                    (eventName === 'reaction_added' || eventName === 'reaction_removed') &&
                    payload?.message_id !== undefined &&
                    payload?.emoji !== undefined
                  ) {
                    applyReactionEventLocal(payload, eventName === 'reaction_removed')
                  } else if (
                    eventName === 'message_edited' &&
                    payload?.message_id !== undefined &&
                    payload?.body !== undefined &&
                    payload?.edited_at !== undefined
                  ) {
                    applyMessageEdited(payload)
                  } else if (
                    eventName === 'message_deleted' &&
                    payload?.message_id !== undefined &&
                    payload?.deleted_by !== undefined
                  ) {
                    applyMessageDeleted(payload)
                  }
                } catch {
                  // Ignore a malformed payload; the server should never emit
                  // one, but we don't want to tear down the whole stream over
                  // a single bad frame.
                }
              }
              eventName = 'message'
              dataLines = []
            } else if (line.startsWith(':')) {
              // SSE comment / heartbeat — ignore.
            } else if (line.startsWith('event:')) {
              eventName = line.slice(6).trimStart()
            } else if (line.startsWith('data:')) {
              const v = line.slice(5)
              dataLines.push(v.startsWith(' ') ? v.slice(1) : v)
            }
            nl = buffer.indexOf('\n')
          }
        }
        if (!controller.signal.aborted) scheduleReconnect()
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        if (!controller.signal.aborted) scheduleReconnect()
      } finally {
        if (activeReader === reader) activeReader = null
      }
    }

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
        if (sorted.length > 0) lastId = sorted[sorted.length - 1].id
        setMessages(sorted)
        void connect(true)
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        const key = err instanceof Error && err.message === 'conversation failed'
          ? 'errors.failedToLoadConversation'
          : 'errors.failedToLoadMessages'
        setError(t(key))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()

    return () => {
      controller.abort()
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer)
        reconnectTimer = null
      }
      // Cancel the reader so the read loop exits even when the fetch mock
      // doesn't propagate abort to the body (notably in tests). The catch is
      // intentional — cancel can throw if the reader is already detached.
      if (activeReader) {
        activeReader.cancel().catch(() => {})
        activeReader = null
      }
    }
  }, [conversationId, t])

  // Auto-scroll to the bottom whenever the message list updates. useLayoutEffect
  // avoids a visible jump between initial paint and the scroll snap.
  useLayoutEffect(() => {
    if (messagesEndRef.current) {
      messagesEndRef.current.scrollIntoView({ block: 'end' })
    }
  }, [messages.length, conversationId])

  // Lightbox: ESC closes; scroll on body locked while open.
  useEffect(() => {
    if (!lightbox) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setLightbox(null) }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [lightbox])

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

  // toggleReaction applies the change optimistically (chips update before the
  // network round-trip) and rolls back on failure. The eventual SSE
  // confirmation overwrites the optimistic state with the server-authoritative
  // count, which keeps two clients in sync even if either one races.
  const userId = user?.id
  const toggleReaction = useCallback(async (msgId: number, emoji: string, currentlyMine: boolean) => {
    if (conversationId === null || userId === undefined) return
    const meID = userId
    const snapshot = messages.find(m => m.id === msgId) ?? null
    setMessages(prev => prev.map(m => {
      if (m.id !== msgId) return m
      const synthetic = currentlyMine
        ? { user_id: meID, emoji, count: Math.max((m.reactions?.[emoji]?.count ?? 1) - 1, 0) }
        : { user_id: meID, emoji, count: (m.reactions?.[emoji]?.count ?? 0) + 1 }
      return {
        ...m,
        reactions: applyReactionEvent(m.reactions, synthetic, meID, currentlyMine),
      }
    }))
    try {
      if (currentlyMine) {
        await removeReaction(conversationId, msgId, emoji)
      } else {
        await addReaction(conversationId, msgId, emoji)
      }
    } catch {
      // Roll back only the reactions field to the pre-toggle snapshot. Rolling
      // back the whole message would clobber any concurrent SSE updates (edits,
      // other reactions) that arrived between the optimistic update and the
      // network failure.
      if (snapshot) {
        setMessages(prev => prev.map(m =>
          m.id === msgId ? { ...m, reactions: snapshot.reactions } : m,
        ))
      }
    }
  }, [conversationId, userId, messages])

  const handlePickFromPicker = useCallback((msgId: number, emoji: string) => {
    setPickerForMsgId(null)
    const msg = messages.find(m => m.id === msgId)
    const mine = !!msg?.reactions?.[emoji]?.me
    void toggleReaction(msgId, emoji, mine)
  }, [messages, toggleReaction])

  // openEditor opens the inline editor for a message bubble. Seeds the draft
  // from the current body so the user starts from what's on screen.
  const openEditor = useCallback((msg: ChatMessage) => {
    setMenuForMsgId(null)
    setEditingMsgId(msg.id)
    setEditingDraft(msg.body)
    setEditingError('')
    setEditingSaving(false)
  }, [])

  const cancelEditor = useCallback(() => {
    setEditingMsgId(null)
    setEditingDraft('')
    setEditingError('')
    setEditingSaving(false)
  }, [])

  const saveEditor = useCallback(async (msgId: number) => {
    if (conversationId === null) return
    const trimmed = editingDraft.trim()
    if (!trimmed) {
      setEditingError(t('edit.saveError'))
      return
    }
    setEditingSaving(true)
    setEditingError('')
    // Capture the pre-edit body/edited_at so a failed save can revert the
    // optimistic update — otherwise the bubble would keep showing the
    // unsaved draft as if it had been persisted.
    const snapshot = messages.find(m => m.id === msgId) ?? null
    // Optimistic update first: the SSE confirmation will overwrite shortly
    // with the server's authoritative edited_at, which matches the pattern
    // used by reactions / message sends in this view.
    const optimisticTime = new Date().toISOString()
    setMessages(prev => prev.map(m =>
      m.id === msgId ? { ...m, body: trimmed, edited_at: optimisticTime } : m,
    ))
    try {
      const updated = await editMessage(conversationId, msgId, trimmed)
      setMessages(prev => prev.map(m =>
        m.id === msgId
          ? { ...m, body: updated.body, edited_at: updated.edited_at }
          : m,
      ))
      setEditingMsgId(null)
      setEditingDraft('')
      setEditingSaving(false)
    } catch {
      if (snapshot) {
        setMessages(prev => prev.map(m =>
          m.id === msgId
            ? { ...m, body: snapshot.body, edited_at: snapshot.edited_at ?? null }
            : m,
        ))
      }
      setEditingError(t('edit.saveError'))
      setEditingSaving(false)
    }
  }, [conversationId, editingDraft, messages, t])

  const confirmDelete = useCallback(async (msgId: number) => {
    if (conversationId === null) return
    setDeleteError('')
    const meID = user?.id ?? null
    const now = new Date().toISOString()
    // Capture the pre-delete snapshot from the current render's state so the
    // rollback in catch{} doesn't depend on the setState updater having run.
    const snapshot = messages.find(m => m.id === msgId) ?? null
    setMessages(prev => prev.map(m => {
      if (m.id !== msgId) return m
      return {
        ...m,
        body: '',
        attachment_path: '',
        attachment_mime: '',
        edited_at: null,
        deleted_at: now,
        deleted_by: meID,
      }
    }))
    try {
      await deleteMessage(conversationId, msgId)
      // Only dismiss the confirm modal after the server has accepted the
      // delete — closing it earlier would hide the error message rendered
      // inside the same modal if the request fails.
      setConfirmDeleteId(null)
    } catch {
      if (snapshot) {
        setMessages(prev => prev.map(m => (m.id === msgId ? snapshot : m)))
      }
      setDeleteError(t('edit.deleteError'))
    }
  }, [conversationId, user?.id, t, messages])

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
        {hasConnected && streamDropped && (
          <span
            className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs bg-amber-500/15 border border-amber-500/40 text-amber-200 shrink-0"
            role="status"
            aria-live="polite"
            title={t('chat.connection.reconnecting')}
            data-testid="family-chat-reconnecting"
          >
            <WifiOff size={12} aria-hidden="true" />
            <span className="truncate max-w-[8rem] sm:max-w-none">{t('chat.connection.reconnecting')}</span>
          </span>
        )}
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
          const isDeleted = !!msg.deleted_at
          const isEditing = editingMsgId === msg.id
          const senderInfo = memberLookup.get(msg.sender_user_id)
          const senderLabel = senderInfo?.label ?? t('chat.memberFallback', { id: msg.sender_user_id })
          const relative = formatRelative(msg.created_at, rtf, t('time.justNow'))
          const attachmentUrl = !isDeleted && msg.attachment_path && msg.attachment_mime
            ? `/api/familychat/conversations/${msg.conversation_id}/attachments/${msg.id}`
            : ''
          const mime = msg.attachment_mime ?? ''
          const isImage = mime.startsWith('image/')
          const isAudio = mime.startsWith('audio/')
          const pickerOpen = pickerForMsgId === msg.id
          const menuOpen = menuForMsgId === msg.id
          const showActions = isOwn && !isDeleted && !isEditing
          const deletedByInfo = msg.deleted_by != null ? memberLookup.get(msg.deleted_by) : undefined
          const deletedByLabel = msg.deleted_by != null && user?.id === msg.deleted_by
            ? t('edit.tombstoneSelf')
            : t('edit.tombstone', { name: deletedByInfo?.label ?? t('chat.memberFallback', { id: msg.deleted_by ?? 0 }) })
          return (
            <div
              key={msg.id}
              className={`flex flex-col group ${isOwn ? 'items-end' : 'items-start'}`}
              data-testid={`chat-bubble-${msg.id}`}
            >
              {!isOwn && !isDeleted && (
                <span className="text-xs text-gray-400 mb-0.5 px-1">{senderLabel}</span>
              )}
              <div className={`relative max-w-[85%] sm:max-w-[70%]`}>
                {isDeleted ? (
                  <div
                    className="px-3 py-2 rounded-2xl text-sm italic bg-gray-800/60 border border-gray-700 text-gray-400"
                    data-testid={`chat-tombstone-${msg.id}`}
                  >
                    {deletedByLabel}
                  </div>
                ) : isEditing ? (
                  <div className={`px-3 py-2 rounded-2xl text-sm break-words ${
                    isOwn ? 'bg-blue-600/40 border border-blue-500' : 'bg-gray-800 border border-gray-700'
                  }`}>
                    <textarea
                      value={editingDraft}
                      onChange={(e) => setEditingDraft(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Escape') {
                          e.preventDefault()
                          cancelEditor()
                        } else if (e.key === 'Enter' && !e.shiftKey) {
                          e.preventDefault()
                          void saveEditor(msg.id)
                        }
                      }}
                      aria-label={t('edit.edit')}
                      data-testid={`chat-edit-input-${msg.id}`}
                      className="w-full bg-gray-900 text-gray-100 border border-gray-700 rounded-lg px-2 py-1 text-sm focus:outline-none focus:border-blue-500"
                      rows={3}
                      autoFocus
                    />
                    {editingError && (
                      <div className="text-xs text-red-400 mt-1">{editingError}</div>
                    )}
                    <div className="flex gap-2 mt-2 justify-end">
                      <button
                        type="button"
                        onClick={cancelEditor}
                        className="px-2 py-1 text-xs rounded-md bg-gray-700 text-gray-200 hover:bg-gray-600"
                        data-testid={`chat-edit-cancel-${msg.id}`}
                      >
                        {t('edit.cancel')}
                      </button>
                      <button
                        type="button"
                        onClick={() => { void saveEditor(msg.id) }}
                        disabled={editingSaving || !editingDraft.trim()}
                        className="px-2 py-1 text-xs rounded-md bg-blue-600 text-white hover:bg-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                        data-testid={`chat-edit-save-${msg.id}`}
                      >
                        {editingSaving ? t('edit.saving') : t('edit.save')}
                      </button>
                    </div>
                  </div>
                ) : (
                  <div
                    className={`px-3 py-2 rounded-2xl text-sm break-words ${
                      isOwn
                        ? 'bg-blue-600 text-white rounded-br-sm'
                        : 'bg-gray-800 text-gray-100 rounded-bl-sm'
                    }`}
                    onPointerDown={(e) => { lastPointerTypeRef.current = e.pointerType }}
                    onContextMenu={(e) => {
                      // Only intercept touch long-press (suppress native menu, open
                      // reaction picker for all messages). Mouse right-clicks keep
                      // the native menu so users can copy text/images; the reaction
                      // picker is reachable via the hover button (Smile icon) on
                      // desktop. Edit/delete actions remain accessible via the
                      // MoreVertical button for own messages.
                      if (lastPointerTypeRef.current === 'touch') {
                        e.preventDefault()
                        setPickerForMsgId(msg.id)
                      }
                    }}
                  >
                    {attachmentUrl && isImage && (
                      <button
                        type="button"
                        onClick={() => setLightbox({ url: attachmentUrl, alt: t('chat.attachmentImageAlt') })}
                        className="block cursor-zoom-in mb-1"
                        aria-label={t('chat.attachmentImageAlt')}
                      >
                        <img
                          src={attachmentUrl}
                          alt={t('chat.attachmentImageAlt')}
                          loading="lazy"
                          className="rounded-lg max-h-60 max-w-full object-contain"
                        />
                      </button>
                    )}
                    {attachmentUrl && isAudio && (
                      <audio
                        controls
                        src={attachmentUrl}
                        className="block max-w-full mb-1"
                        aria-label={t('chat.attachmentAudioAlt')}
                      />
                    )}
                    {attachmentUrl && !isImage && !isAudio && (
                      <a
                        href={attachmentUrl}
                        download
                        className={`flex items-center gap-2 rounded-lg px-2 py-1.5 mb-1 text-xs ${
                          isOwn ? 'bg-blue-700/60 hover:bg-blue-700/80' : 'bg-gray-700/70 hover:bg-gray-700'
                        }`}
                      >
                        <Download size={14} aria-hidden="true" />
                        <span className="truncate">{t('chat.attachmentFileLabel', { mime })}</span>
                      </a>
                    )}
                    {msg.body && (
                      <div className="whitespace-pre-wrap">{msg.body}</div>
                    )}
                  </div>
                )}
                {!isDeleted && !isEditing && (
                  <button
                    type="button"
                    onClick={() => setPickerForMsgId(pickerOpen ? null : msg.id)}
                    aria-label={t('reactions.pickerLabel')}
                    className={`absolute -top-3 ${isOwn ? '-left-2' : '-right-2'} p-1 rounded-full bg-gray-800 border border-gray-700 text-gray-300 hover:text-white opacity-0 group-hover:opacity-100 focus:opacity-100 transition-opacity cursor-pointer`}
                    data-testid={`reaction-trigger-${msg.id}`}
                  >
                    <Smile size={14} aria-hidden="true" />
                  </button>
                )}
                {showActions && (
                  <button
                    type="button"
                    onClick={() => setMenuForMsgId(menuOpen ? null : msg.id)}
                    aria-label={t('edit.menuLabel')}
                    aria-haspopup="menu"
                    aria-expanded={menuOpen}
                    className="absolute -top-3 -right-2 p-1 rounded-full bg-gray-800 border border-gray-700 text-gray-300 hover:text-white opacity-0 group-hover:opacity-100 focus:opacity-100 transition-opacity cursor-pointer"
                    data-testid={`chat-actions-trigger-${msg.id}`}
                  >
                    <MoreVertical size={14} aria-hidden="true" />
                  </button>
                )}
                {menuOpen && showActions && (
                  <>
                    {/* Click outside to dismiss — full-viewport transparent layer
                        intercepts the next click and closes the menu without
                        eating any actual UI interaction. */}
                    <button
                      type="button"
                      aria-hidden="true"
                      tabIndex={-1}
                      onClick={() => setMenuForMsgId(null)}
                      className="fixed inset-0 z-40 cursor-default"
                    />
                    <div
                      role="menu"
                      aria-label={t('edit.menuLabel')}
                      data-testid={`chat-actions-menu-${msg.id}`}
                      className="absolute z-50 -top-2 right-0 mt-6 min-w-[8rem] bg-gray-800 border border-gray-700 rounded-lg shadow-lg overflow-hidden"
                    >
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => openEditor(msg)}
                        className="w-full text-left px-3 py-2 text-sm text-gray-200 hover:bg-gray-700"
                        data-testid={`chat-edit-action-${msg.id}`}
                      >
                        {t('edit.edit')}
                      </button>
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => {
                          setMenuForMsgId(null)
                          setDeleteError('')
                          setConfirmDeleteId(msg.id)
                        }}
                        className="w-full text-left px-3 py-2 text-sm text-red-300 hover:bg-gray-700"
                        data-testid={`chat-delete-action-${msg.id}`}
                      >
                        {t('edit.delete')}
                      </button>
                    </div>
                  </>
                )}
                {pickerOpen && (
                  <ReactionPicker
                    onPick={(emoji) => handlePickFromPicker(msg.id, emoji)}
                    onClose={() => setPickerForMsgId(null)}
                  />
                )}
              </div>
              <ReactionChips
                reactions={msg.reactions}
                onToggle={(emoji, mine) => { void toggleReaction(msg.id, emoji, mine) }}
              />
              <div className="flex items-center gap-1 mt-0.5 px-1">
                {!isDeleted && msg.edited_at && (
                  <span
                    className="text-[10px] text-gray-500 italic"
                    title={msg.edited_at}
                    data-testid={`chat-edited-tag-${msg.id}`}
                  >
                    ({t('edit.editedTag')})
                  </span>
                )}
                {relative && (
                  <span className="text-[10px] text-gray-500">{relative}</span>
                )}
              </div>
            </div>
          )
        })}
        <div ref={messagesEndRef} />
      </div>

      <div className="border-t border-gray-800 bg-gray-950 shrink-0">
        <Composer conversationId={conversationId} onMessageCreated={handleMessageCreated} />
      </div>

      {lightbox && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label={t('chat.lightboxTitle')}
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
          onClick={(e) => { if (e.target === e.currentTarget) setLightbox(null) }}
        >
          <button
            type="button"
            onClick={() => setLightbox(null)}
            aria-label={t('chat.lightboxClose')}
            className="absolute top-4 right-4 p-2 text-white/80 hover:text-white bg-black/40 rounded-full cursor-pointer"
          >
            <X size={24} aria-hidden="true" />
          </button>
          <img
            src={lightbox.url}
            alt={lightbox.alt}
            className="max-w-full max-h-full object-contain"
          />
        </div>
      )}

      {confirmDeleteId !== null && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="family-chat-confirm-delete-title"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4"
          onClick={(e) => { if (e.target === e.currentTarget) setConfirmDeleteId(null) }}
          data-testid="chat-delete-confirm"
        >
          <div className="bg-gray-900 border border-gray-700 rounded-lg max-w-md w-full p-4 shadow-xl">
            <p id="family-chat-confirm-delete-title" className="text-sm text-gray-100">
              {t('edit.confirmDelete')}
            </p>
            {deleteError && (
              <p className="mt-2 text-xs text-red-400">{deleteError}</p>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setConfirmDeleteId(null)}
                className="px-3 py-1.5 text-sm rounded-md bg-gray-800 text-gray-200 hover:bg-gray-700"
                data-testid="chat-delete-cancel"
              >
                {t('edit.cancel')}
              </button>
              <button
                ref={deleteConfirmBtnRef}
                type="button"
                onClick={() => { void confirmDelete(confirmDeleteId) }}
                className="px-3 py-1.5 text-sm rounded-md bg-red-600 text-white hover:bg-red-500"
                data-testid="chat-delete-confirm-button"
              >
                {t('edit.delete')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
