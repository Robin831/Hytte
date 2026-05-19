import { useEffect, useRef, useState, type ChangeEvent, type FormEvent, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Send, Loader2, Paperclip, X } from 'lucide-react'
import type { ChatMessage } from './ChatView'
import { formatFileSize } from './utils'

interface ComposerProps {
  conversationId: number
  onMessageCreated: (msg: ChatMessage) => void
}

// Match the backend cap so we surface "too long" errors locally instead of
// round-tripping a 400. See maxBodyLen in internal/familychat/handlers.go.
const MAX_BODY_LEN = 8000

// Mirrors allowedAttachmentMimes in internal/familychat/attachments.go.
const ATTACHMENT_ACCEPT =
  'image/jpeg,image/png,image/webp,image/heic,image/heif,application/pdf,audio/mpeg,audio/mp4'

const MAX_ATTACHMENT_BYTES = 10 * 1024 * 1024

interface PendingAttachment {
  uploadId: string
  mime: string
  size: number
  name: string
}

export default function Composer({ conversationId, onMessageCreated }: ComposerProps) {
  const { t } = useTranslation('familyChat')
  const [body, setBody] = useState('')
  const [sending, setSending] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState('')
  const [attachment, setAttachment] = useState<PendingAttachment | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  const uploadAbortRef = useRef<AbortController | null>(null)

  // Clear draft + focus the textarea when the conversation changes so the
  // composer doesn't carry a half-typed message to a different chat. Also
  // abort any in-flight send so its response cannot leak the message into
  // the newly selected conversation. The cleanup also runs on unmount.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setBody('')
    setError('')
    setSending(false)
    setUploading(false)
    setAttachment(null)
    textareaRef.current?.focus()
    return () => {
      abortRef.current?.abort()
      abortRef.current = null
      uploadAbortRef.current?.abort()
      uploadAbortRef.current = null
    }
  }, [conversationId])

  // Auto-grow the textarea up to a max height; collapse back when emptied.
  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 160) + 'px'
  }, [body])

  function handleAttachClick() {
    if (uploading || sending) return
    fileInputRef.current?.click()
  }

  async function handleFileSelected(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    // Reset the input so the same file can be picked again after a remove.
    e.target.value = ''
    if (!file) return
    if (file.size > MAX_ATTACHMENT_BYTES) {
      setError(t('composer.errors.fileTooLarge'))
      return
    }
    const controller = new AbortController()
    uploadAbortRef.current?.abort()
    uploadAbortRef.current = controller
    setUploading(true)
    setError('')
    try {
      const form = new FormData()
      form.append('file', file)
      const res = await fetch(`/api/familychat/conversations/${conversationId}/upload`, {
        method: 'POST',
        credentials: 'include',
        body: form,
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        if (res.status === 413) {
          throw new Error(t('composer.errors.fileTooLarge'))
        }
        if (res.status === 400) {
          throw new Error(data?.error === 'unsupported file type'
            ? t('composer.errors.unsupportedType')
            : (data?.error || t('composer.errors.upload')))
        }
        throw new Error(data?.error || t('composer.errors.upload'))
      }
      const data = await res.json()
      if (controller.signal.aborted) return
      if (!data?.upload_id || typeof data.upload_id !== 'string') {
        throw new Error(t('composer.errors.upload'))
      }
      setAttachment({
        uploadId: data.upload_id,
        mime: typeof data.mime === 'string' ? data.mime : file.type,
        size: typeof data.size === 'number' ? data.size : file.size,
        name: file.name,
      })
    } catch (err) {
      if (controller.signal.aborted) return
      if (err instanceof Error && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('composer.errors.upload'))
    } finally {
      if (!controller.signal.aborted) setUploading(false)
      if (uploadAbortRef.current === controller) uploadAbortRef.current = null
    }
  }

  function removeAttachment() {
    uploadAbortRef.current?.abort()
    uploadAbortRef.current = null
    setAttachment(null)
    setError('')
  }

  async function submit() {
    const trimmed = body.trim()
    if (sending || uploading) return
    if (!trimmed && !attachment) return
    if ([...trimmed].length > MAX_BODY_LEN) {
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
      const payload: Record<string, string> = { body: trimmed }
      if (attachment) {
        payload.attachment_path = attachment.uploadId
        payload.attachment_mime = attachment.mime
      }
      const res = await fetch(`/api/familychat/conversations/${targetConversationId}/messages`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
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
      setAttachment(null)
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

  const disabled = sending || uploading || (body.trim().length === 0 && !attachment)

  return (
    <form
      onSubmit={handleSubmit}
      className="px-3 sm:px-4 py-2 flex flex-col gap-1.5"
      data-testid="family-chat-composer"
    >
      {error && (
        <p role="alert" className="text-xs text-red-400">{error}</p>
      )}
      {attachment && (
        <div
          className="flex items-center gap-2 text-xs bg-gray-800 border border-gray-700 rounded-lg px-2.5 py-1.5 self-start max-w-full"
          data-testid="family-chat-attachment-chip"
        >
          <Paperclip size={14} className="text-gray-400 shrink-0" aria-hidden="true" />
          <span className="text-gray-200 truncate">{attachment.name}</span>
          <span className="text-gray-500 shrink-0">{formatFileSize(attachment.size)}</span>
          <button
            type="button"
            onClick={removeAttachment}
            aria-label={t('composer.removeAttachment')}
            className="text-gray-400 hover:text-white shrink-0 cursor-pointer"
          >
            <X size={14} aria-hidden="true" />
          </button>
        </div>
      )}
      <div className="flex items-end gap-2">
        <input
          ref={fileInputRef}
          type="file"
          accept={ATTACHMENT_ACCEPT}
          className="hidden"
          onChange={handleFileSelected}
          aria-hidden="true"
          tabIndex={-1}
        />
        <button
          type="button"
          onClick={handleAttachClick}
          disabled={sending || uploading}
          aria-label={t('composer.attach')}
          title={t('composer.attach')}
          className="flex items-center justify-center w-10 h-10 rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-300 transition-colors disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
        >
          {uploading ? (
            <Loader2 size={18} className="animate-spin" aria-hidden="true" />
          ) : (
            <Paperclip size={18} aria-hidden="true" />
          )}
        </button>
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
