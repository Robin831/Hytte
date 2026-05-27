// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { useVoiceCall, type CallSignalPayload } from './useVoiceCall'

// ── Fakes ─────────────────────────────────────────────────────────────────────

class FakeMediaStreamTrack {
  kind: 'audio' | 'video'
  enabled = true
  stop = vi.fn()
  constructor(kind: 'audio' | 'video' = 'audio') {
    this.kind = kind
  }
}

function makeFakeMediaStream(): MediaStream {
  const track = new FakeMediaStreamTrack('audio')
  const tracks = [track]
  return {
    getTracks: () => tracks,
    getAudioTracks: () => tracks,
    getVideoTracks: () => [],
  } as unknown as MediaStream
}

// makeFakeVideoStream returns a mutable MediaStream that supports
// addTrack/removeTrack, since switchCamera / setCameraEnabled rely on
// swapping the video track in place. Audio is included so a video call's
// stream looks like the real getUserMedia({audio, video}) result.
function makeFakeVideoStream(): {
  stream: MediaStream
  audioTrack: FakeMediaStreamTrack
  videoTracks: FakeMediaStreamTrack[]
} {
  const audioTrack = new FakeMediaStreamTrack('audio')
  const videoTracks: FakeMediaStreamTrack[] = [new FakeMediaStreamTrack('video')]
  const stream = {
    getTracks: () => [audioTrack, ...videoTracks],
    getAudioTracks: () => [audioTrack],
    getVideoTracks: () => [...videoTracks],
    addTrack: (t: FakeMediaStreamTrack) => { videoTracks.push(t) },
    removeTrack: (t: FakeMediaStreamTrack) => {
      const i = videoTracks.indexOf(t)
      if (i >= 0) videoTracks.splice(i, 1)
    },
  } as unknown as MediaStream
  return { stream, audioTrack, videoTracks }
}

interface FakeIceCandidate {
  candidate: string
  sdpMid: string
  sdpMLineIndex: number
}

interface FakeSender {
  track: MediaStreamTrack | null
  replaceTrack: ReturnType<typeof vi.fn>
}

class FakePeerConnection {
  static instances: FakePeerConnection[] = []
  config: RTCConfiguration
  localDescription: RTCSessionDescriptionInit | null = null
  remoteDescription: RTCSessionDescriptionInit | null = null
  tracks: Array<{ track: MediaStreamTrack; stream: MediaStream }> = []
  senders: FakeSender[] = []
  addedRemoteCandidates: RTCIceCandidateInit[] = []
  connectionState: RTCPeerConnectionState = 'new'
  ontrack: ((event: RTCTrackEvent) => void) | null = null
  onicecandidate: ((event: RTCPeerConnectionIceEvent) => void) | null = null
  oniceconnectionstatechange: (() => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  closed = false

  constructor(config: RTCConfiguration) {
    this.config = config
    FakePeerConnection.instances.push(this)
  }

  addTrack(track: MediaStreamTrack, stream: MediaStream): RTCRtpSender {
    this.tracks.push({ track, stream })
    const sender: FakeSender = {
      track,
      replaceTrack: vi.fn(async (next: MediaStreamTrack | null) => { sender.track = next }),
    }
    this.senders.push(sender)
    return sender as unknown as RTCRtpSender
  }

  async createOffer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'offer', sdp: 'v=0\r\nfake-offer' }
  }

  async createAnswer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'answer', sdp: 'v=0\r\nfake-answer' }
  }

  async setLocalDescription(desc: RTCSessionDescriptionInit) {
    this.localDescription = desc
  }

  async setRemoteDescription(desc: RTCSessionDescriptionInit) {
    this.remoteDescription = desc
  }

  async addIceCandidate(cand: RTCIceCandidateInit) {
    this.addedRemoteCandidates.push(cand)
  }

  close() {
    this.closed = true
    this.connectionState = 'closed'
  }

  // Test helper — simulate the browser firing an ICE candidate event.
  fireIce(candidate: FakeIceCandidate | null) {
    if (!this.onicecandidate) return
    this.onicecandidate({
      candidate: candidate
        ? ({ ...candidate, toJSON: () => ({ ...candidate }) } as unknown as RTCIceCandidate)
        : null,
    } as RTCPeerConnectionIceEvent)
  }

  // Test helper — simulate the browser delivering a remote track.
  fireTrack(stream: MediaStream) {
    if (!this.ontrack) return
    this.ontrack({
      streams: [stream],
      track: stream.getTracks()[0],
    } as unknown as RTCTrackEvent)
  }
}

