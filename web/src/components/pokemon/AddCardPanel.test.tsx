// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import AddCardPanel from './AddCardPanel'

// Lightweight i18n mock — return predictable English strings keyed off the
// translation namespace so assertions stay readable.
const TRANSLATIONS: Record<string, string> = {
  'addCard.openLabel': 'Add a card',
  'addCard.dialogLabel': 'Add a card',
  'addCard.inputLabel': 'Search cards',
  'addCard.placeholder': 'Search cards',
  'addCard.close': 'Close',
  'addCard.searching': 'Searching…',
  'addCard.noResults': 'No cards match that search.',
  'addCard.results': 'Card results',
  'addCard.cancel': 'Cancel',
  'addCard.errors.searchFailed': 'Failed to search cards',
  'addCard.errors.addFailed': 'Failed to add card',
  'variantKind.normal': 'Normal',
  'variantKind.reverse_holofoil': 'Reverse holo',
  'variantKind.holofoil': 'Holo',
}

function mockT(key: string, opts?: Record<string, string | number> & { defaultValue?: string }): string {
  if (key === 'addCard.toast.added') return `Added ${opts?.name ?? ''}`
  if (key === 'addCard.variantPickerLabel') return `Pick a variant for ${opts?.name ?? ''}`
  if (key === 'addCard.variantPickerPrompt') return `Pick a variant for ${opts?.name ?? ''}`
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

vi.mock('../../i18n', () => ({
  default: { language: 'en' },
}))

interface Variant {
  id: number
  kind: string
  price_eur: number
  price_nok: number | null
  owned?: boolean
  owned_id?: number | null
  quantity?: number
  condition?: string
  notes?: string
}

interface Card {
  id: string
  set_id: string
  set_name?: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: Variant[]
}

function makeVariant(over: Partial<Variant> = {}): Variant {
  return {
    id: 1,
    kind: 'normal',
    price_eur: 10,
    price_nok: 100,
    owned: false,
    owned_id: null,
    quantity: 0,
    condition: '',
    notes: '',
    ...over,
  }
}

function makeCard(over: Partial<Card> = {}): Card {
  return {
    id: 'sv1-25',
    set_id: 'sv1',
    set_name: 'Scarlet & Violet Base',
    name: 'Pikachu',
    collector_no: '025',
    rarity: 'Common',
    image_small_url: 'https://example.com/small.png',
    image_large_url: 'https://example.com/large.png',
    variants: [makeVariant()],
    ...over,
  }
}

function jsonResponse<T>(body: T, init: { status?: number } = {}) {
  return {
    ok: (init.status ?? 200) < 400,
    status: init.status ?? 200,
    json: () => Promise.resolve(body),
  }
}

// makeFetchMock returns a fetch responder. Search returns `searchResults`;
// POST upsert defaults to 201; overrides take precedence per-call.
function makeFetchMock(
  searchResults: Card[],
  overrides?: (url: string, init?: RequestInit) => Response | null,
) {
  return vi.fn((url: string, init?: RequestInit) => {
    if (overrides) {
      const o = overrides(url, init)
      if (o) return Promise.resolve(o)
    }
    if (url.startsWith('/api/pokemon/cards/search')) {
      return Promise.resolve(jsonResponse({ cards: searchResults }))
    }
    if (url === '/api/pokemon/collection') {
      return Promise.resolve(
        jsonResponse(
          {
            item: {
              id: 42,
              quantity: 1,
              condition: '',
              notes: '',
              acquired_at: new Date().toISOString(),
            },
          },
          { status: 201 },
        ),
      )
    }
    return Promise.resolve(jsonResponse({}, { status: 404 }))
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

function openPanel() {
  fireEvent.click(screen.getByTestId('add-card-fab'))
}

describe('AddCardPanel — opening and closing', () => {
  it('opens on FAB click and closes on Esc', async () => {
    vi.stubGlobal('fetch', makeFetchMock([]))
    render(<AddCardPanel />)

    openPanel()
    expect(await screen.findByRole('dialog', { name: 'Add a card' })).toBeInTheDocument()

    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Add a card' })).not.toBeInTheDocument()
    })
  })

  it('closes on outside-click of the overlay', async () => {
    vi.stubGlobal('fetch', makeFetchMock([]))
    render(<AddCardPanel />)

    openPanel()
    const dialog = await screen.findByRole('dialog', { name: 'Add a card' })
    const overlay = dialog.parentElement
    if (!overlay) throw new Error('dialog has no overlay parent')
    fireEvent.click(overlay)
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Add a card' })).not.toBeInTheDocument()
    })
  })
})

