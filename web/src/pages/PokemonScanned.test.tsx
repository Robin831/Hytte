// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import PokemonScanned from './PokemonScanned'

// LocationProbe mirrors the current location's state into a data attribute so
// the "Enter manually" test can assert that PokemonScanned navigates to
// /pokemon with the expected pre-fill query.
function LocationProbe() {
  const loc = useLocation()
  const state = (loc.state as { addCardQuery?: string } | null) ?? null
  return (
    <div
      data-testid="location-probe"
      data-pathname={loc.pathname}
      data-addcard-query={state?.addCardQuery ?? ''}
    />
  )
}

// Lightweight i18n mock so assertions can read predictable English strings
// without pulling in the real HttpBackend.
const TRANSLATIONS: Record<string, string> = {
  retry: 'Retry',
  'detail.back': 'Back to sets',
  'scanned.title': 'Scanned cards',
  'scanned.subtitle': 'Review your recent scans',
  'scanned.empty': 'No scans yet — open the camera scanner from Add card.',
  'scanned.loadError': 'Failed to load scans',
  'scanned.noPriceYet': 'Price unavailable',
  'scanned.thumbnailAlt': 'Scanned card preview',
  'scanned.thumbnailPlaceholder': 'Image no longer available',
  'scanned.filter.label': 'Filter scans',
  'scanned.filter.needsReview': 'Needs review',
  'scanned.filter.pending': 'Pending',
  'scanned.filter.resolved': 'Recently resolved',
  'scanned.status.queued': 'Queued',
  'scanned.status.processing': 'Processing',
  'scanned.status.matched': 'Matched',
  'scanned.status.noMatch': 'No match',
  'scanned.status.failed': 'Failed',
  'scanned.status.added': 'Added',
  'scanned.status.discarded': 'Discarded',
  'scanned.action.add': 'Add to collection',
  'scanned.action.discard': 'Discard',
  'scanned.action.retry': 'Try again',
  'scanned.action.enterManually': 'Enter manually',
  'scanned.action.pickVariant': 'Pick a variant',
  'scanned.action.actionFailed': 'Action failed, try again',
  'scanned.toast.added': 'Added to collection',
  'scanned.toast.discarded': 'Discarded',
  'scanned.toast.retried': 'Sent back to the scanner queue',
  'scanner.errors.scanFailed': 'Scan failed, try again',
  'variantKind.normal': 'Normal',
  'variantKind.reverse_holofoil': 'Reverse holo',
  'variantKind.holofoil': 'Holo',
}

function mockT(key: string, opts?: Record<string, string | number> & { defaultValue?: string }): string {
  if (key === 'scanned.todayUsage') return `${opts?.used ?? 0} / ${opts?.cap ?? 0} scans today`
  if (key === 'scanned.confidence') return `${opts?.pct ?? 0}% confidence`
  if (key === 'scanned.elapsed') return `${opts?.seconds ?? 0} s`
  if (key === 'tile.collectorNo') return `#${opts?.number ?? ''}`
  if (key.startsWith('variantKind.')) return TRANSLATIONS[key] ?? opts?.defaultValue ?? key
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../i18n', () => ({
  default: { language: 'en' },
}))

interface FetchInit {
  method?: string
  body?: BodyInit | null
}

interface JsonResp {
  ok: boolean
  status: number
  json: () => Promise<unknown>
}

function jsonResponse(body: unknown, status = 200): JsonResp {
  return {
    ok: status < 400,
    status,
    json: () => Promise.resolve(body),
  }
}

interface ScanFixture {
  id: number
  status: string
  created_at: string
  processed_at?: string | null
  resolved_at?: string | null
  confidence?: number | null
  matched_card?: {
    id: string
    set_id: string
    set_name?: string
    name: string
    collector_no: string
    rarity: string
    image_small_url: string
    image_large_url: string
    variants: Array<{
      id: number
      kind: string
      price_eur: number
      price_nok: number | null
    }>
  } | null
  set?: { id: string; name: string } | null
  error_message?: string
  has_image: boolean
  parsed_set_name?: string
  parsed_collector_no?: string
}

function makeMatched(over: Partial<ScanFixture> = {}): ScanFixture {
  return {
    id: 1,
    status: 'matched',
    created_at: new Date('2026-05-15T10:00:00Z').toISOString(),
    processed_at: new Date('2026-05-15T10:00:05Z').toISOString(),
    confidence: 0.92,
    matched_card: {
      id: 'sv1-1',
      set_id: 'sv1',
      set_name: 'Scarlet & Violet Base',
      name: 'Pikachu',
      collector_no: '001',
      rarity: 'Common',
      image_small_url: 'https://example.com/s.png',
      image_large_url: 'https://example.com/l.png',
      variants: [{ id: 11, kind: 'normal', price_eur: 10, price_nok: 100 }],
    },
    set: { id: 'sv1', name: 'Scarlet & Violet Base' },
    has_image: true,
    ...over,
  }
}

