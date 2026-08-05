// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
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
  'undo': 'Undo',
  'toast.itemCheckedOff': 'Item checked off',
  'empty': 'Your grocery list is empty',
  'emptyHint': 'Add items using the input above',
  'checkedSection': 'Completed',
  'translating': 'Translating voice input...',
  'voice.start': 'Start voice input',
  'voice.stop': 'Stop recording',
  'errors.failedToLoad': 'Failed to load grocery list',
  'errors.failedToAdd': 'Failed to add item',
  'errors.failedToUpdate': 'Failed to update item',
  'errors.failedToClear': 'Failed to clear completed items',
  'errors.failedToUndo': 'Failed to undo',
  'errors.failedToTranslate': 'Failed to translate voice input',
  'errors.failedToStartRecording': 'Failed to start recording',
  'common:actions.close': 'Close',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  if (key === 'item.original') return `Original: ${opts?.text ?? ''}`
  if (key === 'toast.itemsCleared') {
    const count = Number(opts?.count ?? 0)
    return `${count} item${count === 1 ? '' : 's'} cleared`
  }
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

// ── EventSource mock ─────────────────────────────────────────────────────
// The SSE subscription effect creates an EventSource; happy-dom doesn't
// provide one. Re-stubbed in beforeEach because afterEach calls
// vi.unstubAllGlobals().

class MockEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  readonly CONNECTING = 0
  readonly OPEN = 1
  readonly CLOSED = 2
  readyState = MockEventSource.OPEN
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  close = vi.fn(() => { this.readyState = MockEventSource.CLOSED })
  addEventListener = vi.fn()
}

