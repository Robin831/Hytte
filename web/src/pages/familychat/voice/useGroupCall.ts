import { useCallback, useEffect, useRef, useState } from 'react'
import type { CallKind, CallSignalPayload } from './useVoiceCall'

// useGroupCall manages a WebRTC *mesh* for group (3+ member) Family Chat calls.
// Unlike useVoiceCall (a single 1:1 RTCPeerConnection) this hook holds one
// RTCPeerConnection per remote participant: with N people in the call each
// client maintains N-1 connections. A mesh is the right trade-off for small
// family groups — it needs no media server (the existing coturn STUN/TURN is
// enough), at the cost of bandwidth that grows with the participant count.
//
// Signalling reuses the same relay endpoints as 1:1 calls but addresses each
// frame at a single peer via `to_user_id`, plus two new lifecycle events:
//   - call_join: a member entered the call. The relay also returns, from the
//     join POST, the list of peers already present so the joiner can dial them.
//   - call_leave: a member left; peers tear down that one connection.
//
// Glare is avoided deterministically: for any pair, the participant with the
// LOWER user id creates the offer and the higher id waits for it. This means
// exactly one offer per pair regardless of join order.
//
// The hook is UI-agnostic and test-friendly: it accepts injectable
// RTCPeerConnection / getUserMedia factories and is driven entirely through
// handleSignalEvent so tests need no real SSE source.

export type GroupCallState = 'idle' | 'active'

// GroupParticipant is one remote peer rendered as a tile in the call grid.
// stream is the remote MediaStream once tracks arrive; cameraEnabled mirrors
// the peer's video on/off state via the received video track's mute/unmute.
export interface GroupParticipant {
  userId: number
  stream: MediaStream | null
  cameraEnabled: boolean
}

// IncomingGroupCall is surfaced to a member who is not yet in an active group
// call so the UI can show a "join the call" banner.
export interface IncomingGroupCall {
  callId: string
  kind: CallKind
  fromUserId: number
}

export type GroupSignalEventName =
  | 'call_offer'
  | 'call_answer'
  | 'call_ice'
  | 'call_end'
  | 'call_join'
  | 'call_leave'

interface PeerEntry {
  pc: RTCPeerConnection
  pendingCandidates: RTCIceCandidateInit[]
  remoteDescriptionSet: boolean
  videoSender: RTCRtpSender | null
}

export interface UseGroupCallOptions {
  conversationId: number | null
  userId: number | null
  rtcPeerConnectionFactory?: (config: RTCConfiguration) => RTCPeerConnection
  getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream>
  generateCallId?: () => string
}

export interface UseGroupCallApi {
  state: GroupCallState
  callId: string | null
  callKind: CallKind
  incomingCall: IncomingGroupCall | null
  participants: GroupParticipant[]
  localStream: MediaStream | null
  muted: boolean
  cameraEnabled: boolean
  error: string | null
  startCall: (kind?: CallKind) => Promise<void>
  joinCall: () => Promise<void>
  declineCall: () => void
  leaveCall: () => Promise<void>
  setMuted: (muted: boolean) => void
  setCameraEnabled: (enabled: boolean) => Promise<void>
  handleSignalEvent: (event: GroupSignalEventName, payload: CallSignalPayload) => Promise<void>
}

const VIDEO_WIDTH = 640
const VIDEO_HEIGHT = 480

function defaultGenerateCallId(): string {
  const c = typeof crypto !== 'undefined' ? crypto : undefined
  if (c && typeof c.randomUUID === 'function') return c.randomUUID()
  const bytes = new Uint8Array(16)
  if (c && typeof c.getRandomValues === 'function') c.getRandomValues(bytes)
  else for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256)
  return Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
}

function mediaConstraints(kind: CallKind): MediaStreamConstraints {
  if (kind === 'voice') return { audio: true }
  return {
    audio: true,
    video: { width: { ideal: VIDEO_WIDTH }, height: { ideal: VIDEO_HEIGHT } },
  }
}

interface ICEFetchResult {
  iceServers: RTCIceServer[]
  ttl?: number
}

async function fetchICEConfig(signal?: AbortSignal): Promise<ICEFetchResult> {
  const res = await fetch('/api/familychat/turn', { credentials: 'include', signal })
  if (!res.ok) throw new Error(`turn config failed: ${res.status}`)
  return res.json()
}

