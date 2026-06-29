import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ChevronLeft,
  MessageSquare,
  Download,
  X,
  Wifi,
  WifiOff,
  Smile,
  MoreVertical,
  Phone,
  PhoneOff,
  PhoneIncoming,
  PhoneMissed,
  Mic,
  MicOff,
  Volume2,
  VolumeX,
  Video,
  VideoOff,
  SwitchCamera,
} from 'lucide-react'
import { Skeleton } from '../../components/ui/skeleton'
import { useAuth } from '../../auth'
import Composer from './Composer'
import ReactionChips from './ReactionChips'
import ReactionPicker from './ReactionPicker'
import {
  addReaction,
  removeReaction,
  applyReactionEvent,
  editMessage,
  deleteMessage,
  type ReactionMap,
} from './api'
import { formatRelative } from './utils'
import VoiceBubble from './voice/VoiceBubble'
import { readCachedWaveform, parseWaveformJSON, DEFAULT_BAR_COUNT, type Waveform } from './voice/waveform'
import * as voicePlayer from './voice/voicePlayer'
import { useVoiceCall, type CallKind, type CallSignalEventName, type CallSignalPayload } from './voice/useVoiceCall'
import { useGroupCall, type GroupSignalEventName } from './voice/useGroupCall'
import GroupCallOverlay from './GroupCallOverlay'

interface ChatViewProps {
  conversationId: number | null
  onBack: () => void
}

interface Conversation {
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

// reconcileMessage merges an authoritative message into the list. When the
// incoming message carries a client_id that matches a local optimistic bubble,
// it replaces that bubble in place — preserving the client_id (so the React key
// stays stable and the row doesn't remount/flicker) and dropping the optimistic
// status. Otherwise it dedupes by id so a message delivered via both the POST
// response and the SSE stream (or gap-fill) shows up exactly once, regardless of
// arrival order.
function reconcileMessage(
  prev: ChatMessage[],
  clientId: string | undefined,
  incoming: ChatMessage,
): ChatMessage[] {
  if (clientId) {
    const idx = prev.findIndex(m => m.client_id === clientId)
    if (idx !== -1) {
      const next = prev.slice()
      next[idx] = { ...incoming, client_id: clientId, status: undefined }
      return next
    }
  }
  if (prev.some(m => m.id === incoming.id)) return prev
  return [...prev, incoming]
}

// parseVoiceMeta extracts a {bars, durationMs} pair from a meta_json blob.
// Returns null when the field is absent, unparseable, or shaped wrong — the
// caller falls back to a localStorage cache (and ultimately a flat waveform)
// so a malformed meta_json never blocks playback.
function parseVoiceMeta(meta: string | null | undefined): Waveform | null {
  if (!meta) return null
  return parseWaveformJSON(meta)
}

interface MemberInfo {
  label: string
  emoji: string
}

interface MissedCallEntry {
  callId: string
  fromUserId: number
  receivedAt: string
  // kind mirrors the call-kind from the original offer so call-back can
  // match the modality (voice → voice, video → video).
  kind: CallKind
}

interface FamilyChild {
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface SiblingInfo {
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface ParentInfo {
  user_id: number
  name: string
  picture: string
}

export default function ChatView({ conversationId, onBack }: ChatViewProps) {
  const { t, i18n } = useTranslation('familyChat')
  const { user, familyStatus } = useAuth()

  const [conversation, setConversation] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [memberLookup, setMemberLookup] = useState<Map<number, MemberInfo>>(new Map())
  const [lightbox, setLightbox] = useState<{ url: string; alt: string } | null>(null)
  // connStatus tracks the live state of the SSE stream so the header can show
  // honest connectivity feedback:
  //   'connecting'   — initial load, before the stream has ever opened
  //   'live'         — stream is open and messages arrive in real time
  //   'reconnecting' — the stream dropped after being live and a backoff retry
  //                    is in flight
  // The 'connecting' → 'reconnecting' distinction keeps the initial-load
  // skeleton from being shadowed by a false "Reconnecting" badge.
  const [connStatus, setConnStatus] = useState<'connecting' | 'live' | 'reconnecting'>('connecting')
  // justReconnected briefly flips true right after the stream recovers from a
  // drop so the header can flash a "Connected" confirmation, then auto-clears.
  const [justReconnected, setJustReconnected] = useState(false)
  // pickerForMsgId is the id of the bubble whose reaction picker is open, or
  // null when nothing is open. We only show one picker at a time.
  const [pickerForMsgId, setPickerForMsgId] = useState<number | null>(null)
  // menuForMsgId is the id of the own-message bubble whose actions menu (edit /
  // delete) is open. Only one menu is open at a time.
  const [menuForMsgId, setMenuForMsgId] = useState<number | null>(null)
  // editingMsgId is the id of the message currently being edited inline, with
  // editingDraft holding the in-progress text and editingError the most recent
  // save failure (so the user can retry without losing their draft).
  const [editingMsgId, setEditingMsgId] = useState<number | null>(null)
  const [editingDraft, setEditingDraft] = useState('')
  const [editingSaving, setEditingSaving] = useState(false)
  const [editingError, setEditingError] = useState('')
  // confirmDeleteId holds the id of the message whose delete-confirm modal is
  // open, or null when the modal is closed.
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null)
  const [deleteError, setDeleteError] = useState('')
  const deleteConfirmBtnRef = useRef<HTMLButtonElement>(null)
  const deletePrevFocusRef = useRef<Element | null>(null)
  // missedCalls collects inbound calls the recipient never answered — surfaced
  // as a tombstone row in the message list with a call-back button. Only
  // populated for missed calls where the local user was the callee (we ignore
  // missed calls that we initiated, since those aren't useful history for us).
  const [missedCalls, setMissedCalls] = useState<MissedCallEntry[]>([])
  // typingUsers maps a member's user id to the epoch-ms timestamp of their most
  // recent typing signal. A sweep effect drops entries older than ~5s so the
  // indicator clears on its own when the other side stops composing.
  const [typingUsers, setTypingUsers] = useState<Map<number, number>>(new Map())
  // lastTypingSentRef throttles outbound typing POSTs to at most one per ~3s
  // while the local user is composing.
  const lastTypingSentRef = useRef(0)
  // endedCallSummary is shown briefly after a call wraps up. Tracks the final
  // duration in seconds so the banner can render "Call ended — m:ss".
  const [endedCallSummary, setEndedCallSummary] = useState<{ durationSec: number } | null>(null)
  // callElapsedSec is the running second-counter used by the active-call
  // overlay. It rounds to whole seconds so the UI updates once per tick.
  const [callElapsedSec, setCallElapsedSec] = useState(0)
  const callStartedAtRef = useRef<number | null>(null)
  const remoteAudioRef = useRef<HTMLAudioElement | null>(null)
  // Video sinks for video calls: remote is the big pane, local is the PiP on
  // mobile and a separate side-by-side pane on desktop. Both local elements
  // bind to the same MediaStream so the layout can switch via CSS without
  // re-acquiring the camera.
  const remoteVideoRef = useRef<HTMLVideoElement | null>(null)
  const localVideoRef = useRef<HTMLVideoElement | null>(null)
  const localVideoDesktopRef = useRef<HTMLVideoElement | null>(null)
  // speakerOn controls the remote audio volume: true = full volume (1.0),
  // false = muted (0) so "Speaker off" means no audio is heard.
  const [speakerOn, setSpeakerOn] = useState(true)
  // pipPosition holds the {x, y} offset for the draggable local-preview
  // window. Initialised to null so the PiP renders in its default top-right
  // corner via Tailwind classes; once the user drags it switches to inline
  // style overrides. Reset to null at the end of each call so the next call
  // always starts from the default corner (see effect below).
  const [pipPosition, setPipPosition] = useState<{ x: number; y: number } | null>(null)
  const pipDragRef = useRef<{ offsetX: number; offsetY: number; pointerId: number } | null>(null)

  // Voice-call state machine. skipSignalSubscription is set so the hook
  // doesn't open its own SSE stream — ChatView already owns one for messages
  // and reactions, and routes call_* frames through handleSignalEvent below.
  const voiceCall = useVoiceCall({
    conversationId,
    userId: user?.id ?? null,
    skipSignalSubscription: true,
  })

  // Group-call mesh for conversations with 3+ members. Like voiceCall it does
  // not open its own SSE stream — ChatView routes call_* / call_join /
  // call_leave frames into it via groupCallSignalRef below.
  const groupCall = useGroupCall({
    conversationId,
    userId: user?.id ?? null,
  })

  const messagesEndRef = useRef<HTMLDivElement>(null)
  // voiceCallSignalRef shadows handleSignalEvent so the long-lived SSE reader
  // can dispatch into the latest hook state without re-running on every
  // re-render the hook returns a new function reference for.
  const voiceCallSignalRef = useRef(voiceCall.handleSignalEvent)
  useEffect(() => {
    voiceCallSignalRef.current = voiceCall.handleSignalEvent
  })
  // voiceCallEndRef lets the conversationId-change cleanup end any active call
  // on the old conversation without re-subscribing on every voice-call state change.
  const voiceCallEndRef = useRef(voiceCall.endCall)
  useEffect(() => {
    voiceCallEndRef.current = voiceCall.endCall
  })
  // voiceCallKindRef shadows voiceCall.callKind so the long-lived SSE reader
  // can capture the current call kind when recording missed calls — the hook
  // resets callKind synchronously inside tearDown before the next render, so
  // capturing it at the top of the SSE dispatch path preserves the correct
  // value even when call_end arrives immediately after call_offer.
  const voiceCallKindRef = useRef<CallKind>(voiceCall.callKind)
  useEffect(() => {
    voiceCallKindRef.current = voiceCall.callKind
  })
  // groupCallSignalRef shadows groupCall.handleSignalEvent so the long-lived
  // SSE reader can dispatch group-call frames into the latest hook state without
  // re-subscribing on every render.
  const groupCallSignalRef = useRef(groupCall.handleSignalEvent)
  useEffect(() => {
    groupCallSignalRef.current = groupCall.handleSignalEvent
  })
  // groupCallLeaveRef lets the conversationId-change cleanup leave any active
  // group call on the old conversation.
  const groupCallLeaveRef = useRef(groupCall.leaveCall)
  useEffect(() => {
    groupCallLeaveRef.current = groupCall.leaveCall
  })
  // Derive the effective PiP position: passthrough while a call is live,
  // null otherwise. pipPosition is reset to null when a new call is initiated
  // (in handleStartCall / handleAcceptCall) so each call starts from the
  // default top-right corner regardless of where the previous call's PiP was
  // dragged. Placed here (after voiceCall) so that voiceCall.state is in scope.
  const activePipPosition =
    voiceCall.state === 'active' || voiceCall.state === 'outgoing-ringing'
      ? pipPosition
      : null
  // Tear down any active call when switching conversations so the mic and
  // RTCPeerConnection don't remain active against the old conversation.
  useEffect(() => {
    return () => {
      void voiceCallEndRef.current()
      void groupCallLeaveRef.current()
    }
  }, [conversationId])
  // currentUserIdRef shadows user?.id so the long-lived SSE reader closure
  // (recreated only when conversationId changes) can read the most recent
  // value without forcing the effect to re-run on auth changes.
  const currentUserIdRef = useRef<number | undefined>(user?.id)
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
  // conversationRef mirrors the conversation state for the long-lived SSE
  // closure so call-event gating can check member count without re-running.
  const conversationRef = useRef<Conversation | null>(null)
  useEffect(() => { currentUserIdRef.current = user?.id }, [user?.id])
  useEffect(() => { conversationRef.current = conversation }, [conversation])

  // Focus management + Escape handling for the delete confirmation modal,
  // matching the pattern used by ConfirmDialog.
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
      if (e.key === 'Escape') { setConfirmDeleteId(null); setDeleteError('') }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [confirmDeleteId])

