// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import PokemonSets from './PokemonSets'
import PokemonSet from './PokemonSet'

// End-to-end smoke flow for the Pokémon collection feature: open the sets
// browser → click a set → mark a card owned → verify the owned count
// increments. fetch is mocked per-URL so the flow runs entirely in-memory.

const TRANSLATIONS: Record<string, string> = {
  pageTitle: 'Pokémon Sets',
  retry: 'Retry',
  showOlder: 'Show older sets',
  hideOlder: 'Hide older sets',
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
  'toast.marked': 'Added to collection',
  'toast.unmarked': 'Removed from collection',
  'addCard.openLabel': 'Add a card to your collection',
}

function mockT(
  key: string,
  opts?: Record<string, string | number> & { defaultValue?: string },
): string {
  if (key === 'tile.ownership')
    return `${opts?.owned ?? 0} / ${opts?.total ?? 0} — ${opts?.percent ?? 0}%`
  if (key === 'tile.openSet') return `Open ${opts?.name ?? ''}`
  if (key === 'tile.openCard') return `Open ${opts?.name ?? ''} (#${opts?.number ?? ''})`
  if (key === 'tile.collectorNo') return `#${opts?.number ?? ''}`
  if (key === 'tile.totalCards') {
    const count = Number(opts?.count ?? 0)
    return count === 1 ? `${count} card` : `${count} cards`
  }
  if (key === 'detail.ownedOf') return `${opts?.owned ?? 0} / ${opts?.total ?? 0}`
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

// formatDate pulls in the real i18n bootstrap (HttpBackend), which would
// thrash the test env. Stub it down to a static language so the module loads
// without network IO.
vi.mock('../i18n', () => ({
  default: { language: 'en' },
}))

interface FetchInit {
  method?: string
  body?: BodyInit | null
}

interface PokemonResponse {
  ok: boolean
  status: number
  json: () => Promise<unknown>
}

function jsonResponse(body: unknown, status = 200): PokemonResponse {
  return {
    ok: status < 400,
    status,
    json: () => Promise.resolve(body),
  }
}

function makeFetchMock() {
  // Pikachu owns nothing initially. The POST handler flips ownership for
  // subsequent /cards reads (in case the page reloads) — but the page also
  // updates state optimistically, so the in-memory flag is just for safety.
  let owned = false

  return vi.fn((url: string, init?: FetchInit) => {
    // 1) Sets browser list endpoint with pagination.
    if (url.startsWith('/api/pokemon/sets?')) {
      return Promise.resolve(
        jsonResponse({
          sets: [
            {
              id: 'sv1',
              name: 'Scarlet & Violet Base',
              series: 'Scarlet & Violet',
              release_date: '2023/03/31',
              total_cards: 1,
              symbol_url: '',
              logo_url: '',
              owned_count: owned ? 1 : 0,
            },
          ],
        }),
      )
    }

    // 2) Set detail (cards in set) endpoint.
    if (url === '/api/pokemon/sets/sv1/cards') {
      return Promise.resolve(
        jsonResponse({
          set: {
            id: 'sv1',
            name: 'Scarlet & Violet Base',
            series: 'Scarlet & Violet',
            release_date: '2023/03/31',
            total_cards: 1,
            symbol_url: '',
            logo_url: '',
            owned_count: owned ? 1 : 0,
          },
          cards: [
            {
              id: 'sv1-1',
              set_id: 'sv1',
              name: 'Pikachu',
              collector_no: '001',
              rarity: 'Common',
              image_small_url: '',
              image_large_url: '',
              variants: [
                {
                  id: 1,
                  kind: 'normal',
                  price_eur: 10,
                  price_nok: 100,
                  owned,
                  owned_id: owned ? 42 : null,
                  quantity: owned ? 1 : 0,
                  condition: '',
                  notes: '',
                  acquired_at: null,
                },
              ],
            },
          ],
        }),
      )
    }

    // 3) Mark-owned mutation.
    if (url === '/api/pokemon/collection' && init?.method === 'POST') {
      owned = true
      return Promise.resolve(
        jsonResponse(
          {
            item: {
              id: 42,
              quantity: 1,
              condition: '',
              notes: '',
              acquired_at: new Date('2026-01-01T00:00:00Z').toISOString(),
            },
          },
          201,
        ),
      )
    }

    return Promise.resolve(jsonResponse({}, 404))
  })
}

function renderApp() {
  return render(
    <MemoryRouter initialEntries={['/pokemon']}>
      <Routes>
        <Route path="/pokemon" element={<PokemonSets />} />
        <Route path="/pokemon/sets/:id" element={<PokemonSet />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('Pokémon smoke flow', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('opens the sets browser, navigates to a set, and marks a card owned', async () => {
    const fetchMock = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)

    renderApp()

    // 1) Sets browser renders the set tile.
    const setLink = await screen.findByRole('link', { name: 'Open Scarlet & Violet Base' })
    expect(setLink).toHaveAttribute('href', '/pokemon/sets/sv1')

    // 2) Click into the set detail page.
    fireEvent.click(setLink)

    // 3) Wait for the card grid and confirm the starting count is 0 / 1.
    await waitFor(() => expect(screen.getByText('Pikachu')).toBeInTheDocument())
    expect(screen.getByTestId('owned-count')).toHaveTextContent('0 / 1')

    // 4) Open the detail panel and click "Mark as owned".
    fireEvent.click(screen.getByTestId('card-tile-sv1-1'))
    const markButton = await screen.findByRole('button', { name: 'Mark as owned' })
    fireEvent.click(markButton)

    // 5) The owned count should increment to 1 / 1 and the tile should report
    //    aria-pressed=true.
    await waitFor(() => expect(screen.getByTestId('owned-count')).toHaveTextContent('1 / 1'))
    expect(screen.getByTestId('card-tile-sv1-1')).toHaveAttribute('aria-pressed', 'true')

    // 6) The POST went out with the expected payload.
    const post = fetchMock.mock.calls.find(([url, init]) =>
      url === '/api/pokemon/collection' && (init as FetchInit | undefined)?.method === 'POST',
    )
    expect(post).toBeTruthy()
    const body = JSON.parse(((post?.[1] as FetchInit | undefined)?.body as string) ?? '{}')
    expect(body).toMatchObject({ card_id: 'sv1-1', variant_id: 1, quantity: 1 })
  })
})
