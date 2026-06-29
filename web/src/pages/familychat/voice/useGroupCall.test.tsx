// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { useGroupCall } from './useGroupCall'
import type { CallSignalPayload } from './useVoiceCall'

// ── Fakes (mirrors useVoiceCall.test.tsx) ──────────────────────────────────────

class FakeMediaStreamTrack {
  kind: 'audio' | 'video'
  enabled = true
  stop = vi.fn()
  onmute: (() => void) | null = null
  onunmute: (() => void) | null = null
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

function makeFakeVideoMediaStream(): MediaStream {
  const audioTrack = new FakeMediaStreamTrack('audio')
  const videoTrack = new FakeMediaStreamTrack('video')
  const allTracks = [audioTrack, videoTrack]
  return {
    getTracks: () => allTracks,
    getAudioTracks: () => [audioTrack],
    getVideoTracks: () => [videoTrack],
  } as unknown as MediaStream
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
  senders: FakeSender[] = []
  addedRemoteCandidates: RTCIceCandidateInit[] = []
  connectionState: RTCPeerConnectionState = 'new'
  ontrack: ((event: RTCTrackEvent) => void) | null = null
  onicecandidate: ((event: RTCPeerConnectionIceEvent) => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  closed = false

  constructor(config: RTCConfiguration) {
    this.config = config
    FakePeerConnection.instances.push(this)
  }

  addTrack(track: MediaStreamTrack): RTCRtpSender {
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

  fireTrack(stream: MediaStream) {
    if (!this.ontrack) return
    this.ontrack({ streams: [stream], track: stream.getTracks()[0] } as unknown as RTCTrackEvent)
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

function installFetchMock(joinParticipants: number[] = []) {
  const calls: FetchCall[] = []
  const mock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = (init?.method ?? 'GET').toUpperCase()
    const body = typeof init?.body === 'string' ? init.body : undefined
    calls.push({ url, method, body })

    if (url.endsWith('/api/familychat/turn') && method === 'GET') {
      return new Response(JSON.stringify({
        iceServers: [{ urls: ['stun:stun.example.com:3478'] }],
        ttl: 3600,
      }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }
    if (url.includes('/calls/') && url.endsWith('/join') && method === 'POST') {
      return new Response(JSON.stringify({ participants: joinParticipants }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    if (url.includes('/calls/') && method === 'POST') {
      return new Response(null, { status: 204 })
    }
    return new Response('not found', { status: 404 })
  })
  vi.stubGlobal('fetch', mock)
  return { calls }
}

function findCall(calls: FetchCall[], method: string, urlSubstring: string): FetchCall | undefined {
  return calls.find(c => c.method === method && c.url.includes(urlSubstring))
}

function installRtcGlobals() {
  vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
  const stream = makeFakeMediaStream()
  const getUserMedia = vi.fn(async () => stream)
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia },
  })
  return { getUserMedia, stream }
}

function offerPayload(from: number, to: number, callId: string): CallSignalPayload {
  return {
    conversation_id: 7,
    call_id: callId,
    from_user_id: from,
    data: { type: 'offer', sdp: 'v=0\r\nremote-offer' },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ...({ to_user_id: to } as any),
  }
}

// ── Tests ──────────────────────────────────────────────────────────────────────

let originalMediaDevicesDescriptor: PropertyDescriptor | undefined

beforeEach(() => {
  FakePeerConnection.instances = []
  originalMediaDevicesDescriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices')
})

afterEach(() => {
  vi.unstubAllGlobals()
  if (originalMediaDevicesDescriptor !== undefined) {
    Object.defineProperty(navigator, 'mediaDevices', originalMediaDevicesDescriptor)
  } else {
    delete (navigator as unknown as Record<string, unknown>).mediaDevices
  }
})

describe('useGroupCall — starting a call', () => {
  it('joins the room and offers to every higher-id peer', async () => {
    const fetchMock = installFetchMock([2, 3]) // existing peers
    installRtcGlobals()
    const factory = vi.fn(makeFakePeerConnection)

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 1, // lower than both peers → offers to both
      rtcPeerConnectionFactory: factory,
      generateCallId: () => 'grp-A',
    }))

    await act(async () => { await result.current.startCall('video') })

    expect(result.current.state).toBe('active')
    expect(result.current.callId).toBe('grp-A')
    expect(result.current.callKind).toBe('video')

    // join POST happened.
    const joinCall = findCall(fetchMock.calls, 'POST', '/calls/grp-A/join')
    expect(joinCall).toBeDefined()

    // One offer per peer, each addressed via to_user_id.
    await waitFor(() => {
      const offers = fetchMock.calls.filter(c => c.method === 'POST' && c.url.includes('/offer'))
      expect(offers).toHaveLength(2)
    })
    const offers = fetchMock.calls.filter(c => c.url.includes('/offer'))
    const targets = offers.map(o => JSON.parse(o.body ?? '{}').to_user_id).sort()
    expect(targets).toEqual([2, 3])
    // Two peer connections (one per peer).
    expect(factory).toHaveBeenCalledTimes(2)
    expect(result.current.participants.map(p => p.userId)).toEqual([2, 3])
  })

  it('does not offer to lower-id peers (waits for their offer instead)', async () => {
    const fetchMock = installFetchMock([2]) // peer 2 is lower than us
    installRtcGlobals()

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 5, // higher than peer 2 → must wait
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      generateCallId: () => 'grp-B',
    }))

    await act(async () => { await result.current.startCall('voice') })

    // No offer sent; we wait for peer 2 to offer.
    expect(fetchMock.calls.some(c => c.url.includes('/offer'))).toBe(false)
  })
})