describe('AddCardPanel — debounced search', () => {
  it('typing "pika" calls the search endpoint with q=pika and renders results', async () => {
    const card = makeCard({ id: 'sv1-25', name: 'Pikachu', collector_no: '025' })
    const fetchMock = makeFetchMock([card])
    vi.stubGlobal('fetch', fetchMock)

    render(<AddCardPanel />)
    openPanel()

    const input = screen.getByLabelText('Search cards')
    fireEvent.change(input, { target: { value: 'pika' } })

    expect(await screen.findByText('Pikachu')).toBeInTheDocument()
    const searchCalls = fetchMock.mock.calls.filter(([url]) =>
      String(url).startsWith('/api/pokemon/cards/search'),
    )
    expect(searchCalls.length).toBeGreaterThanOrEqual(1)
    expect(String(searchCalls[searchCalls.length - 1][0])).toContain('q=pika')
  })

  it('debounces rapid keystrokes — only the final query reaches the API', async () => {
    const card = makeCard({ id: 'sv1-25', name: 'Pikachu' })
    const fetchMock = makeFetchMock([card])
    vi.stubGlobal('fetch', fetchMock)

    render(<AddCardPanel />)
    openPanel()
    const input = screen.getByLabelText('Search cards')

    fireEvent.change(input, { target: { value: 'p' } })
    fireEvent.change(input, { target: { value: 'pi' } })
    fireEvent.change(input, { target: { value: 'pik' } })
    fireEvent.change(input, { target: { value: 'pika' } })

    await screen.findByText('Pikachu')
    const searchCalls = fetchMock.mock.calls.filter(([url]) =>
      String(url).startsWith('/api/pokemon/cards/search'),
    )
    // The debounce cancels the in-flight timer on each keystroke, so we
    // expect exactly one search call for the final value.
    expect(searchCalls.length).toBe(1)
    expect(String(searchCalls[0][0])).toContain('q=pika')
  })
})

describe('AddCardPanel — single variant adds immediately', () => {
  it('POSTs the card and shows the "Added <name>" toast', async () => {
    const card = makeCard({
      id: 'sv1-25',
      name: 'Pikachu',
      variants: [makeVariant({ id: 11, kind: 'normal' })],
    })
    const fetchMock = makeFetchMock([card])
    vi.stubGlobal('fetch', fetchMock)

    render(<AddCardPanel />)
    openPanel()
    fireEvent.change(screen.getByLabelText('Search cards'), { target: { value: 'pika' } })

    fireEvent.click(await screen.findByTestId('add-card-result-sv1-25'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) => url === '/api/pokemon/collection' && (init as RequestInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as RequestInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ card_id: 'sv1-25', variant_id: 11, quantity: 1 })
    })

    expect(await screen.findByText('Added Pikachu')).toBeInTheDocument()
  })

  it('invokes onAdded after a successful add', async () => {
    const card = makeCard({ id: 'sv1-25', variants: [makeVariant({ id: 11 })] })
    vi.stubGlobal('fetch', makeFetchMock([card]))
    const onAdded = vi.fn()

    render(<AddCardPanel onAdded={onAdded} />)
    openPanel()
    fireEvent.change(screen.getByLabelText('Search cards'), { target: { value: 'pika' } })

    fireEvent.click(await screen.findByTestId('add-card-result-sv1-25'))

    await waitFor(() => expect(onAdded).toHaveBeenCalledTimes(1))
  })
})

describe('AddCardPanel — multi-variant picker', () => {
  it('opens the picker and POSTs the selected variant', async () => {
    const card = makeCard({
      id: 'sv1-25',
      name: 'Pikachu',
      variants: [
        makeVariant({ id: 11, kind: 'normal' }),
        makeVariant({ id: 12, kind: 'reverse_holofoil' }),
        makeVariant({ id: 13, kind: 'holofoil' }),
      ],
    })
    const fetchMock = makeFetchMock([card])
    vi.stubGlobal('fetch', fetchMock)

    render(<AddCardPanel />)
    openPanel()
    fireEvent.change(screen.getByLabelText('Search cards'), { target: { value: 'pika' } })

    fireEvent.click(await screen.findByTestId('add-card-result-sv1-25'))

    // No POST yet — picker is open.
    expect(
      fetchMock.mock.calls.find(
        ([url, init]) => url === '/api/pokemon/collection' && (init as RequestInit | undefined)?.method === 'POST',
      ),
    ).toBeFalsy()

    expect(await screen.findByText('Pick a variant for Pikachu')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('add-card-variant-13'))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        ([url, init]) => url === '/api/pokemon/collection' && (init as RequestInit | undefined)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as RequestInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ card_id: 'sv1-25', variant_id: 13 })
    })
  })
})

describe('AddCardPanel — search error', () => {
  it('shows the failure message when the search endpoint errors', async () => {
    const fetchMock = makeFetchMock([], (url) => {
      if (url.startsWith('/api/pokemon/cards/search')) {
        return { ok: false, status: 500, json: () => Promise.resolve({ error: 'boom' }) } as unknown as Response
      }
      return null
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<AddCardPanel />)
    openPanel()
    fireEvent.change(screen.getByLabelText('Search cards'), { target: { value: 'pika' } })

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to search cards')
  })
})
