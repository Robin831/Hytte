// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import CardScanner from './CardScanner'

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
  if (savedMediaDevicesDescriptor) {
    Object.defineProperty(navigator, 'mediaDevices', savedMediaDevicesDescriptor)
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
