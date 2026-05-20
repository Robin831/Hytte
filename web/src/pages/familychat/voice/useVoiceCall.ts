import { useCallback, useEffect, useRef, useState } from 'react'

// useVoiceCall manages the WebRTC lifecycle of a 1:1 voice call between two
// members of a Family Chat conversation. Voice calling is only supported for
// 2-member conversations — the signalling endpoints broadcast to all members
// but this hook tracks a single remoteUserId. Callers should disable the call
// UI (or reject incoming offers) when the conversation has more than 2 members.
//
// It is intentionally UI-agnostic: state transitions and stream handles are
// exposed so an outer component can render the ringing banner, in-call
// controls, and end-of-call summary.
//
// Responsibilities:
//   1. Fetch the STUN/TURN ICE config from /api/familychat/turn before each
//      new call so coturn ephemeral credentials are fresh.
//   2. Manage RTCPeerConnection — createOffer / createAnswer, set local/remote
//      descriptions, trickle ICE candidates.
//   3. POST each signalling artifact (offer, answer, ICE candidate, hang-up)
//      to the relay endpoints provided by the backend.
//   4. Consume call_offer / call_answer / call_ice / call_end SSE events
//      (either via its own subscription or by being fed events through
//      handleSignalEvent) and drive the state machine accordingly.
//   5. Keep an AudioContext "kicked" while the call is active so background
//      tabs do not let the WebRTC pipeline suspend silently.
//
// The hook is deliberately framework-thin: tests inject a fake
// RTCPeerConnection constructor and a stubbed getUserMedia, then call
// handleSignalEvent directly to drive the state machine without standing up
// a real SSE source.

export type VoiceCallState =
  | 'idle'
  | 'outgoing-ringing'
  | 'incoming-ringing'
  | 'active'
  | 'ended'

export type CallSignalEventName =
  | 'call_offer'
  | 'call_answer'
  | 'call_ice'
  | 'call_end'

// CallSignalPayload mirrors the JSON shape emitted by the backend SSE relay
// (see internal/familychat/calls_handlers.go: callRelayPayload). `data` is the
// opaque artifact — SDP for offer/answer, an ICE candidate object for ice, or
// a hang-up envelope for end. `status` is only populated on call_end and lets
// the UI distinguish missed vs ended without an extra query.
export interface CallSignalPayload {
  conversation_id: number
  call_id: string
  from_user_id: number
  data?: unknown
  status?: string
}

export interface ICEServerConfig {
  urls: string[]
  username?: string
  credential?: string
}

export interface ICEFetchResult {
  iceServers: ICEServerConfig[]
  ttl?: number
}

export interface UseVoiceCallOptions {
  conversationId: number | null
  // userId is the local user's id. Used to filter out our own SSE echoes —
  // the relay fans every event to all conversation members including the
  // sender, so without this guard we would react to our own offer/answer.
  userId: number | null
  // Inject a custom RTCPeerConnection constructor. Used by tests to swap in
  // a stub that records method calls. Defaults to globalThis.RTCPeerConnection.
  rtcPeerConnectionFactory?: (config: RTCConfiguration) => RTCPeerConnection
  // Override getUserMedia for tests. Defaults to navigator.mediaDevices.getUserMedia.
  getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream>
  // Skip the hook's internal SSE subscription. The consumer must then feed
  // events via handleSignalEvent. Useful when the surrounding page already
  // has an SSE stream open and wants to fan call events into the hook.
  skipSignalSubscription?: boolean
  // Override the call-id generator. Defaults to crypto.randomUUID with a
  // random-hex fallback for environments that lack it.
  generateCallId?: () => string
}

export interface UseVoiceCallApi {
  state: VoiceCallState
  callId: string | null
  remoteUserId: number | null
  error: string | null
  remoteStream: MediaStream | null
  muted: boolean
  startCall: () => Promise<void>
  acceptCall: () => Promise<void>
  rejectCall: () => Promise<void>
  endCall: () => Promise<void>
  // setMuted flips the local audio track's enabled flag so the peer stops
  // receiving frames without dropping the WebRTC connection. Safe to call in
  // any state — a no-op when no local track exists.
  setMuted: (muted: boolean) => void
  // Drive the call state machine from an external signal source. Exposed so
  // tests can simulate incoming events without a real SSE connection, and so
  // an outer page that already owns an SSE stream can route call_* frames in.
  handleSignalEvent: (event: CallSignalEventName, payload: CallSignalPayload) => Promise<void>
}

