// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import PokemonSet from './PokemonSet'

// Lightweight i18n mock — return predictable English strings keyed off the
// translation namespace so assertions stay readable.
const TRANSLATIONS: Record<string, string> = {
  'pageTitle': 'Pokémon Sets',
  'retry': 'Retry',
  'errors.failedToLoad': 'Failed to load Pokémon sets',
  'errors.markFailed': 'Failed to mark card as owned',
  'errors.unmarkFailed': 'Failed to unmark card',
  'detail.back': 'Back to sets',
  'detail.ownership': 'Owned',
  'detail.setValue': 'Set value',
  'detail.totalCards': 'Total cards',
  'detail.filter': 'Filter cards',
  'detail.filters.all': 'All',
  'detail.filters.owned': 'Owned only',
  'detail.filters.missing': 'Missing only',
  'detail.empty': 'No cards match this filter.',
  'detail.close': 'Close detail',
  'detail.variant': 'Variant',
  'detail.quantity': 'Quantity',
  'detail.increaseQuantity': 'Increase quantity',
  'detail.decreaseQuantity': 'Decrease quantity',
  'detail.condition': 'Condition',
  'detail.notes': 'Notes',
  'detail.notesPlaceholder': 'Notes',
  'detail.markOwned': 'Mark as owned',
  'detail.update': 'Update',
  'detail.unmark': 'Remove from collection',
  'condition.unset': 'Unspecified',
  'condition.mint': 'Mint',
  'condition.near_mint': 'Near mint',
  'condition.lightly_played': 'Lightly played',
  'condition.moderately_played': 'Moderately played',
  'condition.heavily_played': 'Heavily played',
  'condition.damaged': 'Damaged',
  'variantKind.normal': 'Normal',
  'variantKind.reverse_holofoil': 'Reverse holo',
  'variantKind.holofoil': 'Holo',
  'toast.marked': 'Added to collection',
  'toast.unmarked': 'Removed from collection',
  'set.variantFilter.label': 'Variant:',
  'set.variantFilter.any': 'Any',
  'set.variantFilter.normal': 'Normal',
  'set.variantFilter.reverseHolo': 'Reverse Holo',
  'set.variantFilter.allVariants': 'All variants',
}

