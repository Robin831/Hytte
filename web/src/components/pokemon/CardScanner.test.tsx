// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
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
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => TRANSLATIONS[key] ?? key,
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
    const onCapture = vi.fn()
    render(<CardScanner onCapture={onCapture} onClose={onClose} />)

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
    render(<CardScanner onCapture={vi.fn()} onClose={onClose} />)

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
    render(<CardScanner onCapture={vi.fn()} onClose={vi.fn()} />)

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
    render(<CardScanner onCapture={vi.fn()} onClose={onClose} />)

    await waitFor(() => expect(screen.getByTestId('card-scanner-close')).toBeInTheDocument())
    fireEvent.click(screen.getByTestId('card-scanner-close'))

    expect(trackHandle.stop).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('applies torch constraint when the torch button is toggled', async () => {
    render(<CardScanner onCapture={vi.fn()} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByTestId('card-scanner-torch')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('card-scanner-torch'))

    await waitFor(() => {
      expect(trackHandle.applyConstraints).toHaveBeenCalledWith({
        advanced: [{ torch: true }],
      })
    })
  })

  it('calls onCapture with a Blob when the shutter is clicked and video has dimensions', async () => {
    const onCapture = vi.fn()
    render(<CardScanner onCapture={onCapture} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('card-scanner-shutter')).toBeInTheDocument())

    const video = screen.getByTestId('card-scanner-video') as HTMLVideoElement
    Object.defineProperty(video, 'videoWidth', { value: 640, configurable: true })
    Object.defineProperty(video, 'videoHeight', { value: 480, configurable: true })

    const mockCtx = { drawImage: vi.fn() }
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(
      mockCtx as unknown as CanvasRenderingContext2D,
    )

    const fakeBlob = new Blob(['img'], { type: 'image/jpeg' })
    vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (cb) {
      cb(fakeBlob)
    })

    fireEvent.click(screen.getByTestId('card-scanner-shutter'))

    await waitFor(() => {
      expect(onCapture).toHaveBeenCalledOnce()
      expect(onCapture).toHaveBeenCalledWith(fakeBlob)
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
    render(<CardScanner onCapture={vi.fn()} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('card-scanner-shutter')).toBeInTheDocument())
    expect(screen.queryByTestId('card-scanner-torch')).not.toBeInTheDocument()
  })
})

describe('CardScanner — auto-detect state machine', () => {
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

  it('drives searching → candidate → locked, shows holdSteady banner, and fires onCapture', async () => {
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

    const onCapture = vi.fn()
    render(<CardScanner onCapture={onCapture} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('card-scanner-video')).toBeInTheDocument())

    const video = screen.getByTestId('card-scanner-video') as HTMLVideoElement
    Object.defineProperty(video, 'videoWidth', { value: 640, configurable: true })
    Object.defineProperty(video, 'videoHeight', { value: 480, configurable: true })
    Object.defineProperty(video, 'readyState', { value: 4, configurable: true })

    // Tick 1 (ts=0): throttle fires (0 - 0 < DETECT_TICK_MS=500), schedules next rAF
    pendingRafs.shift()?.(0)

    // Tick 2 (ts=600): first detection → candidate state, holdSteady banner appears
    pendingRafs.shift()?.(600)
    await waitFor(() => {
      expect(screen.getByTestId('card-scanner-status')).toBeInTheDocument()
      expect(screen.getByTestId('card-scanner-status').textContent).toBe('Hold steady…')
    })

    // Tick 3 (ts=1200): second matching detection → locked → captured, onCapture fires
    pendingRafs.shift()?.(1200)
    await waitFor(() => {
      expect(onCapture).toHaveBeenCalledOnce()
      expect(onCapture).toHaveBeenCalledWith(fakeBlob)
    })
  })

  it('reverts to searching when toBlob returns null and does not freeze the scanner', async () => {
    const fakeDetection: DetectedRectangle = { x: 100, y: 80, w: 200, h: 280, score: 0.9 }
    vi.mocked(detectCardRectangle).mockReturnValue(fakeDetection)
    vi.mocked(isWithinTolerance).mockReturnValue(true)

    // toBlob returns null — simulates an environment that can't encode
    vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (cb) {
      cb(null)
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

    const onCapture = vi.fn()
    render(<CardScanner onCapture={onCapture} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('card-scanner-video')).toBeInTheDocument())

    const video = screen.getByTestId('card-scanner-video') as HTMLVideoElement
    Object.defineProperty(video, 'videoWidth', { value: 640, configurable: true })
    Object.defineProperty(video, 'videoHeight', { value: 480, configurable: true })
    Object.defineProperty(video, 'readyState', { value: 4, configurable: true })

    // Advance through throttle tick and two detection ticks to trigger captureLocked
    pendingRafs.shift()?.(0)
    pendingRafs.shift()?.(600)
    pendingRafs.shift()?.(1200)

    // onCapture must NOT have been called (blob was null)
    await waitFor(() => expect(onCapture).not.toHaveBeenCalled())

    // Scanner must have recovered: status element gone (back to searching) and
    // the rAF loop restarted (a new callback is queued)
    await waitFor(() => {
      expect(screen.queryByTestId('card-scanner-status')).not.toBeInTheDocument()
      expect(pendingRafs.length).toBeGreaterThan(0)
    })
  })
})
