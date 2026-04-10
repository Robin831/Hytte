// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import HomeworkParentReview from './HomeworkParentReview'

// ── Translation mock ──────────────────────────────────────────────────────────

const TRANSLATIONS: Record<string, string> = {
  'review.title': 'Homework Review',
  'review.noChildren': 'No children linked to your account',
  'review.noConversations': 'No conversations yet',
  'review.noMessages': 'No messages',
  'review.helpLevelBreakdown': 'Help level breakdown',
  'review.conversationCount': 'conversations',
  'review.messageCount': 'messages',
  'review.messages': 'messages',
  'review.repeatedAnswerAlert': 'Repeated answer alert',
  'review.closeTranscript': 'Close transcript',
  'review.errors.failedToLoad': 'Failed to load children',
  'review.errors.failedToLoadReview': 'Failed to load review',
  'review.errors.failedToLoadTranscript': 'Failed to load transcript',
  'helpLevel.hint': 'Hint',
  'helpLevel.explain': 'Explain',
  'helpLevel.walkthrough': 'Walkthrough',
  'helpLevel.answer': 'Answer',
  'noSubject': 'New topic',
  'dismissError': 'Dismiss error',
}

function stableT(key: string, fallback?: string): string {
  return TRANSLATIONS[key] ?? fallback ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: stableT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: (_date: string, _opts: unknown) => 'Apr 10',
}))

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeChild(overrides: Partial<{
  id: number; parent_id: number; child_id: number; nickname: string; avatar_emoji: string
}> = {}) {
  return {
    id: 1,
    parent_id: 10,
    child_id: 42,
    nickname: 'Alice',
    avatar_emoji: '🐱',
    ...overrides,
  }
}

function makeConversation(overrides: Partial<{
  id: number; kid_id: number; subject: string; created_at: string; updated_at: string;
  message_count: number; help_levels: Record<string, number>; repeated_answer_alert: boolean
}> = {}) {
  return {
    id: 1,
    kid_id: 42,
    subject: 'Maths',
    created_at: '2026-04-09T00:00:00Z',
    updated_at: '2026-04-09T00:00:00Z',
    message_count: 5,
    help_levels: { hint: 2, answer: 1 },
    repeated_answer_alert: false,
    ...overrides,
  }
}

function makeReview(conversations = [makeConversation()]) {
  return {
    conversations,
    total_messages: 5,
    help_level_totals: { hint: 2, answer: 1 },
    help_level_averages: { hint: 0.4, answer: 0.2 },
    average_messages_per_conversation: 5,
  }
}

function makeMessage(overrides: Partial<{
  id: number; conversation_id: number; role: string; content: string;
  help_level: string; created_at: string
}> = {}) {
  return {
    id: 1,
    conversation_id: 1,
    role: 'user',
    content: 'What is 2+2?',
    help_level: 'hint',
    created_at: '2026-04-09T10:00:00Z',
    ...overrides,
  }
}

function childrenResponse(children: ReturnType<typeof makeChild>[]) {
  return { ok: true, json: () => Promise.resolve({ children }) }
}

function reviewResponse(review: ReturnType<typeof makeReview>) {
  return { ok: true, json: () => Promise.resolve({ review }) }
}

function transcriptResponse(messages: ReturnType<typeof makeMessage>[]) {
  return { ok: true, json: () => Promise.resolve({ messages }) }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <HomeworkParentReview />
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('HomeworkParentReview – loading children', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows loading spinner on initial render', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { container } = renderPage()
    expect(container.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('shows empty state when no children', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(childrenResponse([]))))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('No children linked to your account')).toBeInTheDocument()
    })
  })

  it('renders child nickname after load', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve(childrenResponse([makeChild({ nickname: 'Bob' })]))
    ))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Bob')).toBeInTheDocument()
    })
  })

  it('shows error when children load fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false })))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Failed to load children')).toBeInTheDocument()
    })
  })
})

