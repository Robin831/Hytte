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

// CallKind discriminates between audio-only and audio+video calls. The same
// signalling pipeline carries both; the difference is purely in the local
// media constraints and the UI surface (PiP overlay, camera toggles).
export type CallKind = 'voice' | 'video'

// Camera facing mode on devices with both a front- and back-facing camera.
// Desktop webcams generally ignore this hint.
export type CameraFacingMode = 'user' | 'environment'

// CallSignalPayload mirrors the JSON shape emitted by the backend SSE relay
// (see internal/familychat/calls_handlers.go: callRelayPayload). `data` is the
// opaque artifact — SDP for offer/answer, an ICE candidate object for ice, or
// a hang-up envelope for end. `status` is only populated on call_end and lets
// the UI distinguish missed vs ended without an extra query. `kind` is set on
// call_offer so the receiver can branch on voice vs video before requesting
// media — once accepted the answer SDP encodes both tracks.
export interface CallSignalPayload {
  conversation_id: number
  call_id: string
  from_user_id: number
  data?: unknown
  status?: string
  kind?: CallKind
}

// Video constraints used by both the caller and the callee. The bead scopes
// us to 640×480 by default (good for 1:1 chat on most networks) with an
// optional downscale to 320×240 when the Network Information API reports a
// slow connection. Keep these as plain numbers — getUserMedia accepts an
// `{ideal: N}` hint but most browsers also accept the bare number.
const VIDEO_FULL_WIDTH = 640
const VIDEO_FULL_HEIGHT = 480
const VIDEO_LOW_WIDTH = 320
const VIDEO_LOW_HEIGHT = 240

// videoSizeForConnection picks {320,240} on slow-2g/2g/3g and {640,480}
// everywhere else. Browsers without `navigator.connection` keep the full
// resolution — no telemetry needed, the bead just asks us to adapt locally.
function videoSizeForConnection(): { width: number; height: number } {
  const slow = isSlowEffectiveType(currentEffectiveType())
  if (slow) return { width: VIDEO_LOW_WIDTH, height: VIDEO_LOW_HEIGHT }
  return { width: VIDEO_FULL_WIDTH, height: VIDEO_FULL_HEIGHT }
}

function currentEffectiveType(): string | undefined {
  if (typeof navigator === 'undefined') return undefined
  const conn = (navigator as unknown as {
    connection?: { effectiveType?: string }
  }).connection
  return conn?.effectiveType
}

function isSlowEffectiveType(effectiveType: string | undefined): boolean {
  return effectiveType === 'slow-2g' || effectiveType === '2g' || effectiveType === '3g'
}

