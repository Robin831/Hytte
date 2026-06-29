import { useCallback, useEffect, useRef, useState } from 'react'
import {
  createVideoFilterPipeline,
  isFilterSupported,
  type FilterKind,
  type VideoFilterPipeline,
} from './videoFilters'

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
  // remoteCameraEnabled reflects whether the remote peer's camera is active.
  // Flips to false when the peer calls replaceTrack(null) (the received video
  // track fires a 'mute' event), and back to true on re-enable. Always true
  // for voice-only calls since no video track is ever received.
  remoteCameraEnabled: boolean
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
  // filter is the active local video effect. 'none' (the default) is a
  // zero-overhead passthrough; other kinds route the camera through a canvas
  // pipeline before the sender / local preview. Always 'none' for voice calls.
  filter: FilterKind
  // setFilter swaps the active local video effect live. It builds/updates the
  // canvas pipeline and replaceTrack()s the processed track onto the existing
  // video sender (no renegotiation) so the peer and the local PiP both see it.
  // Falls back to 'none' if the browser can't run the requested effect. No-op
  // for voice calls.
  setFilter: (filter: FilterKind) => Promise<void>
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
  // localStream is the stream the UI binds to the self-view (PiP). It is the
  // raw camera stream while filter==='none', and a processed stream (canvas
  // video track + the raw audio tracks) while a filter is active.
  const [localStream, setLocalStreamState] = useState<MediaStream | null>(null)
  const [muted, setMutedState] = useState(false)
  const [callKind, setCallKindState] = useState<CallKind>('voice')
  const [cameraEnabled, setCameraEnabledState] = useState(true)
  const [facingMode, setFacingModeState] = useState<CameraFacingMode>('user')
  const [remoteCameraEnabled, setRemoteCameraEnabledState] = useState(true)
  const [filter, setFilterState] = useState<FilterKind>('none')

  // Refs hold values the async signalling callbacks must read without
  // capturing stale closures. React state is used for things the UI needs to
  // re-render on; everything else is a ref.
  const pcRef = useRef<RTCPeerConnection | null>(null)
  // callEpochRef is incremented by tearDown so that any in-flight startCall
  // continuation can detect that the call was ended and bail out rather than
  // re-creating a PeerConnection or POSTing a stale offer.
  const callEpochRef = useRef<number>(0)
  // localStreamRef holds the RAW camera stream (audio + unprocessed video). All
  // camera management (switch / toggle / bandwidth adapt) mutates this stream;
  // the displayed/sent video is derived from it via syncVideoOutput so a filter
  // can be inserted without disturbing the capture plumbing.
  const localStreamRef = useRef<MediaStream | null>(null)
  // displayStreamRef shadows the exposed `localStream` state — the stream the UI
  // renders and which carries the sent video track. Equals localStreamRef when
  // filter==='none', otherwise a processed stream.
  const displayStreamRef = useRef<MediaStream | null>(null)
  // filterRef shadows the filter state for the async routing callbacks.
  const filterRef = useRef<FilterKind>('none')
  // filterPipelineRef holds the active canvas pipeline, or null when filter is
  // 'none' / the camera is off / the browser can't run a pipeline.
  const filterPipelineRef = useRef<VideoFilterPipeline | null>(null)
  // cameraEnabledRef shadows cameraEnabled so syncVideoOutput knows whether the
  // sender should carry a track at all.
  const cameraEnabledRef = useRef<boolean>(true)
  // currentSenderVideoTrackRef remembers the track currently on the video sender
  // so syncVideoOutput can skip a redundant replaceTrack (and the SDP churn some
  // engines do on every replace).
  const currentSenderVideoTrackRef = useRef<MediaStreamTrack | null>(null)
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

  // setRawStream updates the raw camera stream ref. It does NOT touch the
  // displayed `localStream` state — that is owned by setDisplayStream, driven by
  // syncVideoOutput once the (possibly filtered) output track is resolved.
  const setRawStream = useCallback((next: MediaStream | null) => {
    localStreamRef.current = next
  }, [])

  const setDisplayStream = useCallback((next: MediaStream | null) => {
    displayStreamRef.current = next
    setLocalStreamState(next)
  }, [])

  const updateCameraEnabled = useCallback((next: boolean) => {
    cameraEnabledRef.current = next
    setCameraEnabledState(next)
  }, [])

  // stopFilterPipeline tears down the canvas pipeline (RAF loop + output track)
  // if one is running. Safe to call when none exists.
  const stopFilterPipeline = useCallback(() => {
    const pipeline = filterPipelineRef.current
    filterPipelineRef.current = null
    if (pipeline) {
      try { pipeline.stop() } catch { /* already stopped */ }
    }
  }, [])

  // syncVideoOutput reconciles the video sender + the displayed local stream with
  // the current raw camera track, the selected filter, and the camera on/off
  // flag. It is the single chokepoint every camera operation funnels through so
  // a filter can be inserted (or removed) without each call site re-implementing
  // the routing. Stable identity (refs only) so it never re-creates the effects.
  const syncVideoOutput = useCallback(async () => {
    if (callKindRef.current !== 'video') return
    const raw = localStreamRef.current
    const sender = videoSenderRef.current
    const rawVideo = raw?.getVideoTracks()[0] ?? null

    const wantFilter = filterRef.current !== 'none'
      && cameraEnabledRef.current
      && rawVideo !== null
      && isFilterSupported(filterRef.current)

    // Passthrough path: no filter (or unsupported / camera off). Tear down any
    // pipeline and route the raw track (or null when the camera is off) to the
    // sender; the displayed stream is the raw stream itself.
    if (!wantFilter) {
      stopFilterPipeline()
      const target = cameraEnabledRef.current ? rawVideo : null
      if (sender && currentSenderVideoTrackRef.current !== target) {
        try {
          await sender.replaceTrack(target)
          currentSenderVideoTrackRef.current = target
        } catch { /* sender may be closed */ }
      }
      if (raw && displayStreamRef.current !== raw) setDisplayStream(raw)
      return
    }

    // Filtered path: build the pipeline on first use, then feed it the current
    // raw track and effect. The output track is stable, so the sender / display
    // only change when the pipeline is (re)created.
    let pipeline = filterPipelineRef.current
    if (!pipeline) {
      pipeline = createVideoFilterPipeline()
      if (!pipeline) {
        // Pipeline unavailable at runtime — fall back to passthrough so the call
        // keeps working with the unfiltered feed.
        filterRef.current = 'none'
        setFilterState('none')
        const target = cameraEnabledRef.current ? rawVideo : null
        if (sender && currentSenderVideoTrackRef.current !== target) {
          try {
            await sender.replaceTrack(target)
            currentSenderVideoTrackRef.current = target
          } catch { /* sender may be closed */ }
        }
        if (raw && displayStreamRef.current !== raw) setDisplayStream(raw)
        return
      }
      filterPipelineRef.current = pipeline
    }
    pipeline.setSource(rawVideo)
    pipeline.setFilter(filterRef.current)
    const out = pipeline.outputTrack
    if (sender && currentSenderVideoTrackRef.current !== out) {
      try {
        await sender.replaceTrack(out)
        currentSenderVideoTrackRef.current = out
      } catch { /* sender may be closed */ }
    }
    // Build the display stream once and keep its identity stable across source /
    // filter changes (the output track is reused) so the <video> element doesn't
    // re-bind on every swap.
    const currentDisplay = displayStreamRef.current
    if (!currentDisplay || currentDisplay.getVideoTracks()[0] !== out) {
      const display = new MediaStream()
      display.addTrack(out)
      if (raw) for (const a of raw.getAudioTracks()) display.addTrack(a)
      setDisplayStream(display)
    }
  }, [setDisplayStream, stopFilterPipeline])

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

    // Stop the canvas filter pipeline (RAF loop + processed output track) before
    // dropping the raw stream so neither outlives the call.
    stopFilterPipeline()
    filterRef.current = 'none'
    setFilterState('none')
    currentSenderVideoTrackRef.current = null

    const prevLocalStream = localStreamRef.current
    localStreamRef.current = null
    displayStreamRef.current = null
    setLocalStreamState(null)
    if (prevLocalStream) {
      for (const track of prevLocalStream.getTracks()) {
        try { track.stop() } catch { /* already stopped */ }
      }
    }

    setRemoteStream(null)
    setMutedState(false)
    cameraEnabledRef.current = true
    setCameraEnabledState(true)
    setRemoteCameraEnabledState(true)
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
  }, [stopFilterPipeline, updateCallId, updateRemoteUserId, updateState])

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
      // Track remote camera state via mute/unmute events on the received
      // video track. When the peer calls replaceTrack(null) the track fires
      // 'mute'; replaceTrack(<newTrack>) fires 'unmute'. Voice-only calls
      // never expose a video track, so the flag stays true throughout.
      if (event.track.kind === 'video') {
        event.track.onmute = () => setRemoteCameraEnabledState(false)
        event.track.onunmute = () => setRemoteCameraEnabledState(true)
      }
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
    updateCameraEnabled(true)
    // Every call starts with the passthrough filter — fresh and zero-overhead.
    filterRef.current = 'none'
    setFilterState('none')
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
      // The raw stream feeds both the camera plumbing and (with filter 'none')
      // the displayed/sent video directly.
      setRawStream(stream)
      setDisplayStream(stream)
      videoSenderRef.current = null
      currentSenderVideoTrackRef.current = null
      for (const track of stream.getTracks()) {
        const sender = pc.addTrack(track, stream)
        if (track.kind === 'video') {
          videoSenderRef.current = sender
          currentSenderVideoTrackRef.current = track
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
    setRawStream,
    setDisplayStream,
    tearDown,
    updateCallId,
    updateCallKind,
    updateCameraEnabled,
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
      setRawStream(stream)
      setDisplayStream(stream)
      videoSenderRef.current = null
      currentSenderVideoTrackRef.current = null
      for (const track of stream.getTracks()) {
        const sender = pc.addTrack(track, stream)
        if (track.kind === 'video') {
          videoSenderRef.current = sender
          currentSenderVideoTrackRef.current = track
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
    setRawStream,
    setDisplayStream,
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
        updateCameraEnabled(true)
        filterRef.current = 'none'
        setFilterState('none')
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
    updateCameraEnabled,
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
      // Capture the epoch so we can bail if tearDown fires while getUserMedia
      // is in-flight — otherwise the fresh tracks would outlive the call with
      // the camera light staying on until the page is closed.
      const epoch = callEpochRef.current
      try {
        // Request video-only — the existing mic track is still live on
        // localStreamRef and does not need to be re-acquired. Requesting
        // audio:true here would open a second microphone capture, trigger the
        // OS recording indicator, and be discarded immediately on line 972.
        const { width, height } = videoSizeForConnection()
        const videoOnlyConstraints: MediaStreamConstraints = {
          audio: false,
          video: {
            facingMode: facingModeRef.current,
            width: { ideal: width },
            height: { ideal: height },
          },
        }
        const fresh = await optionsRef.current.getUserMedia(videoOnlyConstraints)
        if (callEpochRef.current !== epoch) {
          for (const t of fresh.getTracks()) { try { t.stop() } catch { /* ok */ } }
          return
        }
        const videoTrack = fresh.getVideoTracks()[0]
        if (!videoTrack) {
          // Releasing the audio-only stream avoids leaking a second mic capture.
          for (const t of fresh.getTracks()) { try { t.stop() } catch { /* ok */ } }
          return
        }
        // Swap the new raw video track into the camera stream so the camera
        // plumbing (and, with filter 'none', the preview) picks it up. Audio is
        // preserved.
        const oldVideos = stream.getVideoTracks()
        for (const t of oldVideos) {
          try { stream.removeTrack(t) } catch { /* old browsers */ }
          try { t.stop() } catch { /* ok */ }
        }
        stream.addTrack(videoTrack)
        // Defensive: stop any audio tracks the stream may carry (should be
        // none since we passed audio:false, but guard against browser quirks).
        for (const t of fresh.getAudioTracks()) { try { t.stop() } catch { /* ok */ } }
        // Mark the camera on before routing so syncVideoOutput puts a track
        // (raw or filtered) back on the sender rather than leaving it null.
        cameraEnabledRef.current = true
        // Route the fresh raw track to the sender + preview, applying the active
        // filter if one is selected.
        await syncVideoOutput()
        if (callEpochRef.current !== epoch) {
          try { videoTrack.stop() } catch { /* ok */ }
          return
        }
        setCameraEnabledState(true)
      } catch (err) {
        if (typeof console !== 'undefined') {
          console.warn('voice call: re-enable camera failed', err)
        }
      }
    } else {
      // Disable: mark the camera off and tear the filter pipeline down (no point
      // burning CPU drawing frames nobody receives), then drop the sent track.
      cameraEnabledRef.current = false
      stopFilterPipeline()
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
      currentSenderVideoTrackRef.current = null
      // If a filter had been active the displayed stream pointed at the (now
      // stopped) processed track; fall back to the raw stream so the preview
      // isn't a dead track behind the camera-off overlay.
      const raw = localStreamRef.current
      if (raw && displayStreamRef.current !== raw) setDisplayStream(raw)
      setCameraEnabledState(false)
    }
  }, [setDisplayStream, stopFilterPipeline, syncVideoOutput])

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
    // Capture epoch before any async work (enumerateDevices + getUserMedia) so
    // a tearDown during either await stops the fresh track rather than wiring
    // it into a torn-down peer connection.
    const epoch = callEpochRef.current
    const nextFacing: CameraFacingMode = facingModeRef.current === 'user' ? 'environment' : 'user'
    try {
      const { width, height } = videoSizeForConnection()
      const currentVideoTrack = stream.getVideoTracks()[0] ?? null
      const nextDeviceId = await pickNextCameraDeviceId(currentVideoTrack)
      if (callEpochRef.current !== epoch) return
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
      if (callEpochRef.current !== epoch) {
        for (const t of fresh.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      const videoTrack = fresh.getVideoTracks()[0]
      if (!videoTrack) {
        for (const t of fresh.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      // Swap the fresh raw track into the camera stream, then route it to the
      // sender + preview (re-applying the active filter, whose stable output
      // track means no SDP renegotiation on the swap).
      const oldVideos = stream.getVideoTracks()
      for (const t of oldVideos) {
        try { stream.removeTrack(t) } catch { /* old browsers */ }
        try { t.stop() } catch { /* ok */ }
      }
      stream.addTrack(videoTrack)
      updateFacingMode(nextFacing)
      lastVideoSizeRef.current = { width, height }
      await syncVideoOutput()
      if (callEpochRef.current !== epoch) {
        try { videoTrack.stop() } catch { /* ok */ }
        return
      }
    } catch (err) {
      if (typeof console !== 'undefined') {
        console.warn('voice call: switchCamera failed', err)
      }
    }
  }, [pickNextCameraDeviceId, syncVideoOutput, updateFacingMode])

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
    // Capture epoch before the async getUserMedia so a tearDown while the
    // request is in-flight stops the fresh track rather than wiring it into
    // a torn-down peer connection with the camera light staying on.
    const epoch = callEpochRef.current
    try {
      const fresh = await optionsRef.current.getUserMedia({
        audio: false,
        video: {
          facingMode: facingModeRef.current,
          width: { ideal: target.width },
          height: { ideal: target.height },
        },
      })
      if (callEpochRef.current !== epoch) {
        for (const t of fresh.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      const videoTrack = fresh.getVideoTracks()[0]
      if (!videoTrack) {
        for (const t of fresh.getTracks()) { try { t.stop() } catch { /* ok */ } }
        return
      }
      const oldVideos = stream.getVideoTracks()
      for (const t of oldVideos) {
        try { stream.removeTrack(t) } catch { /* old browsers */ }
        try { t.stop() } catch { /* ok */ }
      }
      stream.addTrack(videoTrack)
      lastVideoSizeRef.current = target
      // Route the resized raw track to the sender + preview (re-applying any
      // active filter). The filter pipeline simply re-points at the new source.
      await syncVideoOutput()
      if (callEpochRef.current !== epoch) {
        try { videoTrack.stop() } catch { /* ok */ }
        return
      }
    } catch (err) {
      if (typeof console !== 'undefined') {
        console.warn('voice call: bandwidth adapt failed', err)
      }
    }
  }, [cameraEnabled, syncVideoOutput])

  // setFilter swaps the active local video effect. It records the choice
  // (synchronously, via the ref) then re-routes the current camera track
  // through syncVideoOutput, which builds/updates the canvas pipeline and
  // replaceTrack()s the processed track onto the existing sender. A no-op for
  // voice calls or when the requested effect isn't supported by this browser.
  const setFilter = useCallback(async (kind: FilterKind) => {
    if (filterRef.current === kind) return
    if (kind !== 'none' && !isFilterSupported(kind)) return
    if (callKindRef.current !== 'video') return
    filterRef.current = kind
    setFilterState(kind)
    await syncVideoOutput()
  }, [syncVideoOutput])

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
    remoteCameraEnabled,
    filter,
    startCall,
    acceptCall,
    rejectCall,
    endCall,
    setMuted,
    setCameraEnabled,
    switchCamera,
    setFilter,
    handleSignalEvent,
  }
}