function mockT(key: string, opts?: Record<string, string | number> & { defaultValue?: string }): string {
  if (key === 'detail.ownedOf') return `${opts?.owned ?? 0} / ${opts?.total ?? 0}`
  if (key === 'tile.openCard') return `Open ${opts?.name ?? ''} (#${opts?.number ?? ''})`
  if (key === 'tile.collectorNo') return `#${opts?.number ?? ''}`
  if (key === 'set.completion.any') return `${opts?.owned ?? 0} / ${opts?.total ?? 0} cards (any variant)`
  if (key === 'set.completion.normal') return `${opts?.owned ?? 0} / ${opts?.total ?? 0} cards with normal print owned`
  if (key === 'set.completion.reverseHolo') return `${opts?.owned ?? 0} / ${opts?.total ?? 0} cards with reverse holo owned`
  if (key === 'set.completion.allVariants') return `${opts?.owned ?? 0} / ${opts?.total ?? 0} cards with every variant owned`
  if (key === 'set.completion.kind') return `${opts?.owned ?? 0} / ${opts?.total ?? 0} cards (this variant)`
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

// Mock the formatDate utility's i18n dependency so we don't pull in the
// HttpBackend at test time.
vi.mock('../i18n', () => ({
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
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: Variant[]
}

interface SetShape {
  id: string
  name: string
  series: string
  release_date: string
  total_cards: number
  symbol_url: string
  logo_url: string
  owned_count: number
}

function makeSet(over: Partial<SetShape> = {}): SetShape {
  return {
    id: 'sv1',
    name: 'Scarlet & Violet Base',
    series: 'Scarlet & Violet',
    release_date: '2023/03/31',
    total_cards: 3,
    symbol_url: '',
    logo_url: '',
    owned_count: 0,
    ...over,
  }
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
    id: 'sv1-1',
    set_id: 'sv1',
    name: 'Pikachu',
    collector_no: '001',
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

type FetchSpec = {
  set: SetShape | null
  cards: Card[]
}

// makeFetchMock returns a vitest fetch mock that responds to the cards and
// sets endpoints, falling through to ok=true with empty pages for follow-up
// pagination requests. Mutation endpoints (POST/DELETE) default to success;
// caller can override per test.
function makeFetchMock(spec: FetchSpec, overrides?: (url: string, init?: RequestInit) => Response | null) {
  return vi.fn((url: string, init?: RequestInit) => {
    if (overrides) {
      const o = overrides(url, init)
      if (o) return Promise.resolve(o)
    }
    if (url.startsWith('/api/pokemon/sets/') && url.endsWith('/cards')) {
      return Promise.resolve(jsonResponse({ set: spec.set, cards: spec.cards }))
    }
    if (url.startsWith('/api/pokemon/collection/')) {
      // DELETE
      return Promise.resolve(jsonResponse({}, { status: 204 }))
    }
    if (url === '/api/pokemon/collection') {
      // POST upsert
      return Promise.resolve(jsonResponse({
        item: {
          id: 42,
          quantity: 1,
          condition: '',
          notes: '',
          acquired_at: new Date().toISOString(),
        },
      }, { status: 201 }))
    }
    return Promise.resolve(jsonResponse({}, { status: 404 }))
  })
}

function renderPage(initialPath = '/pokemon/sets/sv1') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/pokemon/sets/:id" element={<PokemonSet />} />
      </Routes>
    </MemoryRouter>,
  )
}

// LocationProbe exposes the current router location to assertions by writing
// `?...` into a hidden DOM node. We use it to verify the variant filter
// round-trips through the URL.
function LocationProbe() {
  const loc = useLocation()
  return <div data-testid="location-probe" data-pathname={loc.pathname} data-search={loc.search} />
}

function renderPageWithProbe(initialPath = '/pokemon/sets/sv1') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route
          path="/pokemon/sets/:id"
          element={
            <>
              <PokemonSet />
              <LocationProbe />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('PokemonSet – initial render', () => {
  beforeEach(() => {
    const set = makeSet({ total_cards: 2 })
    const cards = [
      makeCard({ id: 'sv1-1', name: 'Pikachu', collector_no: '001' }),
      makeCard({
        id: 'sv1-2',
        name: 'Eevee',
        collector_no: '002',
        variants: [
          makeVariant({ id: 2, kind: 'normal', price_eur: 5, price_nok: 50 }),
          makeVariant({ id: 3, kind: 'reverse_holofoil', price_eur: 8, price_nok: 80 }),
        ],
      }),
    ]
    vi.stubGlobal('fetch', makeFetchMock({ set, cards }))
  })

  it('renders the header, owned count and grid', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Scarlet & Violet Base' })).toBeInTheDocument()
    })
    expect(screen.getByText('Pikachu')).toBeInTheDocument()
    expect(screen.getByText('Eevee')).toBeInTheDocument()
    expect(screen.getByTestId('owned-count')).toHaveTextContent('0 / 2')
  })
})

describe('PokemonSet – mark owned (POST)', () => {
  it('calls POST /api/pokemon/collection, increments owned count, shows owned ring', async () => {
    const set = makeSet({ total_cards: 1 })
    const cards = [makeCard({ id: 'sv1-1', name: 'Pikachu' })]
    const fetchMock = makeFetchMock({ set, cards })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())
    expect(screen.getByTestId('owned-count')).toHaveTextContent('0 / 1')

    fireEvent.click(screen.getByTestId('card-tile-sv1-1'))

    const saveButton = await screen.findByRole('button', { name: 'Mark as owned' })
    fireEvent.click(saveButton)

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(([url, init]) =>
        url === '/api/pokemon/collection' && (init as RequestInit)?.method === 'POST',
      )
      expect(post).toBeTruthy()
      const body = JSON.parse(((post?.[1] as RequestInit | undefined)?.body as string) ?? '{}')
      expect(body).toMatchObject({ card_id: 'sv1-1', variant_id: 1, quantity: 1 })
    })

    await waitFor(() => expect(screen.getByTestId('owned-count')).toHaveTextContent('1 / 1'))
    const tile = screen.getByTestId('card-tile-sv1-1')
    expect(tile).toHaveAttribute('data-ownership', 'owned')
  })
})

describe('PokemonSet – unmark owned (DELETE)', () => {
  it('calls DELETE and removes owned indicator on success', async () => {
    const set = makeSet({ total_cards: 1 })
    const cards = [
      makeCard({
        id: 'sv1-1',
        variants: [makeVariant({ id: 1, owned: true, owned_id: 42, quantity: 1 })],
      }),
    ]
    const fetchMock = makeFetchMock({ set, cards })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())
    expect(screen.getByTestId('owned-count')).toHaveTextContent('1 / 1')

    fireEvent.click(screen.getByTestId('card-tile-sv1-1'))
    const unmark = await screen.findByRole('button', { name: 'Remove from collection' })
    fireEvent.click(unmark)

    await waitFor(() => {
      const del = fetchMock.mock.calls.find(([url, init]) =>
        url === '/api/pokemon/collection/42' && (init as RequestInit)?.method === 'DELETE',
      )
      expect(del).toBeTruthy()
    })

    await waitFor(() => expect(screen.getByTestId('owned-count')).toHaveTextContent('0 / 1'))
    expect(screen.getByTestId('card-tile-sv1-1')).toHaveAttribute('data-ownership', 'missing')
  })
})

