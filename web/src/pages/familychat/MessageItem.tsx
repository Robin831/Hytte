import { memo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Download, MoreVertical, Smile } from 'lucide-react'
import ReactionChips from './ReactionChips'
import ReactionPicker from './ReactionPicker'
import VoiceBubble from './voice/VoiceBubble'
import { readCachedWaveform, parseWaveformJSON, DEFAULT_BAR_COUNT, type Waveform } from './voice/waveform'
import type { ChatMessage } from './useFamilyChatStream'
import type { EditDraft } from './useMessageActions'

// MessageItem renders exactly one bubble: attachments (image / voice note /
// audio / generic download), the message body, the inline edit form, the
// tombstone for a soft-deleted message, reaction chips plus the picker anchor,
// the own-message action menu, and the sending/failed/edited/time footer.
//
// It is purely presentational — every mutation arrives as a prop from
// useMessageActions via ChatView. The only state it owns is view-local
// bookkeeping that belongs to this bubble alone: which pointer type opened the
// context menu, and the two elements ReactionPicker positions itself against.

// Lightbox is the image preview request emitted when an image attachment is
// tapped. The overlay itself lives in ChatView.
export interface Lightbox {
  url: string
  alt: string
}

export interface MessageItemProps {
  msg: ChatMessage
  // isOwn drives the whole own/peer split: bubble colour and side, whether the
  // sender label renders, and whether edit/delete are offered at all.
  isOwn: boolean
  senderLabel: string
  // deletedByLabel is the pre-resolved tombstone text ("You deleted this" vs
  // "<name> deleted this"); only rendered when the message is soft-deleted.
  deletedByLabel: string
  // relative is the already-formatted relative timestamp, or '' to hide it.
  relative: string
  // editDraft is non-null only while this bubble is the one being edited, so
  // keystrokes in one bubble don't re-render every other bubble.
  editDraft: EditDraft | null
  pickerOpen: boolean
  menuOpen: boolean
  onOpenLightbox: (lightbox: Lightbox) => void
  onOpenPicker: (msgId: number) => void
  onClosePicker: () => void
  onToggleReaction: (msgId: number, emoji: string, currentlyMine: boolean) => void
  // onOpenMenu takes null to close the currently open action menu.
  onOpenMenu: (msgId: number | null) => void
  onBeginEdit: (msg: ChatMessage) => void
  onConfirmDelete: (msgId: number) => void
  onEditTextChange: (text: string) => void
  onSaveEdit: (msgId: number) => void
  onCancelEdit: () => void
  onRetry: (msg: ChatMessage) => void
}

// parseVoiceMeta extracts a {bars, durationMs} pair from a meta_json blob.
// Returns null when the field is absent, unparseable, or shaped wrong — the
// caller falls back to a localStorage cache (and ultimately a flat waveform)
// so a malformed meta_json never blocks playback.
function parseVoiceMeta(meta: string | null | undefined): Waveform | null {
  if (!meta) return null
  return parseWaveformJSON(meta)
}