function makeFakePeerConnection(cfg: RTCConfiguration): RTCPeerConnection {
  return new FakePeerConnection(cfg) as unknown as RTCPeerConnection
}

interface FetchCall {
  url: string
  method: string
  body?: string
}

function installFetchMock() {
  const calls: FetchCall[] = []
  const responses = new Map<string, () => Response>()

  const mock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = (init?.method ?? 'GET').toUpperCase()
    const body = typeof init?.body === 'string' ? init.body : undefined
    calls.push({ url, method, body })

    const key = `${method} ${url}`
    const handler = responses.get(key) ?? responses.get(`* ${url}`)
    if (handler) return handler()

    // Default success for relay POSTs.
    if (url.includes('/calls/') && method === 'POST') {
      return new Response(null, { status: 204 })
    }
    if (url.endsWith('/api/familychat/turn') && method === 'GET') {
      return new Response(JSON.stringify({
        iceServers: [{ urls: ['stun:stun.example.com:3478'] }],
        ttl: 3600,
      }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }
    // Default: empty SSE-like body for the stream endpoint so the reader loop
    // exits immediately.
    if (url.includes('/stream')) {
      return new Response(new ReadableStream({
        start(controller) { controller.close() },
      }), { status: 200 })
    }
    return new Response('not found', { status: 404 })
  })

  vi.stubGlobal('fetch', mock)
  return {
    calls,
    setResponse(method: string, url: string, factory: () => Response) {
      responses.set(`${method} ${url}`, factory)
    },
  }
}

function findCall(calls: FetchCall[], method: string, urlSubstring: string): FetchCall | undefined {
  return calls.find(c => c.method === method && c.url.includes(urlSubstring))
}

function installRtcGlobals() {
  vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
  // happy-dom doesn't ship a getUserMedia — install one that hands back our fake.
  const stream = makeFakeMediaStream()
  const getUserMedia = vi.fn(async () => stream)
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia },
  })
  return { getUserMedia, stream }
}

// ── Tests ─────────────────────────────────────────────────────────────────────

let originalMediaDevicesDescriptor: PropertyDescriptor | undefined

beforeEach(() => {
  FakePeerConnection.instances = []
  originalMediaDevicesDescriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices')
})

afterEach(() => {
  vi.unstubAllGlobals()
  // Restore navigator.mediaDevices to its original state so the stub doesn't
  // leak into subsequent test files sharing the same worker.
  if (originalMediaDevicesDescriptor !== undefined) {
    Object.defineProperty(navigator, 'mediaDevices', originalMediaDevicesDescriptor)
  } else {
    delete (navigator as unknown as Record<string, unknown>).mediaDevices
  }
})

