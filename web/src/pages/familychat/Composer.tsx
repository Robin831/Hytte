import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Send, Loader2 } from 'lucide-react'
import type { ChatMessage } from './ChatView'

interface ComposerProps {
  conversationId: number
  onMessageCreated: (msg: ChatMessage) => void
}

// Match the backend cap so we surface "too long" errors locally instead of
// round-tripping a 400. See maxBodyLen in internal/familychat/handlers.go.
const MAX_BODY_LEN = 8000

export default function Composer({ conversationId, onMessageCreated }: ComposerProps) {
  const { t } = useTranslation('familyChat')
  const [body, setBody] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const abortRef = useRef<AbortController | null>(null)

  // Clear draft + focus the textarea when the conversation changes so the
  // composer doesn't carry a half-typed message to a different chat. Also
  // abort any in-flight send so its response cannot leak the message into
  // the newly selected conversation. The cleanup also runs on unmount.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setBody('')
    setError('')
    setSending(false)
    textareaRef.current?.focus()
    return () => {
      abortRef.current?.abort()
      abortRef.current = null
    }
  }, [conversationId])

  // Auto-grow the textarea up to a max height; collapse back when emptied.
  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 160) + 'px'
  }, [body])

  async function submit() {
    const trimmed = body.trim()
    if (!trimmed || sending) return
    if (trimmed.length > MAX_BODY_LEN) {
      setError(t('composer.errors.tooLong'))
      return
    }
    const controller = new AbortController()
    abortRef.current?.abort()
    abortRef.current = controller
    const targetConversationId = conversationId
    setSending(true)
    setError('')
    try {
      const res = await fetch(`/api/familychat/conversations/${targetConversationId}/messages`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: trimmed }),
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('composer.errors.send'))
      }
      const data = await res.json()
      if (controller.signal.aborted) return
      const msg = data?.message as ChatMessage | undefined
      if (!msg || typeof msg.id !== 'number') {
        throw new Error(t('composer.errors.send'))
      }
      onMessageCreated(msg)
      setBody('')
      textareaRef.current?.focus()
    } catch (err) {
      if (controller.signal.aborted) return
      if (err instanceof Error && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('composer.errors.send'))
    } finally {
      if (!controller.signal.aborted) setSending(false)
      if (abortRef.current === controller) abortRef.current = null
    }
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    submit()
  }

  function handleKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    // Enter sends; Shift+Enter inserts a newline. Matches the other Hytte
    // chat surfaces (see Chat.tsx, HomeworkChat.tsx).
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submit()
    }
  }

  const disabled = sending || body.trim().length === 0

  return (
    <form
      onSubmit={handleSubmit}
      className="px-3 sm:px-4 py-2 flex flex-col gap-1.5"
      data-testid="family-chat-composer"
    >
      {error && (
        <p role="alert" className="text-xs text-red-400">{error}</p>
      )}
      <div className="flex items-end gap-2">
        <label htmlFor="family-chat-composer-input" className="sr-only">
          {t('composer.placeholder')}
        </label>
        <textarea
          id="family-chat-composer-input"
          ref={textareaRef}
          value={body}
          onChange={e => setBody(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={t('composer.placeholder')}
          rows={1}
          disabled={sending}
          maxLength={MAX_BODY_LEN}
          className="flex-1 resize-none bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm placeholder:text-gray-500 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:opacity-60"
        />
        <button
          type="submit"
          disabled={disabled}
          aria-label={t('composer.send')}
          title={t('composer.send')}
          className="flex items-center justify-center w-10 h-10 rounded-lg bg-blue-600 hover:bg-blue-500 text-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
        >
          {sending ? (
            <Loader2 size={18} className="animate-spin" aria-hidden="true" />
          ) : (
            <Send size={18} aria-hidden="true" />
          )}
        </button>
      </div>
    </form>
  )
}
