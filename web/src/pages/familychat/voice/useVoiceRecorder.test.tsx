// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { useVoiceRecorder } from './useVoiceRecorder'

// ── Mocks ─────────────────────────────────────────────────────────────────────

class FakeAudioTrack {
  kind = 'audio' as const
  stop = vi.fn()
}

interface FakeStreamShape {
  tracks: FakeAudioTrack[]
  getTracks: () => FakeAudioTrack[]
  getAudioTracks: () => FakeAudioTrack[]
  getVideoTracks: () => FakeAudioTrack[]
}

function makeFakeStream(): FakeStreamShape {
  const tracks = [new FakeAudioTrack()]
  return {
    tracks,
    getTracks: () => tracks,
    getAudioTracks: () => tracks,
    getVideoTracks: () => [],
  }
}

interface FakeRecorderShape {
  mimeType: string
  state: 'inactive' | 'recording' | 'paused'
  start: ReturnType<typeof vi.fn>
  stop: ReturnType<typeof vi.fn>
  ondataavailable: ((event: { data: Blob }) => void) | null
  onstop: (() => void) | null
  onerror: ((event: Event) => void) | null
  emitChunk: (size?: number) => void
  finishStop: () => void
  fireError: () => void
}

function makeFakeRecorder(mimeType: string): FakeRecorderShape {
  const rec: FakeRecorderShape = {
    mimeType,
    state: 'inactive',
    start: vi.fn(),
    stop: vi.fn(),
    ondataavailable: null,
    onstop: null,
    onerror: null,
    emitChunk(size = 32) {
      const blob = new Blob([new Uint8Array(size)], { type: mimeType })
      rec.ondataavailable?.({ data: blob })
    },
    finishStop() {
      rec.state = 'inactive'
      rec.ondataavailable?.({ data: new Blob([new Uint8Array(16)], { type: mimeType }) })
      rec.onstop?.()
    },
    fireError() {
      rec.onerror?.(new Event('error'))
    },
  }
  rec.start.mockImplementation(() => { rec.state = 'recording' })
  return rec
}

let lastRecorder: FakeRecorderShape | null = null

class FakeAnalyser {
  fftSize = 1024
  smoothingTimeConstant = 0.6
  getByteTimeDomainData(arr: Uint8Array) { arr.fill(128) }
}

class FakeAudioContext {
  state = 'running'
  createMediaStreamSource() {
    return {
      connect: vi.fn(),
      disconnect: vi.fn(),
    } as unknown as MediaStreamAudioSourceNode
  }
  createAnalyser() { return new FakeAnalyser() as unknown as AnalyserNode }
  close() { return Promise.resolve() }
}

