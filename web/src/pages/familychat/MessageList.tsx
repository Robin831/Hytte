import { useLayoutEffect, useRef, type MutableRefObject, type ReactNode, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import { MessageSquare } from 'lucide-react'
import { Skeleton } from '../../components/ui/skeleton'
import MessageItem, { type Lightbox } from './MessageItem'
import { formatRelative } from './utils'
import type { ChatMessage } from './useFamilyChatStream'
import type { EditDraft } from './useMessageActions'

// MessageList owns the scrollable message log: the initial loading skeleton,
// the load error, the empty state, the older-history chips, the bubbles
// themselves, and the bottom scroll anchor with its stick-to-bottom behaviour.
//
// It is purely presentational. Paging (which page to fetch, when history has
// run out) and every message mutation stay with ChatView and useMessageActions;
// this component only reports scroll events upward and renders what it is told.
//
// Anything that belongs below the bubbles but above the bottom anchor —
// missed-call rows, the seen-by line, the typing indicator — is passed as
// children so the anchor stays the very last node in the scroll container.

// ScrollAnchor is the pair of scroll metrics captured immediately before older
// history is prepended, used to re-anchor the view on the message the user was
// already reading.
export interface ScrollAnchor {
  scrollHeight: number
  scrollTop: number
}

export interface MessageListProps {
  messages: ChatMessage[]
  loading: boolean
  error: string
  loadingOlder: boolean
  hasMoreOlder: boolean
  // scrollRef is owned by ChatView, which reads the element's scroll metrics to
  // decide when to page backward and when the newest message counts as read.
  scrollRef: RefObject<HTMLDivElement | null>
  onScroll: () => void
  // pendingScrollRestoreRef is set by ChatView just before a prepend; its
  // presence tells the stick-to-bottom effect below to stand down for that one
  // commit so the restore can re-anchor instead.
  pendingScrollRestoreRef: MutableRefObject<ScrollAnchor | null>
  // prependCounter ticks once per prepended page so the restore runs exactly
  // when older messages land, and never on a normal append.
  prependCounter: number
  // conversationId and keyboardInset are stick-to-bottom triggers only: opening
  // a chat and opening/closing the on-screen keyboard both have to re-snap the
  // view to the newest message.
  conversationId: number | null
  keyboardInset: number
  currentUserId: number | undefined
  // memberLabel resolves a user id to a friendly display name, falling back to
  // "Member #id" for ids the current user cannot name.
  memberLabel: (id: number) => string
  rtf: Intl.RelativeTimeFormat
  editDraft: EditDraft
  pickerForMsgId: number | null
  menuForMsgId: number | null
  onOpenLightbox: (lightbox: Lightbox) => void
  onOpenPicker: (msgId: number) => void
  onClosePicker: () => void
  onToggleReaction: (msgId: number, emoji: string, currentlyMine: boolean) => void
  onOpenMenu: (msgId: number | null) => void
  onBeginEdit: (msg: ChatMessage) => void
  onConfirmDelete: (msgId: number) => void
  onEditTextChange: (text: string) => void
  onSaveEdit: (msgId: number) => void
  onCancelEdit: () => void
  onRetry: (msg: ChatMessage) => void
  children?: ReactNode
}

export default function MessageList({
  messages,
  loading,
  error,
  loadingOlder,
  hasMoreOlder,
  scrollRef,
  onScroll,
  pendingScrollRestoreRef,
  prependCounter,
  conversationId,
  keyboardInset,
  currentUserId,
  memberLabel,
  rtf,
  editDraft,
  pickerForMsgId,
  menuForMsgId,
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
  children,
}: MessageListProps) {
  const { t } = useTranslation('familyChat')
  const messagesEndRef = useRef<HTMLDivElement>(null)

  // Auto-scroll to the bottom whenever the message list updates or the keyboard
  // opens/closes (keyboardInset changes). useLayoutEffect avoids a visible jump
  // between initial paint and the scroll snap.
  useLayoutEffect(() => {
    // A commit that prepended older history must not jump to the bottom — the
    // restore effect below re-anchors the view on the message the user was
    // already reading instead. messages.length changes on a prepend too, which
    // is why this guard exists rather than a narrower dependency list.
    if (pendingScrollRestoreRef.current) return
    if (messagesEndRef.current) {
      messagesEndRef.current.scrollIntoView({ block: 'end' })
    }
  }, [messages.length, conversationId, keyboardInset, pendingScrollRestoreRef])

  // Restore the scroll anchor after older messages are prepended: the content
  // above the viewport grew by (newScrollHeight - savedScrollHeight), so adding
  // that delta to the saved scrollTop keeps the same message under the user's
  // eyes. Runs after the auto-scroll effect above (declaration order) and
  // clears the flag that effect reads.
  useLayoutEffect(() => {
    const pending = pendingScrollRestoreRef.current
    if (!pending) return
    pendingScrollRestoreRef.current = null
    const el = scrollRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight - pending.scrollHeight + pending.scrollTop
  }, [prependCounter, pendingScrollRestoreRef, scrollRef])

  const showMessages = !loading && !error

  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      className="flex-1 min-h-0 overflow-y-auto overscroll-contain px-3 sm:px-4 py-3 space-y-2"
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

      {showMessages && messages.length === 0 && (
        <div className="flex flex-col items-center justify-center h-full text-center text-gray-500 py-12">
          <MessageSquare size={32} className="mb-2 text-gray-600" aria-hidden="true" />
          <p className="text-sm">{t('chat.emptyMessages')}</p>
        </div>
      )}

      {showMessages && messages.length > 0 && loadingOlder && (
        <div
          className="flex justify-center py-2 text-xs text-gray-500"
          role="status"
          aria-busy="true"
          data-testid="family-chat-loading-older"
        >
          {t('chat.loadingOlder')}
        </div>
      )}

      {showMessages && messages.length > 0 && !hasMoreOlder && !loadingOlder && (
        <div
          className="flex justify-center py-2 text-xs text-gray-500"
          data-testid="family-chat-history-start"
        >
          {t('chat.beginningOfConversation')}
        </div>
      )}

      {showMessages && messages.map(msg => {
        const deletedById = msg.deleted_by
        return (
          <MessageItem
            key={msg.client_id ?? msg.id}
            msg={msg}
            isOwn={currentUserId === msg.sender_user_id}
            senderLabel={memberLabel(msg.sender_user_id)}
            deletedByLabel={
              deletedById != null && currentUserId === deletedById
                ? t('edit.tombstoneSelf')
                : t('edit.tombstone', { name: memberLabel(deletedById ?? 0) })
            }
            relative={formatRelative(msg.created_at, rtf, t('time.justNow'))}
            editDraft={editDraft.msgId === msg.id ? editDraft : null}
            pickerOpen={pickerForMsgId === msg.id}
            menuOpen={menuForMsgId === msg.id}
            onOpenLightbox={onOpenLightbox}
            onOpenPicker={onOpenPicker}
            onClosePicker={onClosePicker}
            onToggleReaction={onToggleReaction}
            onOpenMenu={onOpenMenu}
            onBeginEdit={onBeginEdit}
            onConfirmDelete={onConfirmDelete}
            onEditTextChange={onEditTextChange}
            onSaveEdit={onSaveEdit}
            onCancelEdit={onCancelEdit}
            onRetry={onRetry}
          />
        )
      })}

      {children}

      <div ref={messagesEndRef} />
    </div>
  )
}