describe('HomeworkParentReview – expanding a child triggers review fetch', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('calls review endpoint when child row is expanded', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(childrenResponse([makeChild()]))
      .mockResolvedValueOnce(reviewResponse(makeReview()))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Alice')).toBeInTheDocument())

    fireEvent.click(screen.getByText('Alice'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/homework/children/42/review',
        expect.objectContaining({ credentials: 'include' }),
      )
    })
  })

  it('does not fetch review again when child is already loaded', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(childrenResponse([makeChild()]))
      .mockResolvedValueOnce(reviewResponse(makeReview()))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Alice')).toBeInTheDocument())

    // Expand
    fireEvent.click(screen.getByText('Alice'))
    await waitFor(() => expect(screen.getByText('Help level breakdown')).toBeInTheDocument())

    // Collapse then expand again
    fireEvent.click(screen.getByText('Alice'))
    fireEvent.click(screen.getByText('Alice'))

    // Still only one review fetch
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

describe('HomeworkParentReview – help level breakdown and alert badge', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders help level breakdown after loading review', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(childrenResponse([makeChild()]))
      .mockResolvedValueOnce(reviewResponse(makeReview([
        makeConversation({ help_levels: { hint: 3, answer: 1 } }),
      ])))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Alice')).toBeInTheDocument())
    fireEvent.click(screen.getByText('Alice'))

    await waitFor(() => {
      expect(screen.getByText('Help level breakdown')).toBeInTheDocument()
      expect(screen.getByText(/Hint/)).toBeInTheDocument()
    })
  })

  it('shows repeated-answer alert badge when flag is set', async () => {
    const conv = makeConversation({ repeated_answer_alert: true })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(childrenResponse([makeChild()]))
      .mockResolvedValueOnce(reviewResponse(makeReview([conv])))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Alice')).toBeInTheDocument())
    fireEvent.click(screen.getByText('Alice'))

    await waitFor(() => {
      // The child row should show an alert count badge
      expect(screen.getByRole('button', { name: /Alice/ })).toBeInTheDocument()
    })

    // Alert badge visible on conversation item
    const alertSpan = await screen.findByLabelText('Repeated answer alert')
    expect(alertSpan).toBeInTheDocument()
  })
})

describe('HomeworkParentReview – transcript modal', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('opens transcript modal when conversation is clicked', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(childrenResponse([makeChild()]))
      .mockResolvedValueOnce(reviewResponse(makeReview([makeConversation({ subject: 'Algebra' })])))
      .mockResolvedValueOnce(transcriptResponse([makeMessage({ content: 'What is x?' })]))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Alice')).toBeInTheDocument())
    fireEvent.click(screen.getByText('Alice'))

    await waitFor(() => expect(screen.getByText('Algebra')).toBeInTheDocument())
    fireEvent.click(screen.getByText('Algebra'))

    await waitFor(() => {
      expect(screen.getByText('What is x?')).toBeInTheDocument()
    })
  })

  it('closes transcript modal and clears loading state', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(childrenResponse([makeChild()]))
      .mockResolvedValueOnce(reviewResponse(makeReview([makeConversation({ subject: 'Algebra' })])))
      .mockResolvedValueOnce(transcriptResponse([makeMessage({ content: 'Some message' })]))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Alice')).toBeInTheDocument())
    fireEvent.click(screen.getByText('Alice'))

    await waitFor(() => expect(screen.getByText('Algebra')).toBeInTheDocument())
    fireEvent.click(screen.getByText('Algebra'))

    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())

    // Close the modal
    fireEvent.click(screen.getByLabelText('Close transcript'))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('aborts in-flight transcript fetch on close', async () => {
    let rejectFetch!: () => void
    const hangingFetch = new Promise<never>((_, reject) => { rejectFetch = reject })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(childrenResponse([makeChild()]))
      .mockResolvedValueOnce(reviewResponse(makeReview([makeConversation({ subject: 'Physics' })])))
      .mockReturnValueOnce(hangingFetch)
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Alice')).toBeInTheDocument())
    fireEvent.click(screen.getByText('Alice'))

    await waitFor(() => expect(screen.getByText('Physics')).toBeInTheDocument())

    // Click conversation — transcript fetch hangs
    fireEvent.click(screen.getByText('Physics'))

    // Modal opens in loading state
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())

    // Close while loading — should abort and close dialog
    fireEvent.click(screen.getByLabelText('Close transcript'))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    // Avoid unhandled rejection from hanging fetch
    rejectFetch()
  })
})
