import { useState, useEffect, useLayoutEffect, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useChatStream } from '../hooks/useChatStream'
import { useIdSet } from '../hooks/useIdSet'
import type { Conversation, Message } from '../hooks/useChatStream'
import ChatHeader from '../components/chat/ChatHeader'
import ConversationSidebar from '../components/chat/ConversationSidebar'
import ChatPageComposer from '../components/chat/ChatPageComposer'
import JumpToLatest from '../components/chat/JumpToLatest'
import ChatPageMessageList from '../components/chat/ChatPageMessageList'
import { DEFAULT_MODEL } from '../components/chat/ModelSelect'
import * as chatApi from './chatApi'

// Distance from the bottom (px) within which the messages view is considered
// "pinned": new streamed tokens keep the view glued to the latest message.
// Past this, the user has scrolled up to read history and we leave them be.
const PIN_THRESHOLD = 80

export default function Chat() {
  const { t } = useTranslation('chat')
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [activeConversation, setActiveConversation] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [localError, setLocalError] = useState('')
  const [loadingConversations, setLoadingConversations] = useState(true)
  const [loadingMessages, setLoadingMessages] = useState(false)
  // Track every conversation with an in-flight send so concurrent sends in
  // different conversations don't clobber each other's pending state.
  const [sendingConversationIds, addSendingConversation, removeSendingConversation] = useIdSet()
  const [showSidebar, setShowSidebar] = useState(true)
  // Model chosen for the next new conversation (used when none is active).
  // Empty means "let the backend pick" (falls back to user's configured model).
  const [newConversationModel, setNewConversationModel] = useState<string>('')
  // Id of the user message currently being re-edited in the composer. The
  // message row is already gone (truncation happened server-side when Edit was
  // clicked); this only drives the "editing" affordance above the textarea.
  const [editingMessageId, setEditingMessageId] = useState<number | null>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  // Whether the messages view is glued to the bottom. `isPinned` drives the
  // "jump to latest" button; `isPinnedRef` mirrors it so the streaming-token
  // scroll effect reads the latest value without re-subscribing.
  const [isPinned, setIsPinned] = useState(true)
  const isPinnedRef = useRef(true)
  // Set when the next scroll-to-bottom should jump instantly (no smooth
  // animation) — used on conversation switch / initial load so the button
  // does not flash while a long history animates into view.
  const instantScrollRef = useRef(false)
  // Suppresses unpin detection in the scroll handler while a smooth
  // jump-to-latest scroll is in flight — cleared once the container
  // actually reaches the pinned zone.
  const jumpingRef = useRef(false)
  // Track locally deleted conversation IDs so we don't resurrect them if a
  // send response arrives after the user deleted the conversation mid-flight.
  const deletedConversationIds = useRef<Set<number>>(new Set())
  const modelUpdateSeqRef = useRef<Map<number, number>>(new Map())

  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    messagesEndRef.current?.scrollIntoView({ behavior })
  }, [])

  const jumpToLatest = useCallback(() => {
    jumpingRef.current = true
    isPinnedRef.current = true
    setIsPinned(true)
    scrollToBottom('smooth')
  }, [scrollToBottom])

  // Re-pin the messages view to the bottom — used when a send begins so the
  // user's new message and the streamed reply stay in view.
  const pinToBottom = useCallback(() => {
    isPinnedRef.current = true
    setIsPinned(true)
  }, [])

  const focusInput = useCallback(() => {
    inputRef.current?.focus()
  }, [])

  // SSE streaming state machine: optimistic placeholders, frame parsing, the
  // three error-recovery branches, abort handling, and the post-stream
  // conversation-list refetch all live in the hook. Chat keeps only UI state.
  const { send, stop, error, clearError, reconcileLoaded } = useChatStream({
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
  })

  // Latest render's state for the event handlers below. Reading it through a
  // ref keeps those handlers' identities stable, so the memoized message rows,
  // sidebar and composer are not re-rendered by every streamed token.
  const latestRef = useRef({ activeConversation, sendingConversationIds, messages, input, send })
  useLayoutEffect(() => {
    latestRef.current = { activeConversation, sendingConversationIds, messages, input, send }
  })

  // Keep the view glued to the latest message only while the user is pinned
  // near the bottom. If they have scrolled up to read history, leave their
  // position untouched so streamed tokens no longer yank them back down.
  useEffect(() => {
    if (isPinnedRef.current) {
      scrollToBottom(instantScrollRef.current ? 'auto' : 'smooth')
    }
    instantScrollRef.current = false
  }, [messages, scrollToBottom])

  // Track how far the messages container is from the bottom so we know whether
  // to auto-scroll and whether to show the "jump to latest" button.
  useEffect(() => {
    const el = scrollContainerRef.current
    if (!el) return
    const handleScroll = () => {
      const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
      const pinned = distanceFromBottom <= PIN_THRESHOLD
      if (jumpingRef.current) {
        if (pinned) {
          jumpingRef.current = false
        } else {
          return
        }
      }
      if (pinned !== isPinnedRef.current) {
        isPinnedRef.current = pinned
        setIsPinned(pinned)
      }
    }
    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [])

  // On conversation switch, re-pin to the bottom and scroll instantly so the
  // latest message is shown without animating a long history.
  useEffect(() => {
    isPinnedRef.current = true
    setIsPinned(true) // eslint-disable-line react-hooks/set-state-in-effect -- resetting derived state on key change
    instantScrollRef.current = true
  }, [activeConversation?.id])

  // Load conversations
  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        setConversations(await chatApi.listConversations(controller.signal, t('errors.failedToLoad')))
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setLocalError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoadingConversations(false)
      }
    })()
    return () => { controller.abort() }
  }, [t])

  // Load messages when conversation changes
  const activeConversationId = activeConversation?.id ?? null
  useEffect(() => {
    // Switching conversations deliberately does NOT abort an in-flight reply:
    // the stream keeps running in the background (the hook only applies its
    // tokens while its conversation is on screen) and the server persists the
    // reply, so returning here shows it via the refetch below.
    clearError()

    const controller = new AbortController()
    ;(async () => {
      if (activeConversationId === null) {
        setMessages([])
        setLoadingMessages(false)
        setLocalError('')
        return
      }
      setLoadingMessages(true)
      try {
        const loaded = await chatApi.listMessages(activeConversationId, controller.signal, t('errors.failedToLoadMessages'))
        // Merge in a reply still streaming for this conversation, so a refetch
        // that raced the stream neither duplicates nor drops it.
        setMessages(reconcileLoaded(activeConversationId, loaded))
        setLocalError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setLocalError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoadingMessages(false)
      }
    })()
    return () => { controller.abort() }
  }, [activeConversationId, t, clearError, reconcileLoaded])

  // Abort any in-flight streaming send when the component unmounts so the
  // background fetch does not keep running with a stale state ref.
  useEffect(() => {
    return () => { stop() }
  }, [stop])

  // Focus the composer when an edit begins and put the caret at the end so the
  // user can amend the recalled message straight away.
  useEffect(() => {
    if (editingMessageId === null) return
    const el = inputRef.current
    if (!el) return
    el.focus()
    el.setSelectionRange(el.value.length, el.value.length)
  }, [editingMessageId])

  const createConversation = useCallback(async () => {
    try {
      const conv = await chatApi.createConversation(newConversationModel, t('errors.failedToCreate'))
      setConversations(prev => [conv, ...prev])
      setActiveConversation(conv)
      setShowSidebar(false)
      setEditingMessageId(null)
      setLocalError('')
      inputRef.current?.focus()
    } catch (err) {
      if (err instanceof Error) setLocalError(err.message)
    }
  }, [newConversationModel, t])

  const updateModel = useCallback(async (id: number, model: string) => {
    setLocalError('')
    const prevModel = latestRef.current.activeConversation?.model
    if (model === prevModel) return
    const seqMap = modelUpdateSeqRef.current
    const seq = (seqMap.get(id) ?? 0) + 1
    seqMap.set(id, seq)
    setActiveConversation(current => (current && current.id === id ? { ...current, model } : current))
    setConversations(prev => prev.map(c => (c.id === id ? { ...c, model } : c)))
    try {
      const updated = await chatApi.updateConversation(id, { model }, t('errors.failedToUpdateModel'))
      if (seqMap.get(id) !== seq) return
      setConversations(prev => prev.map(c => (c.id === id ? updated : c)))
      setActiveConversation(current => (current?.id === id ? updated : current))
    } catch (err) {
      if (seqMap.get(id) === seq && prevModel !== undefined) {
        setActiveConversation(current => (current && current.id === id ? { ...current, model: prevModel } : current))
        setConversations(prev => prev.map(c => (c.id === id ? { ...c, model: prevModel } : c)))
      }
      if (err instanceof Error) setLocalError(err.message)
    }
  }, [t])

  // Resolves true once deleted so the sidebar can close its confirmation.
  const deleteConversation = useCallback(async (id: number): Promise<boolean> => {
    setLocalError('')
    try {
      await chatApi.deleteConversation(id, t('errors.failedToDelete'))
      // A reply may still be streaming in the background for this conversation.
      stop(id)
      deletedConversationIds.current.add(id)
      setConversations(prev => prev.filter(c => c.id !== id))
      if (latestRef.current.activeConversation?.id === id) {
        setActiveConversation(null)
        setMessages([])
        setEditingMessageId(null)
      }
      setLocalError('')
      return true
    } catch (err) {
      if (err instanceof Error) setLocalError(err.message)
      return false
    }
  }, [t, stop])

  // Resolves true when the rename settled (or the title was blank) so the
  // sidebar can leave rename mode; false keeps the editor open after an error.
  const renameConversation = useCallback(async (id: number, title: string): Promise<boolean> => {
    if (!title.trim()) return true
    setLocalError('')
    try {
      const updated = await chatApi.updateConversation(id, { title: title.trim() }, t('errors.failedToRename'))
      setConversations(prev => prev.map(c => c.id === id ? updated : c))
      if (latestRef.current.activeConversation?.id === id) {
        setActiveConversation(updated)
      }
      setLocalError('')
      return true
    } catch (err) {
      if (err instanceof Error) setLocalError(err.message)
      return false
    }
  }, [t])

  // Deletes `messageId` and every later message in the conversation, both
  // server-side and in local state, and returns the deleted message so the
  // caller can re-send (Regenerate) or recall (Edit) its content. Truncating
  // also clears the conversation's Claude CLI session so the next turn starts
  // fresh instead of resuming context that references the removed messages.
  const truncateFrom = useCallback(async (conversationId: number, messageId: number): Promise<Message> => {
    const removed = await chatApi.truncateMessages(conversationId, messageId, t('errors.failedToTruncate'))
    setMessages(prev => {
      const idx = prev.findIndex(m => m.id === messageId)
      return idx === -1 ? prev : prev.slice(0, idx)
    })
    return removed
  }, [t])

  // Regenerate: discard this reply and the user turn that produced it, then
  // re-send that same text so a fresh reply streams in its place.
  const handleRegenerate = useCallback(async (assistantMsg: Message) => {
    const { activeConversation, sendingConversationIds, messages, send } = latestRef.current
    if (!activeConversation || sendingConversationIds.has(activeConversation.id)) return
    const idx = messages.findIndex(m => m.id === assistantMsg.id)
    if (idx < 0) return
    // Walk back to the user turn this reply answered.
    const userMsg = messages.slice(0, idx).reverse().find(m => m.role === 'user')
    // Optimistic rows (negative ids) aren't persisted yet, so they can't be truncated.
    if (!userMsg || userMsg.id < 0) return

    clearError()
    setLocalError('')
    try {
      const removed = await truncateFrom(activeConversation.id, userMsg.id)
      setEditingMessageId(null)
      await send(removed.content)
    } catch (err) {
      if (err instanceof Error) setLocalError(err.message)
    }
  }, [clearError, truncateFrom])

  // Edit: drop this user message and everything after it, then recall its text
  // into the composer so the user can amend and resend.
  const handleEdit = useCallback(async (userMsg: Message) => {
    const { activeConversation, sendingConversationIds } = latestRef.current
    if (!activeConversation || sendingConversationIds.has(activeConversation.id)) return
    if (userMsg.id < 0) return

    clearError()
    setLocalError('')
    try {
      const removed = await truncateFrom(activeConversation.id, userMsg.id)
      setInput(removed.content)
      setEditingMessageId(removed.id)
    } catch (err) {
      if (err instanceof Error) setLocalError(err.message)
    }
  }, [clearError, truncateFrom])

  // Cancelling an edit only leaves the editing mode — the truncated messages
  // stay deleted, matching what the server already persisted.
  const cancelEdit = useCallback(() => {
    setEditingMessageId(null)
    setInput('')
    inputRef.current?.focus()
  }, [])

  // Thin wrapper around the hook's send: the trimmed input is the message
  // content; the hook owns the optimistic rows, streaming, and reconciliation.
  const handleSend = useCallback(() => {
    const { send, input } = latestRef.current
    setEditingMessageId(null)
    send(input.trim())
  }, [])

  // Stops only the on-screen conversation's stream; replies running in the
  // background for other conversations keep going.
  const stopActive = useCallback(() => {
    const id = latestRef.current.activeConversation?.id
    if (id !== undefined) stop(id)
  }, [stop])

  const selectConversation = useCallback((conv: Conversation) => {
    setActiveConversation(conv)
    setShowSidebar(false)
    setEditingMessageId(null)
    clearError()
    setLocalError('')
  }, [clearError])

  const dismissError = useCallback(() => {
    clearError()
    setLocalError('')
  }, [clearError])

  const showConversationList = useCallback(() => setShowSidebar(true), [])

  const onModelChange = useCallback((model: string) => {
    const current = latestRef.current.activeConversation
    if (current) {
      updateModel(current.id, model)
    } else {
      setNewConversationModel(model)
    }
  }, [updateModel])

  const selectedModel = activeConversation ? activeConversation.model : (newConversationModel || DEFAULT_MODEL)
  // True while the active conversation has a stream in flight — disables the
  // model selector and the per-message Regenerate/Edit actions.
  const isSending = activeConversation != null && sendingConversationIds.has(activeConversation.id)

  return (
    <div className="flex h-[calc(100vh-3.5rem)] md:h-screen">
      <ConversationSidebar
        visibleOnMobile={showSidebar}
        conversations={conversations}
        activeId={activeConversationId}
        loading={loadingConversations}
        onSelect={selectConversation}
        onNew={createConversation}
        onRename={renameConversation}
        onDelete={deleteConversation}
      />

      {/* Main chat area — hidden on mobile while the conversation list is open */}
      <div className={`${showSidebar ? 'hidden' : 'flex'} md:flex flex-col flex-1 min-w-0`}>
        <ChatHeader
          title={activeConversation ? activeConversation.title : null}
          model={selectedModel}
          modelDisabled={isSending}
          onModelChange={onModelChange}
          onBack={showConversationList}
        />

        {/* Messages area */}
        <div className="relative flex-1 min-h-0">
        <div ref={scrollContainerRef} className="h-full overflow-y-auto">
          <ChatPageMessageList
            messages={messages}
            hasConversation={activeConversation !== null}
            loading={loadingMessages}
            sending={isSending}
            onNew={createConversation}
            onRegenerate={handleRegenerate}
            onEdit={handleEdit}
            endRef={messagesEndRef}
          />
        </div>
          {/* Jump to latest — shown only while scrolled away from the bottom */}
          {activeConversation && messages.length > 0 && !isPinned && <JumpToLatest onClick={jumpToLatest} />}
        </div>

        <ChatPageComposer
          hasConversation={activeConversation !== null}
          value={input}
          onChange={setInput}
          onSend={handleSend}
          onStop={stopActive}
          sending={isSending}
          isEditing={editingMessageId !== null}
          onCancelEdit={cancelEdit}
          error={error || localError}
          onDismissError={dismissError}
          textareaRef={inputRef}
        />
      </div>
    </div>
  )
}
