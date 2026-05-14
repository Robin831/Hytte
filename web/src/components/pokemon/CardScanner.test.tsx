// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
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
  'scanner.scanning': 'Identifying card…',
  'scanner.errors.scanFailed': 'Scan failed, try again',
  'scanner.errors.timedOut': 'Scan timed out, try again',
  'scanner.errors.noVariant': 'This card has no variants to add',
  'scanner.result.multiLabel': 'Multiple matches',
  'scanner.result.unmatchedLabel': 'Card not recognised',
  'scanner.result.pickCandidate': 'Multiple matches — pick the right card.',
  'scanner.result.candidatesList': 'Candidate cards',
  'scanner.result.yesAdd': 'Yes, add to collection',
  'scanner.result.tryAgain': 'Try again',
  'scanner.result.enterManually': 'Enter manually',
  'scanner.result.noMatch': "Couldn't read the card.",
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  if (key === 'scanner.result.singleLabel') return `Scan match: ${opts?.name ?? ''}`
  if (key === 'scanner.result.confidence') return `Confidence: ${opts?.percent ?? 0}%`
  if (key === 'tile.collectorNo') return `#${opts?.number ?? ''}`
  if (key === 'addCard.toast.added') return `Added ${opts?.name ?? ''}`
  if (key === 'addCard.toast.alreadyOwned') return `${opts?.name ?? ''} already owned`
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
    render(<CardScanner onClose={onClose} />)

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
    render(<CardScanner onClose={onClose} />)

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
    render(<CardScanner onClose={vi.fn()} />)

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
    render(<CardScanner onClose={onClose} />)

    await waitFor(() => expect(screen.getByTestId('card-scanner-close')).toBeInTheDocument())
    fireEvent.click(screen.getByTestId('card-scanner-close'))

    expect(trackHandle.stop).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('applies torch constraint when the torch button is toggled', async () => {
    render(<CardScanner onClose={vi.fn()} />)
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
    render(<CardScanner onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('card-scanner-shutter')).toBeInTheDocument())
    expect(screen.queryByTestId('card-scanner-torch')).not.toBeInTheDocument()
  })
})

// helper builder for the scan response payload
function singleMatchPayload(): unknown {
  return {
    matched: true,
    confidence: 0.94,
    candidates: [
      {
        score: 0.94,
        set: { id: 'sv1', name: 'Scarlet & Violet Base' },
        card: {
          id: 'sv1-25',
          set_id: 'sv1',
          set_name: 'Scarlet & Violet Base',
          name: 'Pikachu',
          collector_no: '025/195',
          rarity: 'Common',
          image_small_url: 'https://example.com/small.png',
          image_large_url: 'https://example.com/large.png',
          variants: [
            {
              id: 11,
              kind: 'normal',
              price_eur: 1,
              price_nok: 12,
              owned: false,
              owned_id: null,
              quantity: 0,
              condition: '',
              notes: '',
            },
          ],
        },
      },
    ],
  }
}

function multiMatchPayload(): unknown {
  const single = singleMatchPayload() as { candidates: unknown[] }
  return {
    matched: true,
    confidence: 0.71,
    candidates: [
      ...single.candidates,
      {
        score: 0.65,
        set: { id: 'swsh1', name: 'Sword & Shield Base' },
        card: {
          id: 'swsh1-25',
          set_id: 'swsh1',
          set_name: 'Sword & Shield Base',
          name: 'Pikachu V',
          collector_no: '025/202',
          rarity: 'Rare',
          image_small_url: 'https://example.com/swsh-small.png',
          image_large_url: 'https://example.com/swsh-large.png',
          variants: [
            {
              id: 22,
              kind: 'normal',
              price_eur: 5,
              price_nok: 55,
              owned: false,
              owned_id: null,
              quantity: 0,
              condition: '',
              notes: '',
            },
          ],
        },
      },
    ],
  }
}

function unmatchedPayload(): unknown {
  return {
    matched: false,
    confidence: 0.22,
    reason: 'low confidence',
    set_name: 'Scarlet & Violet Base',
    collector_number: '025/195',
  }
}

