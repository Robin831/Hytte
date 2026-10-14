import React, { memo, useEffect } from 'react'
import { Pencil, Send, Square, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { ChatPageComposerProps } from './types'

// Maximum auto-grow height of the textarea (matches the max-h-40 class).
const MAX_TEXTAREA_HEIGHT = 160

const TEXTAREA_STYLE: React.CSSProperties = { minHeight: '48px' }

function autoGrow(el: HTMLTextAreaElement) {
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, MAX_TEXTAREA_HEIGHT) + 'px'
}

// Error line plus the message input: auto-growing textarea, send / stop
// button and the "editing message" banner. The textarea ref is owned by the
// parent so it can move focus there (after a send, on edit, on new chat).
// Specific to the Chat page (pages/Chat.tsx); the shared composer used by
// TripChat and Stride's coach thread is ChatComposer in ChatThread.tsx.
function ChatPageComposer({
  hasConversation,
  value,
  onChange,
  onSend,
  onStop,
  sending,
  isEditing,
  onCancelEdit,
  error,
  onDismissError,
  textareaRef,
}: ChatPageComposerProps) {
  const { t } = useTranslation('chat')

  // Resize textarea when input changes (including programmatic clears)
  useEffect(() => {
    if (textareaRef.current) autoGrow(textareaRef.current)
  }, [value, textareaRef])

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Escape' && isEditing) {
      e.preventDefault()
      onCancelEdit()
      return
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      onSend()
    }
  }

  return (
    <>
      {/* Error banner */}
      {error && (
        <div className="px-4 py-2 bg-red-900/50 border-t border-red-800 text-red-300 text-sm flex items-center justify-between">
          <span>{error}</span>
          <button
            onClick={onDismissError}
            className="text-red-400 hover:text-red-300 cursor-pointer"
            aria-label={t('input.dismissError')}
          >
            <X size={14} />
          </button>
        </div>
      )}

      {/* Input area */}
      {hasConversation && (
        <div className="border-t border-gray-700 bg-gray-900 p-4">
          {isEditing && (
            <div className="max-w-3xl mx-auto mb-2 flex items-center justify-between gap-2 text-xs text-gray-400">
              <span className="flex items-center gap-1.5 min-w-0">
                <Pencil size={12} className="shrink-0" />
                <span className="truncate">{t('input.editingNotice')}</span>
              </span>
              <button
                onClick={onCancelEdit}
                className="shrink-0 flex items-center gap-1 px-2 py-1 rounded-lg text-gray-300 hover:text-white hover:bg-gray-800 transition-colors cursor-pointer"
                title={t('input.cancelEdit')}
                aria-label={t('input.cancelEdit')}
                data-testid="chat-cancel-edit-button"
              >
                <X size={12} />
                {t('input.cancelEdit')}
              </button>
            </div>
          )}
          <div className="max-w-3xl mx-auto flex gap-2">
            <textarea
              ref={textareaRef}
              value={value}
              onChange={e => onChange(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t('input.placeholder')}
              rows={1}
              className="flex-1 bg-gray-800 border border-gray-600 rounded-xl px-4 py-3 text-white text-sm resize-none focus:outline-none focus:ring-2 focus:ring-blue-500 placeholder-gray-500 max-h-40 overflow-y-auto"
              style={TEXTAREA_STYLE}
              disabled={sending}
              onInput={e => autoGrow(e.currentTarget)}
            />
            {sending ? (
              <button
                onClick={onStop}
                className="self-end p-3 rounded-xl bg-red-600 hover:bg-red-500 text-white transition-colors cursor-pointer shrink-0"
                title={t('input.stopStreaming')}
                aria-label={t('input.stopStreaming')}
                data-testid="chat-stop-button"
              >
                <Square size={18} />
              </button>
            ) : (
              <button
                onClick={onSend}
                disabled={!value.trim()}
                className="self-end p-3 rounded-xl bg-blue-600 hover:bg-blue-500 text-white transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
                title={t('input.sendLabel')}
                aria-label={t('input.sendLabel')}
                data-testid="chat-send-button"
              >
                <Send size={18} />
              </button>
            )}
          </div>
        </div>
      )}
    </>
  )
}

export default memo(ChatPageComposer)
