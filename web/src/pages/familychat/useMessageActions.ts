import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type MutableRefObject,
  type SetStateAction,
} from 'react'
import { useTranslation } from 'react-i18next'
import {
  addReaction,
  removeReaction,
  applyReactionEvent,
  editMessage,
  deleteMessage,
} from './api'
import { reconcileMessage, type ChatMessage } from './useFamilyChatStream'
// Type-only import: erased at compile time, so this does not create a runtime
// import cycle with Composer (which imports ChatMessage back out of ChatView).
import type { RetryHandle } from './Composer'

// useMessageActions owns every mutation a message can undergo inside one
// conversation: the optimistic-send lifecycle (insert → reconcile → fail →
// retry), the inline edit draft, the soft-delete confirm flow, and reaction
// toggles.
//
// It deliberately does not own the message list. It operates on the
// `messages` / `setMessages` pair produced by useFamilyChatStream so optimistic
// entries and SSE-delivered entries reconcile through a single list — two lists
// would need a merge step, and the reconcile-by-client_id trick only works when
// the optimistic bubble and the authoritative row live in the same array.
//
// The POST for a text send still lives in Composer, which also owns the
// attachment and voice-note upload wiring. This hook supplies the callbacks
// Composer reports progress through (`send`) plus the retry entry point the
// failed bubble taps.

export interface UseMessageActionsOptions {
  // conversationId is the open conversation, or null when none is selected.
  // Every mutation is a no-op while it is null.
  conversationId: number | null
  messages: ChatMessage[]
  setMessages: Dispatch<SetStateAction<ChatMessage[]>>
  // userId is the signed-in user's id, used to stamp optimistic reaction
  // updates with the correct `me` flag.
  userId: number | undefined
}

// EditDraft is the inline editor's whole state: which bubble is open, the
// in-progress text, whether a save is in flight, and the last save failure
// (kept so the user can retry without losing what they typed).
export interface EditDraft {
  msgId: number | null
  text: string
  saving: boolean
  error: string
}

// DeleteTarget is the soft-delete confirmation modal's state: which message it
// is asking about, and the last failure to show inside the modal.
export interface DeleteTarget {
  msgId: number | null
  error: string
}

// ComposerSendHandlers is shaped to spread straight into <Composer {...send} />.
export interface ComposerSendHandlers {
  onMessageCreated: (msg: ChatMessage) => void
  onOptimisticMessage: (msg: ChatMessage) => void
  onMessageFailed: (clientId: string) => void
  retryRef: MutableRefObject<RetryHandle | null>
}

export interface UseMessageActions {
  send: ComposerSendHandlers
  // retry re-POSTs a failed optimistic bubble under its original client_id.
  retry: (msg: ChatMessage) => void
  editDraft: EditDraft
  setEditText: (text: string) => void
  beginEdit: (msg: ChatMessage) => void
  saveEdit: (msgId: number) => Promise<void>
  cancelEdit: () => void
  deleteTarget: DeleteTarget
  // confirmDelete opens the confirmation modal; doDelete performs the delete.
  confirmDelete: (msgId: number) => void
  cancelDelete: () => void
  doDelete: (msgId?: number) => Promise<void>
  toggleReaction: (msgId: number, emoji: string, currentlyMine: boolean) => Promise<void>
  // pendingIds holds the ids of messages with a server mutation in flight
  // (edit save, delete, or reaction toggle), so bubbles can disable their own
  // action affordances while one is outstanding.
  pendingIds: Set<number>
}

const EMPTY_EDIT_DRAFT: EditDraft = { msgId: null, text: '', saving: false, error: '' }
const EMPTY_DELETE_TARGET: DeleteTarget = { msgId: null, error: '' }

