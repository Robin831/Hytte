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
  'scanned.tapToReview': 'Tap to review',
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
  'scanned.page.label': 'Binder page',
  'scanned.page.discardPage': 'Discard page',
  'scanned.page.confirmDiscard': 'Discard this whole page?',
  'scanned.page.cancel': 'Cancel',
  'scanned.page.discardConfirm': 'Yes, discard',
  'scanned.page.discardToast': 'Page discarded',
  'scanned.page.discardFailed': 'Failed to discard page',
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
  if (key === 'scanned.page.progress') return `${opts?.matched ?? 0} / ${opts?.total ?? 0} matched`
  if (key === 'scanned.page.addAllMatched') return `Add all matched (${opts?.count ?? 0})`
  if (key === 'scanned.page.addAllToast') return `Added ${opts?.count ?? 0} cards`
  if (key === 'scanned.page.addAllPartial') return `${opts?.count ?? 0} cards could not be added`
  if (key === 'scanned.detail.openLabel') return `Open scan detail for ${opts?.name ?? ''}`
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

interface PageFixture {
  id: number
  expected_count: number
  matched_count: number
  created_at: string
  children: ScanFixture[]
}

interface FetchSpec {
  needsReview: ScanFixture[]
  pending?: ScanFixture[]
  resolved?: ScanFixture[]
  today?: { used: number; cap: number }
  pages?: {
    needsReview?: PageFixture[]
    pending?: PageFixture[]
    resolved?: PageFixture[]
  }
}

