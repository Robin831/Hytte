import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from 'react'
import { markConversationRead } from './api'
import type { ChatMessage, ReadReceiptPayload } from './useFamilyChatStream'

// useReadMarkers owns both directions of read state for one conversation:
// outbound — telling the server which message the local user has seen, and
// inbound — collecting the peers' receipts so the "Seen by …" line can render.
//
// A marker is only sent when the user can actually see the newest message: the
// tab is visible and the list is scrolled to the bottom. Hidden tabs and
// history readers keep their unread badge until they come back, which is what
// the visibility listener and the scroll hook-in are for.

// AT_BOTTOM_THRESHOLD_PX is how far from the very bottom of the message list
// still counts as "the user is looking at the newest message". A few dozen
// pixels of slack keeps a half-scrolled-by-a-hair list from being treated as
// reading history.
const AT_BOTTOM_THRESHOLD_PX = 80

// MARK_READ_DEBOUNCE_MS collapses a burst of arriving messages into a single
// read marker for the newest one, instead of one POST per message.
const MARK_READ_DEBOUNCE_MS = 300

interface UseReadMarkersOptions {
  conversationId: number | null
  messages: ChatMessage[]
  userId: number | undefined
  // memberLabel names the peers whose receipts cover our newest message.
  memberLabel: (id: number) => string
  // refreshConversations makes the sibling ConversationList refetch, which is
  // what actually clears the unread badge after the server accepts a marker.
  refreshConversations: () => void
  scrollRef: RefObject<HTMLDivElement | null>
}

export interface UseReadMarkersApi {
  // handleReadReceipt is wired into the SSE stream: it records a peer's marker.
  handleReadReceipt: (payload: ReadReceiptPayload) => void
  // maybeMarkRead is called on scroll (and internally on new messages / tab
  // focus) to send a marker when the newest message is genuinely visible.
  maybeMarkRead: () => void
  // resetForConversation drops the acknowledged watermark, any pending or
  // in-flight marker and the peers' receipts. Called when a chat is opened.
  resetForConversation: () => void
  // seenByLabels names the members whose live read receipt covers our newest
  // message. Receipts are observed in-session only (the conversation API
  // carries no per-member last_read_at), so this stays empty after a reload
  // until a peer reads something new.
  seenByLabels: string[]
}

