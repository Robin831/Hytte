// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import PageScanner from './PageScanner'
import { detectGrid, cropCellsToCanvases } from './rectangleDetector'

vi.mock('./rectangleDetector', () => ({
  detectGrid: vi.fn().mockReturnValue([]),
  cropCellsToCanvases: vi.fn().mockReturnValue([]),
  TARGET_ASPECT_RATIO: 0.716,
}))

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

const TRANSLATIONS: Record<string, string> = {
  'pageScanner.dialogLabel': 'Scan a binder page',
  'pageScanner.shutter': 'Capture binder page',
  'pageScanner.uploading': 'Uploading…',
  'pageScanner.scanBinder': 'Scan binder page',
  'pageScanner.errors.noFrame': 'Camera frame not ready, try again',
  'pageScanner.errors.noGrid': "Couldn't find the cards",
  'pageScanner.errors.captureFailed': 'Failed to capture the page, try again',
  'pageScanner.errors.uploadFailed': 'Upload failed, try again',
  'pageScanner.errors.timedOut': 'Upload timed out, try again',
  'scanner.requesting': 'Requesting camera access…',
  'scanner.permissionDenied': 'Camera access was denied.',
  'scanner.cameraUnavailable': 'Camera is unavailable.',
  'scanner.unsupported': "Camera scanning isn't supported.",
  'scanner.enterManually': 'Enter card manually',
  'scanner.torchOn': 'Turn flashlight on',
  'scanner.torchOff': 'Turn flashlight off',
  'scanner.close': 'Close scanner',
  'scanner.dailyLimit': 'Daily scan limit reached',
}