function makeFetchMock(spec: FetchSpec, overrides?: (url: string, init?: FetchInit) => JsonResp | null) {
  return vi.fn((url: string, init?: FetchInit) => {
    if (overrides) {
      const o = overrides(url, init)
      if (o) return Promise.resolve(o)
    }
    if (url.startsWith('/api/pokemon/scans?')) {
      let scans: ScanFixture[] = []
      let pages: PageFixture[] = []
      if (url.includes('matched') && url.includes('no_match')) {
        scans = spec.needsReview
        pages = spec.pages?.needsReview ?? []
      } else if (url.includes('queued')) {
        scans = spec.pending ?? []
        pages = spec.pages?.pending ?? []
      } else if (url.includes('added')) {
        scans = spec.resolved ?? []
        pages = spec.pages?.resolved ?? []
      }
      return Promise.resolve(
        jsonResponse({
          scans,
          pages,
          today: spec.today ?? { used: 12, cap: 600 },
        }),
      )
    }
    if (url.match(/^\/api\/pokemon\/scans\/\d+\/resolve$/) && init?.method === 'POST') {
      return Promise.resolve(jsonResponse({ scan: {} }, 200))
    }
    if (url.match(/^\/api\/pokemon\/scans\/pages\/\d+$/) && init?.method === 'DELETE') {
      return Promise.resolve(jsonResponse({}, 204))
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
  it('Add via the detail modal posts action=add with variant_id (no override) and refetches the list', async () => {
    const fetchMock = makeFetchMock({ needsReview: [makeMatched()] })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-1')).toBeInTheDocument())

    const initialListCalls = fetchMock.mock.calls.filter(
      ([url]) => typeof url === 'string' && (url as string).startsWith('/api/pokemon/scans?'),
    ).length

    // Matched tile is a clickable launchpad — opening it surfaces the modal.
    fireEvent.click(screen.getByTestId('scan-open-detail-1'))
    expect(screen.getByTestId('scan-detail-modal')).toBeInTheDocument()

    // Variant picker now lives in the modal.
    fireEvent.click(screen.getByTestId('scan-detail-variant-11'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/1/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ action: 'add', variant_id: 11, quantity: 1 })
      // No override card_id when the auto-match is accepted.
      expect(body.card_id).toBeUndefined()
    })

    // Refetched after resolve.
    await waitFor(() => {
      const afterCalls = fetchMock.mock.calls.filter(
        ([url]) => typeof url === 'string' && (url as string).startsWith('/api/pokemon/scans?'),
      ).length
      expect(afterCalls).toBeGreaterThan(initialListCalls)
    })
  })

  it('Discarding from the detail modal posts action=discard', async () => {
    const fetchMock = makeFetchMock({ needsReview: [makeMatched()] })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-1')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-open-detail-1'))
    fireEvent.click(screen.getByTestId('scan-detail-discard'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/1/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ action: 'discard' })
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

  it('detail modal lists all variants for a matched card and posts the picked variant', async () => {
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

    // Open the detail modal — the variant buttons live inside it now.
    fireEvent.click(screen.getByTestId('scan-open-detail-1'))
    expect(screen.getByTestId('scan-detail-variant-11')).toBeInTheDocument()
    expect(screen.getByTestId('scan-detail-variant-12')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('scan-detail-variant-12'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/1/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ action: 'add', variant_id: 12 })
      expect(body.card_id).toBeUndefined()
    })
  })

  it('expanding "Wrong match?" lets the user pick a different card and adds via that card with card_id in the body', async () => {
    const overrideCard = {
      id: 'sv1-100',
      set_id: 'sv1',
      set_name: 'Scarlet & Violet Base',
      name: 'Eevee',
      collector_no: '100',
      rarity: 'Common',
      image_small_url: 'https://example.com/eevee.png',
      image_large_url: 'https://example.com/eevee-l.png',
      variants: [{ id: 99, kind: 'normal', price_eur: 3, price_nok: 30 }],
    }
    const fetchMock = makeFetchMock({ needsReview: [makeMatched()] }, (url) => {
      if (url.startsWith('/api/pokemon/cards/search?')) {
        return jsonResponse({ cards: [overrideCard] })
      }
      return null
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-1')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-open-detail-1'))
    fireEvent.click(screen.getByTestId('scan-detail-wrong-match-toggle'))
    expect(screen.getByTestId('scan-detail-wrong-match-panel')).toBeInTheDocument()

    // Typing triggers the debounced /cards/search call — use fake timers to
    // advance past the debounce deterministically without real-time waiting.
    vi.useFakeTimers()
    fireEvent.change(screen.getByTestId('scan-detail-search-input'), { target: { value: 'eevee' } })
    vi.advanceTimersByTime(250)
    vi.useRealTimers()

    await waitFor(() => {
      const searchCall = fetchMock.mock.calls.find(
        ([url]) => typeof url === 'string' && (url as string).startsWith('/api/pokemon/cards/search?'),
      )
      expect(searchCall).toBeTruthy()
    })

    await waitFor(() => expect(screen.getByTestId('scan-detail-pick-sv1-100')).toBeInTheDocument())
    fireEvent.click(screen.getByTestId('scan-detail-pick-sv1-100'))

    // The variant row now reflects the override card's variants.
    await waitFor(() => expect(screen.getByTestId('scan-detail-variant-99')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-detail-variant-99'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/1/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      // Override path: the POST body must carry the picked card_id alongside
      // the variant_id from that card.
      expect(body).toMatchObject({ action: 'add', variant_id: 99, card_id: 'sv1-100', quantity: 1 })
    })
  })

  it('"Use auto-match instead" reverts the override and adds without a card_id', async () => {
    const overrideCard = {
      id: 'sv1-100',
      set_id: 'sv1',
      set_name: 'Scarlet & Violet Base',
      name: 'Eevee',
      collector_no: '100',
      rarity: 'Common',
      image_small_url: 'https://example.com/eevee.png',
      image_large_url: 'https://example.com/eevee-l.png',
      variants: [{ id: 99, kind: 'normal', price_eur: 3, price_nok: 30 }],
    }
    const fetchMock = makeFetchMock({ needsReview: [makeMatched()] }, (url) => {
      if (url.startsWith('/api/pokemon/cards/search?')) {
        return jsonResponse({ cards: [overrideCard] })
      }
      return null
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-row-1')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-open-detail-1'))
    fireEvent.click(screen.getByTestId('scan-detail-wrong-match-toggle'))
    vi.useFakeTimers()
    fireEvent.change(screen.getByTestId('scan-detail-search-input'), { target: { value: 'eevee' } })
    vi.advanceTimersByTime(250)
    vi.useRealTimers()
    await waitFor(() => expect(screen.getByTestId('scan-detail-pick-sv1-100')).toBeInTheDocument())
    fireEvent.click(screen.getByTestId('scan-detail-pick-sv1-100'))
    await waitFor(() => expect(screen.getByTestId('scan-detail-override-banner')).toBeInTheDocument())

    // Revert restores the auto-match's variants.
    fireEvent.click(screen.getByTestId('scan-detail-revert'))
    expect(screen.queryByTestId('scan-detail-override-banner')).not.toBeInTheDocument()
    expect(screen.getByTestId('scan-detail-variant-11')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('scan-detail-variant-11'))
    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/1/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ action: 'add', variant_id: 11 })
      expect(body.card_id).toBeUndefined()
    })
  })
})