describe('useVoiceCall — outgoing call', () => {
  it('fetches TURN config, creates an offer, and POSTs it to the relay', async () => {
    const fetchMock = installFetchMock()
    installRtcGlobals()
    const factory = vi.fn(makeFakePeerConnection)

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: factory,
      skipSignalSubscription: true,
      generateCallId: () => 'call-uuid-A',
    }))

    expect(result.current.state).toBe('idle')

    await act(async () => { await result.current.startCall() })

    expect(result.current.state).toBe('outgoing-ringing')
    expect(result.current.callId).toBe('call-uuid-A')

    // TURN config fetched first.
    expect(findCall(fetchMock.calls, 'GET', '/api/familychat/turn')).toBeDefined()

    // Peer connection built with the iceServers from the TURN response.
    expect(factory).toHaveBeenCalledTimes(1)
    expect(factory.mock.calls[0][0].iceServers).toEqual([
      { urls: ['stun:stun.example.com:3478'] },
    ])

    // Offer POSTed with the SDP wrapped under {data: {type, sdp}}.
    const offerCall = findCall(fetchMock.calls, 'POST', '/calls/call-uuid-A/offer')
    expect(offerCall).toBeDefined()
    expect(JSON.parse(offerCall!.body!)).toEqual({
      data: { type: 'offer', sdp: 'v=0\r\nfake-offer' },
    })

    // The peer connection has the local mic track wired up.
    const pc = FakePeerConnection.instances[0]
    expect(pc.tracks).toHaveLength(1)
    expect(pc.localDescription?.type).toBe('offer')
  })

  it('relays trickled ICE candidates as POSTs to the ice endpoint', async () => {
    const fetchMock = installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'call-uuid-B',
    }))

    await act(async () => { await result.current.startCall() })
    const pc = FakePeerConnection.instances[0]

    await act(async () => {
      pc.fireIce({
        candidate: 'candidate:1 1 udp 2122260223 192.168.1.1 49152 typ host',
        sdpMid: '0',
        sdpMLineIndex: 0,
      })
    })

    await waitFor(() => {
      expect(findCall(fetchMock.calls, 'POST', '/calls/call-uuid-B/ice')).toBeDefined()
    })
    const iceCall = findCall(fetchMock.calls, 'POST', '/calls/call-uuid-B/ice')!
    expect(JSON.parse(iceCall.body!).data.candidate).toContain('192.168.1.1')
  })

  it('transitions to active when the answer arrives and applies remote SDP', async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'call-uuid-C',
    }))

    await act(async () => { await result.current.startCall() })

    const payload: CallSignalPayload = {
      conversation_id: 7,
      call_id: 'call-uuid-C',
      from_user_id: 2,
      data: { type: 'answer', sdp: 'v=0\r\nfake-remote-answer' },
    }
    await act(async () => { await result.current.handleSignalEvent('call_answer', payload) })

    expect(result.current.state).toBe('active')
    expect(result.current.remoteUserId).toBe(2)
    expect(FakePeerConnection.instances[0].remoteDescription?.sdp).toBe('v=0\r\nfake-remote-answer')
  })

  it('buffers ICE candidates that arrive before setRemoteDescription resolves', async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'call-uuid-D',
    }))

    await act(async () => { await result.current.startCall() })

    // ICE first.
    await act(async () => {
      await result.current.handleSignalEvent('call_ice', {
        conversation_id: 7,
        call_id: 'call-uuid-D',
        from_user_id: 2,
        data: { candidate: 'cand:early', sdpMid: '0', sdpMLineIndex: 0 },
      })
    })
    const pc = FakePeerConnection.instances[0]
    expect(pc.addedRemoteCandidates).toHaveLength(0)

    // Answer arrives, candidate should be flushed.
    await act(async () => {
      await result.current.handleSignalEvent('call_answer', {
        conversation_id: 7,
        call_id: 'call-uuid-D',
        from_user_id: 2,
        data: { type: 'answer', sdp: 'v=0\r\nanswer' },
      })
    })
    expect(pc.addedRemoteCandidates).toHaveLength(1)
    expect(pc.addedRemoteCandidates[0].candidate).toBe('cand:early')
  })

  it('endCall POSTs to the end endpoint and tears down the peer connection', async () => {
    const fetchMock = installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'call-uuid-E',
    }))

    await act(async () => { await result.current.startCall() })
    const pc = FakePeerConnection.instances[0]
    await act(async () => { await result.current.endCall() })

    expect(result.current.state).toBe('ended')
    expect(pc.closed).toBe(true)
    expect(findCall(fetchMock.calls, 'POST', '/calls/call-uuid-E/end')).toBeDefined()
  })
})

