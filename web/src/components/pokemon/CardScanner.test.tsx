// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import CardScanner from './CardScanner'
import { detectCardRectangle, isWithinTolerance } from './rectangleDetector'
import type { DetectedRectangle } from './rectangleDetector'

vi.mock('./rectangleDetector', () => ({
  detectCardRectangle: vi.fn().mockReturnValue(null),
  isWithinTolerance: vi.fn().mockReturnValue(false),
  TARGET_ASPECT_RATIO: 0.716,
}))

const TRANSLATIONS: Record<string, string> = {
  'scanner.dialogLabel': 'Scan a Pokémon card',
  'scanner.requesting': 'Requesting camera access…',
  'scanner.permissionDenied': 'Camera access was denied. You can still add cards by searching.',
  'scanner.cameraUnavailable': 'Camera is unavailable. You can still add cards by searching.',
  'scanner.unsupported': "Camera scanning isn't supported in this browser. Use the search instead.",
  'scanner.enterManually': 'Enter card manually',
  'scanner.shutter': 'Capture card',
  'scanner.torchOn': 'Turn flashlight on',
  'scanner.torchOff': 'Turn flashlight off',
  'scanner.close': 'Close scanner',
  'scanner.holdSteady': 'Hold steady…',
  'scanner.scanning': 'Sending…',
  'scanner.queuedToast': 'Sent ✓',
  'scanner.queuedToastLink': 'View in Scanned',
  'scanner.dailyLimit': 'Daily scan limit reached',
  'scanner.errors.scanFailed': 'Scan failed, try again',
  'scanner.errors.timedOut': 'Scan timed out, try again',
  'scanner.preview.title': 'Preview captured card',
  'scanner.preview.imageAlt': 'Captured card preview',
  'scanner.preview.send': 'Send',
  'scanner.preview.retake': 'Retake',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  if (key === 'tile.collectorNo') return `#${opts?.number ?? ''}`
  if (key === 'addCard.toast.added') return `Added ${opts?.name ?? ''}`
  if (key === 'addCard.errors.addFailed') return 'Failed to add card'
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../../i18n', () => ({
  default: { language: 'en' },
}))