describe('CardScanner — auto-detect → POST → result', () => {
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

  function lockAndCapture(payload: unknown) {
    const fakeDetection: DetectedRectangle = { x: 100, y: 80, w: 200, h: 280, score: 0.9 }
    vi.mocked(detectCardRectangle).mockReturnValue(fakeDetection)
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

    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/pokemon/scan') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () => Promise.resolve(payload),
        } as Response)
      }
      if (url === '/api/pokemon/collection') {
        return Promise.resolve({
          ok: true,
          status: 201,
          json: () => Promise.resolve({ item: { id: 42 } }),
        } as Response)
      }
      return Promise.resolve({ ok: false, status: 404, json: () => Promise.resolve({}) } as Response)
    })
    vi.stubGlobal('fetch', fetchMock)

    return { fetchMock, fakeBlob }
  }

  it('POSTs only after lock, then renders single-candidate result modal', async () => {
    const { fetchMock } = lockAndCapture(singleMatchPayload())
    render(<CardScanner onClose={vi.fn()} />)

    await primeVideoAndLoop()

    // Tick 1 (ts=0): throttle skip — no POST yet (state is still searching).
    await act(async () => { pendingRafs.shift()?.(0) })
    expect(fetchMock).not.toHaveBeenCalled()

    // Tick 2 (ts=600): first matching detection → candidate.
    await act(async () => { pendingRafs.shift()?.(600) })
    expect(fetchMock).not.toHaveBeenCalled()

    // Tick 3 (ts=1200): second matching detection → locked → captureLocked
    // → performScan → POST.
    await act(async () => { pendingRafs.shift()?.(1200) })

    await waitFor(() => {
      const scanCalls = fetchMock.mock.calls.filter(([url]) => url === '/api/pokemon/scan')
      expect(scanCalls.length).toBe(1)
    })

    // Spinner shows while POST is in flight, then result modal renders.
    await screen.findByTestId('scan-result-modal')
    expect(screen.getByText('Pikachu')).toBeInTheDocument()
    expect(screen.getByTestId('scan-result-add')).toBeInTheDocument()
    expect(screen.getByTestId('scan-result-try-again')).toBeInTheDocument()
  })

  it('Add → POST /collection, success toast, and 2s cooldown gates auto rescans', async () => {
    const { fetchMock } = lockAndCapture(singleMatchPayload())
    const onAdded = vi.fn()
    render(<CardScanner onClose={vi.fn()} onAdded={onAdded} />)

    await primeVideoAndLoop()

    await act(async () => { pendingRafs.shift()?.(0) })
    await act(async () => { pendingRafs.shift()?.(600) })
    await act(async () => { pendingRafs.shift()?.(1200) })

    await screen.findByTestId('scan-result-add')

    fireEvent.click(screen.getByTestId('scan-result-add'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) => url === '/api/pokemon/collection' &&
          (init as RequestInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
    })

    expect(await screen.findByText('Added Pikachu')).toBeInTheDocument()
    expect(onAdded).toHaveBeenCalledTimes(1)
    // The 2s cooldown is timestamp-based and the test runs well under 2s
    // wall time, so the rAF gate should still suppress a second /scan POST
    // even after the detector reports a locked candidate again.
    const scanCallsBefore = fetchMock.mock.calls.filter(([u]) => u === '/api/pokemon/scan').length
    await act(async () => { pendingRafs.shift()?.(1800) })
    await act(async () => { pendingRafs.shift()?.(2400) })
    await act(async () => { pendingRafs.shift()?.(3000) })
    const scanCallsAfter = fetchMock.mock.calls.filter(([u]) => u === '/api/pokemon/scan').length
    expect(scanCallsAfter).toBe(scanCallsBefore)
  })

  it('multi-candidate response renders each card as a tappable row', async () => {
    lockAndCapture(multiMatchPayload())
    render(<CardScanner onClose={vi.fn()} />)

    await primeVideoAndLoop()
    await act(async () => { pendingRafs.shift()?.(0) })
    await act(async () => { pendingRafs.shift()?.(600) })
    await act(async () => { pendingRafs.shift()?.(1200) })

    await screen.findByTestId('scan-result-modal')
    expect(screen.getByTestId('scan-result-candidate-sv1-25')).toBeInTheDocument()
    expect(screen.getByTestId('scan-result-candidate-swsh1-25')).toBeInTheDocument()
  })

  it('unmatched response shows confidence and reason; Enter manually fires prefill', async () => {
    lockAndCapture(unmatchedPayload())
    const onEnterManually = vi.fn()
    render(<CardScanner onClose={vi.fn()} onEnterManually={onEnterManually} />)

    await primeVideoAndLoop()
    await act(async () => { pendingRafs.shift()?.(0) })
    await act(async () => { pendingRafs.shift()?.(600) })
    await act(async () => { pendingRafs.shift()?.(1200) })

    await screen.findByTestId('scan-result-modal')
    expect(screen.getByText('Confidence: 22%')).toBeInTheDocument()
    expect(screen.getByText('low confidence')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('scan-result-enter-manually'))
    expect(onEnterManually).toHaveBeenCalledWith({
      setName: 'Scarlet & Violet Base',
      collectorNumber: '025/195',
    })
  })

  it('Try again on the result modal resumes scanning', async () => {
    lockAndCapture(singleMatchPayload())
    render(<CardScanner onClose={vi.fn()} />)

    await primeVideoAndLoop()
    await act(async () => { pendingRafs.shift()?.(0) })
    await act(async () => { pendingRafs.shift()?.(600) })
    await act(async () => { pendingRafs.shift()?.(1200) })

    await screen.findByTestId('scan-result-try-again')
    fireEvent.click(screen.getByTestId('scan-result-try-again'))

    await waitFor(() => {
      expect(screen.queryByTestId('scan-result-modal')).not.toBeInTheDocument()
    })
  })

  it('manual capture button bypasses the lock and triggers a POST', async () => {
    const { fetchMock } = lockAndCapture(singleMatchPayload())
    // Override the detector to NEVER lock — but the manual shutter should
    // still produce a scan POST.
    vi.mocked(detectCardRectangle).mockReturnValue(null)
    vi.mocked(isWithinTolerance).mockReturnValue(false)
    render(<CardScanner onClose={vi.fn()} />)

    await primeVideoAndLoop()

    fireEvent.click(screen.getByTestId('card-scanner-shutter'))

    await waitFor(() => {
      const scanCalls = fetchMock.mock.calls.filter(([url]) => url === '/api/pokemon/scan')
      expect(scanCalls.length).toBe(1)
    })
    await screen.findByTestId('scan-result-modal')
  })
})
