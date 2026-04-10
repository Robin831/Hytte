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

  it('shows loading spinner on initial render', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { container } = renderPage()
    expect(container.querySelector('.animate-spin')).toBeInTheDocument()
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