function makeNoMatch(over: Partial<ScanFixture> = {}): ScanFixture {
  return {
    id: 2,
    status: 'no_match',
    created_at: new Date('2026-05-15T10:01:00Z').toISOString(),
    processed_at: new Date('2026-05-15T10:01:05Z').toISOString(),
    confidence: 0.3,
    error_message: 'Could not read collector number',
    has_image: true,
    ...over,
  }
}

function makeFailed(over: Partial<ScanFixture> = {}): ScanFixture {
  return {
    id: 3,
    status: 'failed',
    created_at: new Date('2026-05-15T10:02:00Z').toISOString(),
    processed_at: new Date('2026-05-15T10:02:05Z').toISOString(),
    error_message: 'Claude vision unavailable',
    has_image: true,
    ...over,
  }
}

function makeQueued(over: Partial<ScanFixture> = {}): ScanFixture {
  return {
    id: 4,
    status: 'queued',
    created_at: new Date('2026-05-15T10:03:00Z').toISOString(),
    has_image: true,
    ...over,
  }
}

interface FetchSpec {
  needsReview: ScanFixture[]
  pending?: ScanFixture[]
  resolved?: ScanFixture[]
  today?: { used: number; cap: number }
}

function makeFetchMock(spec: FetchSpec, overrides?: (url: string, init?: FetchInit) => JsonResp | null) {
  return vi.fn((url: string, init?: FetchInit) => {
    if (overrides) {
      const o = overrides(url, init)
      if (o) return Promise.resolve(o)
    }
    if (url.startsWith('/api/pokemon/scans?')) {
      let scans: ScanFixture[] = []
      if (url.includes('matched') && url.includes('no_match')) scans = spec.needsReview
      else if (url.includes('queued')) scans = spec.pending ?? []
      else if (url.includes('added')) scans = spec.resolved ?? []
      return Promise.resolve(
        jsonResponse({
          scans,
          today: spec.today ?? { used: 12, cap: 600 },
        }),
      )
    }
    if (url.match(/^\/api\/pokemon\/scans\/\d+\/resolve$/) && init?.method === 'POST') {
      return Promise.resolve(jsonResponse({ scan: {} }, 200))
    }
    return Promise.resolve(jsonResponse({}, 404))
  })
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/pokemon/scanned']}>
      <Routes>
        <Route path="/pokemon/scanned" element={<PokemonScanned />} />
        <Route path="/pokemon" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('PokemonScanned – render', () => {
  it('shows the header, usage badge, and a row per scan with the correct status pill', async () => {
    const fetchMock = makeFetchMock({
      needsReview: [makeMatched(), makeNoMatch(), makeFailed()],
      today: { used: 12, cap: 600 },
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByTestId('scanned-list')).toBeInTheDocument())

    expect(screen.getByRole('heading', { name: 'Scanned cards' })).toBeInTheDocument()
    expect(screen.getByText('Review your recent scans')).toBeInTheDocument()
    expect(screen.getByTestId('scanned-today-usage')).toHaveTextContent('12 / 600 scans today')

    // Three rows, each with its expected pill.
    expect(screen.getByTestId('scan-row-1')).toHaveAttribute('data-status', 'matched')
    expect(screen.getByTestId('scan-row-2')).toHaveAttribute('data-status', 'no_match')
    expect(screen.getByTestId('scan-row-3')).toHaveAttribute('data-status', 'failed')
    expect(screen.getByTestId('scan-status-pill-matched')).toHaveTextContent('Matched')
    expect(screen.getByTestId('scan-status-pill-no_match')).toHaveTextContent('No match')
    expect(screen.getByTestId('scan-status-pill-failed')).toHaveTextContent('Failed')

    // Matched row shows card name, set, collector number, confidence and price.
    const matchedRow = screen.getByTestId('scan-row-1')
    expect(within(matchedRow).getByText('Pikachu')).toBeInTheDocument()
    expect(within(matchedRow).getByText(/92% confidence/)).toBeInTheDocument()
  })

  it('renders the empty state when no scans come back', async () => {
    vi.stubGlobal('fetch', makeFetchMock({ needsReview: [] }))
    renderPage()
    await waitFor(() => expect(screen.getByTestId('scanned-empty')).toBeInTheDocument())
    expect(screen.getByTestId('scanned-empty')).toHaveTextContent(
      'No scans yet — open the camera scanner from Add card.',
    )
  })

  it('renders the error state when the list endpoint fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse({ error: 'boom' }, 500))),
    )
    renderPage()
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.getByRole('alert')).toHaveTextContent('Failed to load scans')
  })

  it('shows the loading skeleton before the first fetch resolves', async () => {
    // Hold the response so the loading branch is observable.
    let resolveFetch: (value: JsonResp) => void = () => {}
    vi.stubGlobal(
      'fetch',
      vi.fn(
        () =>
          new Promise<JsonResp>(resolve => {
            resolveFetch = resolve
          }),
      ),
    )
    renderPage()
    expect(screen.getAllByTestId('scanned-skeleton').length).toBeGreaterThan(0)
    // Resolve so the test can finish cleanly.
    resolveFetch(jsonResponse({ scans: [], today: { used: 0, cap: 600 } }))
    await waitFor(() => expect(screen.queryByTestId('scanned-skeleton')).not.toBeInTheDocument())
  })
})

