import { useCallback, useEffect, useRef, useState } from 'react'
import type { Dispatch, RefObject, SetStateAction } from 'react'
import { useTranslation } from 'react-i18next'

export interface Conversation {
  id: number
  user_id: number
  title: string
  model: string
  created_at: string
  updated_at: string
}

export interface Message {
  id: number
  conversation_id: number
  role: 'user' | 'assistant'
  content: string
  created_at: string
}

interface UseChatStreamParams {
  /** The conversation a send targets; UI updates from a stream are gated on it still being viewed. */
  activeConversation: Conversation | null
  setActiveConversation: Dispatch<SetStateAction<Conversation | null>>
  setMessages: Dispatch<SetStateAction<Message[]>>
  setConversations: Dispatch<SetStateAction<Conversation[]>>
  /** Conversations with an in-flight send, used to guard against double sends. */
  sendingConversationIds: Set<number>
  addSendingConversation: (id: number) => void
  removeSendingConversation: (id: number) => void
  /** Locally deleted conversation ids so a late response can't resurrect them. */
  deletedConversationIds: RefObject<Set<number>>
  /** Clears the input on send start; restores the draft on recoverable failure. */
  setInput: (value: string) => void
  /** Re-pins the messages view to the bottom when a send begins (UI concern). */
  pinToBottom?: () => void
  /** Returns focus to the input once a send settles (UI concern). */
  focusInput?: () => void
}

export interface UseChatStream {
  /** Starts an SSE send for `content` in the active conversation. */
  send: (content: string) => Promise<void>
  /**
   * Aborts the in-flight stream for `conversationId` (Stop button), or every
   * in-flight stream when called without an id (unmount).
   */
  stop: (conversationId?: number) => void
  /** Id of the assistant placeholder currently streaming, or null. */
  streamingId: number | null
  /** Last send/stream error message, or '' when clear. */
  error: string
  clearError: () => void
  /**
   * Merges a freshly loaded message list for `conversationId` with any reply
   * still streaming (or just finished) for it, and surfaces a send that failed
   * while the user was elsewhere (error banner + restored draft). Call it with
   * the result of every conversation messages fetch before storing it.
   */
  reconcileLoaded: (conversationId: number, loaded: Message[]) => Message[]
}

// Per-conversation state of the latest send, kept outside React state so the
// conversation messages refetch can merge it in (see `reconcileLoaded`).
interface LiveReply {
  tempUser: Message
  tempAssistantId: number
  canonicalUser: Message | null
  accumulated: string
  canonicalAssistant: Message | null
}

// A send that failed while the user was viewing another conversation; shown
// when they return to it.
interface MissedFailure {
  content: string
  error: string
}

// True when `rows` already holds the persisted reply to `userRow`: message ids
// are autoincrement, so an assistant row with a later id answered it.
function hasPersistedReply(rows: Message[], userRow: Message | null): boolean {
  if (!userRow || userRow.id < 0) return false
  return rows.some(m => m.role === 'assistant' && m.id > userRow.id)
}

/**
 * Merges `live` into a loaded message list, deduping by id so a refetch that
 * raced the stream never shows the reply twice or drops it.
 */
export function mergeLiveReply(loaded: Message[], live: LiveReply): Message[] {
  const ids = new Set(loaded.map(m => m.id))
  const out = [...loaded]
  const userRow = live.canonicalUser
  if (userRow) {
    if (!ids.has(userRow.id)) out.push(userRow)
  } else {
    // No `user_message` frame yet, so the server may or may not have saved
    // it. Treat a trailing user row with the same text as the saved copy.
    const last = loaded[loaded.length - 1]
    if (!(last && last.role === 'user' && last.content === live.tempUser.content)) {
      out.push(live.tempUser)
    }
  }
  if (live.canonicalAssistant) {
    if (!ids.has(live.canonicalAssistant.id)) out.push(live.canonicalAssistant)
  } else if (!hasPersistedReply(loaded, userRow)) {
    out.push({
      id: live.tempAssistantId,
      conversation_id: live.tempUser.conversation_id,
      role: 'assistant',
      content: live.accumulated,
      created_at: live.tempUser.created_at,
    })
  }
  return out
}

/**
 * Owns the chat SSE streaming state machine: optimistic placeholder rows, frame
 * parsing, the three error-recovery branches, abort handling, and the
 * post-stream conversation-list refetch. Lifted out of `Chat.tsx` so the parser
 * and reconciliation can be unit-tested and the component keeps only UI state.
 */