export function useReadMarkers({
  conversationId,
  messages,
  userId,
  memberLabel,
  refreshConversations,
  scrollRef,
}: UseReadMarkersOptions): UseReadMarkersApi {
  // peerReads maps a member's user id to the timestamp of their most recent
  // read receipt. Live-only, so it starts empty on every load.
  const [peerReads, setPeerReads] = useState<Map<number, string>>(new Map())
  // lastAckedIdRef is the newest message id we have already told the server we
  // read, so re-renders and repeat effect runs don't re-POST.
  const lastAckedIdRef = useRef(0)
  // readAbortRef / readTimerRef own the in-flight read request and the pending
  // debounce for the current conversation; both are torn down on a switch.
  const readAbortRef = useRef<AbortController | null>(null)
  const readTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // handleReadReceipt records a peer's read marker. The stream already drops
  // our own echo, so anything arriving here belongs to another member.
  const handleReadReceipt = useCallback((payload: ReadReceiptPayload) => {
    setPeerReads(prev => {
      const next = new Map(prev)
      next.set(payload.user_id, payload.at)
      return next
    })
  }, [])

  const resetForConversation = useCallback(() => {
    lastAckedIdRef.current = 0
    if (readTimerRef.current !== null) {
      clearTimeout(readTimerRef.current)
      readTimerRef.current = null
    }
    readAbortRef.current?.abort()
    readAbortRef.current = null
    setPeerReads(new Map())
  }, [])

  // newestServerMessage is the last message carrying an authoritative id. An
  // optimistic bubble has no server id yet, so it can never anchor a read
  // marker; it reconciles a moment later and this picks it up then.
  const newestServerMessage = useMemo(() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (!messages[i].status) return messages[i]
    }
    return null
  }, [messages])

  // isAtBottom reports whether the message list is scrolled at (or within a
  // hair of) the newest message. A list that has never been scrolled — or one
  // shorter than the viewport — counts as at the bottom.
  const isAtBottom = useCallback(() => {
    const el = scrollRef.current
    if (!el) return true
    return el.scrollHeight - el.scrollTop - el.clientHeight <= AT_BOTTOM_THRESHOLD_PX
  }, [scrollRef])

  // postReadMarker sends the marker for `newestId` and refreshes the sibling
  // conversation list so the unread badge drops. Deduplicated against the last
  // acknowledged id, and best-effort: a failed or aborted request never shows an
  // error and never disturbs the stream.
  const postReadMarker = useCallback((newestId: number, at: string) => {
    if (conversationId === null) return
    if (newestId <= 0 || newestId <= lastAckedIdRef.current) return
    // Claim the watermark before the request so a burst of renders can't queue
    // several POSTs for the same id while the first is still in flight.
    lastAckedIdRef.current = newestId
    readAbortRef.current?.abort()
    const controller = new AbortController()
    readAbortRef.current = controller
    markConversationRead(conversationId, { at, signal: controller.signal })
      .then(() => {
        if (controller.signal.aborted) return
        refreshConversations()
      })
      .catch((err: unknown) => {
        // An abort means we switched conversations (or superseded this marker
        // with a newer one) — the watermark is reset by whoever aborted, so
        // leave it alone. A genuine failure rolls it back so the next arriving
        // message, scroll or tab focus retries.
        if (err instanceof Error && err.name === 'AbortError') return
        if (lastAckedIdRef.current === newestId) lastAckedIdRef.current = 0
      })
      .finally(() => {
        if (readAbortRef.current === controller) readAbortRef.current = null
      })
  }, [conversationId, refreshConversations])

  // maybeMarkRead marks the conversation read only when the user can actually
  // see the newest message: the tab is visible and the list is at the bottom.
  const maybeMarkRead = useCallback(() => {
    if (conversationId === null || !newestServerMessage) return
    if (newestServerMessage.id <= lastAckedIdRef.current) return
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return
    if (!isAtBottom()) return
    const { id, created_at } = newestServerMessage
    if (readTimerRef.current !== null) clearTimeout(readTimerRef.current)
    readTimerRef.current = setTimeout(() => {
      readTimerRef.current = null
      postReadMarker(id, created_at)
    }, MARK_READ_DEBOUNCE_MS)
  }, [conversationId, newestServerMessage, isAtBottom, postReadMarker])

  // Mark read on open and whenever a newer message lands (the callback's
  // identity changes with newestServerMessage, so this is exactly once per new
  // newest message).
  useEffect(() => {
    maybeMarkRead()
  }, [maybeMarkRead])

  // Returning to the tab is the other moment a deferred marker can be sent.
  // The ref keeps the listener registration independent of message arrivals.
  const maybeMarkReadRef = useRef(maybeMarkRead)
  useEffect(() => { maybeMarkReadRef.current = maybeMarkRead })
  useEffect(() => {
    if (typeof document === 'undefined') return
    const onVisibility = () => { maybeMarkReadRef.current() }
    document.addEventListener('visibilitychange', onVisibility)
    return () => document.removeEventListener('visibilitychange', onVisibility)
  }, [])

  // Drop any pending/in-flight marker when the conversation changes or the view
  // unmounts, so a late response can't refresh the list for a chat the user has
  // already left.
  useEffect(() => {
    return () => {
      if (readTimerRef.current !== null) {
        clearTimeout(readTimerRef.current)
        readTimerRef.current = null
      }
      readAbortRef.current?.abort()
      readAbortRef.current = null
      lastAckedIdRef.current = 0
    }
  }, [conversationId])

  const seenByLabels = useMemo(() => {
    if (peerReads.size === 0) return []
    let lastOwn: ChatMessage | null = null
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i]
      if (m.sender_user_id === userId && !m.status) { lastOwn = m; break }
    }
    if (!lastOwn) return []
    const sentAt = Date.parse(lastOwn.created_at)
    if (Number.isNaN(sentAt)) return []
    const labels: string[] = []
    for (const [id, at] of peerReads) {
      if (id === userId) continue
      const readAt = Date.parse(at)
      if (Number.isNaN(readAt) || readAt < sentAt) continue
      labels.push(memberLabel(id))
    }
    // Sorted so the list is stable regardless of receipt arrival order.
    return labels.sort((a, b) => a.localeCompare(b))
  }, [peerReads, messages, memberLabel, userId])

  return { handleReadReceipt, maybeMarkRead, resetForConversation, seenByLabels }
}