describe('useGroupCall — answering & mesh maintenance', () => {
  it('answers an inbound targeted offer', async () => {
    const fetchMock = installFetchMock([2])
    installRtcGlobals()

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 5,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      generateCallId: () => 'grp-C',
    }))

    await act(async () => { await result.current.startCall('voice') })
    await act(async () => {
      await result.current.handleSignalEvent('call_offer', offerPayload(2, 5, 'grp-C'))
    })

    const answer = findCall(fetchMock.calls, 'POST', '/answer')
    expect(answer).toBeDefined()
    expect(JSON.parse(answer!.body ?? '{}').to_user_id).toBe(2)
    expect(result.current.participants.map(p => p.userId)).toContain(2)
  })

  it('ignores offers addressed to a different member', async () => {
    const fetchMock = installFetchMock([])
    installRtcGlobals()

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 5,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      generateCallId: () => 'grp-D',
    }))

    await act(async () => { await result.current.startCall('voice') })
    await act(async () => {
      // Offer addressed to user 9, not us.
      await result.current.handleSignalEvent('call_offer', offerPayload(2, 9, 'grp-D'))
    })

    expect(fetchMock.calls.some(c => c.url.includes('/answer'))).toBe(false)
  })

  it('offers to a newcomer who joins after us (when our id is lower)', async () => {
    const fetchMock = installFetchMock([])
    installRtcGlobals()

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      generateCallId: () => 'grp-E',
    }))

    await act(async () => { await result.current.startCall('voice') })
    await act(async () => {
      await result.current.handleSignalEvent('call_join', {
        conversation_id: 7, call_id: 'grp-E', from_user_id: 4, kind: 'voice',
      })
    })

    await waitFor(() => {
      expect(fetchMock.calls.some(c => c.url.includes('/offer'))).toBe(true)
    })
    expect(result.current.participants.map(p => p.userId)).toContain(4)
  })

  it('removes a participant on call_leave', async () => {
    installFetchMock([2])
    installRtcGlobals()

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 5,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      generateCallId: () => 'grp-F',
    }))

    await act(async () => { await result.current.startCall('voice') })
    await act(async () => {
      await result.current.handleSignalEvent('call_offer', offerPayload(2, 5, 'grp-F'))
    })
    expect(result.current.participants.map(p => p.userId)).toContain(2)

    await act(async () => {
      await result.current.handleSignalEvent('call_leave', {
        conversation_id: 7, call_id: 'grp-F', from_user_id: 2,
      })
    })
    expect(result.current.participants.map(p => p.userId)).not.toContain(2)
  })
})

describe('useGroupCall — incoming call', () => {
  it('surfaces an incoming call from call_join while idle, then joins', async () => {
    const fetchMock = installFetchMock([1])
    installRtcGlobals()

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 5,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      generateCallId: () => 'unused',
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_join', {
        conversation_id: 7, call_id: 'grp-G', from_user_id: 1, kind: 'video',
      })
    })

    expect(result.current.state).toBe('idle')
    expect(result.current.incomingCall).toEqual({ callId: 'grp-G', kind: 'video', fromUserId: 1 })

    await act(async () => { await result.current.joinCall() })

    expect(result.current.state).toBe('active')
    expect(result.current.callId).toBe('grp-G')
    expect(findCall(fetchMock.calls, 'POST', '/calls/grp-G/join')).toBeDefined()
  })

  it('clears the incoming banner on a terminal call_end', async () => {
    installFetchMock([])
    installRtcGlobals()

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 5,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
    }))

    await act(async () => {
      await result.current.handleSignalEvent('call_join', {
        conversation_id: 7, call_id: 'grp-H', from_user_id: 1, kind: 'voice',
      })
    })
    expect(result.current.incomingCall).not.toBeNull()

    await act(async () => {
      await result.current.handleSignalEvent('call_end', {
        conversation_id: 7, call_id: 'grp-H', from_user_id: 1,
      })
    })
    expect(result.current.incomingCall).toBeNull()
  })
})