describe('PokemonScanned – filter chips', () => {
  it('switching to Pending requests queued+processing and renders queued rows', async () => {
    const fetchMock = makeFetchMock({
      needsReview: [makeMatched()],
      pending: [makeQueued()],
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-1')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scanned-filter-pending'))

    await waitFor(() => expect(screen.getByTestId('scan-row-4')).toBeInTheDocument())
    expect(screen.queryByTestId('scan-row-1')).not.toBeInTheDocument()

    const pendingCall = fetchMock.mock.calls
      .map(c => c[0] as string)
      .find(url => url.startsWith('/api/pokemon/scans?') && url.includes('queued'))
    expect(pendingCall).toBeTruthy()
    expect(pendingCall).toContain('processing')
  })
})

describe('PokemonScanned – resolve actions', () => {
  it('Add posts action=add with variant_id and refetches the list', async () => {
    const fetchMock = makeFetchMock({ needsReview: [makeMatched()] })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-1')).toBeInTheDocument())

    const initialListCalls = fetchMock.mock.calls.filter(
      ([url]) => typeof url === 'string' && (url as string).startsWith('/api/pokemon/scans?'),
    ).length

    fireEvent.click(screen.getByTestId('scan-action-add-1'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/1/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ action: 'add', variant_id: 11, quantity: 1 })
    })

    // Refetched after resolve.
    await waitFor(() => {
      const afterCalls = fetchMock.mock.calls.filter(
        ([url]) => typeof url === 'string' && (url as string).startsWith('/api/pokemon/scans?'),
      ).length
      expect(afterCalls).toBeGreaterThan(initialListCalls)
    })
  })

  it('Discard posts action=discard', async () => {
    const fetchMock = makeFetchMock({ needsReview: [makeNoMatch()] })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-2')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-action-discard-2'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/2/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ action: 'discard' })
    })
  })

  it('Retry posts action=retry on a failed row', async () => {
    const fetchMock = makeFetchMock({ needsReview: [makeFailed()] })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-3')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-action-retry-3'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/3/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ action: 'retry' })
    })
  })

  it('Enter manually navigates to /pokemon with the parsed hints as addCardQuery', async () => {
    const noMatch = makeNoMatch({
      parsed_set_name: 'Scarlet & Violet Base',
      parsed_collector_no: '055',
    })
    const fetchMock = makeFetchMock({ needsReview: [noMatch] })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-2')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-action-manual-2'))

    await waitFor(() => expect(screen.getByTestId('location-probe')).toBeInTheDocument())
    const probe = screen.getByTestId('location-probe')
    expect(probe).toHaveAttribute('data-pathname', '/pokemon')
    expect(probe).toHaveAttribute('data-addcard-query', 'Scarlet & Violet Base 055')
  })

  it('Enter manually still navigates with an empty query when Claude could not read any hint', async () => {
    const noMatch = makeNoMatch()
    const fetchMock = makeFetchMock({ needsReview: [noMatch] })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-2')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-action-manual-2'))

    await waitFor(() => expect(screen.getByTestId('location-probe')).toBeInTheDocument())
    const probe = screen.getByTestId('location-probe')
    expect(probe).toHaveAttribute('data-pathname', '/pokemon')
    expect(probe).toHaveAttribute('data-addcard-query', '')
  })

  it('shows the variant picker on a matched row with multiple variants', async () => {
    const matched = makeMatched({
      matched_card: {
        id: 'sv1-1',
        set_id: 'sv1',
        set_name: 'Scarlet & Violet Base',
        name: 'Pikachu',
        collector_no: '001',
        rarity: 'Common',
        image_small_url: '',
        image_large_url: '',
        variants: [
          { id: 11, kind: 'normal', price_eur: 10, price_nok: 100 },
          { id: 12, kind: 'reverse_holofoil', price_eur: 15, price_nok: 150 },
        ],
      },
    })
    const fetchMock = makeFetchMock({ needsReview: [matched] })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-1')).toBeInTheDocument())

    // First click opens the picker; the POST should not have fired yet.
    fireEvent.click(screen.getByTestId('scan-action-add-1'))
    expect(screen.getByTestId('scan-variant-picker-1')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('scan-variant-1-12'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/1/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ action: 'add', variant_id: 12 })
    })
  })
})