describe('PokemonSet – filter toggles', () => {
  it('filters to owned only and missing only', async () => {
    const set = makeSet({ total_cards: 2 })
    const cards = [
      makeCard({
        id: 'sv1-1',
        name: 'Pikachu',
        variants: [makeVariant({ id: 1, owned: true, owned_id: 1, quantity: 1 })],
      }),
      makeCard({
        id: 'sv1-2',
        name: 'Eevee',
        variants: [makeVariant({ id: 2 })],
      }),
    ]
    vi.stubGlobal('fetch', makeFetchMock({ set, cards }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    expect(screen.getByText('Eevee')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('radio', { name: 'Owned only' }))
    expect(screen.getByText('Pikachu')).toBeInTheDocument()
    expect(screen.queryByText('Eevee')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('radio', { name: 'Missing only' }))
    expect(screen.queryByText('Pikachu')).not.toBeInTheDocument()
    expect(screen.getByText('Eevee')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('radio', { name: 'All' }))
    expect(screen.getByText('Pikachu')).toBeInTheDocument()
    expect(screen.getByText('Eevee')).toBeInTheDocument()
  })
})

describe('PokemonSet – mark error reverts optimistic UI', () => {
  it('reverts the owned count when the POST fails', async () => {
    const set = makeSet({ total_cards: 1 })
    const cards = [makeCard({ id: 'sv1-1' })]
    const fetchMock = makeFetchMock({ set, cards }, (url, init) => {
      if (url === '/api/pokemon/collection' && (init as RequestInit | undefined)?.method === 'POST') {
        return { ok: false, status: 500, json: () => Promise.resolve({ error: 'boom' }) } as unknown as Response
      }
      return null
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('card-tile-sv1-1'))
    fireEvent.click(await screen.findByRole('button', { name: 'Mark as owned' }))

    await waitFor(() => expect(screen.getByTestId('owned-count')).toHaveTextContent('0 / 1'))
    expect(screen.getByTestId('card-tile-sv1-1')).toHaveAttribute('data-ownership', 'missing')
  })
})

describe('PokemonSet – set value sums owned variants', () => {
  it('updates the set value when a card is marked owned', async () => {
    const set = makeSet({ total_cards: 1 })
    const cards = [
      makeCard({
        id: 'sv1-1',
        variants: [makeVariant({ id: 1, price_nok: 100 })],
      }),
    ]
    vi.stubGlobal('fetch', makeFetchMock({ set, cards }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())
    const valueBefore = screen.getByTestId('set-value').textContent ?? ''
    expect(valueBefore).toMatch(/0/)

    fireEvent.click(screen.getByTestId('card-tile-sv1-1'))
    fireEvent.click(await screen.findByRole('button', { name: 'Mark as owned' }))

    await waitFor(() => {
      const valueAfter = screen.getByTestId('set-value').textContent ?? ''
      expect(valueAfter).toMatch(/100/)
    })
  })
})

describe('PokemonSet – detail panel variant selection', () => {
  it('renders a radio per variant', async () => {
    const set = makeSet({ total_cards: 1 })
    const cards = [
      makeCard({
        id: 'sv1-1',
        variants: [
          makeVariant({ id: 1, kind: 'normal' }),
          makeVariant({ id: 2, kind: 'reverse_holofoil' }),
        ],
      }),
    ]
    vi.stubGlobal('fetch', makeFetchMock({ set, cards }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('card-tile-sv1-1'))
    const fieldset = await screen.findByRole('group', { name: 'Variant' })
    expect(within(fieldset).getByText('Normal')).toBeInTheDocument()
    expect(within(fieldset).getByText('Reverse holo')).toBeInTheDocument()
  })
})

describe('PokemonSet – lightbox', () => {
  it('clicking a tile opens the CardLightbox dialog with the card name', async () => {
    const set = makeSet({ total_cards: 2 })
    const cards = [
      makeCard({ id: 'sv1-1', name: 'Pikachu' }),
      makeCard({ id: 'sv1-2', name: 'Eevee' }),
    ]
    vi.stubGlobal('fetch', makeFetchMock({ set, cards }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    fireEvent.click(screen.getByTestId('card-tile-sv1-2'))

    const dialog = await screen.findByRole('dialog', { name: 'Eevee' })
    expect(dialog).toBeInTheDocument()
    // Sanity: the prev/next zones from the lightbox should be present.
    expect(screen.getByTestId('lightbox-prev-zone')).toBeInTheDocument()
    expect(screen.getByTestId('lightbox-next-zone')).toBeInTheDocument()
  })
})

describe('PokemonSet – variant filter chip', () => {
  // Fixture: three cards.
  //  sv1-1 Pikachu: normal owned, reverse_holofoil missing
  //  sv1-2 Eevee:   normal missing, reverse_holofoil owned
  //  sv1-3 Mew:     ultra rare with only normal (no reverse)
  function mixedCards() {
    return [
      makeCard({
        id: 'sv1-1',
        name: 'Pikachu',
        variants: [
          makeVariant({ id: 1, kind: 'normal', owned: true, owned_id: 11, quantity: 1, price_nok: 50 }),
          makeVariant({ id: 2, kind: 'reverse_holofoil', owned: false, price_nok: 80 }),
        ],
      }),
      makeCard({
        id: 'sv1-2',
        name: 'Eevee',
        variants: [
          makeVariant({ id: 3, kind: 'normal', owned: false, price_nok: 60 }),
          makeVariant({ id: 4, kind: 'reverse_holofoil', owned: true, owned_id: 12, quantity: 1, price_nok: 100 }),
        ],
      }),
      makeCard({
        id: 'sv1-3',
        name: 'Mew',
        variants: [
          makeVariant({ id: 5, kind: 'normal', owned: false, price_nok: 500 }),
        ],
      }),
    ]
  }

  it('renders chips dynamically for the kinds in the set', async () => {
    const set = makeSet({ total_cards: 3 })
    vi.stubGlobal('fetch', makeFetchMock({ set, cards: mixedCards() }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    const group = screen.getByTestId('variant-filter')
    expect(within(group).getByRole('radio', { name: 'Any' })).toBeInTheDocument()
    expect(within(group).getByRole('radio', { name: 'Normal' })).toBeInTheDocument()
    expect(within(group).getByRole('radio', { name: 'Reverse Holo' })).toBeInTheDocument()
    expect(within(group).getByRole('radio', { name: 'All variants' })).toBeInTheDocument()
  })

  it('hides chips for kinds not present in the set', async () => {
    // Only normal variants in this set — no Reverse Holo chip, and no "All
    // variants" chip because no card has more than one variant.
    const set = makeSet({ total_cards: 1 })
    const cards = [makeCard({ id: 'sv1-1', variants: [makeVariant({ id: 1, kind: 'normal' })] })]
    vi.stubGlobal('fetch', makeFetchMock({ set, cards }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    // With a single-kind, single-variant set the chip row collapses entirely.
    expect(screen.queryByTestId('variant-filter')).not.toBeInTheDocument()
  })

  it('Any (default) counts a card as owned when any variant is owned', async () => {
    const set = makeSet({ total_cards: 3 })
    vi.stubGlobal('fetch', makeFetchMock({ set, cards: mixedCards() }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    // Pikachu owns normal, Eevee owns reverse → 2/3 owned, Mew missing.
    expect(screen.getByTestId('owned-count')).toHaveTextContent('2 / 3')
    expect(screen.getByTestId('card-tile-sv1-1')).toHaveAttribute('data-ownership', 'owned')
    expect(screen.getByTestId('card-tile-sv1-2')).toHaveAttribute('data-ownership', 'owned')
    expect(screen.getByTestId('card-tile-sv1-3')).toHaveAttribute('data-ownership', 'missing')
  })

  it('Normal filter shows owned only for cards whose normal is owned', async () => {
    const set = makeSet({ total_cards: 3 })
    vi.stubGlobal('fetch', makeFetchMock({ set, cards: mixedCards() }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    fireEvent.click(within(screen.getByTestId('variant-filter')).getByRole('radio', { name: 'Normal' }))

    // Only Pikachu owns normal → 1/3.
    expect(screen.getByTestId('owned-count')).toHaveTextContent('1 / 3')
    expect(screen.getByTestId('card-tile-sv1-1')).toHaveAttribute('data-ownership', 'owned')
    expect(screen.getByTestId('card-tile-sv1-2')).toHaveAttribute('data-ownership', 'missing')
    expect(screen.getByTestId('card-tile-sv1-3')).toHaveAttribute('data-ownership', 'missing')
  })

  it('Reverse Holo filter excludes cards without a reverse variant from the denominator', async () => {
    const set = makeSet({ total_cards: 3 })
    vi.stubGlobal('fetch', makeFetchMock({ set, cards: mixedCards() }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    fireEvent.click(
      within(screen.getByTestId('variant-filter')).getByRole('radio', { name: 'Reverse Holo' }),
    )

    // Two cards have a reverse_holofoil variant (Pikachu, Eevee). Eevee owns it.
    expect(screen.getByTestId('owned-count')).toHaveTextContent('1 / 2')
    // Mew has no reverse variant — tile is marked not-applicable.
    expect(screen.getByTestId('card-tile-sv1-3')).toHaveAttribute('data-ownership', 'na')
  })

  it('All variants filter only counts cards where every variant is owned', async () => {
    const set = makeSet({ total_cards: 3 })
    vi.stubGlobal('fetch', makeFetchMock({ set, cards: mixedCards() }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    fireEvent.click(
      within(screen.getByTestId('variant-filter')).getByRole('radio', { name: 'All variants' }),
    )

    // No card has every variant owned → 0/3.
    expect(screen.getByTestId('owned-count')).toHaveTextContent('0 / 3')
    // Partial ownership indicator on cards with some variants owned.
    expect(screen.getByTestId('card-tile-sv1-1')).toHaveAttribute('data-ownership', 'partial')
    expect(screen.getByTestId('card-tile-sv1-2')).toHaveAttribute('data-ownership', 'partial')
    // Mew has only one variant and isn't owned → fully missing.
    expect(screen.getByTestId('card-tile-sv1-3')).toHaveAttribute('data-ownership', 'missing')
  })

  it('initializes from URL ?variant=reverse_holofoil', async () => {
    const set = makeSet({ total_cards: 3 })
    vi.stubGlobal('fetch', makeFetchMock({ set, cards: mixedCards() }))

    renderPageWithProbe('/pokemon/sets/sv1?variant=reverse_holofoil')
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    expect(
      within(screen.getByTestId('variant-filter')).getByRole('radio', { name: 'Reverse Holo' }),
    ).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('owned-count')).toHaveTextContent('1 / 2')
  })

  it('writes the active filter back to the URL', async () => {
    const set = makeSet({ total_cards: 3 })
    vi.stubGlobal('fetch', makeFetchMock({ set, cards: mixedCards() }))

    renderPageWithProbe()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    // Default 'any' should not appear in the URL.
    expect(screen.getByTestId('location-probe')).toHaveAttribute('data-search', '')

    fireEvent.click(
      within(screen.getByTestId('variant-filter')).getByRole('radio', { name: 'Reverse Holo' }),
    )

    await waitFor(() => {
      expect(screen.getByTestId('location-probe')).toHaveAttribute(
        'data-search',
        '?variant=reverse_holofoil',
      )
    })

    // Switching back to Any clears the param.
    fireEvent.click(within(screen.getByTestId('variant-filter')).getByRole('radio', { name: 'Any' }))
    await waitFor(() => {
      expect(screen.getByTestId('location-probe')).toHaveAttribute('data-search', '')
    })
  })

  it('falls back to Any when the URL contains an unknown variant', async () => {
    const set = makeSet({ total_cards: 3 })
    vi.stubGlobal('fetch', makeFetchMock({ set, cards: mixedCards() }))

    renderPage('/pokemon/sets/sv1?variant=holofoil')
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    // holofoil isn't in this set's kinds → no chip rendered, Any stays checked.
    expect(
      within(screen.getByTestId('variant-filter')).getByRole('radio', { name: 'Any' }),
    ).toHaveAttribute('aria-checked', 'true')
  })

  it('Set value updates to the active variants only', async () => {
    const set = makeSet({ total_cards: 3 })
    vi.stubGlobal('fetch', makeFetchMock({ set, cards: mixedCards() }))

    renderPage()
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())

    // Default 'any': owned variants are Pikachu-normal (50) and Eevee-reverse
    // (100) → 150.
    expect(screen.getByTestId('set-value').textContent ?? '').toMatch(/150/)

    fireEvent.click(within(screen.getByTestId('variant-filter')).getByRole('radio', { name: 'Normal' }))
    // Only Pikachu-normal counted under the Normal filter → 50.
    await waitFor(() => {
      const text = screen.getByTestId('set-value').textContent ?? ''
      expect(text).toMatch(/50/)
      expect(text).not.toMatch(/150/)
    })
  })
})