export function useMessageActions({
  conversationId,
  messages,
  setMessages,
  userId,
}: UseMessageActionsOptions): UseMessageActions {
  const { t } = useTranslation('familyChat')

  const [editDraft, setEditDraft] = useState<EditDraft>(EMPTY_EDIT_DRAFT)
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>(EMPTY_DELETE_TARGET)
  const [pendingIds, setPendingIds] = useState<Set<number>>(() => new Set())

  // messagesRef mirrors the list so the mutation callbacks can read a
  // pre-change snapshot (for rollback) without taking `messages` as a
  // dependency — otherwise every callback would change identity on each
  // arriving message and defeat memoization in the bubble components.
  const messagesRef = useRef(messages)
  useEffect(() => { messagesRef.current = messages })

  // editDraftRef does the same for the in-progress edit text, keeping saveEdit
  // stable across keystrokes.
  const editDraftRef = useRef(editDraft)
  useEffect(() => { editDraftRef.current = editDraft })

  // pendingCountsRef ref-counts in-flight mutations per message id: a bubble
  // can have two reaction toggles outstanding at once, and the first one to
  // finish must not clear the pending flag for the second.
  const pendingCountsRef = useRef<Map<number, number>>(new Map())

  const beginPending = useCallback((msgId: number) => {
    const counts = pendingCountsRef.current
    counts.set(msgId, (counts.get(msgId) ?? 0) + 1)
    setPendingIds(new Set(counts.keys()))
  }, [])

  const endPending = useCallback((msgId: number) => {
    const counts = pendingCountsRef.current
    const remaining = (counts.get(msgId) ?? 0) - 1
    if (remaining > 0) counts.set(msgId, remaining)
    else counts.delete(msgId)
    setPendingIds(new Set(counts.keys()))
  }, [])

  // Editing, deleting and pending markers are all per-conversation: a switch
  // must not carry a half-typed edit or an open delete confirmation into a
  // different chat. Guarded on the previous id so this does nothing on mount.
  const prevConversationRef = useRef(conversationId)
  useEffect(() => {
    if (prevConversationRef.current === conversationId) return
    prevConversationRef.current = conversationId
    pendingCountsRef.current.clear()
    /* eslint-disable react-hooks/set-state-in-effect -- per-conversation state must be dropped on a switch; it cannot be derived from the new id */
    setEditDraft(EMPTY_EDIT_DRAFT)
    setDeleteTarget(EMPTY_DELETE_TARGET)
    setPendingIds(new Set())
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [conversationId])

  // ── Send lifecycle ─────────────────────────────────────────────────────────

  // onMessageCreated folds the authoritative row (from the POST response or an
  // SSE frame) into the list. Reconciles against an optimistic bubble when the
  // message carries a client_id (the text-send path); otherwise dedupes by id
  // (voice notes and attachments, which are not rendered optimistically).
  const onMessageCreated = useCallback((msg: ChatMessage) => {
    // Defensive: if the user switched conversations while a send was in
    // flight, drop the message rather than leaking it into the wrong chat.
    if (msg.conversation_id !== conversationId) return
    setMessages(prev => reconcileMessage(prev, msg.client_id, msg))
  }, [conversationId, setMessages])

  // onOptimisticMessage renders a just-typed text message immediately in a
  // 'sending' state, before any network round-trip. Scoped to the active
  // conversation so a stray emit can't leak into the wrong chat.
  const onOptimisticMessage = useCallback((msg: ChatMessage) => {
    if (msg.conversation_id !== conversationId) return
    setMessages(prev => [...prev, msg])
  }, [conversationId, setMessages])

  // onMessageFailed flips an optimistic bubble to the 'failed' state when its
  // POST errors, preserving the typed text so the user can tap to retry. No
  // conversation guard is needed: after a switch the bubble is gone, so the
  // client_id no longer matches and this is a no-op.
  const onMessageFailed = useCallback((clientId: string) => {
    setMessages(prev => prev.map(m =>
      m.client_id === clientId ? { ...m, status: 'failed' as const } : m,
    ))
  }, [setMessages])

  // retryRef holds Composer's retry entry point. The failed bubble's tap target
  // calls it to re-POST the preserved text under the same client_id so a
  // successful retry reconciles the existing bubble in place.
  const retryRef = useRef<RetryHandle | null>(null)

  const retry = useCallback((msg: ChatMessage) => {
    if (!msg.client_id) return
    const clientId = msg.client_id
    // Flip back to 'sending' immediately so the affordance reflects the retry.
    setMessages(prev => prev.map(m =>
      m.client_id === clientId ? { ...m, status: 'sending' as const } : m,
    ))
    retryRef.current?.(clientId, msg.body, msg.conversation_id)
  }, [setMessages])

  // send bundles the Composer wiring so callers can spread it in one place.
  const send = useMemo<ComposerSendHandlers>(() => ({
    onMessageCreated,
    onOptimisticMessage,
    onMessageFailed,
    retryRef,
  }), [onMessageCreated, onOptimisticMessage, onMessageFailed])

  // ── Reactions ──────────────────────────────────────────────────────────────

  // toggleReaction applies the change optimistically (chips update before the
  // network round-trip) and rolls back on failure. The eventual SSE
  // confirmation overwrites the optimistic state with the server-authoritative
  // count, which keeps two clients in sync even if either one races.
  const toggleReaction = useCallback(async (
    msgId: number,
    emoji: string,
    currentlyMine: boolean,
  ) => {
    if (conversationId === null || userId === undefined) return
    const meID = userId
    const snapshot = messagesRef.current.find(m => m.id === msgId) ?? null
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
    beginPending(msgId)
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
    } finally {
      endPending(msgId)
    }
  }, [conversationId, userId, setMessages, beginPending, endPending])

  // ── Inline edit ────────────────────────────────────────────────────────────

  // beginEdit opens the inline editor for a bubble, seeding the draft from the
  // current body so the user starts from what is on screen.
  const beginEdit = useCallback((msg: ChatMessage) => {
    setEditDraft({ msgId: msg.id, text: msg.body, saving: false, error: '' })
  }, [])

  const setEditText = useCallback((text: string) => {
    setEditDraft(prev => ({ ...prev, text }))
  }, [])

  const cancelEdit = useCallback(() => {
    setEditDraft(EMPTY_EDIT_DRAFT)
  }, [])

  const saveEdit = useCallback(async (msgId: number) => {
    if (conversationId === null) return
    const trimmed = editDraftRef.current.text.trim()
    if (!trimmed) {
      setEditDraft(prev => ({ ...prev, error: t('edit.saveError') }))
      return
    }
    setEditDraft(prev => ({ ...prev, saving: true, error: '' }))
    // Capture the pre-edit body/edited_at so a failed save can revert the
    // optimistic update — otherwise the bubble would keep showing the unsaved
    // draft as if it had been persisted.
    const snapshot = messagesRef.current.find(m => m.id === msgId) ?? null
    // Optimistic update first: the SSE confirmation will overwrite shortly with
    // the server's authoritative edited_at, which matches the pattern used by
    // reactions and message sends.
    const optimisticTime = new Date().toISOString()
    setMessages(prev => prev.map(m =>
      m.id === msgId ? { ...m, body: trimmed, edited_at: optimisticTime } : m,
    ))
    beginPending(msgId)
    try {
      const updated = await editMessage(conversationId, msgId, trimmed)
      setMessages(prev => prev.map(m =>
        m.id === msgId
          ? { ...m, body: updated.body, edited_at: updated.edited_at }
          : m,
      ))
      // Only close the editor if it is still the one we saved: the user may
      // have cancelled or opened another bubble while the PATCH was in flight,
      // and clobbering that would discard a draft they are actively typing.
      setEditDraft(prev => (prev.msgId === msgId ? EMPTY_EDIT_DRAFT : prev))
    } catch {
      if (snapshot) {
        setMessages(prev => prev.map(m =>
          m.id === msgId
            ? { ...m, body: snapshot.body, edited_at: snapshot.edited_at ?? null }
            : m,
        ))
      }
      setEditDraft(prev => (
        prev.msgId === msgId
          ? { ...prev, saving: false, error: t('edit.saveError') }
          : prev
      ))
    } finally {
      endPending(msgId)
    }
  }, [conversationId, t, setMessages, beginPending, endPending])

  // ── Soft delete ────────────────────────────────────────────────────────────

  const confirmDelete = useCallback((msgId: number) => {
    setDeleteTarget({ msgId, error: '' })
  }, [])

  const cancelDelete = useCallback(() => {
    setDeleteTarget(EMPTY_DELETE_TARGET)
  }, [])

  // deleteTargetRef keeps doDelete stable while still letting it default to
  // whatever message the confirmation modal is currently asking about.
  const deleteTargetRef = useRef(deleteTarget)
  useEffect(() => { deleteTargetRef.current = deleteTarget })

  const doDelete = useCallback(async (msgId?: number) => {
    if (conversationId === null) return
    const targetId = msgId ?? deleteTargetRef.current.msgId
    if (targetId === null) return
    setDeleteTarget(prev => ({ ...prev, error: '' }))
    const meID = userId ?? null
    const now = new Date().toISOString()
    // Capture the pre-delete snapshot from the current list so the rollback in
    // catch{} doesn't depend on the setState updater having run.
    const snapshot = messagesRef.current.find(m => m.id === targetId) ?? null
    setMessages(prev => prev.map(m => {
      if (m.id !== targetId) return m
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
    beginPending(targetId)
    try {
      await deleteMessage(conversationId, targetId)
      // Only dismiss the confirm modal after the server has accepted the
      // delete — closing it earlier would hide the error message rendered
      // inside the same modal if the request fails. Guarded on the target so a
      // late response cannot close a modal that has since moved to another
      // message.
      setDeleteTarget(prev => (prev.msgId === targetId ? EMPTY_DELETE_TARGET : prev))
    } catch {
      if (snapshot) {
        setMessages(prev => prev.map(m => (m.id === targetId ? snapshot : m)))
      }
      setDeleteTarget(prev => (
        prev.msgId === targetId ? { ...prev, error: t('edit.deleteError') } : prev
      ))
    } finally {
      endPending(targetId)
    }
  }, [conversationId, userId, t, setMessages, beginPending, endPending])

  return {
    send,
    retry,
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
    pendingIds,
  }
}
