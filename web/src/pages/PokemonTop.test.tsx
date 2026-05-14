// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import PokemonTop from './PokemonTop'

const TRANSLATIONS: Record<string, string> = {
  'retry': 'Retry',
  'errors.failedToLoad': 'Failed to load Pokémon cards',
  'top.title': 'Top valued cards',
  'top.subtitle': 'What the kid could find in a pack…',
  'top.entryButton': 'Top valued',
  'top.entryLabel': 'View the top valued cards',
  'top.back': 'Back to Pokémon sets',
  'top.empty': 'No top valued cards match this filter.',
  'top.filterLabel': 'Filter top valued cards',
  'top.filter.all': 'All',
  'top.filter.owned': 'Owned',
  'top.filter.missing': 'Missing',
  'top.ownership.owned': 'owned',
  'top.ownership.missing': 'not owned',
  'variantKind.normal': 'Normal',
  'variantKind.holofoil': 'Holo',
  'variantKind.reverse_holofoil': 'Reverse holo',
}

function mockT(key: string, opts?: Record<string, string | number> & { defaultValue?: string }): string {
  if (key === 'tile.collectorNo') return `#${opts?.number ?? ''}`
  if (key === 'top.priceLabel') return `${opts?.nok ?? ''} kr`
  if (key === 'top.priceLabelDetailed') return `${opts?.nok ?? ''} kr (€${opts?.eur ?? ''})`
  if (key === 'top.tileLabel') {
    return `Open ${opts?.name ?? ''} from ${opts?.set ?? ''} (#${opts?.number ?? ''}), rank ${opts?.rank ?? ''}, ${opts?.variant ?? ''}, ${opts?.price ?? ''}, ${opts?.ownership ?? ''}`
  }
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

interface Variant {
  id: number
  kind: string
  price_eur: number
  price_nok: number | null
}

interface TopCard {
  id: string
  set_id: string
  set_name: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: Variant[]
  top_variant_kind: string
  owned_by_me: boolean
}

function makeCard(over: Partial<TopCard> = {}): TopCard {
  return {
    id: 'sv1-1',
    set_id: 'sv1',
    set_name: 'Scarlet & Violet Base',
    name: 'Pikachu',
    collector_no: '001',
    rarity: 'Common',
    image_small_url: 'https://example.com/small.png',
    image_large_url: 'https://example.com/large.png',
    variants: [
      { id: 1, kind: 'normal', price_eur: 25, price_nok: 287 },
    ],
    top_variant_kind: 'normal',
    owned_by_me: false,
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

function makeFetchMock(cards: TopCard[]) {
  return vi.fn((_url: string, _init?: RequestInit) => {
    return Promise.resolve(jsonResponse({ cards }))
  })
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/pokemon/top']}>
      <Routes>
        <Route path="/pokemon/top" element={<PokemonTop />} />
      </Routes>
    </MemoryRouter>,
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('PokemonTop – renders top cards', () => {
  it('shows three tiles with prices and the owned indicator on the right card', async () => {
    const cards: TopCard[] = [
      makeCard({
        id: 'swsh1-1',
        set_id: 'swsh1',
        set_name: 'Sword & Shield Base',
        name: 'Celebi V',
        collector_no: '001',
        top_variant_kind: 'normal',
        owned_by_me: true,
        variants: [{ id: 1, kind: 'normal', price_eur: 25, price_nok: 287 }],
      }),
      makeCard({
        id: 'sv1-25',
        set_id: 'sv1',
        set_name: 'Scarlet & Violet Base',
        name: 'Pikachu',
        collector_no: '025',
        top_variant_kind: 'reverse_holofoil',
        owned_by_me: false,
        variants: [
          { id: 2, kind: 'normal', price_eur: 10, price_nok: 115 },
          { id: 3, kind: 'reverse_holofoil', price_eur: 14.5, price_nok: 166 },
        ],
      }),
      makeCard({
        id: 'sv1-100',
        set_id: 'sv1',
        set_name: 'Scarlet & Violet Base',
        name: 'Eevee',
        collector_no: '100',
        top_variant_kind: 'normal',
        owned_by_me: false,
        variants: [{ id: 4, kind: 'normal', price_eur: 2.5, price_nok: 29 }],
      }),
    ]
    vi.stubGlobal('fetch', makeFetchMock(cards))

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Celebi V')).toBeInTheDocument()
    })
    expect(screen.getByText('Pikachu')).toBeInTheDocument()
    expect(screen.getByText('Eevee')).toBeInTheDocument()

    // Prices rendered (currency formatting is locale-dependent so we look for the digits).
    const celebiTile = screen.getByTestId('top-card-tile-swsh1-1')
    expect(celebiTile.textContent ?? '').toMatch(/287/)
    const pikachuTile = screen.getByTestId('top-card-tile-sv1-25')
    expect(pikachuTile.textContent ?? '').toMatch(/166/)
    // EUR fallback price also visible.
    expect(pikachuTile.textContent ?? '').toMatch(/14|15/)
    // Variant kind label for the top variant.
    expect(pikachuTile.textContent ?? '').toMatch(/Reverse holo/)

    // Owned indicator appears only on Celebi V.
    expect(screen.getByTestId('top-card-owned-swsh1-1')).toBeInTheDocument()
    expect(screen.queryByTestId('top-card-owned-sv1-25')).not.toBeInTheDocument()
    expect(screen.queryByTestId('top-card-owned-sv1-100')).not.toBeInTheDocument()
  })
})

describe('PokemonTop – filter chips change the fetch URL', () => {
  it('refetches with owned=owned when the Owned chip is clicked', async () => {
    const fetchMock = makeFetchMock([])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    // Initial mount fires the "any" request.
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/pokemon/top?owned=any')

    fireEvent.click(screen.getByTestId('top-filter-owned'))

    await waitFor(() => {
      const ownedCall = fetchMock.mock.calls.find(([url]) => url === '/api/pokemon/top?owned=owned')
      expect(ownedCall).toBeTruthy()
    })

    fireEvent.click(screen.getByTestId('top-filter-missing'))

    await waitFor(() => {
      const missingCall = fetchMock.mock.calls.find(([url]) => url === '/api/pokemon/top?owned=missing')
      expect(missingCall).toBeTruthy()
    })
  })
})

describe('PokemonTop – empty state', () => {
  it('shows the empty message when no cards are returned', async () => {
    vi.stubGlobal('fetch', makeFetchMock([]))

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('No top valued cards match this filter.')).toBeInTheDocument()
    })
  })
})