// videoConstraintsForKind builds the constraints object passed to
// getUserMedia. For voice calls we ask for audio only; for video we add the
// camera with a facingMode hint (so mobile picks the front-cam by default)
// and the bandwidth-aware resolution.
function videoConstraintsForKind(
  kind: CallKind,
  facingMode: CameraFacingMode,
): MediaStreamConstraints {
  if (kind === 'voice') return { audio: true }
  const { width, height } = videoSizeForConnection()
  return {
    audio: true,
    video: {
      facingMode,
      width: { ideal: width },
      height: { ideal: height },
    },
  }
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
  // callKind is the kind of the call currently being established / active. For
  // outgoing it reflects the kind passed to startCall; for incoming it
  // reflects the offer's `kind` field (defaulting to 'voice' for legacy
  // clients that pre-date the field).
  callKind: CallKind
  remoteStream: MediaStream | null
  // localStream surfaces the same MediaStream that's wired into the
  // RTCPeerConnection's senders so the UI can render a self-view <video>
  // element (PiP). Null when no call is active or when called for voice-only.
  localStream: MediaStream | null
  muted: boolean
  // cameraEnabled is false when the local video track has been swapped for
  // null on the sender — the peer continues to receive an active connection
  // but sees a frozen / no-video placeholder. true outside video calls too,
  // since voice calls have no camera to disable.
  cameraEnabled: boolean
  facingMode: CameraFacingMode
  startCall: (kind?: CallKind) => Promise<void>
  acceptCall: () => Promise<void>
  rejectCall: () => Promise<void>
  endCall: () => Promise<void>
  // setMuted flips the local audio track's enabled flag so the peer stops
  // receiving frames without dropping the WebRTC connection. Safe to call in
  // any state — a no-op when no local track exists.
  setMuted: (muted: boolean) => void
  // setCameraEnabled toggles whether the local video track is forwarded. When
  // disabled we replaceTrack(null) on the video sender so the remote sees the
  // last frame freeze / a placeholder rather than a still-running but blank
  // feed. No-op for voice calls or when no video track exists.
  setCameraEnabled: (enabled: boolean) => Promise<void>
  // switchCamera toggles between the user-facing and environment-facing camera
  // by re-requesting the video track with the opposite facingMode hint and
  // replaceTrack-ing the sender. No-op outside video calls.
  switchCamera: () => Promise<void>
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
  extra?: Record<string, unknown>,
): Promise<void> {
  const body: Record<string, unknown> = { data }
  if (extra) {
    for (const k of Object.keys(extra)) body[k] = extra[k]
  }
  const res = await fetch(
    `/api/familychat/conversations/${convId}/calls/${callId}/${kind}`,
    {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
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
  const [localStream, setLocalStreamState] = useState<MediaStream | null>(null)
  const [muted, setMutedState] = useState(false)
  const [callKind, setCallKindState] = useState<CallKind>('voice')
  const [cameraEnabled, setCameraEnabledState] = useState(true)
  const [facingMode, setFacingModeState] = useState<CameraFacingMode>('user')

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
  // callKindRef shadows callKind state so the async signalling callbacks can
  // read the current kind without re-running on every render.
  const callKindRef = useRef<CallKind>('voice')
  // facingModeRef shadows facingMode for switchCamera continuations.
  const facingModeRef = useRef<CameraFacingMode>('user')
  // videoSenderRef holds the RTCRtpSender for the local video track so
  // setCameraEnabled / switchCamera / bandwidth adaptation can call
  // replaceTrack on the same sender without re-negotiating the SDP.
  const videoSenderRef = useRef<RTCRtpSender | null>(null)
  // lastVideoSizeRef remembers the resolution we last asked for so the
  // bandwidth-adaptation effect can skip a redundant replaceTrack when the
  // effectiveType change doesn't cross the slow/fast threshold.
  const lastVideoSizeRef = useRef<{ width: number; height: number } | null>(null)
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

  const updateCallKind = useCallback((next: CallKind) => {
    callKindRef.current = next
    setCallKindState(next)
  }, [])

  const updateFacingMode = useCallback((next: CameraFacingMode) => {
    facingModeRef.current = next
    setFacingModeState(next)
  }, [])

  const setLocalStream = useCallback((next: MediaStream | null) => {
    localStreamRef.current = next
    setLocalStreamState(next)
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

    const prevLocalStream = localStreamRef.current
    localStreamRef.current = null
    setLocalStreamState(null)
    if (prevLocalStream) {
      for (const track of prevLocalStream.getTracks()) {
        try { track.stop() } catch { /* already stopped */ }
      }
    }

    setRemoteStream(null)
    setMutedState(false)
    setCameraEnabledState(true)
    callKindRef.current = 'voice'
    setCallKindState('voice')
    facingModeRef.current = 'user'
    setFacingModeState('user')
    videoSenderRef.current = null
    lastVideoSizeRef.current = null
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

  const startCall = useCallback(async (kind: CallKind = 'voice') => {
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
    updateCallKind(kind)
    updateFacingMode('user')
    setCameraEnabledState(true)
    updateState('outgoing-ringing')

    try {
      const ice = await fetchICEConfig()
      if (callEpochRef.current !== epoch) return

      const pc = optionsRef.current.rtcPeerConnectionFactory({
        iceServers: ice.iceServers as RTCIceServer[],
      })
      pcRef.current = pc
      wirePeerConnection(pc, conversationId, theCallId)

      const constraints = videoConstraintsForKind(kind, 'user')
      if (kind === 'video') {
        lastVideoSizeRef.current = videoSizeForConnection()
      }
      const stream = await optionsRef.current.getUserMedia(constraints)
      if (callEpochRef.current !== epoch) {
        // tearDown already ran — release the mic without touching state.
        for (const t of stream.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      setLocalStream(stream)
      videoSenderRef.current = null
      for (const track of stream.getTracks()) {
        const sender = pc.addTrack(track, stream)
        if (track.kind === 'video') {
          videoSenderRef.current = sender
        }
      }

      const offer = await pc.createOffer()
      if (callEpochRef.current !== epoch) return
      await pc.setLocalDescription(offer)
      if (callEpochRef.current !== epoch) return
      // Relay the SDP from localDescription (rather than the raw createOffer
      // result) because some implementations massage the SDP during setLocal.
      const sdp = pc.localDescription ?? offer
      await postSignal(
        conversationId,
        theCallId,
        'offer',
        { type: sdp.type, sdp: sdp.sdp },
        undefined,
        { kind },
      )
    } catch (err) {
      if (callEpochRef.current !== epoch) return
      setError(err instanceof Error ? err.message : 'start-failed')
      tearDown('idle')
    }
  }, [
    conversationId,
    setLocalStream,
    tearDown,
    updateCallId,
    updateCallKind,
    updateFacingMode,
    updateState,
    wirePeerConnection,
  ])

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
    // callKind was already set by the call_offer handler from the offer
    // payload; read it here so the accept path requests the right media.
    const acceptKind = callKindRef.current

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

      const constraints = videoConstraintsForKind(acceptKind, 'user')
      if (acceptKind === 'video') {
        lastVideoSizeRef.current = videoSizeForConnection()
      }
      const stream = await optionsRef.current.getUserMedia(constraints)
      if (callEpochRef.current !== epoch) {
        // tearDown already ran — release the mic without touching state.
        for (const t of stream.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      setLocalStream(stream)
      videoSenderRef.current = null
      for (const track of stream.getTracks()) {
        const sender = pc.addTrack(track, stream)
        if (track.kind === 'video') {
          videoSenderRef.current = sender
        }
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
    setLocalStream,
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
        // Receivers default to 'voice' when the offer omits kind so legacy
        // clients keep working. The kind is captured here (not at accept)
        // because the ringing UI should show "video call" before the user
        // taps Accept.
        const incomingKind: CallKind = payload.kind === 'video' ? 'video' : 'voice'
        updateCallKind(incomingKind)
        updateFacingMode('user')
        setCameraEnabledState(true)
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
    updateCallKind,
    updateFacingMode,
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

  // setCameraEnabled toggles the local video track on the existing sender.
  // We use replaceTrack(null) rather than disabling the track so the peer
  // sees the feed actually stop (last-frame freeze) rather than continuing
  // to receive a black frame stream with the same bandwidth cost.
  const setCameraEnabled = useCallback(async (next: boolean) => {
    const sender = videoSenderRef.current
    if (!sender) return
    if (callKindRef.current !== 'video') return
    if (next) {
      // Re-enable: re-request a video track and put it back on the sender.
      const stream = localStreamRef.current
      if (!stream) return
      try {
        const constraints = videoConstraintsForKind('video', facingModeRef.current)
        const fresh = await optionsRef.current.getUserMedia(constraints)
        const videoTrack = fresh.getVideoTracks()[0]
        if (!videoTrack) {
          // Releasing the audio-only stream avoids leaking a second mic capture.
          for (const t of fresh.getTracks()) { try { t.stop() } catch { /* ok */ } }
          return
        }
        await sender.replaceTrack(videoTrack)
        // Swap the new video track into our local preview stream so the UI
        // re-renders with the fresh feed. Audio track is preserved.
        const oldVideos = stream.getVideoTracks()
        for (const t of oldVideos) {
          try { stream.removeTrack(t) } catch { /* old browsers */ }
          try { t.stop() } catch { /* ok */ }
        }
        stream.addTrack(videoTrack)
        // Release the audio track from the fresh stream since we kept the original.
        for (const t of fresh.getAudioTracks()) { try { t.stop() } catch { /* ok */ } }
        setCameraEnabledState(true)
      } catch (err) {
        if (typeof console !== 'undefined') {
          console.warn('voice call: re-enable camera failed', err)
        }
      }
    } else {
      try {
        await sender.replaceTrack(null)
      } catch {
        // Older browsers occasionally throw when replaceTrack target is null.
        // Fall back to disabling the track so the remote at least sees a
        // black frame; the placeholder UI still kicks in on the remote.
        const stream = localStreamRef.current
        if (stream) {
          for (const t of stream.getVideoTracks()) {
            t.enabled = false
          }
        }
      }
      setCameraEnabledState(false)
    }
  }, [])

  // switchCamera toggles to the next available camera. The bead asks for
  // navigator.mediaDevices.enumerateDevices(); when the platform exposes more
  // than one videoinput we pick the next one by deviceId so the swap works on
  // browsers that ignore the facingMode hint. Falls back to a plain facingMode
  // toggle on platforms without enumerateDevices (or with only one camera).
  const pickNextCameraDeviceId = useCallback(async (currentTrack: MediaStreamTrack | null): Promise<string | null> => {
    if (typeof navigator === 'undefined' || !navigator.mediaDevices?.enumerateDevices) {
      return null
    }
    let devices: MediaDeviceInfo[]
    try {
      devices = await navigator.mediaDevices.enumerateDevices()
    } catch {
      return null
    }
    const videoInputs = devices.filter(d => d.kind === 'videoinput' && d.deviceId)
    if (videoInputs.length < 2) return null
    const currentId = currentTrack?.getSettings?.().deviceId
    const idx = currentId ? videoInputs.findIndex(d => d.deviceId === currentId) : -1
    const next = videoInputs[(idx + 1) % videoInputs.length]
    return next.deviceId
  }, [])

  const switchCamera = useCallback(async () => {
    const sender = videoSenderRef.current
    if (!sender) return
    if (callKindRef.current !== 'video') return
    const stream = localStreamRef.current
    if (!stream) return
    const nextFacing: CameraFacingMode = facingModeRef.current === 'user' ? 'environment' : 'user'
    try {
      const { width, height } = videoSizeForConnection()
      const currentVideoTrack = stream.getVideoTracks()[0] ?? null
      const nextDeviceId = await pickNextCameraDeviceId(currentVideoTrack)
      // Ask only for video — keep the existing mic track so we don't drop
      // audio mid-call by re-acquiring it from getUserMedia. Prefer an exact
      // deviceId when enumerateDevices found a sibling camera; otherwise fall
      // back to the facingMode hint that mobile browsers honour.
      const videoConstraints: MediaTrackConstraints = nextDeviceId !== null
        ? {
            deviceId: { exact: nextDeviceId },
            width: { ideal: width },
            height: { ideal: height },
          }
        : {
            facingMode: nextFacing,
            width: { ideal: width },
            height: { ideal: height },
          }
      const fresh = await optionsRef.current.getUserMedia({
        audio: false,
        video: videoConstraints,
      })
      const videoTrack = fresh.getVideoTracks()[0]
      if (!videoTrack) {
        for (const t of fresh.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      await sender.replaceTrack(videoTrack)
      const oldVideos = stream.getVideoTracks()
      for (const t of oldVideos) {
        try { stream.removeTrack(t) } catch { /* old browsers */ }
        try { t.stop() } catch { /* ok */ }
      }
      stream.addTrack(videoTrack)
      updateFacingMode(nextFacing)
      lastVideoSizeRef.current = { width, height }
    } catch (err) {
      if (typeof console !== 'undefined') {
        console.warn('voice call: switchCamera failed', err)
      }
    }
  }, [pickNextCameraDeviceId, updateFacingMode])

  // adaptVideoToConnection re-requests the video track at the resolution
  // matching the current effectiveType (slow→320×240, fast→640×480) and
  // replaceTrack-s it onto the sender. Driven by the navigator.connection
  // 'change' event subscribed in the effect below.
  const adaptVideoToConnection = useCallback(async () => {
    const sender = videoSenderRef.current
    if (!sender) return
    if (callKindRef.current !== 'video') return
    if (stateRef.current !== 'active' && stateRef.current !== 'outgoing-ringing') return
    if (!cameraEnabled) return
    const target = videoSizeForConnection()
    const last = lastVideoSizeRef.current
    if (last && last.width === target.width && last.height === target.height) {
      // No threshold crossing — skip the costly track re-acquire.
      return
    }
    const stream = localStreamRef.current
    if (!stream) return
    try {
      const fresh = await optionsRef.current.getUserMedia({
        audio: false,
        video: {
          facingMode: facingModeRef.current,
          width: { ideal: target.width },
          height: { ideal: target.height },
        },
      })
      const videoTrack = fresh.getVideoTracks()[0]
      if (!videoTrack) {
        for (const t of fresh.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      await sender.replaceTrack(videoTrack)
      const oldVideos = stream.getVideoTracks()
      for (const t of oldVideos) {
        try { stream.removeTrack(t) } catch { /* old browsers */ }
        try { t.stop() } catch { /* ok */ }
      }
      stream.addTrack(videoTrack)
      lastVideoSizeRef.current = target
    } catch (err) {
      if (typeof console !== 'undefined') {
        console.warn('voice call: bandwidth adapt failed', err)
      }
    }
  }, [cameraEnabled])

  // Subscribe to navigator.connection.change while a call is active so the
  // local capture downscales on slow networks. We do nothing in environments
  // without the Network Information API — most desktop browsers don't
  // implement it, and the call still proceeds at 640×480.
  useEffect(() => {
    if (typeof navigator === 'undefined') return
    const conn = (navigator as unknown as {
      connection?: {
        addEventListener?: (type: string, listener: () => void) => void
        removeEventListener?: (type: string, listener: () => void) => void
      }
    }).connection
    if (!conn || typeof conn.addEventListener !== 'function') return
    const listener = () => { void adaptVideoToConnection() }
    conn.addEventListener('change', listener)
    return () => {
      if (typeof conn.removeEventListener === 'function') {
        conn.removeEventListener('change', listener)
      }
    }
  }, [adaptVideoToConnection])

  return {
    state,
    callId,
    remoteUserId,
    error,
    callKind,
    remoteStream,
    localStream,
    muted,
    cameraEnabled,
    facingMode,
    startCall,
    acceptCall,
    rejectCall,
    endCall,
    setMuted,
    setCameraEnabled,
    switchCamera,
    handleSignalEvent,
  }
}