function installPlatform(opts: { rejectGetUserMedia?: DOMException | null } = {}) {
  const stream = makeFakeStream()
  const getUserMedia = vi.fn(async () => {
    if (opts.rejectGetUserMedia) throw opts.rejectGetUserMedia
    return stream as unknown as MediaStream
  })
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia },
  })

  class FakeMediaRecorder {
    private rec: FakeRecorderShape
    state = 'inactive' as 'inactive' | 'recording' | 'paused'
    mimeType: string
    ondataavailable: ((event: { data: Blob }) => void) | null = null
    onstop: (() => void) | null = null
    onerror: ((event: Event) => void) | null = null
    constructor(_stream: MediaStream, options?: { mimeType?: string }) {
      const mime = options?.mimeType ?? 'audio/webm'
      this.mimeType = mime
      this.rec = makeFakeRecorder(mime)
      lastRecorder = this.rec
      // Bridge handler assignments onto the underlying fake.
      Object.defineProperty(this, 'ondataavailable', {
        get: () => this.rec.ondataavailable,
        set: (v) => { this.rec.ondataavailable = v },
      })
      Object.defineProperty(this, 'onstop', {
        get: () => this.rec.onstop,
        set: (v) => { this.rec.onstop = v },
      })
      Object.defineProperty(this, 'onerror', {
        get: () => this.rec.onerror,
        set: (v) => { this.rec.onerror = v },
      })
    }
    start(_timeslice?: number) {
      this.rec.start()
      this.state = 'recording'
    }
    stop() {
      this.rec.stop()
      this.state = 'inactive'
    }
    static isTypeSupported(_: string) { return true }
  }

  vi.stubGlobal('MediaRecorder', FakeMediaRecorder)
  vi.stubGlobal('AudioContext', FakeAudioContext)
  // requestAnimationFrame is provided by happy-dom but stub it to be a no-op
  // so we don't tie up the event loop with meter sampling we never assert on.
  vi.stubGlobal('requestAnimationFrame', vi.fn(() => 0))
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
  return { getUserMedia, stream }
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('useVoiceRecorder – start/stop/cancel/timeout', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    lastRecorder = null
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    try {
      delete (navigator as unknown as Record<string, unknown>).mediaDevices
    } catch {
      Object.defineProperty(navigator, 'mediaDevices', {
        configurable: true,
        value: undefined,
      })
    }
  })

  it('starts recording and exposes recording state', async () => {
    const { getUserMedia } = installPlatform()
    const { result } = renderHook(() => useVoiceRecorder({ maxDurationMs: 1000 }))

    expect(result.current.state).toBe('idle')
    expect(result.current.supported).toBe(true)

    await act(async () => {
      await result.current.start()
    })

    expect(getUserMedia).toHaveBeenCalledWith({ audio: true })
    expect(result.current.state).toBe('recording')
    expect(lastRecorder).not.toBeNull()
    expect(lastRecorder?.start).toHaveBeenCalled()
  })

  it('stop() resolves with the recorded blob and durationMs', async () => {
    installPlatform()
    const { result } = renderHook(() => useVoiceRecorder({ maxDurationMs: 5000, minDurationMs: 100 }))

    await act(async () => { await result.current.start() })

    // Advance fake time so the elapsed duration exceeds minDurationMs.
    await act(async () => {
      vi.advanceTimersByTime(800)
    })

    let stopResult: Awaited<ReturnType<typeof result.current.stop>> = null
    await act(async () => {
      const promise = result.current.stop()
      // MediaRecorder.stop is fake — fire the bridged events to drive
      // finaliseStop the same way the platform would.
      lastRecorder?.finishStop()
      stopResult = await promise
    })

    expect(stopResult).not.toBeNull()
    expect(stopResult!.blob).toBeInstanceOf(Blob)
    expect(stopResult!.blob.size).toBeGreaterThan(0)
    expect(stopResult!.mimeType).toBe('audio/webm')
    expect(stopResult!.durationMs).toBeGreaterThanOrEqual(800)
    expect(result.current.state).toBe('idle')
  })

  it('cancel() resolves stop with null and tears down the stream', async () => {
    const { stream } = installPlatform()
    const { result } = renderHook(() => useVoiceRecorder({ maxDurationMs: 5000 }))

    await act(async () => { await result.current.start() })
    await act(async () => { vi.advanceTimersByTime(500) })

    await act(async () => {
      result.current.cancel()
      lastRecorder?.finishStop()
    })

    expect(result.current.state).toBe('idle')
    expect(stream.tracks[0].stop).toHaveBeenCalled()
  })

  it('stops automatically when the max-duration timeout fires', async () => {
    installPlatform()
    const onAutoComplete = vi.fn()
    const { result } = renderHook(() => useVoiceRecorder({
      maxDurationMs: 1000,
      minDurationMs: 100,
      onAutoComplete,
    }))

    await act(async () => { await result.current.start() })

    // Fire the cap timer; the hook's max timeout calls recorder.stop().
    await act(async () => {
      vi.advanceTimersByTime(1000)
    })

    expect(lastRecorder?.stop).toHaveBeenCalled()
    // Drive ondataavailable + onstop the way a real MediaRecorder would.
    await act(async () => {
      lastRecorder?.finishStop()
    })
    expect(result.current.state).toBe('idle')
    expect(onAutoComplete).toHaveBeenCalledTimes(1)
    const arg = onAutoComplete.mock.calls[0][0]
    expect(arg).not.toBeNull()
    expect(arg.blob).toBeInstanceOf(Blob)
    expect(arg.durationMs).toBeGreaterThanOrEqual(1000)
  })

  it('handles a denied permission as state=error', async () => {
    installPlatform({ rejectGetUserMedia: new DOMException('denied', 'NotAllowedError') })
    const { result } = renderHook(() => useVoiceRecorder())

    await act(async () => { await result.current.start() })

    expect(result.current.state).toBe('error')
    expect(result.current.error).toBe('permission')
  })

  it('reports unsupported when MediaRecorder is not available', async () => {
    // Don't install MediaRecorder — only set up navigator.mediaDevices.
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn() },
    })
    vi.stubGlobal('MediaRecorder', undefined as unknown as typeof MediaRecorder)

    const { result } = renderHook(() => useVoiceRecorder())
    expect(result.current.supported).toBe(false)

    await act(async () => { await result.current.start() })
    expect(result.current.state).toBe('error')
    expect(result.current.error).toBe('unsupported')
  })

  it('drops recordings shorter than minDurationMs', async () => {
    installPlatform()
    const { result } = renderHook(() => useVoiceRecorder({ minDurationMs: 1000 }))

    await act(async () => { await result.current.start() })
    await act(async () => { vi.advanceTimersByTime(120) })

    let stopResult: Awaited<ReturnType<typeof result.current.stop>> = null
    await act(async () => {
      const promise = result.current.stop()
      lastRecorder?.finishStop()
      stopResult = await promise
    })

    expect(stopResult).toBeNull()
  })

  it('arms the swipe-up cancel when pointer delta exceeds threshold', async () => {
    installPlatform()
    const { result } = renderHook(() => useVoiceRecorder({ cancelThresholdPx: 60 }))
    await act(async () => { await result.current.start() })

    act(() => { result.current.setPointerDelta(-30) })
    expect(result.current.cancelArmed).toBe(false)

    act(() => { result.current.setPointerDelta(-80) })
    expect(result.current.cancelArmed).toBe(true)

    act(() => { result.current.resetPointer() })
    expect(result.current.cancelArmed).toBe(false)
  })

  it('updates elapsedMs while recording', async () => {
    installPlatform()
    const { result } = renderHook(() => useVoiceRecorder({ maxDurationMs: 5000 }))

    await act(async () => { await result.current.start() })
    await act(async () => { vi.advanceTimersByTime(450) })

    await waitFor(() => {
      expect(result.current.elapsedMs).toBeGreaterThanOrEqual(400)
    })
    expect(result.current.remainingMs).toBeLessThanOrEqual(4600)
  })
})
