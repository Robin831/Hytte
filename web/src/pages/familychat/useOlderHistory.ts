import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type Dispatch,
  type MutableRefObject,
  type RefObject,
  type SetStateAction,
} from 'react'
import { fetchOlderMessages, OLDER_PAGE_SIZE } from './api'
import type { ScrollAnchor } from './MessageList'
import type { ChatMessage } from './useFamilyChatStream'

// useOlderHistory owns backward pagination of the message log: fetching the
// page before the oldest rendered message, prepending it without duplicating
// anything the live stream already delivered, and capturing the scroll metrics
// MessageList needs to re-anchor the view on the message the user was reading.
//
// Nothing is cached across a conversation switch — every conversation starts
// out assuming there is more to load and discovers otherwise when a short page
// comes back.

// OLDER_SCROLL_THRESHOLD_PX is how close to the top of the message list the
// user has to scroll before the previous page starts loading. Wide enough that
// the page usually lands before the user hits the very top, small enough that
// it doesn't fire on an idle list that merely isn't scrolled to the bottom.
const OLDER_SCROLL_THRESHOLD_PX = 150

interface UseOlderHistoryOptions {
  conversationId: number | null
  messages: ChatMessage[]
  setMessages: Dispatch<SetStateAction<ChatMessage[]>>
  scrollRef: RefObject<HTMLDivElement | null>
}

export interface UseOlderHistoryApi {
  loadingOlder: boolean
  hasMoreOlder: boolean
  // prependCounter ticks once per prepended page so MessageList's scroll-anchor
  // restore runs exactly when older messages land, and never on a normal append.
  prependCounter: number
  // pendingScrollRestoreRef holds the scroll metrics captured immediately
  // before a prepend. Its presence is also what tells MessageList's
  // auto-scroll-to-bottom effect to stand down for that one commit.
  pendingScrollRestoreRef: MutableRefObject<ScrollAnchor | null>
  // handleScroll loads the previous page when the user nears the top. Safe to
  // call on every scroll event — it is a no-op away from the threshold, while a
  // page is in flight, and once the start of the conversation is reached.
  handleScroll: () => void
  // resetForConversation clears the paging state when a chat is opened.
  resetForConversation: () => void
}

export function useOlderHistory({
  conversationId,
  messages,
  setMessages,
  scrollRef,
}: UseOlderHistoryOptions): UseOlderHistoryApi {
  // loadingOlder is true while a backward page is in flight; hasMoreOlder stays
  // true until a page comes back short (or empty), which is the server's way of
  // saying "that was the start of the conversation".
  const [loadingOlder, setLoadingOlder] = useState(false)
  const [hasMoreOlder, setHasMoreOlder] = useState(true)
  const [prependCounter, setPrependCounter] = useState(0)
  // loadingOlderRef mirrors loadingOlder synchronously: scroll events fire far
  // faster than React commits, so the state flag alone would let several
  // duplicate pages start before the first render lands.
  const loadingOlderRef = useRef(false)
  const pendingScrollRestoreRef = useRef<ScrollAnchor | null>(null)

  // conversationIdRef shadows the current conversation so an in-flight
  // "load older" response can tell whether the user has since switched chats.
  const conversationIdRef = useRef(conversationId)
  useEffect(() => {
    conversationIdRef.current = conversationId
  })

  const resetForConversation = useCallback(() => {
    setLoadingOlder(false)
    setHasMoreOlder(true)
    loadingOlderRef.current = false
    pendingScrollRestoreRef.current = null
  }, [])

  // loadOlderMessages fetches the page immediately preceding the oldest message
  // currently rendered and prepends it. Deduped by id so it can never collide
  // with a message the live stream (or a gap-fill) already delivered, and a
  // short page flips hasMoreOlder off so the terminator renders and no further
  // requests are made.
  const loadOlderMessages = useCallback(async () => {
    if (conversationId === null) return
    if (loadingOlderRef.current || !hasMoreOlder) return
    const oldest = messages[0]
    // An optimistic bubble carries no server id yet, so it can't anchor a
    // backward page. It is always appended at the end, so this only skips the
    // degenerate case where the list holds nothing else.
    if (!oldest || oldest.status) return

    loadingOlderRef.current = true
    setLoadingOlder(true)
    try {
      const page = await fetchOlderMessages(conversationId, oldest.id, OLDER_PAGE_SIZE)
      // The user may have switched conversations while this was in flight;
      // the reset in onConversationOpen already ran, so applying now would
      // leak the previous chat's history into the new one.
      if (conversationIdRef.current !== conversationId) return
      // A short page means the server had nothing more to give.
      if (page.length < OLDER_PAGE_SIZE) setHasMoreOlder(false)
      if (page.length === 0) return
      // The API returns newest-first; the list renders oldest-first.
      const older = page.slice().reverse()
      const el = scrollRef.current
      if (el) {
        pendingScrollRestoreRef.current = { scrollHeight: el.scrollHeight, scrollTop: el.scrollTop }
      }
      setMessages(prev => {
        const known = new Set(prev.map(m => m.id))
        const fresh = older.filter(m => !known.has(m.id))
        if (fresh.length === 0) return prev
        return [...fresh, ...prev]
      })
      setPrependCounter(c => c + 1)
    } catch {
      // Non-fatal: hasMoreOlder is left alone so scrolling up again retries.
    } finally {
      loadingOlderRef.current = false
      setLoadingOlder(false)
    }
  }, [conversationId, hasMoreOlder, messages, setMessages, scrollRef])

  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el || el.scrollTop > OLDER_SCROLL_THRESHOLD_PX) return
    void loadOlderMessages()
  }, [loadOlderMessages, scrollRef])

  return {
    loadingOlder,
    hasMoreOlder,
    prependCounter,
    pendingScrollRestoreRef,
    handleScroll,
    resetForConversation,
  }
}