describe('useVoiceCall — incoming call', () => {
  it('transitions to incoming-ringing when a call_offer arrives', async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'inbound-1',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nremote-offer' },
      })
    })

    expect(result.current.state).toBe('incoming-ringing')
    expect(result.current.callId).toBe('inbound-1')
    expect(result.current.remoteUserId).toBe(2)
    // No peer connection until the user accepts — the offer is only buffered.
    expect(FakePeerConnection.instances).toHaveLength(0)
  })

  it('acceptCall fetches TURN, applies remote SDP, and POSTs an answer', async () => {
    const fetchMock = installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'inbound-2',
        from_user_id: 3,
        data: { type: 'offer', sdp: 'v=0\r\nremote-offer-2' },
      })
    })

    await act(async () => { await result.current.acceptCall() })

    expect(result.current.state).toBe('active')

    expect(findCall(fetchMock.calls, 'GET', '/api/familychat/turn')).toBeDefined()

    const pc = FakePeerConnection.instances[0]
    expect(pc.remoteDescription?.sdp).toBe('v=0\r\nremote-offer-2')
    expect(pc.localDescription?.type).toBe('answer')

    const answerCall = findCall(fetchMock.calls, 'POST', '/calls/inbound-2/answer')
    expect(answerCall).toBeDefined()
    expect(JSON.parse(answerCall!.body!)).toEqual({
      data: { type: 'answer', sdp: 'v=0\r\nfake-answer' },
    })
  })

  it('rejectCall POSTs end without ever creating a peer connection', async () => {
    const fetchMock = installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'inbound-3',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nremote-offer-3' },
      })
    })

    await act(async () => { await result.current.rejectCall() })

    expect(result.current.state).toBe('idle')
    expect(FakePeerConnection.instances).toHaveLength(0)
    expect(findCall(fetchMock.calls, 'POST', '/calls/inbound-3/end')).toBeDefined()
  })

  it('clears stale error when a new inbound offer arrives', async () => {
    installRtcGlobals()
    const fetchMock = installFetchMock()
    // Force TURN to fail so startCall sets an error.
    fetchMock.setResponse('GET', '/api/familychat/turn', () => new Response('', { status: 500 }))

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'call-err',
    }))

    await act(async () => { await result.current.startCall() })
    expect(result.current.error).not.toBeNull()
    expect(result.current.state).toBe('idle')

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'fresh-inbound',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nfresh-offer' },
      })
    })

    expect(result.current.state).toBe('incoming-ringing')
    expect(result.current.error).toBeNull()
  })

  it('call_end during active state transitions to ended and closes the peer connection', async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'inbound-4',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nremote-offer-4' },
      })
    })
    await act(async () => { await result.current.acceptCall() })
    const pc = FakePeerConnection.instances[0]

    await act(async () => {
      await result.current.handleSignalEvent('call_end', {
        conversation_id: 7,
        call_id: 'inbound-4',
        from_user_id: 2,
        status: 'ended',
      })
    })

    expect(result.current.state).toBe('ended')
    expect(pc.closed).toBe(true)
  })
})

describe('useVoiceCall — epoch guards', () => {
  it('acceptCall bails out if rejectCall fires while TURN is in-flight', async () => {
    installRtcGlobals()
    let resolveTurn!: (r: Response) => void
    const turnPromise = new Promise<Response>(resolve => { resolveTurn = resolve })

    const fetchMock = installFetchMock()
    fetchMock.setResponse('GET', '/api/familychat/turn', (() => turnPromise) as unknown as () => Response)

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'inbound-ep',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nremote-offer-ep' },
      })
    })
    expect(result.current.state).toBe('incoming-ringing')

    await act(async () => {
      // acceptCall suspends on the deferred TURN fetch.
      const accept = result.current.acceptCall()
      // rejectCall runs tearDown synchronously, bumping the epoch.
      await result.current.rejectCall()
      // Unblock TURN — the epoch guard in acceptCall should bail out.
      resolveTurn(new Response(JSON.stringify({
        iceServers: [{ urls: ['stun:stun.example.com:3478'] }],
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      await accept
    })

    // No peer connection created and no answer posted.
    expect(FakePeerConnection.instances).toHaveLength(0)
    expect(findCall(fetchMock.calls, 'POST', '/calls/inbound-ep/answer')).toBeUndefined()
    expect(result.current.state).toBe('idle')
  })
})

describe('useVoiceCall — event filtering', () => {
  it('ignores events for a different conversation', async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 99,
        call_id: 'other-conv',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nignored' },
      })
    })

    expect(result.current.state).toBe('idle')
  })

  it("ignores echoes of our own POSTs (from_user_id matches the local user)", async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'self-echo',
        from_user_id: 1,
        data: { type: 'offer', sdp: 'v=0\r\necho' },
      })
    })

    expect(result.current.state).toBe('idle')
  })
})

// ── Video call tests (Hytte-hob4) ────────────────────────────────────────────

