// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import HomeworkPage from './HomeworkPage'

// ── Translation mock ──────────────────────────────────────────────────────────
// stableT must be a stable reference — HomeworkPage's useEffect has `t` as a
// dependency, so a new function on every render would cause an infinite re-run
// loop that burns through fetch mocks out of order.

const TRANSLATIONS: Record<string, string> = {
  'title': 'Homework',
  'newConversation': 'New conversation',
  'noSubject': 'New topic',
  'yesterday': 'Yesterday',
  'empty.noConversations': 'No homework conversations yet',
  'empty.startNew': 'Start a new conversation to get help',
  'errors.failedToLoad': 'Failed to load conversations',
  'errors.failedToCreate': 'Failed to create conversation',
  'errors.failedToRename': 'Failed to rename conversation',
  'errors.failedToDelete': 'Failed to delete conversation',
  'errors.nameRequired': 'Name cannot be empty',
  'loading': 'Loading...',
  'conversationActions': 'Conversation actions',
  'rename': 'Rename',
  'renameLabel': 'Conversation name',
  'delete': 'Delete',
  'cancel': 'Cancel',
  'deleteConfirm': 'Delete this conversation? This cannot be undone.',
  'deleteConfirmYes': 'Delete',
}

function stableT(key: string): string {
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: stableT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: (_date: Date, _opts: unknown) => 'Apr 10',
  formatTime: (_date: Date, _opts: unknown) => '10:00',
}))

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeConversation(overrides: Partial<{
  id: number; subject: string; last_message_preview: string; updated_at: string
}> = {}) {
  return {
    id: 1,
    kid_id: 42,
    subject: 'Maths',
    last_message_preview: '',
    created_at: '2026-04-09T00:00:00Z',
    updated_at: '2026-04-09T00:00:00Z',
    ...overrides,
  }
}

function convListResponse(conversations: ReturnType<typeof makeConversation>[]) {
  return { ok: true, json: () => Promise.resolve({ conversations }) }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <HomeworkPage />
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('HomeworkPage – loading and empty state', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows skeleton loading state on initial render', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    renderPage()
    const status = screen.getByRole('status')
    expect(status).toHaveAttribute('aria-busy', 'true')
  })

  it('shows empty state when no conversations', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(convListResponse([]))))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('No homework conversations yet')).toBeInTheDocument()
    })
  })
})

describe('HomeworkPage – conversation list', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders conversation subject', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve(convListResponse([makeConversation({ subject: 'Algebra' })]))
    ))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Algebra')).toBeInTheDocument()
    })
  })

  it('renders last message preview when present', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve(convListResponse([
        makeConversation({ subject: 'Physics', last_message_preview: 'What is velocity?' }),
      ]))
    ))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('What is velocity?')).toBeInTheDocument()
    })
  })

  it('falls back to noSubject when subject is empty', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve(convListResponse([makeConversation({ subject: '' })]))
    ))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('New topic')).toBeInTheDocument()
    })
  })
})

describe('HomeworkPage – error state', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows error when initial load fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false })))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Failed to load conversations')).toBeInTheDocument()
    })
  })

  it('shows error when create conversation fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convListResponse([]))
      .mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'server error' }) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('No homework conversations yet')).toBeInTheDocument())

    fireEvent.click(screen.getByText('New conversation'))

    await waitFor(() => {
      expect(screen.getByText('server error')).toBeInTheDocument()
    })
  })
})

describe('HomeworkPage – create conversation', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('calls POST /api/homework/conversations on create button click', async () => {
    const newConv = makeConversation({ id: 7, subject: '' })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convListResponse([]))
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ conversation: newConv }) })
      .mockReturnValue(new Promise(() => {}))  // hang any post-navigation fetches
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('No homework conversations yet')).toBeInTheDocument())

    fireEvent.click(screen.getByText('New conversation'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/homework/conversations',
        expect.objectContaining({ method: 'POST' }),
      )
    })
    // No create-error message should appear after a successful POST
    expect(screen.queryByText('Failed to create conversation')).not.toBeInTheDocument()
  })
})

