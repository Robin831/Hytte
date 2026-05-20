// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { useVoiceCall, type CallSignalPayload } from './useVoiceCall'

// ── Fakes ─────────────────────────────────────────────────────────────────────

class FakeMediaStreamTrack {
  kind = 'audio' as const
  stop = vi.fn()
}

function makeFakeMediaStream(): MediaStream {
  const track = new FakeMediaStreamTrack()
  const tracks = [track]
  return {
    getTracks: () => tracks,
    getAudioTracks: () => tracks,
    getVideoTracks: () => [],
  } as unknown as MediaStream
}

interface FakeIceCandidate {
  candidate: string
  sdpMid: string
  sdpMLineIndex: number
}

class FakePeerConnection {
  static instances: FakePeerConnection[] = []
  config: RTCConfiguration
  localDescription: RTCSessionDescriptionInit | null = null
  remoteDescription: RTCSessionDescriptionInit | null = null
  tracks: Array<{ track: MediaStreamTrack; stream: MediaStream }> = []
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

  addTrack(track: MediaStreamTrack, stream: MediaStream) {
    this.tracks.push({ track, stream })
    return {} as RTCRtpSender
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

beforeEach(() => {
  FakePeerConnection.instances = []
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useVoiceCall — outgoing call', () => {
  it('fetches TURN config, creates an offer, and POSTs it to the relay', async () => {
    const fetchMock = installFetchMock()
    installRtcGlobals()
    const factory = vi.fn((cfg: RTCConfiguration) => new FakePeerConnection(cfg))

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
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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

  it('call_end during active state transitions to ended and closes the peer connection', async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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

describe('useVoiceCall — event filtering', () => {
  it('ignores events for a different conversation', async () => {
    installFetchMock()
    installRtcGlobals()

    const { result } = renderHook(() => useVoiceCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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
      rtcPeerConnectionFactory: cfg => new FakePeerConnection(cfg),
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
