// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import WardrobePage from './WardrobePage'
import { ApiError, api, messageFor } from './wardrobeApi'

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
  'kidForm.name': 'Name',
  'measurements.add': 'New measurement',
  'measurements.height': 'Height (cm)',
  'common:actions.save': 'Save',
  'errors.failedToLoad': 'Failed to load wardrobe data',
  'errors.failedToSave': 'Failed to save',
  'errors.failedToDelete': 'Failed to delete',
  'errors.server.measurementOutOfRange': 'The measurement is out of range',
  'errors.server.nameRequired': 'Name is required',
  'errors.server.birthdateFormat': 'Birthdate must be a valid date',
  'categories.inUse': 'This category still has items',
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

// stubFetch dispatches on URL so the page's parallel loads all resolve. `onWrite`
// lets a test answer a specific mutation (POST/PATCH/DELETE) with an error body.
function stubFetch(
  overrides: Partial<Record<string, unknown>> = {},
  onWrite?: (path: string, init?: RequestInit) => unknown,
) {
  const routes: Record<string, unknown> = {
    '/kids': { kids: [kid] },
    '/categories': { categories },
    '/items': { items: [item] },
    '/measurements': { measurements: [kid.latest_measurement] },
    '/needs': needsPayload,
    ...overrides,
  }
  const fetchMock = vi.fn((url: string, init?: RequestInit) => {
    const path = url.replace('/api/wardrobe', '').split('?')[0]
    if (init?.method && onWrite) {
      const custom = onWrite(path, init)
      if (custom) return Promise.resolve(custom)
    }
    const match = Object.keys(routes).find(k => path === k || path.endsWith(k))
    if (!match) return Promise.resolve({ ok: false, json: () => Promise.resolve({}) })
    return Promise.resolve({ ok: true, json: () => Promise.resolve(routes[match]) })
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

// errorResponse mimics the backend's `{"error": "..."}` failure body.
function errorResponse(status: number, message: string) {
  return { ok: false, status, json: () => Promise.resolve({ error: message }) }
}

// messageFor takes the page's TFunction; the mock stands in for it in tests.
const t = mockT as unknown as Parameters<typeof messageFor>[1]

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

  it('shows the backend validation message inside the measurements form', async () => {
    stubFetch({}, (path, init) =>
      init?.method === 'POST' && path === '/kids/1/measurements'
        ? errorResponse(400, 'measurement out of range')
        : undefined,
    )
    renderPage()

    await waitFor(() => {
      expect(screen.getAllByText('Ola').length).toBeGreaterThan(0)
    })
    fireEvent.click(screen.getByRole('tab', { name: /Measurements/ }))

    const panel = screen.getByRole('tabpanel', { name: /Measurements/ })
    fireEvent.change(within(panel).getByLabelText('Height (cm)'), { target: { value: '400' } })
    fireEvent.click(within(panel).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(within(panel).getByRole('alert')).toHaveTextContent('The measurement is out of range')
    })
    // The page-level banner stays empty — the only alert is the one in the form.
    expect(screen.getAllByRole('alert')).toHaveLength(1)
    expect(screen.queryByText('Failed to save')).not.toBeInTheDocument()
  })

  it('keeps the kid dialog open with its values when the backend rejects the save', async () => {
    stubFetch({}, (path, init) =>
      init?.method === 'POST' && path === '/kids'
        ? errorResponse(400, 'birthdate must be YYYY-MM-DD')
        : undefined,
    )
    renderPage()

    await waitFor(() => {
      expect(screen.getAllByText('Ola').length).toBeGreaterThan(0)
    })
    fireEvent.click(screen.getByRole('button', { name: 'Add child' }))

    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Kari' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(within(dialog).getByRole('alert')).toHaveTextContent('Birthdate must be a valid date')
    })
    // The dialog stays open with the entered name so it can be corrected.
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('Name')).toHaveValue('Kari')
    expect(screen.queryByText('Failed to save')).not.toBeInTheDocument()
  })
})

describe('api', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('throws an ApiError carrying the status and the backend message', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(errorResponse(400, 'measurement out of range'))))
    const err = await api('/kids/1/measurements', { method: 'POST', body: '{}' }).catch(e => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(400)
    expect(err.serverMessage).toBe('measurement out of range')
  })

  it('leaves serverMessage null for a non-JSON body', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
      ok: false,
      status: 502,
      json: () => Promise.reject(new SyntaxError('Unexpected token < in JSON')),
    })))
    const err = await api('/items', { method: 'POST', body: '{}' }).catch(e => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(502)
    expect(err.serverMessage).toBeNull()
  })

  it('leaves serverMessage null when the body has no error field', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })))
    const err = await api('/items').catch(e => e)
    expect(err.serverMessage).toBeNull()
  })

  it('returns the response untouched when the request succeeds', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ kids: [] }) })))
    const res = await api('/kids')
    expect(await res.json()).toEqual({ kids: [] })
  })
})

describe('messageFor', () => {
  it.each([
    ['measurement out of range', 'The measurement is out of range'],
    ['name is required', 'Name is required'],
    ['category has items', 'This category still has items'],
    ['birthdate must be YYYY-MM-DD', 'Birthdate must be a valid date'],
    // Unmapped in the test translation table, so mockT echoes the key back —
    // enough to prove the message resolved to a translation key, not raw English.
    ['measured_at must be YYYY-MM-DD', 'errors.server.measuredAtFormat'],
    ['at least one measurement is required', 'errors.server.measurementRequired'],
    ['quantity must be at most 99', 'errors.server.quantityMax'],
    ['invalid condition', 'errors.server.invalidCondition'],
    ['invalid status', 'errors.server.invalidStatus'],
    ['invalid location', 'errors.server.invalidLocation'],
    ['invalid season', 'errors.server.invalidSeason'],
    ['kid_id is required', 'errors.server.kidRequired'],
    ['kid not found', 'errors.server.kidNotFound'],
    ['category not found', 'errors.server.categoryNotFound'],
    ['target_qty must be between 0 and 99', 'errors.server.targetQtyRange'],
    ['invalid size_system', 'errors.server.invalidSizeSystem'],
  ])('translates %s', (serverMessage, expected) => {
    const err = new ApiError(400, serverMessage, 'POST /x failed')
    expect(messageFor(err, t, 'errors.failedToSave')).toBe(expected)
  })

  it('falls back to the generic message for unmapped backend messages', () => {
    const err = new ApiError(500, 'failed to add item', 'POST /items failed')
    expect(messageFor(err, t, 'errors.failedToSave')).toBe('Failed to save')
  })

  it('falls back when there is no server message', () => {
    expect(messageFor(new ApiError(502, null, 'boom'), t, 'errors.failedToDelete')).toBe('Failed to delete')
  })

  it('falls back for errors that are not ApiError', () => {
    expect(messageFor(new Error('network down'), t, 'errors.failedToSave')).toBe('Failed to save')
    expect(messageFor(undefined, t, 'errors.failedToDelete')).toBe('Failed to delete')
  })
})