// postSignal relays one mesh frame addressed at a single peer. `to` is always
// set for group calls so the server's fan-out reaches every member but only the
// addressed one acts on it.
async function postSignal(
  convId: number,
  callId: string,
  kind: 'offer' | 'answer' | 'ice',
  to: number,
  data: unknown,
  extra?: Record<string, unknown>,
): Promise<void> {
  const body: Record<string, unknown> = { data, to_user_id: to }
  if (extra) for (const k of Object.keys(extra)) body[k] = extra[k]
  const res = await fetch(
    `/api/familychat/conversations/${convId}/calls/${callId}/${kind}`,
    {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
  if (!res.ok) throw new Error(`signal ${kind} failed: ${res.status}`)
}

// postJoin announces our entry and returns the peers already in the call.
async function postJoin(convId: number, callId: string, kind: CallKind): Promise<number[]> {
  const res = await fetch(
    `/api/familychat/conversations/${convId}/calls/${callId}/join`,
    {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind }),
    },
  )
  if (!res.ok) throw new Error(`join failed: ${res.status}`)
  const out = await res.json() as { participants?: number[] }
  return Array.isArray(out.participants) ? out.participants : []
}

async function postLeave(convId: number, callId: string): Promise<void> {
  await fetch(
    `/api/familychat/conversations/${convId}/calls/${callId}/leave`,
    { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: '{}' },
  ).catch(() => {})
}

function defaultPeerConnectionFactory(config: RTCConfiguration): RTCPeerConnection {
  const Ctor = (globalThis as unknown as { RTCPeerConnection?: typeof RTCPeerConnection }).RTCPeerConnection
  if (!Ctor) throw new Error('RTCPeerConnection unavailable')
  return new Ctor(config)
}

function defaultGetUserMedia(constraints: MediaStreamConstraints): Promise<MediaStream> {
  if (typeof navigator === 'undefined' || !navigator.mediaDevices?.getUserMedia) {
    return Promise.reject(new Error('getUserMedia unavailable'))
  }
  return navigator.mediaDevices.getUserMedia(constraints)
}