describe('HomeworkPage – overflow menu', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  async function renderWithMenuOpen(conversations = [makeConversation({ subject: 'Algebra' })]) {
    const fetchMock = vi.fn().mockResolvedValue(convListResponse(conversations))
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await waitFor(() => expect(screen.getByText('Algebra')).toBeInTheDocument())
    fireEvent.click(screen.getAllByLabelText('Conversation actions')[0])
    expect(screen.getByRole('menu')).toBeInTheDocument()
    return fetchMock
  }

  it('opens the menu with Rename and Delete items', async () => {
    await renderWithMenuOpen()
    expect(screen.getByRole('menuitem', { name: 'Rename' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toBeInTheDocument()
  })

  it('marks the trigger as expanded while the menu is open', async () => {
    await renderWithMenuOpen()
    expect(screen.getByLabelText('Conversation actions')).toHaveAttribute('aria-expanded', 'true')
  })

  it('closes the menu on Escape', async () => {
    await renderWithMenuOpen()
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu')).not.toBeInTheDocument())
  })

  it('closes the menu on outside click', async () => {
    await renderWithMenuOpen()
    fireEvent.mouseDown(document.body)
    await waitFor(() => expect(screen.queryByRole('menu')).not.toBeInTheDocument())
  })
})

describe('HomeworkPage – rename conversation', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  async function openRename(fetchMock: ReturnType<typeof vi.fn>) {
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await waitFor(() => expect(screen.getByText('Algebra')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText('Conversation actions'))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }))
    return screen.getByLabelText('Conversation name') as HTMLInputElement
  }

  it('PATCHes the trimmed subject and shows it immediately', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convListResponse([makeConversation({ subject: 'Algebra' })]))
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ conversation: makeConversation({ subject: 'Geometry' }) }) })
    const input = await openRename(fetchMock)

    fireEvent.change(input, { target: { value: '  Geometry  ' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/homework/conversations/1',
        expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ subject: 'Geometry' }) }),
      )
    })
    expect(screen.getByText('Geometry')).toBeInTheDocument()
  })

  it('rejects a whitespace-only name without firing a request', async () => {
    const fetchMock = vi.fn().mockResolvedValue(convListResponse([makeConversation({ subject: 'Algebra' })]))
    const input = await openRename(fetchMock)

    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(screen.getByText('Name cannot be empty')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(1) // only the initial list load
  })

  it('cancels rename on Escape leaving the original subject', async () => {
    const fetchMock = vi.fn().mockResolvedValue(convListResponse([makeConversation({ subject: 'Algebra' })]))
    const input = await openRename(fetchMock)

    fireEvent.change(input, { target: { value: 'Geometry' } })
    fireEvent.keyDown(input, { key: 'Escape' })

    await waitFor(() => expect(screen.getByText('Algebra')).toBeInTheDocument())
    expect(screen.queryByText('Geometry')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('rolls back and shows an inline error when PATCH fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convListResponse([makeConversation({ subject: 'Algebra' })]))
      .mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({}) })
    const input = await openRename(fetchMock)

    fireEvent.change(input, { target: { value: 'Geometry' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(screen.getByText('Failed to rename conversation')).toBeInTheDocument())
    expect(screen.getByText('Algebra')).toBeInTheDocument()
    expect(screen.queryByText('Geometry')).not.toBeInTheDocument()
  })
})

describe('HomeworkPage – delete conversation', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  async function openDeleteMenu(fetchMock: ReturnType<typeof vi.fn>) {
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await waitFor(() => expect(screen.getByText('Algebra')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText('Conversation actions'))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }))
  }

  it('asks for confirmation before deleting', async () => {
    const fetchMock = vi.fn().mockResolvedValue(convListResponse([makeConversation({ subject: 'Algebra' })]))
    await openDeleteMenu(fetchMock)

    expect(screen.getByText('Delete this conversation? This cannot be undone.')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(screen.getByText('Algebra')).toBeInTheDocument()
  })

  it('cancelling the confirmation leaves the row alone', async () => {
    const fetchMock = vi.fn().mockResolvedValue(convListResponse([makeConversation({ subject: 'Algebra' })]))
    await openDeleteMenu(fetchMock)

    fireEvent.click(screen.getByRole('menuitem', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByText('Delete this conversation? This cannot be undone.')).not.toBeInTheDocument()
    })
    expect(screen.getByText('Algebra')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('removes the row and calls DELETE on confirm', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convListResponse([makeConversation({ subject: 'Algebra' })]))
      .mockResolvedValueOnce({ ok: true, status: 204, json: () => Promise.resolve({}) })
    await openDeleteMenu(fetchMock)

    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/homework/conversations/1',
        expect.objectContaining({ method: 'DELETE' }),
      )
    })
    expect(screen.queryByText('Algebra')).not.toBeInTheDocument()
    expect(screen.getByText('No homework conversations yet')).toBeInTheDocument()
  })

  it('restores the row and shows an inline error when DELETE fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convListResponse([makeConversation({ subject: 'Algebra' })]))
      .mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({}) })
    await openDeleteMenu(fetchMock)

    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }))

    await waitFor(() => expect(screen.getByText('Failed to delete conversation')).toBeInTheDocument())
    expect(screen.getByText('Algebra')).toBeInTheDocument()
  })
})