describe('useVoiceCall — video call (outgoing)', () => {
  it('requests audio+video when startCall("video") is invoked', async () => {
    installFetchMock()
    const { stream } = makeFakeVideoStream()
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
    const getUserMedia = vi.fn(async () => stream)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia },
    })

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'video-A',
    }))

    await act(async () => { await result.current.startCall('video') })

    expect(result.current.state).toBe('outgoing-ringing')
    expect(result.current.callKind).toBe('video')

    // getUserMedia must have been asked for audio+video with the 640×480
    // default constraints and facingMode 'user'.
    expect(getUserMedia).toHaveBeenCalledTimes(1)
    const constraints = getUserMedia.mock.calls[0][0] as MediaStreamConstraints
    expect(constraints.audio).toBe(true)
    expect(constraints.video).toBeTruthy()
    const v = constraints.video as MediaTrackConstraints
    expect(v.facingMode).toBe('user')
    expect((v.width as { ideal?: number })?.ideal).toBe(640)
    expect((v.height as { ideal?: number })?.ideal).toBe(480)

    // The local stream is exposed for the PiP UI.
    expect(result.current.localStream).toBe(stream)
  })

  it('POSTs the offer with kind=video so the receiver branches correctly', async () => {
    const fetchMock = installFetchMock()
    const { stream } = makeFakeVideoStream()
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn(async () => stream) },
    })

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'video-B',
    }))

    await act(async () => { await result.current.startCall('video') })

    const offerCall = findCall(fetchMock.calls, 'POST', '/calls/video-B/offer')
    expect(offerCall).toBeDefined()
    const body = JSON.parse(offerCall!.body!)
    expect(body.kind).toBe('video')
    expect(body.data.type).toBe('offer')
  })
})

describe('useVoiceCall — video call (incoming)', () => {
  it('reads kind=video from the offer payload before the user accepts', async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'in-video-1',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\noffer' },
        kind: 'video',
      })
    })

    expect(result.current.state).toBe('incoming-ringing')
    expect(result.current.callKind).toBe('video')
  })

  it('requests audio+video on accept when the offer was a video call', async () => {
    installFetchMock()
    const { stream } = makeFakeVideoStream()
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
    const getUserMedia = vi.fn(async () => stream)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia },
    })

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'in-video-2',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\noffer-v' },
        kind: 'video',
      })
    })
    await act(async () => { await result.current.acceptCall() })

    expect(result.current.state).toBe('active')
    expect(result.current.callKind).toBe('video')
    expect(getUserMedia).toHaveBeenCalledTimes(1)
    const constraints = getUserMedia.mock.calls[0][0] as MediaStreamConstraints
    expect(constraints.audio).toBe(true)
    expect(constraints.video).toBeTruthy()
  })

  it('falls back to voice when the offer omits kind (legacy clients)', async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_offer', {
        conversation_id: 7,
        call_id: 'legacy-offer',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nlegacy' },
        // no kind field
      })
    })

    expect(result.current.callKind).toBe('voice')
  })
})

describe('useVoiceCall — video controls', () => {
  it('switchCamera re-requests video with the opposite facingMode and replaceTracks', async () => {
    installFetchMock()
    const original = makeFakeVideoStream()
    const swapped = makeFakeVideoStream()
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)

    // First getUserMedia call (during startCall) returns original; second
    // (during switchCamera) returns the swapped stream so we can inspect both.
    const getUserMedia = vi.fn()
      .mockImplementationOnce(async () => original.stream)
      .mockImplementationOnce(async () => swapped.stream)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia },
    })

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'video-switch-1',
    }))

    await act(async () => { await result.current.startCall('video') })
    expect(result.current.facingMode).toBe('user')

    await act(async () => { await result.current.switchCamera() })

    expect(getUserMedia).toHaveBeenCalledTimes(2)
    const switchConstraints = getUserMedia.mock.calls[1][0] as MediaStreamConstraints
    const sv = switchConstraints.video as MediaTrackConstraints
    expect(sv.facingMode).toBe('environment')
    expect(switchConstraints.audio).toBe(false)
    expect(result.current.facingMode).toBe('environment')

    // The sender's replaceTrack must have been invoked with the new video track.
    const pc = FakePeerConnection.instances[0]
    const videoSender = pc.senders.find(s => s.track?.kind === 'video' || s.replaceTrack.mock.calls.length > 0)
    expect(videoSender).toBeDefined()
    expect(videoSender!.replaceTrack).toHaveBeenCalled()
  })

  it('setCameraEnabled(false) calls replaceTrack(null) on the video sender', async () => {
    installFetchMock()
    const original = makeFakeVideoStream()
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn(async () => original.stream) },
    })

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'video-mute-1',
    }))

    await act(async () => { await result.current.startCall('video') })
    expect(result.current.cameraEnabled).toBe(true)

    await act(async () => { await result.current.setCameraEnabled(false) })

    expect(result.current.cameraEnabled).toBe(false)
    const pc = FakePeerConnection.instances[0]
    const videoSender = pc.senders.find(s => s.replaceTrack.mock.calls.some(c => c[0] === null))
    expect(videoSender).toBeDefined()
  })

  it('does not request video for voice calls', async () => {
    installFetchMock()
    const audioStream = makeFakeMediaStream()
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
    const getUserMedia = vi.fn(async () => audioStream)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia },
    })

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'voice-only-1',
    }))

    await act(async () => { await result.current.startCall('voice') })

    expect(getUserMedia).toHaveBeenCalledTimes(1)
    const constraints = getUserMedia.mock.calls[0][0] as MediaStreamConstraints
    expect(constraints.audio).toBe(true)
    expect(constraints.video).toBeUndefined()
    expect(result.current.callKind).toBe('voice')
  })
})

