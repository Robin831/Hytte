// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import GroceryPage from './GroceryPage'

// ── Translation mock ──────────────────────────────────────────────────────────
// mockT must be a stable reference — GroceryPage's initial-load useEffect has
// `t` as a dependency, so a new function on every render would cause an
// infinite re-run loop that burns through fetch mocks out of order.

const TRANSLATIONS: Record<string, string> = {
  'title': 'Grocery List',
  'addPlaceholder': 'Add an item...',
  'add': 'Add',
  'clearCompleted': 'Clear completed',
  'empty': 'Your grocery list is empty',
  'emptyHint': 'Add items using the input above',
  'checkedSection': 'Completed',
  'errors.failedToLoad': 'Failed to load grocery list',
  'errors.failedToAdd': 'Failed to add item',
  'errors.failedToUpdate': 'Failed to update item',
  'errors.failedToClear': 'Failed to clear completed items',
  'common:actions.close': 'Close',
}

function mockT(key: string, opts?: Record<string, string>): string {
  if (key === 'item.original') return `Original: ${opts?.text ?? ''}`
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

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeItem(overrides: Partial<{
  id: number; content: string; checked: boolean; sort_order: number
}> = {}) {
  return {
    id: 1,
    household_id: 1,
    content: 'Milk',
    original_text: '',
    source_language: 'en',
    checked: false,
    sort_order: 0,
    added_by: 1,
    created_at: '2026-04-09T00:00:00Z',
    ...overrides,
  }
}

function itemsResponse(items: ReturnType<typeof makeItem>[]) {
  return { ok: true, json: () => Promise.resolve({ items }) }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <GroceryPage />
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('GroceryPage – loading and empty state', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows loading spinner on initial render', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { container } = renderPage()
    expect(container.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('shows empty state when no items', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(itemsResponse([]))))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Your grocery list is empty')).toBeInTheDocument()
    })
  })
})

describe('GroceryPage – happy path', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders items returned by the API', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve(itemsResponse([makeItem({ content: 'Bread' })])),
    ))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Bread')).toBeInTheDocument()
    })
  })

  it('adds a new item via the add button', async () => {
    const newItem = makeItem({ id: 2, content: 'Eggs' })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([]))
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ item: newItem }) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Your grocery list is empty')).toBeInTheDocument())

    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Eggs' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    await waitFor(() => {
      expect(screen.getByText('Eggs')).toBeInTheDocument()
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/grocery/items', expect.objectContaining({ method: 'POST' }))
  })

  it('toggles an item checked (optimistic update)', async () => {
    const item = makeItem({ id: 1, content: 'Milk', checked: false })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([item]))
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Milk')).toBeInTheDocument())

    const checkbox = screen.getByRole('checkbox', { name: /milk/i })
    expect(checkbox).toHaveAttribute('aria-checked', 'false')
    fireEvent.click(checkbox)

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /milk/i })).toHaveAttribute('aria-checked', 'true')
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/grocery/items/1/check', expect.objectContaining({ method: 'PATCH' }))
  })

  it('clears completed items on button click', async () => {
    const unchecked = makeItem({ id: 1, content: 'Milk', checked: false })
    const checked = makeItem({ id: 2, content: 'Done item', checked: true })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([unchecked, checked]))
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Clear completed')).toBeInTheDocument())

    fireEvent.click(screen.getByText('Clear completed'))

    await waitFor(() => {
      expect(screen.queryByText('Done item')).not.toBeInTheDocument()
    })
    expect(screen.getByText('Milk')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/grocery/completed', expect.objectContaining({ method: 'DELETE' }))
  })
})

describe('GroceryPage – failure paths', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows error when initial load fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false })))
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert')).toHaveTextContent('Failed to load grocery list')
  })

  it('shows error and reverts to snapshot when clear-completed fails', async () => {
    const unchecked = makeItem({ id: 1, content: 'Milk', checked: false })
    const checked = makeItem({ id: 2, content: 'Done item', checked: true })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([unchecked, checked]))
      .mockResolvedValueOnce({ ok: false })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Clear completed')).toBeInTheDocument())

    fireEvent.click(screen.getByText('Clear completed'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert')).toHaveTextContent('Failed to clear completed items')
    // Both items restored from snapshot
    expect(screen.getByText('Milk')).toBeInTheDocument()
    expect(screen.getByText('Done item')).toBeInTheDocument()
  })

  it('shows error and refetches when toggle fails', async () => {
    const item = makeItem({ id: 1, content: 'Milk', checked: false })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([item]))          // initial load
      .mockResolvedValueOnce({ ok: false })                  // toggle PATCH fails
      .mockResolvedValueOnce(itemsResponse([item]))          // refetch in catch
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Milk')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('checkbox', { name: /milk/i }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert')).toHaveTextContent('Failed to update item')
    // After refetch, item is back to its server state (unchecked)
    expect(screen.getByRole('checkbox', { name: /milk/i })).toHaveAttribute('aria-checked', 'false')
  })
})
