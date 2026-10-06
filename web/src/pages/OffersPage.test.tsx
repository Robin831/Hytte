// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import OffersPage from './OffersPage'

// mockT must be a stable reference — the load effect depends on `t`.
const TRANSLATIONS: Record<string, string> = {
  'title': 'Grocery Offers',
  'empty': 'No offers loaded yet',
  'watchlist.title': "Items I'm looking for",
  'watchlist.add': 'Add',
  'watchlist.placeholder': 'e.g. milk',
  'errors.failedToLoad': 'Failed to load offers',
  'filteredEmpty': 'No offers match your search or chain filter',
  'clearFilters': 'Clear filters',
}

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (key === 'watchedSection') return `Your items (${opts?.count})`
  if (key === 'topSection') return `Top offers (${opts?.count})`
  if (key === 'topSectionShowing') return `Top offers (showing ${opts?.shown} of ${opts?.total})`
  if (key === 'showMore') return `Show ${opts?.count} more`
  if (key === 'validTill') return `until ${opts?.date}`
  if (key === 'unitPrice') return `${opts?.price}/${opts?.unit}`
  if (key === 'addToGrocery') return `Add ${opts?.name} to grocery list`
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

const authState: { user: object | null } = { user: null }

vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

const offersPayload = {
  offers: [
    {
      id: 'milk', dealer_id: 'faa0Ym', dealer_name: 'REMA 1000', heading: 'TINE HELMELK',
      description: '', price: 36.4, pre_price: 49.9, currency: 'NOK', unit_price: 20.8,
      unit_label: 'l', image_url: '', run_from: '2026-07-26', run_till: '2026-08-01',
      discount_pct: 27, matched_keywords: ['melk'],
    },
    {
      id: 'pizza', dealer_id: '257bxm', dealer_name: 'KIWI', heading: 'Grandiosa Pizza',
      description: '', price: 39.9, pre_price: 49.9, currency: 'NOK', image_url: '',
      run_from: '2026-07-26', run_till: '2026-08-01', discount_pct: 20,
    },
  ],
  watchlist: [{ id: 1, keyword: 'melk' }],
  fetched_at: '2026-07-28T04:30:00Z',
}

function renderPage() {
  return render(
    <MemoryRouter>
      <OffersPage />
    </MemoryRouter>,
  )
}