describe('PokemonScanned – page grid', () => {
  function makePageMatched(id: number, over: Partial<ScanFixture> = {}): ScanFixture {
    return makeMatched({ id, ...over })
  }
  function makePageQueued(id: number): ScanFixture {
    return makeQueued({ id })
  }

  function makePage(over: Partial<PageFixture> = {}): PageFixture {
    return {
      id: 42,
      expected_count: 9,
      matched_count: 1,
      created_at: new Date('2026-05-15T11:00:00Z').toISOString(),
      children: [
        makePageMatched(101),
        makePageMatched(102, { id: 102, status: 'no_match' as const, matched_card: null }),
        makePageQueued(103),
      ],
      ...over,
    }
  }

  it('renders a page block with cells and shows the matched count in the progress label', async () => {
    const fetchMock = makeFetchMock({
      needsReview: [],
      pages: { needsReview: [makePage()] },
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-page-42')).toBeInTheDocument())

    expect(screen.getByTestId('scan-page-progress-42')).toHaveTextContent('1 / 9 matched')
    // Three real cells plus 6 empty placeholders for the 3×3 grid.
    expect(screen.getByTestId('scan-page-cell-101')).toBeInTheDocument()
    expect(screen.getByTestId('scan-page-cell-102')).toBeInTheDocument()
    expect(screen.getByTestId('scan-page-cell-103')).toBeInTheDocument()
    expect(screen.getAllByTestId('scan-page-cell-empty')).toHaveLength(6)
  })

  it('"Add all matched" posts action=add for each matched child', async () => {
    const fetchMock = makeFetchMock({
      needsReview: [],
      pages: {
        needsReview: [
          makePage({
            matched_count: 2,
            children: [makePageMatched(201), makePageMatched(202, { id: 202 })],
            expected_count: 4,
          }),
        ],
      },
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-page-42')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-page-add-all-42'))

    await waitFor(() => {
      const post201 = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/201/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      const post202 = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/202/resolve' &&
          (init as FetchInit | undefined)?.method === 'POST',
      )
      expect(post201).toBeTruthy()
      expect(post202).toBeTruthy()
      const body201 = JSON.parse(((post201?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
      expect(body201).toMatchObject({ action: 'add', variant_id: 11, quantity: 1 })
    })
  })

  it('"Discard page" with confirmation issues DELETE /api/pokemon/scans/pages/{id}', async () => {
    const fetchMock = makeFetchMock({
      needsReview: [],
      pages: { needsReview: [makePage()] },
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-page-42')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-page-discard-42'))
    fireEvent.click(screen.getByTestId('scan-page-discard-confirm-42'))

    await waitFor(() => {
      const del = fetchMock.mock.calls.find(
        ([url, init]) =>
          (url as string) === '/api/pokemon/scans/pages/42' &&
          (init as FetchInit | undefined)?.method === 'DELETE',
      )
      expect(del).toBeTruthy()
    })
  })

  it('cancelling the discard confirmation does NOT call DELETE', async () => {
    const fetchMock = makeFetchMock({
      needsReview: [],
      pages: { needsReview: [makePage()] },
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('scan-page-42')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('scan-page-discard-42'))
    fireEvent.click(screen.getByTestId('scan-page-discard-cancel-42'))

    const del = fetchMock.mock.calls.find(
      ([url, init]) =>
        (url as string).startsWith('/api/pokemon/scans/pages/') &&
        (init as FetchInit | undefined)?.method === 'DELETE',
    )
    expect(del).toBeFalsy()
  })

  it('?page=<id> highlights the matching page block once data is loaded', async () => {
    // A fresh page upload starts in the pending state, so the component
    // auto-switches to the "pending" filter when it sees ?page= — the mock
    // returns the page on that filter so the highlight effect can fire.
    const fetchMock = makeFetchMock({
      needsReview: [],
      pending: [],
      pages: {
        needsReview: [makePage()],
        pending: [makePage()],
      },
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/pokemon/scanned?page=42']}>
        <Routes>
          <Route path="/pokemon/scanned" element={<PokemonScanned />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => {
      const block = screen.getByTestId('scan-page-42')
      expect(block).toHaveAttribute('data-highlighted', 'true')
    })
  })
})
