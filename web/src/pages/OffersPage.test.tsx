// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import OffersPage from './OffersPage'

// mockT must be a stable reference — the load effect depends on `t`.
const TRANSLATIONS: Record<string, string> = {
  'title': 'Grocery Offers',
  'empty': 'No offers loaded yet',
  'watchlist.title': "Items I'm looking for",
  'watchlist.add': 'Add',
  'watchlist.placeholder': 'e.g. milk',
  'errors.failedToLoad': 'Failed to load offers',
}

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (key === 'watchedSection') return `Your items (${opts?.count})`
  if (key === 'topSection') return `Top offers (${opts?.count})`
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
})