  const rtf = useMemo(
    () => new Intl.RelativeTimeFormat(i18n.language, { numeric: 'auto' }),
    [i18n.language],
  )

  // Build a label/emoji lookup for every user the current user can name,
  // so member chips and sender labels render with friendly names. The
  // current user is always included from auth context.
  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    ;(async () => {
      const lookup = new Map<number, MemberInfo>()
      lookup.set(user.id, { label: user.name || user.email || `#${user.id}`, emoji: '👤' })
      try {
        if (familyStatus?.is_parent) {
          const res = await fetch('/api/family/children', {
            credentials: 'include',
            signal: controller.signal,
          })
          if (res.ok) {
            const data = await res.json()
            const kids: FamilyChild[] = data.children ?? []
            for (const k of kids) {
              lookup.set(k.child_id, {
                label: k.nickname || `#${k.child_id}`,
                emoji: k.avatar_emoji || '⭐',
              })
            }
          }
        }
        if (familyStatus?.is_child) {
          const res = await fetch('/api/family/my-family', {
            credentials: 'include',
            signal: controller.signal,
          })
          if (res.ok) {
            const data = await res.json()
            const parent: ParentInfo | undefined = data.parent
            if (parent?.user_id) {
              lookup.set(parent.user_id, {
                label: parent.name || t('newModal.parent'),
                emoji: '👤',
              })
            }
            const siblings: SiblingInfo[] = data.siblings ?? []
            for (const s of siblings) {
              lookup.set(s.child_id, {
                label: s.nickname || `#${s.child_id}`,
                emoji: s.avatar_emoji || '⭐',
              })
            }
          }
        }
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        // Non-fatal: chips fall back to "Member #id" if the lookup is empty.
      }
      if (!controller.signal.aborted) setMemberLookup(lookup)
    })()
    return () => { controller.abort() }
  }, [user, familyStatus, t])

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
    // lastId is the highest message id this client has rendered for the
    // current conversation. It seeds gap-fill queries on reconnect and is
    // updated by initial load, SSE events, and gap-fill responses.
    let lastId = 0
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    // recoveredTimer clears the brief "Connected" confirmation after a drop.
    let recoveredTimer: ReturnType<typeof setTimeout> | null = null
    let reconnectAttempts = 0
    let activeReader: ReadableStreamDefaultReader<Uint8Array> | null = null

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
    setConnStatus('connecting')
    setJustReconnected(false)
    setMissedCalls([])
    setEndedCallSummary(null)
    setTypingUsers(new Map())
    lastTypingSentRef.current = 0

    // appendIncoming deduplicates by id so a message that arrives via both
    // SSE and the POST response (the sender path) or via SSE and gap-fill
    // shows up exactly once.
    const appendIncoming = (msg: ChatMessage) => {
      if (msg.conversation_id !== conversationId) return
      if (msg.id > lastId) lastId = msg.id
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
      payload: { message_id: number; user_id: number; emoji: string; count: number; conversation_id?: number },
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
    const applyMessageEdited = (
      payload: { message_id: number; body: string; edited_at: string; conversation_id?: number },
    ) => {
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
    const applyMessageDeleted = (
      payload: { message_id: number; deleted_by: number; conversation_id?: number },
    ) => {
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
    const recordTyping = (userId: number) => {
      if (userId === currentUserIdRef.current) return
      setTypingUsers(prev => {
        const next = new Map(prev)
        next.set(userId, Date.now())
        return next
      })
    }

    const fillGap = async () => {
      if (controller.signal.aborted) return
      try {
        const url = lastId > 0
          ? `/api/familychat/conversations/${conversationId}/messages?since=${lastId}`
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

    const connect = async (firstConnect: boolean) => {
      if (controller.signal.aborted) return
      // Capture the resume point BEFORE fillGap runs. fillGap appends any new
      // messages and bumps lastId; if we passed the post-fillGap lastId to the
      // stream, the backfill watermark would advance past edits/deletes that
      // happened mid-gap (older than the newest message but newer than where we
      // actually left off), silently dropping them. The pre-gap id is the true
      // "last seen while connected" point.
      const resumeId = lastId
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
                  const payload = JSON.parse(dataLines.join('\n'))
                  if (eventName === 'message_new' && payload?.message) {
                    appendIncoming(payload.message as ChatMessage)
                  } else if (
                    (eventName === 'reaction_added' || eventName === 'reaction_removed') &&
                    payload?.message_id !== undefined &&
                    payload?.emoji !== undefined
                  ) {
                    applyReactionEventLocal(payload, eventName === 'reaction_removed')
                  } else if (
                    eventName === 'message_edited' &&
                    payload?.message_id !== undefined &&
                    payload?.body !== undefined &&
                    payload?.edited_at !== undefined
                  ) {
                    applyMessageEdited(payload)
                  } else if (
                    eventName === 'message_deleted' &&
                    payload?.message_id !== undefined &&
                    payload?.deleted_by !== undefined
                  ) {
                    applyMessageDeleted(payload)
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
                      void groupCallSignalRef.current(
                        eventName as GroupSignalEventName,
                        payload as CallSignalPayload,
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
                      void groupCallSignalRef.current(
                        eventName as GroupSignalEventName,
                        payload as CallSignalPayload,
                      )
                    } else if (conversationRef.current?.member_ids.length === 2) {
                      // Route call signalling into the voice-call hook. We also
                      // track missed calls locally so the bubble area can render
                      // a tombstone-style row with a call-back button: a
                      // call_end with status=missed from someone other than us
                      // means we never picked up.
                      const callPayload = payload as CallSignalPayload
                      // Synchronously update voiceCallKindRef when we see a
                      // call_offer so that a same-iteration call_end (e.g.
                      // missed) captures the correct kind without waiting for
                      // a React render to flush the useEffect above.
                      if (eventName === 'call_offer' && callPayload?.kind) {
                        voiceCallKindRef.current = callPayload.kind
                      }
                      if (
                        eventName === 'call_end'
                        && callPayload?.status === 'missed'
                        && callPayload?.from_user_id !== undefined
                        && callPayload.from_user_id !== currentUserIdRef.current
                      ) {
                        // Capture the call kind before voiceCallSignalRef fires
                        // tearDown (which resets callKindRef synchronously). This
                        // preserves video vs voice so call-back matches the modality.
                        const capturedKind = voiceCallKindRef.current
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
                      void voiceCallSignalRef.current(
                        eventName as CallSignalEventName,
                        callPayload,
                      )
                    }
                  }
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
        if (activeReader === reader) activeReader = null
      }
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
        if (sorted.length > 0) lastId = sorted[sorted.length - 1].id
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
      // Stop any voice-note playback owned by this conversation. Switching
      // chats or unmounting must not leave a bubble in the prior conversation
      // continuing to play in the background.
      voicePlayer.stopAll()
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
      setEndedCallSummary(null)
      setTypingUsers(new Map())
    }
  }, [conversationId, t])

  // Auto-scroll to the bottom whenever the message list updates. useLayoutEffect
  // avoids a visible jump between initial paint and the scroll snap.
  useLayoutEffect(() => {
    if (messagesEndRef.current) {
      messagesEndRef.current.scrollIntoView({ block: 'end' })
    }
  }, [messages.length, conversationId])

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

  // Wire the remote audio stream into the hidden <audio> element so the
  // browser actually plays the peer's voice. The peer connection only opens
  // the data path; the page is responsible for piping it into an audio sink.
  // For video calls the same MediaStream is also bound to the remote <video>
  // element below; the <audio> tag is still required for voice calls (no
  // <video> mounts) and as a redundant audio sink while the video pane loads.
  useEffect(() => {
    const el = remoteAudioRef.current
    if (!el) return
    if (voiceCall.remoteStream) {
      el.srcObject = voiceCall.remoteStream
      el.volume = speakerOn ? 1 : 0
      void el.play().catch(() => {
        // Autoplay rejection — the accept-button click already counts as a
        // user gesture in every supported browser, but a stricter policy
        // would surface here. Nothing actionable from this layer.
      })
    } else {
      el.srcObject = null
    }
  }, [voiceCall.remoteStream, speakerOn])

  // Wire the remote video stream into the big remote <video> pane when the
  // pane is mounted (i.e. during an active video call). We also push the
  // stream into the local PiP <video> from voiceCall.localStream. Both are
  // muted=true at the element level — the audio sink above plays the sound.
  useEffect(() => {
    const el = remoteVideoRef.current
    if (!el) return
    el.srcObject = voiceCall.remoteStream ?? null
    if (voiceCall.remoteStream) {
      void el.play().catch(() => { /* autoplay policy — acceptable to ignore */ })
    }
  }, [voiceCall.remoteStream, voiceCall.state])

  useEffect(() => {
    const el = localVideoRef.current
    if (!el) return
    el.srcObject = voiceCall.localStream ?? null
    if (voiceCall.localStream) {
      void el.play().catch(() => { /* same as above */ })
    }
  }, [voiceCall.localStream, voiceCall.state])

  useEffect(() => {
    const el = localVideoDesktopRef.current
    if (!el) return
    el.srcObject = voiceCall.localStream ?? null
    if (voiceCall.localStream) {
      void el.play().catch(() => { /* autoplay policy — acceptable to ignore */ })
    }
  }, [voiceCall.localStream, voiceCall.state])

  // Drive the elapsed-time counter while a call is active. Reset on every
  // state transition so the timer always reads from the moment we entered
  // 'active', not from the moment startCall was first invoked.
  useEffect(() => {
    if (voiceCall.state !== 'active') {
      // Capture the final elapsed seconds when the call leaves the active
      // state so the "Call ended — m:ss" banner can show the right total.
      if (callStartedAtRef.current !== null) {
        const total = Math.floor((Date.now() - callStartedAtRef.current) / 1000)
        callStartedAtRef.current = null
        if (voiceCall.state === 'ended') {
          setEndedCallSummary({ durationSec: total })
        }
      }
      setCallElapsedSec(0)
      return
    }
    callStartedAtRef.current = Date.now()
    setCallElapsedSec(0)
    const interval = setInterval(() => {
      if (callStartedAtRef.current === null) return
      setCallElapsedSec(Math.floor((Date.now() - callStartedAtRef.current) / 1000))
    }, 1000)
    return () => clearInterval(interval)
  }, [voiceCall.state])

  // Auto-dismiss the "Call ended" banner after a short hold so it doesn't sit
  // on screen indefinitely. Five seconds is long enough to read the duration
  // and short enough not to feel sticky.
  useEffect(() => {
    if (!endedCallSummary) return
    const timer = setTimeout(() => setEndedCallSummary(null), 5000)
    return () => clearTimeout(timer)
  }, [endedCallSummary])

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

  const handleMessageCreated = useCallback((msg: ChatMessage) => {
    // Defensive: if the user switched conversations while a send was in
    // flight, drop the message rather than leaking it into the wrong chat.
    if (msg.conversation_id !== conversationId) return
    // Reconcile against an optimistic bubble when the message carries a
    // client_id (the POST-response path for text sends); otherwise dedupe by
    // id (voice notes and attachments, which are not rendered optimistically).
    setMessages(prev => reconcileMessage(prev, msg.client_id, msg))
  }, [conversationId])

  // handleOptimisticMessage renders a just-typed text message immediately in a
  // 'sending' state, before any network round-trip. Scoped to the active
  // conversation so a stray emit can't leak into the wrong chat.
  const handleOptimisticMessage = useCallback((msg: ChatMessage) => {
    if (msg.conversation_id !== conversationId) return
    setMessages(prev => [...prev, msg])
  }, [conversationId])

  // handleMessageFailed flips an optimistic bubble to the 'failed' state when
  // its POST errors, preserving the typed text so the user can tap to retry.
  // No conversation guard is needed: after a switch the bubble is gone, so the
  // client_id no longer matches and this is a no-op.
  const handleMessageFailed = useCallback((clientId: string) => {
    setMessages(prev => prev.map(m =>
      m.client_id === clientId ? { ...m, status: 'failed' as const } : m,
    ))
  }, [])

  // composerRetryRef holds the Composer's retry entry point. The failed bubble's
  // tap target calls it to re-POST the preserved text under the same client_id
  // so a successful retry reconciles normally.
  const composerRetryRef = useRef<
    ((clientId: string, text: string, targetConversationId: number) => void) | null
  >(null)

  const retryFailedMessage = useCallback((msg: ChatMessage) => {
    if (!msg.client_id) return
    const clientId = msg.client_id
    // Flip back to 'sending' immediately so the affordance reflects the retry.
    setMessages(prev => prev.map(m =>
      m.client_id === clientId ? { ...m, status: 'sending' as const } : m,
    ))
    composerRetryRef.current?.(clientId, msg.body, msg.conversation_id)
  }, [])

  // toggleReaction applies the change optimistically (chips update before the
  // network round-trip) and rolls back on failure. The eventual SSE
  // confirmation overwrites the optimistic state with the server-authoritative
  // count, which keeps two clients in sync even if either one races.
  const userId = user?.id
  const toggleReaction = useCallback(async (msgId: number, emoji: string, currentlyMine: boolean) => {
    if (conversationId === null || userId === undefined) return
    const meID = userId
    const snapshot = messages.find(m => m.id === msgId) ?? null
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
    }
  }, [conversationId, userId, messages])

  const handlePickFromPicker = useCallback((msgId: number, emoji: string) => {
    setPickerForMsgId(null)
    const msg = messages.find(m => m.id === msgId)
    const mine = !!msg?.reactions?.[emoji]?.me
    void toggleReaction(msgId, emoji, mine)
  }, [messages, toggleReaction])

  // openEditor opens the inline editor for a message bubble. Seeds the draft
  // from the current body so the user starts from what's on screen.
  const openEditor = useCallback((msg: ChatMessage) => {
    setMenuForMsgId(null)
    setEditingMsgId(msg.id)
    setEditingDraft(msg.body)
    setEditingError('')
    setEditingSaving(false)
  }, [])

  const cancelEditor = useCallback(() => {
    setEditingMsgId(null)
    setEditingDraft('')
    setEditingError('')
    setEditingSaving(false)
  }, [])

  const saveEditor = useCallback(async (msgId: number) => {
    if (conversationId === null) return
    const trimmed = editingDraft.trim()
    if (!trimmed) {
      setEditingError(t('edit.saveError'))
      return
    }
    setEditingSaving(true)
    setEditingError('')
    // Capture the pre-edit body/edited_at so a failed save can revert the
    // optimistic update — otherwise the bubble would keep showing the
    // unsaved draft as if it had been persisted.
    const snapshot = messages.find(m => m.id === msgId) ?? null
    // Optimistic update first: the SSE confirmation will overwrite shortly
    // with the server's authoritative edited_at, which matches the pattern
    // used by reactions / message sends in this view.
    const optimisticTime = new Date().toISOString()
    setMessages(prev => prev.map(m =>
      m.id === msgId ? { ...m, body: trimmed, edited_at: optimisticTime } : m,
    ))
    try {
      const updated = await editMessage(conversationId, msgId, trimmed)
      setMessages(prev => prev.map(m =>
        m.id === msgId
          ? { ...m, body: updated.body, edited_at: updated.edited_at }
          : m,
      ))
      setEditingMsgId(null)
      setEditingDraft('')
      setEditingSaving(false)
    } catch {
      if (snapshot) {
        setMessages(prev => prev.map(m =>
          m.id === msgId
            ? { ...m, body: snapshot.body, edited_at: snapshot.edited_at ?? null }
            : m,
        ))
      }
      setEditingError(t('edit.saveError'))
      setEditingSaving(false)
    }
  }, [conversationId, editingDraft, messages, t])

  const confirmDelete = useCallback(async (msgId: number) => {
    if (conversationId === null) return
    setDeleteError('')
    const meID = user?.id ?? null
    const now = new Date().toISOString()
    // Capture the pre-delete snapshot from the current render's state so the
    // rollback in catch{} doesn't depend on the setState updater having run.
    const snapshot = messages.find(m => m.id === msgId) ?? null
    setMessages(prev => prev.map(m => {
      if (m.id !== msgId) return m
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
    try {
      await deleteMessage(conversationId, msgId)
      // Only dismiss the confirm modal after the server has accepted the
      // delete — closing it earlier would hide the error message rendered
      // inside the same modal if the request fails.
      setConfirmDeleteId(null)
    } catch {
      if (snapshot) {
        setMessages(prev => prev.map(m => (m.id === msgId ? snapshot : m)))
      }
      setDeleteError(t('edit.deleteError'))
    }
  }, [conversationId, user?.id, t, messages])

  const memberChips = useMemo(() => {
    if (!conversation) return []
    return conversation.member_ids.map(id => {
      const info = memberLookup.get(id)
      const isSelf = user?.id === id
      return {
        id,
        label: isSelf
          ? t('chat.you')
          : info?.label ?? t('chat.memberFallback', { id }),
        emoji: info?.emoji ?? '👤',
        isSelf,
      }
    })
  }, [conversation, memberLookup, t, user?.id])

  // Resolve the friendly labels for everyone currently typing, excluding the
  // local user (defensive — recordTyping already skips own-id signals).
  const typingLabels = useMemo(() => {
    return Array.from(typingUsers.keys())
      .filter(id => id !== user?.id)
      .map(id => memberLookup.get(id)?.label ?? t('chat.memberFallback', { id }))
  }, [typingUsers, memberLookup, user?.id, t])

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

  const peerLabel = useCallback((peerId: number | null) => {
    if (peerId === null) return t('chat.memberFallback', { id: 0 })
    return memberLookup.get(peerId)?.label ?? t('chat.memberFallback', { id: peerId })
  }, [memberLookup, t])

  // Label/emoji accessors for the group-call overlay tiles.
  const groupMemberInfo = useCallback((id: number) => {
    const info = memberLookup.get(id)
    return { label: info?.label ?? t('chat.memberFallback', { id }), emoji: info?.emoji ?? '👤' }
  }, [memberLookup, t])
  const groupMemberLabel = useCallback((id: number) => groupMemberInfo(id).label, [groupMemberInfo])
  const selfEmoji = (user?.id !== undefined ? memberLookup.get(user.id)?.emoji : undefined) ?? '👤'

  // Start a group call from the header buttons. A second press while already in
  // a call is a no-op (the hook guards re-entry).
  const handleStartGroupCall = useCallback((kind: CallKind = 'voice') => {
    if (!canGroupCall) return
    void groupCall.startCall(kind)
  }, [canGroupCall, groupCall])

  const incomingCallerLabel = peerLabel(voiceCall.remoteUserId)
  const activeCallPeerLabel = peerLabel(voiceCall.remoteUserId ?? callPeerId)

  const formatCallDuration = useCallback((totalSec: number): string => {
    const safe = Math.max(0, Math.floor(totalSec))
    const minutes = Math.floor(safe / 60)
    const seconds = safe % 60
    return `${minutes}:${seconds.toString().padStart(2, '0')}`
  }, [])

  // startOrIgnoreCall fires the outgoing-call flow only when we're idle. A
  // second press while already ringing is a no-op so a double-tap can't kick
  // off two parallel sessions. Kind is plumbed through so the same handler
  // serves both the voice and video buttons in the header.
  const handleStartCall = useCallback((kind: CallKind = 'voice') => {
    if (!canCall) return
    if (voiceCall.state !== 'idle' && voiceCall.state !== 'ended') return
    // Reset PiP to the default top-right corner so each new call starts fresh
    // regardless of where the user dragged it during the previous call.
    setPipPosition(null)
    void voiceCall.startCall(kind)
  }, [canCall, voiceCall])

  const handleCallBack = useCallback((entry: MissedCallEntry) => {
    // Dismiss the row first so a successful call doesn't leave an obsolete
    // missed-call entry behind in the message list.
    setMissedCalls(prev => prev.filter(m => m.callId !== entry.callId))
    // Use the kind from the original missed call so a video call-back
    // correctly starts as video, not a downgraded voice call.
    handleStartCall(entry.kind)
  }, [handleStartCall])

  const dismissMissedCall = useCallback((callId: string) => {
    setMissedCalls(prev => prev.filter(m => m.callId !== callId))
  }, [])

  // handleAcceptCall resets the PiP position before accepting so the local
  // preview always starts from the default top-right corner for incoming calls,
  // matching the behaviour of outgoing calls (handleStartCall above).
  const handleAcceptCall = useCallback(() => {
    setPipPosition(null)
    void voiceCall.acceptCall()
  }, [voiceCall])

  // PiP drag handlers. Use Pointer Events so touch + mouse share one code
  // path; setPointerCapture keeps the move/up events targeted even if the
  // pointer briefly leaves the small PiP element while dragging.
  const handlePipPointerDown = useCallback((e: PointerEvent<HTMLDivElement>) => {
    // Only initiate drag for primary button / single touch.
    if (e.button !== 0 && e.pointerType === 'mouse') return
    const target = e.currentTarget
    const rect = target.getBoundingClientRect()
    pipDragRef.current = {
      offsetX: e.clientX - rect.left,
      offsetY: e.clientY - rect.top,
      pointerId: e.pointerId,
    }
    try { target.setPointerCapture(e.pointerId) } catch { /* fine */ }
    e.preventDefault()
  }, [])

  const handlePipPointerMove = useCallback((e: PointerEvent<HTMLDivElement>) => {
    const drag = pipDragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    const target = e.currentTarget
    const rect = target.getBoundingClientRect()
    // Clamp to viewport so the PiP can't be dragged off-screen.
    const viewW = typeof window !== 'undefined' ? window.innerWidth : rect.width
    const viewH = typeof window !== 'undefined' ? window.innerHeight : rect.height
    const rawX = e.clientX - drag.offsetX
    const rawY = e.clientY - drag.offsetY
    const clampedX = Math.max(8, Math.min(viewW - rect.width - 8, rawX))
    const clampedY = Math.max(8, Math.min(viewH - rect.height - 8, rawY))
    setPipPosition({ x: clampedX, y: clampedY })
  }, [])

  const handlePipPointerUp = useCallback((e: PointerEvent<HTMLDivElement>) => {
    const drag = pipDragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    pipDragRef.current = null
    try { e.currentTarget.releasePointerCapture(e.pointerId) } catch { /* fine */ }
  }, [])

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
      <header className="flex items-center gap-2 px-3 sm:px-4 py-3 border-b border-gray-800 bg-gray-950 shrink-0">
        <button
          type="button"
          onClick={onBack}
          aria-label={t('chat.back')}
          className="md:hidden p-1.5 -ml-1 text-gray-300 hover:text-white rounded-md cursor-pointer"
        >
          <ChevronLeft size={20} aria-hidden="true" />
        </button>
        <div className="flex-1 min-w-0">
          <h2 className="text-base sm:text-lg font-semibold text-white truncate">
            {loading && !conversation ? (
              <Skeleton className="h-5 w-40" />
            ) : (
              conversation?.name || t('unnamedConversation')
            )}
          </h2>
          {memberChips.length > 0 && (
            <ul
              className="flex flex-wrap gap-1.5 mt-1.5"
              aria-label={t('chat.membersLabel')}
              role="list"
            >
              {memberChips.map(chip => (
                <li
                  key={chip.id}
                  className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs border ${
                    chip.isSelf
                      ? 'bg-blue-500/15 border-blue-500/40 text-blue-200'
                      : 'bg-gray-800 border-gray-700 text-gray-300'
                  }`}
                >
                  <span aria-hidden="true">{chip.emoji}</span>
                  <span className="truncate max-w-[10rem]">{chip.label}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
        {connStatus === 'reconnecting' && (
          <span
            className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs bg-amber-500/15 border border-amber-500/40 text-amber-200 shrink-0"
            role="status"
            aria-live="polite"
            title={t('chat.connection.reconnecting')}
            data-testid="family-chat-reconnecting"
          >
            <WifiOff size={12} aria-hidden="true" />
            <span className="truncate max-w-[8rem] sm:max-w-none">{t('chat.connection.reconnecting')}</span>
          </span>
        )}
        {connStatus === 'live' && justReconnected && (
          <span
            className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs bg-green-500/15 border border-green-500/40 text-green-200 shrink-0"
            role="status"
            aria-live="polite"
            title={t('chat.connection.live')}
            data-testid="family-chat-connected"
          >
            <Wifi size={12} aria-hidden="true" />
            <span className="truncate max-w-[8rem] sm:max-w-none">{t('chat.connection.live')}</span>
          </span>
        )}
        {canCall && (
          <>
            <button
              type="button"
              onClick={() => handleStartCall('voice')}
              disabled={voiceCall.state !== 'idle' && voiceCall.state !== 'ended'}
              aria-label={t('call.start')}
              title={t('call.start')}
              className="shrink-0 p-2 rounded-full text-green-300 hover:text-green-200 hover:bg-green-500/15 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
              data-testid="family-chat-call-button"
            >
              <Phone size={20} aria-hidden="true" />
            </button>
            <button
              type="button"
              onClick={() => handleStartCall('video')}
              disabled={voiceCall.state !== 'idle' && voiceCall.state !== 'ended'}
              aria-label={t('call.startVideo')}
              title={t('call.startVideo')}
              className="shrink-0 p-2 rounded-full text-blue-300 hover:text-blue-200 hover:bg-blue-500/15 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
              data-testid="family-chat-video-call-button"
            >
              <Video size={20} aria-hidden="true" />
            </button>
          </>
        )}
        {canGroupCall && (
          <>
            <button
              type="button"
              onClick={() => handleStartGroupCall('voice')}
              disabled={groupCall.state === 'active'}
              aria-label={t('call.group.start')}
              title={t('call.group.start')}
              className="shrink-0 p-2 rounded-full text-green-300 hover:text-green-200 hover:bg-green-500/15 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
              data-testid="family-chat-group-call-button"
            >
              <Phone size={20} aria-hidden="true" />
            </button>
            <button
              type="button"
              onClick={() => handleStartGroupCall('video')}
              disabled={groupCall.state === 'active'}
              aria-label={t('call.group.startVideo')}
              title={t('call.group.startVideo')}
              className="shrink-0 p-2 rounded-full text-blue-300 hover:text-blue-200 hover:bg-blue-500/15 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
              data-testid="family-chat-group-video-call-button"
            >
              <Video size={20} aria-hidden="true" />
            </button>
          </>
        )}
      </header>

      <div
        className="flex-1 min-h-0 overflow-y-auto px-3 sm:px-4 py-3 space-y-2"
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

        {!loading && !error && messages.length === 0 && (
          <div className="flex flex-col items-center justify-center h-full text-center text-gray-500 py-12">
            <MessageSquare size={32} className="mb-2 text-gray-600" aria-hidden="true" />
            <p className="text-sm">{t('chat.emptyMessages')}</p>
          </div>
        )}

        {!loading && !error && messages.map(msg => {
          const isOwn = user?.id === msg.sender_user_id
          const isDeleted = !!msg.deleted_at
          const isEditing = editingMsgId === msg.id
          const senderInfo = memberLookup.get(msg.sender_user_id)
          const senderLabel = senderInfo?.label ?? t('chat.memberFallback', { id: msg.sender_user_id })
          const relative = formatRelative(msg.created_at, rtf, t('time.justNow'))
          const attachmentUrl = !isDeleted && msg.attachment_path && msg.attachment_mime
            ? `/api/familychat/conversations/${msg.conversation_id}/attachments/${msg.id}`
            : ''
          const mime = msg.attachment_mime ?? ''
          const isImage = mime.startsWith('image/')
          const isAudio = mime.startsWith('audio/')
          // A voice note is an audio/webm attachment with an empty body —
          // the recorder always ships these as standalone bubbles. The
          // bubble renders a precomputed waveform if meta_json carries one;
          // it falls back to a localStorage cache (written immediately
          // after upload by the recorder) and finally to a flat waveform.
          const isVoiceNote = !isDeleted && !!attachmentUrl
            && (mime.startsWith('audio/webm') || mime.startsWith('audio/ogg'))
            && !msg.body.trim()
          const cachedWaveform = isVoiceNote
            ? (parseVoiceMeta(msg.meta_json) ?? readCachedWaveform(msg.id))
            : null
          const voiceBars = cachedWaveform?.bars ?? new Array(DEFAULT_BAR_COUNT).fill(0)
          const voiceDurationMs = cachedWaveform?.durationMs ?? 0
          const pickerOpen = pickerForMsgId === msg.id
          const menuOpen = menuForMsgId === msg.id
          // Optimistic bubbles (still sending or failed) have no authoritative
          // id yet, so reactions and edit/delete are suppressed until the row
          // reconciles to the persisted message.
          const isPending = msg.status === 'sending' || msg.status === 'failed'
          const showActions = isOwn && !isDeleted && !isEditing && !isPending
          const deletedByInfo = msg.deleted_by != null ? memberLookup.get(msg.deleted_by) : undefined
          const deletedByLabel = msg.deleted_by != null && user?.id === msg.deleted_by
            ? t('edit.tombstoneSelf')
            : t('edit.tombstone', { name: deletedByInfo?.label ?? t('chat.memberFallback', { id: msg.deleted_by ?? 0 }) })
          return (
            <div
              key={msg.client_id ?? msg.id}
              className={`flex flex-col group ${isOwn ? 'items-end' : 'items-start'}`}
              data-testid={`chat-bubble-${msg.id}`}
            >
              {!isOwn && !isDeleted && (
                <span className="text-xs text-gray-400 mb-0.5 px-1">{senderLabel}</span>
              )}
              <div className={`relative max-w-[85%] sm:max-w-[70%]`}>
                {isDeleted ? (
                  <div
                    className="px-3 py-2 rounded-2xl text-sm italic bg-gray-800/60 border border-gray-700 text-gray-400"
                    data-testid={`chat-tombstone-${msg.id}`}
                  >
                    {deletedByLabel}
                  </div>
                ) : isEditing ? (
                  <div className={`px-3 py-2 rounded-2xl text-sm break-words ${
                    isOwn ? 'bg-blue-600/40 border border-blue-500' : 'bg-gray-800 border border-gray-700'
                  }`}>
                    <textarea
                      value={editingDraft}
                      onChange={(e) => setEditingDraft(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Escape') {
                          e.preventDefault()
                          cancelEditor()
                        } else if (e.key === 'Enter' && !e.shiftKey) {
                          e.preventDefault()
                          void saveEditor(msg.id)
                        }
                      }}
                      aria-label={t('edit.edit')}
                      data-testid={`chat-edit-input-${msg.id}`}
                      className="w-full bg-gray-900 text-gray-100 border border-gray-700 rounded-lg px-2 py-1 text-sm focus:outline-none focus:border-blue-500"
                      rows={3}
                      autoFocus
                    />
                    {editingError && (
                      <div className="text-xs text-red-400 mt-1">{editingError}</div>
                    )}
                    <div className="flex gap-2 mt-2 justify-end">
                      <button
                        type="button"
                        onClick={cancelEditor}
                        className="px-2 py-1 text-xs rounded-md bg-gray-700 text-gray-200 hover:bg-gray-600"
                        data-testid={`chat-edit-cancel-${msg.id}`}
                      >
                        {t('edit.cancel')}
                      </button>
                      <button
                        type="button"
                        onClick={() => { void saveEditor(msg.id) }}
                        disabled={editingSaving || !editingDraft.trim()}
                        className="px-2 py-1 text-xs rounded-md bg-blue-600 text-white hover:bg-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                        data-testid={`chat-edit-save-${msg.id}`}
                      >
                        {editingSaving ? t('edit.saving') : t('edit.save')}
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
                        setPickerForMsgId(msg.id)
                      }
                    }}
                  >
                    {attachmentUrl && isImage && (
                      <button
                        type="button"
                        onClick={() => setLightbox({ url: attachmentUrl, alt: t('chat.attachmentImageAlt') })}
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
                      }
                      setPickerForMsgId(willOpen ? msg.id : null)
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
                    onClick={() => setMenuForMsgId(menuOpen ? null : msg.id)}
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
                      onClick={() => setMenuForMsgId(null)}
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
                        onClick={() => openEditor(msg)}
                        className="w-full text-left px-3 py-2 text-sm text-gray-200 hover:bg-gray-700"
                        data-testid={`chat-edit-action-${msg.id}`}
                      >
                        {t('edit.edit')}
                      </button>
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => {
                          setMenuForMsgId(null)
                          setDeleteError('')
                          setConfirmDeleteId(msg.id)
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
                    onPick={(emoji) => handlePickFromPicker(msg.id, emoji)}
                    onClose={() => setPickerForMsgId(null)}
                    anchorRef={pickerAnchorRef}
                    triggerRef={pickerGuardRef}
                  />
                )}
              </div>
              <ReactionChips
                reactions={msg.reactions}
                onToggle={(emoji, mine) => { void toggleReaction(msg.id, emoji, mine) }}
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
                    onClick={() => retryFailedMessage(msg)}
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
        })}

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

        <div ref={messagesEndRef} />
      </div>

      <div className="border-t border-gray-800 bg-gray-950 shrink-0">
        {user && (
          <Composer
            conversationId={conversationId}
            currentUserId={user.id}
            onMessageCreated={handleMessageCreated}
            onOptimisticMessage={handleOptimisticMessage}
            onMessageFailed={handleMessageFailed}
            retryRef={composerRetryRef}
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
          onClick={(e) => { if (e.target === e.currentTarget) setConfirmDeleteId(null) }}
          data-testid="chat-delete-confirm"
        >
          <div className="bg-gray-900 border border-gray-700 rounded-lg max-w-md w-full p-4 shadow-xl">
            <p id="family-chat-confirm-delete-title" className="text-sm text-gray-100">
              {t('edit.confirmDelete')}
            </p>
            {deleteError && (
              <p className="mt-2 text-xs text-red-400">{deleteError}</p>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setConfirmDeleteId(null)}
                className="px-3 py-1.5 text-sm rounded-md bg-gray-800 text-gray-200 hover:bg-gray-700"
                data-testid="chat-delete-cancel"
              >
                {t('edit.cancel')}
              </button>
              <button
                ref={deleteConfirmBtnRef}
                type="button"
                onClick={() => { void confirmDelete(confirmDeleteId) }}
                className="px-3 py-1.5 text-sm rounded-md bg-red-600 text-white hover:bg-red-500"
                data-testid="chat-delete-confirm-button"
              >
                {t('edit.delete')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Hidden audio sink for the remote peer's stream. Kept outside the
          conditional overlays so the element survives state transitions and
          srcObject assignment isn't fighting React re-renders. */}
      <audio ref={remoteAudioRef} autoPlay playsInline className="hidden" />

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
        memberLabel={groupMemberLabel}
        memberInfo={groupMemberInfo}
        selfEmoji={selfEmoji}
      />

      {voiceCall.state === 'incoming-ringing' && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="family-chat-incoming-call-title"
          className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-black/85 text-white p-6"
          data-testid="family-chat-incoming-overlay"
        >
          <div className="flex flex-col items-center text-center max-w-sm">
            <div className="mb-6 p-5 rounded-full bg-green-500/20 animate-pulse">
              {voiceCall.callKind === 'video'
                ? <Video size={48} aria-hidden="true" className="text-blue-300" />
                : <PhoneIncoming size={48} aria-hidden="true" className="text-green-300" />}
            </div>
            <p
              className="text-sm uppercase tracking-wide text-gray-400"
              data-testid="family-chat-incoming-kind-label"
            >
              {voiceCall.callKind === 'video'
                ? t('call.incomingVideoLabel')
                : t('call.incomingLabel')}
            </p>
            <h2
              id="family-chat-incoming-call-title"
              className="mt-2 text-2xl font-semibold"
            >
              {incomingCallerLabel}
            </h2>
            <div className="mt-8 flex items-center justify-center gap-6">
              <button
                type="button"
                onClick={() => { void voiceCall.rejectCall() }}
                aria-label={t('call.decline')}
                className="flex flex-col items-center gap-1 text-red-300 hover:text-red-200 cursor-pointer"
                data-testid="family-chat-call-decline"
              >
                <span className="p-4 rounded-full bg-red-600 text-white hover:bg-red-500">
                  <PhoneOff size={28} aria-hidden="true" />
                </span>
                <span className="text-xs">{t('call.decline')}</span>
              </button>
              <button
                type="button"
                onClick={handleAcceptCall}
                aria-label={t('call.accept')}
                className="flex flex-col items-center gap-1 text-green-300 hover:text-green-200 cursor-pointer"
                data-testid="family-chat-call-accept"
              >
                <span className="p-4 rounded-full bg-green-600 text-white hover:bg-green-500">
                  <Phone size={28} aria-hidden="true" />
                </span>
                <span className="text-xs">{t('call.accept')}</span>
              </button>
            </div>
          </div>
        </div>
      )}

      {(voiceCall.state === 'outgoing-ringing' || voiceCall.state === 'active') && voiceCall.callKind === 'voice' && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="family-chat-active-call-title"
          className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-black/85 text-white p-6"
          data-testid="family-chat-active-overlay"
        >
          <div className="flex flex-col items-center text-center max-w-sm w-full">
            <div className="mb-6 p-5 rounded-full bg-green-500/20">
              <Phone size={48} aria-hidden="true" className="text-green-300" />
            </div>
            <h2
              id="family-chat-active-call-title"
              className="text-2xl font-semibold"
            >
              {activeCallPeerLabel}
            </h2>
            <p
              className="mt-2 text-sm text-gray-300"
              data-testid="family-chat-call-status"
            >
              {voiceCall.state === 'outgoing-ringing'
                ? t('call.ringing')
                : formatCallDuration(callElapsedSec)}
            </p>
            {voiceCall.error && (
              <p className="mt-2 text-xs text-red-400">{voiceCall.error}</p>
            )}
            <div className="mt-8 flex items-center justify-center gap-4">
              <button
                type="button"
                onClick={() => voiceCall.setMuted(!voiceCall.muted)}
                aria-label={voiceCall.muted ? t('call.unmute') : t('call.mute')}
                aria-pressed={voiceCall.muted}
                disabled={voiceCall.state !== 'active'}
                className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                  voiceCall.muted ? 'text-amber-300' : 'text-gray-200'
                }`}
                data-testid="family-chat-call-mute"
              >
                <span className={`p-3 rounded-full ${
                  voiceCall.muted ? 'bg-amber-500/20' : 'bg-gray-700'
                }`}>
                  {voiceCall.muted
                    ? <MicOff size={24} aria-hidden="true" />
                    : <Mic size={24} aria-hidden="true" />}
                </span>
                <span className="text-xs">
                  {voiceCall.muted ? t('call.unmute') : t('call.mute')}
                </span>
              </button>
              <button
                type="button"
                onClick={() => { void voiceCall.endCall() }}
                aria-label={t('call.hangup')}
                className="flex flex-col items-center gap-1 text-white cursor-pointer"
                data-testid="family-chat-call-hangup"
              >
                <span className="p-4 rounded-full bg-red-600 hover:bg-red-500">
                  <PhoneOff size={28} aria-hidden="true" />
                </span>
                <span className="text-xs">{t('call.hangup')}</span>
              </button>
              <button
                type="button"
                onClick={() => setSpeakerOn(prev => !prev)}
                aria-label={speakerOn ? t('call.speakerOff') : t('call.speakerOn')}
                aria-pressed={speakerOn}
                disabled={voiceCall.state !== 'active'}
                className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                  speakerOn ? 'text-blue-300' : 'text-gray-200'
                }`}
                data-testid="family-chat-call-speaker"
              >
                <span className={`p-3 rounded-full ${
                  speakerOn ? 'bg-blue-500/20' : 'bg-gray-700'
                }`}>
                  {speakerOn
                    ? <Volume2 size={24} aria-hidden="true" />
                    : <VolumeX size={24} aria-hidden="true" />}
                </span>
                <span className="text-xs">
                  {speakerOn ? t('call.speakerOff') : t('call.speakerOn')}
                </span>
              </button>
            </div>
          </div>
        </div>
      )}

      {(voiceCall.state === 'outgoing-ringing' || voiceCall.state === 'active') && voiceCall.callKind === 'video' && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="family-chat-active-video-call-title"
          className="fixed inset-0 z-50 flex flex-col bg-black text-white"
          data-testid="family-chat-active-video-overlay"
        >
          {/* Video panes: full-screen remote with draggable PiP on mobile;
              side-by-side remote + local panes on desktop (md:flex-row). */}
          <div className="relative flex-1 min-h-0 flex flex-col md:flex-row">
            <div className="relative flex-1 min-h-0 bg-gray-950">
              <video
                ref={remoteVideoRef}
                autoPlay
                playsInline
                muted
                aria-label={t('call.remoteVideo')}
                className="absolute inset-0 w-full h-full object-cover"
                data-testid="family-chat-call-remote-video"
              />
              {/* Shown when the remote peer disables their camera. Sits above
                  the frozen video frame so the viewer has a clear indicator. */}
              {!voiceCall.remoteCameraEnabled && (
                <div
                  className="absolute inset-0 flex flex-col items-center justify-center bg-gray-950/80 text-gray-300"
                  data-testid="family-chat-call-remote-camera-off"
                >
                  <VideoOff size={32} aria-hidden="true" />
                  <span className="mt-2 text-sm">{t('call.remoteCameraOff')}</span>
                </div>
              )}
              {/* Translucent header with peer label + status. */}
              <div className="absolute top-0 inset-x-0 p-3 sm:p-4 flex items-start gap-3 bg-gradient-to-b from-black/60 to-transparent">
                <div className="flex-1 min-w-0">
                  <h2
                    id="family-chat-active-video-call-title"
                    className="text-base sm:text-lg font-semibold truncate"
                  >
                    {activeCallPeerLabel}
                  </h2>
                  <p
                    className="text-xs text-gray-300"
                    data-testid="family-chat-call-status"
                  >
                    {voiceCall.state === 'outgoing-ringing'
                      ? t('call.ringing')
                      : formatCallDuration(callElapsedSec)}
                  </p>
                </div>
              </div>

              {/* Mobile PiP local preview. Hidden on md+ where the local pane
                  below takes over. Defaults to top-right; switches to inline
                  style once dragged. */}
              {voiceCall.localStream && (
                <div
                  data-testid="family-chat-call-local-pip"
                  onPointerDown={handlePipPointerDown}
                  onPointerMove={handlePipPointerMove}
                  onPointerUp={handlePipPointerUp}
                  onPointerCancel={handlePipPointerUp}
                  className={`md:hidden absolute touch-none cursor-move w-28 h-40 sm:w-36 sm:h-48 rounded-lg overflow-hidden border border-gray-700 bg-gray-900 shadow-lg ${
                    activePipPosition === null ? 'top-4 right-4' : ''
                  }`}
                  style={activePipPosition === null ? undefined : { top: activePipPosition.y, left: activePipPosition.x }}
                >
                  <video
                    ref={localVideoRef}
                    autoPlay
                    playsInline
                    muted
                    aria-label={t('call.localPreview')}
                    className="w-full h-full object-cover scale-x-[-1]"
                    data-testid="family-chat-call-local-video"
                  />
                  {!voiceCall.cameraEnabled && (
                    <div
                      className="absolute inset-0 flex items-center justify-center bg-gray-900/80 text-xs text-gray-300"
                      data-testid="family-chat-call-local-camera-off"
                    >
                      <VideoOff size={18} aria-hidden="true" />
                    </div>
                  )}
                </div>
              )}

              {voiceCall.error && (
                <div className="absolute bottom-24 left-4 right-4 sm:left-auto sm:right-4 sm:max-w-sm">
                  <p className="text-xs text-red-300 bg-red-900/40 border border-red-700 rounded-md px-2 py-1">
                    {voiceCall.error}
                  </p>
                </div>
              )}
            </div>

            {/* Desktop-only local pane — sits side-by-side with the remote on
                md+ screens, where the mobile PiP is hidden. */}
            {voiceCall.localStream && (
              <div
                data-testid="family-chat-call-local-pane"
                className="hidden md:block relative flex-1 min-h-0 bg-gray-900 border-l border-gray-800"
              >
                <video
                  ref={localVideoDesktopRef}
                  autoPlay
                  playsInline
                  muted
                  aria-label={t('call.localPreview')}
                  className="absolute inset-0 w-full h-full object-cover scale-x-[-1]"
                  data-testid="family-chat-call-local-video-desktop"
                />
                {!voiceCall.cameraEnabled && (
                  <div
                    className="absolute inset-0 flex items-center justify-center bg-gray-900/80 text-sm text-gray-300"
                    data-testid="family-chat-call-local-camera-off-desktop"
                  >
                    <VideoOff size={24} aria-hidden="true" />
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Bottom control bar — full width on mobile, sits across the bottom
              on desktop too (matches the spec). */}
          <div className="shrink-0 bg-gray-950 border-t border-gray-800 px-4 py-3 flex items-center justify-center gap-3 sm:gap-5">
            <button
              type="button"
              onClick={() => voiceCall.setMuted(!voiceCall.muted)}
              aria-label={voiceCall.muted ? t('call.unmute') : t('call.mute')}
              aria-pressed={voiceCall.muted}
              disabled={voiceCall.state !== 'active'}
              className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                voiceCall.muted ? 'text-amber-300' : 'text-gray-200'
              }`}
              data-testid="family-chat-call-mute"
            >
              <span className={`p-3 rounded-full ${
                voiceCall.muted ? 'bg-amber-500/20' : 'bg-gray-700'
              }`}>
                {voiceCall.muted
                  ? <MicOff size={24} aria-hidden="true" />
                  : <Mic size={24} aria-hidden="true" />}
              </span>
              <span className="text-xs hidden sm:inline">
                {voiceCall.muted ? t('call.unmute') : t('call.mute')}
              </span>
            </button>
            <button
              type="button"
              onClick={() => { void voiceCall.setCameraEnabled(!voiceCall.cameraEnabled) }}
              aria-label={voiceCall.cameraEnabled ? t('call.cameraOff') : t('call.cameraOn')}
              aria-pressed={!voiceCall.cameraEnabled}
              disabled={voiceCall.state !== 'active'}
              className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                voiceCall.cameraEnabled ? 'text-gray-200' : 'text-amber-300'
              }`}
              data-testid="family-chat-call-camera"
            >
              <span className={`p-3 rounded-full ${
                voiceCall.cameraEnabled ? 'bg-gray-700' : 'bg-amber-500/20'
              }`}>
                {voiceCall.cameraEnabled
                  ? <Video size={24} aria-hidden="true" />
                  : <VideoOff size={24} aria-hidden="true" />}
              </span>
              <span className="text-xs hidden sm:inline">
                {voiceCall.cameraEnabled ? t('call.cameraOff') : t('call.cameraOn')}
              </span>
            </button>
            <button
              type="button"
              onClick={() => { void voiceCall.switchCamera() }}
              aria-label={t('call.switchCamera')}
              disabled={voiceCall.state !== 'active' || !voiceCall.cameraEnabled}
              className="flex flex-col items-center gap-1 cursor-pointer text-gray-200 disabled:opacity-40 disabled:cursor-not-allowed"
              data-testid="family-chat-call-switch-camera"
            >
              <span className="p-3 rounded-full bg-gray-700">
                <SwitchCamera size={24} aria-hidden="true" />
              </span>
              <span className="text-xs hidden sm:inline">
                {t('call.switchCamera')}
              </span>
            </button>
            <button
              type="button"
              onClick={() => { void voiceCall.endCall() }}
              aria-label={t('call.hangup')}
              className="flex flex-col items-center gap-1 text-white cursor-pointer"
              data-testid="family-chat-call-hangup"
            >
              <span className="p-4 rounded-full bg-red-600 hover:bg-red-500">
                <PhoneOff size={28} aria-hidden="true" />
              </span>
              <span className="text-xs hidden sm:inline">{t('call.hangup')}</span>
            </button>
            <button
              type="button"
              onClick={() => setSpeakerOn(prev => !prev)}
              aria-label={speakerOn ? t('call.speakerOff') : t('call.speakerOn')}
              aria-pressed={speakerOn}
              disabled={voiceCall.state !== 'active'}
              className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                speakerOn ? 'text-blue-300' : 'text-gray-200'
              }`}
              data-testid="family-chat-call-speaker"
            >
              <span className={`p-3 rounded-full ${
                speakerOn ? 'bg-blue-500/20' : 'bg-gray-700'
              }`}>
                {speakerOn
                  ? <Volume2 size={24} aria-hidden="true" />
                  : <VolumeX size={24} aria-hidden="true" />}
              </span>
              <span className="text-xs hidden sm:inline">
                {speakerOn ? t('call.speakerOff') : t('call.speakerOn')}
              </span>
            </button>
          </div>
        </div>
      )}

      {endedCallSummary && voiceCall.state !== 'active' && voiceCall.state !== 'outgoing-ringing' && voiceCall.state !== 'incoming-ringing' && (
        <div
          role="status"
          aria-live="polite"
          className="fixed top-4 left-1/2 -translate-x-1/2 z-50 px-3 py-1.5 rounded-full bg-gray-800/95 border border-gray-700 text-gray-100 text-xs shadow-lg"
          data-testid="family-chat-call-ended"
        >
          {t('call.ended', { duration: formatCallDuration(endedCallSummary.durationSec) })}
        </div>
      )}
    </div>
  )
}