beforeEach(() => { vi.stubGlobal('EventSource', MockEventSource) })

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeItem(overrides: Partial<{
  id: number; content: string; checked: boolean; sort_order: number
  original_text: string; source_language: string
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

// ── Undo tests ────────────────────────────────────────────────────────────────

describe('GroceryPage – undo', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.clearAllMocks() })

  const ok = () => ({ ok: true, json: () => Promise.resolve({}) })

  it('offers undo after checking an item and unchecks it again on click', async () => {
    const item = makeItem({ id: 1, content: 'Milk', checked: false })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([item]))   // initial load
      .mockResolvedValueOnce(ok())                    // check PATCH
      .mockResolvedValueOnce(ok())                    // undo PATCH
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Milk')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('checkbox', { name: /milk/i }))

    const undo = await screen.findByRole('button', { name: 'Undo' })
    expect(screen.getByRole('status')).toHaveTextContent('Item checked off')
    // The undo affordance is a real button, so it is focusable by keyboard
    expect(undo.tagName).toBe('BUTTON')
    undo.focus()
    expect(undo).toHaveFocus()

    fireEvent.click(undo)

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /milk/i })).toHaveAttribute('aria-checked', 'false')
    })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/grocery/items/1/check', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ checked: false }),
    }))
    // Tapping undo dismisses the toast immediately
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
  })

  it('restores cleared items via POST + check when undo is tapped', async () => {
    const unchecked = makeItem({ id: 1, content: 'Milk', checked: false })
    const bread = makeItem({
      id: 2, content: 'Bread', checked: true,
      original_text: 'ขนมปัง', source_language: 'th',
    })
    const eggs = makeItem({ id: 3, content: 'Eggs', checked: true })
    const restoredBread = makeItem({ id: 4, content: 'Bread', checked: true, original_text: 'ขนมปัง', source_language: 'th' })
    const restoredEggs = makeItem({ id: 5, content: 'Eggs', checked: true })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([unchecked, bread, eggs]))                    // initial load
      .mockResolvedValueOnce(ok())                                                       // DELETE completed
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ item: restoredBread }) }) // POST Bread
      .mockResolvedValueOnce(ok())                                                       // PATCH 4 checked
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ item: restoredEggs }) })  // POST Eggs
      .mockResolvedValueOnce(ok())                                                       // PATCH 5 checked
      .mockResolvedValueOnce(itemsResponse([unchecked, restoredBread, restoredEggs]))     // resync
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Clear completed')).toBeInTheDocument())

    fireEvent.click(screen.getByText('Clear completed'))

    const undo = await screen.findByRole('button', { name: 'Undo' })
    expect(screen.getByRole('status')).toHaveTextContent('2 items cleared')

    fireEvent.click(undo)

    await waitFor(() => {
      expect(screen.getByText('Bread')).toBeInTheDocument()
    })
    expect(screen.getByText('Eggs')).toBeInTheDocument()
    // Each cleared item is re-created with its original fields preserved
    expect(fetchMock).toHaveBeenCalledWith('/api/grocery/items', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ content: 'Bread', original_text: 'ขนมปัง', source_language: 'th' }),
    }))
    expect(fetchMock).toHaveBeenCalledWith('/api/grocery/items', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ content: 'Eggs', original_text: '', source_language: 'en' }),
    }))
    // ...and checked again so the pre-clear state is visible
    expect(fetchMock).toHaveBeenCalledWith('/api/grocery/items/4/check', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ checked: true }),
    }))
    expect(fetchMock).toHaveBeenCalledWith('/api/grocery/items/5/check', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ checked: true }),
    }))
    expect(screen.getAllByRole('checkbox', { name: /bread/i })).toHaveLength(1)
  })

  it('drops the undo affordance once the 8s window elapses', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const item = makeItem({ id: 1, content: 'Milk', checked: false })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([item]))
      .mockResolvedValueOnce(ok())
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Milk')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('checkbox', { name: /milk/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument())

    act(() => { vi.advanceTimersByTime(8000) })

    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
    })
    // No undo request fired — only the initial load and the check PATCH
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('replaces a pending undo toast when a second action is taken', async () => {
    const milk = makeItem({ id: 1, content: 'Milk', checked: false })
    const bread = makeItem({ id: 2, content: 'Bread', checked: false, sort_order: 1 })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([milk, bread]))  // initial load
      .mockResolvedValueOnce(ok())                          // check Milk
      .mockResolvedValueOnce(ok())                          // check Bread
      .mockResolvedValueOnce(ok())                          // undo (Bread only)
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Milk')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('checkbox', { name: /milk/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('checkbox', { name: /bread/i }))
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /bread/i })).toHaveAttribute('aria-checked', 'true')
    })
    // The older toast is gone — only the newest action is undoable
    expect(screen.getAllByRole('button', { name: 'Undo' })).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: 'Undo' }))

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /bread/i })).toHaveAttribute('aria-checked', 'false')
    })
    // Milk stays checked — its undo window already closed
    expect(screen.getByRole('checkbox', { name: /milk/i })).toHaveAttribute('aria-checked', 'true')
    expect(fetchMock).toHaveBeenLastCalledWith('/api/grocery/items/2/check', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ checked: false }),
    }))
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('shows an error toast and refetches when the undo request fails', async () => {
    const item = makeItem({ id: 1, content: 'Milk', checked: false })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([item]))                              // initial load
      .mockResolvedValueOnce(ok())                                               // check PATCH
      .mockResolvedValueOnce({ ok: false })                                      // undo PATCH fails
      .mockResolvedValueOnce(itemsResponse([{ ...item, checked: true }]))        // resync
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Milk')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('checkbox', { name: /milk/i }))
    fireEvent.click(await screen.findByRole('button', { name: 'Undo' }))

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent('Failed to undo')
    })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/grocery/items', expect.objectContaining({
      credentials: 'include',
    }))
    // The refetched server state wins over the optimistic uncheck
    expect(screen.getByRole('checkbox', { name: /milk/i })).toHaveAttribute('aria-checked', 'true')
  })
})

// ── Voice input tests ─────────────────────────────────────────────────────────