describe('useGroupCall — leaving', () => {
  it('posts leave and tears down on leaveCall', async () => {
    const fetchMock = installFetchMock([2])
    installRtcGlobals()

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 5,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      generateCallId: () => 'grp-I',
    }))

    await act(async () => { await result.current.startCall('voice') })
    await act(async () => {
      await result.current.handleSignalEvent('call_offer', offerPayload(2, 5, 'grp-I'))
    })
    await act(async () => { await result.current.leaveCall() })

    expect(findCall(fetchMock.calls, 'POST', '/calls/grp-I/leave')).toBeDefined()
    expect(result.current.state).toBe('idle')
    expect(result.current.participants).toEqual([])
    expect(result.current.callId).toBeNull()
  })

  it('retries leave on server error and still tears down locally', async () => {
    installRtcGlobals()
    let leaveAttempts = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString()
      const method = (init?.method ?? 'GET').toUpperCase()
      if (url.endsWith('/api/familychat/turn')) {
        return new Response(JSON.stringify({ iceServers: [], ttl: 3600 }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      }
      if (url.endsWith('/join') && method === 'POST') {
        return new Response(JSON.stringify({ participants: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      }
      if (url.endsWith('/leave') && method === 'POST') {
        leaveAttempts++
        return new Response('internal error', { status: 500 })
      }
      return new Response(null, { status: 204 })
    }))

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 5,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      generateCallId: () => 'grp-retry',
    }))

    await act(async () => { await result.current.startCall('voice') })
    await act(async () => { await result.current.leaveCall() })

    expect(leaveAttempts).toBe(2)
    expect(result.current.state).toBe('idle')
  })
})

describe('useGroupCall — camera toggle (replaceTrack)', () => {
  it('replaces video sender tracks across all peer connections when toggling camera', async () => {
    installFetchMock([2, 3])
    const videoStream = makeFakeVideoMediaStream()
    const getUserMedia = vi.fn(async () => videoStream)
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia },
    })

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      getUserMedia,
      generateCallId: () => 'grp-cam',
    }))

    await act(async () => { await result.current.startCall('video') })

    // Wait for offers to be sent (userId 1 < peers 2,3)
    await waitFor(() => {
      expect(FakePeerConnection.instances).toHaveLength(2)
    })

    // Each PC should have a video sender from addTrack
    const videoSenders = FakePeerConnection.instances.flatMap(pc =>
      pc.senders.filter(s => (s.track as unknown as FakeMediaStreamTrack)?.kind === 'video')
    )
    expect(videoSenders).toHaveLength(2)

    // Disable camera — replaceTrack(null) on every video sender
    await act(async () => { await result.current.setCameraEnabled(false) })

    expect(result.current.cameraEnabled).toBe(false)
    for (const sender of videoSenders) {
      expect(sender.replaceTrack).toHaveBeenCalledWith(null)
    }

    // Re-enable camera — replaceTrack(videoTrack) on every video sender
    const videoTrack = videoStream.getVideoTracks()[0]
    await act(async () => { await result.current.setCameraEnabled(true) })

    expect(result.current.cameraEnabled).toBe(true)
    for (const sender of videoSenders) {
      expect(sender.replaceTrack).toHaveBeenCalledWith(videoTrack)
    }
  })

  it('falls back to track.enabled when replaceTrack throws', async () => {
    installFetchMock([2])
    const videoStream = makeFakeVideoMediaStream()
    const getUserMedia = vi.fn(async () => videoStream)
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia },
    })

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      getUserMedia,
      generateCallId: () => 'grp-cam-fail',
    }))

    await act(async () => { await result.current.startCall('video') })

    await waitFor(() => {
      expect(FakePeerConnection.instances).toHaveLength(1)
    })

    // Make replaceTrack throw
    const pc = FakePeerConnection.instances[0]
    const videoSender = pc.senders.find(s => (s.track as unknown as FakeMediaStreamTrack)?.kind === 'video')
    expect(videoSender).toBeDefined()
    videoSender!.replaceTrack.mockRejectedValueOnce(new Error('replaceTrack failed'))

    const videoTrack = videoStream.getVideoTracks()[0] as unknown as FakeMediaStreamTrack

    await act(async () => { await result.current.setCameraEnabled(false) })

    // Falls back to setting track.enabled
    expect(videoTrack.enabled).toBe(false)
    expect(result.current.cameraEnabled).toBe(false)
  })

  it('is a no-op for voice-only calls', async () => {
    installFetchMock([])
    installRtcGlobals()

    const { result } = renderHook(() => useGroupCall({
      conversationId: 7,
      userId: 1,
      rtcPeerConnectionFactory: vi.fn(makeFakePeerConnection),
      generateCallId: () => 'grp-voice-cam',
    }))

    await act(async () => { await result.current.startCall('voice') })
    await act(async () => { await result.current.setCameraEnabled(false) })

    // Camera state unchanged for voice calls
    expect(result.current.cameraEnabled).toBe(true)
  })
})
