import { useEffect, useRef, useState, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { useTranslation } from 'react-i18next'
import type { ChatConnectionState } from '../../components/ConnectionStatus'
import { applyReactionEvent, type ReactionMap } from './api'
import type { CallKind, CallSignalEventName, CallSignalPayload } from './voice/useVoiceCall'
import type { GroupSignalEventName } from './voice/useGroupCall'

// useFamilyChatStream owns everything about a Family Chat conversation's data
// feed: the initial conversation + message load, the long-lived SSE reader,
// reconnect with exponential backoff, gap-fill (backfill-since-lastId) after a
// drop, the connection-status flags, the typing-user map with its expiry sweep,
// and missed-call synthesis.
//
// Voice/group call signalling is *not* handled here — call_* frames are routed
// out through the injected onSignal / onGroupSignal callbacks (kept in refs so a
// new callback identity never tears down the stream), which keeps useVoiceCall
// and useGroupCall completely independent of the transport.

export interface Conversation {
  id: number
  name: string
  owner_user_id: number
  created_at: string
  last_message_at: string
  unread_count: number
  member_ids: number[]
}

export interface ChatMessage {
  id: number
  conversation_id: number
  sender_user_id: number
  body: string
  attachment_path?: string
  attachment_mime?: string
  created_at: string
  edited_at?: string | null
  deleted_at?: string | null
  deleted_by?: number | null
  // meta_json is opaque client-controlled JSON the server stores verbatim.
  // Voice notes use it to ship the precomputed waveform (see voice/waveform.ts).
  meta_json?: string | null
  reactions?: ReactionMap
  // client_id is a client-generated correlation id for optimistic sends. It is
  // present on the locally-rendered "sending" bubble and is echoed back by the
  // server on the POST response and the SSE message_new event, so whichever
  // arrives first reconciles the bubble. Kept on the message after reconciling
  // so the React key stays stable (no remount/flicker). Absent on messages
  // loaded from the server.
  client_id?: string
  // status drives the optimistic-send affordance: 'sending' while the POST is
  // in flight, 'failed' once it errors (tap to retry). Absent for authoritative
  // (reconciled or server-loaded) messages.
  status?: 'sending' | 'failed'
}

// MissedCallEntry is an inbound call the recipient never answered — surfaced as
// a tombstone row in the message list with a call-back button. Only synthesised
// for calls where the local user was the callee (a missed call we placed
// ourselves isn't useful history for us).
export interface MissedCallEntry {
  callId: string
  fromUserId: number
  receivedAt: string
  // kind mirrors the call-kind from the original offer so call-back can
  // match the modality (voice → voice, video → video).
  kind: CallKind
}

// Wire shapes for the non-call SSE frames. They mirror what the server
// broadcasts (see internal/familychat); conversation_id is optional because
// older frames omitted it, in which case the frame is assumed to belong to the
// subscribed conversation.
interface ReactionEventPayload {
  message_id: number
  user_id: number
  emoji: string
  count: number
  conversation_id?: number
}

interface MessageEditedPayload {
  message_id: number
  body: string
  edited_at: string
  conversation_id?: number
}

interface MessageDeletedPayload {
  message_id: number
  deleted_by: number
  conversation_id?: number
}

// reconcileMessage merges an authoritative message into the list. When the
// incoming message carries a client_id that matches a local optimistic bubble,
// it replaces that bubble in place — preserving the client_id (so the React key
// stays stable and the row doesn't remount/flicker) and dropping the optimistic
// status. Otherwise it dedupes by id so a message delivered via both the POST
// response and the SSE stream (or gap-fill) shows up exactly once, regardless of
// arrival order.
export function reconcileMessage(
  prev: ChatMessage[],
  clientId: string | undefined,
  incoming: ChatMessage,
): ChatMessage[] {
  if (clientId) {
    const idx = prev.findIndex(m => m.client_id === clientId)
    if (idx !== -1) {
      const reconciled = { ...incoming, client_id: clientId, status: undefined }
      // Replace the optimistic bubble in place, and drop any *other* row that
      // already carries the same authoritative id. That second row appears when
      // an SSE message_new for this same send lands before the POST response
      // and gets appended separately (e.g. an event that omitted our
      // client_id); without this filter the reconcile would leave two bubbles
      // sharing one id.
      const next: ChatMessage[] = []
      prev.forEach((m, i) => {
        if (i === idx) { next.push(reconciled); return }
        if (m.id === incoming.id) return
        next.push(m)
      })
      return next
    }
  }
  if (prev.some(m => m.id === incoming.id)) return prev
  return [...prev, incoming]
}

export interface UseFamilyChatStreamOptions {
  conversationId: number | null
  // userId is the signed-in user's id. Used to compute the `me` flag on
  // reactions, to ignore our own typing echo, and to decide whether a missed
  // call was inbound.
  userId: number | undefined
  // callKind mirrors the 1:1 voice-call hook's current call kind so a missed
  // call records the right modality. The stream also updates its own shadow of
  // this value synchronously when a call_offer arrives, because a same-frame
  // call_end (a missed call) lands before React can flush a render.
  callKind: CallKind
  // onSignal receives 1:1 call signalling frames (2-member conversations).
  onSignal: (event: CallSignalEventName, payload: CallSignalPayload) => void | Promise<void>
  // onGroupSignal receives mesh call signalling frames (3+ member conversations).
  onGroupSignal: (event: GroupSignalEventName, payload: CallSignalPayload) => void | Promise<void>
  // onConversationOpen fires when a conversation's stream starts, so the caller
  // can clear per-conversation UI state it still owns.
  onConversationOpen?: () => void
  // onConversationClose fires from the teardown path (unmount or conversation
  // switch), before the caller's own cleanup.
  onConversationClose?: () => void
}

export interface UseFamilyChatStreamApi {
  conversation: Conversation | null
  messages: ChatMessage[]
  setMessages: Dispatch<SetStateAction<ChatMessage[]>>
  loading: boolean
  error: string
  connStatus: ChatConnectionState
  justReconnected: boolean
  // typingUsers maps a member's user id to the epoch-ms timestamp of their most
  // recent typing signal. A sweep drops entries older than ~5s so the indicator
  // clears on its own when the other side stops composing.
  typingUsers: Map<number, number>
  missedCalls: MissedCallEntry[]
  setMissedCalls: Dispatch<SetStateAction<MissedCallEntry[]>>
  // lastIdRef is the highest message id rendered for the current conversation.
  // It seeds the gap-fill query on reconnect and is exposed so callers can read
  // the resume watermark (tests assert on it).
  lastIdRef: RefObject<number>
}

export function useFamilyChatStream(options: UseFamilyChatStreamOptions): UseFamilyChatStreamApi {
  const { conversationId, userId, callKind } = options
  const { t } = useTranslation('familyChat')

  const [conversation, setConversation] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  // connStatus tracks the live state of the SSE stream so the header can show
  // honest connectivity feedback:
  //   'connecting'   — initial load, before the stream has ever opened
  //   'live'         — stream is open and messages arrive in real time
  //   'reconnecting' — the stream dropped after being live and a backoff retry
  //                    is in flight
  //   'offline'      — the browser reports no network connectivity
  // The 'connecting' → 'reconnecting' distinction keeps the initial-load
  // skeleton from being shadowed by a false "Reconnecting" badge.
  const [connStatus, setConnStatus] = useState<ChatConnectionState>('connecting')
  // justReconnected briefly flips true right after the stream recovers from a
  // drop so the header can flash a "Connected" confirmation, then auto-clears.
  const [justReconnected, setJustReconnected] = useState(false)
  const [missedCalls, setMissedCalls] = useState<MissedCallEntry[]>([])
  const [typingUsers, setTypingUsers] = useState<Map<number, number>>(new Map())

  // lastIdRef is the highest message id this client has rendered for the
  // current conversation. It seeds gap-fill queries on reconnect and is updated
  // by initial load, SSE events, and gap-fill responses.
  const lastIdRef = useRef(0)

  // The refs below shadow values the long-lived SSE reader closure (recreated
  // only when conversationId changes) needs to read at their most recent value,
  // without forcing the effect — and therefore the stream — to re-subscribe.
  const currentUserIdRef = useRef<number | undefined>(userId)
  const conversationRef = useRef<Conversation | null>(null)
  const signalRef = useRef(options.onSignal)
  const groupSignalRef = useRef(options.onGroupSignal)
  const conversationOpenRef = useRef(options.onConversationOpen)
  const conversationCloseRef = useRef(options.onConversationClose)
  // callKindRef shadows the 1:1 hook's callKind — the hook resets it
  // synchronously inside tearDown before the next render, so capturing it at
  // the top of the SSE dispatch path preserves the correct value even when
  // call_end arrives immediately after call_offer.
  const callKindRef = useRef<CallKind>(callKind)
  useEffect(() => { currentUserIdRef.current = userId }, [userId])
  useEffect(() => { conversationRef.current = conversation }, [conversation])
  useEffect(() => {
    signalRef.current = options.onSignal
    groupSignalRef.current = options.onGroupSignal
    conversationOpenRef.current = options.onConversationOpen
    conversationCloseRef.current = options.onConversationClose
    callKindRef.current = callKind
  })

  // Load conversation metadata + initial messages, then subscribe to the SSE
  // stream so new messages arrive without a refetch. The initial load and the
  // SSE subscription share a single AbortController so switching conversation
  // tears both down atomically; the SSE reader is also canceled explicitly so
  // tests (and the rare browser that doesn't propagate abort to a streaming
  // body) terminate the read loop deterministically.
  useEffect(() => {
    // When no conversation is selected, do nothing here — the previous
    // non-null effect's cleanup (below) resets state when switching away.
    // Calling setState directly in the guard body is flagged by
    // react-hooks/set-state-in-effect; cleanup callbacks are not.
    if (conversationId === null) return
    const controller = new AbortController()
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    // recoveredTimer clears the brief "Connected" confirmation after a drop.
    let recoveredTimer: ReturnType<typeof setTimeout> | null = null
    let reconnectAttempts = 0
    let activeReader: ReadableStreamDefaultReader<Uint8Array> | null = null
    let connectInFlight = false
    // browserOffline mirrors navigator.onLine so scheduleReconnect can label a
    // drop as "Offline" (no network) vs "Reconnecting" (server blip) without a
    // separate stream or poll.
    let browserOffline = typeof navigator !== 'undefined' && navigator.onLine === false

    // Initialise loading state at the start of every new conversation fetch.
    // setLoading(true) must live here (not in cleanup) because cleanup runs
    // setLoading(false) to represent "no conversation selected", which is the
    // correct value when conversationId is null. Cleanup callbacks are exempt
    // from react-hooks/set-state-in-effect; the synchronous call here is
    // intentional and safe — React 18 batches all these updates into one commit.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true)
    setError('')
    setMessages([])
    setConversation(null)
    setConnStatus(browserOffline ? 'offline' : 'connecting')
    setJustReconnected(false)
    setMissedCalls([])
    setTypingUsers(new Map())
    lastIdRef.current = 0
    conversationOpenRef.current?.()

    // appendIncoming deduplicates by id so a message that arrives via both
    // SSE and the POST response (the sender path) or via SSE and gap-fill
    // shows up exactly once.
    const appendIncoming = (msg: ChatMessage) => {
      if (msg.conversation_id !== conversationId) return
      if (msg.id > lastIdRef.current) lastIdRef.current = msg.id
      // A message from a member ends their typing state immediately, so the
      // indicator doesn't linger after their reply lands.
      setTypingUsers(prev => {
        if (!prev.has(msg.sender_user_id)) return prev
        const next = new Map(prev)
        next.delete(msg.sender_user_id)
        return next
      })
      // Reconcile against an optimistic bubble when the server echoed our
      // client_id (the SSE-first arrival path); otherwise dedupe by id.
      setMessages(prev => reconcileMessage(prev, msg.client_id, msg))
    }

    // applyReactionEventLocal merges an incoming reaction event into the
    // open message list. We can't compute the recipient's `me` flag from
    // the wire payload alone (the server broadcasts a single payload to
    // every subscriber), so the comparison happens here against the
    // current user's id.
    const applyReactionEventLocal = (
      payload: ReactionEventPayload,
      removed: boolean,
    ) => {
      if (payload.conversation_id !== undefined && payload.conversation_id !== conversationId) return
      setMessages(prev => {
        let changed = false
        const next = prev.map(m => {
          if (m.id !== payload.message_id) return m
          changed = true
          return {
            ...m,
            reactions: applyReactionEvent(m.reactions, payload, currentUserIdRef.current, removed),
          }
        })
        return changed ? next : prev
      })
    }

    // applyMessageEdited overwrites the body + edited_at of the matching
    // message. Keeps the existing reactions / attachment metadata intact.
    const applyMessageEdited = (payload: MessageEditedPayload) => {
      if (payload.conversation_id !== undefined && payload.conversation_id !== conversationId) return
      setMessages(prev => prev.map(m =>
        m.id === payload.message_id
          ? { ...m, body: payload.body, edited_at: payload.edited_at }
          : m,
      ))
    }

    // applyMessageDeleted converts the matching message into a tombstone.
    // Body + attachment metadata are cleared so the bubble flips to the
    // "Message deleted" placeholder; deleted_at + deleted_by drive that
    // rendering and the timestamp tooltip.
    const applyMessageDeleted = (payload: MessageDeletedPayload) => {
      if (payload.conversation_id !== undefined && payload.conversation_id !== conversationId) return
      const now = new Date().toISOString()
      setMessages(prev => prev.map(m =>
        m.id === payload.message_id
          ? {
              ...m,
              body: '',
              attachment_path: '',
              attachment_mime: '',
              edited_at: null,
              deleted_at: m.deleted_at ?? now,
              deleted_by: payload.deleted_by,
            }
          : m,
      ))
    }

    // recordTyping stamps a member's latest typing signal. We never record our
    // own id (the hub fans the event back to every subscriber, including the
    // sender) so the local user never sees their own indicator.
    const recordTyping = (typingUserId: number) => {
      if (typingUserId === currentUserIdRef.current) return
      setTypingUsers(prev => {
        const next = new Map(prev)
        next.set(typingUserId, Date.now())
        return next
      })
    }

    const fillGap = async () => {
      if (controller.signal.aborted) return
      try {
        const url = lastIdRef.current > 0
          ? `/api/familychat/conversations/${conversationId}/messages?since=${lastIdRef.current}`
          : `/api/familychat/conversations/${conversationId}/messages`
        const res = await fetch(url, { credentials: 'include', signal: controller.signal })
        if (!res.ok) return
        const data = await res.json()
        const msgs: ChatMessage[] = data.messages ?? []
        // API returns newest-first; sort ascending so lastId climbs
        // monotonically as we replay the burst.
        msgs.sort((a, b) => a.id - b.id)
        for (const m of msgs) appendIncoming(m)
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        // Non-fatal: the next reconnect attempt will retry.
      }
    }

    const scheduleReconnect = () => {
      if (controller.signal.aborted) return
      if (browserOffline) {
        setConnStatus('offline')
        return
      }
      // Only surface the "Reconnecting" badge once we've actually been live —
      // a failure on the very first connect keeps us in 'connecting' so the
      // initial load never flashes a false "offline".
      setConnStatus(prev => (prev === 'connecting' ? 'connecting' : 'reconnecting'))
      reconnectAttempts += 1
      // Exponential backoff capped at 30s to keep a server outage from
      // hammering the endpoint while still recovering quickly from a
      // transient blip.
      const delay = Math.min(30000, 1000 * 2 ** Math.min(reconnectAttempts, 5))
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null
        void connect(false)
      }, delay)
    }

    // dispatchFrame applies one decoded SSE frame. Message/reaction/typing
    // frames mutate local state; call_* frames are forwarded to the injected
    // signalling callbacks (routed by member count: 3+ → mesh, 2 → 1:1).
    const dispatchFrame = (eventName: string, payload: Record<string, unknown> | null) => {
      if (eventName === 'message_new' && payload?.message) {
        appendIncoming(payload.message as ChatMessage)
      } else if (
        (eventName === 'reaction_added' || eventName === 'reaction_removed') &&
        payload?.message_id !== undefined &&
        payload?.emoji !== undefined
      ) {
        applyReactionEventLocal(
          payload as unknown as ReactionEventPayload,
          eventName === 'reaction_removed',
        )
      } else if (
        eventName === 'message_edited' &&
        payload?.message_id !== undefined &&
        payload?.body !== undefined &&
        payload?.edited_at !== undefined
      ) {
        applyMessageEdited(payload as unknown as MessageEditedPayload)
      } else if (
        eventName === 'message_deleted' &&
        payload?.message_id !== undefined &&
        payload?.deleted_by !== undefined
      ) {
        applyMessageDeleted(payload as unknown as MessageDeletedPayload)
      } else if (
        eventName === 'typing' &&
        payload?.user_id !== undefined
      ) {
        recordTyping(payload.user_id as number)
      } else if (
        eventName === 'call_join'
        || eventName === 'call_leave'
      ) {
        // Group-call lifecycle events only exist for 3+ member
        // conversations; route them straight into the mesh hook.
        if ((conversationRef.current?.member_ids.length ?? 0) >= 3) {
          void groupSignalRef.current(
            eventName as GroupSignalEventName,
            payload as unknown as CallSignalPayload,
          )
        }
      } else if (
        eventName === 'call_offer'
        || eventName === 'call_answer'
        || eventName === 'call_ice'
        || eventName === 'call_end'
      ) {
        // 3+ member conversations use the group mesh; 1:1 uses the
        // single-peer voice-call hook. Route by member count.
        if ((conversationRef.current?.member_ids.length ?? 0) >= 3) {
          void groupSignalRef.current(
            eventName as GroupSignalEventName,
            payload as unknown as CallSignalPayload,
          )
        } else if (conversationRef.current?.member_ids.length === 2) {
          // Route call signalling into the voice-call hook. We also
          // track missed calls locally so the bubble area can render
          // a tombstone-style row with a call-back button: a
          // call_end with status=missed from someone other than us
          // means we never picked up.
          const callPayload = payload as unknown as CallSignalPayload
          // Synchronously update callKindRef when we see a call_offer so that
          // a same-iteration call_end (e.g. missed) captures the correct kind
          // without waiting for a React render to flush the effect above.
          if (eventName === 'call_offer' && callPayload?.kind) {
            callKindRef.current = callPayload.kind
          }
          if (
            eventName === 'call_end'
            && callPayload?.status === 'missed'
            && callPayload?.from_user_id !== undefined
            && callPayload.from_user_id !== currentUserIdRef.current
          ) {
            // Capture the call kind before the signal callback fires
            // tearDown (which resets the hook's callKind synchronously). This
            // preserves video vs voice so call-back matches the modality.
            const capturedKind = callKindRef.current
            setMissedCalls(prev => {
              if (prev.some(m => m.callId === callPayload.call_id)) return prev
              return [
                ...prev,
                {
                  callId: callPayload.call_id,
                  fromUserId: callPayload.from_user_id,
                  receivedAt: new Date().toISOString(),
                  kind: capturedKind,
                },
              ]
            })
          }
          void signalRef.current(
            eventName as CallSignalEventName,
            callPayload,
          )
        }
      }
    }

    const connect = async (firstConnect: boolean) => {
      if (controller.signal.aborted) return
      connectInFlight = true
      // Capture the resume point BEFORE fillGap runs. fillGap appends any new
      // messages and bumps lastId; if we passed the post-fillGap lastId to the
      // stream, the backfill watermark would advance past edits/deletes that
      // happened mid-gap (older than the newest message but newer than where we
      // actually left off), silently dropping them. The pre-gap id is the true
      // "last seen while connected" point.
      const resumeId = lastIdRef.current
      // Skip the gap-fill on the very first connect: the initial /messages
      // fetch already covered everything up to lastId. On reconnects we
      // re-issue it so a disconnect window can't lose messages.
      if (!firstConnect) await fillGap()
      if (controller.signal.aborted) return
      let reader: ReadableStreamDefaultReader<Uint8Array> | null = null
      try {
        // On reconnect, pass the last-seen message id as since_message_id so the
        // stream replays anything missed while we were gone — not just new
        // messages (which fillGap above already covers) but edits and deletes of
        // older messages too, which a plain id>since fetch can't see. A native
        // EventSource would resend this via Last-Event-ID automatically; this
        // fetch-based reader can't set that header, so the query param carries
        // the resume point instead. All replayed events are applied idempotently
        // below, so overlap with fillGap (or a never-dropped event) is a no-op.
        const streamUrl = !firstConnect && resumeId > 0
          ? `/api/familychat/conversations/${conversationId}/stream?since_message_id=${resumeId}`
          : `/api/familychat/conversations/${conversationId}/stream`
        const res = await fetch(
          streamUrl,
          { credentials: 'include', signal: controller.signal },
        )
        if (!res.ok || !res.body) {
          scheduleReconnect()
          return
        }
        reconnectAttempts = 0
        reader = res.body.getReader()
        activeReader = reader
        // A non-first connect means the stream just recovered from a drop. The
        // gap-fill above already backfilled any messages missed during the
        // outage; flash a brief "Connected" confirmation so the user gets a
        // visible signal that messages are arriving live again.
        if (!firstConnect) {
          setJustReconnected(true)
          if (recoveredTimer !== null) clearTimeout(recoveredTimer)
          recoveredTimer = setTimeout(() => {
            recoveredTimer = null
            setJustReconnected(false)
          }, 3000)
        }
        setConnStatus('live')
        const decoder = new TextDecoder()
        let buffer = ''
        let eventName = 'message'
        let dataLines: string[] = []
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          let nl = buffer.indexOf('\n')
          while (nl >= 0) {
            let line = buffer.slice(0, nl)
            buffer = buffer.slice(nl + 1)
            if (line.endsWith('\r')) line = line.slice(0, -1)
            if (line === '') {
              if (dataLines.length > 0) {
                try {
                  dispatchFrame(eventName, JSON.parse(dataLines.join('\n')))
                } catch {
                  // Ignore a malformed payload; the server should never emit
                  // one, but we don't want to tear down the whole stream over
                  // a single bad frame.
                }
              }
              eventName = 'message'
              dataLines = []
            } else if (line.startsWith(':')) {
              // SSE comment / heartbeat — ignore.
            } else if (line.startsWith('event:')) {
              eventName = line.slice(6).trimStart()
            } else if (line.startsWith('data:')) {
              const v = line.slice(5)
              dataLines.push(v.startsWith(' ') ? v.slice(1) : v)
            }
            nl = buffer.indexOf('\n')
          }
        }
        if (!controller.signal.aborted) scheduleReconnect()
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        if (!controller.signal.aborted) scheduleReconnect()
      } finally {
        connectInFlight = false
        if (activeReader === reader) activeReader = null
      }
    }

    // Reflect browser connectivity in the indicator. These only surface state
    // the EventSource-style reader already drives — 'offline' is the honest
    // label while there's no network, and coming back online retries at once
    // instead of waiting out the remaining backoff.
    const handleOffline = () => {
      browserOffline = true
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer)
        reconnectTimer = null
      }
      setConnStatus('offline')
    }
    const handleOnline = () => {
      browserOffline = false
      if (controller.signal.aborted) return
      // If a stream is already open/opening, leave it alone; otherwise surface
      // 'reconnecting' and retry immediately, cancelling any pending backoff.
      if (activeReader || connectInFlight) return
      setConnStatus(prev => (prev === 'connecting' ? 'connecting' : 'reconnecting'))
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer)
        reconnectTimer = null
      }
      reconnectAttempts = 0
      void connect(false)
    }
    if (typeof window !== 'undefined') {
      window.addEventListener('offline', handleOffline)
      window.addEventListener('online', handleOnline)
    }

    ;(async () => {
      try {
        const [convRes, msgRes] = await Promise.all([
          fetch(`/api/familychat/conversations/${conversationId}`, {
            credentials: 'include',
            signal: controller.signal,
          }),
          fetch(`/api/familychat/conversations/${conversationId}/messages`, {
            credentials: 'include',
            signal: controller.signal,
          }),
        ])
        if (!convRes.ok) throw new Error('conversation failed')
        if (!msgRes.ok) throw new Error('messages failed')
        const convData = await convRes.json()
        const msgData = await msgRes.json()
        if (controller.signal.aborted) return
        setConversation(convData.conversation ?? null)
        // The API returns newest-first; display oldest at top to bottom.
        const sorted: ChatMessage[] = (msgData.messages ?? []).slice().reverse()
        if (sorted.length > 0) lastIdRef.current = sorted[sorted.length - 1].id
        setMessages(sorted)
        void connect(true)
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        const key = err instanceof Error && err.message === 'conversation failed'
          ? 'errors.failedToLoadConversation'
          : 'errors.failedToLoadMessages'
        setError(t(key))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()

    return () => {
      controller.abort()
      if (typeof window !== 'undefined') {
        window.removeEventListener('offline', handleOffline)
        window.removeEventListener('online', handleOnline)
      }
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer)
        reconnectTimer = null
      }
      if (recoveredTimer !== null) {
        clearTimeout(recoveredTimer)
        recoveredTimer = null
      }
      // Cancel the reader so the read loop exits even when the fetch mock
      // doesn't propagate abort to the body (notably in tests). The catch is
      // intentional — cancel can throw if the reader is already detached.
      if (activeReader) {
        activeReader.cancel().catch(() => {})
        activeReader = null
      }
      // Let the caller tear down whatever it owns for this conversation (voice
      // playback, transient banners) before the stream state is reset.
      conversationCloseRef.current?.()
      // Reset conversation state when leaving (to null or another conversation)
      // so the next render starts from a blank slate. State updates inside
      // cleanup callbacks are not flagged by react-hooks/set-state-in-effect.
      setConversation(null)
      setMessages([])
      setError('')
      setLoading(false)
      // Reset connection state so the indicator never stays stuck in
      // 'reconnecting' after the view unmounts or switches conversation.
      setConnStatus('connecting')
      setJustReconnected(false)
      setMissedCalls([])
      setTypingUsers(new Map())
      lastIdRef.current = 0
    }
  }, [conversationId, t])

  // Sweep stale typing indicators: drop any member whose most recent signal is
  // older than 5s so the row clears shortly after they stop composing. The
  // interval only runs while at least one member is typing.
  useEffect(() => {
    if (typingUsers.size === 0) return
    const interval = setInterval(() => {
      setTypingUsers(prev => {
        const now = Date.now()
        let changed = false
        const next = new Map(prev)
        for (const [uid, ts] of next) {
          if (now - ts > 5000) {
            next.delete(uid)
            changed = true
          }
        }
        return changed ? next : prev
      })
    }, 1000)
    return () => clearInterval(interval)
  }, [typingUsers])

  return {
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
    lastIdRef,
  }
}
