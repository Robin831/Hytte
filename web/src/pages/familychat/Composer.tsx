import { useCallback, useEffect, useRef, useState, type ChangeEvent, type FormEvent, type KeyboardEvent, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Send, Loader2, Paperclip, X, Mic, Trash2 } from 'lucide-react'
import type { ChatMessage } from './ChatView'
import { formatFileSize } from './utils'
import { uploadAttachment, UploadError } from './api'
import { useVoiceRecorder } from './voice/useVoiceRecorder'
import { trimLeadingTrailingSilence } from './voice/silenceTrim'

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

const VOICE_MAX_DURATION_MS = 30000

interface PendingAttachment {
  uploadId: string
  mime: string
  size: number
  name: string
}

function formatVoiceTime(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000))
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

export default function Composer({ conversationId, onMessageCreated }: ComposerProps) {
  const { t } = useTranslation('familyChat')
  const [body, setBody] = useState('')
  const [sending, setSending] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState('')
  const [attachment, setAttachment] = useState<PendingAttachment | null>(null)
  const [voiceSending, setVoiceSending] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  const uploadAbortRef = useRef<AbortController | null>(null)
  const voiceAbortRef = useRef<AbortController | null>(null)
  const pointerStartRef = useRef<{ id: number; y: number; mode: 'hold' | 'toggle' } | null>(null)

  const shipVoiceNoteRef = useRef<((blob: Blob, mimeType: string) => Promise<void>) | null>(null)
  const recorder = useVoiceRecorder({
    maxDurationMs: VOICE_MAX_DURATION_MS,
    onAutoComplete: (result) => {
      if (result) void shipVoiceNoteRef.current?.(result.blob, result.mimeType)
    },
  })
  const recorderRef = useRef(recorder)
  useEffect(() => { recorderRef.current = recorder })

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
    setVoiceSending(false)
    pointerStartRef.current = null
    textareaRef.current?.focus()
    // Drop any active recording when the user switches chats so the audio
    // doesn't end up posted to the wrong conversation.
    if (recorderRef.current.state === 'recording' || recorderRef.current.state === 'starting') {
      recorderRef.current.cancel()
    }
    return () => {
      abortRef.current?.abort()
      abortRef.current = null
      uploadAbortRef.current?.abort()
      uploadAbortRef.current = null
      voiceAbortRef.current?.abort()
      voiceAbortRef.current = null
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
    if (uploading || sending || voiceSending || recorder.state === 'recording') return
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
      const result = await uploadAttachment(conversationId, file, file.name, controller.signal)
      if (controller.signal.aborted) return
      setAttachment({
        uploadId: result.uploadId,
        mime: result.mime || file.type,
        size: result.size,
        name: file.name,
      })
    } catch (err) {
      if (controller.signal.aborted) return
      if (err instanceof Error && err.name === 'AbortError') return
      setError(mapUploadError(err))
    } finally {
      if (!controller.signal.aborted) setUploading(false)
      if (uploadAbortRef.current === controller) uploadAbortRef.current = null
    }
  }

  function mapUploadError(err: unknown): string {
    if (err instanceof UploadError) {
      if (err.status === 413) return t('composer.errors.fileTooLarge')
      if (err.serverCode === 'unsupported file type') return t('composer.errors.unsupportedType')
      if (err.serverCode) return err.serverCode
      return t('composer.errors.upload')
    }
    return err instanceof Error ? err.message : t('composer.errors.upload')
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

  // shipVoiceNote runs the trim → upload → send pipeline that fires after a
  // committed voice recording. Pulled out of the pointer handlers so the
  // desktop toggle path and the touch hold path share one code path.
  const shipVoiceNote = useCallback(async (blob: Blob, mimeType: string) => {
    const controller = new AbortController()
    voiceAbortRef.current?.abort()
    voiceAbortRef.current = controller
    const targetConversationId = conversationId
    setVoiceSending(true)
    setError('')
    try {
      const trimmed = await trimLeadingTrailingSilence(blob)
      if (controller.signal.aborted) return
      const effective = trimmed.size > 0 ? trimmed : blob
      const ext = mimeType.includes('ogg') ? 'ogg' : 'webm'
      const filename = `voice-note-${Date.now()}.${ext}`
      const upload = await uploadAttachment(targetConversationId, effective, filename, controller.signal)
      if (controller.signal.aborted) return
      const res = await fetch(`/api/familychat/conversations/${targetConversationId}/messages`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          body: '',
          attachment_path: upload.uploadId,
          attachment_mime: upload.mime || mimeType,
        }),
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
    } catch (err) {
      if (controller.signal.aborted) return
      if (err instanceof Error && err.name === 'AbortError') return
      if (err instanceof UploadError) {
        setError(mapUploadError(err))
      } else {
        setError(err instanceof Error ? err.message : t('composer.errors.send'))
      }
    } finally {
      if (!controller.signal.aborted) setVoiceSending(false)
      if (voiceAbortRef.current === controller) voiceAbortRef.current = null
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversationId, onMessageCreated, t])

  useEffect(() => { shipVoiceNoteRef.current = shipVoiceNote })

  const finishRecording = useCallback(async () => {
    const r = recorderRef.current
    if (r.cancelArmed) {
      r.cancel()
      r.resetPointer()
      return
    }
    r.resetPointer()
    const result = await r.stop()
    if (!result) return
    await shipVoiceNote(result.blob, result.mimeType)
  }, [shipVoiceNote])

  const handleMicPointerDown = useCallback((e: PointerEvent<HTMLButtonElement>) => {
    if (uploading || sending || voiceSending) return
    if (!recorder.supported) {
      setError(t('composer.errors.recorderUnsupported'))
      return
    }
    if (recorder.state === 'recording' || recorder.state === 'starting') {
      // Desktop toggle: a second click while recording finishes (or cancels)
      // the current take.
      void finishRecording()
      return
    }
    const isTouch = e.pointerType === 'touch' || e.pointerType === 'pen'
    pointerStartRef.current = {
      id: e.pointerId,
      y: e.clientY,
      mode: isTouch ? 'hold' : 'toggle',
    }
    try { e.currentTarget.setPointerCapture(e.pointerId) } catch { /* capture optional */ }
    void recorder.start()
  }, [finishRecording, recorder, sending, t, uploading, voiceSending])

  const handleMicPointerMove = useCallback((e: PointerEvent<HTMLButtonElement>) => {
    const start = pointerStartRef.current
    if (!start || start.id !== e.pointerId || start.mode !== 'hold') return
    recorder.setPointerDelta(e.clientY - start.y)
  }, [recorder])

  const handleMicPointerUp = useCallback((e: PointerEvent<HTMLButtonElement>) => {
    const start = pointerStartRef.current
    if (!start || start.id !== e.pointerId) return
    try { e.currentTarget.releasePointerCapture(e.pointerId) } catch { /* capture optional */ }
    if (start.mode === 'hold') {
      pointerStartRef.current = null
      if (recorder.state === 'recording' || recorder.state === 'starting') {
        void finishRecording()
      }
    } else {
      // Toggle mode: pointerDown above kicked off recording. The pointerUp
      // does nothing; the user clicks the dedicated stop/cancel buttons.
      pointerStartRef.current = null
    }
  }, [finishRecording, recorder])

  const handleMicPointerCancel = useCallback((e: PointerEvent<HTMLButtonElement>) => {
    const start = pointerStartRef.current
    if (!start || start.id !== e.pointerId) return
    try { e.currentTarget.releasePointerCapture(e.pointerId) } catch { /* capture optional */ }
    pointerStartRef.current = null
    if (start.mode === 'hold' && (recorder.state === 'recording' || recorder.state === 'starting')) {
      recorder.cancel()
      recorder.resetPointer()
    }
  }, [recorder])

  const cancelRecording = useCallback(() => {
    recorder.cancel()
    recorder.resetPointer()
    pointerStartRef.current = null
  }, [recorder])

  const stopRecording = useCallback(() => {
    void finishRecording()
  }, [finishRecording])

  const isRecording = recorder.state === 'recording' || recorder.state === 'starting' || recorder.state === 'processing'
  const disabled = sending || uploading || voiceSending || isRecording || (body.trim().length === 0 && !attachment)
  const micDisabled = sending || uploading || voiceSending || !recorder.supported
  const elapsedLabel = formatVoiceTime(recorder.elapsedMs)
  const remainingLabel = formatVoiceTime(recorder.remainingMs)

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
      {voiceSending && (
        <p className="text-xs text-gray-400" data-testid="family-chat-voice-sending">
          <Loader2 size={12} className="inline animate-spin mr-1" aria-hidden="true" />
          {t('composer.voice.sending')}
        </p>
      )}
      {isRecording ? (
        <div
          className="flex items-center gap-2"
          data-testid="family-chat-voice-recording"
          role="group"
          aria-label={t('composer.voice.recordingLabel')}
        >
          <button
            type="button"
            onClick={cancelRecording}
            aria-label={t('composer.voice.cancel')}
            title={t('composer.voice.cancel')}
            className={`flex items-center justify-center w-10 h-10 rounded-lg transition-colors cursor-pointer ${
              recorder.cancelArmed
                ? 'bg-red-600 text-white'
                : 'bg-gray-800 hover:bg-gray-700 text-gray-300'
            }`}
            data-testid="family-chat-voice-cancel"
          >
            <Trash2 size={18} aria-hidden="true" />
          </button>
          <div
            className="flex-1 flex items-center gap-2 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 min-h-10"
            aria-live="polite"
          >
            <span
              className="inline-flex h-2 w-2 rounded-full bg-red-500 animate-pulse shrink-0"
              aria-hidden="true"
            />
            <ul
              className="flex items-end gap-0.5 h-6 flex-1"
              aria-hidden="true"
              data-testid="family-chat-voice-meter"
            >
              {recorder.levels.map((level, idx) => (
                <li
                  key={idx}
                  className="flex-1 rounded-sm bg-blue-400 transition-[height] duration-75"
                  style={{ height: `${Math.max(8, Math.round(level * 100))}%` }}
                />
              ))}
            </ul>
            <span
              className="text-xs font-mono text-gray-200 shrink-0 tabular-nums"
              data-testid="family-chat-voice-elapsed"
            >
              {elapsedLabel}
            </span>
            <span
              className="text-[10px] text-gray-500 shrink-0 tabular-nums"
              aria-label={t('composer.voice.remaining', { time: remainingLabel })}
            >
              -{remainingLabel}
            </span>
          </div>
          <button
            type="button"
            onClick={stopRecording}
            aria-label={t('composer.voice.send')}
            title={t('composer.voice.send')}
            className="flex items-center justify-center w-10 h-10 rounded-lg bg-blue-600 hover:bg-blue-500 text-white transition-colors cursor-pointer"
            data-testid="family-chat-voice-stop"
          >
            <Send size={18} aria-hidden="true" />
          </button>
        </div>
      ) : (
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
            disabled={sending || uploading || voiceSending}
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
          <button
            type="button"
            onPointerDown={handleMicPointerDown}
            onPointerMove={handleMicPointerMove}
            onPointerUp={handleMicPointerUp}
            onPointerCancel={handleMicPointerCancel}
            disabled={micDisabled}
            aria-label={t('composer.voice.record')}
            title={t('composer.voice.start')}
            className="flex items-center justify-center w-10 h-10 rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-300 transition-colors disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer touch-none select-none"
            data-testid="family-chat-voice-mic"
          >
            <Mic size={18} aria-hidden="true" />
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
      )}
      {recorder.cancelArmed && isRecording && (
        <p
          className="text-xs text-red-300"
          role="status"
          data-testid="family-chat-voice-cancel-hint"
        >
          {t('composer.voice.releaseToCancel')}
        </p>
      )}
    </form>
  )
}
