import { useCallback, useRef, useState } from 'react'
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
  /** The conversation a send targets and that the post-stream swap is gated on. */
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
  /** Aborts the in-flight stream (cancel button / conversation switch / unmount). */
  stop: () => void
  /** Id of the assistant placeholder currently streaming, or null. */
  streamingId: number | null
  /** Last send/stream error message, or '' when clear. */
  error: string
  clearError: () => void
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
  // Tracks the active streaming request so the user can cancel mid-send and so
  // a conversation switch can abort the in-flight stream.
  const streamAbortRef = useRef<AbortController | null>(null)

  const stop = useCallback(() => {
    streamAbortRef.current?.abort()
    streamAbortRef.current = null
  }, [])

  const send = useCallback(
    async (content: string) => {
      if (!content || !activeConversation || sendingConversationIds.has(activeConversation.id)) {
        return
      }
      const sentConversationId = activeConversation.id
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
      setMessages(prev => [...prev, tempUserMsg, tempAssistantMsg])
      setStreamingId(tempAssistantId)
      // Sending is a deliberate action: re-pin to the bottom so the user's new
      // message and the streamed reply stay in view, even if they had scrolled up.
      pinToBottom?.()

      // Abort any earlier in-flight stream before starting this one. The
      // conversation-switch effect in Chat also calls stop() on switches.
      streamAbortRef.current?.abort()
      const controller = new AbortController()
      streamAbortRef.current = controller

      // Accumulate streamed text in a local string so the placeholder row's
      // content reflects the full assistant reply on every render, even though
      // React batches the setState calls.
      let accumulated = ''
      const applyAccumulated = () => {
        const next = accumulated
        setMessages(prev =>
          prev.map(m => (m.id === tempAssistantId ? { ...m, content: next } : m)),
        )
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
          } else if (eventName === 'done') {
            canonicalAssistantMsg = (payload as { assistant_message?: Message }).assistant_message ?? null
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
        // server returned. Skip if the user navigated away from this
        // conversation while the stream was running.
        setActiveConversation(current => {
          if (current?.id !== sentConversationId) return current
          setMessages(prev => {
            // Drop the assistant placeholder if Claude returned an empty body
            // and we never saw a token; otherwise replace with canonical.
            if (!canonicalAssistantMsg && !sawToken) {
              return prev.filter(m => m.id !== tempUserId && m.id !== tempAssistantId)
            }
            return prev.flatMap(m => {
              if (m.id === tempUserId) {
                return canonicalUserMsg ? [canonicalUserMsg] : [m]
              }
              if (m.id === tempAssistantId) {
                return canonicalAssistantMsg ? [canonicalAssistantMsg] : [m]
              }
              return [m]
            })
          })
          return current
        })

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
        if (err instanceof DOMException && err.name === 'AbortError') {
          if (sawToken) {
            // Stop semantics: keep the partial assistant text on screen at
            // whatever was already streamed. The placeholder rows stay in
            // place as non-persisted local entries.
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
        if (streamAbortRef.current === controller) {
          streamAbortRef.current = null
        }
        setStreamingId(current => (current === tempAssistantId ? null : current))
        removeSendingConversation(sentConversationId)
        focusInput?.()
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

  return { send, stop, streamingId, error, clearError }
}