function mockT(key: string): string {
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

let savedMediaDevicesDescriptor: PropertyDescriptor | undefined

beforeEach(() => {
  savedMediaDevicesDescriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices')
  mockNavigate.mockReset()
  vi.mocked(detectGrid).mockReturnValue([])
  vi.mocked(cropCellsToCanvases).mockReturnValue([])
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  if (savedMediaDevicesDescriptor !== undefined) {
    Object.defineProperty(navigator, 'mediaDevices', savedMediaDevicesDescriptor)
  } else {
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

describe('PageScanner — granted', () => {
  let trackHandle: ReturnType<typeof makeTrack>
  let stream: MediaStream
  let getUserMediaMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    trackHandle = makeTrack({ torch: false })
    stream = makeStream(trackHandle.track)
    getUserMediaMock = vi.fn().mockResolvedValue(stream)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: getUserMediaMock },
    })
  })

  it('requests 4K video so the per-cell crops have enough pixels for Claude vision', async () => {
    renderScanner(<PageScanner onClose={vi.fn()} />)

    await waitFor(() => expect(getUserMediaMock).toHaveBeenCalledTimes(1))
    const constraints = getUserMediaMock.mock.calls[0][0] as {
      video?: {
        facingMode?: { ideal?: string }
        width?: { ideal?: number }
        height?: { ideal?: number }
      }
    }
    expect(constraints.video?.width?.ideal).toBe(3840)
    expect(constraints.video?.height?.ideal).toBe(2160)
    expect(constraints.video?.facingMode?.ideal).toBe('environment')
  })

  it('renders the manual shutter, the grid framing guide, and the close button', async () => {
    renderScanner(<PageScanner onClose={vi.fn()} />)

    await waitFor(() => {
      expect(screen.getByTestId('page-scanner-video')).toBeInTheDocument()
    })
    expect(screen.getByTestId('page-scanner-shutter')).toBeInTheDocument()
    expect(screen.getByTestId('page-scanner-grid-guide')).toBeInTheDocument()
    expect(screen.getByTestId('page-scanner-close')).toBeInTheDocument()
  })

  it('renders 2 column dividers and 2 row dividers for the default 3x3 layout', async () => {
    renderScanner(<PageScanner onClose={vi.fn()} />)

    await waitFor(() => {
      expect(screen.getByTestId('page-scanner-grid-guide')).toBeInTheDocument()
    })
    expect(screen.getAllByTestId('page-scanner-grid-col-line')).toHaveLength(2)
    expect(screen.getAllByTestId('page-scanner-grid-row-line')).toHaveLength(2)
  })

  it('renders 3 column dividers and 2 row dividers for a 3x4 page when rows/cols are overridden', async () => {
    renderScanner(<PageScanner onClose={vi.fn()} rows={3} cols={4} />)

    await waitFor(() => {
      expect(screen.getByTestId('page-scanner-grid-guide')).toBeInTheDocument()
    })
    expect(screen.getAllByTestId('page-scanner-grid-col-line')).toHaveLength(3)
    expect(screen.getAllByTestId('page-scanner-grid-row-line')).toHaveLength(2)
  })

  it('does not start a requestAnimationFrame loop (no auto-capture)', async () => {
    const rafSpy = vi.fn().mockReturnValue(1)
    vi.stubGlobal('requestAnimationFrame', rafSpy)
    renderScanner(<PageScanner onClose={vi.fn()} />)

    await waitFor(() => {
      expect(screen.getByTestId('page-scanner-shutter')).toBeInTheDocument()
    })
    expect(rafSpy).not.toHaveBeenCalled()
  })

  it('closes when the close button is clicked and stops camera tracks', async () => {
    const onClose = vi.fn()
    renderScanner(<PageScanner onClose={onClose} />)

    await waitFor(() => expect(screen.getByTestId('page-scanner-close')).toBeInTheDocument())
    fireEvent.click(screen.getByTestId('page-scanner-close'))

    expect(trackHandle.stop).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})

describe('PageScanner — shutter capture flow', () => {
  beforeEach(() => {
    const trackHandle = makeTrack({ torch: false })
    const stream = makeStream(trackHandle.track)
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
  })

  async function primeVideo() {
    await waitFor(() => expect(screen.getByTestId('page-scanner-video')).toBeInTheDocument())
    const video = screen.getByTestId('page-scanner-video') as HTMLVideoElement
    Object.defineProperty(video, 'videoWidth', { value: 1280, configurable: true })
    Object.defineProperty(video, 'videoHeight', { value: 960, configurable: true })
    Object.defineProperty(video, 'readyState', { value: 4, configurable: true })

    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
      drawImage: vi.fn(),
      getImageData: vi.fn().mockReturnValue({
        data: new Uint8ClampedArray(1280 * 960 * 4),
        width: 1280,
        height: 960,
        colorSpace: 'srgb',
      }),
    } as unknown as CanvasRenderingContext2D)
    vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (cb) {
      cb(new Blob(['crop'], { type: 'image/jpeg' }))
    })
    return video
  }

  it('shows an error toast and skips the POST when detectGrid finds nothing', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    vi.mocked(detectGrid).mockReturnValue([])

    renderScanner(<PageScanner onClose={vi.fn()} />)
    await primeVideo()

    fireEvent.click(screen.getByTestId('page-scanner-shutter'))

    await screen.findByText("Couldn't find the cards")
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('POSTs N crops + cells JSON to /api/pokemon/scans/page and navigates to the scanned page', async () => {
    const cells = [
      { row: 0, col: 0, x: 0, y: 0, w: 100, h: 140 },
      { row: 0, col: 1, x: 100, y: 0, w: 100, h: 140 },
    ]
    vi.mocked(detectGrid).mockReturnValue(cells)
    const canvases = cells.map(() => {
      const c = document.createElement('canvas')
      return c
    })
    vi.mocked(cropCellsToCanvases).mockReturnValue(canvases)

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      headers: new Headers(),
      json: () => Promise.resolve({ page_id: 42, job_ids: [1, 2], count: 2 }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    const onClose = vi.fn()
    renderScanner(<PageScanner onClose={onClose} />)
    await primeVideo()

    fireEvent.click(screen.getByTestId('page-scanner-shutter'))

    await waitFor(() => {
      const pageCalls = fetchMock.mock.calls.filter(([url]) => url === '/api/pokemon/scans/page')
      expect(pageCalls.length).toBe(1)
    })

    const [, init] = fetchMock.mock.calls[0]
    const body = (init as RequestInit).body as FormData
    expect(body.getAll('images').length).toBe(2)
    const cellsRaw = body.get('cells')
    expect(typeof cellsRaw).toBe('string')
    expect(JSON.parse(cellsRaw as string)).toEqual([
      { row: 0, col: 0 },
      { row: 0, col: 1 },
    ])

    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/pokemon/scanned?page=42'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('429 daily-limit response renders the inline error and disables the shutter', async () => {
    const cells = [{ row: 0, col: 0, x: 0, y: 0, w: 100, h: 140 }]
    vi.mocked(detectGrid).mockReturnValue(cells)
    vi.mocked(cropCellsToCanvases).mockReturnValue([document.createElement('canvas')])

    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 429,
      headers: new Headers(),
      json: () => Promise.resolve({ error: 'daily scan cap reached' }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    renderScanner(<PageScanner onClose={vi.fn()} />)
    await primeVideo()

    fireEvent.click(screen.getByTestId('page-scanner-shutter'))

    await screen.findByTestId('page-scanner-daily-limit')
    expect(screen.getByText('Daily scan limit reached')).toBeInTheDocument()
    expect(screen.getByTestId('page-scanner-shutter')).toBeDisabled()
    expect(mockNavigate).not.toHaveBeenCalled()
  })
})

describe('PageScanner — unsupported browser', () => {
  beforeEach(() => {
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: undefined,
    })
  })

  it('shows the unsupported fallback and a manual-entry button that calls onClose', async () => {
    const onClose = vi.fn()
    renderScanner(<PageScanner onClose={onClose} />)

    expect(
      await screen.findByText("Camera scanning isn't supported."),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('page-scanner-manual-entry'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