function MessageItem({
  msg,
  isOwn,
  senderLabel,
  deletedByLabel,
  relative,
  editDraft,
  pickerOpen,
  menuOpen,
  onOpenLightbox,
  onOpenPicker,
  onClosePicker,
  onToggleReaction,
  onOpenMenu,
  onBeginEdit,
  onConfirmDelete,
  onEditTextChange,
  onSaveEdit,
  onCancelEdit,
  onRetry,
}: MessageItemProps) {
  const { t } = useTranslation('familyChat')

  // lastPointerTypeRef is set by onPointerDown so onContextMenu knows whether
  // it was triggered by a touch long-press (open picker, suppress native menu)
  // or a mouse right-click (leave native menu alone; picker is on hover button).
  const lastPointerTypeRef = useRef<string>('mouse')
  // pickerAnchorRef: element used by ReactionPicker for placement/positioning.
  // Set to the hover button or the message bubble (long-press), whichever opened the picker.
  const pickerAnchorRef = useRef<HTMLElement | null>(null)
  // pickerGuardRef: the actual toggle button (hover Smile button only). The
  // picker's outside-click handler ignores clicks on this element so the button
  // can toggle the picker closed without the picker immediately re-closing on
  // the same click. NOT set on long-press (no toggle button exists there), so
  // tapping the bubble correctly closes the picker.
  const pickerGuardRef = useRef<HTMLElement | null>(null)

  const isDeleted = !!msg.deleted_at
  const isEditing = editDraft !== null
  const attachmentUrl = !isDeleted && msg.attachment_path && msg.attachment_mime
    ? `/api/familychat/conversations/${msg.conversation_id}/attachments/${msg.id}`
    : ''
  const mime = msg.attachment_mime ?? ''
  const isImage = mime.startsWith('image/')
  const isAudio = mime.startsWith('audio/')
  // A voice note is an audio/webm attachment with an empty body — the recorder
  // always ships these as standalone bubbles. The bubble renders a precomputed
  // waveform if meta_json carries one; it falls back to a localStorage cache
  // (written immediately after upload by the recorder) and finally to a flat
  // waveform.
  const isVoiceNote = !isDeleted && !!attachmentUrl
    && (mime.startsWith('audio/webm') || mime.startsWith('audio/ogg'))
    && !msg.body.trim()
  const cachedWaveform = isVoiceNote
    ? (parseVoiceMeta(msg.meta_json) ?? readCachedWaveform(msg.id))
    : null
  const voiceBars = cachedWaveform?.bars ?? new Array(DEFAULT_BAR_COUNT).fill(0)
  const voiceDurationMs = cachedWaveform?.durationMs ?? 0
  // Optimistic bubbles (still sending or failed) have no authoritative id yet,
  // so reactions and edit/delete are suppressed until the row reconciles to the
  // persisted message.
  const isPending = msg.status === 'sending' || msg.status === 'failed'
  const showActions = isOwn && !isDeleted && !isEditing && !isPending

  return (
    <div
      className={`flex flex-col group ${isOwn ? 'items-end' : 'items-start'}`}
      data-testid={`chat-bubble-${msg.id}`}
    >
      {!isOwn && !isDeleted && (
        <span className="text-xs text-gray-400 mb-0.5 px-1">{senderLabel}</span>
      )}
      <div className="relative max-w-[85%] sm:max-w-[70%]">
        {isDeleted ? (
          <div
            className="px-3 py-2 rounded-2xl text-sm italic bg-gray-800/60 border border-gray-700 text-gray-400"
            data-testid={`chat-tombstone-${msg.id}`}
          >
            {deletedByLabel}
          </div>
        ) : editDraft ? (
          <div className={`px-3 py-2 rounded-2xl text-sm break-words ${
            isOwn ? 'bg-blue-600/40 border border-blue-500' : 'bg-gray-800 border border-gray-700'
          }`}>
            <textarea
              value={editDraft.text}
              onChange={(e) => onEditTextChange(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Escape') {
                  e.preventDefault()
                  onCancelEdit()
                } else if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  onSaveEdit(msg.id)
                }
              }}
              aria-label={t('edit.edit')}
              data-testid={`chat-edit-input-${msg.id}`}
              className="w-full bg-gray-900 text-gray-100 border border-gray-700 rounded-lg px-2 py-1 text-sm focus:outline-none focus:border-blue-500"
              rows={3}
              autoFocus
            />
            {editDraft.error && (
              <div className="text-xs text-red-400 mt-1">{editDraft.error}</div>
            )}
            <div className="flex gap-2 mt-2 justify-end">
              <button
                type="button"
                onClick={onCancelEdit}
                className="px-2 py-1 text-xs rounded-md bg-gray-700 text-gray-200 hover:bg-gray-600"
                data-testid={`chat-edit-cancel-${msg.id}`}
              >
                {t('edit.cancel')}
              </button>
              <button
                type="button"
                onClick={() => onSaveEdit(msg.id)}
                disabled={editDraft.saving || !editDraft.text.trim()}
                className="px-2 py-1 text-xs rounded-md bg-blue-600 text-white hover:bg-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                data-testid={`chat-edit-save-${msg.id}`}
              >
                {editDraft.saving ? t('edit.saving') : t('edit.save')}
              </button>
            </div>
          </div>
        ) : (
          <div
            className={`px-3 py-2 rounded-2xl text-sm break-words ${
              isOwn
                ? 'bg-blue-600 text-white rounded-br-sm'
                : 'bg-gray-800 text-gray-100 rounded-bl-sm'
            } ${msg.status === 'sending' ? 'opacity-70' : ''}`}
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
                // Use the bubble as the positioning anchor only.
                // pickerGuardRef is explicitly cleared here — there is
                // no toggle button for long-press, so clicks on the
                // bubble should correctly dismiss the picker. Clearing
                // prevents a stale guard ref from a prior hover-button
                // open from suppressing the outside-click close.
                pickerAnchorRef.current = e.currentTarget
                pickerGuardRef.current = null
                onOpenPicker(msg.id)
              }
            }}
          >
            {attachmentUrl && isImage && (
              <button
                type="button"
                onClick={() => onOpenLightbox({ url: attachmentUrl, alt: t('chat.attachmentImageAlt') })}
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
            {attachmentUrl && isVoiceNote && (
              <VoiceBubble
                messageId={msg.id}
                src={attachmentUrl}
                bars={voiceBars}
                durationMs={voiceDurationMs}
                isOwn={isOwn}
              />
            )}
            {attachmentUrl && isAudio && !isVoiceNote && (
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
        {!isDeleted && !isEditing && !isPending && (
          <button
            type="button"
            onClick={(e) => {
              const willOpen = !pickerOpen
              if (willOpen) {
                pickerAnchorRef.current = e.currentTarget
                pickerGuardRef.current = e.currentTarget
                onOpenPicker(msg.id)
              } else {
                onClosePicker()
              }
            }}
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
            onClick={() => onOpenMenu(menuOpen ? null : msg.id)}
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
              onClick={() => onOpenMenu(null)}
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
                onClick={() => { onOpenMenu(null); onBeginEdit(msg) }}
                className="w-full text-left px-3 py-2 text-sm text-gray-200 hover:bg-gray-700"
                data-testid={`chat-edit-action-${msg.id}`}
              >
                {t('edit.edit')}
              </button>
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  onOpenMenu(null)
                  onConfirmDelete(msg.id)
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
            onPick={(emoji) => {
              onClosePicker()
              onToggleReaction(msg.id, emoji, !!msg.reactions?.[emoji]?.me)
            }}
            onClose={onClosePicker}
            anchorRef={pickerAnchorRef}
            triggerRef={pickerGuardRef}
          />
        )}
      </div>
      <ReactionChips
        reactions={msg.reactions}
        onToggle={(emoji, mine) => onToggleReaction(msg.id, emoji, mine)}
      />
      <div className="flex items-center gap-1 mt-0.5 px-1">
        {msg.status === 'sending' && (
          <span
            className="text-[10px] text-gray-400 italic"
            role="status"
            data-testid={`chat-sending-${msg.id}`}
          >
            {t('composer.sending')}
          </span>
        )}
        {msg.status === 'failed' && (
          <button
            type="button"
            onClick={() => onRetry(msg)}
            className="text-[10px] text-red-400 hover:text-red-300 italic cursor-pointer"
            data-testid={`chat-failed-${msg.id}`}
          >
            {t('composer.failedRetry')}
          </button>
        )}
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
}

// Memoized: a chat can hold hundreds of bubbles, and every arriving message,
// keystroke in the composer or reaction toggle re-renders the list. Every
// handler prop is stable (useCallback / useMessageActions), and editDraft is
// null for all but the bubble actually being edited, so this cuts the re-render
// down to the messages that genuinely changed.
export default memo(MessageItem)