describe('GroceryPage – voice input', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  function makeMockRecognitionClass(startImpl?: () => void) {
    let instance: InstanceType<ReturnType<typeof makeMockRecognitionClass>> | null = null
    class MockRecognition {
      continuous = false
      interimResults = false
      onresult: ((e: { results: { transcript: string }[][] }) => void) | null = null
      onerror: (() => void) | null = null
      onend: (() => void) | null = null
      start = vi.fn(startImpl)
      stop = vi.fn()
      // eslint-disable-next-line @typescript-eslint/no-this-alias
      constructor() { instance = this }
    }
    return Object.assign(MockRecognition, { getInstance: () => instance })
  }

  it('shows translating indicator and posts correct payloads for voice input', async () => {
    authState.user = { id: 1 }
    const MockRecognition = makeMockRecognitionClass()
    vi.stubGlobal('SpeechRecognition', MockRecognition)

    // Use a deferred promise so we can assert the translating indicator while in-flight
    let resolveTranslate!: (value: unknown) => void
    const translatePromise = new Promise(resolve => { resolveTranslate = resolve })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([]))   // initial load
      .mockImplementationOnce(() => translatePromise) // translate — held open
      .mockResolvedValueOnce({                    // add item
        ok: true,
        json: () => Promise.resolve({ item: makeItem({ id: 2, content: 'Egg' }) }),
      })
      .mockResolvedValueOnce(itemsResponse([makeItem({ id: 2, content: 'Egg' })])) // refetch
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Your grocery list is empty')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: 'Start voice input' }))
    const rec = MockRecognition.getInstance()
    expect(rec).not.toBeNull()
    expect(rec!.start).toHaveBeenCalled()

    // Simulate speech recognition result — kicks off translation
    rec!.onresult!({ results: [[{ transcript: 'ไข่' }]] })

    // Translating indicator should appear while the request is in-flight
    await waitFor(() => {
      expect(screen.getByText('Translating voice input...')).toBeInTheDocument()
    })

    // Verify the translate POST was sent with correct payload
    expect(fetchMock).toHaveBeenCalledWith('/api/grocery/translate', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ text: 'ไข่' }),
    }))

    // Now resolve the translate response
    resolveTranslate({ ok: true, json: () => Promise.resolve({ items: [{ item: 'Egg', original: 'ไข่', language: 'th' }] }) })

    // Translated item should be added via the items endpoint
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/grocery/items', expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ content: 'Egg', original_text: 'ไข่', source_language: 'th' }),
      }))
    })

    // Translating indicator should be gone after completion
    await waitFor(() => {
      expect(screen.queryByText('Translating voice input...')).not.toBeInTheDocument()
    })
  })

  it('shows errors.failedToTranslate when /api/grocery/translate returns non-ok', async () => {
    authState.user = { id: 1 }
    const MockRecognition = makeMockRecognitionClass()
    vi.stubGlobal('SpeechRecognition', MockRecognition)

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(itemsResponse([]))   // initial load
      .mockResolvedValueOnce({ ok: false })        // translate fails
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Your grocery list is empty')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: 'Start voice input' }))
    MockRecognition.getInstance()!.onresult!({ results: [[{ transcript: 'test input' }]] })

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to translate voice input')
    })
  })

  it('shows errors.failedToStartRecording and stays not-recording when start() throws', async () => {
    authState.user = { id: 1 }
    const MockRecognition = makeMockRecognitionClass(() => { throw new Error('permission denied') })
    vi.stubGlobal('SpeechRecognition', MockRecognition)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(itemsResponse([])))

    renderPage()
    await waitFor(() => expect(screen.getByText('Your grocery list is empty')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: 'Start voice input' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to start recording')
    })
    // Button should be back to start state (not recording)
    expect(screen.getByRole('button', { name: 'Start voice input' })).toBeInTheDocument()
  })
})