export function useGroupCall(options: UseGroupCallOptions): UseGroupCallApi {
  const {
    conversationId,
    userId,
    rtcPeerConnectionFactory = defaultPeerConnectionFactory,
    getUserMedia = defaultGetUserMedia,
    generateCallId = defaultGenerateCallId,
  } = options

  const [state, setState] = useState<GroupCallState>('idle')
  const [callId, setCallId] = useState<string | null>(null)
  const [callKind, setCallKind] = useState<CallKind>('voice')
  const [incomingCall, setIncomingCall] = useState<IncomingGroupCall | null>(null)
  const [participants, setParticipants] = useState<GroupParticipant[]>([])
  const [localStream, setLocalStreamState] = useState<MediaStream | null>(null)
  const [muted, setMutedState] = useState(false)
  const [cameraEnabled, setCameraEnabledState] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const peersRef = useRef<Map<number, PeerEntry>>(new Map())
  const localStreamRef = useRef<MediaStream | null>(null)
  const callIdRef = useRef<string | null>(null)
  const callKindRef = useRef<CallKind>('voice')
  const stateRef = useRef<GroupCallState>('idle')
  const iceServersRef = useRef<RTCIceServer[]>([])
  // callEpochRef invalidates async continuations after a leave/teardown so a
  // late getUserMedia or createOffer cannot resurrect a torn-down call.
  const callEpochRef = useRef(0)
  const optionsRef = useRef({ rtcPeerConnectionFactory, getUserMedia, generateCallId, userId })
  useEffect(() => {
    optionsRef.current = { rtcPeerConnectionFactory, getUserMedia, generateCallId, userId }
  })

  const setLocalStream = useCallback((next: MediaStream | null) => {
    localStreamRef.current = next
    setLocalStreamState(next)
  }, [])

  const updateState = useCallback((next: GroupCallState) => {
    stateRef.current = next
    setState(next)
  }, [])

  const updateCallId = useCallback((next: string | null) => {
    callIdRef.current = next
    setCallId(next)
  }, [])

  const updateCallKind = useCallback((next: CallKind) => {
    callKindRef.current = next
    setCallKind(next)
  }, [])

  // upsertParticipant merges a patch into the tile for userId, inserting a new
  // tile (sorted by id for stable ordering) when none exists.
  const upsertParticipant = useCallback((id: number, patch: Partial<GroupParticipant>) => {
    setParticipants(prev => {
      const idx = prev.findIndex(p => p.userId === id)
      if (idx === -1) {
        const next = [...prev, { userId: id, stream: null, cameraEnabled: true, ...patch }]
        next.sort((a, b) => a.userId - b.userId)
        return next
      }
      const next = prev.slice()
      next[idx] = { ...next[idx], ...patch }
      return next
    })
  }, [])

  const removeParticipant = useCallback((id: number) => {
    setParticipants(prev => prev.filter(p => p.userId !== id))
  }, [])

  // closePeer tears down a single peer connection (one mesh edge) and removes
  // its tile. Safe to call for an unknown id.
  const closePeer = useCallback((id: number) => {
    const entry = peersRef.current.get(id)
    if (entry) {
      const { pc } = entry
      pc.ontrack = null
      pc.onicecandidate = null
      pc.onconnectionstatechange = null
      try { pc.close() } catch { /* already closed */ }
      peersRef.current.delete(id)
    }
    removeParticipant(id)
  }, [removeParticipant])

  // tearDown closes every peer connection, stops local capture, and resets all
  // state. Used by leaveCall and the unmount cleanup.
  const tearDown = useCallback(() => {
    callEpochRef.current++
    for (const id of Array.from(peersRef.current.keys())) {
      closePeer(id)
    }
    peersRef.current.clear()
    const prev = localStreamRef.current
    setLocalStream(null)
    if (prev) {
      for (const t of prev.getTracks()) { try { t.stop() } catch { /* ok */ } }
    }
    setParticipants([])
    setMutedState(false)
    setCameraEnabledState(true)
    setIncomingCall(null)
    callKindRef.current = 'voice'
    setCallKind('voice')
    updateCallId(null)
    updateState('idle')
  }, [closePeer, setLocalStream, updateCallId, updateState])

  const addLocalTracks = useCallback((pc: RTCPeerConnection, entry: PeerEntry) => {
    const stream = localStreamRef.current
    if (!stream) return
    for (const track of stream.getTracks()) {
      const sender = pc.addTrack(track, stream)
      if (track.kind === 'video') entry.videoSender = sender
    }
  }, [])

  const wirePeer = useCallback((pc: RTCPeerConnection, peerId: number, entry: PeerEntry) => {
    pc.ontrack = (event: RTCTrackEvent) => {
      const stream = event.streams?.[0] ?? new MediaStream([event.track])
      upsertParticipant(peerId, { stream })
      if (event.track.kind === 'video') {
        event.track.onmute = () => upsertParticipant(peerId, { cameraEnabled: false })
        event.track.onunmute = () => upsertParticipant(peerId, { cameraEnabled: true })
      }
    }
    pc.onicecandidate = (event: RTCPeerConnectionIceEvent) => {
      if (!event.candidate) return
      if (conversationId === null || callIdRef.current === null) return
      const init = typeof event.candidate.toJSON === 'function'
        ? event.candidate.toJSON()
        : ({
            candidate: event.candidate.candidate,
            sdpMid: event.candidate.sdpMid,
            sdpMLineIndex: event.candidate.sdpMLineIndex,
            usernameFragment: event.candidate.usernameFragment,
          } as RTCIceCandidateInit)
      postSignal(conversationId, callIdRef.current, 'ice', peerId, init).catch(err => {
        if (typeof console !== 'undefined') console.warn('group call: ice relay failed', err)
      })
    }
    pc.onconnectionstatechange = () => {
      const cs = pc.connectionState
      if ((cs === 'failed' || cs === 'closed') && peersRef.current.get(peerId) === entry) {
        // Drop just this edge; the rest of the mesh stays up.
        closePeer(peerId)
      }
    }
  }, [conversationId, upsertParticipant, closePeer])

  // createPeerAndOffer dials a peer we should initiate to (our id is lower).
  const createPeerAndOffer = useCallback(async (peerId: number) => {
    if (conversationId === null) return
    const theCallId = callIdRef.current
    if (theCallId === null) return
    if (peersRef.current.has(peerId)) return
    const epoch = callEpochRef.current
    const pc = optionsRef.current.rtcPeerConnectionFactory({ iceServers: iceServersRef.current })
    const entry: PeerEntry = { pc, pendingCandidates: [], remoteDescriptionSet: false, videoSender: null }
    peersRef.current.set(peerId, entry)
    upsertParticipant(peerId, {})
    addLocalTracks(pc, entry)
    wirePeer(pc, peerId, entry)
    try {
      const offer = await pc.createOffer()
      if (callEpochRef.current !== epoch) return
      await pc.setLocalDescription(offer)
      if (callEpochRef.current !== epoch) return
      const sdp = pc.localDescription ?? offer
      await postSignal(conversationId, theCallId, 'offer', peerId, { type: sdp.type, sdp: sdp.sdp }, { kind: callKindRef.current })
    } catch (err) {
      if (typeof console !== 'undefined') console.warn('group call: offer failed', err)
      closePeer(peerId)
    }
  }, [conversationId, addLocalTracks, wirePeer, upsertParticipant, closePeer])

  // answerOffer handles an inbound offer: create the peer (we are the higher
  // id, so we never initiated) and reply with an answer.
  const answerOffer = useCallback(async (peerId: number, offer: RTCSessionDescriptionInit) => {
    if (conversationId === null) return
    const theCallId = callIdRef.current
    if (theCallId === null) return
    const epoch = callEpochRef.current
    let entry = peersRef.current.get(peerId)
    if (!entry) {
      const pc = optionsRef.current.rtcPeerConnectionFactory({ iceServers: iceServersRef.current })
      entry = { pc, pendingCandidates: [], remoteDescriptionSet: false, videoSender: null }
      peersRef.current.set(peerId, entry)
      upsertParticipant(peerId, {})
      addLocalTracks(pc, entry)
      wirePeer(pc, peerId, entry)
    }
    const pc = entry.pc
    try {
      await pc.setRemoteDescription(offer)
      entry.remoteDescriptionSet = true
      const buffered = entry.pendingCandidates
      entry.pendingCandidates = []
      for (const c of buffered) {
        try { await pc.addIceCandidate(c) } catch { /* best effort */ }
      }
      if (callEpochRef.current !== epoch) return
      const answer = await pc.createAnswer()
      if (callEpochRef.current !== epoch) return
      await pc.setLocalDescription(answer)
      if (callEpochRef.current !== epoch) return
      const sdp = pc.localDescription ?? answer
      await postSignal(conversationId, theCallId, 'answer', peerId, { type: sdp.type, sdp: sdp.sdp })
    } catch (err) {
      if (typeof console !== 'undefined') console.warn('group call: answer failed', err)
      closePeer(peerId)
    }
  }, [conversationId, addLocalTracks, wirePeer, upsertParticipant, closePeer])

  // enterCall acquires media, joins the room, and dials the peers already in
  // it. Shared by startCall (fresh call) and joinCall (an incoming one).
  const enterCall = useCallback(async (theCallId: string, kind: CallKind) => {
    if (conversationId === null) {
      setError('no-conversation')
      return
    }
    if (stateRef.current === 'active') return
    setError(null)
    const epoch = callEpochRef.current
    updateCallId(theCallId)
    updateCallKind(kind)
    setMutedState(false)
    setCameraEnabledState(true)
    updateState('active')
    setIncomingCall(null)
    try {
      const ice = await fetchICEConfig()
      if (callEpochRef.current !== epoch) return
      iceServersRef.current = ice.iceServers ?? []

      const stream = await optionsRef.current.getUserMedia(mediaConstraints(kind))
      if (callEpochRef.current !== epoch) {
        for (const t of stream.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      setLocalStream(stream)

      const peers = await postJoin(conversationId, theCallId, kind)
      if (callEpochRef.current !== epoch) return
      const me = optionsRef.current.userId
      for (const peerId of peers) {
        // Deterministic glare avoidance: only the lower id offers; the higher
        // id waits for the offer (handled in handleSignalEvent).
        if (me !== null && me !== undefined && me < peerId) {
          void createPeerAndOffer(peerId)
        }
      }
    } catch (err) {
      if (callEpochRef.current !== epoch) return
      setError(err instanceof Error ? err.message : 'call-failed')
      tearDown()
    }
  }, [conversationId, createPeerAndOffer, setLocalStream, tearDown, updateCallId, updateCallKind, updateState])

  const startCall = useCallback(async (kind: CallKind = 'voice') => {
    if (stateRef.current === 'active') return
    const theCallId = optionsRef.current.generateCallId()
    await enterCall(theCallId, kind)
  }, [enterCall])

  const joinCall = useCallback(async () => {
    const incoming = incomingCall
    if (!incoming) return
    await enterCall(incoming.callId, incoming.kind)
  }, [enterCall, incomingCall])

  const declineCall = useCallback(() => {
    setIncomingCall(null)
  }, [])

  const leaveCall = useCallback(async () => {
    const convId = conversationId
    const theCallId = callIdRef.current
    if (convId !== null && theCallId !== null && stateRef.current === 'active') {
      await postLeave(convId, theCallId)
    }
    tearDown()
  }, [conversationId, tearDown])

  const setMuted = useCallback((next: boolean) => {
    const stream = localStreamRef.current
    setMutedState(next)
    if (!stream) return
    for (const track of stream.getAudioTracks()) track.enabled = !next
  }, [])

  // setCameraEnabled toggles the local video on every peer connection by
  // replacing the video sender's track with null (off) or the live track (on).
  // replaceTrack(null) makes peers see the feed freeze and fires their video
  // track's 'mute' event so their tile shows the camera-off placeholder.
  const setCameraEnabled = useCallback(async (next: boolean) => {
    if (callKindRef.current !== 'video') return
    const stream = localStreamRef.current
    const videoTrack = stream?.getVideoTracks()[0] ?? null
    for (const entry of peersRef.current.values()) {
      if (!entry.videoSender) continue
      try {
        await entry.videoSender.replaceTrack(next ? videoTrack : null)
      } catch {
        if (videoTrack) videoTrack.enabled = next
      }
    }
    if (videoTrack) videoTrack.enabled = next
    setCameraEnabledState(next)
  }, [])

  const handleSignalEvent = useCallback(async (
    event: GroupSignalEventName,
    payload: CallSignalPayload,
  ) => {
    const me = optionsRef.current.userId
    // Ignore our own echoes — the relay fans every frame back to the sender.
    if (me !== null && me !== undefined && payload.from_user_id === me) return
    if (conversationId === null || payload.conversation_id !== conversationId) return
    // Addressed frames (offer/answer/ice) only apply when we are the target.
    const to = (payload as { to_user_id?: number }).to_user_id
    if (to !== undefined && me !== null && me !== undefined && to !== me) return

    const peerId = payload.from_user_id
    const incomingCallId = payload.call_id

    switch (event) {
      case 'call_join': {
        if (stateRef.current === 'active') {
          if (callIdRef.current !== incomingCallId) return
          // A newcomer joined our call. Only the lower id offers.
          if (me !== null && me !== undefined && me < peerId) {
            void createPeerAndOffer(peerId)
          }
          return
        }
        // We are idle — surface a joinable incoming call.
        const kind: CallKind = payload.kind === 'video' ? 'video' : 'voice'
        setIncomingCall({ callId: incomingCallId, kind, fromUserId: peerId })
        return
      }
      case 'call_leave': {
        if (stateRef.current === 'active' && callIdRef.current === incomingCallId) {
          closePeer(peerId)
        }
        return
      }
      case 'call_offer': {
        if (stateRef.current !== 'active' || callIdRef.current !== incomingCallId) return
        const data = payload.data as { type?: RTCSdpType; sdp?: string } | undefined
        if (!data || !data.sdp) return
        await answerOffer(peerId, { type: data.type ?? 'offer', sdp: data.sdp })
        return
      }
      case 'call_answer': {
        if (stateRef.current !== 'active' || callIdRef.current !== incomingCallId) return
        const entry = peersRef.current.get(peerId)
        const data = payload.data as { type?: RTCSdpType; sdp?: string } | undefined
        if (!entry || !data || !data.sdp) return
        try {
          await entry.pc.setRemoteDescription({ type: data.type ?? 'answer', sdp: data.sdp })
          entry.remoteDescriptionSet = true
          const buffered = entry.pendingCandidates
          entry.pendingCandidates = []
          for (const c of buffered) {
            try { await entry.pc.addIceCandidate(c) } catch { /* best effort */ }
          }
        } catch (err) {
          if (typeof console !== 'undefined') console.warn('group call: setRemoteDescription failed', err)
          closePeer(peerId)
        }
        return
      }
      case 'call_ice': {
        const cand = payload.data as RTCIceCandidateInit | undefined
        if (!cand) return
        const entry = peersRef.current.get(peerId)
        if (!entry || !entry.remoteDescriptionSet) {
          // Buffer candidates that beat the remote description over the wire.
          if (entry) entry.pendingCandidates.push(cand)
          return
        }
        try { await entry.pc.addIceCandidate(cand) } catch (err) {
          if (typeof console !== 'undefined') console.warn('group call: addIceCandidate failed', err)
        }
        return
      }
      case 'call_end': {
        // Terminal end (room emptied). Clear a stale incoming banner; if we're
        // somehow still active, fold the call down.
        if (callIdRef.current === incomingCallId && stateRef.current === 'active') {
          tearDown()
        } else if (incomingCall && incomingCall.callId === incomingCallId) {
          setIncomingCall(null)
        }
        return
      }
    }
  }, [conversationId, createPeerAndOffer, answerOffer, closePeer, tearDown, incomingCall])

  // Tear down on unmount so navigating away releases the mic/cameras and all
  // peer connections rather than leaking them.
  useEffect(() => {
    return () => { tearDown() }
  }, [tearDown])

  return {
    state,
    callId,
    callKind,
    incomingCall,
    participants,
    localStream,
    muted,
    cameraEnabled,
    error,
    startCall,
    joinCall,
    declineCall,
    leaveCall,
    setMuted,
    setCameraEnabled,
    handleSignalEvent,
  }
}
