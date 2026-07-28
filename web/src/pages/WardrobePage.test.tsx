// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import WardrobePage from './WardrobePage'

// ── Translation mock ──────────────────────────────────────────────────────────
// mockT must be a stable reference — WardrobePage's load effects have `t` as a
// dependency, so a new function per render would re-run them endlessly.

const TRANSLATIONS: Record<string, string> = {
  'title': "Kids' Wardrobe",
  'empty': 'No children yet',
  'emptyHint': 'Add a child to start tracking clothes, shoes and sizes',
  'addKid': 'Add child',
  'addItem': 'Add item',
  'tabs.inventory': 'Inventory',
  'tabs.measurements': 'Measurements',
  'tabs.needs': 'Needs',
  'stats.height': 'Height',
  'stats.clothingSize': 'Clothing size',
  'stats.shoeSize': 'Shoe size',
  'needs.missingTitle': 'Missing',
  'needs.empty': 'All covered',
  'inventory.empty': 'No items here yet',
  'errors.failedToLoad': 'Failed to load wardrobe data',
}

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (key === 'stats.buy') return `buy ${opts?.size}`
  if (key === 'needs.have') return `${opts?.have} of ${opts?.target}`
  if (key === 'needs.buySize') return `Buy ${opts?.size}`
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────

const authState: { user: object | null } = { user: null }

vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

const kid = {
  id: 1,
  name: 'Ola',
  birthdate: '2021-05-01',
  avatar_emoji: '🦖',
  latest_measurement: {
    id: 1, kid_id: 1, measured_at: '2026-07-01',
    height_cm: 100, foot_length_mm: 160, weight_kg: 0, note: '',
  },
  clothing: { current_size: 104, buy_size: 104 },
  shoe: { current_eu: 26, buy_eu: 27 },
}

const categories = [
  { id: 10, name: 'Regntøy', icon: '🌧️', size_system: 'clothing', target_qty: 1, sort_order: 0 },
  { id: 11, name: 'Sko', icon: '👟', size_system: 'shoe', target_qty: 1, sort_order: 1 },
]

const item = {
  id: 100, kid_id: 1, category_id: 10, name: 'Regnjakke blå', size_label: '98',
  quantity: 1, condition: 'good', status: 'active', location: 'kindergarten',
  season: 'all', notes: '',
}

const needsPayload = {
  needs: [{ category_id: 11, category_name: 'Sko', category_icon: '👟', have: 0, target: 1, recommended_size: 'EU 27' }],
  too_small: [],
}

// stubFetch dispatches on URL so the page's parallel loads all resolve.
function stubFetch(overrides: Partial<Record<string, unknown>> = {}) {
  const routes: Record<string, unknown> = {
    '/kids': { kids: [kid] },
    '/categories': { categories },
    '/items': { items: [item] },
    '/measurements': { measurements: [kid.latest_measurement] },
    '/needs': needsPayload,
    ...overrides,
  }
  const fetchMock = vi.fn((url: string) => {
    const path = url.replace('/api/wardrobe', '').split('?')[0]
    const match = Object.keys(routes).find(k => path === k || path.endsWith(k))
    if (!match) return Promise.resolve({ ok: false, json: () => Promise.resolve({}) })
    return Promise.resolve({ ok: true, json: () => Promise.resolve(routes[match]) })
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPage() {
  return render(
    <MemoryRouter>
      <WardrobePage />
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('WardrobePage', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows loading spinner on initial render', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { container } = renderPage()
    expect(container.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('shows empty state when there are no kids', async () => {
    stubFetch({ '/kids': { kids: [] } })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('No children yet')).toBeInTheDocument()
    })
  })

  it('renders the selected kid with size stats and inventory', async () => {
    stubFetch()
    renderPage()

    await waitFor(() => {
      expect(screen.getAllByText('Ola').length).toBeGreaterThan(0)
    })
    // Size guidance from the backend is displayed. The height also appears in
    // the measurements history (mounted but hidden tab panel) once that fetch
    // resolves, so assert on all matches rather than racing the second one.
    expect(screen.getAllByText('100 cm').length).toBeGreaterThan(0)
    expect(screen.getByText('EU 26')).toBeInTheDocument()
    expect(screen.getByText('buy EU 27')).toBeInTheDocument()
    // Inventory items grouped under their category.
    await waitFor(() => {
      expect(screen.getByText('Regnjakke blå')).toBeInTheDocument()
    })
    expect(screen.getByText(/Regntøy/)).toBeInTheDocument()
  })

  it('shows computed needs with recommended size on the needs tab', async () => {
    stubFetch()
    renderPage()

    await waitFor(() => {
      expect(screen.getAllByText('Ola').length).toBeGreaterThan(0)
    })
    fireEvent.click(screen.getByRole('tab', { name: /Needs/ }))
    await waitFor(() => {
      expect(screen.getByText('0 of 1')).toBeInTheDocument()
    })
    expect(screen.getByText('Buy EU 27')).toBeInTheDocument()
  })

  it('shows an error banner when loading fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false, json: () => Promise.resolve({}) })))
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to load wardrobe data')
    })
  })
})