describe('useVoiceCall — bandwidth adaptation', () => {
  // Each test sets navigator.connection on the fly so the hook reads the
  // current effectiveType. afterEach in the outer suite tears it down.
  function setEffectiveType(type: string): EventTarget & { dispatchChange: () => void } {
    const listeners: Array<() => void> = []
    const conn = {
      effectiveType: type,
      addEventListener(_: string, l: () => void) { listeners.push(l) },
      removeEventListener(_: string, l: () => void) {
        const i = listeners.indexOf(l)
        if (i >= 0) listeners.splice(i, 1)
      },
      dispatchChange() { for (const l of listeners) l() },
    }
    Object.defineProperty(navigator, 'connection', {
      configurable: true,
      value: conn,
    })
    return conn as unknown as EventTarget & { dispatchChange: () => void }
  }

  afterEach(() => {
    // Remove the connection prop so it doesn't leak between tests.
    delete (navigator as unknown as Record<string, unknown>).connection
  })

  it('captures local video at 320×240 when effectiveType is 3g at start', async () => {
    installFetchMock()
    setEffectiveType('3g')
    const { stream } = makeFakeVideoStream()
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
    const getUserMedia = vi.fn(async () => stream)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia },
    })

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'video-slow-1',
    }))

    await act(async () => { await result.current.startCall('video') })

    const constraints = getUserMedia.mock.calls[0][0] as MediaStreamConstraints
    const v = constraints.video as MediaTrackConstraints
    expect((v.width as { ideal?: number })?.ideal).toBe(320)
    expect((v.height as { ideal?: number })?.ideal).toBe(240)
  })

  it('triggers a replaceTrack with 320×240 when connection downgrades during an active video call', async () => {
    installFetchMock()
    const conn = setEffectiveType('4g')
    const original = makeFakeVideoStream()
    const downgrade = makeFakeVideoStream()
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)

    const getUserMedia = vi.fn()
      .mockImplementationOnce(async () => original.stream)
      .mockImplementationOnce(async () => downgrade.stream)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia },
    })

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: makeFakePeerConnection,
      skipSignalSubscription: true,
      generateCallId: () => 'video-adapt-1',
    }))

    await act(async () => { await result.current.startCall('video') })
    // Transition to active via the call_answer path so the bandwidth effect's
    // state guard passes (it adapts when state is 'active' or 'outgoing-ringing').
    await act(async () => {
      await result.current.handleSignalEvent('call_answer', {
        conversation_id: 7,
        call_id: 'video-adapt-1',
        from_user_id: 2,
        data: { type: 'answer', sdp: 'v=0\r\nanswer' },
      })
    })

    // Initial capture was 640×480 (4g).
    const first = getUserMedia.mock.calls[0][0] as MediaStreamConstraints
    const v1 = first.video as MediaTrackConstraints
    expect((v1.width as { ideal?: number })?.ideal).toBe(640)

    // Simulate the network dropping to 3g and emit the change event.
    ;(conn as unknown as { effectiveType: string }).effectiveType = '3g'
    await act(async () => {
      ;(conn as unknown as { dispatchChange: () => void }).dispatchChange()
    })

    // waitFor polls until the async re-acquire chain has issued the second
    // getUserMedia call. This is more robust than awaiting a fixed number of
    // microtasks since happy-dom's scheduling can vary across versions.
    await waitFor(() => {
      expect(getUserMedia.mock.calls.length).toBeGreaterThanOrEqual(2)
    })
    const second = getUserMedia.mock.calls[1][0] as MediaStreamConstraints
    const v2 = second.video as MediaTrackConstraints
    expect((v2.width as { ideal?: number })?.ideal).toBe(320)
    expect((v2.height as { ideal?: number })?.ideal).toBe(240)
  })
})