describe('OffersPage', () => {
  beforeEach(() => { authState.user = { id: 1, is_admin: false } })
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows loading spinner on initial render', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { container } = renderPage()
    expect(container.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('renders watched offers above top offers with prices and discount', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve(offersPayload) })))
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('TINE HELMELK')).toBeInTheDocument()
    })
    expect(screen.getByText('Your items (1)')).toBeInTheDocument()
    expect(screen.getByText('Top offers (1)')).toBeInTheDocument()
    expect(screen.getByText('−27%')).toBeInTheDocument()
    expect(screen.getByText('20.80/l')).toBeInTheDocument()
    // Watchlist chip is rendered.
    expect(screen.getAllByText(/melk/i).length).toBeGreaterThan(0)
  })

  it('filters offers by search', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve(offersPayload) })))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Grandiosa Pizza')).toBeInTheDocument()
    })
    fireEvent.change(screen.getByLabelText('searchPlaceholder'), { target: { value: 'pizza' } })
    expect(screen.queryByText('TINE HELMELK')).not.toBeInTheDocument()
    expect(screen.getByText('Grandiosa Pizza')).toBeInTheDocument()
  })

  it('shows an error banner when loading fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false, json: () => Promise.resolve({}) })))
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to load offers')
    })
  })

  describe('paging and filtered-empty state', () => {
    function makeOffer(i: number, opts: { watched?: boolean; dealer?: 'kiwi' | 'rema' } = {}) {
      const dealer = opts.dealer ?? (i % 2 === 0 ? 'kiwi' : 'rema')
      return {
        id: `${opts.watched ? 'w' : 'o'}${i}`,
        dealer_id: dealer,
        dealer_name: dealer === 'kiwi' ? 'KIWI' : 'REMA 1000',
        heading: `${opts.watched ? 'Watched' : 'Offer'} item ${i}`,
        description: '', price: 10, currency: 'NOK', image_url: '',
        run_from: '2026-07-26', run_till: '2026-08-01',
        matched_keywords: opts.watched ? ['item'] : undefined,
      }
    }

    function stubOffers(offers: object[]) {
      vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ offers, watchlist: [], fetched_at: null }),
      })))
    }

    const countCards = (prefix: string) => screen.queryAllByText(new RegExp(`^${prefix} item \\d+$`)).length

    beforeEach(() => { localStorage.clear() })

    it('renders the first 50 un-watched offers and reveals more per click', async () => {
      stubOffers(Array.from({ length: 120 }, (_, i) => makeOffer(i)))
      renderPage()
      await waitFor(() => expect(screen.getByText('Top offers (showing 50 of 120)')).toBeInTheDocument())
      expect(countCards('Offer')).toBe(50)

      fireEvent.click(screen.getByRole('button', { name: 'Show 50 more' }))
      expect(screen.getByText('Top offers (showing 100 of 120)')).toBeInTheDocument()
      expect(countCards('Offer')).toBe(100)

      fireEvent.click(screen.getByRole('button', { name: 'Show 20 more' }))
      expect(countCards('Offer')).toBe(120)
      expect(screen.getByText('Top offers (120)')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /^Show \d+ more$/ })).not.toBeInTheDocument()
    })

    it('shows the total and no show-more button when nothing is truncated', async () => {
      stubOffers(Array.from({ length: 30 }, (_, i) => makeOffer(i)))
      renderPage()
      await waitFor(() => expect(screen.getByText('Top offers (30)')).toBeInTheDocument())
      expect(countCards('Offer')).toBe(30)
      expect(screen.queryByRole('button', { name: /^Show \d+ more$/ })).not.toBeInTheDocument()
    })

    it('renders every watched offer unpaged', async () => {
      stubOffers([
        ...Array.from({ length: 70 }, (_, i) => makeOffer(i, { watched: true })),
        ...Array.from({ length: 60 }, (_, i) => makeOffer(i)),
      ])
      renderPage()
      await waitFor(() => expect(screen.getByText('Your items (70)')).toBeInTheDocument())
      expect(countCards('Watched')).toBe(70)
      expect(countCards('Offer')).toBe(50)
      expect(screen.getByText('Top offers (showing 50 of 60)')).toBeInTheDocument()
    })

    it('resets to the first page when the search changes', async () => {
      stubOffers(Array.from({ length: 120 }, (_, i) => makeOffer(i)))
      renderPage()
      await waitFor(() => expect(screen.getByText('Top offers (showing 50 of 120)')).toBeInTheDocument())
      fireEvent.click(screen.getByRole('button', { name: 'Show 50 more' }))
      expect(countCards('Offer')).toBe(100)

      fireEvent.change(screen.getByLabelText('searchPlaceholder'), { target: { value: 'item 1' } })
      // "item 1", "item 10".."item 19", "item 100".."item 119" = 31 matches.
      expect(screen.getByText('Top offers (31)')).toBeInTheDocument()

      fireEvent.change(screen.getByLabelText('searchPlaceholder'), { target: { value: '' } })
      expect(screen.getByText('Top offers (showing 50 of 120)')).toBeInTheDocument()
      expect(countCards('Offer')).toBe(50)
    })

    it('resets to the first page when a chain is toggled', async () => {
      stubOffers(Array.from({ length: 200 }, (_, i) => makeOffer(i)))
      renderPage()
      await waitFor(() => expect(screen.getByText('Top offers (showing 50 of 200)')).toBeInTheDocument())
      fireEvent.click(screen.getByRole('button', { name: 'Show 50 more' }))
      fireEvent.click(screen.getByRole('button', { name: 'Show 50 more' }))
      expect(countCards('Offer')).toBe(150)

      fireEvent.click(screen.getByRole('button', { name: 'KIWI' }))
      expect(screen.getByText('Top offers (showing 50 of 100)')).toBeInTheDocument()
      expect(countCards('Offer')).toBe(50)
    })

    it('shows a single filtered-empty state that clears search and chain filters', async () => {
      stubOffers(Array.from({ length: 10 }, (_, i) => makeOffer(i)))
      renderPage()
      await waitFor(() => expect(screen.getByText('Top offers (10)')).toBeInTheDocument())

      fireEvent.click(screen.getByRole('button', { name: 'KIWI' }))
      fireEvent.change(screen.getByLabelText('searchPlaceholder'), { target: { value: 'nothing matches' } })
      expect(screen.getByText('No offers match your search or chain filter')).toBeInTheDocument()
      expect(screen.queryByText(/^Top offers/)).not.toBeInTheDocument()
      expect(screen.queryByText(/^Your items/)).not.toBeInTheDocument()
      expect(screen.queryByText('No offers loaded yet')).not.toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
      expect(screen.getByText('Top offers (10)')).toBeInTheDocument()
      expect(screen.getByLabelText('searchPlaceholder')).toHaveValue('')
      expect(screen.getByRole('button', { name: 'KIWI' })).toHaveAttribute('aria-pressed', 'true')
      expect(localStorage.getItem('offers-hidden-dealers')).toBe('[]')
    })

    it('shows the original empty state when the server returns no offers', async () => {
      stubOffers([])
      renderPage()
      await waitFor(() => expect(screen.getByText('No offers loaded yet')).toBeInTheDocument())
      expect(screen.queryByText('No offers match your search or chain filter')).not.toBeInTheDocument()
    })
  })
})