function renderScanner(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

function makeTrack(opts: { torch?: boolean } = {}) {
  const stop = vi.fn()
  const applyConstraints = vi.fn().mockResolvedValue(undefined)
  const getCapabilities = vi.fn(() => ({ torch: opts.torch ?? false }))
  return {
    track: {
      stop,
      applyConstraints,
      getCapabilities,
      kind: 'video' as const,
    },
    stop,
    applyConstraints,
  }
}

function makeStream(track: ReturnType<typeof makeTrack>['track']) {
  return {
    getTracks: () => [track],
    getVideoTracks: () => [track],
  } as unknown as MediaStream
}

// Save/restore navigator.mediaDevices descriptor so each describe block's
// Object.defineProperty call doesn't leak into subsequent tests.
let savedMediaDevicesDescriptor: PropertyDescriptor | undefined

beforeEach(() => {
  savedMediaDevicesDescriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices')
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  if (savedMediaDevicesDescriptor !== undefined) {
    Object.defineProperty(navigator, 'mediaDevices', savedMediaDevicesDescriptor)
  } else {
    // mediaDevices was not originally an own property; remove the mock added by
    // each describe's beforeEach so it doesn't leak into subsequent test files.
    try {
      delete (navigator as unknown as Record<string, unknown>).mediaDevices
    } catch {
      Object.defineProperty(navigator, 'mediaDevices', {
        configurable: true,
        writable: true,
        value: undefined,
      })
    }
  }
})

describe('CardScanner — unsupported browser', () => {
  beforeEach(() => {
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: undefined,
    })
  })

  it('shows the unsupported fallback and a manual-entry button that calls onClose', async () => {
    const onClose = vi.fn()
    renderScanner(<CardScanner onClose={onClose} />)

    expect(
      await screen.findByText("Camera scanning isn't supported in this browser. Use the search instead."),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('card-scanner-manual-entry'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})

describe('CardScanner — permission denied', () => {
  beforeEach(() => {
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        getUserMedia: vi.fn().mockRejectedValue(new DOMException('denied', 'NotAllowedError')),
      },
    })
  })

  it('renders denied fallback and manual-entry button closes the scanner', async () => {
    const onClose = vi.fn()
    renderScanner(<CardScanner onClose={onClose} />)

    expect(
      await screen.findByText('Camera access was denied. You can still add cards by searching.'),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('card-scanner-manual-entry'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})

describe('CardScanner — granted', () => {
  let trackHandle: ReturnType<typeof makeTrack>
  let stream: MediaStream

  beforeEach(() => {
    trackHandle = makeTrack({ torch: true })
    stream = makeStream(trackHandle.track)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
  })

  it('shows the camera view, guide, shutter, torch toggle, and close button', async () => {
    renderScanner(<CardScanner onClose={vi.fn()} />)

    await waitFor(() => {
      expect(screen.getByTestId('card-scanner-video')).toBeInTheDocument()
    })
    expect(screen.getByTestId('card-scanner-guide')).toBeInTheDocument()
    expect(screen.getByTestId('card-scanner-shutter')).toBeInTheDocument()
    expect(screen.getByTestId('card-scanner-close')).toBeInTheDocument()
    expect(screen.getByTestId('card-scanner-torch')).toBeInTheDocument()
  })

  it('stops all tracks when the close button is clicked and calls onClose', async () => {
    const onClose = vi.fn()
    renderScanner(<CardScanner onClose={onClose} />)

    await waitFor(() => expect(screen.getByTestId('card-scanner-close')).toBeInTheDocument())
    fireEvent.click(screen.getByTestId('card-scanner-close'))

    expect(trackHandle.stop).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('applies torch constraint when the torch button is toggled', async () => {
    renderScanner(<CardScanner onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByTestId('card-scanner-torch')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('card-scanner-torch'))

    await waitFor(() => {
      expect(trackHandle.applyConstraints).toHaveBeenCalledWith({
        advanced: [{ torch: true }],
      })
    })
  })
})

describe('CardScanner — torch not supported', () => {
  beforeEach(() => {
    const trackHandle = makeTrack({ torch: false })
    const stream = makeStream(trackHandle.track)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
  })

  it('hides the torch button when the track does not advertise torch capability', async () => {
    renderScanner(<CardScanner onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('card-scanner-shutter')).toBeInTheDocument())
    expect(screen.queryByTestId('card-scanner-torch')).not.toBeInTheDocument()
  })
})

describe('CardScanner — fire-and-forget queue flow', () => {
  let pendingRafs: FrameRequestCallback[]

  beforeEach(() => {
    const trackHandle = makeTrack({ torch: false })
    const stream = makeStream(trackHandle.track)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })

    pendingRafs = []
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      pendingRafs.push(cb)
      return pendingRafs.length
    })
    vi.stubGlobal('cancelAnimationFrame', vi.fn())

    // Stub URL.createObjectURL / revokeObjectURL so the preview overlay can
    // produce a usable src without needing real Blob URL plumbing in happy-dom.
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})

    vi.mocked(detectCardRectangle).mockReturnValue(null)
    vi.mocked(isWithinTolerance).mockReturnValue(false)
  })

  async function primeVideoAndLoop() {
    await waitFor(() => expect(screen.getByTestId('card-scanner-video')).toBeInTheDocument())
    const video = screen.getByTestId('card-scanner-video') as HTMLVideoElement
    Object.defineProperty(video, 'videoWidth', { value: 640, configurable: true })
    Object.defineProperty(video, 'videoHeight', { value: 480, configurable: true })
    Object.defineProperty(video, 'readyState', { value: 4, configurable: true })
    return video
  }

  // lockAndCaptureWithFetch wires up a detector that locks immediately, a
  // canvas that emits a JPEG blob synchronously, and a fetch mock that the
  // caller controls. The returned `fetchMock` lets tests assert on the calls
  // — and override the response per test case (202 happy path vs 429 cap).
  function lockAndCaptureWithFetch(
    fetchMock: ReturnType<typeof vi.fn>,
    detection: DetectedRectangle = { x: 100, y: 80, w: 200, h: 280, score: 0.9 },
  ) {
    vi.mocked(detectCardRectangle).mockReturnValue(detection)
    vi.mocked(isWithinTolerance).mockReturnValue(true)

    const fakeBlob = new Blob(['img'], { type: 'image/jpeg' })
    vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (cb) {
      cb(fakeBlob)
    })
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
      drawImage: vi.fn(),
      getImageData: vi.fn().mockReturnValue({
        data: new Uint8ClampedArray(640 * 480 * 4),
        width: 640,
        height: 480,
        colorSpace: 'srgb',
      }),
    } as unknown as CanvasRenderingContext2D)

    vi.stubGlobal('fetch', fetchMock)
    return { fakeBlob }
  }

  // advanceToPostThroughPreview ticks the rAF loop to the lock+capture point
  // and then clicks Send on the preview overlay so the scan POST fires.
  async function advanceToPostThroughPreview() {
    await act(async () => { pendingRafs.shift()?.(0) })
    await act(async () => { pendingRafs.shift()?.(600) })
    await act(async () => { pendingRafs.shift()?.(1200) })
    await screen.findByTestId('card-scanner-preview-send')
    fireEvent.click(screen.getByTestId('card-scanner-preview-send'))
  }

  it('202 queued: shows the inline toast, no result modal, scanner returns to searching', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      headers: new Headers(),
      json: () => Promise.resolve({ id: 7, status: 'queued' }),
    } as Response)
    lockAndCaptureWithFetch(fetchMock)
    const onAdded = vi.fn()
    renderScanner(<CardScanner onClose={vi.fn()} onAdded={onAdded} />)

    await primeVideoAndLoop()
    await advanceToPostThroughPreview()

    await waitFor(() => {
      const queueCalls = fetchMock.mock.calls.filter(([url]) => url === '/api/pokemon/scans/queue')
      expect(queueCalls.length).toBe(1)
    })

    // The inline "Sent ✓ — view in Scanned" toast surfaces over the overlay.
    await screen.findByTestId('card-scanner-queued-toast')
    expect(screen.getByTestId('card-scanner-queued-toast-link')).toHaveAttribute(
      'href',
      '/pokemon/scanned',
    )

    // Result modal does NOT exist — the result lives in the Scanned page now.
    expect(screen.queryByTestId('scan-result-modal')).not.toBeInTheDocument()

    // Scanner is back to its searching UI — the shutter is enabled again.
    expect(screen.getByTestId('card-scanner-shutter')).not.toBeDisabled()

    // onAdded callback is invoked so the parent (AddCardPanel) can refresh
    // any background counts it cares about.
    expect(onAdded).toHaveBeenCalledTimes(1)
  })

  it('202 with X-Pokemon-Scan-Dedupe header is treated identically to a fresh queue', async () => {
    const dedupeHeaders = new Headers()
    dedupeHeaders.set('X-Pokemon-Scan-Dedupe', 'true')
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      headers: dedupeHeaders,
      json: () => Promise.resolve({ id: 7, status: 'queued' }),
    } as Response)
    lockAndCaptureWithFetch(fetchMock)
    renderScanner(<CardScanner onClose={vi.fn()} />)

    await primeVideoAndLoop()
    await advanceToPostThroughPreview()

    await screen.findByTestId('card-scanner-queued-toast')
    expect(screen.queryByTestId('scan-result-modal')).not.toBeInTheDocument()
  })

  it('429 daily-limit response renders the inline error and stops the auto-capture loop', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 429,
      headers: new Headers(),
      json: () => Promise.resolve({ error: 'daily scan cap reached', cap: 600, used: 600 }),
    } as Response)
    lockAndCaptureWithFetch(fetchMock)
    renderScanner(<CardScanner onClose={vi.fn()} />)

    await primeVideoAndLoop()
    await advanceToPostThroughPreview()

    // Daily-limit message renders and the shutter is disabled so the kid
    // can't keep auto-firing into more 429s.
    await screen.findByTestId('card-scanner-daily-limit')
    expect(screen.getByText('Daily scan limit reached')).toBeInTheDocument()
    expect(screen.getByTestId('card-scanner-shutter')).toBeDisabled()
  })

  it('2s cooldown after a successful queue suppresses immediate auto re-fire', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      headers: new Headers(),
      json: () => Promise.resolve({ id: 7, status: 'queued' }),
    } as Response)
    lockAndCaptureWithFetch(fetchMock)
    const pinnedNow = 1_000_000
    const dateNowSpy = vi.spyOn(Date, 'now').mockReturnValue(pinnedNow)

    renderScanner(<CardScanner onClose={vi.fn()} />)
    await primeVideoAndLoop()
    await advanceToPostThroughPreview()

    await waitFor(() => {
      const queueCalls = fetchMock.mock.calls.filter(([url]) => url === '/api/pokemon/scans/queue')
      expect(queueCalls.length).toBe(1)
    })

    // cooldownUntilRef.current = pinnedNow + 2000 = 1_002_000. Date.now() is
    // still pinnedNow = 1_000_000 < 1_002_000 so the rAF gate suppresses a
    // second /scans/queue POST.
    const before = fetchMock.mock.calls.filter(([u]) => u === '/api/pokemon/scans/queue').length
    await act(async () => { pendingRafs.shift()?.(1800) })
    await act(async () => { pendingRafs.shift()?.(2400) })
    await act(async () => { pendingRafs.shift()?.(3000) })
    const after = fetchMock.mock.calls.filter(([u]) => u === '/api/pokemon/scans/queue').length
    expect(after).toBe(before)

    dateNowSpy.mockRestore()
  })

  it('aborts the in-flight scan after the timeout fires and resumes scanning', async () => {
    let capturedAbortFn: (() => void) | null = null
    const realSetTimeout = globalThis.setTimeout
    vi.spyOn(globalThis, 'setTimeout').mockImplementation(
      (fn: TimerHandler, delay?: number, ...args: unknown[]) => {
        if (delay === 30_000) {
          capturedAbortFn = fn as () => void
          return -1 as unknown as ReturnType<typeof setTimeout>
        }
        return realSetTimeout(fn as TimerHandler, delay, ...(args as []))
      },
    )

    // fetch never resolves naturally; it rejects when the abort fires.
    const fetchMock = vi.fn((_url: string, init?: RequestInit) =>
      new Promise<Response>((_, reject) => {
        init?.signal?.addEventListener('abort', () =>
          reject(new DOMException('The operation was aborted.', 'AbortError')),
        )
      }),
    )
    lockAndCaptureWithFetch(fetchMock)

    renderScanner(<CardScanner onClose={vi.fn()} />)
    await primeVideoAndLoop()
    await advanceToPostThroughPreview()

    await screen.findByTestId('card-scanner-spinner')
    expect(capturedAbortFn).not.toBeNull()

    await act(async () => { capturedAbortFn!() })

    expect(screen.getByText('Scan timed out, try again')).toBeInTheDocument()
    expect(screen.queryByTestId('card-scanner-spinner')).not.toBeInTheDocument()
  })

  it('manual capture bypasses the lock, previews, then POSTs to the queue', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      headers: new Headers(),
      json: () => Promise.resolve({ id: 7, status: 'queued' }),
    } as Response)
    lockAndCaptureWithFetch(fetchMock)
    // Override the detector to NEVER lock — but the manual shutter should
    // still produce a queue POST via the preview overlay.
    vi.mocked(detectCardRectangle).mockReturnValue(null)
    vi.mocked(isWithinTolerance).mockReturnValue(false)
    renderScanner(<CardScanner onClose={vi.fn()} />)

    await primeVideoAndLoop()
    fireEvent.click(screen.getByTestId('card-scanner-shutter'))

    await screen.findByTestId('card-scanner-preview-send')
    expect(
      fetchMock.mock.calls.filter(([url]) => url === '/api/pokemon/scans/queue').length,
    ).toBe(0)

    fireEvent.click(screen.getByTestId('card-scanner-preview-send'))

    await waitFor(() => {
      const queueCalls = fetchMock.mock.calls.filter(([url]) => url === '/api/pokemon/scans/queue')
      expect(queueCalls.length).toBe(1)
    })
    await screen.findByTestId('card-scanner-queued-toast')
  })

  it('Retake discards the capture and returns to searching without POSTing', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      headers: new Headers(),
      json: () => Promise.resolve({ id: 7, status: 'queued' }),
    } as Response)
    lockAndCaptureWithFetch(fetchMock)
    renderScanner(<CardScanner onClose={vi.fn()} />)

    await primeVideoAndLoop()
    await act(async () => { pendingRafs.shift()?.(0) })
    await act(async () => { pendingRafs.shift()?.(600) })
    await act(async () => { pendingRafs.shift()?.(1200) })

    await screen.findByTestId('card-scanner-preview-retake')
    fireEvent.click(screen.getByTestId('card-scanner-preview-retake'))

    await waitFor(() => {
      expect(screen.queryByTestId('card-scanner-preview-send')).not.toBeInTheDocument()
    })
    expect(
      fetchMock.mock.calls.filter(([url]) => url === '/api/pokemon/scans/queue').length,
    ).toBe(0)
    expect(screen.getByTestId('card-scanner-shutter')).not.toBeDisabled()
  })

  it('preview auto-send timer fires the queue POST without user input', async () => {
    let previewAutoSend: (() => void) | null = null
    const realSetTimeout = globalThis.setTimeout
    vi.spyOn(globalThis, 'setTimeout').mockImplementation(
      (fn: TimerHandler, delay?: number, ...args: unknown[]) => {
        if (delay === 1500) {
          previewAutoSend = fn as () => void
          return -1 as unknown as ReturnType<typeof setTimeout>
        }
        return realSetTimeout(fn as TimerHandler, delay, ...(args as []))
      },
    )

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      headers: new Headers(),
      json: () => Promise.resolve({ id: 7, status: 'queued' }),
    } as Response)
    lockAndCaptureWithFetch(fetchMock)
    renderScanner(<CardScanner onClose={vi.fn()} />)

    await primeVideoAndLoop()
    await act(async () => { pendingRafs.shift()?.(0) })
    await act(async () => { pendingRafs.shift()?.(600) })
    await act(async () => { pendingRafs.shift()?.(1200) })

    await screen.findByTestId('card-scanner-preview-send')
    expect(previewAutoSend).not.toBeNull()

    await act(async () => { previewAutoSend!() })

    await waitFor(() => {
      const queueCalls = fetchMock.mock.calls.filter(([url]) => url === '/api/pokemon/scans/queue')
      expect(queueCalls.length).toBe(1)
    })
  })
})

describe('CardScanner — getUserMedia constraints', () => {
  let getUserMediaMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    const trackHandle = makeTrack({ torch: false })
    const stream = makeStream(trackHandle.track)
    getUserMediaMock = vi.fn().mockResolvedValue(stream)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: getUserMediaMock },
    })
  })

  it('requests 1080p video so the cropped card has enough pixels for Claude vision', async () => {
    renderScanner(<CardScanner onClose={vi.fn()} />)

    await waitFor(() => expect(getUserMediaMock).toHaveBeenCalledTimes(1))
    const constraints = getUserMediaMock.mock.calls[0][0] as {
      video?: {
        facingMode?: { ideal?: string }
        width?: { ideal?: number }
        height?: { ideal?: number }
      }
    }
    expect(constraints.video?.width?.ideal).toBe(1920)
    expect(constraints.video?.height?.ideal).toBe(1080)
    expect(constraints.video?.facingMode?.ideal).toBe('environment')
  })
})
