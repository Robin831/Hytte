import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { MessageSquare, X, PhoneIncoming, PhoneMissed } from 'lucide-react'
import { useAuth } from '../../auth'
import { useKeyboardInset } from '../../hooks/useKeyboardInset'
import CallOverlay from './CallOverlay'
import ChatHeader from './ChatHeader'
import Composer from './Composer'
import GroupCallOverlay from './GroupCallOverlay'
import type { Lightbox } from './MessageItem'
import MessageList from './MessageList'
import { useFamilyChat } from './FamilyChatContext'
import {
  useFamilyChatStream,
  type ChatMessage,
  type MissedCallEntry,
} from './useFamilyChatStream'
import { useChatMembers } from './useChatMembers'
import { useMessageActions } from './useMessageActions'
import { useOlderHistory } from './useOlderHistory'
import { useReadMarkers } from './useReadMarkers'
import * as voicePlayer from './voice/voicePlayer'
import { useVoiceCall, type CallKind } from './voice/useVoiceCall'
import { useGroupCall } from './voice/useGroupCall'

interface ChatViewProps {
  conversationId: number | null
  onBack: () => void
}

// ChatMessage is defined alongside the stream that produces it; re-exported
// here because the composer and bubble components import it from this module.
export type { ChatMessage }

// ChatView composes one conversation out of four hooks — the SSE stream, the
// message mutations, and the two call state machines — and the presentational
// pieces below them: the header, the message log, the composer, the dialogs and
// the call overlays. Everything with its own lifecycle (read markers, backward
// pagination, member name resolution) lives in a hook beside this file.
export default function ChatView({ conversationId, onBack }: ChatViewProps) {
  const { t, i18n } = useTranslation('familyChat')
  const { user, familyStatus } = useAuth()

  const [lightbox, setLightbox] = useState<Lightbox | null>(null)
  // pickerForMsgId is the id of the bubble whose reaction picker is open, or
  // null when nothing is open. We only show one picker at a time.
  const [pickerForMsgId, setPickerForMsgId] = useState<number | null>(null)
  // menuForMsgId is the id of the own-message bubble whose actions menu (edit /
  // delete) is open. Only one menu is open at a time.
  const [menuForMsgId, setMenuForMsgId] = useState<number | null>(null)
  // Inline-edit and delete-confirm state live in useMessageActions below; only
  // the focus bookkeeping for the confirmation modal stays here, since it is a
  // pure view concern.
  const deleteConfirmBtnRef = useRef<HTMLButtonElement>(null)
  const deletePrevFocusRef = useRef<Element | null>(null)
  // lastTypingSentRef throttles outbound typing POSTs to at most one per ~3s
  // while the local user is composing.
  const lastTypingSentRef = useRef(0)
  const messagesScrollRef = useRef<HTMLDivElement>(null)

  // refreshConversations makes the sibling ConversationList refetch, which is
  // what actually clears the unread badge after the server accepts our marker.
  const { refreshConversations } = useFamilyChat()

  // Voice-call state machine. skipSignalSubscription is set so the hook
  // doesn't open its own SSE stream — useFamilyChatStream below already owns
  // one for messages and reactions, and forwards call_* frames into
  // handleSignalEvent.
  const voiceCall = useVoiceCall({
    conversationId,
    userId: user?.id ?? null,
    skipSignalSubscription: true,
  })

  // Group-call mesh for conversations with 3+ members. Like voiceCall it does
  // not open its own SSE stream — the shared stream below routes call_* /
  // call_join / call_leave frames into it.
  const groupCall = useGroupCall({
    conversationId,
    userId: user?.id ?? null,
  })

  // The conversation's data feed: initial load, the SSE reader with reconnect +
  // gap-fill, connection status, typing indicators and missed-call synthesis.
  // Call signalling frames are forwarded to the two call hooks above rather than
  // handled inside the stream, so neither hook needs to know about transport.
  const {
    conversation,
    messages,
    setMessages,
    loading,
    error,
    connStatus,
    justReconnected,
    typingUsers,
    missedCalls,
    setMissedCalls,
  } = useFamilyChatStream({
    conversationId,
    userId: user?.id,
    callKind: voiceCall.callKind,
    onSignal: voiceCall.handleSignalEvent,
    onGroupSignal: groupCall.handleSignalEvent,
    // The stream keeps these callbacks in refs it refreshes every render, so
    // referencing the hooks declared below is safe: nothing here runs until
    // after the render that initialises them.
    onReadReceipt: payload => readMarkers.handleReadReceipt(payload),
    onConversationOpen: () => {
      lastTypingSentRef.current = 0
      // Read state and backward paging are both per-conversation: nothing is
      // cached across a switch, so every conversation starts from scratch.
      readMarkers.resetForConversation()
      olderHistory.resetForConversation()
    },
    onConversationClose: () => {
      // Stop any voice-note playback owned by this conversation. Switching
      // chats or unmounting must not leave a bubble in the prior conversation
      // continuing to play in the background.
      voicePlayer.stopAll()
    },
  })

  // Every mutation a message can undergo — optimistic send + reconcile, retry,
  // inline edit, soft delete and reaction toggles — lives in this hook. It
  // shares the stream's messages/setMessages pair so optimistic bubbles and
  // SSE-delivered rows reconcile through a single list.
  const {
    send,
    retry: retryFailedMessage,
    editDraft,
    setEditText,
    beginEdit,
    saveEdit,
    cancelEdit,
    deleteTarget,
    confirmDelete,
    cancelDelete,
    doDelete,
    toggleReaction,
  } = useMessageActions({
    conversationId,
    messages,
    setMessages,
    userId: user?.id,
  })

  // Friendly names + avatar emoji for everyone the current user can name.
  const { memberLabel, memberInfo, peerLabel, memberChips, selfEmoji } = useChatMembers({
    user,
    familyStatus,
    memberIds: conversation?.member_ids,
  })

  // Backward pagination and read markers both hang off the same scroll
  // container, so the scroll handler below drives them together.
  const olderHistory = useOlderHistory({
    conversationId,
    messages,
    setMessages,
    scrollRef: messagesScrollRef,
  })
  const readMarkers = useReadMarkers({
    conversationId,
    messages,
    userId: user?.id,
    memberLabel,
    refreshConversations,
    scrollRef: messagesScrollRef,
  })
  // voiceCallEndRef / groupCallLeaveRef let the conversationId-change cleanup
  // tear down any active call on the old conversation without re-subscribing on
  // every call-state change.
  const voiceCallEndRef = useRef(voiceCall.endCall)
  useEffect(() => {
    voiceCallEndRef.current = voiceCall.endCall
  })
  const groupCallLeaveRef = useRef(groupCall.leaveCall)
  useEffect(() => {
    groupCallLeaveRef.current = groupCall.leaveCall
  })
  // Tear down any active call when switching conversations so the mic and
  // RTCPeerConnection don't remain active against the old conversation.
  useEffect(() => {
    return () => {
      void voiceCallEndRef.current()
      void groupCallLeaveRef.current()
    }
  }, [conversationId])

  // Focus management + Escape handling for the delete confirmation modal,
  // matching the pattern used by ConfirmDialog.
  const confirmDeleteId = deleteTarget.msgId
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
      if (e.key === 'Escape') cancelDelete()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [confirmDeleteId, cancelDelete])

  const rtf = useMemo(
    () => new Intl.RelativeTimeFormat(i18n.language, { numeric: 'auto' }),
    [i18n.language],
  )

  const keyboardInset = useKeyboardInset()

  // Scroll drives both directions of paging state: near the top loads the
  // previous page, back at the bottom acknowledges the newest message.
  const handleMessagesScroll = () => {
    readMarkers.maybeMarkRead()
    olderHistory.handleScroll()
  }

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

  // notifyTyping fires from the composer on each keystroke; it throttles the
  // outbound signal to at most one POST per ~3s so a fast typist doesn't flood
  // the endpoint. Best-effort: a failed POST just means no indicator shows.
  const notifyTyping = useCallback(() => {
    if (conversationId === null) return
    const now = Date.now()
    if (now - lastTypingSentRef.current < 3000) return
    lastTypingSentRef.current = now
    void fetch(`/api/familychat/conversations/${conversationId}/typing`, {
      method: 'POST',
      credentials: 'include',
    }).catch(() => {})
  }, [conversationId])

  const closePicker = useCallback(() => setPickerForMsgId(null), [])

  // Resolve the friendly labels for everyone currently typing, excluding the
  // local user (defensive — recordTyping already skips own-id signals).
  const typingLabels = useMemo(() => {
    return Array.from(typingUsers.keys())
      .filter(id => id !== user?.id)
      .map(id => memberLabel(id))
  }, [typingUsers, memberLabel, user?.id])

  // 1:1 calls use the single-peer voice-call hook (callPeerId is the other
  // member). 3+ member conversations use the group-call mesh instead, gated by
  // canGroupCall. The two paths are mutually exclusive by member count.
  const callPeerId = useMemo<number | null>(() => {
    if (!conversation || user?.id === undefined) return null
    if (conversation.member_ids.length !== 2) return null
    return conversation.member_ids.find(id => id !== user.id) ?? null
  }, [conversation, user])
  const canCall = callPeerId !== null
  const canGroupCall = (conversation?.member_ids.length ?? 0) >= 3

  // startOrIgnoreCall fires the outgoing-call flow only when we're idle. A
  // second press while already ringing is a no-op so a double-tap can't kick
  // off two parallel sessions. Kind is plumbed through so the same handler
  // serves both the voice and video buttons in the header.
  const handleStartCall = useCallback((kind: CallKind = 'voice') => {
    if (!canCall) return
    if (voiceCall.state !== 'idle' && voiceCall.state !== 'ended') return
    void voiceCall.startCall(kind)
  }, [canCall, voiceCall])

  // Start a group call from the header buttons. A second press while already in
  // a call is a no-op (the hook guards re-entry).
  const handleStartGroupCall = useCallback((kind: CallKind = 'voice') => {
    if (!canGroupCall) return
    void groupCall.startCall(kind)
  }, [canGroupCall, groupCall])

  const handleCallBack = useCallback((entry: MissedCallEntry) => {
    // Dismiss the row first so a successful call doesn't leave an obsolete
    // missed-call entry behind in the message list.
    setMissedCalls(prev => prev.filter(m => m.callId !== entry.callId))
    // Use the kind from the original missed call so a video call-back
    // correctly starts as video, not a downgraded voice call.
    handleStartCall(entry.kind)
  }, [handleStartCall, setMissedCalls])

  const dismissMissedCall = useCallback((callId: string) => {
    setMissedCalls(prev => prev.filter(m => m.callId !== callId))
  }, [setMissedCalls])

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
      <ChatHeader
        title={conversation ? conversation.name : null}
        loading={loading}
        memberChips={memberChips}
        connStatus={connStatus}
        justReconnected={justReconnected}
        onBack={onBack}
        canCall={canCall}
        canGroupCall={canGroupCall}
        callBusy={voiceCall.state !== 'idle' && voiceCall.state !== 'ended'}
        groupCallActive={groupCall.state === 'active'}
        onStartCall={handleStartCall}
        onStartGroupCall={handleStartGroupCall}
      />

      <MessageList
        messages={messages}
        loading={loading}
        error={error}
        loadingOlder={olderHistory.loadingOlder}
        hasMoreOlder={olderHistory.hasMoreOlder}
        scrollRef={messagesScrollRef}
        onScroll={handleMessagesScroll}
        pendingScrollRestoreRef={olderHistory.pendingScrollRestoreRef}
        prependCounter={olderHistory.prependCounter}
        conversationId={conversationId}
        keyboardInset={keyboardInset}
        currentUserId={user?.id}
        memberLabel={memberLabel}
        rtf={rtf}
        editDraft={editDraft}
        pickerForMsgId={pickerForMsgId}
        menuForMsgId={menuForMsgId}
        onOpenLightbox={setLightbox}
        onOpenPicker={setPickerForMsgId}
        onClosePicker={closePicker}
        onToggleReaction={toggleReaction}
        onOpenMenu={setMenuForMsgId}
        onBeginEdit={beginEdit}
        onConfirmDelete={confirmDelete}
        onEditTextChange={setEditText}
        onSaveEdit={saveEdit}
        onCancelEdit={cancelEdit}
        onRetry={retryFailedMessage}
      >
        {!loading && !error && missedCalls.map(entry => (
          <div
            key={`missed-call-${entry.callId}`}
            className="flex justify-center"
            data-testid={`missed-call-${entry.callId}`}
          >
            <div className="inline-flex items-center gap-2 px-3 py-2 rounded-2xl bg-red-900/30 border border-red-700/50 text-red-200 text-xs">
              <PhoneMissed size={14} aria-hidden="true" />
              <span>{t('call.missedFrom', { name: peerLabel(entry.fromUserId) })}</span>
              {canCall && (
                <button
                  type="button"
                  onClick={() => handleCallBack(entry)}
                  className="px-2 py-0.5 rounded-md bg-red-800/50 hover:bg-red-700/60 text-red-100 text-[11px] font-medium cursor-pointer"
                  data-testid={`missed-call-back-${entry.callId}`}
                >
                  {t('call.callBack')}
                </button>
              )}
              <button
                type="button"
                onClick={() => dismissMissedCall(entry.callId)}
                aria-label={t('call.dismiss')}
                title={t('call.dismiss')}
                className="p-0.5 text-red-300 hover:text-red-100 cursor-pointer"
                data-testid={`missed-call-dismiss-${entry.callId}`}
              >
                <X size={12} aria-hidden="true" />
              </button>
            </div>
          </div>
        ))}

        {readMarkers.seenByLabels.length > 0 && (
          <div
            className="flex justify-end px-1 text-[11px] text-gray-500"
            data-testid="family-chat-seen-by"
          >
            {t('chat.seenBy', { names: readMarkers.seenByLabels.join(', ') })}
          </div>
        )}

        {typingLabels.length > 0 && (
          <div
            className="flex items-center px-1 text-xs text-gray-400 italic"
            role="status"
            aria-live="polite"
            data-testid="family-chat-typing-indicator"
          >
            {typingLabels.length === 1
              ? t('chat.typing.single', { name: typingLabels[0] })
              : t('chat.typing.multiple', { count: typingLabels.length })}
          </div>
        )}
      </MessageList>

      <div className="border-t border-gray-800 bg-gray-950 shrink-0">
        {user && (
          <Composer
            conversationId={conversationId}
            currentUserId={user.id}
            {...send}
            onTyping={notifyTyping}
          />
        )}
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
          onClick={(e) => { if (e.target === e.currentTarget) cancelDelete() }}
          data-testid="chat-delete-confirm"
        >
          <div className="bg-gray-900 border border-gray-700 rounded-lg max-w-md w-full p-4 shadow-xl">
            <p id="family-chat-confirm-delete-title" className="text-sm text-gray-100">
              {t('edit.confirmDelete')}
            </p>
            {deleteTarget.error && (
              <p className="mt-2 text-xs text-red-400">{deleteTarget.error}</p>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button
                type="button"
                onClick={cancelDelete}
                className="px-3 py-1.5 text-sm rounded-md bg-gray-800 text-gray-200 hover:bg-gray-700"
                data-testid="chat-delete-cancel"
              >
                {t('edit.cancel')}
              </button>
              <button
                ref={deleteConfirmBtnRef}
                type="button"
                onClick={() => { void doDelete(confirmDeleteId) }}
                className="px-3 py-1.5 text-sm rounded-md bg-red-600 text-white hover:bg-red-500"
                data-testid="chat-delete-confirm-button"
              >
                {t('edit.delete')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Group-call (3+ member) mesh UI. The incoming banner lets an idle
          member join an in-progress group call; the overlay is the active
          in-call grid. */}
      {canGroupCall && groupCall.state === 'idle' && groupCall.incomingCall && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label={t('call.group.title')}
          className="fixed inset-x-0 top-0 z-50 flex items-center gap-3 px-4 py-3 bg-blue-900/95 text-white shadow-lg"
          data-testid="family-chat-group-incoming"
        >
          <PhoneIncoming size={20} aria-hidden="true" className="text-blue-200 shrink-0" />
          <span className="flex-1 min-w-0 text-sm truncate">
            {groupCall.incomingCall.kind === 'video'
              ? t('call.group.incomingVideo', { name: peerLabel(groupCall.incomingCall.fromUserId) })
              : t('call.group.incoming', { name: peerLabel(groupCall.incomingCall.fromUserId) })}
          </span>
          <button
            type="button"
            onClick={() => { void groupCall.joinCall() }}
            className="shrink-0 px-3 py-1.5 rounded-full bg-green-600 hover:bg-green-500 text-sm font-medium cursor-pointer"
            data-testid="family-chat-group-join"
          >
            {t('call.group.join')}
          </button>
          <button
            type="button"
            onClick={() => groupCall.declineCall()}
            aria-label={t('call.group.decline')}
            className="shrink-0 p-1.5 rounded-full hover:bg-white/10 cursor-pointer"
          >
            <X size={18} aria-hidden="true" />
          </button>
        </div>
      )}

      <GroupCallOverlay
        call={groupCall}
        memberLabel={memberLabel}
        memberInfo={memberInfo}
        selfEmoji={selfEmoji}
      />

      <CallOverlay
        call={voiceCall}
        conversationId={conversationId}
        peerLabel={peerLabel}
        fallbackPeerId={callPeerId}
      />
    </div>
  )
}