function defaultGenerateCallId(): string {
  const c = typeof crypto !== 'undefined' ? crypto : undefined
  if (c && typeof c.randomUUID === 'function') return c.randomUUID()
  // Fallback: 16 random bytes hex-encoded. Not RFC4122 but unique enough for
  // a call-id; the server only requires opaque text ≤ 128 chars.
  const bytes = new Uint8Array(16)
  if (c && typeof c.getRandomValues === 'function') c.getRandomValues(bytes)
  else for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256)
  return Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
}

async function fetchICEConfig(signal?: AbortSignal): Promise<ICEFetchResult> {
  const res = await fetch('/api/familychat/turn', { credentials: 'include', signal })
  if (!res.ok) throw new Error(`turn config failed: ${res.status}`)
  return res.json()
}

async function postSignal(
  convId: number,
  callId: string,
  kind: 'offer' | 'answer' | 'ice' | 'end',
  data: unknown,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(
    `/api/familychat/conversations/${convId}/calls/${callId}/${kind}`,
    {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ data }),
      signal,
    },
  )
  // 204 is the documented success code; any 2xx counts as success in case the
  // backend gains a body in a future revision.
  if (!res.ok) throw new Error(`signal ${kind} failed: ${res.status}`)
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

export function useVoiceCall(options: UseVoiceCallOptions): UseVoiceCallApi {
  const {
    conversationId,
    userId,
    rtcPeerConnectionFactory = defaultPeerConnectionFactory,
    getUserMedia = defaultGetUserMedia,
    skipSignalSubscription = false,
    generateCallId = defaultGenerateCallId,
  } = options

  const [state, setState] = useState<VoiceCallState>('idle')
  const [callId, setCallId] = useState<string | null>(null)
  const [remoteUserId, setRemoteUserId] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [remoteStream, setRemoteStream] = useState<MediaStream | null>(null)
  const [muted, setMutedState] = useState(false)

  // Refs hold values the async signalling callbacks must read without
  // capturing stale closures. React state is used for things the UI needs to
  // re-render on; everything else is a ref.
  const pcRef = useRef<RTCPeerConnection | null>(null)
  // callEpochRef is incremented by tearDown so that any in-flight startCall
  // continuation can detect that the call was ended and bail out rather than
  // re-creating a PeerConnection or POSTing a stale offer.
  const callEpochRef = useRef<number>(0)
  const localStreamRef = useRef<MediaStream | null>(null)
  const callIdRef = useRef<string | null>(null)
  const remoteUserIdRef = useRef<number | null>(null)
  const stateRef = useRef<VoiceCallState>('idle')
  // pendingOfferRef stores an inbound SDP offer waiting for the user to
  // acceptCall(). The hook does not auto-accept — the UI must trigger it.
  const pendingOfferRef = useRef<RTCSessionDescriptionInit | null>(null)
  // pendingRemoteCandidatesRef buffers ICE candidates that arrive before the
  // remote description is set (common when answer SDP and the first trickled
  // candidate race over the relay). They are flushed once setRemoteDescription
  // resolves.
  const pendingRemoteCandidatesRef = useRef<RTCIceCandidateInit[]>([])
  const remoteDescriptionSetRef = useRef<boolean>(false)
  const audioCtxRef = useRef<AudioContext | null>(null)
  const visibilityHandlerRef = useRef<(() => void) | null>(null)
  // optionsRef lets the SSE/visibility effects always see the latest factory
  // overrides without re-running on every render that passes new function
  // references.
  const optionsRef = useRef({ rtcPeerConnectionFactory, getUserMedia, generateCallId, userId })
  useEffect(() => {
    optionsRef.current = { rtcPeerConnectionFactory, getUserMedia, generateCallId, userId }
  })

  const updateState = useCallback((next: VoiceCallState) => {
    stateRef.current = next
    setState(next)
  }, [])

  const updateCallId = useCallback((next: string | null) => {
    callIdRef.current = next
    setCallId(next)
  }, [])

  const updateRemoteUserId = useCallback((next: number | null) => {
    remoteUserIdRef.current = next
    setRemoteUserId(next)
  }, [])

  // tearDown closes the peer connection, stops local tracks, releases the
  // AudioContext, and clears every transient buffer. It is safe to call from
  // any state — every step guards a missing handle.
  const tearDown = useCallback((nextState: VoiceCallState = 'idle') => {
    callEpochRef.current++ // Invalidate any in-flight startCall continuation.
    const pc = pcRef.current
    if (pc) {
      // Drop event listeners before close() to avoid a final connectionstate
      // change racing the "ended" UI transition.
      pc.ontrack = null
      pc.onicecandidate = null
      pc.oniceconnectionstatechange = null
      pc.onconnectionstatechange = null
      try { pc.close() } catch { /* already closed */ }
    }
    pcRef.current = null

    const localStream = localStreamRef.current
    localStreamRef.current = null
    if (localStream) {
      for (const track of localStream.getTracks()) {
        try { track.stop() } catch { /* already stopped */ }
      }
    }

    setRemoteStream(null)
    setMutedState(false)
    pendingOfferRef.current = null
    pendingRemoteCandidatesRef.current = []
    remoteDescriptionSetRef.current = false

    const handler = visibilityHandlerRef.current
    visibilityHandlerRef.current = null
    if (handler && typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', handler)
    }

    const ctx = audioCtxRef.current
    audioCtxRef.current = null
    if (ctx && typeof ctx.close === 'function') {
      void ctx.close().catch(() => {})
    }

    updateRemoteUserId(null)
    updateCallId(null)
    updateState(nextState)
  }, [updateCallId, updateRemoteUserId, updateState])

  // installAudioContextKick keeps a silent AudioContext alive for the
  // duration of the call. Browsers throttle background tabs aggressively; an
  // AudioContext in the 'running' state prevents the WebRTC pipeline from
  // being suspended along with the rest of the tab, which would otherwise
  // drop frames or stall ICE keepalives. The context plays nothing — it just
  // anchors the page in the high-priority media path.
  const installAudioContextKick = useCallback(() => {
    if (audioCtxRef.current) return
    if (typeof window === 'undefined') return
    const Ctor = window.AudioContext
      ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!Ctor) return
    let ctx: AudioContext
    try {
      ctx = new Ctor()
    } catch {
      return
    }
    audioCtxRef.current = ctx
    // Best-effort immediate resume so the context reliably reaches 'running'
    // without waiting for the next visibilitychange — some browsers start it
    // suspended even when created during a user gesture.
    if (typeof ctx.resume === 'function') {
      void ctx.resume().catch(() => {})
    }
    // Connect a 0-gain oscillator to the destination. It produces no audible
    // output but it keeps the audio graph "doing something" so the browser
    // does not park the context.
    try {
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()
      gain.gain.value = 0
      osc.connect(gain)
      gain.connect(ctx.destination)
      osc.start()
    } catch {
      // Some test environments (happy-dom) only stub createMediaStreamSource.
      // Skipping the oscillator is fine — the context itself is the anchor.
    }
    const onVisibility = () => {
      // Resume immediately when the tab regains focus; some browsers auto-
      // suspend a context that was created while hidden.
      if (ctx.state !== 'running' && typeof ctx.resume === 'function') {
        void ctx.resume().catch(() => {})
      }
    }
    visibilityHandlerRef.current = onVisibility
    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', onVisibility)
    }
  }, [])

  // wirePeerConnection attaches the standard listeners used by both legs of
  // the call. Caller-specific behaviour (creating the offer vs the answer) is
  // handled inline by startCall / acceptCall after this returns.
  const wirePeerConnection = useCallback((pc: RTCPeerConnection, convId: number, theCallId: string) => {
    pc.ontrack = (event: RTCTrackEvent) => {
      // Prefer the stream the remote sent; fall back to a synthetic one
      // built from the track when the browser does not expose streams[0].
      const stream = event.streams?.[0]
        ?? new MediaStream([event.track])
      setRemoteStream(stream)
    }

    pc.onicecandidate = (event: RTCPeerConnectionIceEvent) => {
      if (!event.candidate) return
      const init = typeof event.candidate.toJSON === 'function'
        ? event.candidate.toJSON()
        : ({
            candidate: event.candidate.candidate,
            sdpMid: event.candidate.sdpMid,
            sdpMLineIndex: event.candidate.sdpMLineIndex,
            usernameFragment: event.candidate.usernameFragment,
          } as RTCIceCandidateInit)
      postSignal(convId, theCallId, 'ice', init).catch(err => {
        // ICE trickling is best-effort; if a single candidate fails to relay
        // the connection can still succeed via earlier candidates. Surface
        // the error for debugging but don't tear down.
        if (typeof console !== 'undefined') {
          console.warn('voice call: ice relay failed', err)
        }
      })
    }

    pc.onconnectionstatechange = () => {
      const cs = pc.connectionState
      if (cs === 'failed' || cs === 'closed') {
        // Surface a tear-down on transport failure so the UI exits the
        // "active" state even if the remote never sent a call_end.
        if (stateRef.current === 'active' || stateRef.current === 'outgoing-ringing') {
          tearDown('ended')
        }
      }
    }
  }, [tearDown])

  // applyRemoteCandidates flushes any ICE candidates that arrived before
  // setRemoteDescription resolved. Called immediately after each successful
  // setRemoteDescription.
  const flushBufferedCandidates = useCallback(async () => {
    const pc = pcRef.current
    if (!pc) return
    const buffered = pendingRemoteCandidatesRef.current
    pendingRemoteCandidatesRef.current = []
    for (const cand of buffered) {
      try {
        await pc.addIceCandidate(cand)
      } catch (err) {
        if (typeof console !== 'undefined') {
          console.warn('voice call: addIceCandidate failed', err)
        }
      }
    }
  }, [])

  const startCall = useCallback(async () => {
    if (conversationId === null) {
      setError('no-conversation')
      return
    }
    if (stateRef.current !== 'idle' && stateRef.current !== 'ended') {
      // Already in a call or ringing — ignore so a double-click on the call
      // button doesn't open a second peer connection.
      return
    }
    setError(null)
    // Capture the current epoch so every resumed continuation can detect
    // whether endCall/tearDown fired while an async step was in-flight.
    const epoch = callEpochRef.current
    const theCallId = optionsRef.current.generateCallId()
    updateCallId(theCallId)
    updateState('outgoing-ringing')

    try {
      const ice = await fetchICEConfig()
      if (callEpochRef.current !== epoch) return

      const pc = optionsRef.current.rtcPeerConnectionFactory({
        iceServers: ice.iceServers as RTCIceServer[],
      })
      pcRef.current = pc
      wirePeerConnection(pc, conversationId, theCallId)

      const localStream = await optionsRef.current.getUserMedia({ audio: true })
      if (callEpochRef.current !== epoch) {
        // tearDown already ran — release the mic without touching state.
        for (const t of localStream.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      localStreamRef.current = localStream
      for (const track of localStream.getTracks()) {
        pc.addTrack(track, localStream)
      }

      const offer = await pc.createOffer()
      if (callEpochRef.current !== epoch) return
      await pc.setLocalDescription(offer)
      if (callEpochRef.current !== epoch) return
      // Relay the SDP from localDescription (rather than the raw createOffer
      // result) because some implementations massage the SDP during setLocal.
      const sdp = pc.localDescription ?? offer
      await postSignal(conversationId, theCallId, 'offer', {
        type: sdp.type,
        sdp: sdp.sdp,
      })
    } catch (err) {
      if (callEpochRef.current !== epoch) return
      setError(err instanceof Error ? err.message : 'start-failed')
      tearDown('idle')
    }
  }, [conversationId, tearDown, updateCallId, updateState, wirePeerConnection])

  const acceptCall = useCallback(async () => {
    const convId = conversationId
    const theCallId = callIdRef.current
    const offer = pendingOfferRef.current
    if (convId === null || theCallId === null || offer === null) {
      setError('no-pending-call')
      return
    }
    if (stateRef.current !== 'incoming-ringing') return
    setError(null)
    // Capture epoch so continuations can detect if tearDown fired while awaiting.
    const epoch = callEpochRef.current

    try {
      const ice = await fetchICEConfig()
      if (callEpochRef.current !== epoch) return

      const pc = optionsRef.current.rtcPeerConnectionFactory({
        iceServers: ice.iceServers as RTCIceServer[],
      })
      pcRef.current = pc
      wirePeerConnection(pc, convId, theCallId)

      await pc.setRemoteDescription(offer)
      if (callEpochRef.current !== epoch) return
      remoteDescriptionSetRef.current = true
      await flushBufferedCandidates()
      if (callEpochRef.current !== epoch) return

      const localStream = await optionsRef.current.getUserMedia({ audio: true })
      if (callEpochRef.current !== epoch) {
        // tearDown already ran — release the mic without touching state.
        for (const t of localStream.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      localStreamRef.current = localStream
      for (const track of localStream.getTracks()) {
        pc.addTrack(track, localStream)
      }

      const answer = await pc.createAnswer()
      if (callEpochRef.current !== epoch) return
      await pc.setLocalDescription(answer)
      if (callEpochRef.current !== epoch) return
      const sdp = pc.localDescription ?? answer
      await postSignal(convId, theCallId, 'answer', {
        type: sdp.type,
        sdp: sdp.sdp,
      })
      if (callEpochRef.current !== epoch) return

      pendingOfferRef.current = null
      updateState('active')
      installAudioContextKick()
    } catch (err) {
      if (callEpochRef.current !== epoch) return
      setError(err instanceof Error ? err.message : 'accept-failed')
      // Notify the caller that we couldn't pick up so their UI updates.
      if (convId !== null && theCallId !== null) {
        postSignal(convId, theCallId, 'end', { reason: 'accept-failed' }).catch(() => {})
      }
      tearDown('idle')
    }
  }, [
    conversationId,
    flushBufferedCandidates,
    installAudioContextKick,
    tearDown,
    updateState,
    wirePeerConnection,
  ])

  const rejectCall = useCallback(async () => {
    const convId = conversationId
    const theCallId = callIdRef.current
    if (convId === null || theCallId === null) {
      tearDown('idle')
      return
    }
    if (stateRef.current !== 'incoming-ringing') return
    // Fire-and-forget the end relay so the caller's UI exits ringing even if
    // the network is flaky. The local tear-down happens regardless.
    postSignal(convId, theCallId, 'end', { reason: 'rejected' }).catch(() => {})
    tearDown('idle')
  }, [conversationId, tearDown])

  const endCall = useCallback(async () => {
    const convId = conversationId
    const theCallId = callIdRef.current
    const wasActive = stateRef.current === 'active' || stateRef.current === 'outgoing-ringing'
    if (convId === null || theCallId === null || !wasActive) {
      tearDown('idle')
      return
    }
    try {
      await postSignal(convId, theCallId, 'end', { reason: 'hangup' })
    } catch {
      // The local hang-up still proceeds — the remote will eventually see
      // ICE/connection failure if our end signal never reaches them.
    }
    tearDown('ended')
  }, [conversationId, tearDown])

  const handleSignalEvent = useCallback(async (
    event: CallSignalEventName,
    payload: CallSignalPayload,
  ) => {
    // Ignore echoes of our own POSTs — the relay fans every event to every
    // member including the sender.
    const me = optionsRef.current.userId
    if (me !== null && me !== undefined && payload.from_user_id === me) return
    if (conversationId === null || payload.conversation_id !== conversationId) return

    const incomingCallId = payload.call_id

    switch (event) {
      case 'call_offer': {
        // Only accept an offer when we are completely idle. An offer that
        // arrives while we are already in a call is a glare condition; the
        // simplest resolution is to ignore the second offer rather than
        // tear down an in-progress call.
        if (stateRef.current !== 'idle' && stateRef.current !== 'ended') return
        const data = payload.data as { type?: RTCSdpType; sdp?: string } | undefined
        if (!data || !data.sdp) return
        pendingOfferRef.current = { type: data.type ?? 'offer', sdp: data.sdp }
        setError(null)
        updateCallId(incomingCallId)
        updateRemoteUserId(payload.from_user_id)
        updateState('incoming-ringing')
        return
      }
      case 'call_answer': {
        if (stateRef.current !== 'outgoing-ringing') return
        if (callIdRef.current !== incomingCallId) return
        const pc = pcRef.current
        const data = payload.data as { type?: RTCSdpType; sdp?: string } | undefined
        if (!pc || !data || !data.sdp) return
        try {
          await pc.setRemoteDescription({ type: data.type ?? 'answer', sdp: data.sdp })
          remoteDescriptionSetRef.current = true
          await flushBufferedCandidates()
          updateRemoteUserId(payload.from_user_id)
          updateState('active')
          installAudioContextKick()
        } catch (err) {
          setError(err instanceof Error ? err.message : 'answer-failed')
          tearDown('ended')
        }
        return
      }
      case 'call_ice': {
        if (callIdRef.current !== incomingCallId) return
        const pc = pcRef.current
        const cand = payload.data as RTCIceCandidateInit | undefined
        if (!cand) return
        if (!pc || !remoteDescriptionSetRef.current) {
          pendingRemoteCandidatesRef.current.push(cand)
          return
        }
        try {
          await pc.addIceCandidate(cand)
        } catch (err) {
          if (typeof console !== 'undefined') {
            console.warn('voice call: addIceCandidate failed', err)
          }
        }
        return
      }
      case 'call_end': {
        if (callIdRef.current !== incomingCallId) return
        tearDown('ended')
        return
      }
    }
  }, [
    conversationId,
    flushBufferedCandidates,
    installAudioContextKick,
    tearDown,
    updateCallId,
    updateRemoteUserId,
    updateState,
  ])

  // Internal SSE subscription — disabled by passing skipSignalSubscription so
  // tests (and outer pages that already own a stream) can route events in
  // manually. The reader mirrors the loop in ChatView so the on-wire framing
  // stays consistent.
  useEffect(() => {
    if (skipSignalSubscription) return
    if (conversationId === null) return
    if (typeof window === 'undefined' || typeof fetch === 'undefined') return

    const controller = new AbortController()
    let cancelled = false
    let activeReader: ReadableStreamDefaultReader<Uint8Array> | null = null

    const dispatch = (eventName: string, data: string) => {
      if (
        eventName !== 'call_offer'
        && eventName !== 'call_answer'
        && eventName !== 'call_ice'
        && eventName !== 'call_end'
      ) return
      let payload: CallSignalPayload
      try {
        payload = JSON.parse(data) as CallSignalPayload
      } catch {
        return
      }
      void handleSignalEvent(eventName as CallSignalEventName, payload)
    }

    const run = async () => {
      try {
        const res = await fetch(
          `/api/familychat/conversations/${conversationId}/stream`,
          { credentials: 'include', signal: controller.signal },
        )
        if (!res.ok || !res.body) return
        const reader = res.body.getReader()
        activeReader = reader
        const decoder = new TextDecoder()
        let buffer = ''
        let eventName = 'message'
        let dataLines: string[] = []
        while (!cancelled) {
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
                dispatch(eventName, dataLines.join('\n'))
              }
              eventName = 'message'
              dataLines = []
            } else if (line.startsWith(':')) {
              // SSE heartbeat comment — ignore.
            } else if (line.startsWith('event:')) {
              eventName = line.slice(6).trimStart()
            } else if (line.startsWith('data:')) {
              const v = line.slice(5)
              dataLines.push(v.startsWith(' ') ? v.slice(1) : v)
            }
            nl = buffer.indexOf('\n')
          }
        }
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        // The hook deliberately does not reconnect — the outer ChatView SSE
        // stream owns reconnection semantics, and a missed call_offer simply
        // means the recipient does not ring. The webpush fallback (sibling 1)
        // still wakes the phone.
      }
    }

    void run()

    return () => {
      cancelled = true
      controller.abort()
      // Some environments don't propagate abort to the body reader; cancel
      // explicitly so the read loop cannot hang and leak after cleanup.
      if (activeReader) {
        activeReader.cancel().catch(() => {})
        activeReader = null
      }
    }
  }, [conversationId, handleSignalEvent, skipSignalSubscription])

  // Tear down on unmount so a mid-call navigation away releases the mic and
  // the PeerConnection rather than leaving the call dangling.
  useEffect(() => {
    return () => {
      tearDown('idle')
    }
  }, [tearDown])

  const setMuted = useCallback((next: boolean) => {
    const stream = localStreamRef.current
    if (!stream) return
    setMutedState(next)
    for (const track of stream.getAudioTracks()) {
      track.enabled = !next
    }
  }, [])

  return {
    state,
    callId,
    remoteUserId,
    error,
    remoteStream,
    muted,
    startCall,
    acceptCall,
    rejectCall,
    endCall,
    setMuted,
    handleSignalEvent,
  }
}