describe('PokemonTop – accessible tile label', () => {
  it('includes rank, set, variant, price, and ownership in the aria-label', async () => {
    const cards: TopCard[] = [
      makeCard({
        id: 'sv1-25',
        set_id: 'sv1',
        set_name: 'Scarlet & Violet Base',
        name: 'Pikachu',
        collector_no: '025',
        top_variant_kind: 'reverse_holofoil',
        owned_by_me: false,
        variants: [
          { id: 2, kind: 'normal', price_eur: 10, price_nok: 115 },
          { id: 3, kind: 'reverse_holofoil', price_eur: 14.5, price_nok: 166 },
        ],
      }),
      makeCard({
        id: 'swsh1-1',
        set_id: 'swsh1',
        set_name: 'Sword & Shield Base',
        name: 'Celebi V',
        collector_no: '001',
        top_variant_kind: 'normal',
        owned_by_me: true,
        variants: [{ id: 1, kind: 'normal', price_eur: 25, price_nok: 287 }],
      }),
    ]
    vi.stubGlobal('fetch', makeFetchMock(cards))

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Pikachu')).toBeInTheDocument()
    })

    const pikachuTile = screen.getByTestId('top-card-tile-sv1-25')
    const pikachuLabel = pikachuTile.getAttribute('aria-label') ?? ''
    expect(pikachuLabel).toMatch(/Pikachu/)
    expect(pikachuLabel).toMatch(/Scarlet & Violet Base/)
    expect(pikachuLabel).toMatch(/#025/)
    expect(pikachuLabel).toMatch(/rank 1/)
    expect(pikachuLabel).toMatch(/Reverse holo/)
    expect(pikachuLabel).toMatch(/166 kr/)
    expect(pikachuLabel).toMatch(/not owned/)
    // Should not advertise toggle semantics — this is a navigation button.
    expect(pikachuTile.hasAttribute('aria-pressed')).toBe(false)

    const celebiTile = screen.getByTestId('top-card-tile-swsh1-1')
    const celebiLabel = celebiTile.getAttribute('aria-label') ?? ''
    expect(celebiLabel).toMatch(/rank 2/)
    // Owned cards must report "owned" (not "not owned") as the ownership suffix.
    expect(celebiLabel).toMatch(/, owned$/)
    expect(celebiTile.hasAttribute('aria-pressed')).toBe(false)
  })
})
