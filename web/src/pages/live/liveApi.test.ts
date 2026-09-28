// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { HttpError, formatBytes, formatDuration, negotiate, preferH264 } from './liveApi'

afterEach(() => {
  vi.unstubAllGlobals()
})

function fakePeer() {
  return {
    iceGatheringState: 'complete',
    localDescription: { sdp: 'v=0\r\nlocal\r\n' },
    createOffer: vi.fn(() => Promise.resolve({ type: 'offer', sdp: 'v=0\r\noffer\r\n' })),
    setLocalDescription: vi.fn(() => Promise.resolve()),
    setRemoteDescription: vi.fn(() => Promise.resolve()),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }
}

describe('negotiate', () => {
  it('POSTs the gathered offer as application/sdp and applies the answer', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response('v=0\r\nanswer\r\n', {
      status: 201,
      headers: { Location: '/api/live/sessions/7/whip/abc' },
    })))
    vi.stubGlobal('fetch', fetchMock)
    const pc = fakePeer()

    const location = await negotiate(pc as unknown as RTCPeerConnection, '/api/live/sessions/7/whip')

    expect(location).toBe('/api/live/sessions/7/whip/abc')
    expect(fetchMock).toHaveBeenCalledWith('/api/live/sessions/7/whip', expect.objectContaining({
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/sdp' },
      body: 'v=0\r\nlocal\r\n',
    }))
    expect(pc.setRemoteDescription).toHaveBeenCalledWith({ type: 'answer', sdp: 'v=0\r\nanswer\r\n' })
  })

  it('throws HttpError with the status on a non-201 response', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('', { status: 404 }))))
    const err = await negotiate(fakePeer() as unknown as RTCPeerConnection, '/x').catch(e => e)
    expect(err).toBeInstanceOf(HttpError)
    expect((err as HttpError).status).toBe(404)
  })
})

describe('preferH264', () => {
  it('moves H.264 codecs to the front', () => {
    vi.stubGlobal('RTCRtpSender', {
      getCapabilities: () => ({
        codecs: [
          { mimeType: 'video/VP8', clockRate: 90000 },
          { mimeType: 'video/H264', clockRate: 90000 },
          { mimeType: 'video/rtx', clockRate: 90000 },
        ],
      }),
    })
    const setCodecPreferences = vi.fn()
    preferH264({ setCodecPreferences } as unknown as RTCRtpTransceiver)
    expect(setCodecPreferences.mock.calls[0][0].map((c: { mimeType: string }) => c.mimeType)).toEqual([
      'video/H264', 'video/VP8', 'video/rtx',
    ])
  })

  it('leaves preferences alone when H.264 is unavailable', () => {
    vi.stubGlobal('RTCRtpSender', { getCapabilities: () => ({ codecs: [{ mimeType: 'video/VP8', clockRate: 90000 }] }) })
    const setCodecPreferences = vi.fn()
    preferH264({ setCodecPreferences } as unknown as RTCRtpTransceiver)
    expect(setCodecPreferences).not.toHaveBeenCalled()
  })
})

describe('formatting', () => {
  it('formats durations', () => {
    expect(formatDuration(0)).toBe('0:00')
    expect(formatDuration(75)).toBe('1:15')
    expect(formatDuration(3723)).toBe('1:02:03')
  })

  it('formats data used', () => {
    expect(formatBytes(250_000_000, 'en')).toBe('250 MB')
    expect(formatBytes(1_250_000_000, 'en')).toBe('1.3 GB')
  })
})