export function useChatStream({
  activeConversation,
  setActiveConversation,
  setMessages,
  setConversations,
  sendingConversationIds,
  addSendingConversation,
  removeSendingConversation,
  deletedConversationIds,
  setInput,
  pinToBottom,
  focusInput,
}: UseChatStreamParams): UseChatStream {
  const { t } = useTranslation('chat')
  const [error, setError] = useState('')
  const [streamingId, setStreamingId] = useState<number | null>(null)
  const clearError = useCallback(() => setError(''), [])
  // In-flight streaming requests keyed by conversation id, so Stop can cancel
  // one conversation's reply while streams in other conversations keep running
  // in the background (switching away no longer aborts a reply).
  const streamAbortsRef = useRef<Map<number, AbortController>>(new Map())
  // The latest send per conversation while it streams, and after `done` until
  // its rows are known to be on screen; merged into a refetched message list.
  const liveRepliesRef = useRef<Map<number, LiveReply>>(new Map())
  // Sends that failed while their conversation was not on screen.
  const missedFailuresRef = useRef<Map<number, MissedFailure>>(new Map())
  // The conversation currently on screen. Streams gate every UI mutation on
  // this so tokens from a background stream never bleed into another thread.
  const viewedConversationIdRef = useRef<number | null>(activeConversation?.id ?? null)
  const viewedConversationId = activeConversation?.id ?? null
  useEffect(() => {
    viewedConversationIdRef.current = viewedConversationId
  }, [viewedConversationId])

  const stop = useCallback((conversationId?: number) => {
    const aborts = streamAbortsRef.current
    if (conversationId === undefined) {
      for (const controller of aborts.values()) controller.abort()
      aborts.clear()
      return
    }
    aborts.get(conversationId)?.abort()
    aborts.delete(conversationId)
  }, [])

  const reconcileLoaded = useCallback(
    (conversationId: number, loaded: Message[]) => {
      const failure = missedFailuresRef.current.get(conversationId)
      if (failure) {
        missedFailuresRef.current.delete(conversationId)
        setInput(failure.content)
        setError(failure.error)
      }
      const live = liveRepliesRef.current.get(conversationId)
      if (!live) return loaded
      // A finished reply only needs merging once: after this its canonical
      // rows are on screen, and keeping it around could resurrect them after
      // a later truncate (Edit / Regenerate).
      if (live.canonicalAssistant) liveRepliesRef.current.delete(conversationId)
      return mergeLiveReply(loaded, live)
    },
    [setInput],
  )

  const send = useCallback(
    async (content: string) => {
      if (!content || !activeConversation || sendingConversationIds.has(activeConversation.id)) {
        return
      }
      const sentConversationId = activeConversation.id
      const isViewed = () => viewedConversationIdRef.current === sentConversationId
      // The messages state belongs to the sent conversation when every row is
      // from it. After switching away and back, the refetch replaces the state
      // with canonical rows, so the optimistic placeholders are gone.
      const stateIsSentConversation = (prev: Message[]) =>
        prev.length > 0 && prev.every(m => m.conversation_id === sentConversationId)
      setInput('')
      addSendingConversation(sentConversationId)
      setError('')

      // Optimistic rows: the user bubble and an empty assistant bubble that
      // we mutate as `token` events arrive. Negative ids mark them as
      // placeholders that will be swapped for canonical rows on `user_message`
      // / `done`.
      const tempUserId = -Date.now()
      const tempAssistantId = tempUserId - 1
      const tempUserMsg: Message = {
        id: tempUserId,
        conversation_id: sentConversationId,
        role: 'user',
        content,
        created_at: new Date().toISOString(),
      }
      const tempAssistantMsg: Message = {
        id: tempAssistantId,
        conversation_id: sentConversationId,
        role: 'assistant',
        content: '',
        created_at: new Date().toISOString(),
      }
      const live: LiveReply = {
        tempUser: tempUserMsg,
        tempAssistantId,
        canonicalUser: null,
        accumulated: '',
        canonicalAssistant: null,
      }
      liveRepliesRef.current.set(sentConversationId, live)
      missedFailuresRef.current.delete(sentConversationId)
      const forgetLive = () => {
        if (liveRepliesRef.current.get(sentConversationId) === live) {
          liveRepliesRef.current.delete(sentConversationId)
        }
      }
      setMessages(prev => [...prev, tempUserMsg, tempAssistantMsg])
      setStreamingId(tempAssistantId)
      // Sending is a deliberate action: re-pin to the bottom so the user's new
      // message and the streamed reply stay in view, even if they had scrolled up.
      pinToBottom?.()

      // Abort any earlier in-flight stream for this conversation before
      // starting this one; streams in other conversations are left alone.
      streamAbortsRef.current.get(sentConversationId)?.abort()
      const controller = new AbortController()
      streamAbortsRef.current.set(sentConversationId, controller)

      // Accumulate streamed text in a local string so the placeholder row's
      // content reflects the full assistant reply on every render, even though
      // React batches the setState calls. Accumulation continues while the
      // user is viewing another conversation; only the UI update is skipped.
      let accumulated = ''
      const applyAccumulated = () => {
        live.accumulated = accumulated
        if (!isViewed()) return
        const next = accumulated
        const userRow = live.canonicalUser
        setMessages(prev => {
          if (prev.some(m => m.id === tempAssistantId)) {
            return prev.map(m => (m.id === tempAssistantId ? { ...m, content: next } : m))
          }
          // The user came back mid-reply and the refetch dropped the
          // placeholder: re-add it so the rest of the reply streams in view,
          // unless the refetch already holds the persisted reply.
          if (stateIsSentConversation(prev) && !hasPersistedReply(prev, userRow)) {
            return [...prev, { ...tempAssistantMsg, content: next }]
          }
          return prev
        })
      }

      let sawToken = false
      // Hoisted so the catch block can reference the canonical user row when
      // deciding whether to keep the user bubble visible after a stream error.
      let canonicalUserMsg: Message | null = null

      try {
        const res = await fetch(`/api/chat/conversations/${sentConversationId}/messages/stream`, {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content }),
          signal: controller.signal,
        })
        if (!res.ok || !res.body) {
          const data = await res.json().catch(() => null)
          throw new Error(data?.error || t('errors.failedToSend'))
        }

        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        let canonicalAssistantMsg: Message | null = null
        let serverError: string | null = null

        const handleFrame = (frame: string) => {
          let eventName = 'message'
          const dataLines: string[] = []
          for (const line of frame.split('\n')) {
            if (line.startsWith('event:')) {
              eventName = line.slice(6).trim()
            } else if (line.startsWith('data:')) {
              dataLines.push(line.slice(5).trim())
            }
          }
          if (dataLines.length === 0) return
          let payload: unknown
          try {
            payload = JSON.parse(dataLines.join('\n'))
          } catch {
            return
          }
          if (eventName === 'token') {
            const text = (payload as { text?: string }).text ?? ''
            if (text) {
              sawToken = true
              accumulated += text
              applyAccumulated()
            }
          } else if (eventName === 'user_message') {
            canonicalUserMsg = payload as Message
            live.canonicalUser = canonicalUserMsg
          } else if (eventName === 'done') {
            canonicalAssistantMsg = (payload as { assistant_message?: Message }).assistant_message ?? null
            live.canonicalAssistant = canonicalAssistantMsg
          } else if (eventName === 'error') {
            serverError = (payload as { error?: string }).error ?? t('errors.streamError')
          }
        }

        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          const frames = buffer.split('\n\n')
          buffer = frames.pop() ?? ''
          for (const frame of frames) {
            if (!frame.trim()) continue
            handleFrame(frame)
          }
        }
        // Flush the TextDecoder's internal buffer so any multi-byte UTF-8
        // codepoint that was split across the final two chunks is completed.
        buffer += decoder.decode()
        // Flush any trailing frame that didn't end with the double-newline.
        if (buffer.trim()) handleFrame(buffer)

        if (serverError) {
          throw new Error(serverError)
        }

        // Detect an unexpected server disconnect: the stream ended without
        // a `done` or `error` event and the AbortController was not fired
        // (AbortError from Stop/navigation reaches the catch block, not here).
        if (!canonicalAssistantMsg && !controller.signal.aborted) {
          throw new Error(t('errors.streamError'))
        }

        // Final swap: replace optimistic rows with the canonical ones the
        // server returned. Skip while the user is viewing another
        // conversation; the messages refetch on return shows persisted rows.
        if (isViewed()) {
          // Both are assigned inside handleFrame, which TS's control-flow
          // analysis can't see, so restate their declared types.
          const finalUserMsg = canonicalUserMsg as Message | null
          const finalAssistantMsg = canonicalAssistantMsg as Message | null
          setMessages(prev => {
            // Drop the assistant placeholder if Claude returned an empty body
            // and we never saw a token; otherwise replace with canonical.
            if (!finalAssistantMsg && !sawToken) {
              return prev.filter(m => m.id !== tempUserId && m.id !== tempAssistantId)
            }
            const ids = new Set(prev.map(m => m.id))
            const hasPlaceholders = ids.has(tempUserId) || ids.has(tempAssistantId)
            if (hasPlaceholders) {
              // The canonical rows land on screen here, so a later refetch
              // has nothing to merge (idempotent if React re-runs this).
              forgetLive()
              // A refetch may have landed between the last token and `done`
              // with the canonical rows already in it: drop the placeholder
              // instead of swapping in a second copy of the same id.
              const swap = (m: Message, canonical: Message | null) =>
                !canonical ? [m] : ids.has(canonical.id) ? [] : [canonical]
              return prev.flatMap(m => {
                if (m.id === tempUserId) return swap(m, finalUserMsg)
                if (m.id === tempAssistantId) return swap(m, finalAssistantMsg)
                return [m]
              })
            }
            // The user switched away and back: the refetch replaced the state
            // and dropped the placeholders. Append the canonical rows that
            // the refetch did not already include (deduped by id). While the
            // refetch is still in flight (state still holds the previous
            // conversation), `reconcileLoaded` merges them in when it lands.
            if (!stateIsSentConversation(prev)) return prev
            forgetLive()
            const missing = [finalUserMsg, finalAssistantMsg].filter(
              (m): m is Message => m !== null && !ids.has(m.id),
            )
            return missing.length > 0 ? [...prev, ...missing] : prev
          })
        }

        // Refresh conversation list to pick up auto-title updates (non-fatal).
        try {
          const convRes = await fetch('/api/chat/conversations', { credentials: 'include' })
          if (convRes.ok) {
            const convData = await convRes.json()
            const allConvs: Conversation[] = convData.conversations ?? []
            setConversations(
              allConvs.filter(c => !deletedConversationIds.current.has(c.id)),
            )
            const updated = allConvs.find(c => c.id === sentConversationId)
            if (updated) {
              setActiveConversation(current =>
                current?.id === sentConversationId ? updated : current,
              )
            }
          }
        } catch (refreshErr) {
          console.error('Failed to refresh conversations after sending message', refreshErr)
        }
      } catch (err) {
        // Whatever the failure, the server holds the authoritative rows now
        // (a refetch shows them), so there is no live reply left to merge.
        forgetLive()
        const isAbort = err instanceof DOMException && err.name === 'AbortError'
        if (!isViewed()) {
          // The user is in another conversation: leave its messages, input and
          // error banner untouched. Returning here refetches the persisted
          // rows; a real failure (not Stop / delete / unmount) also restores
          // the draft and shows the error then, since the message may never
          // have been saved.
          if (!isAbort) {
            missedFailuresRef.current.set(sentConversationId, {
              content,
              error: (err instanceof Error && err.message) || t('errors.streamError'),
            })
          }
        } else if (isAbort) {
          if (sawToken) {
            // Stop semantics: keep the partial assistant text on screen at
            // whatever was already streamed. The server saved that partial
            // text as the assistant row, so the placeholders mirror it until
            // the next refetch replaces them with the canonical rows.
            setMessages(prev =>
              prev.map(m =>
                m.id === tempAssistantId
                  ? { ...m, content: accumulated || m.content }
                  : m,
              ),
            )
          } else if (canonicalUserMsg) {
            // Aborted before any tokens arrived but after the server persisted
            // the user message (we received the `user_message` SSE event).
            // Drop only the empty assistant placeholder; keep the canonical user
            // row so the conversation history remains consistent.
            setMessages(prev =>
              prev
                .filter(m => m.id !== tempAssistantId)
                .map(m => (m.id === tempUserId ? canonicalUserMsg! : m)),
            )
          } else {
            // Aborted before the `user_message` SSE event arrived — the server
            // may not have persisted the message yet. Remove both optimistic
            // placeholders and restore the draft so the user can re-send.
            setMessages(prev =>
              prev.filter(m => m.id !== tempUserId && m.id !== tempAssistantId),
            )
            setInput(content)
          }
        } else {
          // Stream error: the backend has already persisted the user_message
          // before running Claude, so keep it visible (swap in the canonical
          // row if one arrived, otherwise leave the optimistic placeholder).
          // Only remove the assistant placeholder and restore the draft input
          // so the user can re-send without losing their message context.
          setMessages(prev =>
            prev
              .filter(m => m.id !== tempAssistantId)
              .map(m => (m.id === tempUserId && canonicalUserMsg ? canonicalUserMsg : m)),
          )
          setInput(content)
          if (err instanceof Error) setError(err.message || t('errors.streamError'))
          else setError(t('errors.streamError'))
        }
      } finally {
        // Once the reply settled out of view, the refetch on return reads the
        // complete persisted rows; nothing to merge. In view, a finished reply
        // is kept only if the final swap found the previous conversation still
        // on screen, for the refetch that is about to land.
        if (!isViewed() || !live.canonicalAssistant) forgetLive()
        if (streamAbortsRef.current.get(sentConversationId) === controller) {
          streamAbortsRef.current.delete(sentConversationId)
        }
        setStreamingId(current => (current === tempAssistantId ? null : current))
        removeSendingConversation(sentConversationId)
        if (isViewed()) focusInput?.()
      }
    },
    [
      activeConversation,
      sendingConversationIds,
      addSendingConversation,
      removeSendingConversation,
      setActiveConversation,
      setMessages,
      setConversations,
      deletedConversationIds,
      setInput,
      pinToBottom,
      focusInput,
      t,
    ],
  )

  return { send, stop, streamingId, error, clearError, reconcileLoaded }
}
